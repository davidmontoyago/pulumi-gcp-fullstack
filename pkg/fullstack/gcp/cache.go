// Package gcp provides Google Cloud Platform infrastructure components for fullstack applications.
package gcp

import (
	"encoding/base64"
	"fmt"
	"log"
	"strings"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/projects"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/redis"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/secretmanager"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/vpcaccess"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Env vars for Secret with Redis cache configuration
//
//nolint:revive // Environment variable names should match their actual env var names
const (
	REDIS_HOST         = "REDIS_HOST"
	REDIS_PORT         = "REDIS_PORT"
	REDIS_READ_HOST    = "REDIS_READ_HOST"
	REDIS_READ_PORT    = "REDIS_READ_PORT"
	REDIS_AUTH_STRING  = "REDIS_AUTH_STRING"
	REDIS_TLS_CA_CERTS = "REDIS_TLS_CA_CERTS"
)

// deployCache creates a Redis cache instance with private VPC access and firewall rules
func (f *FullStack) deployCache(ctx *pulumi.Context, args *CacheInstanceArgs) error {
	if err := ctx.Log.Debug("Deploying Redis cache with config: %v", &pulumi.LogArgs{
		Resource: f,
	}); err != nil {
		log.Printf("failed to log Redis cache deployment with pulumi context: %v", err)
	}

	redisAPI, err := f.enableRedisAPI(ctx)
	if err != nil {
		return fmt.Errorf("failed to enable Redis API: %w", err)
	}

	instance, err := f.createRedisInstance(ctx, args, redisAPI)
	if err != nil {
		return fmt.Errorf("failed to create Redis instance: %w", err)
	}

	// Create VPC access connector for Cloud Run to reach Redis' private IP
	connector, err := f.createVPCAccessConnector(ctx, instance.AuthorizedNetwork, args)
	if err != nil {
		return fmt.Errorf("failed to create VPC access connector: %w", err)
	}

	// Create firewall rule to allow Cloud Run to connect to Redis
	firewall, err := f.createCacheFirewallRule(ctx, connector, instance.Port, instance.AuthorizedNetwork)
	if err != nil {
		return fmt.Errorf("failed to create cache firewall rule: %w", err)
	}

	// Store Redis credentials in Secret Manager
	// Cloud run instance will automatically mount it as a volume
	secretVersion, err := f.secureCacheCredentials(ctx, instance)
	if err != nil {
		return fmt.Errorf("failed to secure cache credentials: %w", err)
	}

	f.redisInstance = instance
	f.vpcConnector = connector
	f.cacheFirewall = firewall
	f.cacheCredentialsSecret = secretVersion

	return nil
}

// enableRedisAPI enables the Redis API service
func (f *FullStack) enableRedisAPI(ctx *pulumi.Context) (*projects.Service, error) {
	return projects.NewService(ctx, f.NewResourceName("cache", "redis-api", 63), &projects.ServiceArgs{
		Project:                  pulumi.String(f.Project),
		Service:                  pulumi.String("redis.googleapis.com"),
		DisableOnDestroy:         pulumi.Bool(false),
		DisableDependentServices: pulumi.Bool(false),
	},
		pulumi.Parent(f),
		pulumi.RetainOnDelete(true),
	)
}

// createVPCAccessConnector creates a Serverless VPC Access connector for Cloud Run to reach private resources
func (f *FullStack) createVPCAccessConnector(ctx *pulumi.Context, cacheNetwork pulumi.StringOutput, args *CacheInstanceArgs) (*vpcaccess.Connector, error) {
	// Enable VPC Access API
	vpcAPI, err := projects.NewService(ctx, f.NewResourceName("cache", "vpcaccess-api", 63), &projects.ServiceArgs{
		Project:                  pulumi.String(f.Project),
		Service:                  pulumi.String("vpcaccess.googleapis.com"),
		DisableOnDestroy:         pulumi.Bool(false),
		DisableDependentServices: pulumi.Bool(false),
	},
		pulumi.Parent(f),
		pulumi.RetainOnDelete(true),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to enable VPC access API: %w", err)
	}

	connectorName := f.NewResourceName("cache", "vpc-connector", 25)

	return vpcaccess.NewConnector(ctx, connectorName, &vpcaccess.ConnectorArgs{
		Name:    pulumi.String(connectorName),
		Project: pulumi.String(f.Project),
		Region:  pulumi.String(f.Region),
		Network: cacheNetwork.ApplyT(func(network string) pulumi.StringInput {
			return pulumi.String(network)
		}).(pulumi.StringInput),
		IpCidrRange: pulumi.String(func() string {
			if args.ConnectorIPCidrRange == "" {
				return "10.8.0.0/28" // fallback to default
			}

			return args.ConnectorIPCidrRange
		}()),
		MinInstances: pulumi.Int(args.ConnectorMinInstances),
		MaxInstances: pulumi.Int(args.ConnectorMaxInstances),
	}, pulumi.Parent(f), pulumi.DependsOn([]pulumi.Resource{vpcAPI}))
}

// createCacheFirewallRule creates a firewall rule to allow Cloud Run to connect to Redis
func (f *FullStack) createCacheFirewallRule(ctx *pulumi.Context, connector *vpcaccess.Connector,
	instancePort pulumi.IntOutput,
	cacheNetwork pulumi.StringOutput) (*compute.Firewall, error) {

	firewall, err := compute.NewFirewall(ctx, f.NewResourceName("cache", "firewall", 63), &compute.FirewallArgs{
		Name:    pulumi.String(f.NewResourceName("cache", "allow-cloudrun-to-redis", 63)),
		Project: pulumi.String(f.Project),
		Network: cacheNetwork.ApplyT(func(network string) pulumi.StringInput {
			return pulumi.String(network)
		}).(pulumi.StringInput),
		Direction: pulumi.String("INGRESS"),
		Priority:  pulumi.Int(1000),
		Allows: compute.FirewallAllowArray{
			&compute.FirewallAllowArgs{
				Protocol: pulumi.String("tcp"),
				Ports: pulumi.StringArray{instancePort.ApplyT(func(port int) string {
					return fmt.Sprintf("%d", port)
				}).(pulumi.StringOutput)},
			},
		},
		SourceRanges: pulumi.StringArray{
			connector.IpCidrRange.ApplyT(func(ipCidrRange *string) string {
				if ipCidrRange != nil {
					return *ipCidrRange
				}

				if err := ctx.Log.Warn("No IP CIDR range found for connector, using fallback", nil); err != nil {
					log.Printf("failed to log IP CIDR details with pulumi context: %v", err)
				}

				return "10.8.0.0/28" // fallback to default
			}).(pulumi.StringOutput),
		},
		Description: pulumi.String("Allow TCP on Redis instace port from Cloud Run VPC Connector subnet"),
	}, pulumi.Parent(f))
	if err != nil {
		return nil, fmt.Errorf("failed to create cache firewall rule: %w", err)
	}

	return firewall, nil
}

// createRedisInstance creates a Redis instance with auth and TLS enabled
func (f *FullStack) createRedisInstance(ctx *pulumi.Context, config *CacheInstanceArgs, redisAPI *projects.Service) (*redis.Instance, error) {
	// Set defaults if not provided
	applyCacheConfigDefaults(config)

	return redis.NewInstance(ctx, f.NewResourceName("cache", "instance", 63), &redis.InstanceArgs{
		Name:                  pulumi.String(f.NewResourceName("cache", "instance", 63)),
		Project:               pulumi.String(f.Project),
		Region:                pulumi.String(f.Region),
		Tier:                  pulumi.String(config.Tier),
		MemorySizeGb:          pulumi.Int(config.MemorySizeGb),
		RedisVersion:          pulumi.String(config.RedisVersion),
		Labels:                mergeLabels(f.Labels, pulumi.StringMap{"cache": pulumi.String("true")}),
		AuthorizedNetwork:     pulumi.String(config.AuthorizedNetwork),
		AuthEnabled:           pulumi.Bool(true),
		TransitEncryptionMode: pulumi.String("SERVER_AUTHENTICATION"),
	}, pulumi.Parent(f), pulumi.DependsOn([]pulumi.Resource{redisAPI}))
}

// secureCacheCredentials stores Redis connection details in Secret Manager
func (f *FullStack) secureCacheCredentials(ctx *pulumi.Context, instance *redis.Instance) (*secretmanager.SecretVersion, error) {
	// Enable Secret Manager API
	secretManagerAPI, err := projects.NewService(ctx, f.NewResourceName("cache", "secretmanager-api", 63), &projects.ServiceArgs{
		Project: pulumi.String(f.Project),
		Service: pulumi.String("secretmanager.googleapis.com"),
	}, pulumi.Parent(f),
		pulumi.RetainOnDelete(true),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to enable Secret Manager API: %w", err)
	}

	// Create secret to store Redis credentials
	secretID := f.NewResourceName("cache", "creds", 63)
	secret, err := secretmanager.NewSecret(ctx, secretID, &secretmanager.SecretArgs{
		Project: pulumi.String(f.Project),
		Replication: &secretmanager.SecretReplicationArgs{
			// With google-managed default encryption
			Auto: &secretmanager.SecretReplicationAutoArgs{},
		},
		SecretId:           pulumi.String(secretID),
		DeletionProtection: pulumi.Bool(false),
		Labels:             mergeLabels(f.Labels, pulumi.StringMap{"cache-credentials": pulumi.String("true")}),
	}, pulumi.Parent(f),
		pulumi.DependsOn([]pulumi.Resource{secretManagerAPI}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache credentials secret: %w", err)
	}

	// Create dotenv format with Redis connection details
	dotenvData := createDotEnvSecretData(instance)

	// Create secret version with Redis credentials marked as sensitive
	return secretmanager.NewSecretVersion(ctx, f.NewResourceName("cache", "credentials-version", 63), &secretmanager.SecretVersionArgs{
		Secret: secret.ID(),
		SecretData: pulumi.ToSecret(dotenvData).(pulumi.StringOutput).ApplyT(func(s string) *string {
			return &s
		}).(pulumi.StringPtrOutput),
	}, pulumi.Parent(f), pulumi.DependsOn([]pulumi.Resource{secret}))
}

// createDotEnvSecretData creates dotenv format with Redis connection details
func createDotEnvSecretData(instance *redis.Instance) pulumi.StringOutput {
	return pulumi.All(
		instance.Host,
		instance.Port,
		instance.ReadEndpoint,
		instance.ReadEndpointPort,
		instance.AuthString,
		instance.ServerCaCerts,
	).ApplyT(func(args []interface{}) string {
		host := args[0].(string)
		port := args[1].(int)
		readEndpoint := args[2].(string)
		readEndpointPort := args[3].(int)
		authString := args[4].(string)
		serverCaCerts := args[5].([]redis.InstanceServerCaCert)

		// Concatenate all CA certificates if available
		var allCerts []string
		for _, cert := range serverCaCerts {
			if cert.Cert != nil && *cert.Cert != "" {
				allCerts = append(allCerts, *cert.Cert)
			}
		}

		concatenatedCerts := strings.Join(allCerts, "\n")
		// Base64 encode the concatenated certificates to avoid .env parsing issues
		encodedCerts := base64.StdEncoding.EncodeToString([]byte(concatenatedCerts))

		return fmt.Sprintf("%s=%s\n%s=%d\n%s=%s\n%s=%d\n%s=%s\n%s=%s",
			REDIS_HOST, host,
			REDIS_PORT, port,
			REDIS_READ_HOST, readEndpoint,
			REDIS_READ_PORT, readEndpointPort,
			REDIS_AUTH_STRING, authString,
			REDIS_TLS_CA_CERTS, encodedCerts)
	}).(pulumi.StringOutput)
}

func applyCacheConfigDefaults(config *CacheInstanceArgs) {
	if config.RedisVersion == "" {
		config.RedisVersion = "REDIS_7_0"
	}

	if config.Tier == "" {
		config.Tier = "BASIC"
	}

	if config.MemorySizeGb == 0 {
		config.MemorySizeGb = 1
	}

	if config.AuthorizedNetwork == "" {
		config.AuthorizedNetwork = "default"
	}

	if config.ConnectorMinInstances == 0 {
		config.ConnectorMinInstances = 2
	}

	if config.ConnectorMaxInstances == 0 {
		config.ConnectorMaxInstances = 3
	}
}
