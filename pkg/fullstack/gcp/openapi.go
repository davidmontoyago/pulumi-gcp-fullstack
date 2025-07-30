package gcp

import (
	"github.com/getkin/kin-openapi/openapi3"
)

// stringPtr returns a pointer to the given string
func stringPtr(s string) *string {
	return &s
}

// newOpenAPISpec creates a new OpenAPI 3.0.1 specification for API Gateway
// that routes traffic to Cloud Run backend and frontend services.
func newOpenAPISpec(backendServiceURI, frontendServiceURI string, configArgs *APIConfigArgs) *openapi3.T {
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
		Paths: func() *openapi3.Paths {
			paths := &openapi3.Paths{}
			paths.Set("/api/{proxy+}", createAPIPathItem(backendServiceURI))
			paths.Set("/ui/{proxy+}", createUIPathItem(frontendServiceURI))

			return paths
		}(),
		Components: &openapi3.Components{
			SecuritySchemes: make(openapi3.SecuritySchemes),
		},
	}

	// Add CORS configuration if enabled
	if configArgs != nil && configArgs.EnableCORS {
		spec.Extensions = make(map[string]interface{})
		spec.Extensions["x-google-cors"] = createCORSConfig(configArgs)
	}

	return spec
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
func createAPIPathItem(serviceURI string) *openapi3.PathItem {
	return &openapi3.PathItem{
		Get:     createAPIOperation("apiProxyGet", "get"),
		Post:    createAPIOperation("apiProxyPost", "post"),
		Put:     createAPIOperation("apiProxyPut", "put"),
		Delete:  createAPIOperation("apiProxyDelete", "delete"),
		Options: createCORSOperation("apiProxyOptions"),
		Extensions: map[string]interface{}{
			"x-google-backend": map[string]interface{}{
				"address": serviceURI + "/{proxy}",
			},
		},
	}
}

// createUIPathItem creates a PathItem for UI routes with GET and OPTIONS methods
func createUIPathItem(frontendServiceURI string) *openapi3.PathItem {
	return &openapi3.PathItem{
		Get:     createUIOperation("uiProxyGet"),
		Options: createCORSOperation("uiProxyOptions"),
		Extensions: map[string]interface{}{
			"x-google-backend": map[string]interface{}{
				"address": frontendServiceURI + "/{proxy}",
			},
		},
	}
}

// createAPIOperation creates an operation for API endpoints
func createAPIOperation(operationID, method string) *openapi3.Operation {
	operation := &openapi3.Operation{
		OperationID: operationID,
		Parameters: []*openapi3.ParameterRef{
			{
				Value: &openapi3.Parameter{
					Name:     "proxy",
					In:       "path",
					Required: true,
					Schema: &openapi3.SchemaRef{
						Value: &openapi3.Schema{
							Type: &openapi3.Types{"string"},
						},
					},
				},
			},
		},
		Responses: openapi3.NewResponses(),
	}

	// Add request body for POST and PUT operations
	if method == "post" || method == "put" {
		operation.RequestBody = &openapi3.RequestBodyRef{
			Value: &openapi3.RequestBody{
				Required: false,
				Content: openapi3.NewContentWithJSONSchema(&openapi3.Schema{
					Type: &openapi3.Types{"object"},
				}),
			},
		}
	}

	// Add responses
	operation.Responses.Set("200", &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: stringPtr("Successful response"),
			Content: openapi3.NewContentWithJSONSchema(&openapi3.Schema{
				Type: &openapi3.Types{"object"},
			}),
		},
	})
	operation.Responses.Set("404", &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: stringPtr("Not found"),
		},
	})

	return operation
}

// createUIOperation creates an operation for UI endpoints
func createUIOperation(operationID string) *openapi3.Operation {
	return &openapi3.Operation{
		OperationID: operationID,
		Parameters: []*openapi3.ParameterRef{
			{
				Value: &openapi3.Parameter{
					Name:     "proxy",
					In:       "path",
					Required: true,
					Schema: &openapi3.SchemaRef{
						Value: &openapi3.Schema{
							Type: &openapi3.Types{"string"},
						},
					},
				},
			},
		},
		Responses: openapi3.NewResponses(),
	}
}

// createCORSOperation creates an OPTIONS operation for CORS preflight requests
func createCORSOperation(operationID string) *openapi3.Operation {
	return &openapi3.Operation{
		OperationID: operationID,
		Parameters: []*openapi3.ParameterRef{
			{
				Value: &openapi3.Parameter{
					Name:     "proxy",
					In:       "path",
					Required: true,
					Schema: &openapi3.SchemaRef{
						Value: &openapi3.Schema{
							Type: &openapi3.Types{"string"},
						},
					},
				},
			},
		},
		Responses: openapi3.NewResponses(),
	}
}
