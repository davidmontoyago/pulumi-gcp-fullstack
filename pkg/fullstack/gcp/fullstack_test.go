package gcp_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
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
		outputs["selfLink"] = "https://www.googleapis.com/apis/run.googleapis.com/v1/projects/" + testProjectName + "/location/us-central1/services" + args.Name
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
		outputs["selfLink"] = "https://www.googleapis.com/compute/v1/projects/" + testProjectName + "/global/backendServices/" + args.Name
		// Expected outputs: name, project, description, protocol, portName, timeoutSec, healthChecks
	case "gcp:compute/uRLMap:URLMap":
		outputs["selfLink"] = "https://www.googleapis.com/compute/v1/projects/" + testProjectName + "/global/urlMaps/" + args.Name
		// Expected outputs: name, project, description, defaultService, pathMatchers, hostRules
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
					MaxInstanceCount:      3,
					DeletionProtection:    false,
					ContainerPort:         3000,
					EnableUnauthenticated: false,
				},
			},
			Network: &gcp.NetworkArgs{
				DomainURL: "myapp.example.com",
				APIGateway: &gcp.APIGatewayArgs{
					Name: "gateway",
					Config: &gcp.APIConfigArgs{
						EnableCORS: true,
						Backend: &gcp.Upstream{
							JWTAuth: &gcp.JWTAuth{
								// JWT authentication will be automatically configured
								// with the frontend service account email and JWKS URI
							},
						},
					},
				},
			},
			Labels: map[string]string{
				"this-label-should-be-set": "test",
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

		// Assert backend service labels are set correctly
		backendLabelsCh := make(chan map[string]string, 1)
		defer close(backendLabelsCh)
		backendService.Labels.ApplyT(func(labels map[string]string) error {
			backendLabelsCh <- labels

			return nil
		})
		backendLabels := <-backendLabelsCh
		assert.Equal(t, "test", backendLabels["this-label-should-be-set"], "Backend service should have the expected label")
		assert.Equal(t, "true", backendLabels["backend"], "Backend service should have the expected label")

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

		// Verify backend service account
		backendAccount := fullstack.GetBackendAccount()
		require.NotNil(t, backendAccount, "Backend service account should not be nil")

		// Verify frontend service account
		frontendAccount := fullstack.GetFrontendAccount()
		require.NotNil(t, frontendAccount, "Frontend service account should not be nil")

		// Verify API Gateway service account
		gatewayAccount := fullstack.GetGatewayServiceAccount()
		require.NotNil(t, gatewayAccount, "API Gateway service account should not be nil")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}

