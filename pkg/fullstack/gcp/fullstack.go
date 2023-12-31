package gcp

import (
	"fmt"
	"math"
	"strings"

	cloudrunv2 "github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/cloudrunv2"
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
	// Whether to restrict access to the given list of client IPs. Valid only when EnableCloudArmor=true.
	ClientIPAllowlist []string
	// Whether to disable public internet access. Useful during development. Defaults to false.
	EnablePrivateTrafficOnly bool
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

	frontendAccount, err := f.deployFrontendCloudRunInstance(ctx, args.Frontend, backendService.Uri)
	if err != nil {
		return err
	}

	// allow backend to be invoked from frontend
	_, err = cloudrunv2.NewServiceIamMember(ctx, fmt.Sprintf("%s-%s-invoker", f.BackendName, f.FrontendName), &cloudrunv2.ServiceIamMemberArgs{
		Name:     backendService.Name,
		Project:  pulumi.String(f.Project),
		Location: pulumi.String(f.Region),
		Role:     pulumi.String("roles/run.invoker"),
		Member:   pulumi.Sprintf("serviceAccount:%s", frontendAccount.Email),
	})
	if err != nil {
		return err
	}

	// create an external load balancer with a NEG for the frontend
	err = f.deployExternalLoadBalancer(ctx, args.FrontendName, args.Network)
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
