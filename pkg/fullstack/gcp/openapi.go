package gcp

import (
	"github.com/getkin/kin-openapi/openapi3"
)

// Path translation constants
const (
	AppendPathToAddress     = "APPEND_PATH_TO_ADDRESS"
	ConstantAddress         = "CONSTANT_ADDRESS"
	ConstantAddressWithPath = "CONSTANT_ADDRESS_WITH_PATH"
)

// newOpenAPISpec creates a new OpenAPI 3.0.1 specification for API Gateway
// that routes traffic to Cloud Run backend and frontend services.
func newOpenAPISpec(backendServiceURI, frontendServiceURI string, configArgs *APIConfigArgs, backendJWTConfig *JWTAuth) *openapi3.T {
	paths := &openapi3.Paths{}
	securitySchemes := make(openapi3.SecuritySchemes)

	// Configure JWT authentication if enabled for backend
	if backendJWTConfig != nil {
		// Add JWT security scheme for service-to-service authentication
		securitySchemes["JWT"] = &openapi3.SecuritySchemeRef{
			Value: &openapi3.SecurityScheme{
				Type:         "http",
				Scheme:       "bearer",
				BearerFormat: "JWT",
				Extensions: map[string]interface{}{
					"x-google-issuer":   backendJWTConfig.Issuer,
					"x-google-jwks_uri": backendJWTConfig.JwksURI,
				},
			},
		}
	}

	// Add backend API paths
	if configArgs != nil && configArgs.Backend != nil && len(configArgs.Backend.APIPaths) > 0 {
		addPaths(paths, configArgs.Backend.APIPaths, backendServiceURI, createAPIPathItem, backendJWTConfig)
	} else {
		// Default backend path if none specified
		pathItem := createAPIPathItem(backendServiceURI, "/api/v1", AppendPathToAddress)
		// Apply JWT security if configured
		if backendJWTConfig != nil {
			pathItem.Get.Security = &openapi3.SecurityRequirements{{"JWT": []string{}}}
			pathItem.Post.Security = &openapi3.SecurityRequirements{{"JWT": []string{}}}
			pathItem.Put.Security = &openapi3.SecurityRequirements{{"JWT": []string{}}}
			pathItem.Delete.Security = &openapi3.SecurityRequirements{{"JWT": []string{}}}
			// OPTIONS should not require authentication for CORS preflight
		}
		paths.Set("/api/v1/{proxy}", pathItem)
	}

	// Add frontend API paths
	if configArgs != nil && configArgs.Frontend != nil && len(configArgs.Frontend.APIPaths) > 0 {
		addPaths(paths, configArgs.Frontend.APIPaths, frontendServiceURI, createUIPathItem, nil) // Frontend doesn't need JWT auth
	} else {
		// Default frontend path if none specified
		paths.Set("/ui/{proxy}", createUIPathItem(frontendServiceURI, "/ui/v1", AppendPathToAddress))
	}

	spec := &openapi3.T{
		OpenAPI: "3.0.1",
		Info: &openapi3.Info{
			Title:       "API Gateway for Cloud Run",
			Description: "API Gateway routing to Cloud Run backend and frontend",
			Version:     "1.0.0",
		},
		Servers: openapi3.Servers{
			&openapi3.Server{
				URL: "https://{gateway_host}",
			},
		},
		Paths: paths,
		Components: &openapi3.Components{
			SecuritySchemes: securitySchemes,
		},
	}

	// Add CORS configuration if enabled
	if configArgs != nil && configArgs.EnableCORS {
		spec.Extensions = make(map[string]interface{})
		spec.Extensions["x-google-cors"] = createCORSConfig(configArgs)
	}

	return spec
}

// createUpstreamPath is a function type for creating path items
type createUpstreamPath func(serviceURI, upstreamPath, pathTranslation string) *openapi3.PathItem

