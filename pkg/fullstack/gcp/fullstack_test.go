package gcp_test

import (
	"encoding/base64"
	"testing"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/apigateway"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudrunv2"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davidmontoyago/pulumi-gcp-fullstack/pkg/fullstack/gcp"
)

const (
	testProjectName     = "test-project"
	backendServiceName  = "backend"
	frontendServiceName = "frontend"
	testRegion          = "us-central1"
)

type fullstackMocks struct{}

func (m *fullstackMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	outputs := map[string]interface{}{}
	for k, v := range args.Inputs {
		outputs[string(k)] = v
	}

	// Mock resource outputs for each resource type:
	switch args.TypeToken {
	case "gcp:serviceaccount/account:Account":
		outputs["name"] = args.Name
		outputs["accountId"] = args.Name + "123" // Mock accountId
		outputs["project"] = testProjectName
		outputs["displayName"] = args.Name
		outputs["email"] = args.Name + "@test-project.iam.gserviceaccount.com"
		// Expected outputs: name, accountId, project, displayName, email
	case "gcp:cloudrunv2/service:Service":
		outputs["name"] = args.Name
		outputs["location"] = testRegion
		outputs["project"] = testProjectName
		outputs["uri"] = "https://" + args.Name + "-hash-uc.a.run.app"
		// Expected outputs: name, location, project, uri, template
	case "gcp:secretmanager/secret:Secret":
		outputs["secretId"] = args.Name
		outputs["project"] = testProjectName
		outputs["labels"] = map[string]string{"env": "production"}
		// Expected outputs: secretId, project, labels
	case "gcp:secretmanager/secretIamMember:SecretIamMember":
		outputs["secretId"] = args.Name
		outputs["role"] = "roles/secretmanager.secretAccessor"
		outputs["member"] = "user:test-user@example.com"
		// Expected outputs: secretId, role, member
	case "gcp:secretmanager/secretVersion:SecretVersion":
		outputs["name"] = args.Name
		outputs["secret"] = args.Name + "-secret-id"
		outputs["version"] = "1"
		outputs["createTime"] = "2023-01-01T00:00:00Z"
		// Expected outputs: name, secret, version, createTime
	case "gcp:apigateway/api:Api":
		outputs["apiId"] = args.Name
		outputs["name"] = args.Name
		outputs["project"] = testProjectName
		outputs["displayName"] = args.Name
		// Expected outputs: apiId, name, project, displayName
	case "gcp:apigateway/apiConfig:ApiConfig":
		outputs["apiConfigId"] = args.Name
		outputs["name"] = args.Name
		outputs["api"] = args.Name + "123" // Mock apiId
		outputs["project"] = testProjectName
		// Expected outputs: apiConfigId, name, api, project
	case "gcp:apigateway/gateway:Gateway":
		outputs["name"] = args.Name
		outputs["region"] = testRegion
		outputs["project"] = testProjectName
		outputs["apiConfig"] = args.Name + "123" // Mock apiConfigId
		outputs["defaultHostname"] = args.Name + ".apigateway.test-project.cloud.goog"
		// Expected outputs: name, region, project, apiConfig, defaultHostname
	case "gcp:compute/managedSslCertificate:ManagedSslCertificate":
		outputs["name"] = args.Name
		outputs["project"] = testProjectName
		outputs["managed"] = map[string]interface{}{
			"domains": []string{"myapp.example.com"},
		}
		// Expected outputs: name, project, managed
	case "gcp:compute/regionNetworkEndpointGroup:RegionNetworkEndpointGroup":
		outputs["name"] = args.Name
		outputs["project"] = testProjectName
		outputs["region"] = testRegion
		outputs["networkEndpointType"] = "SERVERLESS"

		// Check if this is an API Gateway NEG (has serverlessDeployment) or
		// Cloud Run NEG (has cloudRun) and pass values around.
		if serverlessDeployment, ok := args.Inputs["serverlessDeployment"]; ok {
			// API Gateway NEG
			deploymentMap := serverlessDeployment.ObjectValue()
			outputs["serverlessDeployment"] = deploymentMap
		} else if cloudRun, ok := args.Inputs["cloudRun"]; ok && !cloudRun.IsNull() {
			// Cloud Run NEG
			cloudRunMap := cloudRun.ObjectValue()
			outputs["cloudRun"] = cloudRunMap
		}
	// Expected outputs: name, project, region, networkEndpointType, serverlessDeployment/cloudRun
	case "gcp:compute/globalAddress:GlobalAddress":
		// Add mock values for key outputs
		outputs["address"] = "34.102.136.185"
		outputs["creationTimestamp"] = "2023-01-01T00:00:00.000-00:00"
		outputs["labelFingerprint"] = "42WmSpB8rSM="
		outputs["selfLink"] = "https://www.googleapis.com/compute/v1/projects/" + testProjectName + "/global/addresses/" + args.Name
		// Expected outputs: name, project, address, description, ipVersion, creationTimestamp, labelFingerprint, selfLink
	case "gcp:compute/globalForwardingRule:GlobalForwardingRule":
		// Expected outputs: name, project, description, portRange, loadBalancingScheme, ipAddress, target
	case "gcp:compute/targetHttpsProxy:TargetHttpsProxy":
		// Expected outputs: name, project, description, urlMap, sslCertificates
	case "gcp:compute/backendService:BackendService":
		// Expected outputs: name, project, description, protocol, portName, timeoutSec, healthChecks
	case "gcp:compute/urlMap:URLMap":
		// Expected outputs: name, project, description, defaultService
	case "gcp:compute/subnetwork:Subnetwork":
		// Expected outputs: name, project, region, description, purpose, network, ipCidrRange, role
	case "gcp:compute/securityPolicy:SecurityPolicy":
		// Expected outputs: name, project, description, type
	case "gcp:dns/recordSet:RecordSet":
		// Expected outputs: name, managedZone, type, ttl, rrdatas, project
	}

	return args.Name + "_id", resource.NewPropertyMapFromMap(outputs), nil
}

