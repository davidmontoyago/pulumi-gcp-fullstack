// Package gcp provides Google Cloud Platform infrastructure components for fullstack applications.
package gcp

import (
	apigateway "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/apigateway"
	cloudrunv2 "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudrunv2"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/dns"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// FullStack represents a complete fullstack application infrastructure on Google Cloud Platform.
type FullStack struct {
	pulumi.ResourceState

	Project       string
	Region        string
	BackendName   string
	BackendImage  pulumi.StringOutput
	FrontendName  string
	FrontendImage pulumi.StringOutput
	Labels        map[string]string

	name           string
	gatewayEnabled bool

	backendService  *cloudrunv2.Service
	backendAccount  *serviceaccount.Account
	frontendService *cloudrunv2.Service
	frontendAccount *serviceaccount.Account

	// IAM members for API Gateway invoker permissions
	gatewayServiceAccount    *serviceaccount.Account
	backendGatewayIamMember  *cloudrunv2.ServiceIamMember
	frontendGatewayIamMember *cloudrunv2.ServiceIamMember

	// Network infrastructure
	apiGateway *apigateway.Gateway
	apiConfig  *apigateway.ApiConfig
	// The NEG used when API Gateway is enabled
	apiGatewayNeg *compute.RegionNetworkEndpointGroup
	// The NEGs used when API Gateway is disabled
	backendNeg  *compute.RegionNetworkEndpointGroup
	frontendNeg *compute.RegionNetworkEndpointGroup

	globalForwardingRule *compute.GlobalForwardingRule

	certificate *compute.ManagedSslCertificate
	dnsRecord   *dns.RecordSet
	urlMap      *compute.URLMap
}

// GetBackendService returns the backend Cloud Run service.
func (f *FullStack) GetBackendService() *cloudrunv2.Service {
	return f.backendService
}

// GetFrontendService returns the frontend Cloud Run service.
func (f *FullStack) GetFrontendService() *cloudrunv2.Service {
	return f.frontendService
}

// GetAPIGateway returns the API Gateway instance.
func (f *FullStack) GetAPIGateway() *apigateway.Gateway {
	return f.apiGateway
}

// GetAPIConfig returns the API Gateway configuration.
func (f *FullStack) GetAPIConfig() *apigateway.ApiConfig {
	return f.apiConfig
}

// GetBackendGatewayIamMember returns the backend service IAM member for API Gateway invoker permissions.
func (f *FullStack) GetBackendGatewayIamMember() *cloudrunv2.ServiceIamMember {
	return f.backendGatewayIamMember
}

// GetFrontendGatewayIamMember returns the frontend service IAM member for API Gateway invoker permissions.
func (f *FullStack) GetFrontendGatewayIamMember() *cloudrunv2.ServiceIamMember {
	return f.frontendGatewayIamMember
}

// GetCertificate returns the managed SSL certificate for the domain.
func (f *FullStack) GetCertificate() *compute.ManagedSslCertificate {
	return f.certificate
}

// GetGlobalForwardingRule returns the global forwarding rule for the load balancer.
func (f *FullStack) GetGlobalForwardingRule() *compute.GlobalForwardingRule {
	return f.globalForwardingRule
}

// LookupDNSZone finds the appropriate DNS managed zone for the given domain in the current project
func (f *FullStack) LookupDNSZone(ctx *pulumi.Context, domainURL string) (string, error) {
	return f.lookupDNSZone(ctx, domainURL, f.Project)
}

// GetDNSRecord returns the DNS record created for the load balancer
func (f *FullStack) GetDNSRecord() *dns.RecordSet {
	return f.dnsRecord
}

// GetBackendAccount returns the backend service account.
func (f *FullStack) GetBackendAccount() *serviceaccount.Account {
	return f.backendAccount
}

// GetFrontendAccount returns the frontend service account.
func (f *FullStack) GetFrontendAccount() *serviceaccount.Account {
	return f.frontendAccount
}

// GetGatewayServiceAccount returns the API Gateway service account.
func (f *FullStack) GetGatewayServiceAccount() *serviceaccount.Account {
	return f.gatewayServiceAccount
}

// GetBackendNEG returns the region network endpoint group for the backend service.
func (f *FullStack) GetBackendNEG() *compute.RegionNetworkEndpointGroup {
	return f.backendNeg
}

// GetFrontendNEG returns the region network endpoint group for the frontend service.
func (f *FullStack) GetFrontendNEG() *compute.RegionNetworkEndpointGroup {
	return f.frontendNeg
}

// GetGatewayNEG returns the region network endpoint group for the API Gateway.
func (f *FullStack) GetGatewayNEG() *compute.RegionNetworkEndpointGroup {
	return f.apiGatewayNeg
}

// GetURLMap returns the URL map for the load balancer.
func (f *FullStack) GetURLMap() *compute.URLMap {
	return f.urlMap
}

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
