# pulumi-gcp-fullstack

[![Develop](https://github.com/davidmontoyago/pulumi-gcp-fullstack/actions/workflows/develop.yaml/badge.svg)](https://github.com/davidmontoyago/pulumi-gcp-fullstack/actions/workflows/develop.yaml)
[![Go Coverage](https://raw.githubusercontent.com/wiki/davidmontoyago/pulumi-gcp-fullstack/coverage.svg)](https://raw.githack.com/wiki/davidmontoyago/pulumi-gcp-fullstack/coverage.html)

Pulumi [Component](https://www.pulumi.com/docs/concepts/resources/components/#component-resources) to easily deploy a serverless fullstack app (frontend and backend) in GCP, and securely publish it to the internet.

Features:

1. A backend Cloud Run instance.
    - Env config loaded from Secret Manager
    - A companion Redis cache with auto-configured AuthN & TLS.
1. A frontend Cloud Run instance.
    - Env config loaded from Secret Manager
2. An regional or global HTTPs load balancer ([Classic Application Load Balancer](https://cloud.google.com/load-balancing/docs/https#global-classic-connections)), with an optional gateway before the frontend and backend instances (See: [Load Balancer Recipe](#load-balancer-recipe)).
    - A Google-managed certificate.
    - Optional: default best-practice Cloud Armor policy.
    - Optional: restrict access to an allowlist of IPs.

## Install

```
go get github.com/davidmontoyago/pulumi-gcp-fullstack
```

## Quickstart

### Basic Setup

```go
mystack, err = gcp.NewFullStack(ctx, "my-fullstack", &gcp.FullStackArgs{
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

### Full Config

```go
mystack, err := gcp.NewFullStack(ctx, "my-fullstack", &gcp.FullStackArgs{
    Project:       cfg.GCPProject,
    Region:        cfg.GCPRegion,
    BackendImage:  pulumi.String(cfg.BackendImage),
    FrontendImage: pulumi.String(cfg.FrontendImage),
    Network: &gcp.NetworkArgs{
        DomainURL:                cfg.DomainURL,
        EnableCloudArmor:         cfg.EnableCloudArmor,
        ClientIPAllowlist:        cfg.ClientIPAllowlist,
        EnablePrivateTrafficOnly: cfg.EnablePrivateTrafficOnly,
        EnableGlobalEntrypoint:   false,
        APIGateway: &gcp.APIGatewayArgs{
            // In GCP preview
            Disabled: true,
            Config: &gcp.APIConfigArgs{
                Backend: &gcp.Upstream{
                    APIPaths: []*gcp.APIPathArgs{
                        {
                            Path: "/api/v1",
                        },
                    },
                },
                Frontend: &gcp.Upstream{
                    APIPaths: []*gcp.APIPathArgs{
                        {
                            Path:         "/ui",
                            UpstreamPath: "/api/v1",
                        },
                    },
                },
            },
        },
    },
    Backend: &gcp.BackendArgs{
        InstanceArgs: &gcp.InstanceArgs{
            MaxInstanceCount:   3,
            DeletionProtection: false,
            ContainerPort:      9001,
            LivenessProbe: &gcp.Probe{
                Path:                "api/v1/healthz",
                InitialDelaySeconds: 60,
                PeriodSeconds:       10,
                TimeoutSeconds:      5,
                FailureThreshold:    3,
            },
            StartupProbe: &gcp.Probe{
                InitialDelaySeconds: 30,
                PeriodSeconds:       5,
                TimeoutSeconds:      3,
                FailureThreshold:    10,
            },
            EnvVars: map[string]string{
                "ENV":               "prod",
            },
        },
        CacheInstance: &gcp.CacheInstanceArgs{
            // Backend creds, certs, host and port will be auto-configured for the backend
            RedisVersion: "REDIS_7_0",
            Tier:         "BASIC",
            MemorySizeGb: 2,
            IpCidrRange:  "10.9.0.0/28",
        },
        ProjectIAMRoles: []string{
            "roles/pubsub.admin",
        },
    },
    Frontend: &gcp.FrontendArgs{
        InstanceArgs: &gcp.InstanceArgs{
            MaxInstanceCount:     3,
            SecretConfigFileName: ".env",
            SecretConfigFilePath: "/app/config/",
            DeletionProtection:   false,
            ContainerPort:        3000,
            LivenessProbe: &gcp.Probe{
                Path:                "api/v1/healthz",
                InitialDelaySeconds: 60,
                PeriodSeconds:       10,
                TimeoutSeconds:      5,
                FailureThreshold:    3,
            },
            StartupProbe: &gcp.Probe{
                InitialDelaySeconds: 30,
                PeriodSeconds:       5,
                TimeoutSeconds:      3,
                FailureThreshold:    10,
            },
        },
    },
    Labels: map[string]string{
        "environment": "production",
        "managed-by":  "pulumi",
    },
})
```

## Architecture

### Load Balancer

```
    [Global or Regional
      Forwarding Rule]
            |
            v
    [Target HTTPS Proxy]
            |
            v
        [URL Map]
            |
            v
     [Load Balancer
    Backend Services]
            |
            v
    [Serverless NEGs]
            |
            v
[API Gateway - Optional]
            |
            v
    [Cloud Run Services]

```

See:
- https://cloud.google.com/api-gateway/docs/gateway-load-balancing
- https://cloud.google.com/api-gateway/docs/gateway-serverless-neg


### General Topology

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
[Frontend] [Backend]
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

For detailed configuration options, resource naming conventions, and JWT authentication examples, see [Configuration Documentation](docs/configuration.md).
