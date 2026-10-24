package gcp_test

import (
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/internals"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davidmontoyago/pulumi-gcp-fullstack/pkg/fullstack/gcp"
	"github.com/davidmontoyago/pulumi-gcp-fullstack/pkg/fullstack/gcp/connector"
)

const testTunnelAuth = "eyJ-test-tunnel-token"

func externalWAFArgs(externalWAFConnector string) *gcp.FullStackArgs {
	return &gcp.FullStackArgs{
		Project:       testProjectName,
		Region:        testRegion,
		BackendName:   backendServiceName,
		BackendImage:  pulumi.String("gcr.io/test-project/backend:latest"),
		FrontendName:  frontendServiceName,
		FrontendImage: pulumi.String("gcr.io/test-project/frontend:latest"),
		Backend: &gcp.BackendArgs{
			InstanceArgs: &gcp.InstanceArgs{EnablePublicIngress: true},
		},
		Frontend: &gcp.FrontendArgs{
			InstanceArgs: &gcp.InstanceArgs{EnablePublicIngress: true},
		},
		Network: &gcp.NetworkArgs{
			DomainURL:            testDomainURL,
			EnableExternalWAF:    true,
			ExternalWAFConnector: externalWAFConnector,
		},
	}
}

// awaitOutput resolves a Pulumi output inside a mocked program.
func awaitOutput(t *testing.T, ctx *pulumi.Context, output pulumi.Output) interface{} {
	t.Helper()

	result, err := internals.UnsafeAwaitOutput(ctx.Context(), output)
	require.NoError(t, err)

	return result.Value
}

func TestNewFullStack_WithTunnelConnector(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		fullstack, err := gcp.NewFullStack(ctx, "test-fullstack", externalWAFArgs(gcp.ExternalWAFConnectorTunnel))
		require.NoError(t, err)

		// Instances are private and only reachable through the connectors
		assert.Equal(t, "INGRESS_TRAFFIC_INTERNAL_ONLY", awaitOutput(t, ctx, fullstack.GetBackendService().Ingress))
		assert.Equal(t, "INGRESS_TRAFFIC_INTERNAL_ONLY", awaitOutput(t, ctx, fullstack.GetFrontendService().Ingress))
		assert.Nil(t, fullstack.GetBackendDomainMapping(), "Domain mappings should not be created with tunnel connectors")
		assert.Nil(t, fullstack.GetFrontendDomainMapping())

		assert.Nil(t, fullstack.GetConnectorNetwork(), "Connector network should not exist before the first connector")

		tunnelConnector, err := fullstack.DeployTunnelConnector(ctx, connector.Cloudflared(pulumi.String(testTunnelAuth)))
		require.NoError(t, err)

		connectorNetwork := fullstack.GetConnectorNetwork()
		require.NotNil(t, connectorNetwork, "First connector should create the connector network")
		assert.NotNil(t, connectorNetwork.GetNetwork(), "Connector network should default to a dedicated network")
		assert.NotNil(t, connectorNetwork.GetRunAppZone())
		require.NotNil(t, tunnelConnector.GetWorkerPool())
		assert.Equal(t, map[string]string{
			"tunnel-connector": "true",
		}, awaitOutput(t, ctx, tunnelConnector.GetWorkerPool().Labels), "Connector resources should be labeled")
		assert.Equal(t, tunnelConnector, fullstack.GetTunnelConnectors()["cloudflared"])

		_, err = fullstack.DeployTunnelConnector(ctx, connector.Cloudflared(pulumi.String(testTunnelAuth)))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tunnel connector cloudflared already deployed")

		_, err = fullstack.DeployTunnelConnector(ctx, &connector.Args{
			Name:       "tailscale",
			Image:      "docker.io/tailscale/tailscale:v1.90.0",
			Credential: &connector.Credential{Value: pulumi.String(testTunnelAuth), EnvVar: "TS_AUTHKEY"},
		})
		require.NoError(t, err)
		assert.Same(t, connectorNetwork, fullstack.GetConnectorNetwork(), "Connectors should share one network")
		assert.Len(t, fullstack.GetTunnelConnectors(), 2)

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))
	require.NoError(t, err)
}

