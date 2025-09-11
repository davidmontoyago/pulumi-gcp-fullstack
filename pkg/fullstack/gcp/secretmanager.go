package gcp

import (
	"fmt"

	secretmanager "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/secretmanager"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/serviceaccount"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func (f *FullStack) newEnvConfigSecret(ctx *pulumi.Context,
	serviceName string,
	serviceAccount *serviceaccount.Account,
	deletionProtection bool,
	labels pulumi.StringMap,
) (*secretmanager.Secret, error) {

	secretID := f.NewResourceName(serviceName, "config-secret", 63)

	configSecret, err := secretmanager.NewSecret(ctx, secretID, &secretmanager.SecretArgs{
		Labels: labels,
		Replication: &secretmanager.SecretReplicationArgs{
			// With google-managed default encryption
			Auto: &secretmanager.SecretReplicationAutoArgs{},
		},
		SecretId:           pulumi.String(secretID),
		DeletionProtection: pulumi.Bool(deletionProtection),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create secret: %w", err)
	}

	// allow the instance GSA to access the secret
	secretAccessorName := f.NewResourceName(serviceName, "secret-accessor", 63)
	_, err = secretmanager.NewSecretIamMember(ctx, secretAccessorName, &secretmanager.SecretIamMemberArgs{
		Project:  pulumi.String(f.Project),
		SecretId: configSecret.SecretId,
		Role:     pulumi.String("roles/secretmanager.secretAccessor"),
		Member:   pulumi.Sprintf("serviceAccount:%s", serviceAccount.Email),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to grant secret accessor: %w", err)
	}

	// Create initial empty secret version
	_, versionErr := secretmanager.NewSecretVersion(ctx, fmt.Sprintf("%s-secret-seed", secretID), &secretmanager.SecretVersionArgs{
		Secret:     configSecret.ID(),
		SecretData: pulumi.String(fmt.Sprintf("SERVICE_NAME=%s", serviceName)),
	},
		pulumi.IgnoreChanges([]string{"secretData"}),
	)

	if versionErr != nil {
		return nil, fmt.Errorf("failed to create secret version: %w", versionErr)
	}

	return configSecret, nil
}
