package gcp

import "testing"

// generates a resource name with prefix and name
func TestGeneratesResourceNameWithPrefixAndName(t *testing.T) {
	f := &FullStack{
		prefix: "prefix",
	}
	name := "name"
	max := 20

	resourceName := f.newResourceName(name, max)

	expected := "prefix-name"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}

// prefix is longer than max length
func TestPrefixIsLongerThanMaxLength(t *testing.T) {
	f := &FullStack{
		prefix: "this-is-a-long-prefix",
	}
	name := "ok-name"
	max := 20

	resourceName := f.newResourceName(name, max)

	expected := "this-is-a-long-p-ok"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}

// name is longer than max length
func TestNameIsLongerThanMaxLength(t *testing.T) {
	f := &FullStack{
		prefix: "ok-prefix",
	}
	name := "this-is-a-long-name"
	max := 15

	resourceName := f.newResourceName(name, max)

	expected := "ok-this-is-a-lo"
	if resourceName != expected {
		t.Errorf("Expected resource name to be %s, but got %s", expected, resourceName)
	}
}
