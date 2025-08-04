// Package main provides the entry point for the Pulumi GCP Fullstack application.
package main

import (
	"log"

	"github.com/davidmontoyago/pulumi-gcp-fullstack/pkg/fullstack/gcp"
	"github.com/davidmontoyago/pulumi-gcp-fullstack/pkg/fullstack/gcp/config"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Load config helper
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		// Create FullStack instance
		fullstack, err := gcp.NewFullStack(ctx, "my-fullstack", &gcp.FullStackArgs{
			Project:       cfg.GCPProject,
			Region:        cfg.GCPRegion,
			BackendImage:  pulumi.String(cfg.BackendImage),
			FrontendImage: pulumi.String(cfg.FrontendImage),
			Network: &gcp.NetworkArgs{
				DomainURL:                cfg.DomainURL,
				EnableCloudArmor:         cfg.EnableCloudArmor,
				ClientIPAllowlist:        cfg.ClientIPAllowlist,
				EnablePrivateTrafficOnly: cfg.EnablePrivateTrafficOnly,
			},
			Labels: map[string]string{
				"environment": "production",
				"managed-by":  "pulumi",
			},
		})
		if err != nil {
			return err
		}

		// Export important outputs
		ctx.Export("backendServiceUrl", fullstack.GetBackendService().Uri)
		ctx.Export("frontendServiceUrl", fullstack.GetFrontendService().Uri)
		ctx.Export("apiGatewayUrl", fullstack.GetAPIGateway().DefaultHostname)

		log.Println("Fullstack deployment loaded and ready!")

		return nil
	})
}
