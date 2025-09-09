package gcp_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/apigateway"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudrun"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudrunv2"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/monitoring"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/redis"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/secretmanager"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/storage"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/vpcaccess"
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
	// Ensure imported types are referenced to avoid linter warnings
	_ = redis.Instance{}
	_ = secretmanager.Secret{}
	_ = vpcaccess.Connector{}
	_ = cloudrun.DomainMapping{}
	_ = storage.Bucket{}
	_ = monitoring.Slo{}
	_ = monitoring.AlertPolicy{}
	_ = monitoring.GenericService{}

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
		// Reflect ipAddress from inputs for assertions
		if ip, ok := args.Inputs["ipAddress"]; ok {
			outputs["ipAddress"] = ip
		} else {
			outputs["ipAddress"] = "34.102.136.185"
		}
		outputs["selfLink"] = "https://www.googleapis.com/compute/v1/projects/" + testProjectName + "/global/forwardingRules/" + args.Name
		// Expected outputs: name, project, description, portRange, loadBalancingScheme, ipAddress, target
	case "gcp:compute/address:Address":
		// Regional address
		outputs["address"] = "35.201.123.45"
		outputs["selfLink"] = "https://www.googleapis.com/compute/v1/projects/" + testProjectName + "/regions/" + testRegion + "/addresses/" + args.Name
		// Expected outputs: name, project, address, selfLink, region
	case "gcp:compute/forwardingRule:ForwardingRule":
		// Reflect ipAddress from inputs
		if ip, ok := args.Inputs["ipAddress"]; ok {
			outputs["ipAddress"] = ip
		} else {
			outputs["ipAddress"] = "35.201.123.45"
		}
		outputs["selfLink"] = "https://www.googleapis.com/compute/v1/projects/" + testProjectName + "/regions/" + testRegion + "/forwardingRules/" + args.Name
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
	case "gcp:projects/service:Service":
		outputs["service"] = args.Inputs["service"]
		// Expected outputs: project, service
	case "gcp:redis/instance:Instance":
		outputs["host"] = "100.0.0.3"
		outputs["port"] = 6379
		outputs["readEndpoint"] = "100.0.0.3"
		outputs["readEndpointPort"] = 6379
		outputs["authString"] = "mock-auth-string-12345"
		outputs["serverCaCerts"] = []map[string]interface{}{
			{
				"cert": "-----BEGIN CERTIFICATE-----\nMOCK_CERT_DATA\n-----END CERTIFICATE-----",
			},
		}
		// Expected outputs: name, project, region, host, port, readEndpoint, readEndpointPort, authString, serverCaCerts
	case "gcp:storage/bucket:Bucket":
		outputs["name"] = args.Name
		outputs["project"] = testProjectName
		outputs["url"] = "gs://" + args.Name
		outputs["selfLink"] = "https://www.googleapis.com/storage/v1/b/" + args.Name
		// Expected outputs: name, project, location, storageClass, url, selfLink
	case "gcp:vpcaccess/connector:Connector":
		outputs["selfLink"] = "https://www.googleapis.com/compute/v1/projects/" + testProjectName + "/regions/" + testRegion + "/connectors/" + args.Name
		// Expected outputs: name, project, region, ipCidrRange
	case "gcp:compute/firewall:Firewall":
		outputs["name"] = args.Name
		outputs["network"] = "default"
		// Expected outputs: name, project, network
	case "gcp:cloudrun/domainMapping:DomainMapping":
		outputs["location"] = testRegion
		outputs["statuses"] = []map[string]interface{}{
			{
				"resourceRecords": []map[string]interface{}{
					{
						"name":   "api.example.com.",
						"type":   "CNAME",
						"rrdata": "1234567890.example.com.",
					},
				},
			},
		}
		// Expected outputs: name, location, status
	case "gcp:monitoring/slo:Slo":
		outputs["name"] = args.Name
		// Expected outputs: name, service, displayName, goal, rollingPeriodDays
	case "gcp:monitoring/alertPolicy:AlertPolicy":
		outputs["name"] = args.Name
		// Expected outputs: name, displayName, conditions, combiner, notificationChannels
	case "gcp:monitoring/genericService:GenericService":
		outputs["name"] = args.Name
		// Expected outputs: name, project, displayName, serviceId, basicService
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
					StartupCPUBoost:    true,
				},
				ProjectIAMRoles: []string{
					"roles/redis.editor",
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
					MaxInstanceCount:    3,
					DeletionProtection:  false,
					ContainerPort:       3000,
					EnablePublicIngress: false,
					StartupCPUBoost:     true,
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

		// Assert backend StartupCPUBoost is set correctly
		backendStartupCPUBoostCh := make(chan bool, 1)
		defer close(backendStartupCPUBoostCh)
		backendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			backendStartupCPUBoostCh <- *containers[0].Resources.StartupCpuBoost

			return nil
		})
		assert.Equal(t, true, <-backendStartupCPUBoostCh, "Backend StartupCPUBoost should be enabled")

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

		// Assert frontend StartupCPUBoost is set correctly
		frontendStartupCPUBoostCh := make(chan bool, 1)
		defer close(frontendStartupCPUBoostCh)
		frontendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			frontendStartupCPUBoostCh <- *containers[0].Resources.StartupCpuBoost

			return nil
		})
		assert.Equal(t, true, <-frontendStartupCPUBoostCh, "Frontend StartupCPUBoost should be enabled")

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

		// Assert frontend environment variables are set correctly
		frontendEnvVarsCh := make(chan []cloudrunv2.ServiceTemplateContainerEnv, 1)
		defer close(frontendEnvVarsCh)
		frontendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			frontendEnvVarsCh <- containers[0].Envs

			return nil
		})
		frontendEnvVars := <-frontendEnvVarsCh

		// Find and verify DOTENV_CONFIG_PATH environment variable
		var dotenvConfigPathEnv *cloudrunv2.ServiceTemplateContainerEnv
		for i := range frontendEnvVars {
			if frontendEnvVars[i].Name == "DOTENV_CONFIG_PATH" {
				dotenvConfigPathEnv = &frontendEnvVars[i]

				break
			}
		}
		require.NotNil(t, dotenvConfigPathEnv, "Frontend should have DOTENV_CONFIG_PATH environment variable")
		require.NotNil(t, dotenvConfigPathEnv.Value, "Frontend DOTENV_CONFIG_PATH value should not be nil")
		assert.Equal(t, "/app/.next/config/.env.production", *dotenvConfigPathEnv.Value, "Frontend DOTENV_CONFIG_PATH should match expected value")

		// Find and verify APP_BASE_URL environment variable
		var appBaseURLEnv *cloudrunv2.ServiceTemplateContainerEnv
		for i := range frontendEnvVars {
			if frontendEnvVars[i].Name == "APP_BASE_URL" {
				appBaseURLEnv = &frontendEnvVars[i]

				break
			}
		}
		require.NotNil(t, appBaseURLEnv, "Frontend should have APP_BASE_URL environment variable")
		require.NotNil(t, appBaseURLEnv.Value, "Frontend APP_BASE_URL value should not be nil")
		assert.Equal(t, "https://myapp.example.com", *appBaseURLEnv.Value, "Frontend APP_BASE_URL should match expected value")

		// Verify custom environment variables are also set
		var nodeEnvVar *cloudrunv2.ServiceTemplateContainerEnv
		for i := range frontendEnvVars {
			if frontendEnvVars[i].Name == "NODE_ENV" {
				nodeEnvVar = &frontendEnvVars[i]

				break
			}
		}
		require.NotNil(t, nodeEnvVar, "Frontend should have NODE_ENV environment variable")
		require.NotNil(t, nodeEnvVar.Value, "Frontend NODE_ENV value should not be nil")
		assert.Equal(t, "production", *nodeEnvVar.Value, "Frontend NODE_ENV should match expected value")

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

		// Assert backend environment variables are set correctly
		backendEnvVarsCh := make(chan []cloudrunv2.ServiceTemplateContainerEnv, 1)
		defer close(backendEnvVarsCh)
		backendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			backendEnvVarsCh <- containers[0].Envs

			return nil
		})
		backendEnvVars := <-backendEnvVarsCh

		// Find and verify DOTENV_CONFIG_PATH environment variable
		var backendDotenvConfigPathEnv *cloudrunv2.ServiceTemplateContainerEnv
		for i := range backendEnvVars {
			if backendEnvVars[i].Name == "DOTENV_CONFIG_PATH" {
				backendDotenvConfigPathEnv = &backendEnvVars[i]

				break
			}
		}
		require.NotNil(t, backendDotenvConfigPathEnv, "Backend should have DOTENV_CONFIG_PATH environment variable")
		require.NotNil(t, backendDotenvConfigPathEnv.Value, "Backend DOTENV_CONFIG_PATH value should not be nil")
		assert.Equal(t, "/app/config/.env", *backendDotenvConfigPathEnv.Value, "Backend DOTENV_CONFIG_PATH should match expected value")

		// Find and verify APP_BASE_URL environment variable
		var backendAppBaseURLEnv *cloudrunv2.ServiceTemplateContainerEnv
		for i := range backendEnvVars {
			if backendEnvVars[i].Name == "APP_BASE_URL" {
				backendAppBaseURLEnv = &backendEnvVars[i]

				break
			}
		}
		require.NotNil(t, backendAppBaseURLEnv, "Backend should have APP_BASE_URL environment variable")
		require.NotNil(t, backendAppBaseURLEnv.Value, "Backend APP_BASE_URL value should not be nil")
		assert.Equal(t, "https://myapp.example.com", *backendAppBaseURLEnv.Value, "Backend APP_BASE_URL should match expected value")

		// Verify custom environment variables are also set
		var backendLogLevelVar *cloudrunv2.ServiceTemplateContainerEnv
		for i := range backendEnvVars {
			if backendEnvVars[i].Name == "LOG_LEVEL" {
				backendLogLevelVar = &backendEnvVars[i]

				break
			}
		}
		require.NotNil(t, backendLogLevelVar, "Backend LOG_LEVEL environment variable")
		require.NotNil(t, backendLogLevelVar.Value, "Backend LOG_LEVEL value should not be nil")
		assert.Equal(t, "info", *backendLogLevelVar.Value, "Backend LOG_LEVEL should match expected value")

		var backendDatabaseURLVar *cloudrunv2.ServiceTemplateContainerEnv
		for i := range backendEnvVars {
			if backendEnvVars[i].Name == "DATABASE_URL" {
				backendDatabaseURLVar = &backendEnvVars[i]

				break
			}
		}
		require.NotNil(t, backendDatabaseURLVar, "Backend should have DATABASE_URL environment variable")
		require.NotNil(t, backendDatabaseURLVar.Value, "Backend DATABASE_URL value should not be nil")
		assert.Equal(t, "postgresql://localhost:5432/testdb", *backendDatabaseURLVar.Value, "Backend DATABASE_URL should match expected value")

		// Verify backend service account
		backendAccount := fullstack.GetBackendAccount()
		require.NotNil(t, backendAccount, "Backend service account should not be nil")

		// Assert project-level IAM roles are bound to backend service account when provided
		backendAccountEmailCh := make(chan string, 1)
		defer close(backendAccountEmailCh)
		backendAccount.Email.ApplyT(func(email string) error {
			backendAccountEmailCh <- email

			return nil
		})
		expectedBackendMember := "serviceAccount:" + <-backendAccountEmailCh

		backendProjectIamMembers := fullstack.GetBackendProjectIamMembers()
		require.NotNil(t, backendProjectIamMembers, "Backend project IAM members should not be nil")
		assert.Len(t, backendProjectIamMembers, 1, "Exactly one backend project IAM member should be created")

		roleCh := make(chan string, 1)
		memberCh := make(chan string, 1)
		projectCh := make(chan string, 1)
		defer close(roleCh)
		defer close(memberCh)
		defer close(projectCh)

		backendProjectIamMembers[0].Role.ApplyT(func(role string) error {
			roleCh <- role

			return nil
		})
		backendProjectIamMembers[0].Member.ApplyT(func(member string) error {
			memberCh <- member

			return nil
		})
		backendProjectIamMembers[0].Project.ApplyT(func(project string) error {
			projectCh <- project

			return nil
		})

		assert.Equal(t, "roles/redis.editor", <-roleCh, "Backend project IAM role should match the requested role")
		assert.Equal(t, expectedBackendMember, <-memberCh, "Backend project IAM member should be the backend service account")
		assert.Equal(t, testProjectName, <-projectCh, "Backend project IAM member project should match")

		// Verify frontend service account
		frontendAccount := fullstack.GetFrontendAccount()
		require.NotNil(t, frontendAccount, "Frontend service account should not be nil")

		// Verify API Gateway service account
		gatewayAccount := fullstack.GetGatewayServiceAccount()
		require.NotNil(t, gatewayAccount, "API Gateway service account should not be nil")

		// Regional vs Global LB assertions (EnableGlobalEntrypoint defaults to false)
		// Global forwarding rule should NOT be present
		assert.Nil(t, fullstack.GetGlobalForwardingRule(), "Global forwarding rule should be nil when using regional entrypoint")

		// Regional forwarding rule SHOULD be present
		regionalFR := fullstack.GetRegionalForwardingRule()
		require.NotNil(t, regionalFR, "Regional forwarding rule should not be nil")

		// Assert regional forwarding rule has a non-empty IP address
		regionalIPCh := make(chan string, 1)
		defer close(regionalIPCh)
		regionalFR.IpAddress.ApplyT(func(ip string) error {
			regionalIPCh <- ip

			return nil
		})
		regionalIP := <-regionalIPCh
		assert.NotEmpty(t, regionalIP, "Regional forwarding rule IP address should not be empty")

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
					EnablePublicIngress: false,
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
				DomainURL:              "myapp.example.com",
				EnableGlobalEntrypoint: true,
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
				APIGateway: &gcp.APIGatewayArgs{
					Disabled: true,
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

		// Verify backend service has private ingress
		backendServiceIngressCh := make(chan string, 1)
		defer close(backendServiceIngressCh)
		fullstack.GetBackendService().Ingress.ApplyT(func(ingress string) error {
			backendServiceIngressCh <- ingress

			return nil
		})
		assert.Equal(t, "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER", <-backendServiceIngressCh, "Backend service should have private ingress")

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

		// Check UI path rule (/* -> frontend)
		uiPathRule := pathRules[1]
		assert.Contains(t, uiPathRule.Paths, "/*", "Second path rule should match /*")
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

func TestNewFullStack_WithSecrets(t *testing.T) {
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
					Secrets: []*gcp.SecretVolumeArgs{
						{
							SecretID:   pulumi.String("backend-db-secret"),
							Name:       "db-credentials",
							Path:       "/app/secrets/db",
							SecretName: "database.json",
							Version:    pulumi.String("1"),
						},
						{
							SecretID: pulumi.String("backend-api-key"),
							Name:     "api-key",
							Path:     "/app/secrets/api",
							Version:  pulumi.String("latest"),
						},
					},
				},
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

		// Verify backend service secret configurations
		backendService := fullstack.GetBackendService()
		require.NotNil(t, backendService, "Backend service should not be nil")

		// Verify backend volumes for secrets
		backendVolumesCh := make(chan []cloudrunv2.ServiceTemplateVolume, 1)
		defer close(backendVolumesCh)
		backendService.Template.Volumes().ApplyT(func(volumes []cloudrunv2.ServiceTemplateVolume) error {
			backendVolumesCh <- volumes

			return nil
		})
		backendVolumes := <-backendVolumesCh

		// Should have 3 volumes: 1 for envconfig + 2 for secrets
		require.Len(t, backendVolumes, 3, "Backend should have 3 volumes (1 envconfig + 2 secrets)")

		// Find the secret volumes (not the envconfig volume)
		var dbSecretVolume, apiKeySecretVolume *cloudrunv2.ServiceTemplateVolume
		for i := range backendVolumes {
			switch backendVolumes[i].Name {
			case "db-credentials":
				dbSecretVolume = &backendVolumes[i]
			case "api-key":
				apiKeySecretVolume = &backendVolumes[i]
			}
		}

		// Verify db-credentials secret volume
		require.NotNil(t, dbSecretVolume, "DB credentials secret volume should be present")
		assert.Equal(t, "db-credentials", dbSecretVolume.Name, "DB secret volume name should match")
		assert.NotNil(t, dbSecretVolume.Secret, "DB secret volume should have secret configuration")
		assert.NotNil(t, dbSecretVolume.Secret.Items, "DB secret volume should have items")
		assert.Len(t, dbSecretVolume.Secret.Items, 1, "DB secret volume should have exactly one item")

		dbSecretItem := dbSecretVolume.Secret.Items[0]
		assert.Equal(t, "database.json", dbSecretItem.Path, "DB secret item path should match the secret name")
		assert.Equal(t, "1", *dbSecretItem.Version, "DB secret item version should be '1'")
		assert.Equal(t, 0400, *dbSecretItem.Mode, "DB secret item mode should be 0400 for read-only access")

		// Verify api-key secret volume
		require.NotNil(t, apiKeySecretVolume, "API key secret volume should be present")
		assert.Equal(t, "api-key", apiKeySecretVolume.Name, "API key volume name should match")
		assert.NotNil(t, apiKeySecretVolume.Secret, "API key volume should have secret configuration")
		assert.NotNil(t, apiKeySecretVolume.Secret.Items, "API key volume should have items")
		assert.Len(t, apiKeySecretVolume.Secret.Items, 1, "API key volume should have exactly one item")

		apiKeySecretItem := apiKeySecretVolume.Secret.Items[0]
		assert.Equal(t, ".env", apiKeySecretItem.Path, "API key secret item path should match the secret name")
		assert.Equal(t, "latest", *apiKeySecretItem.Version, "API key secret item version should be 'latest'")
		assert.Equal(t, 0400, *apiKeySecretItem.Mode, "API key secret item mode should be 0400 for read-only access")

		// Verify backend volume mounts for secrets
		backendVolumeMountsCh := make(chan []cloudrunv2.ServiceTemplateContainerVolumeMount, 1)
		defer close(backendVolumeMountsCh)
		backendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			if len(containers) > 0 {
				backendVolumeMountsCh <- containers[0].VolumeMounts
			}

			return nil
		})
		backendVolumeMounts := <-backendVolumeMountsCh

		// Should have 3 volume mounts: 1 for envconfig + 2 for secrets
		require.Len(t, backendVolumeMounts, 3, "Backend should have 3 volume mounts (1 envconfig + 2 secrets)")

		// Find the secret volume mounts (not the envconfig mount)
		var dbSecretMount, apiKeySecretMount *cloudrunv2.ServiceTemplateContainerVolumeMount
		for i := range backendVolumeMounts {
			switch backendVolumeMounts[i].Name {
			case "db-credentials":
				dbSecretMount = &backendVolumeMounts[i]
			case "api-key":
				apiKeySecretMount = &backendVolumeMounts[i]
			}
		}

		// Verify db-credentials volume mount
		require.NotNil(t, dbSecretMount, "DB credentials volume mount should be present")
		assert.Equal(t, "db-credentials", dbSecretMount.Name, "DB secret mount name should match")
		assert.Equal(t, "/app/secrets/db", dbSecretMount.MountPath, "DB secret mount path should match specified path")

		// Verify api-key volume mount
		require.NotNil(t, apiKeySecretMount, "API key volume mount should be present")
		assert.Equal(t, "api-key", apiKeySecretMount.Name, "API key mount name should match")
		assert.Equal(t, "/app/secrets/api", apiKeySecretMount.MountPath, "API key mount path should match specified path")

		// Verify basic backend service configuration
		assert.Equal(t, testProjectName, fullstack.Project, "Project should match")
		assert.Equal(t, testRegion, fullstack.Region, "Region should match")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}

