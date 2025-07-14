# pulumi-gcp-fullstack

Pulumi [Component](https://www.pulumi.com/docs/concepts/resources/components/#component-resources) to easily deploy a serverless fullstack app (frontend and backend) in GCP, and securely publish it to the internet.

Includes:

1. A backend Cloud Run instance.
    - Env config loaded from Secret Manager
1. A frontend Cloud Run instance.
    - Env config loaded from Secret Manager
1. An global HTTPs load balancer ([Classic Application Load Balancer](https://cloud.google.com/load-balancing/docs/https#global-classic-connections)) to control access to the frontend (and the backend).
    - A Google-managed certificate.
    - Optional: default best-practice Cloud Armor policy.
    - Optional: restrict access to an allowlist of IPs.
    - Optional: Google API Gateway integration for backend traffic routing.

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
            Enabled: true,
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

## API Gateway Integration

When API Gateway is configured, the load balancer routes traffic through the API Gateway instead of directly to the Cloud Run backend. This provides:

- **API Management**: Centralized API definition and configuration
- **Traffic Control**: Rate limiting, authentication, and authorization
- **CORS Support**: Built-in CORS handling for web applications
- **Backend Routing**: Automatic routing to Cloud Run services

The API Gateway uses a Serverless NEG (Network Endpoint Group) to integrate with the load balancer, following Google Cloud best practices.

### Configuration Options

- **Enabled**: Boolean to enable/disable API Gateway deployment
- **Regions**: List of regions where to deploy API Gateway instances
- **Config**: API configuration including CORS settings and backend routing

Resource names are automatically generated using the backend service name as a base, ensuring proper prefixing and length limits.

See:
- https://cloud.google.com/api-gateway/docs/gateway-serverless-neg