func TestNewFullStack_WithGatewayDefaults(t *testing.T) {
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
					EnableUnauthenticated: false,
				},
			},
			Network: &gcp.NetworkArgs{
				DomainURL:  "myapp.example.com",
				APIGateway: &gcp.APIGatewayArgs{
					// 	Name: "gateway",
					// 	Config: &gcp.APIConfigArgs{
					// 		EnableCORS: true,
					// 	},
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

		// Assert OpenAPI documents has correct paths
		apiPathsCh := make(chan openapi3.Paths, 1)
		defer close(apiPathsCh)

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

		// Parse the decoded JSON and verify the first path entry
		var openAPISpec map[string]interface{}
		err = json.Unmarshal(decodedBytes, &openAPISpec)
		require.NoError(t, err, "Decoded content should be valid JSON")

		// Get the paths object
		pathsObj, found := openAPISpec["paths"].(map[string]interface{})
		require.True(t, found, "Paths should be a map[string]interface{}")
		require.NotEmpty(t, pathsObj, "Paths object should not be empty")

		// Get the first and second path keys
		pathKeys := make([]string, 0, len(pathsObj))
		for pathKey := range pathsObj {
			pathKeys = append(pathKeys, pathKey)
		}

		require.Len(t, pathKeys, 2, "Should have exactly two path keys")

		// Sort keys in ascending order for consistent assertions
		sort.Strings(pathKeys)

		// Assert that the first path key matches "/api/v1/{proxy}"
		assert.Equal(t, "/api/v1/{proxy}", pathKeys[0], "First path key should match expected value")

		// Assert that the second path key matches "/ui/{proxy}"
		assert.Equal(t, "/ui/{proxy}", pathKeys[1], "Second path key should match expected value")

		// Verify that the "address" attribute of "x-google-backend" object matches the backend Instance URL
		// Get the first path (API path) and check its GET operation's x-google-backend configuration
		apiPathObj, apiPathFound := pathsObj["/api/v1/{proxy}"].(map[string]interface{})
		require.True(t, apiPathFound, "API path object should be a map[string]interface{}")

		getOperation, getOpFound := apiPathObj["get"].(map[string]interface{})
		require.True(t, getOpFound, "GET operation should be a map[string]interface{}")

		xGoogleBackend, backendFound := getOperation["x-google-backend"].(map[string]interface{})
		require.True(t, backendFound, "x-google-backend should be a map[string]interface{}")

		backendAddress, addressFound := xGoogleBackend["address"].(string)
		require.True(t, addressFound, "address should be a string")

		// Get the backend service URL to compare against
		backendServiceURLCh := make(chan string, 1)
		defer close(backendServiceURLCh)
		backendService.Uri.ApplyT(func(uri string) error {
			backendServiceURLCh <- uri

			return nil
		})
		backendServiceURL := <-backendServiceURLCh

		// The expected address should be: backendServiceURL + "/api/v1" (without /{proxy} since we use APPEND_PATH_TO_ADDRESS)
		expectedAddress := backendServiceURL + "/api/v1"
		assert.Equal(t, expectedAddress, backendAddress, "Backend address in x-google-backend should match the backend service URL with path")

		// Verify JWT authentication is NOT configured
		// Check that security definitions are not present (JWT should not be enabled by default)
		_, securityDefinitionsFound := openAPISpec["securityDefinitions"]
		assert.False(t, securityDefinitionsFound, "Security definitions should not be present when JWT is not enabled")

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

		// Verify Gateway NEG configuration
		neg := fullstack.GetGatewayNEG()
		require.NotNil(t, neg, "Gateway NEG should not be nil")

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
		assert.Equal(t, "myapp.example.com.", <-dnsRecordNameCh, "DNS record name should match the provided domain")

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

func TestNewFullStack_WithBackendJWTAuthentication(t *testing.T) {
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
				APIGateway: &gcp.APIGatewayArgs{
					Name: "gateway",
					Config: &gcp.APIConfigArgs{
						EnableCORS: true,
						Backend: &gcp.Upstream{
							JWTAuth: &gcp.JWTAuth{
								// JWT authentication will be automatically configured
								// with the frontend service account email and JWKS URI
							},
						},
					},
				},
			},
		}

		fullstack, err := gcp.NewFullStack(ctx, "test-fullstack", args)
		require.NoError(t, err)

		// Verify API Gateway configuration
		apiGateway := fullstack.GetAPIGateway()
		require.NotNil(t, apiGateway, "API Gateway should not be nil")

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

		// Parse the decoded JSON and verify JWT security configuration
		var openAPISpec map[string]interface{}
		err = json.Unmarshal(decodedBytes, &openAPISpec)
		require.NoError(t, err, "Decoded content should be valid JSON")

		// Verify security schemes are configured
		securityDefinitions, found := openAPISpec["securityDefinitions"].(map[string]interface{})
		require.True(t, found, "Security definitions should be present")
		require.NotEmpty(t, securityDefinitions, "Security definitions should not be empty")

		// Verify JWT security scheme is configured
		jwtScheme, jwtFound := securityDefinitions["JWT"].(map[string]interface{})
		require.True(t, jwtFound, "JWT security scheme should be present")
		require.NotEmpty(t, jwtScheme, "JWT security scheme should not be empty")

		// Verify JWT scheme type is "apiKey" (converted from OpenAPI 3.0 http/bearer)
		jwtType, typeFound := jwtScheme["type"].(string)
		require.True(t, typeFound, "JWT scheme type should be present")
		assert.Equal(t, "apiKey", jwtType, "JWT scheme type should be apiKey")

		// Verify JWT scheme name is set (for apiKey type)
		jwtName, nameFound := jwtScheme["name"].(string)
		require.True(t, nameFound, "JWT scheme name should be present")
		assert.Equal(t, "Authorization", jwtName, "JWT scheme name should be Authorization")

		// Verify JWT scheme in is "header" (for apiKey type)
		jwtIn, inFound := jwtScheme["in"].(string)
		require.True(t, inFound, "JWT scheme in should be present")
		assert.Equal(t, "header", jwtIn, "JWT scheme in should be header")

		// Verify x-google-issuer is set to the expected default value
		// The default issuer should be the frontend service account email
		frontendService := fullstack.GetFrontendService()
		require.NotNil(t, frontendService, "Frontend service should not be nil")

		frontendServiceAccountCh := make(chan string, 1)
		defer close(frontendServiceAccountCh)
		frontendService.Template.ServiceAccount().ApplyT(func(serviceAccount *string) error {
			if serviceAccount != nil {
				frontendServiceAccountCh <- *serviceAccount
			} else {
				frontendServiceAccountCh <- ""
			}

			return nil
		})
		expectedIssuer := <-frontendServiceAccountCh
		require.NotEmpty(t, expectedIssuer, "Frontend service account should not be empty")

		// Verify x-google-jwks_uri is set to the expected default value
		expectedJwksURI := fmt.Sprintf("https://www.googleapis.com/service_accounts/v1/metadata/x509/%s", expectedIssuer)

		// Get the x-google-issuer and x-google-jwks_uri from the JWT scheme
		xGoogleIssuer, issuerFound := jwtScheme["x-google-issuer"].(string)
		require.True(t, issuerFound, "x-google-issuer should be present")
		assert.Equal(t, expectedIssuer, xGoogleIssuer, "x-google-issuer should match the frontend service account email")

		xGoogleJwksURI, jwksURIFound := jwtScheme["x-google-jwks_uri"].(string)
		require.True(t, jwksURIFound, "x-google-jwks_uri should be present")
		assert.Equal(t, expectedJwksURI, xGoogleJwksURI, "x-google-jwks_uri should match the expected JWKS URI")

		// Verify that API paths require JWT authentication
		pathsObj, pathsFound := openAPISpec["paths"].(map[string]interface{})
		require.True(t, pathsFound, "Paths should be a map[string]interface{}")
		require.NotEmpty(t, pathsObj, "Paths object should not be empty")

		// Check that the API path requires JWT security
		apiPathObj, apiPathFound := pathsObj["/api/v1/{proxy}"].(map[string]interface{})
		require.True(t, apiPathFound, "API path object should be a map[string]interface{}")

		// Verify GET operation has JWT security requirement
		getOperation, getOpFound := apiPathObj["get"].(map[string]interface{})
		require.True(t, getOpFound, "GET operation should be a map[string]interface{}")

		getSecurity, getSecurityFound := getOperation["security"].([]interface{})
		require.True(t, getSecurityFound, "GET operation should have security requirements")
		require.Len(t, getSecurity, 1, "GET operation should have exactly one security requirement")

		getSecurityReq, getSecurityReqFound := getSecurity[0].(map[string]interface{})
		require.True(t, getSecurityReqFound, "GET security requirement should be a map[string]interface{}")
		assert.Contains(t, getSecurityReq, "JWT", "GET operation should require JWT authentication")

		// Verify POST operation has JWT security requirement
		postOperation, postOpFound := apiPathObj["post"].(map[string]interface{})
		require.True(t, postOpFound, "POST operation should be a map[string]interface{}")

		postSecurity, postSecurityFound := postOperation["security"].([]interface{})
		require.True(t, postSecurityFound, "POST operation should have security requirements")
		require.Len(t, postSecurity, 1, "POST operation should have exactly one security requirement")

		postSecurityReq, postSecurityReqFound := postSecurity[0].(map[string]interface{})
		require.True(t, postSecurityReqFound, "POST security requirement should be a map[string]interface{}")
		assert.Contains(t, postSecurityReq, "JWT", "POST operation should require JWT authentication")

		// Verify that UI paths do NOT require JWT authentication (frontend should be public)
		uiPathObj, uiPathFound := pathsObj["/ui/{proxy}"].(map[string]interface{})
		require.True(t, uiPathFound, "UI path object should be a map[string]interface{}")

		uiGetOperation, uiGetOpFound := uiPathObj["get"].(map[string]interface{})
		require.True(t, uiGetOpFound, "UI GET operation should be a map[string]interface{}")

		// UI operations should not have security requirements
		_, uiGetSecurityFound := uiGetOperation["security"]
		assert.False(t, uiGetSecurityFound, "UI GET operation should not have security requirements")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}