func TestNewFullStack_WithCache(t *testing.T) {
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
				CacheInstance: &gcp.CacheInstanceArgs{
					RedisVersion:          "REDIS_7_0",
					Tier:                  "BASIC",
					MemorySizeGb:          2,
					ConnectorIPCidrRange:  "10.9.0.0/28",
					ConnectorMinInstances: 3,
					ConnectorMaxInstances: 9,
				},
				ProjectIAMRoles: []string{"roles/some-other-special-role"},
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

		// Verify Redis instance configuration
		redisInstance := fullstack.GetRedisInstance()
		require.NotNil(t, redisInstance, "Redis instance should not be nil")

		// Assert Redis instance basic properties
		redisProjectCh := make(chan string, 1)
		defer close(redisProjectCh)
		redisInstance.Project.ApplyT(func(project string) error {
			redisProjectCh <- project

			return nil
		})
		assert.Equal(t, testProjectName, <-redisProjectCh, "Redis instance project should match")

		redisRegionCh := make(chan string, 1)
		defer close(redisRegionCh)
		redisInstance.Region.ApplyT(func(region string) error {
			redisRegionCh <- region

			return nil
		})
		assert.Equal(t, testRegion, <-redisRegionCh, "Redis instance region should match")

		// Assert Redis instance connection details
		redisHostCh := make(chan string, 1)
		defer close(redisHostCh)
		redisInstance.Host.ApplyT(func(host string) error {
			redisHostCh <- host

			return nil
		})
		assert.Equal(t, "100.0.0.3", <-redisHostCh, "Redis host should match mock value")

		redisPortCh := make(chan int, 1)
		defer close(redisPortCh)
		redisInstance.Port.ApplyT(func(port int) error {
			redisPortCh <- port

			return nil
		})
		assert.Equal(t, 6379, <-redisPortCh, "Redis port should be 6379")

		// Verify VPC connector configuration
		vpcConnector := fullstack.GetVPCConnector()
		require.NotNil(t, vpcConnector, "VPC connector should not be nil")

		vpcProjectCh := make(chan string, 1)
		defer close(vpcProjectCh)
		vpcConnector.Project.ApplyT(func(project string) error {
			vpcProjectCh <- project

			return nil
		})
		assert.Equal(t, testProjectName, <-vpcProjectCh, "VPC connector project should match")

		vpcRegionCh := make(chan string, 1)
		defer close(vpcRegionCh)
		vpcConnector.Region.ApplyT(func(region string) error {
			vpcRegionCh <- region

			return nil
		})
		assert.Equal(t, testRegion, <-vpcRegionCh, "VPC connector region should match")

		vpcCidrCh := make(chan *string, 1)
		defer close(vpcCidrCh)
		vpcConnector.IpCidrRange.ApplyT(func(cidr *string) error {
			vpcCidrCh <- cidr

			return nil
		})
		cidr := <-vpcCidrCh
		require.NotNil(t, cidr, "VPC connector CIDR should not be nil")
		assert.Equal(t, "10.9.0.0/28", *cidr, "VPC connector CIDR should match expected value")

		// Verify VPC connector min and max instances configuration
		vpcMinInstancesCh := make(chan int, 1)
		defer close(vpcMinInstancesCh)
		vpcConnector.MinInstances.ApplyT(func(minInstances int) error {
			vpcMinInstancesCh <- minInstances

			return nil
		})
		minInstances := <-vpcMinInstancesCh
		require.NotNil(t, minInstances, "VPC connector min instances should not be nil")
		assert.Equal(t, 3, minInstances, "VPC connector min instances should match expected value")

		vpcMaxInstancesCh := make(chan int, 1)
		defer close(vpcMaxInstancesCh)
		vpcConnector.MaxInstances.ApplyT(func(maxInstances int) error {
			vpcMaxInstancesCh <- maxInstances

			return nil
		})
		maxInstances := <-vpcMaxInstancesCh
		require.NotNil(t, maxInstances, "VPC connector max instances should not be nil")
		assert.Equal(t, 9, maxInstances, "VPC connector max instances should match expected value")

		// Verify cache firewall rule configuration
		cacheFirewall := fullstack.GetCacheFirewall()
		require.NotNil(t, cacheFirewall, "Cache firewall should not be nil")

		firewallProjectCh := make(chan string, 1)
		defer close(firewallProjectCh)
		cacheFirewall.Project.ApplyT(func(project string) error {
			firewallProjectCh <- project

			return nil
		})
		assert.Equal(t, testProjectName, <-firewallProjectCh, "Firewall project should match")

		// Verify cache secret version configuration
		cacheSecret := fullstack.GetCacheSecretVersion()
		require.NotNil(t, cacheSecret, "Cache secret version should not be nil")

		// Assert secret data contains Redis connection details (async pattern)
		secretDataCh := make(chan *string, 1)
		defer close(secretDataCh)
		cacheSecret.SecretData.ApplyT(func(data *string) error {
			secretDataCh <- data

			return nil
		})
		secretData := <-secretDataCh
		require.NotNil(t, secretData, "Secret data should not be nil")
		assert.Contains(t, *secretData, "REDIS_HOST=100.0.0.3", "Secret should contain Redis host")
		assert.Contains(t, *secretData, "REDIS_PORT=6379", "Secret should contain Redis port")
		assert.Contains(t, *secretData, "REDIS_AUTH_STRING=mock-auth-string-12345", "Secret should contain auth string")
		assert.Contains(t, *secretData, "REDIS_TLS_CA_CERTS=", "Secret should contain TLS CA certs field")

		// Verify backend service has VPC access connector configured
		backendService := fullstack.GetBackendService()
		require.NotNil(t, backendService, "Backend service should not be nil")

		// Assert VPC access is configured for private connectivity to Redis
		backendVpcAccessCh := make(chan *cloudrunv2.ServiceTemplateVpcAccess, 1)
		defer close(backendVpcAccessCh)
		backendService.Template.VpcAccess().ApplyT(func(vpcAccess *cloudrunv2.ServiceTemplateVpcAccess) error {
			backendVpcAccessCh <- vpcAccess

			return nil
		})
		vpcAccess := <-backendVpcAccessCh
		require.NotNil(t, vpcAccess, "Backend service should have VPC access configured")

		// Assert VPC connector is set correctly
		require.NotNil(t, vpcAccess.Connector, "VPC connector should be configured")
		assert.Contains(t, *vpcAccess.Connector, "test-cache-vpc-connector", "VPC connector should be configured for cache access")

		// Assert egress is set to private ranges only
		require.NotNil(t, vpcAccess.Egress, "VPC egress should be configured")
		assert.Equal(t, "PRIVATE_RANGES_ONLY", *vpcAccess.Egress, "VPC egress should be set to private ranges only")

		// Verify backend service has cache credentials volume mount
		backendVolumeMountsCh := make(chan []cloudrunv2.ServiceTemplateContainerVolumeMount, 1)
		defer close(backendVolumeMountsCh)
		backendService.Template.Containers().ApplyT(func(containers []cloudrunv2.ServiceTemplateContainer) error {
			if len(containers) > 0 {
				backendVolumeMountsCh <- containers[0].VolumeMounts
			}

			return nil
		})
		backendVolumeMounts := <-backendVolumeMountsCh

		// Find the cache credentials volume mount
		var cacheCredentialsMount *cloudrunv2.ServiceTemplateContainerVolumeMount
		for _, mount := range backendVolumeMounts {
			if mount.Name == "cache-credentials" {
				cacheCredentialsMount = &mount

				break
			}
		}

		// Assert cache credentials volume mount exists and is configured correctly
		require.NotNil(t, cacheCredentialsMount, "Backend should have cache credentials volume mount")
		assert.Equal(t, "cache-credentials", cacheCredentialsMount.Name, "Cache credentials mount name should match")
		assert.Equal(t, "/app/cache-config", cacheCredentialsMount.MountPath, "Cache credentials should be mounted at /app/cache-config")

		// Verify backend service has cache credentials volume
		backendVolumesCh := make(chan []cloudrunv2.ServiceTemplateVolume, 1)
		defer close(backendVolumesCh)
		backendService.Template.Volumes().ApplyT(func(volumes []cloudrunv2.ServiceTemplateVolume) error {
			backendVolumesCh <- volumes

			return nil
		})
		backendVolumes := <-backendVolumesCh

		// Find the cache credentials volume
		var cacheCredentialsVolume *cloudrunv2.ServiceTemplateVolume
		for i := range backendVolumes {
			if backendVolumes[i].Name == "cache-credentials" {
				cacheCredentialsVolume = &backendVolumes[i]

				break
			}
		}

		// Assert cache credentials volume exists and is configured correctly
		require.NotNil(t, cacheCredentialsVolume, "Backend should have cache credentials volume")
		assert.Equal(t, "cache-credentials", cacheCredentialsVolume.Name, "Cache credentials volume name should match")
		assert.NotNil(t, cacheCredentialsVolume.Secret, "Cache credentials volume should have secret configuration")
		assert.NotNil(t, cacheCredentialsVolume.Secret.Items, "Cache credentials volume should have secret items")
		assert.Len(t, cacheCredentialsVolume.Secret.Items, 1, "Cache credentials volume should have exactly one secret item")

		cacheSecretItem := cacheCredentialsVolume.Secret.Items[0]
		assert.Equal(t, ".env", cacheSecretItem.Path, "Cache credentials secret item should be named .env")
		assert.Equal(t, 0400, *cacheSecretItem.Mode, "Cache credentials secret item should have read-only permissions")

		// Verify backend service has redis.editor IAM role
		backendProjectIamMembers := fullstack.GetBackendProjectIamMembers()
		require.NotNil(t, backendProjectIamMembers, "Backend project IAM members should not be nil")
		assert.Len(t, backendProjectIamMembers, 2, "Exactly one backend project IAM member should be created")

		roleCh := make(chan string, 2)
		defer close(roleCh)
		backendProjectIamMembers[0].Role.ApplyT(func(role string) error {
			roleCh <- role

			return nil
		})
		assert.Equal(t, "roles/some-other-special-role", <-roleCh, "Backend project IAM roles passed should be honored")

		backendProjectIamMembers[1].Role.ApplyT(func(role string) error {
			roleCh <- role

			return nil
		})
		assert.Equal(t, "roles/redis.editor", <-roleCh, "Backend project IAM role should allow redis editor")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}