// addPaths creates OpenAPI paths from a list of path configurations
func addPaths(paths *openapi3.Paths, pathConfigs []*APIPathArgs, serviceURI string, createPathItem createUpstreamPath, jwtAuthConfig *JWTAuth) {
	for _, pathConfig := range pathConfigs {

		// Always match the remaining of the path (/{proxy}) and pass it to the upstream
		gatewayPath := pathConfig.Path + "/{proxy}"
		upstreamPath := pathConfig.UpstreamPath

		// Decide how to translate to upstream
		var pathTranslation string

		rewritePath := upstreamPath != "" && upstreamPath != pathConfig.Path
		if rewritePath {
			// The gateway and upstream share the same path
			pathTranslation = AppendPathToAddress
		} else {
			// Path rewriting - the upstream chose its own path
			upstreamPath = pathConfig.UpstreamPath
			pathTranslation = ConstantAddress
		}

		pathItem := createPathItem(serviceURI, upstreamPath, pathTranslation)

		// Apply JWT security if configured
		if jwtAuthConfig != nil {
			pathItem.Get.Security = &openapi3.SecurityRequirements{{"JWT": []string{}}}
			pathItem.Post.Security = &openapi3.SecurityRequirements{{"JWT": []string{}}}
			pathItem.Put.Security = &openapi3.SecurityRequirements{{"JWT": []string{}}}
			pathItem.Delete.Security = &openapi3.SecurityRequirements{{"JWT": []string{}}}
			// OPTIONS should not require authentication for CORS preflight
		}

		paths.Set(gatewayPath, pathItem)
	}
}

// createCORSConfig creates the CORS configuration for Google API Gateway
func createCORSConfig(configArgs *APIConfigArgs) map[string]interface{} {
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

	return map[string]interface{}{
		"allowOrigin":   corsAllowedOrigins[0],
		"allowMethods":  corsAllowedMethods[0],
		"allowHeaders":  corsAllowedHeaders[0],
		"exposeHeaders": "Content-Length",
		"maxAge":        "3600",
	}
}

// createAPIPathItem creates a PathItem for API routes with all HTTP methods
func createAPIPathItem(backendServiceURI, upstreamPath, pathTranslation string) *openapi3.PathItem {
	return &openapi3.PathItem{
		Get:     createAPIOperation("apiProxyGet", "get", backendServiceURI, upstreamPath, pathTranslation),
		Post:    createAPIOperation("apiProxyPost", "post", backendServiceURI, upstreamPath, pathTranslation),
		Put:     createAPIOperation("apiProxyPut", "put", backendServiceURI, upstreamPath, pathTranslation),
		Delete:  createAPIOperation("apiProxyDelete", "delete", backendServiceURI, upstreamPath, pathTranslation),
		Options: createCORSOperation("apiProxyOptions", backendServiceURI, upstreamPath, pathTranslation),
	}
}

// createUIPathItem creates a PathItem for UI routes with GET and OPTIONS methods
func createUIPathItem(frontendServiceURI, upstreamPath, pathTranslation string) *openapi3.PathItem {
	return &openapi3.PathItem{
		Get:     createUIOperation("uiProxyGet", frontendServiceURI, upstreamPath, pathTranslation),
		Options: createCORSOperation("uiProxyOptions", frontendServiceURI, upstreamPath, pathTranslation),
	}
}

