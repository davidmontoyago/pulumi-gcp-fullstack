package gcp

import (
	"fmt"

	compute "github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/compute"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// deployExternalLoadBalancer sets up a global classic Application Load Balancer
// in front of the Run Service with the following feats:
//
// - HTTPS by default with GCP managed certificate
// - HTTP forward & redirect to HTTPs
//
// See:
// https://cloud.google.com/load-balancing/docs/https/setting-up-https-serverless
// https://cloud.google.com/load-balancing/docs/negs/serverless-neg-concepts#and
// https://cloud.google.com/load-balancing/docs/https#global-classic-connections
func (f *FullStack) deployExternalLoadBalancer(ctx *pulumi.Context, serviceName string, args *NetworkArgs) error {
	var cloudArmorPolicy *compute.SecurityPolicy
	var err error
	if args.EnableCloudArmor {
		policyName := fmt.Sprintf("%s-%s", f.Name, serviceName)
		cloudArmorPolicy, err = newCloudArmorPolicy(ctx, policyName, args, f.Project)
		if err != nil {
			return err
		}
	}

	backendUrlMap, err := newCloudRunNEG(ctx, cloudArmorPolicy, serviceName, args.ProxyNetworkName, f.Project, f.Region)
	if err != nil {
		return err
	}

	err = newHTTPSProxy(ctx, serviceName, args.DomainURL, f.Project, args.EnablePrivateTrafficOnly, backendUrlMap)
	return err
}

func newHTTPSProxy(ctx *pulumi.Context, serviceName, domainName, project string, privateTraffic bool, backendUrlMap *compute.URLMap) error {
	certificate, err := compute.NewManagedSslCertificate(ctx, fmt.Sprintf("%s-tls", serviceName), &compute.ManagedSslCertificateArgs{
		Description: pulumi.String(fmt.Sprintf("TLS cert for %s", serviceName)),
		Project:     pulumi.String(project),
		Managed: &compute.ManagedSslCertificateManagedArgs{
			Domains: pulumi.StringArray{
				pulumi.String(domainName),
			},
		},
	})
	if err != nil {
		return err
	}
	ctx.Export("load_balancer_https_certificate_id", certificate.ID())
	ctx.Export("load_balancer_https_certificate_uri", certificate.SelfLink)

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
	ctx.Export("load_balancer_https_proxy_id", httpsProxy.ID())
	ctx.Export("load_balancer_https_proxy_uri", httpsProxy.SelfLink)

	if !privateTraffic {
		// https://cloud.google.com/load-balancing/docs/https#forwarding-rule
		trafficRule, err := compute.NewGlobalForwardingRule(ctx, fmt.Sprintf("%s-https", serviceName), &compute.GlobalForwardingRuleArgs{
			Description:         pulumi.String(fmt.Sprintf("HTTPS forwarding rule to LB traffic for %s", serviceName)),
			Project:             pulumi.String(project),
			PortRange:           pulumi.String("443"),
			LoadBalancingScheme: pulumi.String("EXTERNAL"),
			Target:              httpsProxy.SelfLink,
		})
		if err != nil {
			return err
		}
		ctx.Export("load_balancer_global_forwarding_rule_id", trafficRule.ID())
		ctx.Export("load_balancer_global_forwarding_rule_uri", trafficRule.SelfLink)
		ctx.Export("load_balancer_global_forwarding_rule_ip_address", trafficRule.IpAddress)
	}

	return nil
}

func newCloudRunNEG(ctx *pulumi.Context, policy *compute.SecurityPolicy, serviceName, network, project, region string) (*compute.URLMap, error) {
	// create proxy-only subnet required by Cloud Run to get traffic from the LB
	// See:
	// https://cloud.google.com/load-balancing/docs/https#proxy-only-subnet
	trafficNetwork := network
	if trafficNetwork == "" {
		trafficNetwork = "default"
	}
	subnet, err := compute.NewSubnetwork(ctx, fmt.Sprintf("%s-proxy-only", serviceName), &compute.SubnetworkArgs{
		Name:        pulumi.String(fmt.Sprintf("%s-proxy-only", serviceName)),
		Description: pulumi.String(fmt.Sprintf("proxy-only subnet for cloud run traffic for %s", serviceName)),
		Project:     pulumi.String(project),
		Region:      pulumi.String(region),
		Purpose:     pulumi.String("REGIONAL_MANAGED_PROXY"),
		Network:     pulumi.String(trafficNetwork),
		// Extended subnetworks in auto subnet mode networks cannot overlap with 10.128.0.0/9
		IpCidrRange: pulumi.String("10.127.0.0/24"),
		Role:        pulumi.String("ACTIVE"),
	})
	if err != nil {
		return nil, err
	}
	ctx.Export("load_balancer_proxy_subnet_id", subnet.ID())
	ctx.Export("load_balancer_proxy_subnet_uri", subnet.SelfLink)

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
		return nil, err
	}
	ctx.Export("load_balancer_network_endpoint_group_id", neg.ID())
	ctx.Export("load_balancer_network_endpoint_group_uri", neg.SelfLink)

	serviceArgs := &compute.BackendServiceArgs{
		Description:         pulumi.String(fmt.Sprintf("service backend for %s", serviceName)),
		Project:             pulumi.String(project),
		LoadBalancingScheme: pulumi.String("EXTERNAL"),
		Backends: compute.BackendServiceBackendArray{
			&compute.BackendServiceBackendArgs{
				Group: neg.SelfLink,
			},
		},
		// TODO allow enabling IAP (Identity Aware Proxy)
	}
	if policy != nil {
		serviceArgs.SecurityPolicy = policy.SelfLink
	}
	service, err := compute.NewBackendService(ctx, fmt.Sprintf("%s-default", serviceName), serviceArgs)
	if err != nil {
		return nil, err
	}
	ctx.Export("load_balancer_backend_service_id", neg.ID())
	ctx.Export("load_balancer_backend_service_uri", neg.SelfLink)

	// TODO create compute address if enabled
	backendUrlMap, err := compute.NewURLMap(ctx, fmt.Sprintf("%s-default", serviceName), &compute.URLMapArgs{
		Description:    pulumi.String(fmt.Sprintf("URL map to LB traffic for %s", serviceName)),
		Project:        pulumi.String(project),
		DefaultService: service.SelfLink,
	})
	if err != nil {
		return nil, err
	}
	ctx.Export("load_balancer_url_map_id", backendUrlMap.ID())
	ctx.Export("load_balancer_url_map_uri", backendUrlMap.SelfLink)
	return backendUrlMap, nil
}
