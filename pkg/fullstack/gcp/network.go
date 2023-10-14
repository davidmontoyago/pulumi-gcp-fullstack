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
	if args.EnableCloudArmor {
		_, err := newCloudArmorPolicy(args, serviceName, ctx, f.Project)
		if err != nil {
			return err
		}
	}

	// TODO pass policy
	backendUrlMap, err := newCloudRunNEG(ctx, serviceName, args.ProxyNetworkName, f.Project, f.Region)
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

	if !privateTraffic {
		// https://cloud.google.com/load-balancing/docs/https#forwarding-rule
		_, err = compute.NewGlobalForwardingRule(ctx, fmt.Sprintf("%s-https", serviceName), &compute.GlobalForwardingRuleArgs{
			Description:         pulumi.String(fmt.Sprintf("HTTPS forwarding rule to LB traffic for %s", serviceName)),
			Project:             pulumi.String(project),
			PortRange:           pulumi.String("443"),
			LoadBalancingScheme: pulumi.String("EXTERNAL"),
			Target:              httpsProxy.SelfLink,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func newCloudRunNEG(ctx *pulumi.Context, serviceName, network, project, region string) (*compute.URLMap, error) {
	// create proxy-only subnet required by Cloud Run to get traffic from the LB
	// See:
	// https://cloud.google.com/load-balancing/docs/https#proxy-only-subnet
	trafficNetwork := network
	if trafficNetwork == "" {
		trafficNetwork = "default"
	}
	_, err := compute.NewSubnetwork(ctx, fmt.Sprintf("%s-proxy-only", serviceName), &compute.SubnetworkArgs{
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
		return nil, err
	}

	// TODO create compute address if enabled
	backendUrlMap, err := compute.NewURLMap(ctx, fmt.Sprintf("%s-default", serviceName), &compute.URLMapArgs{
		Description:    pulumi.String(fmt.Sprintf("URL map to LB traffic for %s", serviceName)),
		Project:        pulumi.String(project),
		DefaultService: service.SelfLink,
	})
	if err != nil {
		return nil, err
	}
	return backendUrlMap, nil
}

// creates a best-practice Cloud Armor security policy.
// See:
// https://github.com/GoogleCloudPlatform/terraform-google-cloud-armor/blob/9ea03ee3ff0778a087888582e806da7342635d69/main.tf#L445
func newCloudArmorPolicy(args *NetworkArgs, serviceName string, ctx *pulumi.Context, project string) (*compute.SecurityPolicy, error) {
	// Every security policy must have a default rule at priority 2147483647 with match condition *.
	// See:
	// https://cloud.google.com/armor/docs/waf-rules
	var defaultRules compute.SecurityPolicyRuleArray
	defaultRules = append(defaultRules, &compute.SecurityPolicyRuleArgs{
		Action:      pulumi.String("allow"),
		Description: pulumi.String("default allow rule"),
		Priority:    pulumi.Int(2147483647),
		Match: &compute.SecurityPolicyRuleMatchArgs{
			VersionedExpr: pulumi.String("SRC_IPS_V1"),
			Config: &compute.SecurityPolicyRuleMatchConfigArgs{
				SrcIpRanges: pulumi.StringArray{
					pulumi.String("*"),
				},
			},
		},
	})

	var preconfiguredRules compute.SecurityPolicyRuleArray
	for i, rule := range []string{
		"sqli-v33-stable",
		"xss-v33-stable",
		"lfi-v33-stable",
		"rfi-v33-stable",
		"rce-v33-stable",
		"methodenforcement-v33-stable",
		"scannerdetection-v33-stable",
		"protocolattack-v33-stable",
		"sessionfixation-v33-stable",
		"nodejs-v33-stable",
	} {
		preconfiguredWafRule := fmt.Sprintf("evaluatePreconfiguredWaf('%s', {'sensitivity': 1})", rule)
		preconfiguredRules = append(preconfiguredRules, &compute.SecurityPolicyRuleArgs{
			Action:      pulumi.String("deny(502)"),
			Description: pulumi.String(fmt.Sprintf("preconfigured waf rule %s", rule)),
			Priority:    pulumi.Int(20 + i),
			Match: &compute.SecurityPolicyRuleMatchArgs{
				Expr: &compute.SecurityPolicyRuleMatchExprArgs{
					Expression: pulumi.String(preconfiguredWafRule),
				},
			},
		})
	}

	// IP allowlist rule to restrict access to a handful of IPs... not for the enterprise
	var ipAllowlistRules compute.SecurityPolicyRuleArray
	if len(args.ClientIPAllowlist) > 0 {
		ipRanges := pulumi.StringArray{}
		for _, ip := range args.ClientIPAllowlist {
			ipRanges = append(ipRanges, pulumi.String(ip))
		}

		ipAllowlistRules = append(ipAllowlistRules, &compute.SecurityPolicyRuleArgs{
			Action:      pulumi.String("allow"),
			Priority:    pulumi.Int(1),
			Description: pulumi.String(fmt.Sprintf("ip allowlist rule for %s", serviceName)),
			Match: &compute.SecurityPolicyRuleMatchArgs{

				VersionedExpr: pulumi.String("SRC_IPS_V1"),
				Config: &compute.SecurityPolicyRuleMatchConfigArgs{
					SrcIpRanges: ipRanges,
				},
			},
		}, &compute.SecurityPolicyRuleArgs{
			Action:      pulumi.String("deny(403)"),
			Description: pulumi.String("default ip fallback deny rule"),
			Priority:    pulumi.Int(2),
			Match: &compute.SecurityPolicyRuleMatchArgs{
				VersionedExpr: pulumi.String("SRC_IPS_V1"),
				Config: &compute.SecurityPolicyRuleMatchConfigArgs{
					SrcIpRanges: pulumi.StringArray{
						pulumi.String("*"),
					},
				},
			},
		})
	}

	rules := append(defaultRules, preconfiguredRules...)
	rules = append(rules, ipAllowlistRules...)

	// TODO allow reCAPTCHA
	// TODO add rate limiting rules
	// TODO add named IP preconfigured rules

	policy, err := compute.NewSecurityPolicy(ctx, fmt.Sprintf("%s-default", serviceName), &compute.SecurityPolicyArgs{
		Description: pulumi.String(fmt.Sprintf("Cloud Armor security policy for %s", serviceName)),
		Project:     pulumi.String(project),
		Rules:       rules,
		Type:        pulumi.String("CLOUD_ARMOR"),
	})
	return policy, err
}
