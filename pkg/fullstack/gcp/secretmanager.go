package gcp

import (
	secretmanager "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/secretmanager"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func (f *FullStack) newEnvConfigSecret(ctx *pulumi.Context, serviceName string, serviceAccount *serviceaccount.Account, deletionProtection bool, labels pulumi.StringMap) (*secretmanager.Secret, error) {
	secretID := f.newResourceName(serviceName, "config-secret", 100)

	configSecret, err := secretmanager.NewSecret(ctx, secretID, &secretmanager.SecretArgs{
		Labels: labels,
		Replication: &secretmanager.SecretReplicationArgs{
			UserManaged: &secretmanager.SecretReplicationUserManagedArgs{
				Replicas: secretmanager.SecretReplicationUserManagedReplicaArray{
					&secretmanager.SecretReplicationUserManagedReplicaArgs{
						Location: pulumi.String(f.Region),
					},
				},
			},
		},
		SecretId:           pulumi.String(secretID),
		DeletionProtection: pulumi.Bool(deletionProtection),
	})
	if err != nil {
		return nil, err
	}

	// allow the instance GSA to access the secret
	secretAccessorName := f.newResourceName(serviceName, "secret-accessor", 100)
	_, err = secretmanager.NewSecretIamMember(ctx, secretAccessorName, &secretmanager.SecretIamMemberArgs{
		Project:  pulumi.String(f.Project),
		SecretId: configSecret.SecretId,
		Role:     pulumi.String("roles/secretmanager.secretAccessor"),
		Member:   pulumi.Sprintf("serviceAccount:%s", serviceAccount.Email),
	})
	if err != nil {
		return nil, err
	}

	return configSecret, nil
}
