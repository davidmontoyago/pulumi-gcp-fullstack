package connector

import (
	"errors"
	"fmt"

	"github.com/pulumi/pulumi-gcp/sdk/v10/go/gcp/compute"
	"github.com/pulumi/pulumi-gcp/sdk/v10/go/gcp/dns"
	"github.com/pulumi/pulumi-gcp/sdk/v10/go/gcp/projects"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const defaultSubnetCIDR = "10.10.0.0/26"

// privateGoogleAccessVIPs are the private.googleapis.com VIPs, only routable from within Google Cloud.
// Requests to them are routed through the VPC by the private-ranges-only egress setting.
// See: https://docs.cloud.google.com/vpc/docs/configure-private-google-access
var privateGoogleAccessVIPs = []string{"199.36.153.8", "199.36.153.9", "199.36.153.10", "199.36.153.11"}

// Network is the VPC path from connectors to internal-ingress Cloud Run services.
type Network struct {
	networkName pulumi.StringOutput
	subnetName  pulumi.StringOutput

	network    *compute.Network
	subnetwork *compute.Subnetwork
	runAppZone *dns.ManagedZone
	runAppRecs *dns.RecordSet
}

// NewNetwork creates the connectors network: a dedicated network and subnet with Private Google
// Access (unless an existing one is given), and a private run.app zone resolving to the
// private.googleapis.com VIPs.
func NewNetwork(ctx *pulumi.Context, scope Scope, args *NetworkArgs) (*Network, error) {
	if args == nil {
		args = &NetworkArgs{}
	}
	if (args.NetworkName == "") != (args.SubnetName == "") {
		return nil, errors.New("connector network name and subnet name must be set together")
	}

	dnsAPI, err := projects.NewService(ctx, scope.Namer.NewResourceName("connector", "dns-api", 63), &projects.ServiceArgs{
		Project:          pulumi.String(scope.Project),
		Service:          pulumi.String("dns.googleapis.com"),
		DisableOnDestroy: pulumi.Bool(false),
	}, pulumi.Parent(scope.Parent), pulumi.RetainOnDelete(true))
	if err != nil {
		return nil, fmt.Errorf("failed to enable DNS API: %w", err)
	}

	network := &Network{
		networkName: pulumi.String(args.NetworkName).ToStringOutput(),
		subnetName:  pulumi.String(args.SubnetName).ToStringOutput(),
	}

	if args.NetworkName == "" {
		err = network.createDedicatedNetwork(ctx, scope, args)
		if err != nil {
			return nil, err
		}
	}

	zoneName := scope.Namer.NewResourceName("connector", "run-app-zone", 63)
	network.runAppZone, err = dns.NewManagedZone(ctx, zoneName, &dns.ManagedZoneArgs{
		Project:     pulumi.String(scope.Project),
		Name:        pulumi.String(zoneName),
		DnsName:     pulumi.String("run.app."),
		Description: pulumi.String("Routes run.app through Private Google Access for tunnel connectors"),
		Visibility:  pulumi.String("private"),
		Labels:      scope.Labels,
		PrivateVisibilityConfig: &dns.ManagedZonePrivateVisibilityConfigArgs{
			Networks: dns.ManagedZonePrivateVisibilityConfigNetworkArray{
				&dns.ManagedZonePrivateVisibilityConfigNetworkArgs{
					NetworkUrl: pulumi.Sprintf("projects/%s/global/networks/%s", scope.Project, network.networkName),
				},
			},
		},
	}, pulumi.Parent(scope.Parent), pulumi.DependsOn([]pulumi.Resource{dnsAPI}))
	if err != nil {
		return nil, fmt.Errorf("failed to create private run.app zone: %w", err)
	}

	network.runAppRecs, err = dns.NewRecordSet(ctx, scope.Namer.NewResourceName("connector", "run-app-records", 63), &dns.RecordSetArgs{
		Project:     pulumi.String(scope.Project),
		ManagedZone: network.runAppZone.Name,
		Name:        pulumi.String("*.run.app."),
		Type:        pulumi.String("A"),
		Ttl:         pulumi.Int(300),
		Rrdatas:     pulumi.ToStringArray(privateGoogleAccessVIPs),
	}, pulumi.Parent(scope.Parent))
	if err != nil {
		return nil, fmt.Errorf("failed to create run.app records: %w", err)
	}

	return network, nil
}

// createDedicatedNetwork creates a custom-mode network and a subnet with Private Google Access.
// No firewall rules: egress is allowed by default and nothing ingresses.
func (n *Network) createDedicatedNetwork(ctx *pulumi.Context, scope Scope, args *NetworkArgs) error {
	cidr := args.IPCidrRange
	if cidr == "" {
		cidr = defaultSubnetCIDR
	}

	networkName := scope.Namer.NewResourceName("connector", "network", 63)
	network, err := compute.NewNetwork(ctx, networkName, &compute.NetworkArgs{
		Project:               pulumi.String(scope.Project),
		Name:                  pulumi.String(networkName),
		Description:           pulumi.String("Dedicated network for tunnel connectors"),
		AutoCreateSubnetworks: pulumi.Bool(false),
	}, pulumi.Parent(scope.Parent))
	if err != nil {
		return fmt.Errorf("failed to create connector network: %w", err)
	}

	subnetName := scope.Namer.NewResourceName("connector", "subnet", 63)
	subnetwork, err := compute.NewSubnetwork(ctx, subnetName, &compute.SubnetworkArgs{
		Project:               pulumi.String(scope.Project),
		Name:                  pulumi.String(subnetName),
		Region:                pulumi.String(scope.Region),
		Network:               network.ID(),
		IpCidrRange:           pulumi.String(cidr),
		PrivateIpGoogleAccess: pulumi.Bool(true),
	}, pulumi.Parent(scope.Parent))
	if err != nil {
		return fmt.Errorf("failed to create connector subnet: %w", err)
	}

	n.network = network
	n.subnetwork = subnetwork
	n.networkName = network.Name
	n.subnetName = subnetwork.Name

	return nil
}

// GetNetwork returns the dedicated network, or nil when an existing network is used.
func (n *Network) GetNetwork() *compute.Network {
	return n.network
}

// GetSubnetwork returns the dedicated subnet, or nil when an existing network is used.
func (n *Network) GetSubnetwork() *compute.Subnetwork {
	return n.subnetwork
}

// GetRunAppZone returns the private run.app DNS zone.
func (n *Network) GetRunAppZone() *dns.ManagedZone {
	return n.runAppZone
}

// GetRunAppRecordSet returns the run.app wildcard record set.
func (n *Network) GetRunAppRecordSet() *dns.RecordSet {
	return n.runAppRecs
}
