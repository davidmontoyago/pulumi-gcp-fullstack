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
	CacheInstance   *CacheInstanceArgs
	BucketInstance  *BucketInstanceArgs
}

// FrontendArgs contains configuration for the frontend service.
type FrontendArgs struct {
	*InstanceArgs
}

// Probe contains configuration for TCP and HTTP health check probes
type Probe struct {
	Path string
	// Initial delay in seconds before the first health check. Defaults to 10 for TCP and 15 for HTTP.
	InitialDelaySeconds int
	// Period in seconds between health checks. Defaults to 2 for TCP and 5 for HTTP.
	PeriodSeconds int
	// Timeout in seconds for the health check. Defaults to 1 for TCP and 3 for HTTP.
	TimeoutSeconds int
	// Number of consecutive failures before considering the instance unhealthy. Defaults to 3.
	FailureThreshold int
	// Port to use for the health check.
	// Defaults to the container port for the ingress container and should be set for sidecar containers.
	Port int
}

// InstanceArgs contains configuration for Cloud Run service instances.
type InstanceArgs struct {
	ResourceLimits       pulumi.StringMap
	SecretConfigFileName string
	SecretConfigFilePath string
	EnvVars              map[string]string
	MaxInstanceCount     int
	DeletionProtection   bool
	ContainerPort        int
	StartupProbe         *Probe
	LivenessProbe        *Probe
	// Whether to enable public ingress. Defaults to false.
	// Set to true if EnableExternalWAF is true to allow the instance to be
	// reachable from the WAF proxy. Make sure to allow-list the WAF proxy IPs
	// at the application level to ensure only the WAF proxy can access the
	// instance.
	EnablePublicIngress bool
	// IDs of additional Secret Manager secrets to mount into the container. Defaults to nil.
	Secrets []*SecretVolumeArgs
	// Whether to enable startup CPU boost for faster cold starts. Defaults to false.
	StartupCPUBoost bool

	// Instance boot time SLO. Disabled if nil.
	ColdStartSLO *ColdStartSLOArgs

	// Sidecars to deploy with the instance. Defaults to nil.
	// Sidecars are private to the instance and are not exposed to the public internet.
	Sidecars []*SidecarArgs
}

// SidecarArgs contains configuration for a sidecar container to deploy with the instance.
// No container port is allowed for sidecar containers, as only the main container can
// have an ingress port.
type SidecarArgs struct {
	// Name of the sidecar container.
	Name string
	// Image of the sidecar container.
	Image string
	// Arguments to pass to the sidecar container.
	Args []string
	// Environment variables to set in the sidecar container.
	EnvVars map[string]string
	// Startup probe configuration for the sidecar container.
	StartupProbe *Probe
	// Liveness probe configuration for the sidecar container.
	LivenessProbe *Probe
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
	// Whether to secure the frontend and backend instances with an external WAF using Cloud Run Domain Mapping.
	// Set to true to disable the external load balancer. Defaults to false.
	EnableExternalWAF bool
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
	Version pulumi.StringInput
}

// CacheInstanceArgs contains configuration for Redis cache deployment.
type CacheInstanceArgs struct {
	RedisVersion string
	Tier         string
	MemorySizeGb int
	// Authorized network for the Redis instance, firewall and VPC connector. Defaults to "default".
	AuthorizedNetwork string
	// IP CIDR range for the private traffic VPC connector. Defaults to "10.8.0.0/28".
	ConnectorIPCidrRange string
	// Minimum number of instances for the VPC connector. Defaults to the lowest allowed value of 2.
	ConnectorMinInstances int
	// Maximum number of instances for the VPC connector. Defaults to the lowest allowed value of 3.
	ConnectorMaxInstances int
}

// BucketInstanceArgs contains configuration for Cloud Storage bucket deployment.
type BucketInstanceArgs struct {
	// Storage class for the bucket. Defaults to "STANDARD".
	StorageClass string
	// Location for the bucket. Defaults to "US".
	Location string
	// Number of days before objects are automatically deleted. Defaults to 365.
	RetentionDays int
	// Whether to force destroy the bucket even if it contains objects. Defaults to false.
	ForceDestroy bool
}

// ColdStartSLOArgs contains configuration for the container startup latency SLO.
type ColdStartSLOArgs struct {
	// Goal for the SLO. Defaults to 0.99, that is a 99% success rate.
	Goal pulumi.Float64Input
	// Max boot time acceptable for the instance in milliseconds. Defaults to 1000ms = 1 second.
	MaxBootTimeMs pulumi.Float64Input
	// Rolling period for the SLO in days. Defaults to 7 days.
	RollingPeriodDays pulumi.IntInput
	// Where to send the alert. Alerting is disabled if not set.
	AlertChannelID string
	// Alert burn rate threshold. Defaults to 0.1, that is alert if burn rate is > 10%.
	AlertBurnRateThreshold pulumi.Float64Input
	// Amount of time that a time series must be in violation to trigger an alert.
	// Defaults to 86400s, that is 1 day.
	AlertThresholdDuration pulumi.StringInput
}
