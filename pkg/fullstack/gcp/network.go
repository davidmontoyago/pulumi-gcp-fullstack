package gcp

import (
	"fmt"
	"log"
	"strings"

	apigateway "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/apigateway"
	compute "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/dns"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/projects"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// deployExternalLoadBalancer sets up a global classic Application Load Balancer
// in front of the Run Service with the following feats:
//
// - HTTPS by default with GCP managed certificate
// - HTTP forward & redirect to HTTPs
// - Optional API Gateway integration for backend traffic wrangling
//
// See:
// https://cloud.google.com/load-balancing/docs/https/setting-up-https-serverless
// https://cloud.google.com/load-balancing/docs/negs/serverless-neg-concepts#and
// https://cloud.google.com/load-balancing/docs/https#global-classic-connections
// https://cloud.google.com/api-gateway/docs/gateway-serverless-neg
func (f *FullStack) deployExternalLoadBalancer(ctx *pulumi.Context, args *NetworkArgs, apiGateway *apigateway.Gateway) error {
	endpointName := "gcp-lb"

	var cloudArmorPolicy *compute.SecurityPolicy
	var err error
	if args.EnableCloudArmor {
		cloudArmorPolicy, err = f.newCloudArmorPolicy(ctx, endpointName, args)
		if err != nil {
			return fmt.Errorf("failed to create Cloud Armor policy: %w", err)
		}
	}

	if args.EnableIAP {
		// identity platform can't be fully enabled programatically yet.
		// make sure to enable in marketplace console.
		//
		// See:
		// https://issuetracker.google.com/issues/194945691?pli=1
		// https://stackoverflow.com/questions/67778417/enable-google-cloud-identity-platform-programmatically-no-ui
		identityPlatformName := f.newResourceName(endpointName, "cloudidentity", 100)
		_, err := projects.NewService(ctx, identityPlatformName, &projects.ServiceArgs{
			Project: pulumi.String(f.Project),
			Service: pulumi.String("cloudidentity.googleapis.com"),
		})
		if err != nil {
			return fmt.Errorf("failed to enable cloudidentity API: %w", err)
		}

		idToolkitName := f.newResourceName(endpointName, "idtoolkit", 100)
		_, err = projects.NewService(ctx, idToolkitName, &projects.ServiceArgs{
			Project: pulumi.String(f.Project),
			Service: pulumi.String("identitytoolkit.googleapis.com"),
		})
		if err != nil {
			return fmt.Errorf("failed to enable identitytoolkit API: %w", err)
		}

		// Enabling IAP requires Google project to be under an organization.
		// This is a requirement for OAuth Brands which are created as Internal
		// by default. To allow for external users it needs to be set to Public
		// via the UI.
		// An organization can be created via either Cloud Identity or Workspaces (GSuite).
		// If using Cloud Identity, Google will require you to verify the domain provided
		// with TXT record.
		//
		// See:
		// https://cloud.google.com/iap/docs/programmatic-oauth-clients#branding
		// https://support.google.com/cloud/answer/10311615#user-type&zippy=%2Cinternal
		// https://cloud.google.com/resource-manager/docs/cloud-platform-resource-hierarchy#organizations
		// iapBrandName := f.newResourceName(serviceName, "iap-auth", 100)
		// _, err = projects.NewService(ctx, iapBrandName, &projects.ServiceArgs{
		// 	Project: pulumi.String(f.Project),
		// 	Service: pulumi.String("iap.googleapis.com"),
		// })
		// if err != nil {
		// 	return err
		// }
		// _, err = iap.NewBrand(ctx, iapBrandName, &iap.BrandArgs{
		// 	SupportEmail:     pulumi.String(args.IAPSupportEmail),
		// 	ApplicationTitle: pulumi.String("Cloud IAP protected Application"),
		// 	Project:          projectService.Project,
		// })
		// if err != nil {
		// 	return err
		// }
	}

	// Create NEG for either Cloud Run or API Gateway
	lbRouteURLMap, err := f.setupTrafficRouterToUpstreamNEG(ctx, cloudArmorPolicy, endpointName, args.DomainURL, args.ProxyNetworkName, apiGateway)
	if err != nil {
		return fmt.Errorf("failed to setup traffic router: %w", err)
	}

	err = f.newHTTPSProxy(ctx, endpointName, args.DomainURL, args.EnablePrivateTrafficOnly, args.EnableGlobalEntrypoint, lbRouteURLMap)
	if err != nil {
		return fmt.Errorf("failed to create HTTPS proxy: %w", err)
	}

	return nil
}

func (f *FullStack) newHTTPSProxy(ctx *pulumi.Context, serviceName, domainURL string, privateTraffic bool, enableGlobalEntrypoint bool, backendURLMap *compute.URLMap) error {
	tlsCertName := f.newResourceName(serviceName, "tls-cert", 100)
	certificate, err := compute.NewManagedSslCertificate(ctx, tlsCertName, &compute.ManagedSslCertificateArgs{
		Description: pulumi.String(fmt.Sprintf("TLS cert for %s", serviceName)),
		Project:     pulumi.String(f.Project),
		Managed: &compute.ManagedSslCertificateManagedArgs{
			Domains: pulumi.StringArray{
				pulumi.String(domainURL),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create managed SSL certificate: %w", err)
	}
	ctx.Export("load_balancer_https_certificate_id", certificate.ID())
	ctx.Export("load_balancer_https_certificate_uri", certificate.SelfLink)
	f.certificate = certificate

	httpsProxyName := f.newResourceName(serviceName, "https-proxy", 100)
	httpsProxy, err := compute.NewTargetHttpsProxy(ctx, httpsProxyName, &compute.TargetHttpsProxyArgs{
		Description: pulumi.String(fmt.Sprintf("proxy to LB traffic for %s", serviceName)),
		Project:     pulumi.String(f.Project),
		UrlMap:      backendURLMap.SelfLink,
		SslCertificates: pulumi.StringArray{
			certificate.SelfLink,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create target HTTPS proxy: %w", err)
	}
	ctx.Export("load_balancer_https_proxy_id", httpsProxy.ID())
	ctx.Export("load_balancer_https_proxy_uri", httpsProxy.SelfLink)

	if !privateTraffic {
		var lbIPAddress pulumi.StringOutput
		if enableGlobalEntrypoint {
			lbIPAddress, err = f.createGlobalInternetEntrypoint(ctx, serviceName, httpsProxy)
		} else {
			lbIPAddress, err = f.createRegionalInternetEntrypoint(ctx, serviceName, httpsProxy)
		}
		if err != nil {
			return fmt.Errorf("failed to create internet entrypoint: %w", err)
		}

		// Create DNS record for the LB IP address
		dnsRecord, dnsErr := f.createDNSRecord(ctx, serviceName, domainURL, lbIPAddress)
		if dnsErr != nil {
			return fmt.Errorf("failed to create DNS record: %w", dnsErr)
		}
		f.dnsRecord = dnsRecord
	}

	return nil
}

func (f *FullStack) setupTrafficRouterToUpstreamNEG(ctx *pulumi.Context,
	policy *compute.SecurityPolicy,
	serviceName,
	domainURL,
	network string,
	apiGateway *apigateway.Gateway) (*compute.URLMap, error) {
	// create proxy-only subnet required by Cloud Run to get traffic from the LB
	// See:
	// https://cloud.google.com/load-balancing/docs/https#proxy-only-subnet
	trafficNetwork := network
	if trafficNetwork == "" {
		trafficNetwork = "default"
	}

	proxySubnetName := f.newResourceName(serviceName, "proxy-subnet", 100)
	subnet, err := compute.NewSubnetwork(ctx, proxySubnetName, &compute.SubnetworkArgs{
		Name:        pulumi.String(proxySubnetName),
		Description: pulumi.String(fmt.Sprintf("proxy-only subnet for %s traffic", serviceName)),
		Project:     pulumi.String(f.Project),
		Region:      pulumi.String(f.Region),
		Purpose:     pulumi.String("REGIONAL_MANAGED_PROXY"),
		Network:     pulumi.String(trafficNetwork),
		// Extended subnetworks in auto subnet mode networks cannot overlap with 10.128.0.0/9
		IpCidrRange: pulumi.String("10.127.0.0/24"),
		Role:        pulumi.String("ACTIVE"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy-only subnet: %w", err)
	}
	ctx.Export("load_balancer_proxy_subnet_id", subnet.ID())
	ctx.Export("load_balancer_proxy_subnet_uri", subnet.SelfLink)

	var urlMap *compute.URLMap

	if f.gatewayEnabled {
		urlMap, err = f.routeTrafficToGateway(ctx, policy, serviceName, domainURL, apiGateway)
		if err != nil {
			return nil, fmt.Errorf("failed to route traffic to API Gateway: %w", err)
		}
	} else {
		urlMap, err = f.routeTrafficToCloudRunInstances(ctx, policy, serviceName, domainURL)
		if err != nil {
			return nil, fmt.Errorf("failed to route traffic to Cloud Run: %w", err)
		}
	}

	ctx.Export("load_balancer_url_map_id", urlMap.ID())
	ctx.Export("load_balancer_url_map_uri", urlMap.SelfLink)
	f.urlMap = urlMap

	return urlMap, nil
}

// routeTrafficToGateway creates a Gateway NEG and routes traffic to it
// and returns the URL map.
func (f *FullStack) routeTrafficToGateway(ctx *pulumi.Context,
	policy *compute.SecurityPolicy,
	serviceName,
	_ string,
	apiGateway *apigateway.Gateway) (*compute.URLMap, error) {

	// Create NEG for API Gateway
	lbGatewayBackendService, err := f.createGatewayNEG(ctx, policy, serviceName, apiGateway)
	if err != nil {
		return nil, fmt.Errorf("failed to create API Gateway NEG: %w", err)
	}

	urlMapName := f.newResourceName(serviceName, "url-map", 100)

	// Create URL map for Gateway NEG
	urlMap, err := compute.NewURLMap(ctx, urlMapName, &compute.URLMapArgs{
		Description: pulumi.String(fmt.Sprintf("URL map to LB traffic for %s", serviceName)),
		Project:     pulumi.String(f.Project),
		// All traffic is deferred to the Gateway NEG
		DefaultService: lbGatewayBackendService.SelfLink,
		// TODO set host rules to match DNS
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create URL map for Gateway: %w", err)
	}

	return urlMap, nil
}

// createGatewayNEG creates a Network Endpoint Group (NEG) for API Gateway integration
// and returns the associated backend service.
func (f *FullStack) createGatewayNEG(ctx *pulumi.Context,
	policy *compute.SecurityPolicy,
	serviceName string,
	apiGateway *apigateway.Gateway) (*compute.BackendService, error) {
	// This feature is currently in preview. The NEG gets to fail attached to the API Gateway.
	// See:
	// - https://discuss.google.dev/t/serverless-neg-and-api-gateway/189045
	// - https://discuss.google.dev/t/cloud-run-accessed-via-serverless-neg-with-url-mask-returns-404/172725
	gatewayNegName := f.newResourceName(serviceName, "gateway-neg", 100)
	neg, err := compute.NewRegionNetworkEndpointGroup(ctx, gatewayNegName, &compute.RegionNetworkEndpointGroupArgs{
		Description:         pulumi.String(fmt.Sprintf("NEG to route LB traffic to API Gateway for %s", serviceName)),
		Project:             pulumi.String(f.Project),
		Region:              pulumi.String(f.Region),
		NetworkEndpointType: pulumi.String("SERVERLESS"),
		ServerlessDeployment: &compute.RegionNetworkEndpointGroupServerlessDeploymentArgs{
			Platform: pulumi.String("apigateway.googleapis.com"),
			Resource: apiGateway.GatewayId,
			// Gateway NEG can also be configured with a URL mask
			// See:
			// - https://cloud.google.com/load-balancing/docs/https/setting-up-https-serverless#using-url-mask
			// UrlMask: pulumi.String("davidmontoyago.path2prod.dev/<gateway>/my-gateway-id"),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Gateway NEG: %w", err)
	}
	f.apiGatewayNeg = neg
	ctx.Export("load_balancer_gateway_network_endpoint_group_id", neg.ID())
	ctx.Export("load_balancer_gateway_network_endpoint_group_uri", neg.SelfLink)

	lbGatewayServiceArgs := &compute.BackendServiceArgs{
		Description:         pulumi.String(fmt.Sprintf("service backend for %s", serviceName)),
		Project:             pulumi.String(f.Project),
		LoadBalancingScheme: pulumi.String("EXTERNAL"),
		Backends: compute.BackendServiceBackendArray{
			&compute.BackendServiceBackendArgs{
				// Point the LB backend to the Gateway NEG
				Group: f.apiGatewayNeg.SelfLink,
			},
		},
	}

	// Attach Cloud Armor policy if enabled
	if policy != nil {
		lbGatewayServiceArgs.SecurityPolicy = policy.SelfLink
	}

	// Create the LB's backend service for Gateway NEG
	backendServiceName := f.newResourceName(serviceName, "gateway-backend-service", 100)
	lbGatewayBackendService, err := compute.NewBackendService(ctx, backendServiceName, lbGatewayServiceArgs)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gateway backend service: %w", err)
	}
	ctx.Export("load_balancer_gateway_backend_service_id", lbGatewayBackendService.ID())
	ctx.Export("load_balancer_gateway_backend_service_uri", lbGatewayBackendService.SelfLink)

	return lbGatewayBackendService, nil
}

// createCloudRunNEGs creates Network Endpoint Groups (NEGs) for Cloud Run instances
// and returns the associated backend and frontend services.
func (f *FullStack) createCloudRunNEGs(ctx *pulumi.Context,
	policy *compute.SecurityPolicy,
	serviceName string) (*compute.BackendService, *compute.BackendService, error) {
	// No Gateway. Create NEGs for backend and frontendCloud Run instances

	cloudrunBackendNegName := f.newResourceName(serviceName, "backend-cloudrun-neg", 100)
	backendNeg, err := compute.NewRegionNetworkEndpointGroup(ctx, cloudrunBackendNegName, &compute.RegionNetworkEndpointGroupArgs{
		Description:         pulumi.String(fmt.Sprintf("NEG to route LB traffic to %s", serviceName)),
		Project:             pulumi.String(f.Project),
		Region:              pulumi.String(f.Region),
		NetworkEndpointType: pulumi.String("SERVERLESS"),
		CloudRun: &compute.RegionNetworkEndpointGroupCloudRunArgs{
			Service: f.backendService.Name,
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create backend Cloud Run NEG: %w", err)
	}
	f.backendNeg = backendNeg
	ctx.Export("load_balancer_backend_network_endpoint_group_id", backendNeg.ID())
	ctx.Export("load_balancer_backend_network_endpoint_group_uri", backendNeg.SelfLink)

	cloudrunFrontendNegName := f.newResourceName(serviceName, "frontend-cloudrun-neg", 100)
	frontendNeg, err := compute.NewRegionNetworkEndpointGroup(ctx, cloudrunFrontendNegName, &compute.RegionNetworkEndpointGroupArgs{
		Description:         pulumi.String(fmt.Sprintf("NEG to route LB traffic to %s", serviceName)),
		Project:             pulumi.String(f.Project),
		Region:              pulumi.String(f.Region),
		NetworkEndpointType: pulumi.String("SERVERLESS"),
		CloudRun: &compute.RegionNetworkEndpointGroupCloudRunArgs{
			Service: f.frontendService.Name,
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create frontend Cloud Run NEG: %w", err)
	}
	f.frontendNeg = frontendNeg
	ctx.Export("load_balancer_frontend_network_endpoint_group_id", frontendNeg.ID())
	ctx.Export("load_balancer_frontend_network_endpoint_group_uri", frontendNeg.SelfLink)

	lbBackendServiceArgs := &compute.BackendServiceArgs{
		Description:         pulumi.String(fmt.Sprintf("service backend for %s", serviceName)),
		Project:             pulumi.String(f.Project),
		LoadBalancingScheme: pulumi.String("EXTERNAL"),
		Backends: compute.BackendServiceBackendArray{
			&compute.BackendServiceBackendArgs{
				// Point the LB backend to the Gateway NEG
				Group: f.backendNeg.SelfLink,
			},
		},
	}

	lbFrontendServiceArgs := &compute.BackendServiceArgs{
		Description:         pulumi.String(fmt.Sprintf("service backend for %s", serviceName)),
		Project:             pulumi.String(f.Project),
		LoadBalancingScheme: pulumi.String("EXTERNAL"),
		Backends: compute.BackendServiceBackendArray{
			&compute.BackendServiceBackendArgs{
				// Point the LB backend to the Gateway NEG
				Group: f.frontendNeg.SelfLink,
			},
		},
	}

	// Attach Cloud Armor policy if enabled
	if policy != nil {
		lbBackendServiceArgs.SecurityPolicy = policy.SelfLink
		lbFrontendServiceArgs.SecurityPolicy = policy.SelfLink
	}

	// Create the LB backends - They'll be attached to the URL map
	backendServiceName := f.newResourceName(serviceName, "cloudrun-backend-service", 100)
	backendService, err := compute.NewBackendService(ctx, backendServiceName, lbBackendServiceArgs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create backend service for NEG: %w", err)
	}
	ctx.Export("load_balancer_cloud_run_backend_service_id", backendService.ID())
	ctx.Export("load_balancer_cloud_run_backend_service_uri", backendService.SelfLink)

	frontendServiceName := f.newResourceName(serviceName, "cloudrun-frontend-service", 100)
	frontendService, err := compute.NewBackendService(ctx, frontendServiceName, lbFrontendServiceArgs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create frontend service for NEG: %w", err)
	}
	ctx.Export("load_balancer_cloud_run_frontend_service_id", frontendService.ID())
	ctx.Export("load_balancer_cloud_run_frontend_service_uri", frontendService.SelfLink)

	return backendService, frontendService, nil
}

// routeTrafficToCloudRunInstances creates Cloud Run NEGs and URL mapping rules to route traffic to them
// and returns the URL map.
func (f *FullStack) routeTrafficToCloudRunInstances(ctx *pulumi.Context,
	policy *compute.SecurityPolicy,
	serviceName,
	domainURL string) (*compute.URLMap, error) {

	// Create NEGs for Cloud Run instances
	backendService, frontendService, err := f.createCloudRunNEGs(ctx, policy, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to create Cloud Run NEGs: %w", err)
	}

	urlMapName := f.newResourceName(serviceName, "url-map", 100)

	paths := &compute.URLMapPathMatcherArgs{
		Name:           pulumi.String("traffic-paths"),
		DefaultService: backendService.SelfLink,
		PathRules: compute.URLMapPathMatcherPathRuleArray{
			&compute.URLMapPathMatcherPathRuleArgs{
				Paths: pulumi.StringArray{
					// TODO make me configurable
					pulumi.String("/api/*"),
				},
				Service: backendService.SelfLink,
			},
			&compute.URLMapPathMatcherPathRuleArgs{
				Paths: pulumi.StringArray{
					// TODO make me configurable
					pulumi.String("/*"),
				},
				Service: frontendService.SelfLink,
			},
		},
	}

	urlMap, err := compute.NewURLMap(ctx, urlMapName, &compute.URLMapArgs{
		Description: pulumi.String(fmt.Sprintf("URL map to LB traffic for %s", serviceName)),
		Project:     pulumi.String(f.Project),
		// Default to the backend if no path matches
		DefaultService: backendService.SelfLink,
		PathMatchers: compute.URLMapPathMatcherArray{
			paths,
		},
		// Host rules (can be customized for your domain)
		HostRules: compute.URLMapHostRuleArray{
			&compute.URLMapHostRuleArgs{
				Hosts: pulumi.StringArray{
					// Favor domain URL over "*" to avoid host header attacks
					pulumi.String(domainURL),
				},
				PathMatcher: paths.Name,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create URL map for Cloud Run: %w", err)
	}

	return urlMap, nil
}

// createGlobalInternetEntrypoint creates a global IP address and forwarding rule for external traffic
func (f *FullStack) createGlobalInternetEntrypoint(ctx *pulumi.Context, serviceName string, httpsProxy *compute.TargetHttpsProxy) (pulumi.StringOutput, error) {
	labels := mergeLabels(f.Labels, pulumi.StringMap{
		"load_balancer": pulumi.String("true"),
	})

	// reserve an IP address for the LB
	ipAddressName := f.newResourceName(serviceName, "global-ip", 100)
	ipAddress, err := compute.NewGlobalAddress(ctx, ipAddressName, &compute.GlobalAddressArgs{
		Project:     pulumi.String(f.Project),
		Description: pulumi.String(fmt.Sprintf("IP address for %s", serviceName)),
		IpVersion:   pulumi.String("IPV4"),
		Labels:      labels,
	})
	if err != nil {
		return pulumi.StringOutput{}, fmt.Errorf("failed to reserve global IP address: %w", err)
	}
	ctx.Export("load_balancer_global_address_id", ipAddress.ID())
	ctx.Export("load_balancer_global_address_uri", ipAddress.SelfLink)
	ctx.Export("load_balancer_global_address_ip_address", ipAddress.Address)

	// https://cloud.google.com/load-balancing/docs/https#forwarding-rule
	forwardingRuleName := f.newResourceName(serviceName, "https-forwarding", 100)
	trafficRule, err := compute.NewGlobalForwardingRule(ctx, forwardingRuleName, &compute.GlobalForwardingRuleArgs{
		Description:         pulumi.String(fmt.Sprintf("HTTPS forwarding rule to LB traffic for %s", serviceName)),
		Project:             pulumi.String(f.Project),
		PortRange:           pulumi.String("443"),
		LoadBalancingScheme: pulumi.String("EXTERNAL"),
		Target:              httpsProxy.SelfLink,
		IpAddress:           ipAddress.Address,
		Labels:              labels,
	})
	if err != nil {
		return pulumi.StringOutput{}, fmt.Errorf("failed to create global forwarding rule: %w", err)
	}
	ctx.Export("load_balancer_global_forwarding_rule_id", trafficRule.ID())
	ctx.Export("load_balancer_global_forwarding_rule_uri", trafficRule.SelfLink)
	ctx.Export("load_balancer_global_forwarding_rule_ip_address", trafficRule.IpAddress)

	// Store the forwarding rule in the FullStack struct
	f.globalForwardingRule = trafficRule

	return ipAddress.Address, nil
}

// createRegionalInternetEntrypoint creates a regional IP address and regional forwarding rule
// for external traffic (Classic Application Load Balancer in Standard Tier)
func (f *FullStack) createRegionalInternetEntrypoint(ctx *pulumi.Context, serviceName string, httpsProxy *compute.TargetHttpsProxy) (pulumi.StringOutput, error) {
	labels := mergeLabels(f.Labels, pulumi.StringMap{
		"load_balancer": pulumi.String("true"),
	})

	// Reserve a regional IP address for the LB (Standard Network Tier)
	ipAddressName := f.newResourceName(serviceName, "regional-ip", 100)
	ipAddress, err := compute.NewAddress(ctx, ipAddressName, &compute.AddressArgs{
		Project:     pulumi.String(f.Project),
		Region:      pulumi.String(f.Region),
		Description: pulumi.String(fmt.Sprintf("Regional IP address for %s", serviceName)),
		// Classic ALB with regional FR requires Standard tier
		NetworkTier: pulumi.StringPtr("STANDARD"),
		Labels:      labels,
	})
	if err != nil {
		return pulumi.StringOutput{}, fmt.Errorf("failed to reserve regional IP address: %w", err)
	}
	ctx.Export("load_balancer_regional_address_id", ipAddress.ID())
	ctx.Export("load_balancer_regional_address_uri", ipAddress.SelfLink)
	ctx.Export("load_balancer_regional_address_ip_address", ipAddress.Address)

	// Create a regional forwarding rule pointing to the global Target HTTPS Proxy (classic ALB)
	forwardingRuleName := f.newResourceName(serviceName, "regional-https-forwarding", 100)
	trafficRule, err := compute.NewForwardingRule(ctx, forwardingRuleName, &compute.ForwardingRuleArgs{
		Description:         pulumi.String(fmt.Sprintf("HTTPS forwarding rule to LB traffic for %s", serviceName)),
		Project:             pulumi.String(f.Project),
		Region:              pulumi.String(f.Region),
		PortRange:           pulumi.String("443"),
		LoadBalancingScheme: pulumi.String("EXTERNAL"),
		Target:              httpsProxy.SelfLink,
		IpAddress:           ipAddress.Address,
		// Classic ALB regional requires Standard tier
		NetworkTier: pulumi.StringPtr("STANDARD"),
		Labels:      labels,
	})
	if err != nil {
		return pulumi.StringOutput{}, fmt.Errorf("failed to create regional forwarding rule: %w", err)
	}
	ctx.Export("load_balancer_regional_forwarding_rule_id", trafficRule.ID())
	ctx.Export("load_balancer_regional_forwarding_rule_uri", trafficRule.SelfLink)
	ctx.Export("load_balancer_regional_forwarding_rule_ip_address", trafficRule.IpAddress)

	// Store the forwarding rule in the FullStack struct
	f.regionalForwardingRule = trafficRule

	return ipAddress.Address, nil
}

// lookupDNSZone finds the appropriate DNS managed zone for the given domain
func (f *FullStack) lookupDNSZone(ctx *pulumi.Context, domainURL string) (string, error) {
	// Get all managed zones in the project
	managedZones, err := dns.GetManagedZones(ctx, &dns.GetManagedZonesArgs{
		Project: &f.Project,
	})
	if err != nil {
		return "", fmt.Errorf("failed to get managed zones: %w", err)
	}

	// Find the managed zone that matches our domain
	var targetZoneName string
	var targetZoneDNSName string
	for _, zone := range managedZones.ManagedZones {
		// Check if the domain URL ends with the zone's DNS name (with or without trailing dot)
		zoneDNSName := strings.TrimSuffix(zone.DnsName, ".")
		if strings.HasSuffix(domainURL, zoneDNSName) {
			// If we find multiple matches, prefer the most specific one (longest DNS name)
			if targetZoneName == "" || len(zone.DnsName) > len(targetZoneDNSName) {
				if zone.Name != nil {
					targetZoneName = *zone.Name
				}
				targetZoneDNSName = zone.DnsName
			}
		}
	}

	if targetZoneName == "" {
		return "", fmt.Errorf("no managed zone found for domain %s in project %s", domainURL, f.Project)
	}

	if err := ctx.Log.Debug(fmt.Sprintf("Found managed zone %s for domain %s", targetZoneName, domainURL), nil); err != nil {
		log.Printf("failed to log managed zone with Pulumi context: %v", err)
	}

	return targetZoneName, nil
}

// createDNSRecord creates a DNS A record for the given domain and IP address
func (f *FullStack) createDNSRecord(ctx *pulumi.Context, serviceName, domainURL string, ipAddress pulumi.StringOutput) (*dns.RecordSet, error) {
	// Look up the DNS managed zone for the domain
	managedZoneName, err := f.lookupDNSZone(ctx, domainURL)
	if err != nil {
		return nil, err
	}

	dnsRecordName := f.newResourceName(serviceName, "dns-record", 100)

	// Ensure domain URL ends with a trailing dot for DNS compliance
	dnsName := domainURL
	if !strings.HasSuffix(dnsName, ".") {
		dnsName += "."
	}

	dnsRecord, err := dns.NewRecordSet(ctx, dnsRecordName, &dns.RecordSetArgs{
		ManagedZone: pulumi.String(managedZoneName),
		Name:        pulumi.String(dnsName),
		Type:        pulumi.String("A"),
		Ttl:         pulumi.Int(3600),
		Rrdatas:     pulumi.StringArray{ipAddress},
	})
	if err != nil {
		return nil, err
	}

	ctx.Export("load_balancer_dns_record_id", dnsRecord.ID())
	ctx.Export("load_balancer_dns_record_ip_address", dnsRecord.Rrdatas)

	return dnsRecord, nil
}
