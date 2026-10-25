package connector_test

import (
	"testing"

	namer "github.com/davidmontoyago/commodity-namer"
	cloudrunv2 "github.com/pulumi/pulumi-gcp/sdk/v10/go/gcp/cloudrunv2"
	"github.com/pulumi/pulumi-gcp/sdk/v10/go/gcp/dns"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/internals"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davidmontoyago/pulumi-gcp-fullstack/pkg/fullstack/gcp/connector"
)

const (
	testProject       = "test-project"
	testRegion        = "us-central1"
	testTunnelAuth    = "eyJ-test-tunnel-token"
	testProxyName     = "proxy"
	testProxyImage    = "proxy:1.0"
	testProxyAuthFile = "/etc/proxy/token"
)

type connectorMocks struct{}

func (m *connectorMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	outputs := map[string]interface{}{}
	for k, v := range args.Inputs {
		outputs[string(k)] = v
	}

	switch args.TypeToken {
	case "gcp:serviceaccount/account:Account":
		outputs["email"] = args.Name + "@test-project.iam.gserviceaccount.com"
	case "gcp:secretmanager/secret:Secret":
		outputs["secretId"] = args.Name
	case "gcp:secretmanager/secretVersion:SecretVersion":
		outputs["version"] = "1"
	case "gcp:compute/network:Network", "gcp:compute/subnetwork:Subnetwork", "gcp:dns/managedZone:ManagedZone":
		outputs["name"] = args.Name
	}

	return args.Name + "_id", resource.NewPropertyMapFromMap(outputs), nil
}

func (m *connectorMocks) Call(_ pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return resource.PropertyMap{}, nil
}

func testScope() connector.Scope {
	return connector.Scope{
		Project: testProject,
		Region:  testRegion,
		Namer:   namer.New("test"),
		Labels:  pulumi.StringMap{"env": pulumi.String("test")},
	}
}

// awaitOutput resolves a Pulumi output inside a mocked program.
func awaitOutput(t *testing.T, ctx *pulumi.Context, output pulumi.Output) internals.UnsafeAwaitOutputResult {
	t.Helper()

	result, err := internals.UnsafeAwaitOutput(ctx.Context(), output)
	require.NoError(t, err)

	return result
}

func TestNewNetwork_Dedicated(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		network, err := connector.NewNetwork(ctx, testScope(), nil)
		require.NoError(t, err)

		subnet := network.GetSubnetwork()
		require.NotNil(t, network.GetNetwork(), "Dedicated network should be created")
		require.NotNil(t, subnet)
		assert.Equal(t, false, *awaitOutput(t, ctx, network.GetNetwork().AutoCreateSubnetworks).Value.(*bool))
		assert.Equal(t, "10.10.0.0/26", awaitOutput(t, ctx, subnet.IpCidrRange).Value)
		assert.Equal(t, true, awaitOutput(t, ctx, subnet.PrivateIpGoogleAccess).Value, "Subnet needs Private Google Access")

		zone := network.GetRunAppZone()
		assert.Equal(t, "run.app.", awaitOutput(t, ctx, zone.DnsName).Value)
		assert.Equal(t, "private", *awaitOutput(t, ctx, zone.Visibility).Value.(*string))
		visibility := awaitOutput(t, ctx, zone.PrivateVisibilityConfig).Value.(*dns.ManagedZonePrivateVisibilityConfig)
		require.Len(t, visibility.Networks, 1)
		assert.Equal(t, "projects/test-project/global/networks/test-connector-network", visibility.Networks[0].NetworkUrl)

		records := network.GetRunAppRecordSet()
		assert.Equal(t, "*.run.app.", awaitOutput(t, ctx, records.Name).Value)
		assert.Equal(t, "A", awaitOutput(t, ctx, records.Type).Value)
		assert.Equal(t, []string{"199.36.153.8", "199.36.153.9", "199.36.153.10", "199.36.153.11"}, awaitOutput(t, ctx, records.Rrdatas).Value)

		return nil
	}, pulumi.WithMocks("project", "stack", &connectorMocks{}))
	require.NoError(t, err)
}

func TestNewNetwork_CustomCIDR(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		network, err := connector.NewNetwork(ctx, testScope(), &connector.NetworkArgs{IPCidrRange: "10.20.0.0/26"})
		require.NoError(t, err)

		assert.Equal(t, "10.20.0.0/26", awaitOutput(t, ctx, network.GetSubnetwork().IpCidrRange).Value)

		return nil
	}, pulumi.WithMocks("project", "stack", &connectorMocks{}))
	require.NoError(t, err)
}

