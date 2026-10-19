package connector

import (
	"errors"
	"fmt"
	"path"

	cloudrunv2 "github.com/pulumi/pulumi-gcp/sdk/v10/go/gcp/cloudrunv2"
	"github.com/pulumi/pulumi-gcp/sdk/v10/go/gcp/projects"
	"github.com/pulumi/pulumi-gcp/sdk/v10/go/gcp/secretmanager"
	"github.com/pulumi/pulumi-gcp/sdk/v10/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	// Only private ranges and Private Google Access VIPs go through the VPC.
	// Everything else uses Cloud Run's built-in internet egress (no Cloud NAT).
	egressPrivateRangesOnly = "PRIVATE_RANGES_ONLY"
	credentialVolumeName    = "credential"
	// Readable by non-root images (e.g. cloudflared runs as uid 65532).
	credentialFileMode = 0444
)

// defaultResourceLimits are the worker pool minimums.
// See: https://docs.cloud.google.com/run/docs/configuring/workerpools/cpu
var defaultResourceLimits = pulumi.StringMap{
	"cpu":    pulumi.String("80m"),
	"memory": pulumi.String("512Mi"),
}

// TunnelConnector holds the resources of a deployed connector.
type TunnelConnector struct {
	name string

	serviceAccount    *serviceaccount.Account
	credentialSecret  *secretmanager.Secret
	credentialVersion *secretmanager.SecretVersion
	workerPool        *cloudrunv2.WorkerPool
}

// Deploy runs a connector process as a Cloud Run worker pool on the connectors network.
// The credential is readable only by the connector's own service account, which has no project roles.
func Deploy(ctx *pulumi.Context, scope Scope, network *Network, args *Args) (*TunnelConnector, error) {
	if network == nil {
		return nil, errors.New("connector network is required")
	}
	args, err := withDefaults(args)
	if err != nil {
		return nil, err
	}

	connector := &TunnelConnector{name: args.Name}

	accountName := scope.Namer.NewResourceName(args.Name, "connector", 28)
	connector.serviceAccount, err = serviceaccount.NewAccount(ctx, accountName, &serviceaccount.AccountArgs{
		Project:     pulumi.String(scope.Project),
		AccountId:   pulumi.String(accountName),
		DisplayName: pulumi.String(fmt.Sprintf("Tunnel connector (%s)", args.Name)),
	}, pulumi.Parent(scope.Parent))
	if err != nil {
		return nil, fmt.Errorf("failed to create connector service account: %w", err)
	}

	err = connector.storeCredential(ctx, scope, args)
	if err != nil {
		return nil, err
	}

	container := &cloudrunv2.WorkerPoolTemplateContainerArgs{
		Name:      pulumi.String(args.Name),
		Image:     pulumi.String(args.Image),
		Args:      args.Args,
		Envs:      newEnvVars(args, connector.credentialVersion),
		Resources: &cloudrunv2.WorkerPoolTemplateContainerResourcesArgs{Limits: args.ResourceLimits},
	}
	if args.StartupProbe != nil {
		container.StartupProbe = newStartupProbe(args.StartupProbe)
	}
	if args.LivenessProbe != nil {
		container.LivenessProbe = newLivenessProbe(args.LivenessProbe)
	}

	template := &cloudrunv2.WorkerPoolTemplateArgs{
		ServiceAccount: connector.serviceAccount.Email,
		Containers:     cloudrunv2.WorkerPoolTemplateContainerArray{container},
		VpcAccess: &cloudrunv2.WorkerPoolTemplateVpcAccessArgs{
			Egress: pulumi.String(egressPrivateRangesOnly),
			NetworkInterfaces: cloudrunv2.WorkerPoolTemplateVpcAccessNetworkInterfaceArray{
				&cloudrunv2.WorkerPoolTemplateVpcAccessNetworkInterfaceArgs{
					Network:    network.networkName,
					Subnetwork: network.subnetName,
				},
			},
		},
	}

	if args.Credential.FilePath != "" {
		template.Volumes = cloudrunv2.WorkerPoolTemplateVolumeArray{
			&cloudrunv2.WorkerPoolTemplateVolumeArgs{
				Name: pulumi.String(credentialVolumeName),
				Secret: &cloudrunv2.WorkerPoolTemplateVolumeSecretArgs{
					Secret: connector.credentialSecret.SecretId,
					Items: cloudrunv2.WorkerPoolTemplateVolumeSecretItemArray{
						&cloudrunv2.WorkerPoolTemplateVolumeSecretItemArgs{
							Path: pulumi.String(path.Base(args.Credential.FilePath)),
							// Pinned so a rotation rolls out a new revision.
							Version: connector.credentialVersion.Version,
							Mode:    pulumi.Int(credentialFileMode),
						},
					},
				},
			},
		}
		container.VolumeMounts = cloudrunv2.WorkerPoolTemplateContainerVolumeMountArray{
			&cloudrunv2.WorkerPoolTemplateContainerVolumeMountArgs{
				Name:      pulumi.String(credentialVolumeName),
				MountPath: pulumi.String(path.Dir(args.Credential.FilePath)),
			},
		}
	}

	workerPoolName := scope.Namer.NewResourceName(args.Name, "connector", 49)
	connector.workerPool, err = cloudrunv2.NewWorkerPool(ctx, workerPoolName, &cloudrunv2.WorkerPoolArgs{
		Project:            pulumi.String(scope.Project),
		Location:           pulumi.String(scope.Region),
		Name:               pulumi.String(workerPoolName),
		Description:        pulumi.String(fmt.Sprintf("Tunnel connector (%s)", args.Name)),
		Labels:             scope.Labels,
		DeletionProtection: pulumi.Bool(args.DeletionProtection),
		Scaling: &cloudrunv2.WorkerPoolScalingArgs{
			ScalingMode:         pulumi.String("MANUAL"),
			ManualInstanceCount: pulumi.Int(args.Replicas),
		},
		Template: template,
	}, pulumi.Parent(scope.Parent))
	if err != nil {
		return nil, fmt.Errorf("failed to create connector worker pool: %w", err)
	}

	return connector, nil
}

