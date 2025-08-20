package gcp

import (
	"fmt"
	"math"
	"strings"
)

func (f *FullStack) newResourceName(serviceName, resourceType string, maxLength int) string {
	var resourceName string
	if resourceType == "" {
		resourceName = fmt.Sprintf("%s-%s", f.name, serviceName)
	} else {
		resourceName = fmt.Sprintf("%s-%s-%s", f.name, serviceName, resourceType)
	}

	if len(resourceName) <= maxLength {
		return resourceName
	}

	surplus := len(resourceName) - maxLength

	// Calculate how much to truncate from each part
	var prefixSurplus, serviceSurplus, typeSurplus int
	if resourceType == "" {
		// Only two parts to truncate
		prefixSurplus = int(math.Ceil(float64(surplus) / 2))
		serviceSurplus = surplus - prefixSurplus
		typeSurplus = 0
	} else {
		prefixSurplus = int(math.Ceil(float64(surplus) / 3))
		serviceSurplus = int(math.Ceil(float64(surplus-prefixSurplus) / 2))
		typeSurplus = surplus - prefixSurplus - serviceSurplus
	}

	// Truncate each part, ensuring we don't truncate more than the part's length
	// and we keep at least one character to avoid leading dashes
	var shortPrefix string
	if prefixSurplus < len(f.name) {
		shortPrefix = f.name[:len(f.name)-prefixSurplus]
	} else {
		shortPrefix = f.name[:1]
	}

	var shortServiceName string
	if serviceSurplus < len(serviceName) {
		shortServiceName = serviceName[:len(serviceName)-serviceSurplus]
	} else {
		shortServiceName = serviceName[:1]
	}

	if resourceType == "" {
		resourceName = fmt.Sprintf("%s-%s",
			strings.TrimSuffix(shortPrefix, "-"),
			strings.TrimSuffix(shortServiceName, "-"),
		)
	} else {
		var shortResourceType string
		if typeSurplus < len(resourceType) {
			shortResourceType = resourceType[:len(resourceType)-typeSurplus]
		} else {
			shortResourceType = resourceType[:1]
		}

		resourceName = fmt.Sprintf("%s-%s-%s",
			strings.TrimSuffix(shortPrefix, "-"),
			strings.TrimSuffix(shortServiceName, "-"),
			strings.TrimSuffix(shortResourceType, "-"),
		)
	}

	return resourceName
}
