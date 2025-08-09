// Package gcp provides Google Cloud Platform infrastructure components for fullstack applications.
package gcp

import (
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// FullStackArgs contains configuration arguments for creating a FullStack instance.
type FullStackArgs struct {
	Project       string
	Region        string
	BackendName   string
	BackendImage  pulumi.StringInput
	FrontendName  string
	FrontendImage pulumi.StringInput
	// Optional additional config
	Backend  *BackendArgs
	Frontend *FrontendArgs
	Network  *NetworkArgs
	Labels   map[string]string
}

// BackendArgs contains configuration for the backend service.
type BackendArgs struct {
	*InstanceArgs
	ProjectIAMRoles []string
	CacheConfig     *CacheConfigArgs
}

// FrontendArgs contains configuration for the frontend service.
type FrontendArgs struct {
	*InstanceArgs
}

// Probe contains configuration for health check probes
type Probe struct {
	Path                string
	InitialDelaySeconds int
	PeriodSeconds       int
	TimeoutSeconds      int
	FailureThreshold    int
}

// InstanceArgs contains configuration for Cloud Run service instances.
type InstanceArgs struct {
	ResourceLimits        pulumi.StringMap
	SecretConfigFileName  string
	SecretConfigFilePath  string
	EnvVars               map[string]string
	MaxInstanceCount      int
	DeletionProtection    bool
	ContainerPort         int
	StartupProbe          *Probe
	LivenessProbe         *Probe
	EnableUnauthenticated bool
	// Optional VPC access connector for private traffic.
	// E.g.: to a memorystore instance
	PrivateVpcAccessConnector pulumi.StringInput
	// IDs of additional Secret Manager secrets to mount into the container. Defaults to nil.
	Secrets []SecretVolumeArgs
}

// NetworkArgs contains configuration for network infrastructure including load balancers and API Gateway.
type NetworkArgs struct {
	// Domain name for the internet-facing certificate. Required.
	// E.g.: "myapp.path2prod.dev"
	DomainURL string
	// GCP network where to host the load balancer instances. Defaults to "default".
	ProxyNetworkName string
	// Whether to apply best-practice Cloud Armor policies to the load balancer. Defaults to false.
	EnableCloudArmor bool
	// Whether to enable Identity Aware Proxy for authentication. Defaults to false.
	EnableIAP bool
	// Support email for IAP's OAuth consent screen. Required if EnableIAP=true.
	IAPSupportEmail string
	// Whether to restrict access to the given list of client IPs. Valid only when EnableCloudArmor=true.
	ClientIPAllowlist []string
	// Whether to disable public internet access. Useful during development. Defaults to false.
	EnablePrivateTrafficOnly bool
	// API Gateway configuration. If provided, traffic will be routed through API Gateway.
	APIGateway *APIGatewayArgs
	// Whether to enable global forwarding rule and global IP address for the load balancer.
	EnableGlobalEntrypoint bool
}

// APIGatewayArgs contains configuration for Google API Gateway
type APIGatewayArgs struct {
	// Name of the API Gateway and its resources. Defaults to "gateway".
	Name string
	// API Gateway configuration. Required when enabled.
	Config *APIConfigArgs
	// Whether to disable API Gateway. Defaults to false.
	Disabled bool
	// List of regions where to deploy API Gateway instances.
	Regions []string
}

// APIPathArgs contains configuration for API Gateway API paths
type APIPathArgs struct {
	// Path to match in the public API. Defaults to "/api/v1".
	Path string
	// Optional. If not set, defaults to Path.
	UpstreamPath string
}

// JWTAuth contains JWT authentication configuration for upstream services
type JWTAuth struct {
	// JWT issuer (iss claim). Required for JWT validation.
	Issuer string
	// JWKS URI for JWT token validation. Required for JWT validation.
	JwksURI string
}

// Upstream contains configuration for an upstream service
type Upstream struct {
	// Service URL for the upstream service.
	ServiceURL pulumi.StringOutput
	// API paths configuration for the upstream service.
	APIPaths []*APIPathArgs
	// JWT authentication configuration. Optional.
	JWTAuth *JWTAuth
}

// APIConfigArgs contains configuration for API Gateway API Config
type APIConfigArgs struct {
	// Backend upstream configuration.
	Backend *Upstream
	// Frontend upstream configuration.
	Frontend *Upstream
	// Whether to enable CORS. Defaults to true.
	EnableCORS bool
	// CORS allowed origins. Defaults to ["*"].
	CORSAllowedOrigins []string
	// CORS allowed methods. Defaults to ["GET", "POST", "PUT", "DELETE", "OPTIONS"].
	CORSAllowedMethods []string
	// CORS allowed headers. Defaults to ["*"].
	CORSAllowedHeaders []string
	// OpenAPI specification file path. Optional - defaults to "/openapi.yaml".
	OpenAPISpecPath string
}

// SecretVolumeArgs contains configuration for mounting a secret as a volume.
type SecretVolumeArgs struct {
	// SecretID is the ID of the secret (can be secret name or secret ID reference).
	SecretID pulumi.StringInput
	// Name for the volume mount.
	Name string
	// Path to mount the secret in the container.
	Path string
	// Secret name. Defaults to ".env".
	SecretName string
	// Version of the secret to mount. Defaults to "latest".
	Version string
}

// CacheConfigArgs contains configuration for Redis cache deployment.
type CacheConfigArgs struct {
	RedisVersion string
	Tier         string
	MemorySizeGb int
	// Authorized network for the Redis instance, firewall and VPC connector. Defaults to "default".
	AuthorizedNetwork string
}
