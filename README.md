# pulumi-gcp-fullstack

[![Develop](https://github.com/davidmontoyago/pulumi-gcp-fullstack/actions/workflows/develop.yaml/badge.svg)](https://github.com/davidmontoyago/pulumi-gcp-fullstack/actions/workflows/develop.yaml)
[![Go Coverage](https://github.com/davidmontoyago/pulumi-gcp-pullstack/wiki/coverage.svg)](https://raw.githack.com/wiki/davidmontoyago/pulumi-gcp-fullstack/coverage.html)

Pulumi [Component](https://www.pulumi.com/docs/concepts/resources/components/#component-resources) to easily deploy a serverless fullstack app (frontend and backend) in GCP, and securely publish it to the internet.

Features:

1. A backend Cloud Run instance.
    - Env config loaded from Secret Manager
1. A frontend Cloud Run instance.
    - Env config loaded from Secret Manager
1. An global HTTPs load balancer ([Classic Application Load Balancer](https://cloud.google.com/load-balancing/docs/https#global-classic-connections)) pointed to a gateway, and the gateway pointed to the frontend and the backend.
    - A Google-managed certificate.
    - Optional: default best-practice Cloud Armor policy.
    - Optional: restrict access to an allowlist of IPs.

## Install

```
go get github.com/davidmontoyago/pulumi-gcp-fullstack
```

## Quickstart

### Basic Setup

```
mystack, err = gcp.NewFullStack(ctx, "my-gcp-stack", &gcp.FullStackArgs{
    Project:       "my-gcp-project",
    Region:        "us-central1",
    BackendImage:  "us-docker.pkg.dev/my-gcp-project/my-app/backend",
    FrontendImage: "us-docker.pkg.dev/my-gcp-project/my-app/frontend",
    Network: &gcp.NetworkArgs{
        DomainURL:        "myapp.example.org",
        EnableCloudArmor: true,
    },
})
```

### With API Gateway Integration

```
mystack, err = gcp.NewFullStack(ctx, "my-gcp-stack", &gcp.FullStackArgs{
    Project:       "my-gcp-project",
    Region:        "us-central1",
    BackendImage:  "us-docker.pkg.dev/my-gcp-project/my-app/backend",
    FrontendImage: "us-docker.pkg.dev/my-gcp-project/my-app/frontend",
    Network: &gcp.NetworkArgs{
        DomainURL:        "myapp.example.org",
        EnableCloudArmor: true,
        APIGateway: &gcp.APIGatewayArgs{
            Disabled: false,
            Regions: []string{"us-central1", "us-east1"},
            Config: &gcp.APIConfigArgs{
                EnableCORS:      true,
                CORSAllowedOrigins: []string{"https://myapp.example.org"},
                CORSAllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
                CORSAllowedHeaders: []string{"*"},
            },
        },
    },
})
```

## Architecture

### Load Balancer Recipe

```
[Forwarding Rule]
        |
        v
[Target HTTPS Proxy]
        |
        v
    [URL Map]
        |
        v
[Backend Service]
        |
        v
 [Serverless NEG]
        |
        v
   [API Gateway]
        |
        v
[Cloud Run Service]

```

See:
- https://cloud.google.com/api-gateway/docs/gateway-load-balancing

### Topology

```
     [Internet]
          |
          v
[GCP HTTPS Load Balancer]
          |
          v
      [Gateway]
          |
          v
  [Frontend & Backend]
          |
          v
   [Cloud Resources]
```

## API Gateway Integration

When API Gateway is configured, the load balancer routes traffic through the API Gateway instead of directly to the Cloud Run backend. This provides:

- **API Management**: Centralized API definition and configuration
- **Traffic Control**: Rate limiting, authentication, and authorization
- **CORS Support**: Built-in CORS handling for web applications
- **Backend Routing**: Automatic routing to Cloud Run services

The API Gateway uses a Serverless NEG (Network Endpoint Group) to integrate with the load balancer, following Google Cloud best practices.

### Configuration Options

#### APIGatewayArgs
- **Disabled**: Boolean to enable/disable API Gateway deployment (defaults to false)
- **Regions**: List of regions where to deploy API Gateway instances (defaults to the project region)
- **Config**: API configuration including CORS settings and backend routing

#### APIConfigArgs
- **OpenAPISpecPath**: Path to OpenAPI specification file (defaults to "/openapi.yaml")
- **Backend**: Backend upstream configuration
- **Frontend**: Frontend upstream configuration
- **EnableCORS**: Whether to enable CORS support (defaults to true)
- **CORSAllowedOrigins**: List of allowed origins for CORS (defaults to ["*"])
- **CORSAllowedMethods**: List of allowed HTTP methods for CORS (defaults to ["GET", "POST", "PUT", "DELETE", "OPTIONS"])
- **CORSAllowedHeaders**: List of allowed headers for CORS (defaults to ["*"])

#### Upstream
- **ServiceURL**: Cloud Run service URL (automatically configured)
- **APIPaths**: List of API path configurations
- **JWTAuth**: JWT authentication configuration (optional)

#### APIPathArgs
- **Path**: Path to match in the public API (e.g., "/api/v1")
- **UpstreamPath**: Optional upstream path (defaults to Path if not specified)

#### JWTAuth
- **Issuer**: JWT issuer (iss claim) for token validation (automatically set to frontend service account email)
- **JwksURI**: JWKS URI for JWT token validation (automatically set to frontend service account JWKS endpoint)

**Note**: JWT authentication is designed for service-to-service authentication where only the frontend service account can access the backend API. This is not for user authentication.

Resource names are automatically generated using the backend service name as a base, ensuring proper prefixing and length limits.

### JWT Authentication Example

To enable JWT authentication for service-to-service communication between frontend and backend:

```go
fullstack, err := gcp.NewFullStack(ctx, "my-stack", &gcp.FullStackArgs{
    Project:       "my-project",
    Region:        "us-central1",
    BackendName:   "backend",
    BackendImage:  pulumi.String("gcr.io/my-project/backend:latest"),
    FrontendName:  "frontend",
    FrontendImage: pulumi.String("gcr.io/my-project/frontend:latest"),
    Network: &gcp.NetworkArgs{
        DomainURL: "myapp.example.com",
        APIGateway: &gcp.APIGatewayArgs{
            Config: &gcp.APIConfigArgs{
                Backend: &gcp.Upstream{
                    JWTAuth: &gcp.JWTAuth{
                        // Issuer and JwksURI will be automatically configured
                        // with the frontend service account credentials
                    },
                },
            },
        },
    },
})
```

When JWT authentication is enabled:
- The frontend service account email is automatically set as the JWT issuer
- The frontend service account JWKS URI is automatically configured
- All backend API endpoints require a valid JWT token from the frontend service account
- The frontend can generate JWT tokens using its service account credentials
- Only requests with valid JWT tokens from the frontend service account can access the backend API

## Resource Naming Convention

The component automatically generates resource names following a consistent pattern to ensure uniqueness and compliance with GCP naming requirements:

### Base Naming Pattern
- **Prefix**: Uses the stack name (e.g., my-gcp-stack") as the base prefix
- **Service Suffix**: Appends service-specific suffixes like `-backend`, `-frontend`, `-gateway`
- **Length Limits**: Automatically truncates names to comply with GCP resource name length limits

### Examples
For a stack named `my-gcp-stack"`:
- Cloud Run Backend: `my-gcp-stack-backend`
- Cloud Run Frontend: `my-gcp-stack-frontend`
- API Gateway: `my-gcp-stack-gateway`
- Load Balancer: `my-gcp-stack-lb`
- Secret Manager: `my-gcp-stack-secrets`

### Network Resources
Network-related resources follow the same pattern:
- Firewall Rules: `my-gcp-stack-fw-*`
- Cloud Armor Policy: `my-gcp-stack-armor`

### Customization
Resource names can be customized by modifying the stack name parameter in `NewFullStack()`. The component ensures all generated names are unique within the project and region.

See:
- https://cloud.google.com/api-gateway/docs/gateway-serverless-neg
