package gcp

import (
	"fmt"

	cloudrunv2 "github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/cloudrunv2"
	secretmanager "github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/secretmanager"
	serviceAccount "github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func DeployBackendCloudRunInstance(ctx *pulumi.Context, backendName, backendImageName, project, region string) (*cloudrunv2.Service, *serviceAccount.Account, error) {
	accountName := fmt.Sprintf("%s-identity", backendName)
	serviceAccount, err := serviceAccount.NewAccount(ctx, accountName, &serviceAccount.AccountArgs{
		AccountId:   pulumi.String(accountName),
		DisplayName: pulumi.String(fmt.Sprintf("Backend service account (%s)", backendName)),
		Project:     pulumi.String(project),
	})
	if err != nil {
		return nil, nil, err
	}
	ctx.Export("cloud_run_service_backend_account_id", serviceAccount.ID())

	// TODO add default secret

	backendService, err := cloudrunv2.NewService(ctx, backendName, &cloudrunv2.ServiceArgs{
		Ingress:     pulumi.String("INGRESS_TRAFFIC_INTERNAL_ONLY"),
		Description: pulumi.String(fmt.Sprintf("Serverless instance (%s)", backendName)),
		Location:    pulumi.String(region),
		Project:     pulumi.String(project),
		Template: &cloudrunv2.ServiceTemplateArgs{
			Containers: cloudrunv2.ServiceTemplateContainerArray{
				&cloudrunv2.ServiceTemplateContainerArgs{
					Image: pulumi.String(backendImageName),
					Resources: &cloudrunv2.ServiceTemplateContainerResourcesArgs{
						// TODO make configurable
						Limits: pulumi.StringMap{
							"memory": pulumi.String("1Gi"),
							"cpu":    pulumi.String("1000m"),
						},
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

func DeployFrontendCloudRunInstance(ctx *pulumi.Context, serviceName, frontendImage, project, region string, backendURL pulumi.StringOutput) (*serviceAccount.Account, error) {
	// TODO concat frontend name
	accountName := "frontend-identity"
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
	secretID := fmt.Sprintf("%s-config", serviceName)
	configSecret, err := secretmanager.NewSecret(ctx, secretID, &secretmanager.SecretArgs{
		Labels: pulumi.StringMap{
			"frontend": pulumi.String("true"),
		},
		Replication: &secretmanager.SecretReplicationArgs{
			UserManaged: &secretmanager.SecretReplicationUserManagedArgs{
				Replicas: secretmanager.SecretReplicationUserManagedReplicaArray{
					&secretmanager.SecretReplicationUserManagedReplicaArgs{
						Location: pulumi.String(region),
					},
				},
			},
		},
		SecretId: pulumi.String(secretID),
	})
	if err != nil {
		return nil, err
	}

	// allow the frontend GSA to access the secret
	_, err = secretmanager.NewSecretIamMember(ctx, secretID, &secretmanager.SecretIamMemberArgs{
		Project:  pulumi.String(project),
		SecretId: configSecret.SecretId,
		Role:     pulumi.String("roles/secretmanager.secretAccessor"),
		Member:   pulumi.Sprintf("serviceAccount:%s", serviceAccount.Email),
	})
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
				// TODO make configurable
				MaxInstanceCount: pulumi.Int(3),
			},
			Containers: cloudrunv2.ServiceTemplateContainerArray{
				&cloudrunv2.ServiceTemplateContainerArgs{
					Image: pulumi.String(frontendImage),
					Resources: &cloudrunv2.ServiceTemplateContainerResourcesArgs{
						Limits: pulumi.StringMap{
							// TODO make configurable
							"memory": pulumi.String("2Gi"),
							"cpu":    pulumi.String("2000m"),
						},
					},
					Ports: cloudrunv2.ServiceTemplateContainerPortArray{
						cloudrunv2.ServiceTemplateContainerPortArgs{
							ContainerPort: pulumi.Int(3000),
						},
					},
					Envs: cloudrunv2.ServiceTemplateContainerEnvArray{
						cloudrunv2.ServiceTemplateContainerEnvArgs{
							// TODO make configurable
							Name:  pulumi.String("DOTENV_CONFIG_PATH"),
							Value: pulumi.String("/app/.next/config/.env.production"),
						},
						cloudrunv2.ServiceTemplateContainerEnvArgs{
							Name:  pulumi.String("BACKEND_API_URL"),
							Value: backendURL,
						},
					},
					VolumeMounts: &cloudrunv2.ServiceTemplateContainerVolumeMountArray{
						cloudrunv2.ServiceTemplateContainerVolumeMountArgs{
							// TODO make configurable
							MountPath: pulumi.String("/app/.next/config/"),
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
								// TODO make configurable
								Path:    pulumi.String(".env.production"),
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

	// TODO make configurable
	enableUnauthenticated := false
	if enableUnauthenticated {
		_, err = cloudrunv2.NewServiceIamMember(ctx, "frontend-allow-unauthenticated", &cloudrunv2.ServiceIamMemberArgs{
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
