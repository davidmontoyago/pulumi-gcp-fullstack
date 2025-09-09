package gcp

import (
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/monitoring"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// ColdStartSLO tracks the container startup latency SLO
type ColdStartSLO struct {
	Slo         *monitoring.Slo
	AlertPolicy *monitoring.AlertPolicy
}

// setupColdStartSLO creates a Cloud Run Cold Start SLO to measure
// and optimized for faster boot times.
func (f *FullStack) setupColdStartSLO(ctx *pulumi.Context, cloudRunServiceName string, args *ColdStartSLOArgs) (*ColdStartSLO, error) {

	// Create a microservice to associate with the SLO
	// See:
	// https://cloud.google.com/stackdriver/docs/solutions/slo-monitoring/ui/define-svc
	customServiceName := f.newResourceName(cloudRunServiceName, "monitoring-service", 100)
	monitoringService, err := monitoring.NewGenericService(ctx, customServiceName, &monitoring.GenericServiceArgs{
		Project:     pulumi.String(f.Project),
		DisplayName: pulumi.String("Cloud Run Cold Start SLO monitored service"),
		ServiceId:   pulumi.String(customServiceName),
		BasicService: &monitoring.GenericServiceBasicServiceArgs{
			ServiceType: pulumi.String("CLOUD_RUN"),
			ServiceLabels: pulumi.StringMap{
				"service_name": pulumi.String(cloudRunServiceName),
				"location":     pulumi.String(f.Region),
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create generic service: %w", err)
	}

	// Create the SLO using Cloud Run's built-in container/startup_latencies metric
	sloName := f.newResourceName(cloudRunServiceName, "startup-latency-slo", 100)
	slo, err := monitoring.NewSlo(ctx, sloName, &monitoring.SloArgs{
		Project:     pulumi.String(f.Project),
		DisplayName: pulumi.String("Cloud Run Container Startup Latency SLO"),

		// Reference the Cloud Run service
		Service: monitoringService.ServiceId,

		Goal:              args.Goal,
		RollingPeriodDays: args.RollingPeriodDays,

		// Request-based SLI measuring latency distribution
		RequestBasedSli: &monitoring.SloRequestBasedSliArgs{
			// Distribution cut for latency-based SLI
			DistributionCut: &monitoring.SloRequestBasedSliDistributionCutArgs{
				Range: &monitoring.SloRequestBasedSliDistributionCutRangeArgs{
					// Boot times should stay within this range
					Min: pulumi.Float64(0.0),
					Max: args.MaxBootTimeMs,
				},

				// Filter for the Cloud Run container startup latencies metric
				DistributionFilter: pulumi.Sprintf(strings.Join([]string{
					`resource.type="cloud_run_revision"`,
					`resource.labels.service_name="%s"`,
					`metric.type="run.googleapis.com/container/startup_latencies"`,
				}, " AND "), cloudRunServiceName),
			},
		},
	}, pulumi.DependsOn([]pulumi.Resource{monitoringService}))
	if err != nil {
		return nil, fmt.Errorf("failed to create cold start SLO: %w", err)
	}

	coldStartSLO := &ColdStartSLO{
		Slo: slo,
	}

	// Create an alerting policy for SLO burn rate
	if args.AlertChannelID != "" {
		alertPolicy, err := f.setupSLOAlertPolicy(ctx, cloudRunServiceName, slo, args)
		if err != nil {
			return nil, fmt.Errorf("failed to create cold start SLO alert: %w", err)
		}
		coldStartSLO.AlertPolicy = alertPolicy
	}

	return coldStartSLO, nil
}

func (f *FullStack) setupSLOAlertPolicy(ctx *pulumi.Context, cloudRunServiceName string, slo *monitoring.Slo, args *ColdStartSLOArgs) (*monitoring.AlertPolicy, error) {
	alertPolicyName := f.newResourceName(cloudRunServiceName, "startup-latency-slo-alert", 100)
	alertPolicy, err := monitoring.NewAlertPolicy(ctx, alertPolicyName, &monitoring.AlertPolicyArgs{
		Project:     pulumi.String(f.Project),
		DisplayName: pulumi.String("Cloud Run Cold Start SLO Alert"),

		Conditions: monitoring.AlertPolicyConditionArray{
			&monitoring.AlertPolicyConditionArgs{
				DisplayName: pulumi.String("SLO burn rate too high"),

				ConditionThreshold: &monitoring.AlertPolicyConditionConditionThresholdArgs{
					Filter: slo.Name.ApplyT(func(name string) string {
						return fmt.Sprintf(strings.Join([]string{
							`resource.type="gce_instance"`,
							`metric.type="run.googleapis.com/container/startup_latencies"`,
							`metric.labels.slo_name="%s"`,
						}, " AND "), name)
					}).(pulumi.StringOutput),

					Comparison: pulumi.String("COMPARISON_GT"),

					ThresholdValue: pulumi.Float64(0.1), // Alert if burn rate > 10%

					Duration: pulumi.String("300s"), // 5 minutes

					Aggregations: monitoring.AlertPolicyConditionConditionThresholdAggregationArray{
						&monitoring.AlertPolicyConditionConditionThresholdAggregationArgs{
							AlignmentPeriod:  pulumi.String("300s"),
							PerSeriesAligner: pulumi.String("ALIGN_RATE"),
						},
					},
				},
			},
		},

		Combiner: pulumi.String("OR"),

		// Notification channel
		NotificationChannels: pulumi.StringArray{
			pulumi.String(fmt.Sprintf("projects/%s/notificationChannels/%s", f.Project, args.AlertChannelID)),
		},

		AlertStrategy: &monitoring.AlertPolicyAlertStrategyArgs{
			AutoClose: pulumi.String("1800s"), // 30 minutes
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create cold start SLO alert: %w", err)
	}

	return alertPolicy, nil
}
