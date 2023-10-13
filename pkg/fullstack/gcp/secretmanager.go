package gcp

import (
	"fmt"

	secretmanager "github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/secretmanager"
	serviceAccount "github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func newEnvConfigSecret(ctx *pulumi.Context, serviceName, region, project string, serviceAccount *serviceAccount.Account) (*secretmanager.Secret, error) {
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

	// allow the instance GSA to access the secret
	_, err = secretmanager.NewSecretIamMember(ctx, secretID, &secretmanager.SecretIamMemberArgs{
		Project:  pulumi.String(project),
		SecretId: configSecret.SecretId,
		Role:     pulumi.String("roles/secretmanager.secretAccessor"),
		Member:   pulumi.Sprintf("serviceAccount:%s", serviceAccount.Email),
	})
	if err != nil {
		return nil, err
	}
	return configSecret, nil
}
