package gcp

import "github.com/pulumi/pulumi/sdk/v3/go/pulumi"

// mergeLabels adds custom to default labels.
func mergeLabels(defaultLabels map[string]string, additionalLabels pulumi.StringMap) pulumi.StringMap {
	merged := make(pulumi.StringMap)

	for k, v := range defaultLabels {
		merged[k] = pulumi.String(v)
	}

	// Add additional labels (these will override any conflicting keys)
	for k, v := range additionalLabels {
		merged[k] = v
	}

	return merged
}
