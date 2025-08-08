// Package gcp provides Google Cloud Platform infrastructure components for fullstack applications.
package gcp

import (
	"fmt"
	"log"
	"math"
	"strings"

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

		name:           name,
		gatewayEnabled: args.Network != nil && args.Network.APIGateway != nil && !args.Network.APIGateway.Disabled,
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

	// create an external load balancer and point to a serverless NEG (API gateway or Cloud run)
	err = f.deployExternalLoadBalancer(ctx, args.Network, apiGateway)
	if err != nil {
		return fmt.Errorf("failed to deploy external load balancer: %w", err)
	}

	return nil
}

// createCloudRunInstancesIAM creates IAM members to allow unauthenticated access to Cloud Run instances
func (f *FullStack) createCloudRunInstancesIAM(ctx *pulumi.Context, frontendService, backendService *cloudrunv2.Service) error {
	if err := ctx.Log.Info(fmt.Sprintf("Routing traffic to Cloud Run instances: %v and %v", frontendService.Uri, backendService.Uri), nil); err != nil {
		log.Println("failed to log routing details with Pulumi context: %w", err)
	}

	// If no gateway enabled, traffic goes directly to the cloud run instances. yehaaw!
	_, err := cloudrunv2.NewServiceIamMember(ctx, fmt.Sprintf("%s-allow-unauthenticated", f.FrontendName), &cloudrunv2.ServiceIamMemberArgs{
		Name:     frontendService.Name,
		Project:  pulumi.String(f.Project),
		Location: pulumi.String(f.Region),
		Role:     pulumi.String("roles/run.invoker"),
		Member:   pulumi.Sprintf("allUsers"),
	})
	if err != nil {
		return fmt.Errorf("failed to grant frontend invoker: %w", err)
	}

	_, err = cloudrunv2.NewServiceIamMember(ctx, fmt.Sprintf("%s-allow-unauthenticated", f.BackendName), &cloudrunv2.ServiceIamMemberArgs{
		Name:     backendService.Name,
		Project:  pulumi.String(f.Project),
		Location: pulumi.String(f.Region),
		Role:     pulumi.String("roles/run.invoker"),
		Member:   pulumi.Sprintf("allUsers"),
	})
	if err != nil {
		return fmt.Errorf("failed to grant backend invoker: %w", err)
	}

	// _, err = cloudrunv2.NewServiceIamMember(ctx, fmt.Sprintf("%s-%s-invoker", f.BackendName, f.FrontendName), &cloudrunv2.ServiceIamMemberArgs{
	// 	Name:     backendService.Name,
	// 	Project:  pulumi.String(f.Project),
	// 	Location: pulumi.String(f.Region),
	// 	Role:     pulumi.String("roles/run.invoker"),
	// 	Member:   pulumi.Sprintf("serviceAccount:%s", frontendAccount.Email),
	// })
	// if err != nil {
	// 	return err
	// }

	return nil
}

func (f *FullStack) newResourceName(serviceName, resourceType string, maxLength int) string {
	var resourceName string
	if resourceType == "" {
		resourceName = fmt.Sprintf("%s-%s", f.name, serviceName)
	} else {
		resourceName = fmt.Sprintf("%s-%s-%s", f.name, serviceName, resourceType)
	}

	if len(resourceName) <= maxLength {
		return resourceName
	}

	surplus := len(resourceName) - maxLength

	// Calculate how much to truncate from each part
	var prefixSurplus, serviceSurplus, typeSurplus int
	if resourceType == "" {
		// Only two parts to truncate
		prefixSurplus = int(math.Ceil(float64(surplus) / 2))
		serviceSurplus = surplus - prefixSurplus
		typeSurplus = 0
	} else {
		prefixSurplus = int(math.Ceil(float64(surplus) / 3))
		serviceSurplus = int(math.Ceil(float64(surplus-prefixSurplus) / 2))
		typeSurplus = surplus - prefixSurplus - serviceSurplus
	}

	// Truncate each part, ensuring we don't truncate more than the part's length
	// and we keep at least one character to avoid leading dashes
	var shortPrefix string
	if prefixSurplus < len(f.name) {
		shortPrefix = f.name[:len(f.name)-prefixSurplus]
	} else {
		shortPrefix = f.name[:1]
	}

	var shortServiceName string
	if serviceSurplus < len(serviceName) {
		shortServiceName = serviceName[:len(serviceName)-serviceSurplus]
	} else {
		shortServiceName = serviceName[:1]
	}

	if resourceType == "" {
		resourceName = fmt.Sprintf("%s-%s",
			strings.TrimSuffix(shortPrefix, "-"),
			strings.TrimSuffix(shortServiceName, "-"),
		)
	} else {
		var shortResourceType string
		if typeSurplus < len(resourceType) {
			shortResourceType = resourceType[:len(resourceType)-typeSurplus]
		} else {
			shortResourceType = resourceType[:1]
		}

		resourceName = fmt.Sprintf("%s-%s-%s",
			strings.TrimSuffix(shortPrefix, "-"),
			strings.TrimSuffix(shortServiceName, "-"),
			strings.TrimSuffix(shortResourceType, "-"),
		)
	}

	return resourceName
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
