package gcp

import (
	"fmt"

	apigateway "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/apigateway"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// APIGatewayArgs contains configuration for Google API Gateway
type APIGatewayArgs struct {
	// API Gateway configuration. Required when enabled.
	Config *APIConfigArgs
	// Whether to enable API Gateway. Defaults to false.
	Enabled bool
	// List of regions where to deploy API Gateway instances.
	Regions []string
}

// APIConfigArgs contains configuration for API Gateway API Config
type APIConfigArgs struct {
	// OpenAPI specification file path. Optional - defaults to "/openapi.yaml".
	OpenAPISpecPath string
	// Backend service URL (Cloud Run service URL). Required.
	BackendServiceURL pulumi.StringOutput
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
// - API definition with OpenAPI spec
// - API Config with backend routing to Cloud Run
// - Regional gateways for external access
// - CORS support for web applications
//
// See:
// https://cloud.google.com/api-gateway/docs/gateway-serverless-neg
// https://cloud.google.com/api-gateway/docs/gateway-load-balancing
func (f *FullStack) deployAPIGateway(ctx *pulumi.Context, args *APIGatewayArgs) (*apigateway.Gateway, error) {
	if args == nil || !args.Enabled {
		return nil, nil
	}

	if args.Config == nil {
		return nil, fmt.Errorf("APIConfigArgs is required when API Gateway is enabled")
	}

	// Use backend name as base for API ID
	apiID := f.newResourceName(f.BackendName, 50)
	displayName := f.newResourceName(f.BackendName, 100)

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

	// Create API Config
	apiConfig, err := f.createAPIConfig(ctx, apiID, args.Config, api)
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

	gatewayID := f.newResourceName(fmt.Sprintf("%s-%s", f.BackendName, region), 50)
	gatewayDisplayName := f.newResourceName(fmt.Sprintf("%s-%s", f.BackendName, region), 100)

	// Create Gateway
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

	return gateway, nil
}

func (f *FullStack) createAPIConfig(ctx *pulumi.Context, apiID string, configArgs *APIConfigArgs, api *apigateway.Api) (*apigateway.ApiConfig, error) {
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

	// Generate OpenAPI spec with backend routing to Cloud Run
	openAPISpec := pulumi.All(configArgs.BackendServiceURL).ApplyT(func(args []interface{}) (string, error) {
		backendURL := args[0].(string)

		spec := fmt.Sprintf(`openapi: 3.0.1
info:
  title: API Gateway for Cloud Run
  description: API Gateway routing to Cloud Run backend
  version: 1.0.0
servers:
  - url: https://{gateway_host}
paths:
  /{proxy+}:
    x-google-backend:
      address: %s/{proxy}
    get:
      operationId: proxyGet
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
      operationId: proxyPost
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
      operationId: proxyPut
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
      operationId: proxyDelete
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
      operationId: proxyOptions
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
`, backendURL)

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
