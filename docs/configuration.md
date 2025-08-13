# Config Options



## APIGatewayArgs
- **Disabled**: Boolean to enable/disable API Gateway deployment (defaults to false)
- **Regions**: List of regions where to deploy API Gateway instances (defaults to the project region)
- **Config**: API configuration including CORS settings and backend routing

## APIConfigArgs
- **OpenAPISpecPath**: Path to OpenAPI specification file (defaults to "/openapi.yaml")
- **Backend**: Backend upstream configuration
- **Frontend**: Frontend upstream configuration
- **EnableCORS**: Whether to enable CORS support (defaults to true)
- **CORSAllowedOrigins**: List of allowed origins for CORS (defaults to ["*"])
- **CORSAllowedMethods**: List of allowed HTTP methods for CORS (defaults to ["GET", "POST", "PUT", "DELETE", "OPTIONS"])
- **CORSAllowedHeaders**: List of allowed headers for CORS (defaults to ["*"])

## Upstream
- **ServiceURL**: Cloud Run service URL (automatically configured)
- **APIPaths**: List of API path configurations
- **JWTAuth**: JWT authentication configuration (optional)

## APIPathArgs
- **Path**: Path to match in the public API (e.g., "/api/v1")
- **UpstreamPath**: Optional upstream path (defaults to Path if not specified)

## JWTAuth
- **Issuer**: JWT issuer (iss claim) for token validation (automatically set to frontend service account email)
- **JwksURI**: JWKS URI for JWT token validation (automatically set to frontend service account JWKS endpoint)

**Note**: JWT authentication is designed for service-to-service authentication where only the frontend service account can access the backend API. This is not for user authentication.

## CacheInstanceArgs
- **RedisVersion**: Redis version to deploy (defaults to "REDIS_7_0")
- **Tier**: Redis tier - "BASIC" or "STANDARD_HA" (defaults to "BASIC")
- **MemorySizeGb**: Memory size in GB for the Redis instance (defaults to 1)
- **AuthorizedNetwork**: VPC network for Redis access (defaults to "default")
- **ConnectorIPCidrRange**: IP CIDR range for VPC connector (defaults to "10.8.0.0/28")

**Note**: Cache instances are automatically configured with auth and TLS enabled. Backend services get VPC access and cache credentials mounted at `/app/cache-config/.env`.

Resource names are automatically generated using the backend service name as a base, ensuring proper prefixing and length limits.

## JWT Authentication Example

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

## Cache Configuration Example

To enable a Redis cache for the backend service:

```go
fullstack, err := gcp.NewFullStack(ctx, "my-stack", &gcp.FullStackArgs{
    Project:       "my-project",
    Region:        "us-central1",
    BackendName:   "backend",
    BackendImage:  pulumi.String("gcr.io/my-project/backend:latest"),
    FrontendName:  "frontend",
    FrontendImage: pulumi.String("gcr.io/my-project/frontend:latest"),
    Backend: &gcp.BackendArgs{
        CacheInstance: &gcp.CacheInstanceArgs{
            RedisVersion: "REDIS_7_0",
            Tier:         "BASIC",
            MemorySizeGb: 2,
        },
    },
})
```

When cache is enabled:
- Redis instance is deployed with private IP and auth/TLS enabled
- VPC connector allows backend to reach Redis privately
- Cache credentials are automatically mounted at `/app/cache-config/.env`

### Environment Variables

| Variable             | Description                          |
| -------------------- | ------------------------------------ |
| `REDIS_HOST`         | Redis instance primary endpoint      |
| `REDIS_PORT`         | Redis instance port (6378 for TLS  ) |
| `REDIS_READ_HOST`    | Redis read replica endpoint          |
| `REDIS_READ_PORT`    | Redis read replica port              |
| `REDIS_AUTH_STRING`  | Redis authentication password        |
| `REDIS_TLS_CA_CERTS` | Base64-encoded TLS CA certificates   |

**Note**: `REDIS_TLS_CA_CERTS` requires base64 decoding:

```go
caCerts, err := base64.StdEncoding.DecodeString(caCertsB64)
if err != nil {
    slog.Error("Failed to base64 decode CA certificates", "error", err)
    return nil
}

caCertsPool := x509.NewCertPool()
caCertsPool.AppendCertsFromPEM(caCerts)

tlsConfig := &tls.Config{
    MinVersion: tls.VersionTLS12,
    RootCAs:    caCertsPool,
}

client := redis.NewClient(&redis.Options{
    Addr:      addr,
    Password:  auth,
    DB:        0,
    TLSConfig: tlsConfig,
})
```

# Resource Naming Convention

The component automatically generates resource names following a consistent pattern to ensure uniqueness and compliance with GCP naming requirements:

## Base Naming Pattern
- **Prefix**: Uses the stack name (e.g., my-gcp-stack") as the base prefix
- **Service Suffix**: Appends service-specific suffixes like `-backend`, `-frontend`, `-gateway`
- **Length Limits**: Automatically truncates names to comply with GCP resource name length limits

## Examples
For a stack named `my-gcp-stack"`:
- Cloud Run Backend: `my-gcp-stack-backend`
- Cloud Run Frontend: `my-gcp-stack-frontend`
- API Gateway: `my-gcp-stack-gateway`
- Load Balancer: `my-gcp-stack-lb`
- Secret Manager: `my-gcp-stack-secrets`

## Network Resources
Network-related resources follow the same pattern:
- Firewall Rules: `my-gcp-stack-fw-*`
- Cloud Armor Policy: `my-gcp-stack-armor`

## Customization
Resource names can be customized by modifying the stack name parameter in `NewFullStack()`. The component ensures all generated names are unique within the project and region.