func (m *fullstackMocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	switch args.Token {
	case "gcp:dns/getManagedZones:getManagedZones":
		// Mock DNS zones lookup
		outputs := map[string]interface{}{
			"managedZones": []map[string]interface{}{
				{
					"description":   "Example DNS zone",
					"dnsName":       "example.com.",
					"id":            "example-com-id",
					"managedZoneId": "example-com-id",
					"name":          "example-com",
					"nameServers":   []string{"ns-cloud-a1.googledomains.com.", "ns-cloud-a2.googledomains.com."},
					"project":       testProjectName,
					"visibility":    "public",
				},
			},
		}

		return resource.NewPropertyMapFromMap(outputs), nil
	default:
		return resource.PropertyMap{}, nil
	}
}

func TestNewFullStack_HappyPath(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		args := &gcp.FullStackArgs{
			Project:       testProjectName,
			Region:        testRegion,
			BackendName:   backendServiceName,
			BackendImage:  pulumi.String("gcr.io/test-project/backend:latest"),
			FrontendName:  frontendServiceName,
			FrontendImage: pulumi.String("gcr.io/test-project/frontend:latest"),
			Backend: &gcp.BackendArgs{
				InstanceArgs: &gcp.InstanceArgs{
					ResourceLimits: pulumi.StringMap{
						"memory": pulumi.String("512Mi"),
						"cpu":    pulumi.String("500m"),
					},
					EnvVars: map[string]string{
						"LOG_LEVEL":    "info",
						"DATABASE_URL": "postgresql://localhost:5432/testdb",
					},
					MaxInstanceCount:   5,
					DeletionProtection: true,
					ContainerPort:      8080,
				},
			},
			Frontend: &gcp.FrontendArgs{
				InstanceArgs: &gcp.InstanceArgs{
					ResourceLimits: pulumi.StringMap{
						"memory": pulumi.String("1Gi"),
						"cpu":    pulumi.String("1000m"),
					},
					SecretConfigFileName: ".env.production",
					SecretConfigFilePath: "/app/.next/config/",
					EnvVars: map[string]string{
						"NODE_ENV":            "production",
						"NEXT_PUBLIC_API_URL": "https://api.example.com",
						"ANALYTICS_ID":        "GA-123456789",
					},
					MaxInstanceCount:   3,
					DeletionProtection: false,
					ContainerPort:      3000,
				},
				EnableUnauthenticated: false,
			},
			Network: &gcp.NetworkArgs{
				DomainURL: "myapp.example.com",
				APIGateway: &gcp.APIGatewayArgs{
					Name: "gateway",
					Config: &gcp.APIConfigArgs{
						EnableCORS: true,
					},
				},
			},
		}

		fullstack, err := gcp.NewFullStack(ctx, "test-fullstack", args)
		require.NoError(t, err)

		// Verify basic properties
		assert.Equal(t, testProjectName, fullstack.Project)
		assert.Equal(t, testRegion, fullstack.Region)
		assert.Equal(t, backendServiceName, fullstack.BackendName)
		assert.Equal(t, frontendServiceName, fullstack.FrontendName)

		// Verify backend and frontend images using async pattern
		stackBackendImageCh := make(chan string, 1)
		defer close(stackBackendImageCh)
		fullstack.BackendImage.ApplyT(func(image string) error {
			stackBackendImageCh <- image

			return nil
		})
		assert.Equal(t, "gcr.io/test-project/backend:latest", <-stackBackendImageCh, "Backend image should match")

		stackFrontendImageCh := make(chan string, 1)
		defer close(stackFrontendImageCh)
		fullstack.FrontendImage.ApplyT(func(image string) error {
			stackFrontendImageCh <- image

			return nil
		})
		assert.Equal(t, "gcr.io/test-project/frontend:latest", <-stackFrontendImageCh, "Frontend image should match")

		// Verify backend service configuration
		backendService := fullstack.GetBackendService()
		require.NotNil(t, backendService, "Backend service should not be nil")

		// Verify backend service basic properties
		backendProjectCh := make(chan string, 1)
		defer close(backendProjectCh)
		backendService.Project.ApplyT(func(project string) error {
			backendProjectCh <- project

			return nil
		})
		assert.Equal(t, testProjectName, <-backendProjectCh, "Backend service project should match")

		portCh := make(chan int, 1)
		defer close(portCh)
		backendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			portCh <- *containers[0].Ports.ContainerPort

			return nil
		})
		assert.Equal(t, 8080, <-portCh, "Backend container port should be 8080")

		// Assert backend image is set correctly
		backendImageCh := make(chan string, 1)
		defer close(backendImageCh)
		backendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			backendImageCh <- containers[0].Image

			return nil
		})
		assert.Equal(t, "gcr.io/test-project/backend:latest", <-backendImageCh, "Backend image should match the provided image")

		// Verify frontend service configuration
		frontendService := fullstack.GetFrontendService()
		require.NotNil(t, frontendService, "Frontend service should not be nil")

		// Verify frontend service basic properties
		frontendProjectCh := make(chan string, 1)
		defer close(frontendProjectCh)
		frontendService.Project.ApplyT(func(project string) error {
			frontendProjectCh <- project

			return nil
		})
		assert.Equal(t, testProjectName, <-frontendProjectCh, "Frontend service project should match")

		frontendLocationCh := make(chan string, 1)
		defer close(frontendLocationCh)
		frontendService.Location.ApplyT(func(location string) error {
			frontendLocationCh <- location

			return nil
		})
		assert.Equal(t, testRegion, <-frontendLocationCh, "Frontend service location should match")

		// Assert frontend container port is set correctly
		frontendPortCh := make(chan int, 1)
		defer close(frontendPortCh)
		frontendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			frontendPortCh <- *containers[0].Ports.ContainerPort

			return nil
		})
		assert.Equal(t, 3000, <-frontendPortCh, "Frontend container port should be 3000")

		// Assert frontend image is set correctly
		frontendImageCh := make(chan string, 1)
		defer close(frontendImageCh)
		frontendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			frontendImageCh <- containers[0].Image

			return nil
		})
		assert.Equal(t, "gcr.io/test-project/frontend:latest", <-frontendImageCh, "Frontend image should match the provided image")

		// Assert frontend container probe configurations
		frontendContainerCh := make(chan cloudrunv2.ServiceTemplateContainer, 1)
		defer close(frontendContainerCh)
		frontendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			frontendContainerCh <- containers[0]

			return nil
		})
		frontendContainer := <-frontendContainerCh

		// Assert StartupProbe is configured
		assert.NotNil(t, frontendContainer.StartupProbe, "Frontend container should have StartupProbe configured")
		assert.NotNil(t, frontendContainer.StartupProbe.TcpSocket, "Frontend container StartupProbe should have TcpSocket configured")
		assert.Equal(t, 3000, *frontendContainer.StartupProbe.TcpSocket.Port, "Frontend container StartupProbe should use port 3000")

		// Assert LivenessProbe is configured
		assert.NotNil(t, frontendContainer.LivenessProbe, "Frontend container should have LivenessProbe configured")
		assert.NotNil(t, frontendContainer.LivenessProbe.HttpGet, "Frontend container LivenessProbe should have HttpGet configured")
		assert.Equal(t, "/healthz", *frontendContainer.LivenessProbe.HttpGet.Path, "Frontend container LivenessProbe should use /healthz path")
		assert.Equal(t, 3000, *frontendContainer.LivenessProbe.HttpGet.Port, "Frontend container LivenessProbe should use port 3000")

		// Verify API Gateway configuration
		apiGateway := fullstack.GetAPIGateway()
		require.NotNil(t, apiGateway, "API Gateway should not be nil")

		// Assert gateway region is set correctly
		gatewayRegionCh := make(chan string, 1)
		defer close(gatewayRegionCh)
		apiGateway.Region.ApplyT(func(region string) error {
			gatewayRegionCh <- region

			return nil
		})
		assert.Equal(t, testRegion, <-gatewayRegionCh, "API Gateway region should match the project region")

		// Verify IAM member configurations
		backendIamMember := fullstack.GetBackendGatewayIamMember()
		require.NotNil(t, backendIamMember, "Backend IAM member should not be nil")

		// Assert backend IAM member Name matches the backend service name
		backendIamMemberNameCh := make(chan string, 1)
		defer close(backendIamMemberNameCh)
		backendIamMember.Name.ApplyT(func(name string) error {
			backendIamMemberNameCh <- name

			return nil
		})
		backendServiceNameCh := make(chan string, 1)
		defer close(backendServiceNameCh)
		backendService.Name.ApplyT(func(name string) error {
			backendServiceNameCh <- name

			return nil
		})
		assert.Equal(t, <-backendServiceNameCh, <-backendIamMemberNameCh, "Backend IAM member Name should match the backend service name")

		frontendIamMember := fullstack.GetFrontendGatewayIamMember()
		require.NotNil(t, frontendIamMember, "Frontend IAM member should not be nil")

		// Assert frontend IAM member Name matches the frontend service name
		frontendIamMemberNameCh := make(chan string, 1)
		defer close(frontendIamMemberNameCh)
		frontendIamMember.Name.ApplyT(func(name string) error {
			frontendIamMemberNameCh <- name

			return nil
		})
		frontendServiceNameCh := make(chan string, 1)
		defer close(frontendServiceNameCh)
		frontendService.Name.ApplyT(func(name string) error {
			frontendServiceNameCh <- name

			return nil
		})
		assert.Equal(t, <-frontendServiceNameCh, <-frontendIamMemberNameCh, "Frontend IAM member Name should match the frontend service name")

		// Verify backend volume mount configuration
		backendVolumeMountCh := make(chan cloudrunv2.ServiceTemplateContainerVolumeMount, 1)
		defer close(backendVolumeMountCh)
		backendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			if len(containers) > 0 && len(containers[0].VolumeMounts) > 0 {
				backendVolumeMountCh <- containers[0].VolumeMounts[0]
			}

			return nil
		})
		backendVolumeMount := <-backendVolumeMountCh
		assert.Equal(t, "/app/config/", backendVolumeMount.MountPath, "Backend volume mount path should be /app/config/")

		// Verify frontend volume mount configuration
		frontendVolumeMountCh := make(chan cloudrunv2.ServiceTemplateContainerVolumeMount, 1)
		defer close(frontendVolumeMountCh)
		frontendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			if len(containers) > 0 && len(containers[0].VolumeMounts) > 0 {
				frontendVolumeMountCh <- containers[0].VolumeMounts[0]
			}

			return nil
		})
		frontendVolumeMount := <-frontendVolumeMountCh
		assert.Equal(t, "/app/.next/config/", frontendVolumeMount.MountPath, "Frontend volume mount path should be /app/.next/config/")

		// Verify backend service has StartupProbe and LivenessProbe configured
		backendContainerCh := make(chan cloudrunv2.ServiceTemplateContainer, 1)
		defer close(backendContainerCh)
		backendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			if len(containers) > 0 {
				backendContainerCh <- containers[0]
			}

			return nil
		})
		backendContainer := <-backendContainerCh

		// Assert StartupProbe is configured
		assert.NotNil(t, backendContainer.StartupProbe, "Backend container should have StartupProbe configured")
		assert.NotNil(t, backendContainer.StartupProbe.TcpSocket, "Backend container StartupProbe should have TcpSocket configured")
		assert.Equal(t, 8080, *backendContainer.StartupProbe.TcpSocket.Port, "Backend container StartupProbe should use port 8080")

		// Assert LivenessProbe is configured
		assert.NotNil(t, backendContainer.LivenessProbe, "Backend container should have LivenessProbe configured")
		assert.NotNil(t, backendContainer.LivenessProbe.HttpGet, "Backend container LivenessProbe should have HttpGet configured")
		assert.Equal(t, "/healthz", *backendContainer.LivenessProbe.HttpGet.Path, "Backend container LivenessProbe should use /healthz path")
		assert.Equal(t, 8080, *backendContainer.LivenessProbe.HttpGet.Port, "Backend container LivenessProbe should use port 8080")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}

