// Package gcp provides Google Cloud Platform infrastructure components for fullstack applications.
package gcp

import (
	"fmt"

	apigateway "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/apigateway"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudrun"
	cloudrunv2 "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudrunv2"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/dns"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/projects"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/redis"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/secretmanager"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/vpcaccess"
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

	name                string
	gatewayEnabled      bool
	loadBalancerEnabled bool

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

	// Domain mappings to use when the external LB is disabled and External WAF is used
	backendDomainMapping    *cloudrun.DomainMapping
	frontendDomainMapping   *cloudrun.DomainMapping
	backendResourceRecords  []cloudrun.DomainMappingStatusResourceRecord
	frontendResourceRecords []cloudrun.DomainMappingStatusResourceRecord

	globalForwardingRule   *compute.GlobalForwardingRule
	regionalForwardingRule *compute.ForwardingRule

	certificate *compute.ManagedSslCertificate
	dnsRecord   *dns.RecordSet
	urlMap      *compute.URLMap

	// Project-level IAM roles bound to the backend service account
	backendProjectIamMembers []*projects.IAMMember

	// Cache infrastructure
	redisInstance          *redis.Instance
	vpcConnector           *vpcaccess.Connector
	cacheFirewall          *compute.Firewall
	cacheCredentialsSecret *secretmanager.SecretVersion
}

// NewFullStack creates a new FullStack instance with the provided configuration.
func NewFullStack(ctx *pulumi.Context, name string, args *FullStackArgs, opts ...pulumi.ResourceOption) (*FullStack, error) {
	// Set default values for BackendName and FrontendName if not provided
	backendName := args.BackendName
	if backendName == "" {
		backendName = "backend"
	}

	frontendName := args.FrontendName
	if frontendName == "" {
		frontendName = "frontend"
	}

	fullStack := &FullStack{
		Project:       args.Project,
		Region:        args.Region,
		BackendImage:  args.BackendImage.ToStringOutput(),
		FrontendImage: args.FrontendImage.ToStringOutput(),
		BackendName:   backendName,
		FrontendName:  frontendName,
		Labels:        args.Labels,

		name:                name,
		gatewayEnabled:      args.Network != nil && args.Network.APIGateway != nil && !args.Network.APIGateway.Disabled,
		loadBalancerEnabled: args.Network != nil && !args.Network.EnableExternalWAF,
	}
	err := ctx.RegisterComponentResource("pulumi-fullstack:gcp:FullStack", name, fullStack, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to register component resource: %w", err)
	}

	// proceed to provision
	err = fullStack.deploy(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy full stack: %w", err)
	}

	err = ctx.RegisterResourceOutputs(fullStack, pulumi.Map{})
	if err != nil {
		return nil, fmt.Errorf("failed to register resource outputs: %w", err)
	}

	return fullStack, nil
}