func TestNewNetwork_Existing(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		network, err := connector.NewNetwork(ctx, testScope(), &connector.NetworkArgs{
			NetworkName: "serverless",
			SubnetName:  "serverless-us-central1",
		})
		require.NoError(t, err)

		assert.Nil(t, network.GetNetwork(), "Existing network should not be created")
		assert.Nil(t, network.GetSubnetwork())
		visibility := awaitOutput(t, ctx, network.GetRunAppZone().PrivateVisibilityConfig).Value.(*dns.ManagedZonePrivateVisibilityConfig)
		assert.Equal(t, "projects/test-project/global/networks/serverless", visibility.Networks[0].NetworkUrl)

		return nil
	}, pulumi.WithMocks("project", "stack", &connectorMocks{}))
	require.NoError(t, err)
}

func TestDeploy_Cloudflared(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		network, err := connector.NewNetwork(ctx, testScope(), nil)
		require.NoError(t, err)

		tunnelConnector, err := connector.Deploy(ctx, testScope(), network, connector.Cloudflared(pulumi.String(testTunnelAuth)))
		require.NoError(t, err)
		assert.Equal(t, "cloudflared", tunnelConnector.GetName())
		require.NotNil(t, tunnelConnector.GetCredentialSecret())
		assert.Equal(t, "test-cloudflared-credential", awaitOutput(t, ctx, tunnelConnector.GetCredentialSecret().SecretId).Value)

		// Credential is secret and readable only by the connector's service account
		credential := awaitOutput(t, ctx, tunnelConnector.GetCredentialSecretVersion().SecretData)
		assert.Equal(t, testTunnelAuth, *credential.Value.(*string))
		assert.True(t, credential.Secret, "Credential should be marked as secret")

		workerPool := tunnelConnector.GetWorkerPool()
		require.NotNil(t, workerPool)

		assert.Equal(t, false, *awaitOutput(t, ctx, workerPool.DeletionProtection).Value.(*bool), "Deletion protection should default to false")

		scaling := awaitOutput(t, ctx, workerPool.Scaling).Value.(cloudrunv2.WorkerPoolScaling)
		assert.Equal(t, "MANUAL", *scaling.ScalingMode)
		assert.Equal(t, 1, *scaling.ManualInstanceCount)

		template := awaitOutput(t, ctx, workerPool.Template).Value.(cloudrunv2.WorkerPoolTemplate)
		assert.Equal(t, awaitOutput(t, ctx, tunnelConnector.GetServiceAccount().Email).Value, *template.ServiceAccount)

		require.NotNil(t, template.VpcAccess)
		assert.Equal(t, "PRIVATE_RANGES_ONLY", *template.VpcAccess.Egress)
		require.Len(t, template.VpcAccess.NetworkInterfaces, 1)
		assert.Equal(t, "test-connector-network", *template.VpcAccess.NetworkInterfaces[0].Network)
		assert.Equal(t, "test-connector-subnet", *template.VpcAccess.NetworkInterfaces[0].Subnetwork)

		require.Len(t, template.Containers, 1)
		container := template.Containers[0]
		assert.Equal(t, connector.CloudflaredImage, container.Image)
		assert.Equal(t, map[string]string{"cpu": "80m", "memory": "512Mi"}, container.Resources.Limits, "Should default to worker pool minimums")
		assert.Equal(t, []string{
			"tunnel", "--loglevel", "warn", "--protocol", "http2", "--metrics", "0.0.0.0:2000",
			"run", "--token-file", "/etc/cloudflared/token",
		}, container.Args)

		envVars := map[string]string{}
		for _, env := range container.Envs {
			envVars[env.Name] = *env.Value
		}
		assert.Equal(t, map[string]string{"GOMAXPROCS": "1", "GOMEMLIMIT": "400MiB"}, envVars, "Token should not be an env var")

		require.Len(t, container.VolumeMounts, 1)
		assert.Equal(t, "/etc/cloudflared", container.VolumeMounts[0].MountPath)

		require.Len(t, template.Volumes, 1)
		secretItems := template.Volumes[0].Secret.Items
		require.Len(t, secretItems, 1)
		assert.Equal(t, "token", secretItems[0].Path)
		assert.Equal(t, "1", *secretItems[0].Version, "Credential version should be pinned")
		assert.Equal(t, 0444, *secretItems[0].Mode)

		require.NotNil(t, container.StartupProbe)
		assert.Equal(t, "/ready", *container.StartupProbe.HttpGet.Path)
		assert.Equal(t, 2000, *container.StartupProbe.HttpGet.Port)
		require.NotNil(t, container.LivenessProbe)
		assert.Equal(t, "/ready", *container.LivenessProbe.HttpGet.Path)

		return nil
	}, pulumi.WithMocks("project", "stack", &connectorMocks{}))
	require.NoError(t, err)
}

