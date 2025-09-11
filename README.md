# pulumi-gcp-fullstack

[![Develop](https://github.com/davidmontoyago/pulumi-gcp-fullstack/actions/workflows/develop.yaml/badge.svg)](https://github.com/davidmontoyago/pulumi-gcp-fullstack/actions/workflows/develop.yaml) [![Go Coverage](https://raw.githubusercontent.com/wiki/davidmontoyago/pulumi-gcp-fullstack/coverage.svg)](https://raw.githack.com/wiki/davidmontoyago/pulumi-gcp-fullstack/coverage.html) [![Go Reference](https://pkg.go.dev/badge/github.com/davidmontoyago/pulumi-gcp-fullstack.svg)](https://pkg.go.dev/github.com/davidmontoyago/pulumi-gcp-fullstack)


Pulumi [Component](https://www.pulumi.com/docs/concepts/resources/components/#component-resources) to easily deploy a serverless fullstack app (frontend and backend) in GCP, and securely publish it to the internet.

Features:

1. A backend Cloud Run instance.
    - Env config loaded from Secret Manager
    - An optional companion Redis cache with auto-configured AuthN & TLS.
    - An optional companion Bucket with auto-configured IAM.
    - Optional cold start SLO monitoring and alerting.
1. A frontend Cloud Run instance.
    - Env config loaded from Secret Manager
    - Optional cold start SLO monitoring and alerting.
2. An regional or global HTTPs load balancer ([Classic Application Load Balancer](https://cloud.google.com/load-balancing/docs/https#global-classic-connections)), with an optional gateway before the frontend and backend instances (See: [Load Balancer Recipe](#load-balancer-recipe)).
    - A Google-managed certificate.
    - Optional: default best-practice Cloud Armor policy.
    - Optional: restrict access to an allowlist of IPs.
    - Optional: disable the load balancer all together and secure with an external WAF like [cloudflare](https://github.com/davidmontoyago/pulumi-cloudflare-free-edge-protection).

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
    BackendName:   "backend",
    BackendImage:  pulumi.String(cfg.BackendImage),
    FrontendName:  "frontend",
    FrontendImage: pulumi.String(cfg.FrontendImage),
    Network: &gcp.NetworkArgs{
        DomainURL:                cfg.DomainURL,
        EnableCloudArmor:         cfg.EnableCloudArmor,
        ClientIPAllowlist:        cfg.ClientIPAllowlist,
        EnablePrivateTrafficOnly: cfg.EnablePrivateTrafficOnly,
        EnableGlobalEntrypoint:   false,
        EnableExternalWAF:        false,
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
            MaxInstanceCount:    3,
            DeletionProtection:  false,
            ContainerPort:       9001,
            StartupCPUBoost:     true,
            EnablePublicIngress: false, // Set to true if EnableExternalWAF is true
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
                "ENV": "prod",
            },
            // Cold start SLO configuration
            ColdStartSLO: &gcp.ColdStartSLOArgs{
                Goal:                   pulumi.Float64(0.99),    // 99% success rate
                MaxBootTimeMs:          pulumi.Float64(1000),    // 1 second max boot time
                RollingPeriodDays:      pulumi.Int(7),           // 7 day rolling window
                AlertChannelID:         "projects/my-project/notificationChannels/123",
                AlertBurnRateThreshold: pulumi.Float64(0.1),     // Alert if burn rate > 10%
                AlertThresholdDuration: pulumi.String("86400s"), // 1 day threshold
            },
            // Secret volume mounts
            Secrets: []*gcp.SecretVolumeArgs{
                {
                    SecretID:   pulumi.String("my-app-secrets"),
                    Name:       "app-secrets",
                    Path:       "/etc/secrets",
                    SecretName: "config.json",
                    Version:    pulumi.String("latest"),
                },
            },
        },
        CacheInstance: &gcp.CacheInstanceArgs{
            // Backend creds, certs, host and port will be auto-configured for the backend
            RedisVersion:             "REDIS_7_0",
            Tier:                     "BASIC",
            MemorySizeGb:             2,
            AuthorizedNetwork:        "default",
            ConnectorIPCidrRange:     "10.8.0.0/28",
            ConnectorMinInstances:    2,
            ConnectorMaxInstances:    3,
        },
        BucketInstance: &gcp.BucketInstanceArgs{
            StorageClass:  "STANDARD",
            Location:      "US",
            RetentionDays: 365,
            ForceDestroy:  false,
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
            StartupCPUBoost:      true,
            EnablePublicIngress:  false, // Set to true if EnableExternalWAF is true
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
            // Cold start SLO configuration for frontend
            ColdStartSLO: &gcp.ColdStartSLOArgs{
                Goal:              pulumi.Float64(0.95),    // 95% success rate for frontend
                MaxBootTimeMs:     pulumi.Float64(2000),    // 2 seconds max boot time
                RollingPeriodDays: pulumi.Int(7),           // 7 day rolling window
                AlertChannelID:    "projects/my-project/notificationChannels/123",
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