func TestNewFullStack_WithCustomApiPaths(t *testing.T) {
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
				APIGateway: &gcp.APIGatewayArgs{
					Config: &gcp.APIConfigArgs{
						Backend: &gcp.Upstream{
							APIPaths: []*gcp.APIPathArgs{
								{
									Path: "/api/v1",
								},
							},
						},
						Frontend: &gcp.Upstream{
							APIPaths: []*gcp.APIPathArgs{
								{
									Path:         "/ui",
									UpstreamPath: "/api/v1",
								},
							},
						},
					},
				},
			},
		}

		fullstack, err := gcp.NewFullStack(ctx, "test-fullstack", args)
		require.NoError(t, err)

		// Verify API Gateway configuration
		apiGateway := fullstack.GetAPIGateway()
		require.NotNil(t, apiGateway, "API Gateway should not be nil")

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

		// Parse the decoded JSON and verify custom path configuration
		var openAPISpec map[string]interface{}
		err = json.Unmarshal(decodedBytes, &openAPISpec)
		require.NoError(t, err, "Decoded content should be valid JSON")

		// Verify paths are configured correctly
		paths, found := openAPISpec["paths"].(map[string]interface{})
		require.True(t, found, "Paths should be present")
		require.NotEmpty(t, paths, "Paths should not be empty")

		// Verify backend path configuration
		backendPath, backendFound := paths["/api/v1/{proxy}"].(map[string]interface{})
		require.True(t, backendFound, "Backend path /api/v1/{proxy} should be present")
		require.NotEmpty(t, backendPath, "Backend path should not be empty")

		// Verify frontend path configuration with path rewriting
		frontendPath, frontendFound := paths["/ui/{proxy}"].(map[string]interface{})
		require.True(t, frontendFound, "Frontend path /ui/{proxy} should be present")
		require.NotEmpty(t, frontendPath, "Frontend path should not be empty")

		// Verify GET operation exists for both paths
		backendGet, backendGetFound := backendPath["get"].(map[string]interface{})
		require.True(t, backendGetFound, "Backend GET operation should be present")

		frontendGet, frontendGetFound := frontendPath["get"].(map[string]interface{})
		require.True(t, frontendGetFound, "Frontend GET operation should be present")

		// Verify x-google-backend configuration for backend (no path rewriting)
		backendExtensions, backendExtFound := backendGet["x-google-backend"].(map[string]interface{})
		require.True(t, backendExtFound, "Backend x-google-backend should be present")

		backendAddress, backendAddrFound := backendExtensions["address"].(string)
		require.True(t, backendAddrFound, "Backend address should be present")

		// Expected backend address: service URL only (no custom upstream path)
		expectedBackendAddress := "https://test-fullstack-backend-service-hash-uc.a.run.app"
		assert.Equal(t, expectedBackendAddress, backendAddress,
			"Backend address should be service URL only (no path rewriting)")

		// Verify x-google-backend configuration for frontend (with path rewriting)
		frontendExtensions, frontendExtFound := frontendGet["x-google-backend"].(map[string]interface{})
		require.True(t, frontendExtFound, "Frontend x-google-backend should be present")

		frontendAddress, frontendAddrFound := frontendExtensions["address"].(string)
		require.True(t, frontendAddrFound, "Frontend address should be present")

		// Expected frontend address: service URL + custom upstream path
		expectedFrontendAddress := "https://test-fullstack-frontend-service-hash-uc.a.run.app/api/v1"
		assert.Equal(t, expectedFrontendAddress, frontendAddress,
			"Frontend address should be service URL + custom upstream path (path rewriting)")

		return nil
	}, pulumi.WithMocks("test-project", "test-stack", &fullstackMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}