func TestNewFullStack_TunnelModeImpliesExternalWAF(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		// Only the tunnel connector is set: secure by default
		args := externalWAFArgs(gcp.ExternalWAFConnectorTunnel)
		args.Network.EnableExternalWAF = false
		args.Backend.EnablePublicIngress = false
		args.Frontend.EnablePublicIngress = false

		fullstack, err := gcp.NewFullStack(ctx, "test-fullstack", args)
		require.NoError(t, err)

		assert.Nil(t, fullstack.GetGlobalForwardingRule(), "Tunnel mode should not create a load balancer")
		assert.Nil(t, fullstack.GetRegionalForwardingRule())
		assert.Nil(t, fullstack.GetURLMap())
		assert.Nil(t, fullstack.GetCertificate())
		assert.Nil(t, fullstack.GetBackendNEG())
		assert.Nil(t, fullstack.GetBackendDomainMapping())
		assert.Equal(t, "INGRESS_TRAFFIC_INTERNAL_ONLY", awaitOutput(t, ctx, fullstack.GetBackendService().Ingress))
		assert.Equal(t, "INGRESS_TRAFFIC_INTERNAL_ONLY", awaitOutput(t, ctx, fullstack.GetFrontendService().Ingress))

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))
	require.NoError(t, err)
}

func TestNewFullStack_TunnelConnectorWithDomainMapping(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		// Default connector: domain mappings, public ingress
		fullstack, err := gcp.NewFullStack(ctx, "test-fullstack", externalWAFArgs(""))
		require.NoError(t, err)

		// Staging a connector doesn't change the services' ingress
		tunnelConnector, err := fullstack.DeployTunnelConnector(ctx, connector.Cloudflared(pulumi.String(testTunnelAuth)))
		require.NoError(t, err)
		assert.NotNil(t, tunnelConnector.GetWorkerPool())
		assert.NotNil(t, fullstack.GetConnectorNetwork())

		assert.Equal(t, "INGRESS_TRAFFIC_ALL", awaitOutput(t, ctx, fullstack.GetBackendService().Ingress))
		assert.Equal(t, "INGRESS_TRAFFIC_ALL", awaitOutput(t, ctx, fullstack.GetFrontendService().Ingress))
		assert.NotNil(t, fullstack.GetBackendDomainMapping())
		assert.NotNil(t, fullstack.GetFrontendDomainMapping())

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))
	require.NoError(t, err)
}

func TestNewFullStack_TunnelConnectorWithExistingNetwork(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		args := externalWAFArgs(gcp.ExternalWAFConnectorTunnel)
		args.Network.TunnelConnectorNetwork = &connector.NetworkArgs{
			NetworkName: "serverless",
			SubnetName:  "serverless-us-central1",
		}
		fullstack, err := gcp.NewFullStack(ctx, "test-fullstack", args)
		require.NoError(t, err)

		_, err = fullstack.DeployTunnelConnector(ctx, connector.Cloudflared(pulumi.String(testTunnelAuth)))
		require.NoError(t, err)

		connectorNetwork := fullstack.GetConnectorNetwork()
		require.NotNil(t, connectorNetwork)
		assert.Nil(t, connectorNetwork.GetNetwork(), "Existing network should not be created")
		assert.Nil(t, connectorNetwork.GetSubnetwork())
		assert.NotNil(t, connectorNetwork.GetRunAppZone())

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))
	require.NoError(t, err)
}

func TestFullStack_DeployTunnelConnectorErrors(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		fullstack, err := gcp.NewFullStack(ctx, "test-fullstack", externalWAFArgs(gcp.ExternalWAFConnectorTunnel))
		require.NoError(t, err)

		_, err = fullstack.DeployTunnelConnector(ctx, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tunnel connector args are required")
		assert.Nil(t, fullstack.GetConnectorNetwork(), "Nil args should not create the network")

		_, err = fullstack.DeployTunnelConnector(ctx, &connector.Args{Name: "proxy"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to deploy tunnel connector: image is required")
		assert.Empty(t, fullstack.GetTunnelConnectors(), "Failed connectors should not be registered")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))
	require.NoError(t, err)
}

func TestFullStack_DeployTunnelConnectorNetworkError(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		args := externalWAFArgs(gcp.ExternalWAFConnectorTunnel)
		args.Network.TunnelConnectorNetwork = &connector.NetworkArgs{NetworkName: "serverless"}
		fullstack, err := gcp.NewFullStack(ctx, "test-fullstack", args)
		require.NoError(t, err)

		_, err = fullstack.DeployTunnelConnector(ctx, connector.Cloudflared(pulumi.String(testTunnelAuth)))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to deploy tunnel connector network")
		assert.Nil(t, fullstack.GetConnectorNetwork())
		assert.Empty(t, fullstack.GetTunnelConnectors())

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))
	require.NoError(t, err)
}

func TestNewFullStack_ExternalWAFConnectorValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        *gcp.FullStackArgs
		expectedErr string
	}{
		{
			name:        "invalid connector",
			args:        externalWAFArgs("ngrok"),
			expectedErr: `invalid external WAF connector "ngrok"`,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				_, err := gcp.NewFullStack(ctx, "test-fullstack", testCase.args)
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.expectedErr)

				return nil
			}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))
			require.NoError(t, err)
		})
	}
}
