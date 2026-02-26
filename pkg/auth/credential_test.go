package auth

import (
	"testing"
)

func TestApiFromScopes_Empty(t *testing.T) {
	api := apiFromScopes(nil)
	// The API should be created with empty resource
	_ = api // No panic is the main assertion
}

func TestApiFromScopes_SingleScope(t *testing.T) {
	api := apiFromScopes([]string{"https://storage.azure.com/.default"})
	// The function trims "/.default" to get the resource
	_ = api // No panic is the main assertion
}

func TestApiFromScopes_NoDefaultSuffix(t *testing.T) {
	api := apiFromScopes([]string{"https://management.azure.com"})
	_ = api // Should work without /.default
}

func TestApiFromScopes_MultipleScopes(t *testing.T) {
	// When multiple scopes are provided, only the first one is used
	api := apiFromScopes([]string{
		"https://storage.azure.com/.default",
		"https://management.azure.com/.default",
	})
	_ = api
}
