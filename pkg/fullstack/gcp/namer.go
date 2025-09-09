package gcp

import (
	"fmt"
	"math"
)

// Namer provides consistent resource naming with length constraints
type Namer struct {
	baseName string
}

// NewNamer creates a new Namer instance with the given base name
func NewNamer(baseName string) *Namer {
	return &Namer{baseName: baseName}
}

// NewResourceName generates a consistent resource name with length limits.
func (e *Namer) NewResourceName(serviceName, resourceType string, maxLength int) string {
	var resourceName string
	if resourceType == "" {
		resourceName = fmt.Sprintf("%s-%s", e.baseName, serviceName)
	} else {
		resourceName = fmt.Sprintf("%s-%s-%s", e.baseName, serviceName, resourceType)
	}

	if len(resourceName) <= maxLength {
		return resourceName
	}

	surplus := len(resourceName) - maxLength
	resourceName = e.truncateResourceName(serviceName, resourceType, surplus, maxLength)

	return resourceName
}

// truncateResourceName handles the complex logic for truncating resource names.
func (e *Namer) truncateResourceName(serviceName, resourceType string, surplus, maxLength int) string {
	mainComponentLength := len(e.baseName)
	if mainComponentLength > surplus {
		return e.truncateMainComponent(serviceName, resourceType, surplus)
	}

	return e.proportionalTruncate(serviceName, resourceType, maxLength)
}

// truncateMainComponent truncates the main component name when it's long enough.
func (e *Namer) truncateMainComponent(serviceName, resourceType string, surplus int) string {
	truncatedMainComponent := e.baseName[:len(e.baseName)-surplus]
	if resourceType == "" {
		return fmt.Sprintf("%s-%s", truncatedMainComponent, serviceName)
	}

	return fmt.Sprintf("%s-%s-%s", truncatedMainComponent, serviceName, resourceType)
}

// proportionalTruncate applies proportional truncation when main component is too short.
func (e *Namer) proportionalTruncate(serviceName, resourceType string, maxLength int) string {
	originalLength := len(fmt.Sprintf("%s-%s-%s", e.baseName, serviceName, resourceType))
	if resourceType == "" {
		originalLength = len(fmt.Sprintf("%s-%s", e.baseName, serviceName))
	}

	truncateFactorFloat := float64(maxLength) / float64(originalLength)
	truncateFactor := math.Floor(truncateFactorFloat*100) / 100

	mainComponentLength := int(math.Floor(float64(len(e.baseName)) * truncateFactor))
	serviceNameLength := int(math.Floor(float64(len(serviceName)) * truncateFactor))
	resourceTypeLength := int(math.Floor(float64(len(resourceType)) * truncateFactor))

	if resourceType == "" {
		return fmt.Sprintf("%s-%s", e.baseName[:mainComponentLength], serviceName[:serviceNameLength])
	}

	return fmt.Sprintf("%s-%s-%s", e.baseName[:mainComponentLength], serviceName[:serviceNameLength], resourceType[:resourceTypeLength])
}