func TestNewFullStack_WithDefaults(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		args := &gcp.FullStackArgs{
			Project:       testProjectName,
			Region:        testRegion,
			BackendName:   backendServiceName,
			BackendImage:  pulumi.String("gcr.io/test-project/backend:latest"),
			FrontendName:  frontendServiceName,
			FrontendImage: pulumi.String("gcr.io/test-project/frontend:latest"),
			Backend: &gcp.BackendArgs{
				InstanceArgs: &gcp.InstanceArgs{
					// ResourceLimits: pulumi.StringMap{
					// 	"memory": pulumi.String("512Mi"),
					// 	"cpu":    pulumi.String("500m"),
					// },
					// EnvVars: map[string]string{
					// 	"LOG_LEVEL":    "info",
					// 	"DATABASE_URL": "postgresql://localhost:5432/testdb",
					// },
					// MaxInstanceCount:   5,
					// DeletionProtection: true,
					// ContainerPort:      8080,
				},
			},
			Frontend: &gcp.FrontendArgs{
				InstanceArgs: &gcp.InstanceArgs{
					// ResourceLimits: pulumi.StringMap{
					// 	"memory": pulumi.String("1Gi"),
					// 	"cpu":    pulumi.String("1000m"),
					// },
					// SecretConfigFileName: ".env.production",
					// SecretConfigFilePath: "/app/.next/config/",
					// EnvVars: map[string]string{
					// 	"NODE_ENV":            "production",
					// 	"NEXT_PUBLIC_API_URL": "https://api.example.com",
					// 	"ANALYTICS_ID":        "GA-123456789",
					// },
					// MaxInstanceCount:   3,
					// DeletionProtection: false,
					// ContainerPort:      3000,
				},
				EnableUnauthenticated: false,
			},
			Network: &gcp.NetworkArgs{
				DomainURL: "myapp.example.com",
				// APIGateway: &gcp.APIGatewayArgs{
				// 	Name: "gateway",
				// 	Config: &gcp.APIConfigArgs{
				// 		EnableCORS: true,
				// 	},
				// },
			},
		}

		fullstack, err := gcp.NewFullStack(ctx, "test-fullstack", args)
		require.NoError(t, err)

		// Verify basic properties
		assert.Equal(t, testProjectName, fullstack.Project)
		assert.Equal(t, testRegion, fullstack.Region)
		assert.Equal(t, backendServiceName, fullstack.BackendName)
		assert.Equal(t, frontendServiceName, fullstack.FrontendName)

		// Verify backend and frontend images using async pattern
		stackBackendImageCh := make(chan string, 1)
		defer close(stackBackendImageCh)
		fullstack.BackendImage.ApplyT(func(image string) error {
			stackBackendImageCh <- image

			return nil
		})
		assert.Equal(t, "gcr.io/test-project/backend:latest", <-stackBackendImageCh, "Backend image should match")

		stackFrontendImageCh := make(chan string, 1)
		defer close(stackFrontendImageCh)
		fullstack.FrontendImage.ApplyT(func(image string) error {
			stackFrontendImageCh <- image

			return nil
		})
		assert.Equal(t, "gcr.io/test-project/frontend:latest", <-stackFrontendImageCh, "Frontend image should match")

		// Verify backend service configuration
		backendService := fullstack.GetBackendService()
		require.NotNil(t, backendService, "Backend service should not be nil")

		// Verify backend service basic properties
		backendProjectCh := make(chan string, 1)
		defer close(backendProjectCh)
		backendService.Project.ApplyT(func(project string) error {
			backendProjectCh <- project

			return nil
		})
		assert.Equal(t, testProjectName, <-backendProjectCh, "Backend service project should match")

		portCh := make(chan int, 1)
		defer close(portCh)
		backendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			portCh <- *containers[0].Ports.ContainerPort

			return nil
		})
		assert.Equal(t, 4001, <-portCh, "Backend container port should be 4001")

		// Assert backend image is set correctly
		backendImageCh := make(chan string, 1)
		defer close(backendImageCh)
		backendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			backendImageCh <- containers[0].Image

			return nil
		})
		assert.Equal(t, "gcr.io/test-project/backend:latest", <-backendImageCh, "Backend image should match the provided image")

		// Verify frontend service configuration
		frontendService := fullstack.GetFrontendService()
		require.NotNil(t, frontendService, "Frontend service should not be nil")

		// Verify frontend service basic properties
		frontendProjectCh := make(chan string, 1)
		defer close(frontendProjectCh)
		frontendService.Project.ApplyT(func(project string) error {
			frontendProjectCh <- project

			return nil
		})
		assert.Equal(t, testProjectName, <-frontendProjectCh, "Frontend service project should match")

		frontendLocationCh := make(chan string, 1)
		defer close(frontendLocationCh)
		frontendService.Location.ApplyT(func(location string) error {
			frontendLocationCh <- location

			return nil
		})
		assert.Equal(t, testRegion, <-frontendLocationCh, "Frontend service location should match")

		// Assert frontend container port is set correctly
		frontendPortCh := make(chan int, 1)
		defer close(frontendPortCh)
		frontendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			frontendPortCh <- *containers[0].Ports.ContainerPort

			return nil
		})
		assert.Equal(t, 3000, <-frontendPortCh, "Frontend container port should be 3000")

		// Assert frontend image is set correctly
		frontendImageCh := make(chan string, 1)
		defer close(frontendImageCh)
		frontendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			frontendImageCh <- containers[0].Image

			return nil
		})
		assert.Equal(t, "gcr.io/test-project/frontend:latest", <-frontendImageCh, "Frontend image should match the provided image")

		// Verify API Gateway configuration
		apiGateway := fullstack.GetAPIGateway()
		require.NotNil(t, apiGateway, "API Gateway should not be nil")

		// Assert gateway region is set correctly
		gatewayRegionCh := make(chan string, 1)
		defer close(gatewayRegionCh)
		apiGateway.Region.ApplyT(func(region string) error {
			gatewayRegionCh <- region

			return nil
		})
		assert.Equal(t, testRegion, <-gatewayRegionCh, "API Gateway region should match the project region")

		// Verify API Config properties
		apiConfig := fullstack.GetAPIConfig()
		require.NotNil(t, apiConfig, "API Config should not be nil")

		// Assert API Config has OpenAPI documents
		openapiDocumentsCh := make(chan []apigateway.ApiConfigOpenapiDocument, 1)
		defer close(openapiDocumentsCh)
		apiConfig.OpenapiDocuments.ApplyT(func(documents []apigateway.ApiConfigOpenapiDocument) error {
			openapiDocumentsCh <- documents

			return nil
		})
		openapiDocuments := <-openapiDocumentsCh
		require.NotNil(t, openapiDocuments, "OpenAPI documents should not be nil")
		assert.Len(t, openapiDocuments, 1, "Should have exactly one OpenAPI document")

		// Assert API Config has Gateway configuration
		gatewayConfigCh := make(chan *apigateway.ApiConfigGatewayConfig, 1)
		defer close(gatewayConfigCh)
		apiConfig.GatewayConfig.ApplyT(func(config *apigateway.ApiConfigGatewayConfig) error {
			gatewayConfigCh <- config

			return nil
		})
		gatewayConfig := <-gatewayConfigCh
		require.NotNil(t, gatewayConfig, "Gateway config should not be nil")
		require.NotNil(t, gatewayConfig.BackendConfig, "Backend config should not be nil")

		// Assert the OpenAPI document contents can be decoded from base64
		document := openapiDocuments[0]
		require.NotNil(t, document.Document, "Document should not be nil")

		base64Contents := document.Document.Contents
		require.NotEmpty(t, base64Contents, "Document contents should not be empty")

		// Verify the contents can be decoded from base64
		decodedBytes, err := base64.StdEncoding.DecodeString(base64Contents)
		require.NoError(t, err, "Document contents should be valid base64")
		require.NotEmpty(t, decodedBytes, "Decoded contents should not be empty")

		// Verify the decoded content is valid JSON/OpenAPI 2.0 spec
		decodedContent := string(decodedBytes)
		assert.Contains(t, decodedContent, "\"swagger\":\"2.0\"", "Decoded content should contain OpenAPI 2.0 version")
		assert.Contains(t, decodedContent, "\"paths\":", "Decoded content should contain paths section")
		assert.Contains(t, decodedContent, "\"info\":", "Decoded content should contain info section")

		// Verify IAM member configurations
		backendIamMember := fullstack.GetBackendGatewayIamMember()
		require.NotNil(t, backendIamMember, "Backend IAM member should not be nil")

		// Assert backend IAM member Name matches the backend service name
		backendIamMemberNameCh := make(chan string, 1)
		defer close(backendIamMemberNameCh)
		backendIamMember.Name.ApplyT(func(name string) error {
			backendIamMemberNameCh <- name

			return nil
		})
		backendServiceNameCh := make(chan string, 1)
		defer close(backendServiceNameCh)
		backendService.Name.ApplyT(func(name string) error {
			backendServiceNameCh <- name

			return nil
		})
		assert.Equal(t, <-backendServiceNameCh, <-backendIamMemberNameCh, "Backend IAM member Name should match the backend service name")

		frontendIamMember := fullstack.GetFrontendGatewayIamMember()
		require.NotNil(t, frontendIamMember, "Frontend IAM member should not be nil")

		// Assert frontend IAM member Name matches the frontend service name
		frontendIamMemberNameCh := make(chan string, 1)
		defer close(frontendIamMemberNameCh)
		frontendIamMember.Name.ApplyT(func(name string) error {
			frontendIamMemberNameCh <- name

			return nil
		})
		frontendServiceNameCh := make(chan string, 1)
		defer close(frontendServiceNameCh)
		frontendService.Name.ApplyT(func(name string) error {
			frontendServiceNameCh <- name

			return nil
		})
		assert.Equal(t, <-frontendServiceNameCh, <-frontendIamMemberNameCh, "Frontend IAM member Name should match the frontend service name")

		// Verify certificate configuration
		certificate := fullstack.GetCertificate()
		require.NotNil(t, certificate, "Certificate should not be nil")

		// Assert certificate name is set correctly
		certificateNameCh := make(chan string, 1)
		defer close(certificateNameCh)
		certificate.Name.ApplyT(func(name string) error {
			certificateNameCh <- name

			return nil
		})
		assert.Contains(t, <-certificateNameCh, "gcp-lb-tls-cert", "Certificate name should contain expected pattern")

		// Assert certificate domains match the provided domain
		certificateDomainsCh := make(chan []string, 1)
		defer close(certificateDomainsCh)
		certificate.Managed.ApplyT(func(managed *compute.ManagedSslCertificateManaged) error {
			certificateDomainsCh <- managed.Domains

			return nil
		})
		domains := <-certificateDomainsCh
		require.NotNil(t, domains, "Certificate domains should not be nil")
		assert.Len(t, domains, 1, "Certificate should have exactly one domain")
		assert.Equal(t, "myapp.example.com", domains[0], "Certificate domain should match the provided domain")

		// Verify NEG configuration
		neg := fullstack.GetNEG()
		require.NotNil(t, neg, "NEG should not be nil")

		// Assert NEG name is set correctly
		negNameCh := make(chan string, 1)
		defer close(negNameCh)
		neg.Name.ApplyT(func(name string) error {
			negNameCh <- name

			return nil
		})
		assert.Equal(t, <-negNameCh, "test-fullstack-gcp-lb-gateway-neg", "NEG name should match convention")

		// Assert NEG platform is set correctly for API Gateway
		negPlatformCh := make(chan string, 1)
		defer close(negPlatformCh)
		neg.ServerlessDeployment.ApplyT(func(deployment *compute.RegionNetworkEndpointGroupServerlessDeployment) error {
			negPlatformCh <- deployment.Platform

			return nil
		})
		assert.Equal(t, <-negPlatformCh, "apigateway.googleapis.com", "NEG platform should be set to apigateway.googleapis.com for API Gateway")

		// Assert NEG resource matches the API Gateway name
		negResourceCh := make(chan string, 1)
		defer close(negResourceCh)
		neg.ServerlessDeployment.ApplyT(func(deployment *compute.RegionNetworkEndpointGroupServerlessDeployment) error {
			negResourceCh <- *deployment.Resource

			return nil
		})
		assert.Equal(t, "test-fullstack-gateway", <-negResourceCh, "NEG resource should match the API Gateway ID")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}