func TestNewFullStack_WithBucket(t *testing.T) {
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
				BucketInstance: &gcp.BucketInstanceArgs{
					StorageClass:  "STANDARD",
					Location:      "US",
					RetentionDays: 30,
					ForceDestroy:  true,
				},
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

		// Verify storage bucket configuration
		storageBucket := fullstack.GetStorageBucket()
		require.NotNil(t, storageBucket, "Storage bucket should not be nil")

		// Assert bucket basic properties
		bucketProjectCh := make(chan string, 1)
		defer close(bucketProjectCh)
		storageBucket.Project.ApplyT(func(project string) error {
			bucketProjectCh <- project

			return nil
		})
		assert.Equal(t, testProjectName, <-bucketProjectCh, "Storage bucket project should match")

		bucketLocationCh := make(chan string, 1)
		defer close(bucketLocationCh)
		storageBucket.Location.ApplyT(func(location string) error {
			bucketLocationCh <- location

			return nil
		})
		assert.Equal(t, "US", <-bucketLocationCh, "Storage bucket location should match")

		bucketStorageClassCh := make(chan string, 1)
		defer close(bucketStorageClassCh)
		storageBucket.StorageClass.ApplyT(func(storageClass *string) error {
			bucketStorageClassCh <- *storageClass

			return nil
		})
		assert.Equal(t, "STANDARD", <-bucketStorageClassCh, "Storage bucket storage class should match")

		backendProjectIamMembers := fullstack.GetBackendProjectIamMembers()
		require.NotNil(t, backendProjectIamMembers, "Backend project IAM members should not be nil")
		assert.Len(t, backendProjectIamMembers, 1, "Exactly one backend project IAM member should be created")

		roleCh := make(chan string, 1)
		memberCh := make(chan string, 1)
		projectCh := make(chan string, 1)
		defer close(roleCh)
		defer close(memberCh)
		defer close(projectCh)

		backendProjectIamMembers[0].Role.ApplyT(func(role string) error {
			roleCh <- role

			return nil
		})
		backendProjectIamMembers[0].Member.ApplyT(func(member string) error {
			memberCh <- member

			return nil
		})
		backendProjectIamMembers[0].Project.ApplyT(func(project string) error {
			projectCh <- project

			return nil
		})

		assert.Equal(t, "roles/storage.objectAdmin", <-roleCh, "Backend project IAM role should match the requested role")
		assert.Equal(t, "serviceAccount:test-fullsta-backend-account@test-project.iam.gserviceaccount.com", <-memberCh, "Backend project IAM member should be the backend service account")
		assert.Equal(t, testProjectName, <-projectCh, "Backend project IAM member project should match")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}

