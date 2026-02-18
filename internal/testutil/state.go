// Package testutil provides test helpers and mock implementations
// for use across the tfmigrate test suite.
package testutil

import (
	"context"
	"fmt"

	tfjson "github.com/hashicorp/terraform-json"
)

// MockStateReader implements state.StateReader for tests.
// It returns pre-configured states keyed by layer path.
type MockStateReader struct {
	// States maps layer paths to their mock state.
	States map[string]*tfjson.State

	// Err, if set, is returned by all ReadState calls.
	Err error

	// ReadCount tracks how many times ReadState was called per layer path.
	ReadCount map[string]int
}

// NewMockStateReader creates a MockStateReader with the given states.
func NewMockStateReader(states map[string]*tfjson.State) *MockStateReader {
	return &MockStateReader{
		States:    states,
		ReadCount: make(map[string]int),
	}
}

// ReadState returns the mock state for the given layer path.
func (m *MockStateReader) ReadState(_ context.Context, layerPath string) (*tfjson.State, error) {
	if m.Err != nil {
		return nil, m.Err
	}

	m.ReadCount[layerPath]++

	s, ok := m.States[layerPath]
	if !ok {
		return nil, fmt.Errorf("no mock state for layer %q", layerPath)
	}
	return s, nil
}

// BuildState constructs a *tfjson.State with the given resources in the root module.
func BuildState(resources ...*tfjson.StateResource) *tfjson.State {
	return &tfjson.State{
		FormatVersion: "1.0",
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				Resources: resources,
			},
		},
	}
}

// BuildStateWithModules constructs a *tfjson.State with root resources
// and child modules.
func BuildStateWithModules(rootResources []*tfjson.StateResource, childModules []*tfjson.StateModule) *tfjson.State {
	return &tfjson.State{
		FormatVersion: "1.0",
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				Resources:    rootResources,
				ChildModules: childModules,
			},
		},
	}
}

// NewResource creates a simple StateResource for testing.
func NewResource(address, resType, name string, index interface{}, attrs map[string]interface{}) *tfjson.StateResource {
	return &tfjson.StateResource{
		Address:         address,
		Mode:            tfjson.ManagedResourceMode,
		Type:            resType,
		Name:            name,
		Index:           index,
		ProviderName:    "registry.opentofu.org/hashicorp/aws",
		AttributeValues: attrs,
	}
}
