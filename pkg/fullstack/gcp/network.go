package gcp

import (
	"fmt"

	apigateway "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/apigateway"
	compute "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
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

func (f *FullStack) newHTTPSProxy(ctx *pulumi.Context, serviceName, domainName, project string, privateTraffic bool, backendURLMap *compute.URLMap) error {
	tlsCertName := f.newResourceName(serviceName, "tls-cert", 100)
	certificate, err := compute.NewManagedSslCertificate(ctx, tlsCertName, &compute.ManagedSslCertificateArgs{
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
		// https://cloud.google.com/load-balancing/docs/https#forwarding-rule
		forwardingRuleName := f.newResourceName(serviceName, "https-forwarding", 100)
		trafficRule, err := compute.NewGlobalForwardingRule(ctx, forwardingRuleName, &compute.GlobalForwardingRuleArgs{
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
	if apiGateway != nil {
		// Create NEG for API Gateway
		gatewayNegName := f.newResourceName(serviceName, "gateway-neg", 100)
		neg, err = compute.NewRegionNetworkEndpointGroup(ctx, gatewayNegName, &compute.RegionNetworkEndpointGroupArgs{
			Description:         pulumi.String(fmt.Sprintf("NEG to route LB traffic to API Gateway for %s", serviceName)),
			Project:             pulumi.String(project),
			Region:              pulumi.String(region),
			NetworkEndpointType: pulumi.String("SERVERLESS"),
			ServerlessDeployment: &compute.RegionNetworkEndpointGroupServerlessDeploymentArgs{
				Platform: pulumi.String("apigateway.googleapis.com"),
				Resource: apiGateway.GatewayId,
			},
		})
	} else {
		// Create NEG for Cloud Run
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
	}
	if err != nil {
		return nil, err
	}
	ctx.Export("load_balancer_network_endpoint_group_id", neg.ID())
	ctx.Export("load_balancer_network_endpoint_group_uri", neg.SelfLink)
	f.neg = neg

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

	backendServiceName := f.newResourceName(serviceName, "backend-service", 100)
	service, err := compute.NewBackendService(ctx, backendServiceName, serviceArgs)
	if err != nil {
		return nil, err
	}
	ctx.Export("load_balancer_backend_service_id", neg.ID())
	ctx.Export("load_balancer_backend_service_uri", neg.SelfLink)

	// TODO create compute address if enabled
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
