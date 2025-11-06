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

	if args.ColdStartSLO != nil {
		setColdStartSLODefaults(args.ColdStartSLO)
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

	accountName := f.NewResourceName(backendName, "account", 28)
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

	secretEnvVarsBySidecarName, volumeMountsBySidecarName, sidecarsVolumes, err := f.setupSidecarsSecrets(ctx, backendName, args.Sidecars, serviceAccount)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to setup sidecars secrets: %w", err)
	}
	if sidecarsVolumes != nil {
		*volumes = append(*volumes, *sidecarsVolumes...)
	}

	backendServiceName := f.NewResourceName(backendName, "service", 63)

	containers := cloudrunv2.ServiceTemplateContainerArray{
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
	}

	// Add sidecars if enabled
	for _, sidecar := range args.Sidecars {
		// setup the sidecar container with the "volumes from secrets"
		// and the "env vars from secrets"
		sidecarContainer := newSidecarContainer(sidecar,
			volumeMountsBySidecarName[sidecar.Name],
			secretEnvVarsBySidecarName[sidecar.Name],
		)
		// add the sidecar to the containers
		containers = append(containers, sidecarContainer)
	}

	serviceTemplate := &cloudrunv2.ServiceTemplateArgs{
		Scaling: &cloudrunv2.ServiceTemplateScalingArgs{
			MaxInstanceCount: pulumi.Int(args.MaxInstanceCount),
		},
		Containers:     containers,
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

	if args.ColdStartSLO != nil {
		backendSLO, err := f.setupColdStartSLO(ctx, backendServiceName, args.ColdStartSLO)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to setup cold start SLO for backend Cloud Run service: %w", err)
		}
		f.backendColdStartSLO = backendSLO
	}

	return backendService, serviceAccount, nil
}

// setupSidecarsSecrets setups secrets for sidecars as volumes and as env vars
func (f *FullStack) setupSidecarsSecrets(ctx *pulumi.Context, serviceName string,
	sidecars []*SidecarArgs,
	serviceAccount *serviceaccount.Account) (
	map[string]*cloudrunv2.ServiceTemplateContainerEnvArray,
	map[string]*cloudrunv2.ServiceTemplateContainerVolumeMountArray,
	*cloudrunv2.ServiceTemplateVolumeArray,
	error,
) {

	// Collect all sidecars secrets and separate by sidecar name and type
	secretEnvVarsBySidecarName := make(map[string]*cloudrunv2.ServiceTemplateContainerEnvArray)
	volumeMountsBySidecarName := make(map[string]*cloudrunv2.ServiceTemplateContainerVolumeMountArray)
	allVolumes := &cloudrunv2.ServiceTemplateVolumeArray{}

	for _, sidecar := range sidecars {

		if len(sidecar.Secrets) > 0 {
			var sidecarEnvVarSecrets []*SecretVolumeArgs
			var sidecarVolumeSecrets []*SecretVolumeArgs
			for _, secret := range sidecar.Secrets {
				if secret.MountAsEnvVars {
					// secret is to be mounted as an env var
					sidecarEnvVarSecrets = append(sidecarEnvVarSecrets, secret)
				} else {
					// secret is to be mounted as a volume
					sidecarVolumeSecrets = append(sidecarVolumeSecrets, secret)
				}
			}

			// setup the "volumes from secrets" for the sidecar
			instanceName := fmt.Sprintf("%s-%s", serviceName, sidecar.Name)
			sidecarVols, sidecarMounts, err := f.mountSecrets(ctx, instanceName, sidecarVolumeSecrets, serviceAccount.Email)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to mount sidecar secrets: %w", err)
			}

			*allVolumes = append(*allVolumes, *sidecarVols...)
			volumeMountsBySidecarName[sidecar.Name] = sidecarMounts

			// setup the "env vars from secrets" for the sidecar
			sidecarEnvVars, err := f.mountSecretsAsEnvVars(ctx, sidecarEnvVarSecrets, serviceName, serviceAccount.Email)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to mount sidecar secrets as environment variables: %w", err)
			}
			secretEnvVarsBySidecarName[sidecar.Name] = sidecarEnvVars
		}
	}

	return secretEnvVarsBySidecarName, volumeMountsBySidecarName, allVolumes, nil
}

