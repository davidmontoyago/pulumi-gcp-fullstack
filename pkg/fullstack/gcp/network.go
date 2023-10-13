package gcp

import (
	"fmt"

	compute "github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/compute"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// createExternalLoadBalancer sets up a global classic Application Load Balancer
// in front of the Run Service with the following feats:
//
// - HTTPS by default with GCP managed certificate
// - HTTP forward & redirect to HTTPs
func DeployExternalLoadBalancer(ctx *pulumi.Context, serviceName, project, region string) error {
	// ---- createCloudRunNEG ---
	// proxy-only subnet required by Cloud Run to get traffic from the LB
	// See:
	// https://cloud.google.com/load-balancing/docs/https#proxy-only-subnet
	_, err := compute.NewSubnetwork(ctx, fmt.Sprintf("%s-proxy-only", serviceName), &compute.SubnetworkArgs{
		Name:        pulumi.String(fmt.Sprintf("%s-proxy-only", serviceName)),
		Description: pulumi.String(fmt.Sprintf("proxy-only subnet for cloud run traffic for %s", serviceName)),
		Project:     pulumi.String(project),
		Region:      pulumi.String(region),
		Purpose:     pulumi.String("REGIONAL_MANAGED_PROXY"),
		Network:     pulumi.String("default"),
		// Extended subnetworks in auto subnet mode networks cannot overlap with 10.128.0.0/9
		IpCidrRange: pulumi.String("10.127.0.0/24"),
		Role:        pulumi.String("ACTIVE"),
	})
	if err != nil {
		return err
	}

	neg, err := compute.NewRegionNetworkEndpointGroup(ctx, fmt.Sprintf("%s-default", serviceName), &compute.RegionNetworkEndpointGroupArgs{
		Description:         pulumi.String(fmt.Sprintf("NEG to route LB traffic to %s", serviceName)),
		Project:             pulumi.String(project),
		Region:              pulumi.String(region),
		NetworkEndpointType: pulumi.String("SERVERLESS"),
		CloudRun: &compute.RegionNetworkEndpointGroupCloudRunArgs{
			Service: pulumi.String(serviceName),
		},
	})
	if err != nil {
		return err
	}

	service, err := compute.NewBackendService(ctx, fmt.Sprintf("%s-default", serviceName), &compute.BackendServiceArgs{
		Description:         pulumi.String(fmt.Sprintf("service backend for %s", serviceName)),
		Project:             pulumi.String(project),
		LoadBalancingScheme: pulumi.String("EXTERNAL"),
		Backends: compute.BackendServiceBackendArray{
			&compute.BackendServiceBackendArgs{
				Group: neg.SelfLink,
			},
		},
		// TODO currently unable to set policyURL via SecurityPolicy
		// See: https://github.com/pulumi/pulumi-google-native/issues/215

		// TODO allow enabling IAP (Identity Aware Proxy)
	})
	if err != nil {
		return err
	}

	// TODO create compute address if enabled
	backendUrlMap, err := compute.NewURLMap(ctx, fmt.Sprintf("%s-default", serviceName), &compute.URLMapArgs{
		Description:    pulumi.String(fmt.Sprintf("URL map to LB traffic for %s", serviceName)),
		Project:        pulumi.String(project),
		DefaultService: service.SelfLink,
	})
	if err != nil {
		return err
	}

	// ---- createHTTPSProxy ---
	certificate, err := compute.NewManagedSslCertificate(ctx, fmt.Sprintf("%s-tls", serviceName), &compute.ManagedSslCertificateArgs{
		Description: pulumi.String(fmt.Sprintf("TLS cert for %s", serviceName)),
		Project:     pulumi.String(project),
		Managed: &compute.ManagedSslCertificateManagedArgs{
			Domains: pulumi.StringArray{
				pulumi.String("adventure.path2prod.dev"),
			},
		},
	})
	if err != nil {
		return err
	}

	httpsProxy, err := compute.NewTargetHttpsProxy(ctx, fmt.Sprintf("%s-https", serviceName), &compute.TargetHttpsProxyArgs{
		Description: pulumi.String(fmt.Sprintf("proxy to LB traffic for %s", serviceName)),
		Project:     pulumi.String(project),
		UrlMap:      backendUrlMap.SelfLink,
		SslCertificates: pulumi.StringArray{
			certificate.SelfLink,
		},
	})
	if err != nil {
		return err
	}

	_, err = compute.NewGlobalForwardingRule(ctx, fmt.Sprintf("%s-https", serviceName), &compute.GlobalForwardingRuleArgs{
		Description: pulumi.String(fmt.Sprintf("HTTPS forwarding rule to LB traffic for %s", serviceName)),
		Project:     pulumi.String(project),
		PortRange:   pulumi.String("443"),
		// NetworkTier:         compute.GlobalForwardingRuleNetworkTierPremium,
		LoadBalancingScheme: pulumi.String("EXTERNAL"),
		Target:              httpsProxy.SelfLink,
	})

	return err
}