func TestNewFullStack_WithoutGateway(t *testing.T) {
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

		// Verify basic properties
		assert.Equal(t, testProjectName, fullstack.Project)
		assert.Equal(t, testRegion, fullstack.Region)
		assert.Equal(t, backendServiceName, fullstack.BackendName)
		assert.Equal(t, frontendServiceName, fullstack.FrontendName)

		// Verify backend service configuration
		backendService := fullstack.GetBackendService()
		require.NotNil(t, backendService, "Backend service should not be nil")

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

		frontendLocationCh := make(chan string, 1)
		defer close(frontendLocationCh)
		frontendService.Location.ApplyT(func(location string) error {
			frontendLocationCh <- location

			return nil
		})
		assert.Equal(t, testRegion, <-frontendLocationCh, "Frontend service location should match")

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
		require.Nil(t, apiGateway, "API Gateway should not be present")

		// Verify NEG configuration for Cloud Run (when no gateway is present)
		backendNeg := fullstack.GetBackendNEG()
		require.NotNil(t, backendNeg, "NEG should not be nil")

		// Assert NEG name is set correctly for Cloud Run
		negNameCh := make(chan string, 1)
		defer close(negNameCh)
		backendNeg.Name.ApplyT(func(name string) error {
			negNameCh <- name
			return nil
		})
		assert.Equal(t, "test-fullstack-gcp-lb-backend-cloudrun-neg", <-negNameCh, "NEG name should match Cloud Run convention")

		// Assert NEG has CloudRun configuration (not ServerlessDeployment)
		negCloudRunCh := make(chan *compute.RegionNetworkEndpointGroupCloudRun, 1)
		cloudRunServiceCh := make(chan *string, 1)
		defer close(negCloudRunCh)
		backendNeg.CloudRun.ApplyT(func(cloudRun *compute.RegionNetworkEndpointGroupCloudRun) error {
			cloudRunServiceCh <- cloudRun.Service
			return nil
		})
		actualCloudRunService := <-cloudRunServiceCh
		require.NotNil(t, actualCloudRunService, "CloudRun service should not be nil")

		// Get the actual backend service name to compare against
		backendServiceNameCh := make(chan string, 1)
		defer close(backendServiceNameCh)
		backendService.Name.ApplyT(func(name string) error {
			backendServiceNameCh <- name
			return nil
		})

		// The NEG should be created with CloudRun pointing to the backend service
		assert.Equal(t, <-backendServiceNameCh, *actualCloudRunService, "CloudRun NEG service should match the backend service name")

		// Verify frontend NEG configuration for Cloud Run (when no gateway is present)
		frontendNeg := fullstack.GetFrontendNEG()
		require.NotNil(t, frontendNeg, "Frontend NEG should not be nil")

		// Assert frontend NEG name is set correctly for Cloud Run
		frontendNegNameCh := make(chan string, 1)
		defer close(frontendNegNameCh)
		frontendNeg.Name.ApplyT(func(name string) error {
			frontendNegNameCh <- name
			return nil
		})
		assert.Equal(t, "test-fullstack-gcp-lb-frontend-cloudrun-neg", <-frontendNegNameCh, "Frontend NEG name should match Cloud Run convention")

		// Assert frontend NEG has CloudRun configuration (not ServerlessDeployment)
		frontendCloudRunServiceCh := make(chan *string, 1)
		defer close(frontendCloudRunServiceCh)
		frontendNeg.CloudRun.ApplyT(func(cloudRun *compute.RegionNetworkEndpointGroupCloudRun) error {
			frontendCloudRunServiceCh <- cloudRun.Service
			return nil
		})
		actualFrontendCloudRunService := <-frontendCloudRunServiceCh
		require.NotNil(t, actualFrontendCloudRunService, "Frontend CloudRun service should not be nil")

		// Get the actual frontend service name to compare against
		frontendServiceNameCh := make(chan string, 1)
		defer close(frontendServiceNameCh)
		frontendService.Name.ApplyT(func(name string) error {
			frontendServiceNameCh <- name
			return nil
		})

		// The frontend NEG should be created with CloudRun pointing to the frontend service
		assert.Equal(t, <-frontendServiceNameCh, *actualFrontendCloudRunService, "Frontend CloudRun NEG service should match the frontend service name")

		// Verify URL Map configuration
		urlMap := fullstack.GetURLMap()
		require.NotNil(t, urlMap, "URL Map should not be nil")

		// Assert URL Map has the correct default service (should be backend service)
		urlMapDefaultServiceCh := make(chan *string, 1)
		defer close(urlMapDefaultServiceCh)
		urlMap.DefaultService.ApplyT(func(defaultService *string) error {
			urlMapDefaultServiceCh <- defaultService
			return nil
		})
		defaultService := <-urlMapDefaultServiceCh
		assert.Contains(t, *defaultService, "backend-service", "Default service should point to backend service")

		// Assert URL Map has path matchers configured
		urlMapPathMatchersCh := make(chan []compute.URLMapPathMatcher, 1)
		defer close(urlMapPathMatchersCh)
		urlMap.PathMatchers.ApplyT(func(pathMatchers []compute.URLMapPathMatcher) error {
			urlMapPathMatchersCh <- pathMatchers
			return nil
		})
		pathMatchers := <-urlMapPathMatchersCh
		require.Len(t, pathMatchers, 1, "URL Map should have exactly one path matcher")

		// Assert path matcher has correct name
		assert.Equal(t, "traffic-paths", pathMatchers[0].Name, "Path matcher should be named 'traffic-paths'")

		// Assert path matcher has correct path rules
		pathRules := pathMatchers[0].PathRules
		require.Len(t, pathRules, 2, "Path matcher should have exactly 2 path rules")

		// Check API path rule (/api/* -> backend)
		apiPathRule := pathRules[0]
		assert.Contains(t, apiPathRule.Paths, "/api/*", "First path rule should match /api/*")
		assert.Contains(t, *apiPathRule.Service, "backend-service", "API path rule should route to backend service")

		// Check UI path rule (/ui/* -> frontend)
		uiPathRule := pathRules[1]
		assert.Contains(t, uiPathRule.Paths, "/ui/*", "Second path rule should match /ui/*")
		assert.Contains(t, *uiPathRule.Service, "frontend-service", "UI path rule should route to frontend service")

		// Assert URL Map has host rules configured with domain URL (not wildcard)
		urlMapHostRulesCh := make(chan []compute.URLMapHostRule, 1)
		defer close(urlMapHostRulesCh)
		urlMap.HostRules.ApplyT(func(hostRules []compute.URLMapHostRule) error {
			urlMapHostRulesCh <- hostRules
			return nil
		})
		hostRules := <-urlMapHostRulesCh
		require.Len(t, hostRules, 1, "URL Map should have exactly one host rule")

		// Assert host rule uses specific domain (not wildcard "*")
		hostRule := hostRules[0]
		assert.Equal(t, "myapp.example.com", hostRule.Hosts[0], "Host rule should use the specific domain URL")
		assert.NotContains(t, hostRule.Hosts, "*", "Host rule should not use wildcard '*' for security")
		assert.Equal(t, "traffic-paths", hostRule.PathMatcher, "Host rule should reference the correct path matcher")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}
