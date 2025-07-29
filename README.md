# pulumi-gcp-fullstack

[![Develop](https://github.com/davidmontoyago/pulumi-gcp-fullstack/actions/workflows/develop.yaml/badge.svg)](https://github.com/davidmontoyago/pulumi-gcp-fullstack/actions/workflows/develop.yaml)

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
- **BackendServiceURL**: Cloud Run backend service URL (automatically configured)
- **FrontendServiceURL**: Cloud Run frontend service URL (automatically configured)
- **EnableCORS**: Whether to enable CORS support (defaults to true)
- **CORSAllowedOrigins**: List of allowed origins for CORS (defaults to ["*"])
- **CORSAllowedMethods**: List of allowed HTTP methods for CORS (defaults to ["GET", "POST", "PUT", "DELETE", "OPTIONS"])
- **CORSAllowedHeaders**: List of allowed headers for CORS (defaults to ["*"])

Resource names are automatically generated using the backend service name as a base, ensuring proper prefixing and length limits.

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
