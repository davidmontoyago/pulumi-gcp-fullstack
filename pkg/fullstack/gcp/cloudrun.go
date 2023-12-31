package gcp

import (
	"fmt"

	cloudrunv2 "github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/cloudrunv2"
	serviceAccount "github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

var (
	defaultBackendResourceLimits = pulumi.StringMap{
		"memory": pulumi.String("1Gi"),
		"cpu":    pulumi.String("1000m"),
	}
	defaultFrontendResourceLimits = pulumi.StringMap{
		"memory": pulumi.String("2Gi"),
		"cpu":    pulumi.String("2000m"),
	}
)

func (f *FullStack) deployBackendCloudRunInstance(ctx *pulumi.Context, args *BackendArgs) (*cloudrunv2.Service, *serviceAccount.Account, error) {
	if args == nil {
		args = &BackendArgs{
			&InstanceArgs{
				SecretConfigFileName: ".env",
				SecretConfigFilePath: "/app/config/",
				MaxInstanceCount:     3,
			},
		}
	}
	if args.ResourceLimits == nil {
		args.ResourceLimits = defaultBackendResourceLimits
	}

	backendName := f.BackendName
	accountName := f.newResourceName(backendName, 28)
	serviceAccount, err := serviceAccount.NewAccount(ctx, accountName, &serviceAccount.AccountArgs{
		AccountId:   pulumi.String(accountName),
		DisplayName: pulumi.String(fmt.Sprintf("Backend service account (%s)", backendName)),
		Project:     pulumi.String(f.Project),
	})
	if err != nil {
		return nil, nil, err
	}
	ctx.Export("cloud_run_service_backend_account_id", serviceAccount.ID())

	// TODO add default secret

	backendService, err := cloudrunv2.NewService(ctx, backendName, &cloudrunv2.ServiceArgs{
		Ingress:     pulumi.String("INGRESS_TRAFFIC_INTERNAL_ONLY"),
		Description: pulumi.String(fmt.Sprintf("Serverless instance (%s)", backendName)),
		Location:    pulumi.String(f.Region),
		Project:     pulumi.String(f.Project),
		Template: &cloudrunv2.ServiceTemplateArgs{
			Scaling: &cloudrunv2.ServiceTemplateScalingArgs{
				MaxInstanceCount: pulumi.Int(args.MaxInstanceCount),
			},
			Containers: cloudrunv2.ServiceTemplateContainerArray{
				&cloudrunv2.ServiceTemplateContainerArgs{
					Image: pulumi.String(f.BackendImage),
					Resources: &cloudrunv2.ServiceTemplateContainerResourcesArgs{
						Limits: args.ResourceLimits,
					},
					// TODO read config from secret
				},
			},
			ServiceAccount: serviceAccount.Email,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	ctx.Export("cloud_run_service_backend_id", backendService.ID())
	ctx.Export("cloud_run_service_backend_uri", backendService.Uri)

	return backendService, serviceAccount, nil
}

func (f *FullStack) deployFrontendCloudRunInstance(ctx *pulumi.Context, args *FrontendArgs, backendURL pulumi.StringOutput) (*serviceAccount.Account, error) {
	if args == nil {
		args = &FrontendArgs{}
	}
	if args.InstanceArgs == nil {
		args.InstanceArgs = &InstanceArgs{
			// default to a NextJs app
			SecretConfigFileName: ".env.production",
			SecretConfigFilePath: "/app/.next/config/",
			MaxInstanceCount:     3,
		}
	}
	if args.ResourceLimits == nil {
		args.ResourceLimits = defaultFrontendResourceLimits
	}

	frontendImage := f.FrontendImage
	project := f.Project
	region := f.Region

	serviceName := f.FrontendName
	accountName := f.newResourceName(serviceName, 28)
	serviceAccount, err := serviceAccount.NewAccount(ctx, accountName, &serviceAccount.AccountArgs{
		AccountId:   pulumi.String(accountName),
		DisplayName: pulumi.String(fmt.Sprintf("Frontend service account (%s)", serviceName)),
		Project:     pulumi.String(project),
	})
	if err != nil {
		return nil, err
	}
	ctx.Export("cloud_run_service_frontend_account_id", serviceAccount.ID())

	// create a secret to hold env vars for the cloud run instance
	configSecret, err := newEnvConfigSecret(ctx, serviceName, region, project, serviceAccount)
	if err != nil {
		return nil, err
	}

	frontendService, err := cloudrunv2.NewService(ctx, serviceName, &cloudrunv2.ServiceArgs{
		Name:        pulumi.String(serviceName),
		Ingress:     pulumi.String("INGRESS_TRAFFIC_INTERNAL_LOAD_BALANCER"),
		Description: pulumi.String(fmt.Sprintf("Serverless instance (%s)", serviceName)),
		Location:    pulumi.String(region),
		Project:     pulumi.String(project),
		Labels: pulumi.StringMap{
			"frontend": pulumi.String("true"),
			// TODO make optional
			// "gcb-trigger-id": buildTriggerID,
		},
		Template: &cloudrunv2.ServiceTemplateArgs{
			Scaling: &cloudrunv2.ServiceTemplateScalingArgs{
				MaxInstanceCount: pulumi.Int(args.MaxInstanceCount),
			},
			Containers: cloudrunv2.ServiceTemplateContainerArray{
				&cloudrunv2.ServiceTemplateContainerArgs{
					Image: pulumi.String(frontendImage),
					Resources: &cloudrunv2.ServiceTemplateContainerResourcesArgs{
						Limits: args.ResourceLimits,
					},
					Ports: cloudrunv2.ServiceTemplateContainerPortArray{
						cloudrunv2.ServiceTemplateContainerPortArgs{
							ContainerPort: pulumi.Int(3000),
						},
					},
					Envs: newFrontendEnvVars(args, backendURL),
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
			// TODO setup liveness/readiness probes
		},
	})
	if err != nil {
		return nil, err
	}
	ctx.Export("cloud_run_service_frontend_id", frontendService.ID())
	ctx.Export("cloud_run_service_frontend_uri", frontendService.Uri)

	if args.EnableUnauthenticated {
		_, err = cloudrunv2.NewServiceIamMember(ctx, fmt.Sprintf("%s-allow-unauthenticated", f.FrontendName), &cloudrunv2.ServiceIamMemberArgs{
			Name:     frontendService.Name,
			Project:  pulumi.String(project),
			Location: pulumi.String(region),
			Role:     pulumi.String("roles/run.invoker"),
			Member:   pulumi.Sprintf("allUsers"),
		})
		if err != nil {
			return nil, err
		}
	}

	return serviceAccount, nil
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
