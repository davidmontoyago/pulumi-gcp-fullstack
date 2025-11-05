package gcp

import "github.com/pulumi/pulumi/sdk/v3/go/pulumi"

func setDefaultStringArray(input []string, defaultValue []string) pulumi.StringArrayOutput {
	if input == nil {
		input = defaultValue
	}
	result := make(pulumi.StringArray, 0, len(input))
	for _, v := range input {
		result = append(result, pulumi.String(v))
	}

	return result.ToStringArrayOutput()
}
