package gcp

import (
	"fmt"

	cloudrunv2 "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/cloudrunv2"
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
			InitialDelaySeconds: 15,
			PeriodSeconds:       3,
			TimeoutSeconds:      1,
			FailureThreshold:    3,
		}
	}

	// Set default liveness probe if not provided
	if args.LivenessProbe == nil {
		args.LivenessProbe = &Probe{
			Path:                "healthz",
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		}
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
	ctx.Export("cloud_run_service_backend_account_id", serviceAccount.ID())

	// create a secret to hold env vars for the cloud run instance
	configSecret, err := f.newEnvConfigSecret(ctx,
		backendName,
		serviceAccount,
		args.DeletionProtection,
		backendLabels,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create backend config secret: %w", err)
	}

	backendServiceName := f.newResourceName(backendName, "service", 100)
	serviceTemplate := &cloudrunv2.ServiceTemplateArgs{
		Scaling: &cloudrunv2.ServiceTemplateScalingArgs{
			MaxInstanceCount: pulumi.Int(args.MaxInstanceCount),
		},
		Containers: cloudrunv2.ServiceTemplateContainerArray{
			&cloudrunv2.ServiceTemplateContainerArgs{
				Image: f.BackendImage,
				Envs:  newBackendEnvVars(args),
				Resources: &cloudrunv2.ServiceTemplateContainerResourcesArgs{
					Limits: args.ResourceLimits,
				},
				Ports: cloudrunv2.ServiceTemplateContainerPortsArgs{
					ContainerPort: pulumi.Int(args.ContainerPort),
				},
				StartupProbe: &cloudrunv2.ServiceTemplateContainerStartupProbeArgs{
					TcpSocket: &cloudrunv2.ServiceTemplateContainerStartupProbeTcpSocketArgs{
						Port: pulumi.Int(args.ContainerPort),
					},
					InitialDelaySeconds: pulumi.Int(args.StartupProbe.InitialDelaySeconds),
					PeriodSeconds:       pulumi.Int(args.StartupProbe.PeriodSeconds),
					TimeoutSeconds:      pulumi.Int(args.StartupProbe.TimeoutSeconds),
					FailureThreshold:    pulumi.Int(args.StartupProbe.FailureThreshold),
				},
				LivenessProbe: &cloudrunv2.ServiceTemplateContainerLivenessProbeArgs{
					HttpGet: &cloudrunv2.ServiceTemplateContainerLivenessProbeHttpGetArgs{
						Path: pulumi.String(fmt.Sprintf("/%s", args.LivenessProbe.Path)),
						Port: pulumi.Int(args.ContainerPort),
					},
					InitialDelaySeconds: pulumi.Int(args.LivenessProbe.InitialDelaySeconds),
					PeriodSeconds:       pulumi.Int(args.LivenessProbe.PeriodSeconds),
					TimeoutSeconds:      pulumi.Int(args.LivenessProbe.TimeoutSeconds),
					FailureThreshold:    pulumi.Int(args.LivenessProbe.FailureThreshold),
				},
				VolumeMounts: &cloudrunv2.ServiceTemplateContainerVolumeMountArray{
					cloudrunv2.ServiceTemplateContainerVolumeMountArgs{
						MountPath: pulumi.String(args.SecretConfigFilePath),
						Name:      pulumi.String("envconfig"),
					},
				},
			},
		},
		ServiceAccount: serviceAccount.Email,
		Volumes: &cloudrunv2.ServiceTemplateVolumeArray{
			&cloudrunv2.ServiceTemplateVolumeArgs{
				Name: pulumi.String("envconfig"),
				Secret: &cloudrunv2.ServiceTemplateVolumeSecretArgs{
					Secret: configSecret.SecretId,
					Items: cloudrunv2.ServiceTemplateVolumeSecretItemArray{
						&cloudrunv2.ServiceTemplateVolumeSecretItemArgs{
							Path:    pulumi.String(args.SecretConfigFileName),
							Version: pulumi.String("latest"),
							Mode:    pulumi.IntPtr(0500),
						},
					},
				},
			},
		},
	}
	if args.PrivateVpcAccessConnector != nil {
		serviceTemplate.VpcAccess = &cloudrunv2.ServiceTemplateVpcAccessArgs{
			Connector: args.PrivateVpcAccessConnector,
			Egress:    pulumi.String("PRIVATE_RANGES_ONLY"),
		}
	}
	backendService, err := cloudrunv2.NewService(ctx, backendServiceName, &cloudrunv2.ServiceArgs{
		Name:               pulumi.String(backendServiceName),
		Ingress:            pulumi.String("INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER"),
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
	ctx.Export("cloud_run_service_backend_id", backendService.ID())
	ctx.Export("cloud_run_service_backend_uri", backendService.Uri)

	return backendService, serviceAccount, nil
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
	ctx.Export("cloud_run_service_frontend_account_id", serviceAccount.ID())

	// create a secret to hold env vars for the cloud run instance
	configSecret, err := f.newEnvConfigSecret(ctx, serviceName, serviceAccount, args.DeletionProtection, pulumi.StringMap{
		"frontend": pulumi.String("true"),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create frontend config secret: %w", err)
	}

	frontendServiceName := f.newResourceName(serviceName, "service", 100)
	frontendService, err := cloudrunv2.NewService(ctx, frontendServiceName, &cloudrunv2.ServiceArgs{
		Name:        pulumi.String(frontendServiceName),
		Ingress:     pulumi.String("INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER"),
		Description: pulumi.String(fmt.Sprintf("Serverless instance (%s)", serviceName)),
		Location:    pulumi.String(region),
		Project:     pulumi.String(project),
		Labels:      frontendLabels,
		Template: &cloudrunv2.ServiceTemplateArgs{
			Scaling: &cloudrunv2.ServiceTemplateScalingArgs{
				MaxInstanceCount: pulumi.Int(args.MaxInstanceCount),
			},
			Containers: cloudrunv2.ServiceTemplateContainerArray{
				&cloudrunv2.ServiceTemplateContainerArgs{
					Image: frontendImage,
					Resources: &cloudrunv2.ServiceTemplateContainerResourcesArgs{
						Limits: args.ResourceLimits,
					},
					Ports: cloudrunv2.ServiceTemplateContainerPortsArgs{
						ContainerPort: pulumi.Int(args.ContainerPort),
					},
					Envs: newFrontendEnvVars(args, backendURL),
					StartupProbe: &cloudrunv2.ServiceTemplateContainerStartupProbeArgs{
						TcpSocket: &cloudrunv2.ServiceTemplateContainerStartupProbeTcpSocketArgs{
							Port: pulumi.Int(args.ContainerPort),
						},
						InitialDelaySeconds: pulumi.Int(args.StartupProbe.InitialDelaySeconds),
						PeriodSeconds:       pulumi.Int(args.StartupProbe.PeriodSeconds),
						TimeoutSeconds:      pulumi.Int(args.StartupProbe.TimeoutSeconds),
						FailureThreshold:    pulumi.Int(args.StartupProbe.FailureThreshold),
					},
					LivenessProbe: &cloudrunv2.ServiceTemplateContainerLivenessProbeArgs{
						HttpGet: &cloudrunv2.ServiceTemplateContainerLivenessProbeHttpGetArgs{
							Path: pulumi.String(fmt.Sprintf("/%s", args.LivenessProbe.Path)),
							Port: pulumi.Int(args.ContainerPort),
						},
						InitialDelaySeconds: pulumi.Int(args.LivenessProbe.InitialDelaySeconds),
						PeriodSeconds:       pulumi.Int(args.LivenessProbe.PeriodSeconds),
						TimeoutSeconds:      pulumi.Int(args.LivenessProbe.TimeoutSeconds),
						FailureThreshold:    pulumi.Int(args.LivenessProbe.FailureThreshold),
					},
					VolumeMounts: &cloudrunv2.ServiceTemplateContainerVolumeMountArray{
						cloudrunv2.ServiceTemplateContainerVolumeMountArgs{
							MountPath: pulumi.String(args.SecretConfigFilePath),
							Name:      pulumi.String("envconfig"),
						},
					},
				},
			},
			ServiceAccount: serviceAccount.Email,
			Volumes: &cloudrunv2.ServiceTemplateVolumeArray{
				&cloudrunv2.ServiceTemplateVolumeArgs{
					Name: pulumi.String("envconfig"),
					Secret: &cloudrunv2.ServiceTemplateVolumeSecretArgs{
						Secret: configSecret.SecretId,
						Items: cloudrunv2.ServiceTemplateVolumeSecretItemArray{
							&cloudrunv2.ServiceTemplateVolumeSecretItemArgs{
								Path:    pulumi.String(args.SecretConfigFileName),
								Version: pulumi.String("latest"),
								Mode:    pulumi.IntPtr(0500),
							},
						},
					},
				},
			},
		},
		DeletionProtection: pulumi.Bool(args.DeletionProtection),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create frontend Cloud Run service: %w", err)
	}
	ctx.Export("cloud_run_service_frontend_id", frontendService.ID())
	ctx.Export("cloud_run_service_frontend_uri", frontendService.Uri)

	return frontendService, serviceAccount, nil
}

func newFrontendEnvVars(args *FrontendArgs, backendURL pulumi.StringOutput) cloudrunv2.ServiceTemplateContainerEnvArray {
	envVars := cloudrunv2.ServiceTemplateContainerEnvArray{
		cloudrunv2.ServiceTemplateContainerEnvArgs{
			Name:  pulumi.String("DOTENV_CONFIG_PATH"),
			Value: pulumi.String(fmt.Sprintf("%s%s", args.SecretConfigFilePath, args.SecretConfigFileName)),
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

func newBackendEnvVars(args *BackendArgs) cloudrunv2.ServiceTemplateContainerEnvArray {
	envVars := cloudrunv2.ServiceTemplateContainerEnvArray{
		cloudrunv2.ServiceTemplateContainerEnvArgs{
			Name:  pulumi.String("DOTENV_CONFIG_PATH"),
			Value: pulumi.String(fmt.Sprintf("%s%s", args.SecretConfigFilePath, args.SecretConfigFileName)),
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
