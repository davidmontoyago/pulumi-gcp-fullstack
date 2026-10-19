package connector

import (
	namer "github.com/davidmontoyago/commodity-namer"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Scope is where connector resources are deployed and how they are named.
type Scope struct {
	Project string
	Region  string
	Namer   namer.Namer
	Labels  pulumi.StringMap
	// Parent resource for the connector resources.
	Parent pulumi.Resource
}

// Args configures a tunnel connector process.
type Args struct {
	// Connector name, unique per scope (e.g. "cloudflared"). Required.
	Name string
	// Container image. Required. Pin a version, not latest.
	Image string
	// Container args (the image entrypoint is kept). Inputs, so service URIs can be interpolated.
	Args pulumi.StringArrayInput
	// Plain environment variables.
	EnvVars map[string]string
	// Credential to join the external network. Required. Stored in Secret Manager.
	Credential *Credential
	// Instance count. Defaults to 1. Use 2 for HA.
	Replicas int
	// Container limits. Defaults to the worker pool minimums: 0.08 vCPU and 512 MiB.
	ResourceLimits pulumi.StringMap
	// HTTP startup probe. Disabled if nil.
	StartupProbe *Probe
	// HTTP liveness probe. Disabled if nil.
	LivenessProbe *Probe
	// Whether to protect the worker pool from deletion. Defaults to false.
	DeletionProtection bool
}

// Credential is a secret delivered as a file (preferred) or as an env var.
// Exactly one of FilePath or EnvVar must be set.
type Credential struct {
	// Secret value (e.g. a tunnel token).
	Value pulumi.StringInput
	// Absolute path of the mounted file (e.g. "/etc/cloudflared/token").
	FilePath string
	// Env var name, for processes that only read credentials from the environment.
	EnvVar string
}

// Probe is an HTTP health check on the connector container.
type Probe struct {
	Path                string
	Port                int
	InitialDelaySeconds int
	PeriodSeconds       int
	TimeoutSeconds      int
	FailureThreshold    int
}

// NetworkArgs configures the network shared by the connectors of a scope.
type NetworkArgs struct {
	// Existing network name. Defaults to a dedicated network.
	// The private run.app zone affects every resource on it.
	NetworkName string
	// Existing subnet name. Required with NetworkName. Must have Private Google Access.
	SubnetName string
	// CIDR of the dedicated subnet. Defaults to "10.10.0.0/26".
	IPCidrRange string
}
