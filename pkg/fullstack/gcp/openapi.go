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
	paths := openapi3.Paths{}

	// Use simple catch-all paths that defer to upstream services
	paths["/api/{proxy}"] = createAPIPathItem(backendServiceURI)
	paths["/ui/{proxy}"] = createUIPathItem(frontendServiceURI)

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
func createAPIPathItem(backendServiceURI string) *openapi3.PathItem {
	return &openapi3.PathItem{
		Get:     createAPIOperation("apiProxyGet", "get", backendServiceURI),
		Post:    createAPIOperation("apiProxyPost", "post", backendServiceURI),
		Put:     createAPIOperation("apiProxyPut", "put", backendServiceURI),
		Delete:  createAPIOperation("apiProxyDelete", "delete", backendServiceURI),
		Options: createCORSOperation("apiProxyOptions", backendServiceURI),
	}
}

// createUIPathItem creates a PathItem for UI routes with GET and OPTIONS methods
func createUIPathItem(frontendServiceURI string) *openapi3.PathItem {
	return &openapi3.PathItem{
		Get:     createUIOperation("uiProxyGet", frontendServiceURI),
		Options: createCORSOperation("uiProxyOptions", frontendServiceURI),
	}
}

// createAPIOperation creates an operation for API endpoints
func createAPIOperation(operationID, method, serviceURI string) *openapi3.Operation {
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
							Type: openapi3.TypeString,
						},
					},
				},
			},
		},
		Responses: openapi3.NewResponses(),
		Extensions: map[string]interface{}{
			"x-google-backend": map[string]interface{}{
				"address": serviceURI + "/{proxy}",
			},
		},
	}

	// Add request body for POST and PUT operations
	if method == "post" || method == "put" {
		operation.RequestBody = &openapi3.RequestBodyRef{
			Value: &openapi3.RequestBody{
				Required: false,
				Content: openapi3.NewContentWithJSONSchema(&openapi3.Schema{
					Type: openapi3.TypeObject,
				}),
			},
		}
	}

	// Add responses with proper descriptions for v2 compatibility
	operation.Responses["200"] = &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: stringPtr("Successful response"),
			Content: openapi3.NewContentWithJSONSchema(&openapi3.Schema{
				Type: openapi3.TypeObject,
			}),
		},
	}
	operation.Responses["400"] = &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: stringPtr("Bad request"),
		},
	}
	operation.Responses["401"] = &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: stringPtr("Unauthorized"),
		},
	}
	operation.Responses["403"] = &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: stringPtr("Forbidden"),
		},
	}
	operation.Responses["404"] = &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: stringPtr("Not found"),
		},
	}
	operation.Responses["500"] = &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: stringPtr("Internal server error"),
		},
	}
	// Add default response to catch all other cases
	operation.Responses["default"] = &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: stringPtr("Default response"),
		},
	}

	return operation
}

// createUIOperation creates an operation for UI endpoints
func createUIOperation(operationID, serviceURI string) *openapi3.Operation {
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
							Type: openapi3.TypeString,
						},
					},
				},
			},
		},
		Responses: openapi3.NewResponses(),
		Extensions: map[string]interface{}{
			"x-google-backend": map[string]interface{}{
				"address": serviceURI + "/{proxy}",
			},
		},
	}

	// Add responses with proper descriptions for v2 compatibility
	operation.Responses["200"] = &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: stringPtr("Successful response"),
			Content: openapi3.NewContentWithJSONSchema(&openapi3.Schema{
				Type: openapi3.TypeObject,
			}),
		},
	}
	operation.Responses["404"] = &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: stringPtr("Not found"),
		},
	}
	operation.Responses["default"] = &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: stringPtr("Default response"),
		},
	}

	return operation
}

// createCORSOperation creates an OPTIONS operation for CORS preflight requests
func createCORSOperation(operationID, serviceURI string) *openapi3.Operation {
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
							Type: openapi3.TypeString,
						},
					},
				},
			},
		},
		Responses: openapi3.NewResponses(),
		Extensions: map[string]interface{}{
			"x-google-backend": map[string]interface{}{
				"address": serviceURI + "/{proxy}",
			},
		},
	}

	// Add responses for CORS preflight
	operation.Responses["200"] = &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: stringPtr("CORS preflight successful"),
		},
	}
	operation.Responses["default"] = &openapi3.ResponseRef{
		Value: &openapi3.Response{
			Description: stringPtr("Default response"),
		},
	}

	return operation
}