func TestDeploy_EnvVarCredential(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		network, err := connector.NewNetwork(ctx, testScope(), nil)
		require.NoError(t, err)

		tunnelConnector, err := connector.Deploy(ctx, testScope(), network, &connector.Args{
			Name:     "tailscale",
			Image:    "docker.io/tailscale/tailscale:v1.90.0",
			Replicas: 2,
			ResourceLimits: pulumi.StringMap{
				"cpu":    pulumi.String("250m"),
				"memory": pulumi.String("512Mi"),
			},
			Credential: &connector.Credential{
				Value:  pulumi.String("tskey-auth-test"),
				EnvVar: "TS_AUTHKEY",
			},
		})
		require.NoError(t, err)

		workerPool := tunnelConnector.GetWorkerPool()
		scaling := awaitOutput(t, ctx, workerPool.Scaling).Value.(cloudrunv2.WorkerPoolScaling)
		assert.Equal(t, 2, *scaling.ManualInstanceCount)

		template := awaitOutput(t, ctx, workerPool.Template).Value.(cloudrunv2.WorkerPoolTemplate)
		assert.Empty(t, template.Volumes, "Env var credentials should not mount a volume")

		container := template.Containers[0]
		assert.Equal(t, "250m", container.Resources.Limits["cpu"])
		assert.Nil(t, container.StartupProbe)
		require.Len(t, container.Envs, 1)
		assert.Equal(t, "TS_AUTHKEY", container.Envs[0].Name)
		require.NotNil(t, container.Envs[0].ValueSource)
		assert.Equal(t, "1", *container.Envs[0].ValueSource.SecretKeyRef.Version)

		return nil
	}, pulumi.WithMocks("project", "stack", &connectorMocks{}))
	require.NoError(t, err)
}

func TestDeploy_Validation(t *testing.T) {
	t.Parallel()

	validCredential := &connector.Credential{Value: pulumi.String(testTunnelAuth), FilePath: testProxyAuthFile}

	tests := []struct {
		name        string
		args        *connector.Args
		expectedErr string
	}{
		{name: "nil args", args: nil, expectedErr: "connector args are required"},
		{name: "missing name", args: &connector.Args{Image: testProxyImage, Credential: validCredential}, expectedErr: "connector name is required"},
		{name: "missing image", args: &connector.Args{Name: testProxyName, Credential: validCredential}, expectedErr: "image is required"},
		{name: "missing credential", args: &connector.Args{Name: testProxyName, Image: testProxyImage}, expectedErr: "credential is required"},
		{
			name:        "missing credential value",
			args:        &connector.Args{Name: testProxyName, Image: testProxyImage, Credential: &connector.Credential{FilePath: testProxyAuthFile}},
			expectedErr: "credential is required",
		},
		{
			name: "neither file nor env var",
			args: &connector.Args{Name: testProxyName, Image: testProxyImage, Credential: &connector.Credential{
				Value: pulumi.String(testTunnelAuth),
			}},
			expectedErr: "exactly one of credential file path or env var",
		},
		{
			name: "file path at root",
			args: &connector.Args{Name: testProxyName, Image: testProxyImage, Credential: &connector.Credential{
				Value: pulumi.String(testTunnelAuth), FilePath: "/token",
			}},
			expectedErr: "must be absolute and inside a directory",
		},
		{
			name: "both file and env var",
			args: &connector.Args{Name: testProxyName, Image: testProxyImage, Credential: &connector.Credential{
				Value: pulumi.String(testTunnelAuth), FilePath: testProxyAuthFile, EnvVar: "TOKEN",
			}},
			expectedErr: "exactly one of credential file path or env var",
		},
		{
			name: "relative file path",
			args: &connector.Args{Name: testProxyName, Image: testProxyImage, Credential: &connector.Credential{
				Value: pulumi.String(testTunnelAuth), FilePath: "token",
			}},
			expectedErr: "must be absolute and inside a directory",
		},
		{
			name:        "negative replicas",
			args:        &connector.Args{Name: testProxyName, Image: testProxyImage, Credential: validCredential, Replicas: -1},
			expectedErr: "replicas must not be negative",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				network, err := connector.NewNetwork(ctx, testScope(), nil)
				require.NoError(t, err)

				_, err = connector.Deploy(ctx, testScope(), network, testCase.args)
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.expectedErr)

				return nil
			}, pulumi.WithMocks("project", "stack", &connectorMocks{}))
			require.NoError(t, err)
		})
	}
}

func TestNewNetwork_Validation(t *testing.T) {
	t.Parallel()

	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		_, err := connector.NewNetwork(ctx, testScope(), &connector.NetworkArgs{NetworkName: "serverless"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be set together")

		_, err = connector.Deploy(ctx, testScope(), nil, connector.Cloudflared(pulumi.String(testTunnelAuth)))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "connector network is required")

		return nil
	}, pulumi.WithMocks("project", "stack", &connectorMocks{}))
	require.NoError(t, err)
}
