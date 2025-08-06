// Package gcp provides Google Cloud Platform infrastructure components for fullstack applications.
package gcp

import (
	"encoding/base64"
	"fmt"
	"log"

	"github.com/getkin/kin-openapi/openapi2conv"
	apigateway "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/apigateway"
	cloudrunv2 "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudrunv2"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// deployAPIGateway sets up Google API Gateway with the following features:
//
// - Dedicated service account for API Gateway
// - API definition with OpenAPI spec
// - API Config with backend routing to Cloud Run
// - Regional gateways for external access
// - CORS support for web applications
// - Proper IAM permissions for API Gateway to invoke Cloud Run services
//
// See:
// https://cloud.google.com/api-gateway/docs/gateway-serverless-neg
// https://cloud.google.com/api-gateway/docs/gateway-load-balancing
func (f *FullStack) deployAPIGateway(ctx *pulumi.Context, args *APIGatewayArgs) (*apigateway.Gateway, error) {
	if args == nil || args.Disabled {
		return nil, nil
	}

	if args.Config == nil {
		return nil, fmt.Errorf("APIConfigArgs is required when API Gateway is enabled")
	}

	if err := ctx.Log.Info(fmt.Sprintf("Routing traffic to API Gateway: %#v", args), nil); err != nil {
		log.Println("failed to log API Gateway args with Pulumi context: %w", err)
	}

	// Create API Gateway IAM resources (service account and permissions)
	gatewayServiceAccount, err := f.createAPIGatewayIAM(ctx, args.Name)
	if err != nil {
		return nil, err
	}

	apiID := f.newResourceName(args.Name, "api", 50)
	displayName := fmt.Sprintf("Gateway API (apiID: %s)", apiID)
	gatewayLabels := mergeLabels(f.Labels, pulumi.StringMap{
		"gateway": pulumi.String("true"),
	})

	// Create the API
	api, err := apigateway.NewApi(ctx, apiID, &apigateway.ApiArgs{
		ApiId:       pulumi.String(apiID),
		DisplayName: pulumi.String(displayName),
		Project:     pulumi.String(f.Project),
		Labels:      gatewayLabels,
	})
	if err != nil {
		return nil, err
	}
	ctx.Export("api_gateway_api_id", api.ApiId)
	ctx.Export("api_gateway_api_name", api.Name)

	apiConfig, err := f.createAPIConfig(ctx, apiID, args.Config, api, gatewayServiceAccount.Email, gatewayLabels)
	if err != nil {
		return nil, err
	}
	ctx.Export("api_gateway_config_id", apiConfig.ApiConfigId)
	ctx.Export("api_gateway_config_name", apiConfig.Name)

	// Create Gateway in the first region (or default region if none specified)
	region := f.Region
	if len(args.Regions) > 0 {
		region = args.Regions[0]
	}

	gatewayID := f.newResourceName(args.Name, "", 50)
	gatewayDisplayName := fmt.Sprintf("Gateway (gatewayID: %s)", gatewayID)

	gateway, err := apigateway.NewGateway(ctx, gatewayID, &apigateway.GatewayArgs{
		GatewayId:   pulumi.String(gatewayID),
		DisplayName: pulumi.String(gatewayDisplayName),
		Region:      pulumi.String(region),
		Project:     pulumi.String(f.Project),
		ApiConfig:   apiConfig.ID(),
		Labels:      gatewayLabels,
	})
	if err != nil {
		return nil, err
	}
	ctx.Export("api_gateway_gateway_id", gateway.GatewayId)
	ctx.Export("api_gateway_gateway_name", gateway.Name)
	ctx.Export("api_gateway_gateway_default_hostname", gateway.DefaultHostname)

	f.apiGateway = gateway

	return gateway, nil
}

// createAPIGatewayIAM creates a dedicated service account for API Gateway and grants
// it the necessary permissions to invoke Cloud Run services.
//
// This function ensures that the API Gateway has its own identity and can properly
// route traffic to both backend and frontend Cloud Run services.
func (f *FullStack) createAPIGatewayIAM(ctx *pulumi.Context, gatewayName string) (*serviceaccount.Account, error) {
	// Create dedicated service account for API Gateway
	apiGatewayAccountName := f.newResourceName(gatewayName, "account", 28)
	serviceAccount, err := serviceaccount.NewAccount(ctx, apiGatewayAccountName, &serviceaccount.AccountArgs{
		AccountId:   pulumi.String(apiGatewayAccountName),
		DisplayName: pulumi.String(fmt.Sprintf("API Gateway service account (%s)", gatewayName)),
		Project:     pulumi.String(f.Project),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create API Gateway service account: %w", err)
	}
	ctx.Export("api_gateway_service_account_id", serviceAccount.ID())
	ctx.Export("api_gateway_service_account_email", serviceAccount.Email)
	f.gatewayServiceAccount = serviceAccount

	// Grant API Gateway service account permission to invoke Cloud Run services
	err = f.grantAPIGatewayInvokerPermissions(ctx, serviceAccount.Email, gatewayName)
	if err != nil {
		return nil, err
	}

	return serviceAccount, nil
}

// grantAPIGatewayInvokerPermissions grants the API Gateway service account
// permission to invoke both backend and frontend Cloud Run services.
//
// This function ensures that the dedicated API Gateway service account can
// properly route traffic to the Cloud Run services.
func (f *FullStack) grantAPIGatewayInvokerPermissions(ctx *pulumi.Context, apiGatewayServiceAccountEmail pulumi.StringOutput, gatewayName string) error {
	// Grant API Gateway permission to invoke backend service
	backendInvokerName := f.newResourceName(gatewayName, "backend-invoker", 100)
	backendIamMember, err := cloudrunv2.NewServiceIamMember(ctx, backendInvokerName, &cloudrunv2.ServiceIamMemberArgs{
		Name:     f.backendService.Name,
		Project:  pulumi.String(f.Project),
		Location: pulumi.String(f.Region),
		Role:     pulumi.String("roles/run.invoker"),
		Member:   pulumi.Sprintf("serviceAccount:%s", apiGatewayServiceAccountEmail),
	})
	if err != nil {
		return fmt.Errorf("failed to grant API Gateway backend invoker permissions: %w", err)
	}
	f.backendGatewayIamMember = backendIamMember

	// Grant API Gateway permission to invoke frontend service
	frontendInvokerName := f.newResourceName(gatewayName, "frontend-invoker", 100)
	frontendIamMember, err := cloudrunv2.NewServiceIamMember(ctx, frontendInvokerName, &cloudrunv2.ServiceIamMemberArgs{
		Name:     f.frontendService.Name,
		Project:  pulumi.String(f.Project),
		Location: pulumi.String(f.Region),
		Role:     pulumi.String("roles/run.invoker"),
		Member:   pulumi.Sprintf("serviceAccount:%s", apiGatewayServiceAccountEmail),
	})
	if err != nil {
		return fmt.Errorf("failed to grant API Gateway frontend invoker permissions: %w", err)
	}
	f.frontendGatewayIamMember = frontendIamMember

	return nil
}

// createAPIConfig configures the API gateway, and sets the
// Gateway service account email used to invoke the backend and frontend.
// The OpenAPI spec document is responsible for mapping paths to the backend and
// frontend services URLs. See Backend.ServiceURL and Frontend.ServiceURL.
func (f *FullStack) createAPIConfig(ctx *pulumi.Context,
	apiID string,
	configArgs *APIConfigArgs,
	api *apigateway.Api,
	gatewayServiceAccountEmail pulumi.StringOutput,
	gatewayLabels pulumi.StringMap) (*apigateway.ApiConfig, error) {

	if configArgs == nil {
		return nil, fmt.Errorf("APIConfigArgs is required")
	}

	// Set default OpenAPI spec path if not provided
	openAPISpecPath := configArgs.OpenAPISpecPath
	if openAPISpecPath == "" {
		openAPISpecPath = "/openapi.yaml"
	}

	// Generate OpenAPI spec with backend routing
	openAPISpec := f.generateOpenAPISpec(ctx, configArgs)

	// Convert OpenAPI spec to base64 encoding
	base64OpenAPISpec := openAPISpec.ApplyT(func(spec string) string {
		return base64.StdEncoding.EncodeToString([]byte(spec))
	}).(pulumi.StringOutput)

	// Create API Config
	apiConfig, err := apigateway.NewApiConfig(ctx, fmt.Sprintf("%s-config", apiID), &apigateway.ApiConfigArgs{
		Api:         api.ApiId,
		ApiConfigId: pulumi.String(fmt.Sprintf("%s-config", apiID)),
		DisplayName: pulumi.String(fmt.Sprintf("Config for %s", apiID)),
		Project:     pulumi.String(f.Project),
		OpenapiDocuments: apigateway.ApiConfigOpenapiDocumentArray{
			&apigateway.ApiConfigOpenapiDocumentArgs{
				Document: &apigateway.ApiConfigOpenapiDocumentDocumentArgs{
					Path:     pulumi.String(openAPISpecPath),
					Contents: base64OpenAPISpec,
				},
			},
		},
		GatewayConfig: &apigateway.ApiConfigGatewayConfigArgs{
			BackendConfig: &apigateway.ApiConfigGatewayConfigBackendConfigArgs{
				GoogleServiceAccount: gatewayServiceAccountEmail,
			},
		},
		Labels: gatewayLabels,
	}, pulumi.ReplaceOnChanges([]string{"*"}))
	if err != nil {
		return nil, err
	}
	f.apiConfig = apiConfig

	return apiConfig, nil
}

// generateOpenAPISpec creates a standard OpenAPI 3.0.1 specification for API Gateway
// that routes all traffic to the Cloud Run backend service. This YAML boilerplate
// is required by Google API Gateway to understand the API structure and routing rules.
//
// The specification includes:
// - Proxy routing with {proxy+} path parameter to forward all requests
// - Support for GET, POST, PUT, DELETE, OPTIONS HTTP methods
// - CORS configuration for web applications
// - Backend routing to Cloud Run service
//
// See:
// https://cloud.google.com/api-gateway/docs/reference/rest/v1/projects.locations.apis.configs#OpenApiDocument
func (f *FullStack) generateOpenAPISpec(ctx *pulumi.Context, configArgs *APIConfigArgs) pulumi.StringOutput {
	openAPISpec := pulumi.All(
		configArgs.Backend.ServiceURL,
		configArgs.Frontend.ServiceURL,
		f.frontendService.Template.ServiceAccount(),
	).ApplyT(func(args []interface{}) (string, error) {

		backendURL := args[0].(string)
		frontendURL := args[1].(string)

		// If JWT is enabled, set config defaults
		frontendServiceAccountEmailPtr := args[2].(*string)
		if frontendServiceAccountEmailPtr != nil {
			applyJWTConfigDefaults(configArgs.Backend.JWTAuth, *frontendServiceAccountEmailPtr)
		}

		v3Spec := newOpenAPISpec(backendURL, frontendURL, configArgs, configArgs.Backend.JWTAuth)

		// Debug: Print v3 spec
		v3JSON, err := v3Spec.MarshalJSON()
		if err != nil {
			return "", fmt.Errorf("failed to marshal v3 spec: %w", err)
		}
		if err := ctx.Log.Debug(fmt.Sprintf("DEBUG: OpenAPI v3 spec:\n%s\n", string(v3JSON)), nil); err != nil {
			log.Printf("failed to log v3 spec with Pulumi context: %v", err)
		}

		// Convert OpenAPI 3 to OpenAPI 2 spec as expected by Google API Gateway
		// See:
		// - https://cloud.google.com/endpoints/docs/openapi
		// - https://github.com/cloudendpoints/esp/issues/446
		v2Spec, err := openapi2conv.FromV3(v3Spec)
		if err != nil {
			return "", fmt.Errorf("failed to convert v3 to v2: %w", err)
		}

		// Debug: Print v2 spec
		v2JSON, err := v2Spec.MarshalJSON()
		if err != nil {
			return "", fmt.Errorf("failed to marshal v2 spec: %w", err)
		}
		if err := ctx.Log.Debug(fmt.Sprintf("DEBUG: OpenAPI v2 spec:\n%s\n", string(v2JSON)), nil); err != nil {
			log.Printf("failed to log v2 spec with Pulumi context: %v", err)
		}

		return string(v2JSON), nil
	}).(pulumi.StringOutput)

	return openAPISpec
}

// applyJWTConfigDefaults applies default JWT configuration values for service-to-service authentication
func applyJWTConfigDefaults(jwtAuth *JWTAuth, frontendServiceAccountEmail string) {
	if jwtAuth != nil {
		if jwtAuth.Issuer == "" {
			jwtAuth.Issuer = frontendServiceAccountEmail
		}
		if jwtAuth.JwksURI == "" {
			jwtAuth.JwksURI = fmt.Sprintf("https://www.googleapis.com/service_accounts/v1/metadata/x509/%s", frontendServiceAccountEmail)
		}
	}
}

// applyDefaultGatewayArgs applies default API Gateway configuration to the provided args.
// If the provided args is nil, it returns a new instance with default config.
// If the provided args has a nil Config, it applies the default config.
func applyDefaultGatewayArgs(args *APIGatewayArgs, backendServiceURL, frontendServiceURL pulumi.StringOutput) *APIGatewayArgs {
	var gatewayArgs *APIGatewayArgs
	if args == nil {
		gatewayArgs = &APIGatewayArgs{}
	} else {
		gatewayArgs = args
	}

	if gatewayArgs.Config == nil {
		gatewayArgs.Config = &APIConfigArgs{}
	}

	// Initialize Backend and Frontend if they are nil
	if gatewayArgs.Config.Backend == nil {
		gatewayArgs.Config.Backend = &Upstream{}
	}
	if gatewayArgs.Config.Frontend == nil {
		gatewayArgs.Config.Frontend = &Upstream{}
	}

	// Ignore any value given and set always to the services URLs
	gatewayArgs.Config.Backend.ServiceURL = backendServiceURL
	gatewayArgs.Config.Frontend.ServiceURL = frontendServiceURL

	if gatewayArgs.Name == "" {
		gatewayArgs.Name = "gateway"
	}

	return gatewayArgs
}
