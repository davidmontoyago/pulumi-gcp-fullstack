package gcp

import (
	"fmt"

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
}

type BackendArgs struct {
	ResourceLimits pulumi.StringMap
}

type FrontendArgs struct {
	ResourceLimits pulumi.StringMap
}

func NewFullStack(ctx *pulumi.Context, name string, args *FullStackArgs, opts ...pulumi.ResourceOption) (*FullStack, error) {
	f := &FullStack{
		Project:       args.Project,
		Region:        args.Region,
		BackendImage:  args.BackendImage,
		FrontendImage: args.FrontendImage,
		BackendName:   args.BackendName,
		FrontendName:  args.FrontendName,
	}
	err := ctx.RegisterComponentResource("pulumi-fullstack:gcp:FullStack", name, f, opts...)
	if err != nil {
		return nil, err
	}

	// TODO prefix all resources with name
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
	err = DeployExternalLoadBalancer(ctx, f.FrontendName, f.Project, f.Region)
	return err
}