func (f *FullStack) deploy(ctx *pulumi.Context, args *FullStackArgs) error {
	if args.Backend != nil && args.Backend.CacheInstance != nil {
		// Deploy cache companion for backend
		err := f.deployCache(ctx, args.Backend.CacheInstance)
		if err != nil {
			return fmt.Errorf("failed to deploy cache: %w", err)
		}
	}

	backendService, backendAcccount, err := f.deployBackendCloudRunInstance(ctx, args.Backend)
	if err != nil {
		return fmt.Errorf("failed to deploy backend Cloud Run: %w", err)
	}

	f.backendService = backendService
	f.backendAccount = backendAcccount

	frontendService, frontendAccount, err := f.deployFrontendCloudRunInstance(ctx, args.Frontend, backendService.Uri)
	if err != nil {
		return fmt.Errorf("failed to deploy frontend Cloud Run: %w", err)
	}

	f.frontendService = frontendService
	f.frontendAccount = frontendAccount

	var apiGateway *apigateway.Gateway
	var gatewayArgs *APIGatewayArgs

	if f.gatewayEnabled {
		// Deploy API Gateway if enabled
		gatewayArgs = applyDefaultGatewayArgs(args.Network.APIGateway, backendService.Uri, frontendService.Uri)

		apiGateway, err = f.deployAPIGateway(ctx, gatewayArgs)
		if err != nil {
			return fmt.Errorf("failed to deploy API Gateway: %w", err)
		}
	} else {
		err = f.createCloudRunInstancesIAM(ctx, frontendService, backendService)
		if err != nil {
			return fmt.Errorf("failed to create Cloud Run IAM: %w", err)
		}
	}

	if f.loadBalancerEnabled {
		// create an external load balancer and point to a serverless NEG (API gateway or Cloud run)
		err = f.deployExternalLoadBalancer(ctx, args.Network, apiGateway)
		if err != nil {
			return fmt.Errorf("failed to deploy external load balancer: %w", err)
		}
	} else if args.Network.EnableExternalWAF {
		// create domain mappings for the backend and frontend services
		backendDomainMapping, backendResourceRecords, err := f.createInstanceDomainMapping(
			ctx,
			f.BackendName,
			fmt.Sprintf("api-%s", args.Network.DomainURL),
			backendService.Name,
		)
		if err != nil {
			return fmt.Errorf("failed to create backend domain mapping: %w", err)
		}
		f.backendDomainMapping = backendDomainMapping
		f.backendResourceRecords = backendResourceRecords

		frontendDomainMapping, frontendResourceRecords, err := f.createInstanceDomainMapping(
			ctx,
			f.FrontendName,
			args.Network.DomainURL,
			frontendService.Name,
		)
		if err != nil {
			return fmt.Errorf("failed to create frontend domain mapping: %w", err)
		}
		f.frontendDomainMapping = frontendDomainMapping
		f.frontendResourceRecords = frontendResourceRecords

		// TODO allow backend and frontend to have separate URLs
	}

	return nil
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

// GetRegionalForwardingRule returns the regional forwarding rule for the load balancer when regional entrypoint is enabled.
func (f *FullStack) GetRegionalForwardingRule() *compute.ForwardingRule {
	return f.regionalForwardingRule
}

// LookupDNSZone finds the appropriate DNS managed zone for the given domain in the current project
func (f *FullStack) LookupDNSZone(ctx *pulumi.Context, domainURL string) (string, error) {
	return f.lookupDNSZone(ctx, domainURL)
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

// GetBackendProjectIamMembers returns project-level IAM members bound to the backend service account.
func (f *FullStack) GetBackendProjectIamMembers() []*projects.IAMMember {
	return f.backendProjectIamMembers
}

// GetRedisInstance returns the Redis cache instance.
func (f *FullStack) GetRedisInstance() *redis.Instance {
	return f.redisInstance
}

// GetVPCConnector returns the VPC access connector for cache connectivity.
func (f *FullStack) GetVPCConnector() *vpcaccess.Connector {
	return f.vpcConnector
}

// GetCacheFirewall returns the firewall rule for cache connectivity.
func (f *FullStack) GetCacheFirewall() *compute.Firewall {
	return f.cacheFirewall
}

// GetCacheSecretVersion returns the secret version containing Redis credentials.
func (f *FullStack) GetCacheSecretVersion() *secretmanager.SecretVersion {
	return f.cacheCredentialsSecret
}

// GetBackendDomainMapping returns the backend domain mapping for External WAF.
func (f *FullStack) GetBackendDomainMapping() *cloudrun.DomainMapping {
	return f.backendDomainMapping
}

// GetFrontendDomainMapping returns the frontend domain mapping for External WAF.
// TODO: implement frontend domain mapping functionality
func (f *FullStack) GetFrontendDomainMapping() *cloudrun.DomainMapping {
	return f.frontendDomainMapping
}

// GetBackendResourceRecords returns the resource records from the backend domain mapping status.
func (f *FullStack) GetBackendResourceRecords() []cloudrun.DomainMappingStatusResourceRecord {
	return f.backendResourceRecords
}

// GetFrontendResourceRecords returns the resource records from the frontend domain mapping status.
func (f *FullStack) GetFrontendResourceRecords() []cloudrun.DomainMappingStatusResourceRecord {
	return f.frontendResourceRecords
}
