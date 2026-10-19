package connector

import (
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	// CloudflaredImage is the default cloudflared image.
	// See: https://github.com/cloudflare/cloudflared/releases
	CloudflaredImage = "docker.io/cloudflare/cloudflared:2026.10.1"

	cloudflaredName         = "cloudflared"
	cloudflaredAuthFilePath = "/etc/cloudflared/token"
	cloudflaredMetricsPort  = 2000
	// Returns 200 once at least one edge connection is up.
	cloudflaredReadyPath = "/ready"
)

// Cloudflared returns the args for a Cloudflare Tunnel connector running with the given token.
// The tunnel's ingress rules are managed remotely in Cloudflare, so no upstream URLs are needed.
// See: https://developers.cloudflare.com/tunnel/reference/run-parameters/
func Cloudflared(token pulumi.StringInput) *Args {
	return &Args{
		Name:  cloudflaredName,
		Image: CloudflaredImage,
		// The image entrypoint is "cloudflared --no-autoupdate".
		Args: pulumi.ToStringArray([]string{
			"tunnel",
			"--loglevel", "warn",
			// Avoids a slow QUIC (UDP) attempt before falling back.
			"--protocol", "http2",
			"--metrics", fmt.Sprintf("0.0.0.0:%d", cloudflaredMetricsPort),
			"run",
			// Requires cloudflared >= 2025.4.0
			"--token-file", cloudflaredAuthFilePath,
		}),
		// Size the Go runtime to the fractional CPU quota and the 512 MiB limit.
		EnvVars: map[string]string{
			"GOMAXPROCS": "1",
			"GOMEMLIMIT": "400MiB",
		},
		Credential: &Credential{
			Value:    token,
			FilePath: cloudflaredAuthFilePath,
		},
		// Boot is CPU-throttled at 0.08 vCPU, so allow up to 2 minutes.
		StartupProbe: &Probe{
			Path:             cloudflaredReadyPath,
			Port:             cloudflaredMetricsPort,
			PeriodSeconds:    5,
			TimeoutSeconds:   3,
			FailureThreshold: 24,
		},
		LivenessProbe: &Probe{
			Path:             cloudflaredReadyPath,
			Port:             cloudflaredMetricsPort,
			PeriodSeconds:    30,
			TimeoutSeconds:   5,
			FailureThreshold: 3,
		},
	}
}