// storeCredential stores the credential in Secret Manager and grants access to the connector only.
func (c *TunnelConnector) storeCredential(ctx *pulumi.Context, scope Scope, args *Args) error {
	secretManagerAPI, err := projects.NewService(ctx, scope.Namer.NewResourceName(args.Name, "secretmanager-api", 63), &projects.ServiceArgs{
		Project:          pulumi.String(scope.Project),
		Service:          pulumi.String("secretmanager.googleapis.com"),
		DisableOnDestroy: pulumi.Bool(false),
	}, pulumi.Parent(scope.Parent), pulumi.RetainOnDelete(true))
	if err != nil {
		return fmt.Errorf("failed to enable Secret Manager API: %w", err)
	}

	secretID := scope.Namer.NewResourceName(args.Name, "credential", 63)
	c.credentialSecret, err = secretmanager.NewSecret(ctx, secretID, &secretmanager.SecretArgs{
		Project:  pulumi.String(scope.Project),
		SecretId: pulumi.String(secretID),
		Labels:   scope.Labels,
		Replication: &secretmanager.SecretReplicationArgs{
			// With google-managed default encryption
			Auto: &secretmanager.SecretReplicationAutoArgs{},
		},
	}, pulumi.Parent(scope.Parent), pulumi.DependsOn([]pulumi.Resource{secretManagerAPI}))
	if err != nil {
		return fmt.Errorf("failed to create connector credential secret: %w", err)
	}

	c.credentialVersion, err = secretmanager.NewSecretVersion(ctx, scope.Namer.NewResourceName(args.Name, "credential-version", 63), &secretmanager.SecretVersionArgs{
		Secret:     c.credentialSecret.ID(),
		SecretData: pulumi.ToSecret(args.Credential.Value.ToStringOutput()).(pulumi.StringOutput),
	}, pulumi.Parent(scope.Parent))
	if err != nil {
		return fmt.Errorf("failed to create connector credential version: %w", err)
	}

	_, err = secretmanager.NewSecretIamMember(ctx, scope.Namer.NewResourceName(args.Name, "credential-accessor", 63), &secretmanager.SecretIamMemberArgs{
		Project:  pulumi.String(scope.Project),
		SecretId: c.credentialSecret.SecretId,
		Role:     pulumi.String("roles/secretmanager.secretAccessor"),
		Member:   pulumi.Sprintf("serviceAccount:%s", c.serviceAccount.Email),
	}, pulumi.Parent(scope.Parent))
	if err != nil {
		return fmt.Errorf("failed to grant connector credential accessor: %w", err)
	}

	return nil
}