func TestNewFullStack_WithExternalWAFAndNoGoogleLoadBalancer(t *testing.T) {
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
					EnablePublicIngress: true,
				},
			},
			Frontend: &gcp.FrontendArgs{
				InstanceArgs: &gcp.InstanceArgs{},
			},
			Network: &gcp.NetworkArgs{
				DomainURL:         "myapp.example.com",
				EnableExternalWAF: true,
			},
		}

		fullstack, err := gcp.NewFullStack(ctx, "test-fullstack", args)
		require.NoError(t, err)

		// Verify basic properties
		assert.Equal(t, testProjectName, fullstack.Project)
		assert.Equal(t, testRegion, fullstack.Region)
		assert.Equal(t, backendServiceName, fullstack.BackendName)
		assert.Equal(t, frontendServiceName, fullstack.FrontendName)

		// Verify no load balancer infrastructure was created
		assert.Nil(t, fullstack.GetGlobalForwardingRule(), "Global forwarding rule should be nil when using External WAF")
		assert.Nil(t, fullstack.GetRegionalForwardingRule(), "Regional forwarding rule should be nil when using External WAF")
		assert.Nil(t, fullstack.GetCertificate(), "Certificate should be nil when using External WAF")
		assert.Nil(t, fullstack.GetURLMap(), "URL Map should be nil when using External WAF")
		assert.Nil(t, fullstack.GetDNSRecord(), "DNS record should be nil when using External WAF")

		// Verify NEG infrastructure was not created
		assert.Nil(t, fullstack.GetBackendNEG(), "Backend NEG should be nil when using External WAF")
		assert.Nil(t, fullstack.GetFrontendNEG(), "Frontend NEG should be nil when using External WAF")
		assert.Nil(t, fullstack.GetGatewayNEG(), "Gateway NEG should be nil when using External WAF")

		// Verify backend domain mapping was created
		backendDomainMapping := fullstack.GetBackendDomainMapping()
		require.NotNil(t, backendDomainMapping, "Backend domain mapping should not be nil when using External WAF")

		// Assert domain mapping name matches the expected domain
		domainMappingNameCh := make(chan string, 1)
		defer close(domainMappingNameCh)
		backendDomainMapping.Name.ApplyT(func(name string) error {
			domainMappingNameCh <- name

			return nil
		})
		assert.Equal(t, "api-myapp.example.com", <-domainMappingNameCh, "Domain mapping name should match the provided domain")

		// Assert domain mapping location matches the region
		domainMappingLocationCh := make(chan string, 1)
		defer close(domainMappingLocationCh)
		backendDomainMapping.Location.ApplyT(func(location string) error {
			domainMappingLocationCh <- location

			return nil
		})
		assert.Equal(t, testRegion, <-domainMappingLocationCh, "Domain mapping location should match the region")

		// Assert domain mapping spec route name points to backend service
		domainMappingRouteNameCh := make(chan string, 1)
		defer close(domainMappingRouteNameCh)
		backendDomainMapping.Spec.RouteName().ApplyT(func(routeName string) error {
			domainMappingRouteNameCh <- routeName

			return nil
		})
		backendServiceNameCh := make(chan string, 1)
		defer close(backendServiceNameCh)
		fullstack.GetBackendService().Name.ApplyT(func(name string) error {
			backendServiceNameCh <- name

			return nil
		})
		assert.Equal(t, <-backendServiceNameCh, <-domainMappingRouteNameCh, "Domain mapping should route to backend service")

		// Verify backend service has public ingress
		backendServiceIngressCh := make(chan string, 1)
		defer close(backendServiceIngressCh)
		fullstack.GetBackendService().Ingress.ApplyT(func(ingress string) error {
			backendServiceIngressCh <- ingress

			return nil
		})
		assert.Equal(t, "INGRESS_TRAFFIC_ALL", <-backendServiceIngressCh, "Backend service should have public ingress")

		// Verify frontend domain mapping was created
		frontendDomainMapping := fullstack.GetFrontendDomainMapping()
		require.NotNil(t, frontendDomainMapping, "Frontend domain mapping should not be nil when using External WAF")

		// Assert frontend domain mapping name matches the expected domain
		frontendDomainMappingNameCh := make(chan string, 1)
		defer close(frontendDomainMappingNameCh)
		frontendDomainMapping.Name.ApplyT(func(name string) error {
			frontendDomainMappingNameCh <- name

			return nil
		})
		assert.Equal(t, "myapp.example.com", <-frontendDomainMappingNameCh, "Frontend domain mapping name should match the provided domain")

		// Assert frontend domain mapping location matches the region
		frontendDomainMappingLocationCh := make(chan string, 1)
		defer close(frontendDomainMappingLocationCh)
		frontendDomainMapping.Location.ApplyT(func(location string) error {
			frontendDomainMappingLocationCh <- location

			return nil
		})
		assert.Equal(t, testRegion, <-frontendDomainMappingLocationCh, "Frontend domain mapping location should match the region")

		// Assert frontend domain mapping spec route name points to frontend service
		frontendDomainMappingRouteNameCh := make(chan string, 1)
		defer close(frontendDomainMappingRouteNameCh)
		frontendDomainMapping.Spec.RouteName().ApplyT(func(routeName string) error {
			frontendDomainMappingRouteNameCh <- routeName

			return nil
		})
		frontendServiceNameCh := make(chan string, 1)
		defer close(frontendServiceNameCh)
		fullstack.GetFrontendService().Name.ApplyT(func(name string) error {
			frontendServiceNameCh <- name

			return nil
		})
		assert.Equal(t, <-frontendServiceNameCh, <-frontendDomainMappingRouteNameCh, "Frontend domain mapping should route to frontend service")

		// Verify that both domain mappings have the correct metadata namespace
		backendMetadataNamespaceCh := make(chan string, 1)
		defer close(backendMetadataNamespaceCh)
		backendDomainMapping.Metadata.Namespace().ApplyT(func(namespace string) error {
			backendMetadataNamespaceCh <- namespace

			return nil
		})
		assert.Equal(t, testProjectName, <-backendMetadataNamespaceCh, "Backend domain mapping should have correct project namespace")

		frontendMetadataNamespaceCh := make(chan string, 1)
		defer close(frontendMetadataNamespaceCh)
		frontendDomainMapping.Metadata.Namespace().ApplyT(func(namespace string) error {
			frontendMetadataNamespaceCh <- namespace

			return nil
		})
		assert.Equal(t, testProjectName, <-frontendMetadataNamespaceCh, "Frontend domain mapping should have correct project namespace")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}

