// Package gcp provides Google Cloud Platform infrastructure components for fullstack applications.
package gcp

import (
	"fmt"

	apigateway "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/apigateway"
	cloudrunv2 "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudrunv2"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// APIGatewayArgs contains configuration for Google API Gateway
type APIGatewayArgs struct {
	// Name of the API Gateway and its resources. Defaults to "gateway".
	Name string
	// API Gateway configuration. Required when enabled.
	Config *APIConfigArgs
	// Whether to disable API Gateway. Defaults to false.
	Disabled bool
	// List of regions where to deploy API Gateway instances.
	Regions []string
}

// APIConfigArgs contains configuration for API Gateway API Config
type APIConfigArgs struct {
	// OpenAPI specification file path. Optional - defaults to "/openapi.yaml".
	OpenAPISpecPath string
	// Backend service URL (Cloud Run service URL). Required.
	BackendServiceURL pulumi.StringOutput
	// Frontend service URL (Cloud Run service URL). Required for dual routing.
	FrontendServiceURL pulumi.StringOutput
	// Whether to enable CORS. Defaults to true.
	EnableCORS bool
	// CORS allowed origins. Defaults to ["*"].
	CORSAllowedOrigins []string
	// CORS allowed methods. Defaults to ["GET", "POST", "PUT", "DELETE", "OPTIONS"].
	CORSAllowedMethods []string
	// CORS allowed headers. Defaults to ["*"].
	CORSAllowedHeaders []string
}

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

	// Create dedicated service account for API Gateway
	apiGatewayAccountName := f.newResourceName(args.Name, "account", 28)
	apiGatewayServiceAccount, err := serviceaccount.NewAccount(ctx, apiGatewayAccountName, &serviceaccount.AccountArgs{
		AccountId:   pulumi.String(apiGatewayAccountName),
		DisplayName: pulumi.String(fmt.Sprintf("API Gateway service account (%s)", args.Name)),
		Project:     pulumi.String(f.Project),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create API Gateway service account: %w", err)
	}
	ctx.Export("api_gateway_service_account_id", apiGatewayServiceAccount.ID())
	ctx.Export("api_gateway_service_account_email", apiGatewayServiceAccount.Email)

	// Use backend name as base for API ID
	apiID := f.newResourceName(args.Name, "api", 50)
	displayName := f.newResourceName(args.Name, "api", 100)

	// Create the API
	api, err := apigateway.NewApi(ctx, apiID, &apigateway.ApiArgs{
		ApiId:       pulumi.String(apiID),
		DisplayName: pulumi.String(displayName),
		Project:     pulumi.String(f.Project),
	})
	if err != nil {
		return nil, err
	}
	ctx.Export("api_gateway_api_id", api.ApiId)
	ctx.Export("api_gateway_api_name", api.Name)

	apiConfig, err := f.createAPIConfig(ctx, apiID, args.Config, api, apiGatewayServiceAccount.Email)
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
	gatewayDisplayName := f.newResourceName(args.Name, "", 100)

	gateway, err := apigateway.NewGateway(ctx, gatewayID, &apigateway.GatewayArgs{
		GatewayId:   pulumi.String(gatewayID),
		DisplayName: pulumi.String(gatewayDisplayName),
		Region:      pulumi.String(region),
		Project:     pulumi.String(f.Project),
		ApiConfig:   apiConfig.Name,
	})
	if err != nil {
		return nil, err
	}
	ctx.Export("api_gateway_gateway_id", gateway.GatewayId)
	ctx.Export("api_gateway_gateway_name", gateway.Name)
	ctx.Export("api_gateway_gateway_default_hostname", gateway.DefaultHostname)

	// Grant API Gateway service account permission to invoke Cloud Run services
	err = f.grantAPIGatewayInvokerPermissions(ctx, apiGatewayServiceAccount.Email, args.Name)
	if err != nil {
		return nil, err
	}

	return gateway, nil
}

// grantAPIGatewayInvokerPermissions grants the API Gateway service account
// permission to invoke both backend and frontend Cloud Run services.
//
// This function ensures that the dedicated API Gateway service account can
// properly route traffic to the Cloud Run services.
func (f *FullStack) grantAPIGatewayInvokerPermissions(ctx *pulumi.Context, apiGatewayServiceAccountEmail pulumi.StringOutput, gatewayName string) error {
	// Grant API Gateway permission to invoke backend service
	backendInvokerName := f.newResourceName(gatewayName, "backend-invoker", 100)
	_, err := cloudrunv2.NewServiceIamMember(ctx, backendInvokerName, &cloudrunv2.ServiceIamMemberArgs{
		Name:     pulumi.String(gatewayName),
		Project:  pulumi.String(f.Project),
		Location: pulumi.String(f.Region),
		Role:     pulumi.String("roles/run.invoker"),
		Member: apiGatewayServiceAccountEmail.ApplyT(func(email string) string {
			return fmt.Sprintf("serviceAccount:%s", email)
		}).(pulumi.StringOutput),
	})
	if err != nil {
		return fmt.Errorf("failed to grant API Gateway backend invoker permissions: %w", err)
	}

	// Grant API Gateway permission to invoke frontend service
	frontendInvokerName := f.newResourceName(f.FrontendName, "frontend-invoker", 100)
	_, err = cloudrunv2.NewServiceIamMember(ctx, frontendInvokerName, &cloudrunv2.ServiceIamMemberArgs{
		Name:     pulumi.String(f.FrontendName),
		Project:  pulumi.String(f.Project),
		Location: pulumi.String(f.Region),
		Role:     pulumi.String("roles/run.invoker"),
		Member: apiGatewayServiceAccountEmail.ApplyT(func(email string) string {
			return fmt.Sprintf("serviceAccount:%s", email)
		}).(pulumi.StringOutput),
	})
	if err != nil {
		return fmt.Errorf("failed to grant API Gateway frontend invoker permissions: %w", err)
	}

	return nil
}

// createAPIConfig configures the API gateway, and sets the
// Gateway service account email used to invoke the backend and frontend.
// The OpenAPI spec document is responsible for mapping paths to the backend and
// frontend services URLs. See BackendServiceURL and FrontendServiceURL.
func (f *FullStack) createAPIConfig(ctx *pulumi.Context, apiID string, configArgs *APIConfigArgs, api *apigateway.Api, gatewayServiceAccountEmail pulumi.StringOutput) (*apigateway.ApiConfig, error) {
	if configArgs == nil {
		return nil, fmt.Errorf("APIConfigArgs is required")
	}

	// Set default OpenAPI spec path if not provided
	openAPISpecPath := configArgs.OpenAPISpecPath
	if openAPISpecPath == "" {
		openAPISpecPath = "/openapi.yaml"
	}

	// Generate OpenAPI spec with backend routing
	openAPISpec := f.generateOpenAPISpec(configArgs)

	// Create API Config
	apiConfig, err := apigateway.NewApiConfig(ctx, fmt.Sprintf("%s-config", apiID), &apigateway.ApiConfigArgs{
		Api:         api.ApiId,
		ApiConfigId: pulumi.String(fmt.Sprintf("%s-config", apiID)),
		DisplayName: pulumi.String(fmt.Sprintf("Config for %s", apiID)),
		Project:     pulumi.String(f.Project),
		OpenapiDocuments: apigateway.ApiConfigOpenapiDocumentArray{
			&apigateway.ApiConfigOpenapiDocumentArgs{
				Document: &apigateway.ApiConfigOpenapiDocumentDocumentArgs{
					Path: pulumi.String(openAPISpecPath),
					Contents: pulumi.All(openAPISpec).ApplyT(func(args []interface{}) (string, error) {
						return args[0].(string), nil
					}).(pulumi.StringOutput),
				},
			},
		},
		GatewayConfig: &apigateway.ApiConfigGatewayConfigArgs{
			BackendConfig: &apigateway.ApiConfigGatewayConfigBackendConfigArgs{
				GoogleServiceAccount: gatewayServiceAccountEmail,
			},
		},
	})
	if err != nil {
		return nil, err
	}

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
func (f *FullStack) generateOpenAPISpec(configArgs *APIConfigArgs) pulumi.StringOutput {
	// Set default CORS values
	corsAllowedOrigins := configArgs.CORSAllowedOrigins
	if len(corsAllowedOrigins) == 0 {
		corsAllowedOrigins = []string{"*"}
	}

	corsAllowedMethods := configArgs.CORSAllowedMethods
	if len(corsAllowedMethods) == 0 {
		corsAllowedMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	}

	corsAllowedHeaders := configArgs.CORSAllowedHeaders
	if len(corsAllowedHeaders) == 0 {
		corsAllowedHeaders = []string{"*"}
	}

	openAPISpec := pulumi.All(configArgs.BackendServiceURL, configArgs.FrontendServiceURL).ApplyT(func(args []interface{}) (string, error) {
		backendURL := args[0].(string)
		frontendURL := args[1].(string)

		spec := fmt.Sprintf(`openapi: 3.0.1
info:
  title: API Gateway for Cloud Run
  description: API Gateway routing to Cloud Run backend and frontend
  version: 1.0.0
servers:
  - url: https://{gateway_host}
paths:
  /api/{proxy+}:
    x-google-backend:
      address: %s/{proxy}
    get:
      operationId: apiProxyGet
      parameters:
        - name: proxy
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: object
        '404':
          description: Not found
    post:
      operationId: apiProxyPost
      parameters:
        - name: proxy
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: false
        content:
          application/json:
            schema:
              type: object
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: object
        '404':
          description: Not found
    put:
      operationId: apiProxyPut
      parameters:
        - name: proxy
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: false
        content:
          application/json:
            schema:
              type: object
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: object
        '404':
          description: Not found
    delete:
      operationId: apiProxyDelete
      parameters:
        - name: proxy
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: object
        '404':
          description: Not found
    options:
      operationId: apiProxyOptions
      parameters:
        - name: proxy
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: CORS preflight response
          headers:
            Access-Control-Allow-Origin:
              schema:
                type: string
            Access-Control-Allow-Methods:
              schema:
                type: string
            Access-Control-Allow-Headers:
              schema:
                type: string
  /{proxy+}:
    x-google-backend:
      address: %s/{proxy}
    get:
      operationId: frontendProxyGet
      parameters:
        - name: proxy
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Successful response
          content:
            text/html:
              schema:
                type: string
        '404':
          description: Not found
    options:
      operationId: frontendProxyOptions
      parameters:
        - name: proxy
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: CORS preflight response
          headers:
            Access-Control-Allow-Origin:
              schema:
                type: string
            Access-Control-Allow-Methods:
              schema:
                type: string
            Access-Control-Allow-Headers:
              schema:
                type: string
`, backendURL, frontendURL)

		// Add CORS configuration if enabled
		if configArgs.EnableCORS {
			spec += fmt.Sprintf(`
x-google-cors:
  allowOrigin: %s
  allowMethods: %s
  allowHeaders: %s
  exposeHeaders: "Content-Length"
  maxAge: "3600"
`, corsAllowedOrigins[0], corsAllowedMethods[0], corsAllowedHeaders[0])
		}

		return spec, nil
	}).(pulumi.StringOutput)

	return openAPISpec
}
