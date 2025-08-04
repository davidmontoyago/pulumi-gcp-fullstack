// Package config provides an environment config helper
package config

import (
	"fmt"
	"log"

	"github.com/kelseyhightower/envconfig"
)

// Config allows setting the fullstack via environment variables
type Config struct {
	GCPProject               string   `envconfig:"GCP_PROJECT" required:"true"`
	GCPRegion                string   `envconfig:"GCP_REGION" required:"true"`
	BackendImage             string   `envconfig:"BACKEND_IMAGE" required:"true"`
	FrontendImage            string   `envconfig:"FRONTEND_IMAGE" required:"true"`
	DomainURL                string   `envconfig:"DOMAIN_URL" required:"true"`
	EnableCloudArmor         bool     `envconfig:"ENABLE_CLOUD_ARMOR" default:"false"`
	ClientIPAllowlist        []string `envconfig:"CLIENT_IP_ALLOWLIST" default:""`
	EnablePrivateTrafficOnly bool     `envconfig:"ENABLE_PRIVATE_TRAFFIC_ONLY" default:"false"`
}

// LoadConfig loads configuration from environment variables
// All environment variables are required and will cause an error if not set
func LoadConfig() (*Config, error) {
	var config Config

	err := envconfig.Process("", &config)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration from environment variables: %w", err)
	}

	log.Printf("Configuration loaded successfully:")
	log.Printf("  GCP Project: %s", config.GCPProject)
	log.Printf("  GCP Region: %s", config.GCPRegion)
	log.Printf("  Backend Image: %s", config.BackendImage)
	log.Printf("  Frontend Image: %s", config.FrontendImage)
	log.Printf("  Domain URL: %s", config.DomainURL)
	log.Printf("  Enable Cloud Armor: %t", config.EnableCloudArmor)
	log.Printf("  Client IP Allowlist: %v", config.ClientIPAllowlist)
	log.Printf("  Enable Private Traffic Only: %t", config.EnablePrivateTrafficOnly)

	return &config, nil
}