// createAPIOperation creates an operation for API endpoints
func createAPIOperation(operationID, method, serviceURI, upstreamPath, pathTranslation string) *openapi3.Operation {
	operation := &openapi3.Operation{
		OperationID: operationID,
		Parameters: []*openapi3.ParameterRef{
			{
				Value: &openapi3.Parameter{
					Name:     "proxy",
					In:       "path",
					Required: true,
					Schema:   &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
				},
			},
		},
		Responses: openapi3.NewResponses(),
		Extensions: map[string]interface{}{
			"x-google-backend": map[string]interface{}{
				"address":         serviceURI + upstreamPath,
				"pathTranslation": pathTranslation,
				"protocol":        "h2",
			},
		},
	}

	// Add request body for POST and PUT operations
	if method == "post" || method == "put" {
		operation.RequestBody = &openapi3.RequestBodyRef{
			Value: &openapi3.RequestBody{
				Required: false,
				Content:  openapi3.NewContentWithJSONSchema(openapi3.NewObjectSchema()),
			},
		}
	}

	// Add responses with proper descriptions for v2 compatibility
	operation.Responses.Set("200", &openapi3.ResponseRef{Value: &openapi3.Response{
		Description: stringPtr("Successful response"),
		Content:     openapi3.NewContentWithJSONSchema(openapi3.NewObjectSchema()),
	}})
	operation.Responses.Set("400", &openapi3.ResponseRef{Value: &openapi3.Response{Description: stringPtr("Bad request")}})
	operation.Responses.Set("401", &openapi3.ResponseRef{Value: &openapi3.Response{Description: stringPtr("Unauthorized")}})
	operation.Responses.Set("403", &openapi3.ResponseRef{Value: &openapi3.Response{Description: stringPtr("Forbidden")}})
	operation.Responses.Set("404", &openapi3.ResponseRef{Value: &openapi3.Response{Description: stringPtr("Not found")}})
	operation.Responses.Set("500", &openapi3.ResponseRef{Value: &openapi3.Response{Description: stringPtr("Internal server error")}})
	// Add default response to catch all other cases
	operation.Responses.Set("default", &openapi3.ResponseRef{Value: &openapi3.Response{Description: stringPtr("Default response")}})

	return operation
}

// createUIOperation creates an operation for UI endpoints
func createUIOperation(operationID, serviceURI, upstreamPath, pathTranslation string) *openapi3.Operation {
	operation := &openapi3.Operation{
		OperationID: operationID,
		Parameters: []*openapi3.ParameterRef{
			{
				Value: &openapi3.Parameter{
					Name:     "proxy",
					In:       "path",
					Required: true,
					Schema:   &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
				},
			},
		},
		Responses: openapi3.NewResponses(),
		Extensions: map[string]interface{}{
			"x-google-backend": map[string]interface{}{
				"address":         serviceURI + upstreamPath,
				"pathTranslation": pathTranslation,
				"protocol":        "h2",
			},
		},
	}

	// Add responses with proper descriptions for v2 compatibility
	operation.Responses.Set("200", &openapi3.ResponseRef{Value: &openapi3.Response{
		Description: stringPtr("Successful response"),
		Content:     openapi3.NewContentWithJSONSchema(openapi3.NewObjectSchema()),
	}})
	operation.Responses.Set("404", &openapi3.ResponseRef{Value: &openapi3.Response{Description: stringPtr("Not found")}})
	operation.Responses.Set("default", &openapi3.ResponseRef{Value: &openapi3.Response{Description: stringPtr("Default response")}})

	return operation
}

// createCORSOperation creates an OPTIONS operation for CORS preflight requests
func createCORSOperation(operationID, serviceURI, upstreamPath, pathTranslation string) *openapi3.Operation {
	operation := &openapi3.Operation{
		OperationID: operationID,
		Parameters: []*openapi3.ParameterRef{
			{
				Value: &openapi3.Parameter{
					Name:     "proxy",
					In:       "path",
					Required: true,
					Schema:   &openapi3.SchemaRef{Value: openapi3.NewStringSchema()},
				},
			},
		},
		Responses: openapi3.NewResponses(),
		Extensions: map[string]interface{}{
			"x-google-backend": map[string]interface{}{
				"address":         serviceURI + upstreamPath,
				"pathTranslation": pathTranslation,
				"protocol":        "h2",
			},
		},
	}

	// Add responses for CORS preflight
	operation.Responses.Set("200", &openapi3.ResponseRef{Value: &openapi3.Response{Description: stringPtr("CORS preflight successful")}})
	operation.Responses.Set("default", &openapi3.ResponseRef{Value: &openapi3.Response{Description: stringPtr("Default response")}})

	return operation
}

// stringPtr returns a pointer to the given string
func stringPtr(s string) *string {
	return &s
}