func (f *FullStack) mountSecrets(ctx *pulumi.Context,
	instanceName string,
	secrets []*SecretVolumeArgs,
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
		secretAccessorName := f.NewResourceName(instanceName, fmt.Sprintf("%s-secret-accessor", secret.Name), 63)
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

// mountSecretsAsEnvVars grants IAM access to secrets that will be mounted as environment variables
func (f *FullStack) mountSecretsAsEnvVars(ctx *pulumi.Context,
	secrets []*SecretVolumeArgs,
	serviceName string,
	serviceAccountEmail pulumi.StringOutput,
) (*cloudrunv2.ServiceTemplateContainerEnvArray, error) {

	// map of secret name to env vars
	containerEnvVars := &cloudrunv2.ServiceTemplateContainerEnvArray{}

	for _, secret := range secrets {
		// Create IAM binding for the secret
		secretAccessorName := f.NewResourceName(serviceName, fmt.Sprintf("%s-secret-accessor", secret.Name), 63)
		_, err := secretmanager.NewSecretIamMember(ctx, secretAccessorName, &secretmanager.SecretIamMemberArgs{
			Project:  pulumi.String(f.Project),
			SecretId: secret.SecretID,
			Role:     pulumi.String("roles/secretmanager.secretAccessor"),
			Member:   pulumi.Sprintf("serviceAccount:%s", serviceAccountEmail),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to grant secret accessor for %s: %w", secret.Name, err)
		}

		version := secret.Version
		if version == nil {
			version = pulumi.String("latest")
		}

		*containerEnvVars = append(*containerEnvVars, cloudrunv2.ServiceTemplateContainerEnvArgs{
			Name: pulumi.String(secret.Name),
			ValueSource: &cloudrunv2.ServiceTemplateContainerEnvValueSourceArgs{
				SecretKeyRef: &cloudrunv2.ServiceTemplateContainerEnvValueSourceSecretKeyRefArgs{
					Secret:  secret.SecretID,
					Version: version,
				},
			},
		})
	}

	return containerEnvVars, nil
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
		moreVolumes, moreMounts, err := f.mountSecrets(ctx, serviceName, secrets, serviceAccount.Email)
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
	if f.storageBucket != nil {
		// Allow backend to access storage bucket objects
		instanceRoles = append(instanceRoles, "roles/storage.objectAdmin")
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
	accountName := f.NewResourceName(serviceName, "account", 28)
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

	secretEnvVarsBySidecarName, volumeMountsBySidecarName, sidecarsVolumes, err := f.setupSidecarsSecrets(ctx, serviceName, args.Sidecars, serviceAccount)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to setup sidecars secrets: %w", err)
	}
	if sidecarsVolumes != nil {
		*volumes = append(*volumes, *sidecarsVolumes...)
	}

	frontendServiceName := f.NewResourceName(serviceName, "service", 63)

	containers := cloudrunv2.ServiceTemplateContainerArray{
		&cloudrunv2.ServiceTemplateContainerArgs{
			Image: frontendImage,
			Resources: &cloudrunv2.ServiceTemplateContainerResourcesArgs{

				CpuIdle:         pulumi.Bool(true),
				Limits:          args.ResourceLimits,
				StartupCpuBoost: pulumi.Bool(args.StartupCPUBoost),
			},
			Ports: cloudrunv2.ServiceTemplateContainerPortsArgs{
				ContainerPort: pulumi.Int(args.ContainerPort),
			},

			Envs:          newFrontendEnvVars(args, backendURL, f.AppBaseURL),
			StartupProbe:  startupProbe(args.ContainerPort, args.StartupProbe),
			LivenessProbe: livenessProbe(args.ContainerPort, args.LivenessProbe.Path, args.LivenessProbe),
			VolumeMounts:  volumeMounts,
		},
	}

	// Add sidecars if enabled
	for _, sidecar := range args.Sidecars {
		// setup the sidecar container with the "volumes from secrets"
		// and the "env vars from secrets"
		sidecarContainer := newSidecarContainer(sidecar,
			volumeMountsBySidecarName[sidecar.Name],
			secretEnvVarsBySidecarName[sidecar.Name],
		)
		// add the sidecar to the containers
		containers = append(containers, sidecarContainer)
	}

	frontendServiceTemplate := &cloudrunv2.ServiceTemplateArgs{
		Scaling: &cloudrunv2.ServiceTemplateScalingArgs{
			MaxInstanceCount: pulumi.Int(args.MaxInstanceCount),
		},
		Containers:     containers,
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

	if args.ColdStartSLO != nil {
		frontendSLO, err := f.setupColdStartSLO(ctx, frontendServiceName, args.ColdStartSLO)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to setup cold start SLO for backend Cloud Run service: %w", err)
		}
		f.frontendColdStartSLO = frontendSLO
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
	domainMappingName := f.NewResourceName(serviceName, "domain-mapping", 63)
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

func newSidecarEnvVars(envVars map[string]string) cloudrunv2.ServiceTemplateContainerEnvArray {
	sidecarEnvVars := cloudrunv2.ServiceTemplateContainerEnvArray{}
	for envVarName, envVarValue := range envVars {
		sidecarEnvVars = append(sidecarEnvVars, cloudrunv2.ServiceTemplateContainerEnvArgs{
			Name:  pulumi.String(envVarName),
			Value: pulumi.String(envVarValue),
		})
	}

	return sidecarEnvVars
}

func newSidecarContainer(sidecar *SidecarArgs, volumeMounts *cloudrunv2.ServiceTemplateContainerVolumeMountArray, secretEnvVars *cloudrunv2.ServiceTemplateContainerEnvArray) *cloudrunv2.ServiceTemplateContainerArgs {
	container := &cloudrunv2.ServiceTemplateContainerArgs{
		Name:  pulumi.String(sidecar.Name),
		Image: pulumi.String(sidecar.Image),
		Resources: &cloudrunv2.ServiceTemplateContainerResourcesArgs{
			CpuIdle: pulumi.Bool(true),
		},
		Args: setDefaultStringArray(sidecar.Args, []string{}),
	}

	// Combine regular env vars and secret-based env vars
	var allEnvVars cloudrunv2.ServiceTemplateContainerEnvArray
	if len(sidecar.EnvVars) > 0 {
		allEnvVars = newSidecarEnvVars(sidecar.EnvVars)
	}
	if secretEnvVars != nil && len(*secretEnvVars) > 0 {
		allEnvVars = append(allEnvVars, *secretEnvVars...)
	}
	if len(allEnvVars) > 0 {
		container.Envs = allEnvVars
	}

	if volumeMounts != nil && len(*volumeMounts) > 0 {
		container.VolumeMounts = volumeMounts
	}

	if sidecar.StartupProbe != nil {
		container.StartupProbe = startupProbe(sidecar.StartupProbe.Port, sidecar.StartupProbe)
	}

	if sidecar.LivenessProbe != nil {
		container.LivenessProbe = livenessProbe(sidecar.LivenessProbe.Port, sidecar.LivenessProbe.Path, sidecar.LivenessProbe)
	}

	return container
}