func TestNewFullStack_WithColdStartSLOs(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		testAlertChannelID := "test-alert-channel-123"

		args := &gcp.FullStackArgs{
			Project:       testProjectName,
			Region:        testRegion,
			BackendName:   backendServiceName,
			BackendImage:  pulumi.String("gcr.io/test-project/backend:latest"),
			FrontendName:  frontendServiceName,
			FrontendImage: pulumi.String("gcr.io/test-project/frontend:latest"),
			Backend: &gcp.BackendArgs{
				InstanceArgs: &gcp.InstanceArgs{
					ColdStartSLO: &gcp.ColdStartSLOArgs{
						Goal:              pulumi.Float64(0.95), // 95% instead of default 99%
						MaxBootTimeMs:     pulumi.Float64(2000), // 2 seconds instead of default 1 second
						RollingPeriodDays: pulumi.Int(14),       // 14 days instead of default 7 days
						AlertChannelID:    testAlertChannelID,
					},
				},
			},
			Frontend: &gcp.FrontendArgs{
				InstanceArgs: &gcp.InstanceArgs{
					ColdStartSLO: &gcp.ColdStartSLOArgs{
						Goal:              pulumi.Float64(0.98), // 98% instead of default 99%
						MaxBootTimeMs:     pulumi.Float64(1500), // 1.5 seconds instead of default 1 second
						RollingPeriodDays: pulumi.Int(30),       // 30 days instead of default 7 days
						// No AlertChannelID - alerting should be disabled
					},
				},
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

		// Verify backend Cold Start SLO configuration
		backendSLO := fullstack.GetBackendColdStartSLO()
		require.NotNil(t, backendSLO, "Backend Cold Start SLO should not be nil")

		// Assert backend SLO is configured
		require.NotNil(t, backendSLO.Slo, "Backend SLO should not be nil")

		backendSLOServiceCh := make(chan string, 1)
		defer close(backendSLOServiceCh)
		backendSLO.Slo.Service.ApplyT(func(service string) error {
			backendSLOServiceCh <- service

			return nil
		})
		backendSLOService := <-backendSLOServiceCh
		assert.Contains(t, backendSLOService, "test-fullstack-backend-service-monitoring-service", "Backend SLO service should match expected generic instance name")

		backendSLOGoalCh := make(chan float64, 1)
		defer close(backendSLOGoalCh)
		backendSLO.Slo.Goal.ApplyT(func(goal float64) error {
			backendSLOGoalCh <- goal

			return nil
		})
		backendSLOGoal := <-backendSLOGoalCh
		assert.Equal(t, 0.95, backendSLOGoal, "Backend SLO goal should be 95%")

		backendSLORollingPeriodCh := make(chan int, 1)
		defer close(backendSLORollingPeriodCh)
		backendSLO.Slo.RollingPeriodDays.ApplyT(func(days *int) error {
			if days != nil {
				backendSLORollingPeriodCh <- *days
			} else {
				backendSLORollingPeriodCh <- 0
			}

			return nil
		})
		backendSLORollingPeriod := <-backendSLORollingPeriodCh
		assert.Equal(t, 14, backendSLORollingPeriod, "Backend SLO rolling period should be 14 days")

		// Assert backend alert policy is configured
		require.NotNil(t, backendSLO.AlertPolicy, "Backend alert policy should not be nil")

		backendAlertNotificationChannelsCh := make(chan []string, 1)
		defer close(backendAlertNotificationChannelsCh)
		backendSLO.AlertPolicy.NotificationChannels.ApplyT(func(channels []string) error {
			backendAlertNotificationChannelsCh <- channels

			return nil
		})
		backendAlertChannels := <-backendAlertNotificationChannelsCh
		require.Len(t, backendAlertChannels, 1, "Backend alert policy should have exactly one notification channel")
		expectedAlertChannel := fmt.Sprintf("projects/test-project/notificationChannels/%s", testAlertChannelID)
		assert.Contains(t, backendAlertChannels[0], expectedAlertChannel, "Backend alert notification channel should match expected pattern")

		// Verify frontend Cold Start SLO configuration
		frontendSLO := fullstack.GetFrontendColdStartSLO()
		require.NotNil(t, frontendSLO, "Frontend Cold Start SLO should not be nil")

		// Assert frontend SLO is configured
		require.NotNil(t, frontendSLO.Slo, "Frontend SLO should not be nil")

		frontendSLOServiceCh := make(chan string, 1)
		defer close(frontendSLOServiceCh)
		frontendSLO.Slo.Service.ApplyT(func(service string) error {
			frontendSLOServiceCh <- service

			return nil
		})
		frontendSLOService := <-frontendSLOServiceCh
		assert.Contains(t, frontendSLOService, "test-fullstack-frontend-service-monitoring-service", "Frontend SLO service should match expected generic instance name")

		frontendSLOGoalCh := make(chan float64, 1)
		defer close(frontendSLOGoalCh)
		frontendSLO.Slo.Goal.ApplyT(func(goal float64) error {
			frontendSLOGoalCh <- goal

			return nil
		})
		frontendSLOGoal := <-frontendSLOGoalCh
		assert.Equal(t, 0.98, frontendSLOGoal, "Frontend SLO goal should be 98%")

		frontendSLORollingPeriodCh := make(chan int, 1)
		defer close(frontendSLORollingPeriodCh)
		frontendSLO.Slo.RollingPeriodDays.ApplyT(func(days *int) error {
			if days != nil {
				frontendSLORollingPeriodCh <- *days
			} else {
				frontendSLORollingPeriodCh <- 0
			}

			return nil
		})
		frontendSLORollingPeriod := <-frontendSLORollingPeriodCh
		assert.Equal(t, 30, frontendSLORollingPeriod, "Frontend SLO rolling period should be 30 days")

		// Assert frontend alert policy is not configured (no alert channel ID provided)
		assert.Nil(t, frontendSLO.AlertPolicy, "Frontend alert policy should be nil when no alert channel ID is provided")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}

func TestNewFullStack_WithColdStartSLOAlertPolicy(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		testAlertChannelID := "test-alert-channel-custom-456"

		args := &gcp.FullStackArgs{
			Project:       testProjectName,
			Region:        testRegion,
			BackendName:   backendServiceName,
			BackendImage:  pulumi.String("gcr.io/test-project/backend:latest"),
			FrontendName:  frontendServiceName,
			FrontendImage: pulumi.String("gcr.io/test-project/frontend:latest"),
			Backend: &gcp.BackendArgs{
				InstanceArgs: &gcp.InstanceArgs{
					ColdStartSLO: &gcp.ColdStartSLOArgs{
						Goal:                   pulumi.Float64(0.97), // 97% instead of default 99%
						MaxBootTimeMs:          pulumi.Float64(3000), // 3 seconds instead of default 1 second
						RollingPeriodDays:      pulumi.Int(21),       // 21 days instead of default 7 days
						AlertChannelID:         testAlertChannelID,
						AlertBurnRateThreshold: pulumi.Float64(0.2),    // 20% instead of default 10%
						AlertThresholdDuration: pulumi.String("3600s"), // 1 hour instead of default 1 day
					},
				},
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

		// Verify backend Cold Start SLO configuration
		backendSLO := fullstack.GetBackendColdStartSLO()
		require.NotNil(t, backendSLO, "Backend Cold Start SLO should not be nil")

		// Assert backend SLO is configured with custom values
		require.NotNil(t, backendSLO.Slo, "Backend SLO should not be nil")

		backendSLOGoalCh := make(chan float64, 1)
		defer close(backendSLOGoalCh)
		backendSLO.Slo.Goal.ApplyT(func(goal float64) error {
			backendSLOGoalCh <- goal

			return nil
		})
		backendSLOGoal := <-backendSLOGoalCh
		assert.Equal(t, 0.97, backendSLOGoal, "Backend SLO goal should be 97%")

		backendSLORollingPeriodCh := make(chan int, 1)
		defer close(backendSLORollingPeriodCh)
		backendSLO.Slo.RollingPeriodDays.ApplyT(func(days *int) error {
			if days != nil {
				backendSLORollingPeriodCh <- *days
			} else {
				backendSLORollingPeriodCh <- 0
			}

			return nil
		})
		backendSLORollingPeriod := <-backendSLORollingPeriodCh
		assert.Equal(t, 21, backendSLORollingPeriod, "Backend SLO rolling period should be 21 days")

		// Assert backend alert policy is configured with custom values
		require.NotNil(t, backendSLO.AlertPolicy, "Backend alert policy should not be nil")

		// Verify alert notification channels
		backendAlertNotificationChannelsCh := make(chan []string, 1)
		defer close(backendAlertNotificationChannelsCh)
		backendSLO.AlertPolicy.NotificationChannels.ApplyT(func(channels []string) error {
			backendAlertNotificationChannelsCh <- channels

			return nil
		})
		backendAlertChannels := <-backendAlertNotificationChannelsCh
		require.Len(t, backendAlertChannels, 1, "Backend alert policy should have exactly one notification channel")
		expectedAlertChannel := fmt.Sprintf("projects/test-project/notificationChannels/%s", testAlertChannelID)
		assert.Contains(t, backendAlertChannels[0], expectedAlertChannel, "Backend alert notification channel should match expected pattern")

		// Verify alert policy conditions with custom burn rate threshold and duration
		backendAlertConditionsCh := make(chan []monitoring.AlertPolicyCondition, 1)
		defer close(backendAlertConditionsCh)
		backendSLO.AlertPolicy.Conditions.ApplyT(func(conditions []monitoring.AlertPolicyCondition) error {
			backendAlertConditionsCh <- conditions

			return nil
		})
		backendAlertConditions := <-backendAlertConditionsCh
		require.Len(t, backendAlertConditions, 1, "Backend alert policy should have exactly one condition")

		// Verify condition configuration
		condition := backendAlertConditions[0]

		// Verify condition threshold value (burn rate threshold)
		require.NotNil(t, condition.ConditionThreshold, "Alert condition should have threshold configuration")
		require.NotNil(t, condition.ConditionThreshold.ThresholdValue, "Alert condition threshold value should not be nil")
		assert.Equal(t, 0.2, *condition.ConditionThreshold.ThresholdValue, "Alert threshold value should be 0.2 (20%)")

		// Verify condition duration (alert threshold duration)
		require.NotEmpty(t, condition.ConditionThreshold.Duration, "Alert condition duration should not be empty")
		assert.Equal(t, "3600s", condition.ConditionThreshold.Duration, "Alert threshold duration should be 3600s (1 hour)")

		// Verify condition comparison and aggregation
		assert.Equal(t, "COMPARISON_GT", condition.ConditionThreshold.Comparison, "Alert condition should use greater than comparison")
		require.NotNil(t, condition.ConditionThreshold.Aggregations, "Alert condition should have aggregations")
		assert.Len(t, condition.ConditionThreshold.Aggregations, 1, "Alert condition should have exactly one aggregation")

		aggregation := condition.ConditionThreshold.Aggregations[0]
		assert.Equal(t, "300s", *aggregation.AlignmentPeriod, "Alert aggregation should use ALIGN_RATE")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}