// withDefaults validates the args and returns a copy with defaults applied.
func withDefaults(args *Args) (*Args, error) {
	if args == nil {
		return nil, errors.New("connector args are required")
	}
	if args.Name == "" {
		return nil, errors.New("connector name is required")
	}
	if args.Image == "" {
		return nil, fmt.Errorf("image is required for connector %s", args.Name)
	}
	credential := args.Credential
	if credential == nil || credential.Value == nil {
		return nil, fmt.Errorf("credential is required for connector %s", args.Name)
	}
	if (credential.FilePath == "") == (credential.EnvVar == "") {
		return nil, fmt.Errorf("exactly one of credential file path or env var is required for connector %s", args.Name)
	}
	if credential.FilePath != "" && (!path.IsAbs(credential.FilePath) || path.Dir(credential.FilePath) == "/") {
		return nil, fmt.Errorf("credential file path for connector %s must be absolute and inside a directory: %q", args.Name, credential.FilePath)
	}
	if args.Replicas < 0 {
		return nil, fmt.Errorf("replicas must not be negative for connector %s", args.Name)
	}

	defaulted := *args
	if defaulted.Replicas == 0 {
		defaulted.Replicas = 1
	}
	if defaulted.ResourceLimits == nil {
		defaulted.ResourceLimits = defaultResourceLimits
	}

	return &defaulted, nil
}

func newEnvVars(args *Args, credentialVersion *secretmanager.SecretVersion) cloudrunv2.WorkerPoolTemplateContainerEnvArray {
	envVars := make(cloudrunv2.WorkerPoolTemplateContainerEnvArray, 0, len(args.EnvVars)+1)
	for name, value := range args.EnvVars {
		envVars = append(envVars, &cloudrunv2.WorkerPoolTemplateContainerEnvArgs{
			Name:  pulumi.String(name),
			Value: pulumi.String(value),
		})
	}

	if args.Credential.EnvVar != "" {
		envVars = append(envVars, &cloudrunv2.WorkerPoolTemplateContainerEnvArgs{
			Name: pulumi.String(args.Credential.EnvVar),
			ValueSource: &cloudrunv2.WorkerPoolTemplateContainerEnvValueSourceArgs{
				SecretKeyRef: &cloudrunv2.WorkerPoolTemplateContainerEnvValueSourceSecretKeyRefArgs{
					Secret:  credentialVersion.Secret,
					Version: credentialVersion.Version,
				},
			},
		})
	}

	return envVars
}

func newStartupProbe(probe *Probe) *cloudrunv2.WorkerPoolTemplateContainerStartupProbeArgs {
	return &cloudrunv2.WorkerPoolTemplateContainerStartupProbeArgs{
		HttpGet: &cloudrunv2.WorkerPoolTemplateContainerStartupProbeHttpGetArgs{
			Path: pulumi.String(probe.Path),
			Port: pulumi.Int(probe.Port),
		},
		InitialDelaySeconds: pulumi.Int(probe.InitialDelaySeconds),
		PeriodSeconds:       pulumi.Int(probe.PeriodSeconds),
		TimeoutSeconds:      pulumi.Int(probe.TimeoutSeconds),
		FailureThreshold:    pulumi.Int(probe.FailureThreshold),
	}
}

func newLivenessProbe(probe *Probe) *cloudrunv2.WorkerPoolTemplateContainerLivenessProbeArgs {
	return &cloudrunv2.WorkerPoolTemplateContainerLivenessProbeArgs{
		HttpGet: &cloudrunv2.WorkerPoolTemplateContainerLivenessProbeHttpGetArgs{
			Path: pulumi.String(probe.Path),
			Port: pulumi.Int(probe.Port),
		},
		InitialDelaySeconds: pulumi.Int(probe.InitialDelaySeconds),
		PeriodSeconds:       pulumi.Int(probe.PeriodSeconds),
		TimeoutSeconds:      pulumi.Int(probe.TimeoutSeconds),
		FailureThreshold:    pulumi.Int(probe.FailureThreshold),
	}
}

// GetName returns the connector name.
func (c *TunnelConnector) GetName() string {
	return c.name
}

// GetServiceAccount returns the connector's service account.
func (c *TunnelConnector) GetServiceAccount() *serviceaccount.Account {
	return c.serviceAccount
}

// GetCredentialSecret returns the Secret Manager secret holding the credential.
func (c *TunnelConnector) GetCredentialSecret() *secretmanager.Secret {
	return c.credentialSecret
}

// GetCredentialSecretVersion returns the secret version mounted by the connector.
func (c *TunnelConnector) GetCredentialSecretVersion() *secretmanager.SecretVersion {
	return c.credentialVersion
}

// GetWorkerPool returns the worker pool running the connector.
func (c *TunnelConnector) GetWorkerPool() *cloudrunv2.WorkerPool {
	return c.workerPool
}
