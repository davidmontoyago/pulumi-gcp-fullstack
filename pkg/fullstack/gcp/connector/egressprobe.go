package connector

import (
	"fmt"

	cloudrunv2 "github.com/pulumi/pulumi-gcp/sdk/v10/go/gcp/cloudrunv2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const (
	// EgressProbeImage is the default egress probe image: busybox 1.37, pinned by digest.
	EgressProbeImage = "docker.io/library/busybox:1.37@sha256:a7dbc319b1438115a207e11930dc4bb3e643dcb3ec0bdd694bb36cd8fad289ec"

	egressProbeName        = "egress-probe"
	defaultEgressProbePort = 8081
	// Cloud Run serves run.app over HTTPS only.
	egressProbeTargetPort = 443

	// Connects with busybox nc until the target accepts a TCP connection, then serves the local
	// port with busybox httpd so the TCP startup probe passes. The sidecar keeps running.
	egressProbeScript = `until nc -z -w 5 "$EGRESS_PROBE_HOST" "$EGRESS_PROBE_TARGET_PORT"; do
  echo "egress probe: waiting for TCP $EGRESS_PROBE_HOST:$EGRESS_PROBE_TARGET_PORT"
  sleep 2
done
echo "egress probe: TCP $EGRESS_PROBE_HOST:$EGRESS_PROBE_TARGET_PORT reachable"
mkdir -p /tmp/egress-probe
exec httpd -f -p "$EGRESS_PROBE_PORT" -h /tmp/egress-probe`
)

// defaultEgressProbeResourceLimits are the probe sidecar's limits.
// Unverified: the minimum Cloud Run accepts for a worker pool sidecar.
var defaultEgressProbeResourceLimits = pulumi.StringMap{
	"cpu":    pulumi.String("80m"),
	"memory": pulumi.String("128Mi"),
}

// Direct VPC egress can take a minute or more to connect after an instance starts, so the probe
// waits up to 3 minutes (36 × 5s).
// See: https://docs.cloud.google.com/run/docs/configuring/vpc-direct-vpc#limitations
const (
	egressProbePeriodSeconds    = 5
	egressProbeTimeoutSeconds   = 3
	egressProbeFailureThreshold = 36
)

// newEgressProbeContainer returns the sidecar running the TCP egress check.
func newEgressProbeContainer(probe *EgressProbe) *cloudrunv2.WorkerPoolTemplateContainerArgs {
	return &cloudrunv2.WorkerPoolTemplateContainerArgs{
		Name:     pulumi.String(egressProbeName),
		Image:    pulumi.String(probe.Image),
		Commands: pulumi.ToStringArray([]string{"sh", "-c", egressProbeScript}),
		Envs: cloudrunv2.WorkerPoolTemplateContainerEnvArray{
			&cloudrunv2.WorkerPoolTemplateContainerEnvArgs{
				Name:  pulumi.String("EGRESS_PROBE_HOST"),
				Value: probe.Host,
			},
			&cloudrunv2.WorkerPoolTemplateContainerEnvArgs{
				Name:  pulumi.String("EGRESS_PROBE_TARGET_PORT"),
				Value: pulumi.String(fmt.Sprintf("%d", egressProbeTargetPort)),
			},
			&cloudrunv2.WorkerPoolTemplateContainerEnvArgs{
				Name:  pulumi.String("EGRESS_PROBE_PORT"),
				Value: pulumi.String(fmt.Sprintf("%d", probe.Port)),
			},
		},
		Resources: &cloudrunv2.WorkerPoolTemplateContainerResourcesArgs{Limits: probe.ResourceLimits},
		// TCP startup probe on the local listener, which only opens after the egress check passes.
		StartupProbe: &cloudrunv2.WorkerPoolTemplateContainerStartupProbeArgs{
			TcpSocket: &cloudrunv2.WorkerPoolTemplateContainerStartupProbeTcpSocketArgs{
				Port: pulumi.Int(probe.Port),
			},
			PeriodSeconds:    pulumi.Int(egressProbePeriodSeconds),
			TimeoutSeconds:   pulumi.Int(egressProbeTimeoutSeconds),
			FailureThreshold: pulumi.Int(egressProbeFailureThreshold),
		},
	}
}

// egressProbeWithDefaults validates the probe and returns a copy with defaults applied.
func egressProbeWithDefaults(connectorName string, probe *EgressProbe) (*EgressProbe, error) {
	if probe.Host == nil {
		return nil, fmt.Errorf("egress probe host is required for connector %s", connectorName)
	}
	if connectorName == egressProbeName {
		return nil, fmt.Errorf("connector name %q is reserved for the egress probe", egressProbeName)
	}
	if probe.Port < 0 {
		return nil, fmt.Errorf("egress probe port must not be negative for connector %s", connectorName)
	}

	defaulted := *probe
	if defaulted.Image == "" {
		defaulted.Image = EgressProbeImage
	}
	if defaulted.Port == 0 {
		defaulted.Port = defaultEgressProbePort
	}
	if defaulted.ResourceLimits == nil {
		defaulted.ResourceLimits = defaultEgressProbeResourceLimits
	}

	return &defaulted, nil
}
