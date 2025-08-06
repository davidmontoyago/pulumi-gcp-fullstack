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
		cloudArmorPolicy, err = f.newCloudArmorPolicy(ctx, endpointName, args, f.Project)
		if err != nil {
			return err
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
			return err
		}

		idToolkitName := f.newResourceName(endpointName, "idtoolkit", 100)
		_, err = projects.NewService(ctx, idToolkitName, &projects.ServiceArgs{
			Project: pulumi.String(f.Project),
			Service: pulumi.String("identitytoolkit.googleapis.com"),
		})
		if err != nil {
			return err
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
	backendURLMap, err := f.newServerlessNEG(ctx, cloudArmorPolicy, endpointName, args.ProxyNetworkName, f.Project, f.Region, apiGateway)
	if err != nil {
		return err
	}

	err = f.newHTTPSProxy(ctx, endpointName, args.DomainURL, f.Project, args.EnablePrivateTrafficOnly, backendURLMap)

	return err
}

func (f *FullStack) newHTTPSProxy(ctx *pulumi.Context, serviceName, domainURL, project string, privateTraffic bool, backendURLMap *compute.URLMap) error {
	tlsCertName := f.newResourceName(serviceName, "tls-cert", 100)
	certificate, err := compute.NewManagedSslCertificate(ctx, tlsCertName, &compute.ManagedSslCertificateArgs{
		Description: pulumi.String(fmt.Sprintf("TLS cert for %s", serviceName)),
		Project:     pulumi.String(project),
		Managed: &compute.ManagedSslCertificateManagedArgs{
			Domains: pulumi.StringArray{
				pulumi.String(domainURL),
			},
		},
	})
	if err != nil {
		return err
	}
	ctx.Export("load_balancer_https_certificate_id", certificate.ID())
	ctx.Export("load_balancer_https_certificate_uri", certificate.SelfLink)
	f.certificate = certificate

	httpsProxyName := f.newResourceName(serviceName, "https-proxy", 100)
	httpsProxy, err := compute.NewTargetHttpsProxy(ctx, httpsProxyName, &compute.TargetHttpsProxyArgs{
		Description: pulumi.String(fmt.Sprintf("proxy to LB traffic for %s", serviceName)),
		Project:     pulumi.String(project),
		UrlMap:      backendURLMap.SelfLink,
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
		err = f.createGlobalInternetEntrypoint(ctx, serviceName, domainURL, project, httpsProxy)
		if err != nil {
			return err
		}
	}

	return nil
}

func (f *FullStack) newServerlessNEG(ctx *pulumi.Context, policy *compute.SecurityPolicy, serviceName, network, project, region string, apiGateway *apigateway.Gateway) (*compute.URLMap, error) {
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

	// Create NEG - either for Cloud Run or API Gateway
	var neg *compute.RegionNetworkEndpointGroup

	// Create health check only for Cloud Run NEGs
	// Note: Health checks are NOT supported for Serverless NEGs (API Gateway, Cloud Run, etc.)
	// See: https://cloud.google.com/load-balancing/docs/negs/serverless-neg-concepts#health-checks
	var healthCheck *compute.HealthCheck

	if apiGateway != nil {
		// Create NEG for API Gateway

		// This feature is currently in preview. The NEG gets to fail attached to the API Gateway.
		// See:
		// - https://discuss.google.dev/t/serverless-neg-and-api-gateway/189045
		// - https://discuss.google.dev/t/cloud-run-accessed-via-serverless-neg-with-url-mask-returns-404/172725
		gatewayNegName := f.newResourceName(serviceName, "gateway-neg", 100)
		neg, err = compute.NewRegionNetworkEndpointGroup(ctx, gatewayNegName, &compute.RegionNetworkEndpointGroupArgs{
			Description:         pulumi.String(fmt.Sprintf("NEG to route LB traffic to API Gateway for %s", serviceName)),
			Project:             pulumi.String(project),
			Region:              pulumi.String(region),
			NetworkEndpointType: pulumi.String("SERVERLESS"),
			ServerlessDeployment: &compute.RegionNetworkEndpointGroupServerlessDeploymentArgs{
				Platform: pulumi.String("apigateway.googleapis.com"),
				Resource: apiGateway.GatewayId,
				// Gateway NEG can also be configured with a URL mask
				// See:
				// - https://cloud.google.com/load-balancing/docs/https/setting-up-https-serverless#using-url-mask
				// UrlMask: pulumi.String("davidmontoyago.path2prod.dev/<gateway>/my-gateway-id"),
			},
		}, pulumi.DependsOn([]pulumi.Resource{apiGateway}))
	} else {
		// No Gateway. Create NEG for Cloud Run
		cloudrunNegName := f.newResourceName(serviceName, "cloudrun-neg", 100)
		neg, err = compute.NewRegionNetworkEndpointGroup(ctx, cloudrunNegName, &compute.RegionNetworkEndpointGroupArgs{
			Description:         pulumi.String(fmt.Sprintf("NEG to route LB traffic to %s", serviceName)),
			Project:             pulumi.String(project),
			Region:              pulumi.String(region),
			NetworkEndpointType: pulumi.String("SERVERLESS"),
			CloudRun: &compute.RegionNetworkEndpointGroupCloudRunArgs{
				// TODO fix. this should be neg per backend/frontend
				Service: pulumi.String(serviceName),
			},
		})
		if err != nil {
			return nil, err
		}

		// Only create health check for Cloud Run NEGs
		healthCheckName := f.newResourceName(serviceName, "health-check", 100)
		healthCheck, err = compute.NewHealthCheck(ctx, healthCheckName, &compute.HealthCheckArgs{
			Description: pulumi.String(fmt.Sprintf("Health check for %s backend service", serviceName)),
			Project:     pulumi.String(project),
			HttpHealthCheck: &compute.HealthCheckHttpHealthCheckArgs{
				Port: pulumi.Int(443),
				// TODO make configurable
				RequestPath: pulumi.String("/api/v1/healthz"),
			},
			CheckIntervalSec:   pulumi.Int(5),
			TimeoutSec:         pulumi.Int(3),
			HealthyThreshold:   pulumi.Int(2),
			UnhealthyThreshold: pulumi.Int(3),
		})
		if err != nil {
			return nil, err
		}
		ctx.Export("load_balancer_health_check_id", healthCheck.ID())
		ctx.Export("load_balancer_health_check_uri", healthCheck.SelfLink)
	}
	if err != nil {
		return nil, err
	}
	ctx.Export("load_balancer_network_endpoint_group_id", neg.ID())
	ctx.Export("load_balancer_network_endpoint_group_uri", neg.SelfLink)
	f.neg = neg

	// Create the LB's backend service
	serviceArgs := &compute.BackendServiceArgs{
		Description:         pulumi.String(fmt.Sprintf("service backend for %s", serviceName)),
		Project:             pulumi.String(project),
		LoadBalancingScheme: pulumi.String("EXTERNAL"),
		Backends: compute.BackendServiceBackendArray{
			&compute.BackendServiceBackendArgs{
				// Point the backend service to the NEG
				Group: neg.SelfLink,
			},
		},
		// TODO allow enabling IAP (Identity Aware Proxy)
	}

	// Attach Cloud Armor policy if enabled
	if policy != nil {
		serviceArgs.SecurityPolicy = policy.SelfLink
	}
	// Only add health check for Cloud Run NEGs
	if healthCheck != nil {
		serviceArgs.HealthChecks = healthCheck.SelfLink
	}

	backendServiceName := f.newResourceName(serviceName, "backend-service", 100)
	service, err := compute.NewBackendService(ctx, backendServiceName, serviceArgs)
	if err != nil {
		return nil, err
	}
	ctx.Export("load_balancer_backend_service_id", service.ID())
	ctx.Export("load_balancer_backend_service_uri", service.SelfLink)

	urlMapName := f.newResourceName(serviceName, "url-map", 100)
	backendURLMap, err := compute.NewURLMap(ctx, urlMapName, &compute.URLMapArgs{
		Description:    pulumi.String(fmt.Sprintf("URL map to LB traffic for %s", serviceName)),
		Project:        pulumi.String(project),
		DefaultService: service.SelfLink,
	})
	if err != nil {
		return nil, err
	}
	ctx.Export("load_balancer_url_map_id", backendURLMap.ID())
	ctx.Export("load_balancer_url_map_uri", backendURLMap.SelfLink)

	return backendURLMap, nil
}

// createGlobalInternetEntrypoint creates a global IP address and forwarding rule for external traffic
func (f *FullStack) createGlobalInternetEntrypoint(ctx *pulumi.Context, serviceName, domainURL, project string, httpsProxy *compute.TargetHttpsProxy) error {
	labels := mergeLabels(f.Labels, pulumi.StringMap{
		"load_balancer": pulumi.String("true"),
	})

	ipAddressName := f.newResourceName(serviceName, "global-ip", 100)
	ipAddress, err := compute.NewGlobalAddress(ctx, ipAddressName, &compute.GlobalAddressArgs{
		Project:     pulumi.String(project),
		Description: pulumi.String(fmt.Sprintf("IP address for %s", serviceName)),
		IpVersion:   pulumi.String("IPV4"),
		Labels:      labels,
	})
	if err != nil {
		return err
	}
	ctx.Export("load_balancer_global_address_id", ipAddress.ID())
	ctx.Export("load_balancer_global_address_uri", ipAddress.SelfLink)
	ctx.Export("load_balancer_global_address_ip_address", ipAddress.Address)

	// https://cloud.google.com/load-balancing/docs/https#forwarding-rule
	forwardingRuleName := f.newResourceName(serviceName, "https-forwarding", 100)
	trafficRule, err := compute.NewGlobalForwardingRule(ctx, forwardingRuleName, &compute.GlobalForwardingRuleArgs{
		Description:         pulumi.String(fmt.Sprintf("HTTPS forwarding rule to LB traffic for %s", serviceName)),
		Project:             pulumi.String(project),
		PortRange:           pulumi.String("443"),
		LoadBalancingScheme: pulumi.String("EXTERNAL"),
		Target:              httpsProxy.SelfLink,
		IpAddress:           ipAddress.Address,
		Labels:              labels,
	})
	if err != nil {
		return err
	}
	ctx.Export("load_balancer_global_forwarding_rule_id", trafficRule.ID())
	ctx.Export("load_balancer_global_forwarding_rule_uri", trafficRule.SelfLink)
	ctx.Export("load_balancer_global_forwarding_rule_ip_address", trafficRule.IpAddress)

	// Store the forwarding rule in the FullStack struct
	f.globalForwardingRule = trafficRule

	// Create DNS record for the global IP address
	dnsRecord, err := f.createDNSRecord(ctx, serviceName, domainURL, project, ipAddress.Address)
	if err != nil {
		return err
	}
	f.dnsRecord = dnsRecord

	return nil
}

// lookupDNSZone finds the appropriate DNS managed zone for the given domain
func (f *FullStack) lookupDNSZone(ctx *pulumi.Context, domainURL, project string) (string, error) {
	// Get all managed zones in the project
	managedZones, err := dns.GetManagedZones(ctx, &dns.GetManagedZonesArgs{
		Project: &project,
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
		return "", fmt.Errorf("no managed zone found for domain %s in project %s", domainURL, project)
	}

	if err := ctx.Log.Debug(fmt.Sprintf("Found managed zone %s for domain %s", targetZoneName, domainURL), nil); err != nil {
		log.Printf("failed to log managed zone with Pulumi context: %v", err)
	}

	return targetZoneName, nil
}

// createDNSRecord creates a DNS A record for the given domain and IP address
func (f *FullStack) createDNSRecord(ctx *pulumi.Context, serviceName, domainURL, project string, ipAddress pulumi.StringOutput) (*dns.RecordSet, error) {
	// Look up the DNS managed zone for the domain
	managedZoneName, err := f.lookupDNSZone(ctx, domainURL, project)
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
