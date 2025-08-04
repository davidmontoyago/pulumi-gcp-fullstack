// Package gcp provides Google Cloud Platform infrastructure components for fullstack applications.
package gcp

import (
	"fmt"
	"math"
	"strings"

	apigateway "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/apigateway"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

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

		name: name,
	}
	err := ctx.RegisterComponentResource("pulumi-fullstack:gcp:FullStack", name, fullStack, opts...)
	if err != nil {
		return nil, err
	}

	// proceed to provision
	err = fullStack.deploy(ctx, args)
	if err != nil {
		return nil, err
	}

	err = ctx.RegisterResourceOutputs(fullStack, pulumi.Map{})
	if err != nil {
		return nil, err
	}

	return fullStack, nil
}

func (f *FullStack) deploy(ctx *pulumi.Context, args *FullStackArgs) error {
	backendService, backendAcccount, err := f.deployBackendCloudRunInstance(ctx, args.Backend)
	if err != nil {
		return err
	}

	f.backendService = backendService
	f.backendAccount = backendAcccount

	frontendService, frontendAccount, err := f.deployFrontendCloudRunInstance(ctx, args.Frontend, backendService.Uri)
	if err != nil {
		return err
	}

	f.frontendService = frontendService
	f.frontendAccount = frontendAccount

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

		if err := ctx.Log.Info(fmt.Sprintf("Using API Gateway args: %+v", gatewayArgs), nil); err != nil {
			return fmt.Errorf("failed to log API Gateway args: %w", err)
		}

		apiGateway, err = f.deployAPIGateway(ctx, gatewayArgs)
		if err != nil {
			return err
		}

		f.apiGateway = apiGateway
	}

	// create an external load balancer and point to a serverless NEG (API gateway or Cloud run)
	err = f.deployExternalLoadBalancer(ctx, args.Network, apiGateway)

	return err
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
