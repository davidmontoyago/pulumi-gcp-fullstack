// Package gcp provides Google Cloud Platform infrastructure components for fullstack applications.
package gcp

import (
	"fmt"
	"log"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/projects"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/storage"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Env vars for bucket configuration
//
//nolint:revive // Environment variable names should match their actual env var names
const (
	BUCKET_NAME = "BUCKET_NAME"
)

// deployBucket creates a Cloud Storage bucket with best-practice security and lifecycle policies
func (f *FullStack) deployBucket(ctx *pulumi.Context, args *BucketInstanceArgs) error {
	if err := ctx.Log.Debug("Deploying Cloud Storage bucket with config: %v", &pulumi.LogArgs{
		Resource: f,
	}); err != nil {
		log.Printf("failed to log bucket deployment with pulumi context: %v", err)
	}

	storageAPI, err := f.enableStorageAPI(ctx)
	if err != nil {
		return fmt.Errorf("failed to enable Storage API: %w", err)
	}

	bucket, err := f.createStorageBucket(ctx, args, storageAPI)
	if err != nil {
		return fmt.Errorf("failed to create storage bucket: %w", err)
	}

	f.storageBucket = bucket

	return nil
}

// enableStorageAPI enables the Cloud Storage API service
func (f *FullStack) enableStorageAPI(ctx *pulumi.Context) (*projects.Service, error) {
	return projects.NewService(ctx, f.NewResourceName("bucket", "storage-api", 63), &projects.ServiceArgs{
		Project:                  pulumi.String(f.Project),
		Service:                  pulumi.String("storage.googleapis.com"),
		DisableOnDestroy:         pulumi.Bool(false),
		DisableDependentServices: pulumi.Bool(false),
	}, pulumi.Parent(f))
}

// createStorageBucket creates a Cloud Storage bucket with security and lifecycle policies
func (f *FullStack) createStorageBucket(ctx *pulumi.Context, config *BucketInstanceArgs, storageAPI *projects.Service) (*storage.Bucket, error) {
	// Set defaults if not provided
	applyBucketConfigDefaults(config)

	bucketName := f.NewResourceName("bucket", "storage", 63)
	return storage.NewBucket(ctx, bucketName, &storage.BucketArgs{
		Name:         pulumi.String(bucketName),
		Project:      pulumi.String(f.Project),
		Location:     pulumi.String(config.Location),
		StorageClass: pulumi.String(config.StorageClass),
		Labels:       mergeLabels(f.Labels, pulumi.StringMap{"bucket": pulumi.String("true")}),
		ForceDestroy: pulumi.Bool(config.ForceDestroy),

		// Enable uniform bucket-level access for better security
		UniformBucketLevelAccess: pulumi.Bool(true),

		// Prevent public access
		PublicAccessPrevention: pulumi.String("enforced"),

		// Enable versioning for data protection
		Versioning: &storage.BucketVersioningArgs{
			Enabled: pulumi.Bool(true),
		},

		// Google-managed encryption (default)
		Encryption: &storage.BucketEncryptionArgs{
			DefaultKmsKeyName: pulumi.String(""),
		},

		// Lifecycle management for cost optimization
		LifecycleRules: storage.BucketLifecycleRuleArray{
			&storage.BucketLifecycleRuleArgs{
				Action: &storage.BucketLifecycleRuleActionArgs{
					Type: pulumi.String("Delete"),
				},
				Condition: &storage.BucketLifecycleRuleConditionArgs{
					Age: pulumi.Int(config.RetentionDays),
				},
			},
			// Delete old versions after 30 days
			&storage.BucketLifecycleRuleArgs{
				Action: &storage.BucketLifecycleRuleActionArgs{
					Type: pulumi.String("Delete"),
				},
				Condition: &storage.BucketLifecycleRuleConditionArgs{
					NumNewerVersions: pulumi.Int(10), // Keep up to 10 versions
					Age:              pulumi.Int(30), // Delete versions older than 30 days
				},
			},
		},

		// CORS configuration to allow access from Cloud Run
		Cors: storage.BucketCorArray{
			&storage.BucketCorArgs{
				Origins: pulumi.StringArray{
					pulumi.String("https://*.run.app"),
					pulumi.String("https://*.googleapis.com"),
				},
				Methods: pulumi.StringArray{
					pulumi.String("GET"),
					pulumi.String("POST"),
					pulumi.String("PUT"),
					pulumi.String("DELETE"),
					pulumi.String("HEAD"),
				},
				ResponseHeaders: pulumi.StringArray{
					pulumi.String("*"),
				},
				MaxAgeSeconds: pulumi.Int(3600),
			},
		},
	}, pulumi.Parent(f), pulumi.DependsOn([]pulumi.Resource{storageAPI}))
}

func applyBucketConfigDefaults(config *BucketInstanceArgs) {
	if config.StorageClass == "" {
		config.StorageClass = "STANDARD"
	}

	if config.Location == "" {
		config.Location = "US"
	}

	if config.RetentionDays == 0 {
		config.RetentionDays = 365
	}

	// ForceDestroy defaults to false for safety
}
