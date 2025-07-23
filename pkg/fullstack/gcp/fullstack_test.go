package gcp

import (
	"testing"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudrunv2"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// name is longer than max length
func TestNameIsLongerThanMaxLength(t *testing.T) {
	f := &FullStack{
		name: "this-is-a-long-name",
	}
	name := "ok-name"
	max := 20

	resourceName := f.newResourceName(name, "resource", max)

	expected := "this-is-a-lon-ok-res"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}

// name is longer than max length
func TestNameIsLongerThanMaxLength2(t *testing.T) {
	f := &FullStack{
		name: "ok-name",
	}
	name := "this-is-a-long-name"
	max := 15

	resourceName := f.newResourceName(name, "resource", max)

	expected := "o-this-is-a-lo-r"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}

// generates a resource name with name, service name and resource type
func TestGeneratesResourceName(t *testing.T) {
	f := &FullStack{
		name: "myapp",
	}
	serviceName := "backend"
	resourceType := "account"
	max := 30

	resourceName := f.newResourceName(serviceName, resourceType, max)

	expected := "myapp-backend-account"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}

// name, service name and resource type are longer than max length
func TestResourceNameIsLongerThanMaxLength(t *testing.T) {
	f := &FullStack{
		name: "this-is-a-very-long-app-name",
	}
	serviceName := "backend-service"
	resourceType := "service-account"
	max := 25

	resourceName := f.newResourceName(serviceName, resourceType, max)

	expected := "this-is-a-very-l-bac-serv"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}

// name, service name, and resourceType is empty, within max length
func TestResourceTypeEmptyWithinMaxLength(t *testing.T) {
	f := &FullStack{
		name: "myapp",
	}
	serviceName := "backend"
	resourceType := ""
	max := 20

	resourceName := f.newResourceName(serviceName, resourceType, max)

	expected := "myapp-backend"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}

// name, service name, and resourceType is empty, needs truncation
func TestResourceTypeEmptyNeedsTruncation(t *testing.T) {
	f := &FullStack{
		name: "this-is-a-very-long-app-name",
	}
	serviceName := "backend-service"
	resourceType := ""
	max := 15

	resourceName := f.newResourceName(serviceName, resourceType, max)

	// Truncation: two parts, so prefix and serviceName are truncated
	expected := "this-is-a-ver-b"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}

type fullstackMocks struct{}

func (m *fullstackMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	// Mock resource outputs for each resource type:
	//
	// gcp:serviceaccount/account:Account
	//   - name: string (resource name)
	//   - accountId: string (service account ID)
	//   - project: string (GCP project ID)
	//   - displayName: string (human-readable name)
	//   - email: string (service account email, computed)
	//
	// gcp:cloudrunv2/service:Service
	//   - name: string (service name)
	//   - location: string (service location)
	//   - project: string (GCP project ID)
	//   - uri: string (service URL)
	//   - template: object (service template configuration)
	//
	// gcp:secretmanager/secret:Secret
	//   - secretId: string (secret identifier)
	//   - project: string (GCP project ID)
	//   - labels: map[string]string (secret labels)
	//
	// gcp:secretmanager/secretIamMember:SecretIamMember
	//   - secretId: string (secret identifier)
	//   - role: string (IAM role)
	//   - member: string (principal to bind)
	//
	// gcp:apigateway/api:Api
	//   - apiId: string (API identifier)
	//   - name: string (API name)
	//   - project: string (GCP project ID)
	//   - displayName: string (human-readable name)
	//
	// gcp:apigateway/apiConfig:ApiConfig
	//   - apiConfigId: string (API config identifier)
	//   - name: string (API config name)
	//   - api: string (API reference)
	//   - project: string (GCP project ID)
	//
	// gcp:apigateway/gateway:Gateway
	//   - name: string (gateway name)
	//   - region: string (gateway region)
	//   - project: string (GCP project ID)
	//   - apiConfig: string (API config reference)
	//   - defaultHostname: string (gateway hostname)
	outputs := map[string]interface{}{}
	for k, v := range args.Inputs {
		outputs[string(k)] = v
	}

	switch args.TypeToken {
	case "gcp:serviceaccount/account:Account":
		outputs["name"] = args.Name
		outputs["accountId"] = args.Name + "123" // Mock accountId
		outputs["project"] = "test-project"
		outputs["displayName"] = args.Name
		outputs["email"] = args.Name + "@test-project.iam.gserviceaccount.com"
		// Expected outputs: name, accountId, project, displayName, email
	case "gcp:cloudrunv2/service:Service":
		outputs["name"] = args.Name
		outputs["location"] = "us-central1"
		outputs["project"] = "test-project"
		outputs["uri"] = "https://" + args.Name + "-hash-uc.a.run.app"
		// Expected outputs: name, location, project, uri, template
	case "gcp:secretmanager/secret:Secret":
		outputs["secretId"] = args.Name
		outputs["project"] = "test-project"
		outputs["labels"] = map[string]string{"env": "production"}
		// Expected outputs: secretId, project, labels
	case "gcp:secretmanager/secretIamMember:SecretIamMember":
		outputs["secretId"] = args.Name
		outputs["role"] = "roles/secretmanager.secretAccessor"
		outputs["member"] = "user:test-user@example.com"
		// Expected outputs: secretId, role, member
	case "gcp:apigateway/api:Api":
		outputs["apiId"] = args.Name
		outputs["name"] = args.Name
		outputs["project"] = "test-project"
		outputs["displayName"] = args.Name
		// Expected outputs: apiId, name, project, displayName
	case "gcp:apigateway/apiConfig:ApiConfig":
		outputs["apiConfigId"] = args.Name
		outputs["name"] = args.Name
		outputs["api"] = args.Name + "123" // Mock apiId
		outputs["project"] = "test-project"
		// Expected outputs: apiConfigId, name, api, project
	case "gcp:apigateway/gateway:Gateway":
		outputs["name"] = args.Name
		outputs["region"] = "us-central1"
		outputs["project"] = "test-project"
		outputs["apiConfig"] = args.Name + "123" // Mock apiConfigId
		outputs["defaultHostname"] = args.Name + ".apigateway.test-project.cloud.goog"
		// Expected outputs: name, region, project, apiConfig, defaultHostname
	}

	return args.Name + "_id", resource.NewPropertyMapFromMap(outputs), nil
}

func (m *fullstackMocks) Call(_ pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return resource.PropertyMap{}, nil
}

func TestNewFullStack_HappyPath(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		args := &FullStackArgs{
			Project:       "test-project",
			Region:        "us-central1",
			BackendName:   "backend",
			BackendImage:  "gcr.io/test-project/backend:latest",
			FrontendName:  "frontend",
			FrontendImage: "gcr.io/test-project/frontend:latest",
			Backend: &BackendArgs{
				InstanceArgs: &InstanceArgs{
					ResourceLimits: pulumi.StringMap{
						"memory": pulumi.String("512Mi"),
						"cpu":    pulumi.String("500m"),
					},
					SecretConfigFileName: ".env",
					SecretConfigFilePath: "/app/config/",
					EnvVars: map[string]string{
						"LOG_LEVEL":    "info",
						"DATABASE_URL": "postgresql://localhost:5432/testdb",
					},
					MaxInstanceCount:   5,
					DeletionProtection: true,
					ContainerPort:      8080,
				},
			},
			Frontend: &FrontendArgs{
				InstanceArgs: &InstanceArgs{
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
			Network: &NetworkArgs{
				DomainURL: "myapp.example.com",
				APIGateway: &APIGatewayArgs{
					Name: "gateway",
					Config: &APIConfigArgs{
						EnableCORS: true,
					},
				},
			},
		}

		fullstack, err := NewFullStack(ctx, "test-fullstack", args)
		require.NoError(t, err)

		// Verify basic properties
		assert.Equal(t, "test-project", fullstack.Project)
		assert.Equal(t, "us-central1", fullstack.Region)
		assert.Equal(t, "backend", fullstack.BackendName)
		assert.Equal(t, "frontend", fullstack.FrontendName)
		assert.Equal(t, "gcr.io/test-project/backend:latest", fullstack.BackendImage)
		assert.Equal(t, "gcr.io/test-project/frontend:latest", fullstack.FrontendImage)

		// Test resource name generation with the new method
		resourceName := fullstack.newResourceName("backend", "service", 100)
		assert.Equal(t, "test-fullstack-backend-service", resourceName)

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
		assert.Equal(t, "test-project", <-backendProjectCh, "Backend service project should match")

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
		assert.Equal(t, "test-project", <-frontendProjectCh, "Frontend service project should match")

		frontendLocationCh := make(chan string, 1)
		defer close(frontendLocationCh)
		frontendService.Location.ApplyT(func(location string) error {
			frontendLocationCh <- location
			return nil
		})
		assert.Equal(t, "us-central1", <-frontendLocationCh, "Frontend service location should match")

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
		assert.Equal(t, "us-central1", <-gatewayRegionCh, "API Gateway region should match the project region")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))

	if err != nil {
		t.Fatalf("Pulumi WithMocks failed: %v", err)
	}
}
