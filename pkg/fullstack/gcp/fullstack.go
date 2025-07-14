package gcp

import (
	"fmt"
	"log"
	"math"
	"strings"

	apigateway "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/apigateway"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type FullStack struct {
	pulumi.ResourceState

	Project       string
	Region        string
	BackendName   string
	BackendImage  string
	FrontendName  string
	FrontendImage string

	prefix string
}

type FullStackArgs struct {
	Project       string
	Region        string
	BackendName   string
	BackendImage  string
	FrontendName  string
	FrontendImage string
	// Optional additional config
	Backend  *BackendArgs
	Frontend *FrontendArgs
	Network  *NetworkArgs
}

type BackendArgs struct {
	*InstanceArgs
}

type FrontendArgs struct {
	*InstanceArgs
	EnableUnauthenticated bool
}

type InstanceArgs struct {
	ResourceLimits       pulumi.StringMap
	SecretConfigFileName string
	SecretConfigFilePath string
	EnvVars              map[string]string
	MaxInstanceCount     int
}

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

func NewFullStack(ctx *pulumi.Context, name string, args *FullStackArgs, opts ...pulumi.ResourceOption) (*FullStack, error) {
	f := &FullStack{
		Project:       args.Project,
		Region:        args.Region,
		BackendImage:  args.BackendImage,
		FrontendImage: args.FrontendImage,
		BackendName:   args.BackendName,
		FrontendName:  args.FrontendName,

		prefix: name,
	}
	err := ctx.RegisterComponentResource("pulumi-fullstack:gcp:FullStack", name, f, opts...)
	if err != nil {
		return nil, err
	}

	// proceed to provision
	err = f.deploy(ctx, args)
	if err != nil {
		return nil, err
	}

	err = ctx.RegisterResourceOutputs(f, pulumi.Map{})
	if err != nil {
		return nil, err
	}

	return f, nil
}

func (f *FullStack) deploy(ctx *pulumi.Context, args *FullStackArgs) error {
	backendService, _, err := f.deployBackendCloudRunInstance(ctx, args.Backend)
	if err != nil {
		return err
	}

	frontendService, _, err := f.deployFrontendCloudRunInstance(ctx, args.Frontend, backendService.Uri)
	if err != nil {
		return err
	}

	// TODO should be removed as we want the frontend to not bypass the API gateway
	// allow backend to be invoked from frontend
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

	// Deploy API Gateway (enabled by default, can be disabled)

	var apiGateway *apigateway.Gateway
	var gatewayArgs *APIGatewayArgs
	if args.Network != nil {
		gatewayArgs = applyDefaultGatewayArgs(args.Network.APIGateway, backendService.Uri, frontendService.Uri)

		apiGateway, err = f.deployAPIGateway(ctx, gatewayArgs)
		if err != nil {
			return err
		}
	}

	// create an external load balancer and point to a serverless NEG (API gateway or Cloud run)
	err = f.deployExternalLoadBalancer(ctx, args.FrontendName, args.Network, apiGateway)
	return err
}

func (f *FullStack) newResourceName(name string, max int) string {
	resourceName := fmt.Sprintf("%s-%s", f.prefix, name)
	if len(resourceName) > max {
		// if too big, cut back
		surplus := len(resourceName) - max

		prefixSurplus := int(math.Ceil(float64(surplus) / 2))
		shortPrefix := f.prefix[:len(f.prefix)-prefixSurplus]

		nameSurplus := surplus - prefixSurplus
		shortName := name[:len(name)-nameSurplus]

		resourceName = fmt.Sprintf("%s-%s",
			strings.TrimSuffix(shortPrefix, "-"),
			strings.TrimSuffix(shortName, "-"),
		)
	}
	return resourceName
}

// applyDefaultGatewayArgs applies default API Gateway configuration to the provided args.
// If the provided args is nil, it returns a new instance with default config.
// If the provided args has a nil Config, it applies the default config.
func applyDefaultGatewayArgs(existingArgs *APIGatewayArgs, backendServiceURL, frontendServiceURL pulumi.StringOutput) *APIGatewayArgs {
	defaultGatewayArgs := &APIGatewayArgs{
		Config: &APIConfigArgs{
			BackendServiceURL:  backendServiceURL,
			FrontendServiceURL: frontendServiceURL,
		},
	}

	var gatewayArgs *APIGatewayArgs
	if existingArgs == nil {
		gatewayArgs = defaultGatewayArgs
	} else {
		gatewayArgs = existingArgs
	}

	if existingArgs.Config == nil {
		gatewayArgs.Config = defaultGatewayArgs.Config
	}

	log.Printf("Using API Gateway args: %+v", gatewayArgs)

	return gatewayArgs
}
