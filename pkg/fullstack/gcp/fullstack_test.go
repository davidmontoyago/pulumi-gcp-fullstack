package gcp

import "testing"

// name is longer than max length
func TestNameIsLongerThanMaxLength(t *testing.T) {
	f := &FullStack{
		name: "this-is-a-long-name",
	}
	name := "ok-name"
	max := 20

	resourceName := f.newResourceName(name, "resource", max)

	expected := "this-is-a-lon-ok-res"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}

// name is longer than max length
func TestNameIsLongerThanMaxLength2(t *testing.T) {
	f := &FullStack{
		name: "ok-name",
	}
	name := "this-is-a-long-name"
	max := 15

	resourceName := f.newResourceName(name, "resource", max)

	expected := "o-this-is-a-lo-r"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}

// generates a resource name with name, service name and resource type
func TestGeneratesResourceName(t *testing.T) {
	f := &FullStack{
		name: "myapp",
	}
	serviceName := "backend"
	resourceType := "account"
	max := 30

	resourceName := f.newResourceName(serviceName, resourceType, max)

	expected := "myapp-backend-account"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}

// name, service name and resource type are longer than max length
func TestResourceNameIsLongerThanMaxLength(t *testing.T) {
	f := &FullStack{
		name: "this-is-a-very-long-app-name",
	}
	serviceName := "backend-service"
	resourceType := "service-account"
	max := 25

	resourceName := f.newResourceName(serviceName, resourceType, max)

	expected := "this-is-a-very-l-bac-serv"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}

// name, service name, and resourceType is empty, within max length
func TestResourceTypeEmptyWithinMaxLength(t *testing.T) {
	f := &FullStack{
		name: "myapp",
	}
	serviceName := "backend"
	resourceType := ""
	max := 20

	resourceName := f.newResourceName(serviceName, resourceType, max)

	expected := "myapp-backend"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}

// name, service name, and resourceType is empty, needs truncation
func TestResourceTypeEmptyNeedsTruncation(t *testing.T) {
	f := &FullStack{
		name: "this-is-a-very-long-app-name",
	}
	serviceName := "backend-service"
	resourceType := ""
	max := 15

	resourceName := f.newResourceName(serviceName, resourceType, max)

	// Truncation: two parts, so prefix and serviceName are truncated
	expected := "this-is-a-ver-b"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}
