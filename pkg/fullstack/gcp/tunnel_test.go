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

		connectorNetwork := fullstack.GetConnectorNetwork()
		require.NotNil(t, connectorNetwork, "Connector network should be created upfront")
		assert.NotNil(t, connectorNetwork.GetRunAppZone())

		tunnelConnector, err := fullstack.DeployTunnelConnector(ctx, connector.Cloudflared(pulumi.String(testTunnelAuth)))
		require.NoError(t, err)
		require.NotNil(t, tunnelConnector.GetWorkerPool())
		assert.Equal(t, map[string]string{
			"tunnel-connector": "true",
		}, awaitOutput(t, ctx, tunnelConnector.GetWorkerPool().Labels), "Connector resources should be labeled")
		assert.Equal(t, tunnelConnector, fullstack.GetTunnelConnectors()["cloudflared"])

		_, err = fullstack.DeployTunnelConnector(ctx, connector.Cloudflared(pulumi.String(testTunnelAuth)))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tunnel connector cloudflared already deployed")

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))
	require.NoError(t, err)
}

func TestNewFullStack_TunnelConnectorRequiresTunnelMode(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		// Default connector: domain mappings, public ingress
		fullstack, err := gcp.NewFullStack(ctx, "test-fullstack", externalWAFArgs(""))
		require.NoError(t, err)

		assert.Equal(t, "INGRESS_TRAFFIC_ALL", awaitOutput(t, ctx, fullstack.GetBackendService().Ingress))
		assert.NotNil(t, fullstack.GetBackendDomainMapping())
		assert.Nil(t, fullstack.GetConnectorNetwork())

		_, err = fullstack.DeployTunnelConnector(ctx, connector.Cloudflared(pulumi.String(testTunnelAuth)))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `tunnel connectors require external WAF connector "tunnel"`)

		return nil
	}, pulumi.WithMocks("project", "stack", &fullstackMocks{}))
	require.NoError(t, err)
}

func TestNewFullStack_ExternalWAFConnectorValidation(t *testing.T) {
	t.Parallel()

	withoutExternalWAF := externalWAFArgs(gcp.ExternalWAFConnectorTunnel)
	withoutExternalWAF.Network.EnableExternalWAF = false

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
		{
			name:        "tunnel without external WAF",
			args:        withoutExternalWAF,
			expectedErr: `external WAF connector "tunnel" requires EnableExternalWAF`,
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
