package gcp

import (
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"

	"github.com/davidmontoyago/pulumi-gcp-fullstack/pkg/fullstack/gcp/connector"
)

// Cloud Run ingress settings.
const (
	ingressAll                  = "INGRESS_TRAFFIC_ALL"
	ingressInternalOnly         = "INGRESS_TRAFFIC_INTERNAL_ONLY"
	ingressInternalLoadBalancer = "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER"
)

// DeployTunnelConnector deploys a tunnel connector that proxies an external network (e.g. the
// Cloudflare edge) to the FullStack's Cloud Run services. Call it once per connector, after
// NewFullStack.
//
// The first call creates the network shared by all connectors (NetworkArgs.TunnelConnectorNetwork).
// It works with any ExternalWAFConnector, which only decides the services' ingress:
//   - ExternalWAFConnectorDomainMapping: services stay public. Use it to stage a connector before
//     switching to the tunnel.
//   - ExternalWAFConnectorTunnel: services are internal-only, so connectors are the only way in.
//
// E.g.: fullstack.DeployTunnelConnector(ctx, connector.Cloudflared(edge.GetTunnelToken()))
func (f *FullStack) DeployTunnelConnector(ctx *pulumi.Context, args *connector.Args) (*connector.TunnelConnector, error) {
	if args == nil {
		return nil, fmt.Errorf("tunnel connector args are required")
	}
	if _, exists := f.tunnelConnectors[args.Name]; exists {
		return nil, fmt.Errorf("tunnel connector %s already deployed", args.Name)
	}

	if f.connectorNetwork == nil {
		connectorNetwork, err := connector.NewNetwork(ctx, f.connectorScope(), f.connectorNetworkArgs)
		if err != nil {
			return nil, fmt.Errorf("failed to deploy tunnel connector network: %w", err)
		}
		f.connectorNetwork = connectorNetwork
	}

	tunnelConnector, err := connector.Deploy(ctx, f.connectorScope(), f.connectorNetwork, args)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy tunnel connector: %w", err)
	}
	f.tunnelConnectors[args.Name] = tunnelConnector

	return tunnelConnector, nil
}

// GetTunnelConnectors returns the deployed tunnel connectors keyed by name.
func (f *FullStack) GetTunnelConnectors() map[string]*connector.TunnelConnector {
	return f.tunnelConnectors
}

// GetConnectorNetwork returns the network shared by tunnel connectors, or nil before the first
// DeployTunnelConnector call.
func (f *FullStack) GetConnectorNetwork() *connector.Network {
	return f.connectorNetwork
}

// tunnelConnectorsEnabled reports whether the services are reachable only through tunnel connectors.
func (f *FullStack) tunnelConnectorsEnabled() bool {
	return f.externalWAFConnector == ExternalWAFConnectorTunnel
}

func (f *FullStack) connectorScope() connector.Scope {
	return connector.Scope{
		Project: f.Project,
		Region:  f.Region,
		Namer:   f.Namer,
		Labels: mergeLabels(f.Labels, pulumi.StringMap{
			"tunnel-connector": pulumi.String("true"),
		}),
		Parent: f,
	}
}

// instanceIngress returns the Cloud Run ingress for the backend and frontend instances.
func (f *FullStack) instanceIngress(enablePublicIngress bool) string {
	if f.tunnelConnectorsEnabled() {
		// Reachable only through the tunnel connectors' VPC path.
		return ingressInternalOnly
	}
	if enablePublicIngress {
		// The instance is likely using an external WAF. Make it reachable.
		return ingressAll
	}

	return ingressInternalLoadBalancer
}

// resolveExternalWAFConnector validates and defaults NetworkArgs.ExternalWAFConnector.
func resolveExternalWAFConnector(network *NetworkArgs) (string, error) {
	if network == nil || network.ExternalWAFConnector == "" {
		return ExternalWAFConnectorDomainMapping, nil
	}

	switch network.ExternalWAFConnector {
	case ExternalWAFConnectorDomainMapping, ExternalWAFConnectorTunnel:
		return network.ExternalWAFConnector, nil
	default:
		return "", fmt.Errorf("invalid external WAF connector %q", network.ExternalWAFConnector)
	}
}
