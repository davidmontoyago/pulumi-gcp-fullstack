package gcp

import (
	"fmt"
	"log"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudrun"
	cloudrunv2 "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudrunv2"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/projects"
	secretmanager "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/secretmanager"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

var (
	// Resource "requests" do not apply to Cloud Run as in k8s
	defaultBackendResourceLimits = pulumi.StringMap{
		"memory": pulumi.String("1Gi"),
		"cpu":    pulumi.String("1000m"),
	}
	defaultFrontendResourceLimits = pulumi.StringMap{
		"memory": pulumi.String("2Gi"),
		"cpu":    pulumi.String("2000m"),
	}
)

// InstanceDefaults contains the default values for different service types
type InstanceDefaults struct {
	SecretConfigFileName string
	SecretConfigFilePath string
	ContainerPort        int
	ResourceLimits       pulumi.StringMap
}

// setInstanceDefaults takes an existing InstanceArgs (or nil) and returns a new one with safe defaults.
// The defaults are customized based on the service type (backend vs frontend).
func setInstanceDefaults(args *InstanceArgs, defaults InstanceDefaults) *InstanceArgs {
	if args == nil {
		args = &InstanceArgs{}
	}

	if args.SecretConfigFileName == "" {
		args.SecretConfigFileName = defaults.SecretConfigFileName
	}
	if args.SecretConfigFilePath == "" {
		args.SecretConfigFilePath = defaults.SecretConfigFilePath
	}
	if args.ResourceLimits == nil {
		args.ResourceLimits = defaults.ResourceLimits
	}
	if args.ContainerPort == 0 {
		args.ContainerPort = defaults.ContainerPort
	}
	if args.MaxInstanceCount == 0 {
		args.MaxInstanceCount = 3
	}

	// Set default startup probe if not provided
	if args.StartupProbe == nil {
		args.StartupProbe = &Probe{
			InitialDelaySeconds: 10,
			PeriodSeconds:       2,
			TimeoutSeconds:      1,
			FailureThreshold:    3,
		}
	}

	// Set default liveness probe if not provided
	if args.LivenessProbe == nil {
		args.LivenessProbe = &Probe{
			Path:                "healthz",
			InitialDelaySeconds: 15,
			PeriodSeconds:       5,
			TimeoutSeconds:      3,
			FailureThreshold:    3,
		}
	}

	if args.Secrets == nil {
		args.Secrets = []*SecretVolumeArgs{}
	}

	return args
}

func (f *FullStack) deployBackendCloudRunInstance(ctx *pulumi.Context, args *BackendArgs) (*cloudrunv2.Service, *serviceaccount.Account, error) {
	// Set defaults for backend
	backendDefaults := InstanceDefaults{
		SecretConfigFileName: ".env",
		SecretConfigFilePath: "/app/config/",
		ContainerPort:        4001,
		ResourceLimits:       defaultBackendResourceLimits,
	}

	if args == nil {
		args = &BackendArgs{}
	}
	args.InstanceArgs = setInstanceDefaults(args.InstanceArgs, backendDefaults)

	backendName := f.BackendName
	backendLabels := mergeLabels(f.Labels, pulumi.StringMap{
		"backend": pulumi.String("true"),
	})

	accountName := f.newResourceName(backendName, "account", 28)
	serviceAccount, err := serviceaccount.NewAccount(ctx, accountName, &serviceaccount.AccountArgs{
		AccountId:   pulumi.String(accountName),
		DisplayName: pulumi.String(fmt.Sprintf("Backend service account (%s)", backendName)),
		Project:     pulumi.String(f.Project),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create backend service account: %w", err)
	}

	additionalSecrets := args.Secrets
	if f.cacheCredentialsSecret != nil {
		// if enabled, append secret with cache credentials
		additionalSecrets = append(additionalSecrets, &SecretVolumeArgs{
			SecretID:   f.cacheCredentialsSecret.Secret,
			Name:       "cache-credentials",
			Path:       "/app/cache-config",
			SecretName: ".env",
			Version:    f.cacheCredentialsSecret.Version,
		})
	}

	volumes, volumeMounts, err := f.setupInstanceSecrets(ctx, backendName, additionalSecrets, serviceAccount,
		backendLabels, args.InstanceArgs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to setup instance secrets: %w", err)
	}

	backendServiceName := f.newResourceName(backendName, "service", 100)
	serviceTemplate := &cloudrunv2.ServiceTemplateArgs{
		Scaling: &cloudrunv2.ServiceTemplateScalingArgs{
			MaxInstanceCount: pulumi.Int(args.MaxInstanceCount),
		},
		Containers: cloudrunv2.ServiceTemplateContainerArray{
			&cloudrunv2.ServiceTemplateContainerArgs{
				Image: f.BackendImage,
				Envs:  newBackendEnvVars(args, f.AppBaseURL),
				Resources: &cloudrunv2.ServiceTemplateContainerResourcesArgs{
					CpuIdle:         pulumi.Bool(true),
					Limits:          args.ResourceLimits,
					StartupCpuBoost: pulumi.Bool(args.StartupCPUBoost),
				},
				Ports: cloudrunv2.ServiceTemplateContainerPortsArgs{
					ContainerPort: pulumi.Int(args.ContainerPort),
				},
				StartupProbe:  startupProbe(args.ContainerPort, args.StartupProbe),
				LivenessProbe: livenessProbe(args.ContainerPort, args.LivenessProbe.Path, args.LivenessProbe),
				VolumeMounts:  volumeMounts,
			},
		},
		ServiceAccount: serviceAccount.Email,
		Volumes:        volumes,
	}

	if f.vpcConnector != nil {
		// Access to cache instance with private IP
		serviceTemplate.VpcAccess = &cloudrunv2.ServiceTemplateVpcAccessArgs{
			Connector: f.vpcConnector.SelfLink,
			Egress:    pulumi.String("PRIVATE_RANGES_ONLY"),
		}
	}

	ingress := "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER"
	if args.EnablePublicIngress {
		// The instance is likely using an external WAF. Make it reachable.
		ingress = "INGRESS_TRAFFIC_ALL"
	}

	backendService, err := cloudrunv2.NewService(ctx, backendServiceName, &cloudrunv2.ServiceArgs{
		Name:               pulumi.String(backendServiceName),
		Ingress:            pulumi.String(ingress),
		Description:        pulumi.String(fmt.Sprintf("Serverless instance (%s)", backendName)),
		Location:           pulumi.String(f.Region),
		Project:            pulumi.String(f.Project),
		Labels:             backendLabels,
		Template:           serviceTemplate,
		DeletionProtection: pulumi.Bool(args.DeletionProtection),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create backend Cloud Run service: %w", err)
	}

	err = f.grantProjectLevelIAMRoles(ctx, args.ProjectIAMRoles, backendServiceName, serviceAccount)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to grant project level IAM roles to backend Cloud Run service: %w", err)
	}

	return backendService, serviceAccount, nil
}

func (f *FullStack) mountSecrets(ctx *pulumi.Context,
	secrets []*SecretVolumeArgs,
	backendName string,
	serviceAccountEmail pulumi.StringOutput,
) (*cloudrunv2.ServiceTemplateVolumeArray, *cloudrunv2.ServiceTemplateContainerVolumeMountArray, error) {

	volumes := &cloudrunv2.ServiceTemplateVolumeArray{}
	volumeMounts := &cloudrunv2.ServiceTemplateContainerVolumeMountArray{}

	for _, secret := range secrets {
		// mount secret as a container volume
		secretVolume := newSecretVolume(secret)

		*volumes = append(*volumes, secretVolume)

		*volumeMounts = append(*volumeMounts, cloudrunv2.ServiceTemplateContainerVolumeMountArgs{
			MountPath: pulumi.String(secret.Path),
			Name:      pulumi.String(secret.Name),
		})

		// Create IAM binding for the secret (similar to secretmanager.go)
		secretAccessorName := f.newResourceName(backendName, fmt.Sprintf("%s-secret-accessor", secret.Name), 100)
		_, err := secretmanager.NewSecretIamMember(ctx, secretAccessorName, &secretmanager.SecretIamMemberArgs{
			Project:  pulumi.String(f.Project),
			SecretId: secret.SecretID,
			Role:     pulumi.String("roles/secretmanager.secretAccessor"),
			Member:   pulumi.Sprintf("serviceAccount:%s", serviceAccountEmail),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to grant secret accessor for %s: %w", secret.Name, err)
		}

	}

	return volumes, volumeMounts, nil
}

// setupInstanceSecrets creates the configuration secret and sets up all secret volumes and mounts for a service instance
func (f *FullStack) setupInstanceSecrets(
	ctx *pulumi.Context,
	serviceName string,
	secrets []*SecretVolumeArgs,
	serviceAccount *serviceaccount.Account,
	labels pulumi.StringMap,
	args *InstanceArgs,
) (*cloudrunv2.ServiceTemplateVolumeArray, *cloudrunv2.ServiceTemplateContainerVolumeMountArray, error) {

	// create a secret to hold env vars for the cloud run instance
	configSecret, err := f.newEnvConfigSecret(ctx,
		serviceName,
		serviceAccount,
		args.DeletionProtection,
		labels,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create config secret: %w", err)
	}

	// add the volume for the default config
	volumes := &cloudrunv2.ServiceTemplateVolumeArray{
		newSecretVolume(&SecretVolumeArgs{
			SecretID: configSecret.SecretId,
			Name:     "envconfig",
			Path:     args.SecretConfigFileName,
			Version:  pulumi.String("latest"),
		}),
	}

	volumeMounts := &cloudrunv2.ServiceTemplateContainerVolumeMountArray{
		cloudrunv2.ServiceTemplateContainerVolumeMountArgs{
			MountPath: pulumi.String(args.SecretConfigFilePath),
			Name:      pulumi.String("envconfig"),
		},
	}

	// add other secrets passed
	if len(secrets) > 0 {
		moreVolumes, moreMounts, err := f.mountSecrets(ctx, secrets, serviceName, serviceAccount.Email)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to mount additional secrets: %w", err)
		}
		*volumes = append(*volumes, *moreVolumes...)
		*volumeMounts = append(*volumeMounts, *moreMounts...)
	}

	return volumes, volumeMounts, nil
}

func (f *FullStack) grantProjectLevelIAMRoles(ctx *pulumi.Context,
	iamRoles []string,
	backendServiceName string,
	serviceAccount *serviceaccount.Account) error {

	instanceRoles := iamRoles
	if f.redisInstance != nil {
		// Allow backend to write to Redis instance
		instanceRoles = append(instanceRoles, "roles/redis.editor")
	}

	if len(instanceRoles) > 0 {
		for _, role := range instanceRoles {
			iamMember, err := projects.NewIAMMember(ctx, fmt.Sprintf("%s-%s", backendServiceName, role), &projects.IAMMemberArgs{
				Project: pulumi.String(f.Project),
				Role:    pulumi.String(role),
				Member:  pulumi.Sprintf("serviceAccount:%s", serviceAccount.Email),
			})
			if err != nil {
				return fmt.Errorf("failed to add IAM role to backend Cloud Run service: %w", err)
			}
			// Track created IAM members for testing/inspection
			f.backendProjectIamMembers = append(f.backendProjectIamMembers, iamMember)
		}
	}

	return nil
}

func (f *FullStack) deployFrontendCloudRunInstance(ctx *pulumi.Context, args *FrontendArgs, backendURL pulumi.StringOutput) (*cloudrunv2.Service, *serviceaccount.Account, error) {
	// Set defaults for frontend
	frontendDefaults := InstanceDefaults{
		SecretConfigFileName: ".env.production",
		SecretConfigFilePath: "/app/.next/config/",
		ContainerPort:        3000,
		ResourceLimits:       defaultFrontendResourceLimits,
	}

	if args == nil {
		args = &FrontendArgs{}
	}
	args.InstanceArgs = setInstanceDefaults(args.InstanceArgs, frontendDefaults)

	frontendLabels := mergeLabels(f.Labels, pulumi.StringMap{
		"frontend": pulumi.String("true"),
	})

	frontendImage := f.FrontendImage
	project := f.Project
	region := f.Region

	serviceName := f.FrontendName
	accountName := f.newResourceName(serviceName, "account", 28)
	serviceAccount, err := serviceaccount.NewAccount(ctx, accountName, &serviceaccount.AccountArgs{
		AccountId:   pulumi.String(accountName),
		DisplayName: pulumi.String(fmt.Sprintf("Frontend service account (%s)", serviceName)),
		Project:     pulumi.String(project),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create frontend service account: %w", err)
	}

	volumes, volumeMounts, err := f.setupInstanceSecrets(ctx, serviceName, args.Secrets, serviceAccount, frontendLabels, args.InstanceArgs)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to setup instance secrets: %w", err)
	}

	frontendServiceName := f.newResourceName(serviceName, "service", 100)

	frontendServiceTemplate := &cloudrunv2.ServiceTemplateArgs{
		Scaling: &cloudrunv2.ServiceTemplateScalingArgs{
			MaxInstanceCount: pulumi.Int(args.MaxInstanceCount),
		},
		Containers: cloudrunv2.ServiceTemplateContainerArray{
			&cloudrunv2.ServiceTemplateContainerArgs{
				Image: frontendImage,
				Resources: &cloudrunv2.ServiceTemplateContainerResourcesArgs{
					// Stay serverless. Optimize for cold starts.
					CpuIdle:         pulumi.Bool(true),
					Limits:          args.ResourceLimits,
					StartupCpuBoost: pulumi.Bool(args.StartupCPUBoost),
				},
				Ports: cloudrunv2.ServiceTemplateContainerPortsArgs{
					ContainerPort: pulumi.Int(args.ContainerPort),
				},
				// TODO get app base url from input
				Envs:          newFrontendEnvVars(args, backendURL, f.AppBaseURL),
				StartupProbe:  startupProbe(args.ContainerPort, args.StartupProbe),
				LivenessProbe: livenessProbe(args.ContainerPort, args.LivenessProbe.Path, args.LivenessProbe),
				VolumeMounts:  volumeMounts,
			},
		},
		ServiceAccount: serviceAccount.Email,
		Volumes:        volumes,
	}

	ingress := "INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER"
	if args.EnablePublicIngress {
		// The instance is likely using an external WAF. Make it reachable.
		ingress = "INGRESS_TRAFFIC_ALL"
	}

	frontendService, err := cloudrunv2.NewService(ctx, frontendServiceName, &cloudrunv2.ServiceArgs{
		Name:               pulumi.String(frontendServiceName),
		Ingress:            pulumi.String(ingress),
		Description:        pulumi.String(fmt.Sprintf("Serverless instance (%s)", serviceName)),
		Location:           pulumi.String(region),
		Project:            pulumi.String(project),
		Labels:             frontendLabels,
		Template:           frontendServiceTemplate,
		DeletionProtection: pulumi.Bool(args.DeletionProtection),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create frontend Cloud Run service: %w", err)
	}

	return frontendService, serviceAccount, nil
}

// createCloudRunInstancesIAM creates IAM members to allow unauthenticated access to Cloud Run instances
func (f *FullStack) createCloudRunInstancesIAM(ctx *pulumi.Context, frontendService, backendService *cloudrunv2.Service) error {
	if err := ctx.Log.Info(fmt.Sprintf("Routing traffic to Cloud Run instances: %v and %v", frontendService.Uri, backendService.Uri), nil); err != nil {
		log.Println("failed to log routing details with Pulumi context: %w", err)
	}

	// If no gateway enabled, traffic goes directly to the cloud run instances. yehaaw!
	_, err := cloudrunv2.NewServiceIamMember(ctx, fmt.Sprintf("%s-allow-unauthenticated", f.FrontendName), &cloudrunv2.ServiceIamMemberArgs{
		Name:     frontendService.Name,
		Project:  pulumi.String(f.Project),
		Location: pulumi.String(f.Region),
		Role:     pulumi.String("roles/run.invoker"),
		Member:   pulumi.Sprintf("allUsers"),
	})
	if err != nil {
		return fmt.Errorf("failed to grant frontend invoker: %w", err)
	}

	_, err = cloudrunv2.NewServiceIamMember(ctx, fmt.Sprintf("%s-allow-unauthenticated", f.BackendName), &cloudrunv2.ServiceIamMemberArgs{
		Name:     backendService.Name,
		Project:  pulumi.String(f.Project),
		Location: pulumi.String(f.Region),
		Role:     pulumi.String("roles/run.invoker"),
		Member:   pulumi.Sprintf("allUsers"),
	})
	if err != nil {
		return fmt.Errorf("failed to grant backend invoker: %w", err)
	}

	return nil
}

func newFrontendEnvVars(args *FrontendArgs, backendURL pulumi.StringOutput, appBaseURL string) cloudrunv2.ServiceTemplateContainerEnvArray {
	envVars := cloudrunv2.ServiceTemplateContainerEnvArray{
		cloudrunv2.ServiceTemplateContainerEnvArgs{
			Name:  pulumi.String("DOTENV_CONFIG_PATH"),
			Value: pulumi.String(fmt.Sprintf("%s%s", args.SecretConfigFilePath, args.SecretConfigFileName)),
		},
		cloudrunv2.ServiceTemplateContainerEnvArgs{
			Name:  pulumi.String("APP_BASE_URL"),
			Value: pulumi.String(appBaseURL),
		},
		cloudrunv2.ServiceTemplateContainerEnvArgs{
			Name:  pulumi.String("BACKEND_API_URL"),
			Value: backendURL,
		},
	}
	for enVarName, envVarValue := range args.EnvVars {
		envVars = append(envVars, cloudrunv2.ServiceTemplateContainerEnvArgs{
			Name:  pulumi.String(enVarName),
			Value: pulumi.String(envVarValue),
		})
	}

	return envVars
}

func newBackendEnvVars(args *BackendArgs, appBaseURL string) cloudrunv2.ServiceTemplateContainerEnvArray {
	envVars := cloudrunv2.ServiceTemplateContainerEnvArray{
		cloudrunv2.ServiceTemplateContainerEnvArgs{
			Name:  pulumi.String("DOTENV_CONFIG_PATH"),
			Value: pulumi.String(fmt.Sprintf("%s%s", args.SecretConfigFilePath, args.SecretConfigFileName)),
		},
		cloudrunv2.ServiceTemplateContainerEnvArgs{
			Name:  pulumi.String("APP_BASE_URL"),
			Value: pulumi.String(appBaseURL),
		},
	}
	for enVarName, envVarValue := range args.EnvVars {
		envVars = append(envVars, cloudrunv2.ServiceTemplateContainerEnvArgs{
			Name:  pulumi.String(enVarName),
			Value: pulumi.String(envVarValue),
		})
	}

	return envVars
}

func newSecretVolume(secret *SecretVolumeArgs) *cloudrunv2.ServiceTemplateVolumeArgs {
	newVar := &cloudrunv2.ServiceTemplateVolumeArgs{
		Name: pulumi.String(secret.Name),
		Secret: &cloudrunv2.ServiceTemplateVolumeSecretArgs{
			Secret: secret.SecretID,
			Items: cloudrunv2.ServiceTemplateVolumeSecretItemArray{
				&cloudrunv2.ServiceTemplateVolumeSecretItemArgs{
					Path: pulumi.String(func() string {
						if secret.SecretName != "" {
							return secret.SecretName
						}

						return ".env"
					}()),
					Version: secret.Version,
					Mode:    pulumi.IntPtr(0400),
				},
			},
		},
	}

	return newVar
}

func (f *FullStack) createInstanceDomainMapping(
	ctx *pulumi.Context,
	serviceName string,
	domainURL string,
	targetInstanceName pulumi.StringOutput,
) (*cloudrun.DomainMapping, error) {
	domainMappingName := f.newResourceName(serviceName, "domain-mapping", 100)
	domainMapping, err := cloudrun.NewDomainMapping(ctx, domainMappingName, &cloudrun.DomainMappingArgs{
		Location: pulumi.String(f.Region),
		Name:     pulumi.String(domainURL),
		Metadata: &cloudrun.DomainMappingMetadataArgs{
			Namespace: pulumi.String(f.Project),
		},
		Spec: &cloudrun.DomainMappingSpecArgs{
			RouteName: targetInstanceName,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create custom domain mapping: %w", err)
	}

	return domainMapping, nil
}

func startupProbe(port int, probe *Probe) *cloudrunv2.ServiceTemplateContainerStartupProbeArgs {
	return &cloudrunv2.ServiceTemplateContainerStartupProbeArgs{
		TcpSocket: &cloudrunv2.ServiceTemplateContainerStartupProbeTcpSocketArgs{
			Port: pulumi.Int(port),
		},
		InitialDelaySeconds: pulumi.Int(probe.InitialDelaySeconds),
		PeriodSeconds:       pulumi.Int(probe.PeriodSeconds),
		TimeoutSeconds:      pulumi.Int(probe.TimeoutSeconds),
		FailureThreshold:    pulumi.Int(probe.FailureThreshold),
	}
}

func livenessProbe(port int, path string, probe *Probe) *cloudrunv2.ServiceTemplateContainerLivenessProbeArgs {
	return &cloudrunv2.ServiceTemplateContainerLivenessProbeArgs{
		HttpGet: &cloudrunv2.ServiceTemplateContainerLivenessProbeHttpGetArgs{
			Path: pulumi.String(fmt.Sprintf("/%s", path)),
			Port: pulumi.Int(port),
		},
		InitialDelaySeconds: pulumi.Int(probe.InitialDelaySeconds),
		PeriodSeconds:       pulumi.Int(probe.PeriodSeconds),
		TimeoutSeconds:      pulumi.Int(probe.TimeoutSeconds),
		FailureThreshold:    pulumi.Int(probe.FailureThreshold),
	}
}