func TestNewFullStack_WithGlobalInternetLoadBalancer(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		args := &gcp.FullStackArgs{
			Project:       testProjectName,
			Region:        testRegion,
			BackendName:   backendServiceName,
			BackendImage:  pulumi.String("gcr.io/test-project/backend:latest"),
			FrontendName:  frontendServiceName,
			FrontendImage: pulumi.String("gcr.io/test-project/frontend:latest"),
			Backend: &gcp.BackendArgs{
				InstanceArgs: &gcp.InstanceArgs{},
			},
			Frontend: &gcp.FrontendArgs{
				InstanceArgs: &gcp.InstanceArgs{},
			},
			Network: &gcp.NetworkArgs{
				DomainURL: "myapp.example.com",
			},
		}

		fullstack, err := gcp.NewFullStack(ctx, "test-fullstack", args)
		require.NoError(t, err)

		// Verify global forwarding rule configuration
		globalForwardingRule := fullstack.GetGlobalForwardingRule()
		require.NotNil(t, globalForwardingRule, "Global forwarding rule should not be nil")

		// Assert forwarding rule has a non-null IP address
		forwardingRuleIPCh := make(chan string, 1)
		defer close(forwardingRuleIPCh)
		globalForwardingRule.IpAddress.ApplyT(func(ipAddress string) error {
			forwardingRuleIPCh <- ipAddress

			return nil
		})
		ipAddress := <-forwardingRuleIPCh
		assert.NotEmpty(t, ipAddress, "Forwarding rule IP address should not be empty")

		// Verify certificate configuration (simplified)
		certificate := fullstack.GetCertificate()
		require.NotNil(t, certificate, "Certificate should not be nil")

		// Assert certificate domains match the provided domain
		certificateDomainsCh := make(chan []string, 1)
		defer close(certificateDomainsCh)
		certificate.Managed.ApplyT(func(managed *compute.ManagedSslCertificateManaged) error {
			certificateDomainsCh <- managed.Domains

			return nil
		})
		domains := <-certificateDomainsCh
		require.NotNil(t, domains, "Certificate domains should not be nil")
		assert.Len(t, domains, 1, "Certificate should have exactly one domain")
		assert.Equal(t, "myapp.example.com", domains[0], "Certificate domain should match the provided domain")

		// Verify DNS record configuration
		dnsRecord := fullstack.GetDNSRecord()
		require.NotNil(t, dnsRecord, "DNS record should not be nil")

		// Assert DNS record name matches the provided domain
		dnsRecordNameCh := make(chan string, 1)
		defer close(dnsRecordNameCh)
		dnsRecord.Name.ApplyT(func(name string) error {
			dnsRecordNameCh <- name

			return nil
		})
		assert.Equal(t, "myapp.example.com", <-dnsRecordNameCh, "DNS record name should match the provided domain")

		// Assert DNS record type is A
		dnsRecordTypeCh := make(chan string, 1)
		defer close(dnsRecordTypeCh)
		dnsRecord.Type.ApplyT(func(recordType string) error {
			dnsRecordTypeCh <- recordType

			return nil
		})
		assert.Equal(t, "A", <-dnsRecordTypeCh, "DNS record type should be A")

		// Assert DNS record IP address matches the forwarding rule IP address
		dnsRecordRrdatasCh := make(chan []string, 1)
		defer close(dnsRecordRrdatasCh)
		dnsRecord.Rrdatas.ApplyT(func(rrdatas []string) error {
			dnsRecordRrdatasCh <- rrdatas

			return nil
		})
		dnsRecordIPs := <-dnsRecordRrdatasCh
		require.NotNil(t, dnsRecordIPs, "DNS record IP addresses should not be nil")
		assert.Len(t, dnsRecordIPs, 1, "DNS record should have exactly one IP address")
		assert.Equal(t, ipAddress, dnsRecordIPs[0], "DNS record IP address should match the forwarding rule IP address")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}
