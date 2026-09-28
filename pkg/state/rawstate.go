package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
)

// rawStateV4 is the subset of the OpenTofu/Terraform version 4 state file
// format (as printed by `tofu state pull`) that statebridge needs.
type rawStateV4 struct {
	Version   int                  `json:"version"`
	Resources []rawStateV4Resource `json:"resources"`
}

type rawStateV4Resource struct {
	Module    string                       `json:"module"`
	Mode      string                       `json:"mode"`
	Type      string                       `json:"type"`
	Name      string                       `json:"name"`
	Provider  string                       `json:"provider"`
	Instances []rawStateV4ResourceInstance `json:"instances"`
}

type rawStateV4ResourceInstance struct {
	IndexKey   interface{}            `json:"index_key"`
	Deposed    string                 `json:"deposed"`
	Attributes map[string]interface{} `json:"attributes"`
}

// parseRawState converts raw state (the output of `tofu state pull`) into a
// *tfjson.State shaped like the output of `tofu show -json`, so it can be
// consumed by NewStateIndex and FlattenState.
//
// Unlike `tofu show -json`, this needs no provider schemas, so it also works
// when the state was written by an older provider schema version than the one
// currently installed. The trade-off is that attribute values are returned as
// stored: they are not upgraded to the current schema, and dynamically-typed
// attributes keep their {"value": ..., "type": ...} wrapper.
//
// Deposed objects are skipped. Child modules are attached directly to the
// root module rather than nested, which FlattenState handles identically.
func parseRawState(data []byte) (*tfjson.State, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return &tfjson.State{}, nil
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // match terraform-exec's Show, which decodes numbers as json.Number
	var raw rawStateV4
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding raw state: %w", err)
	}
	if raw.Version != 4 {
		return nil, fmt.Errorf("unsupported raw state version %d (expected 4)", raw.Version)
	}

	root := &tfjson.StateModule{}
	modules := map[string]*tfjson.StateModule{"": root}
	for _, r := range raw.Resources {
		mod, ok := modules[r.Module]
		if !ok {
			mod = &tfjson.StateModule{Address: r.Module}
			modules[r.Module] = mod
			root.ChildModules = append(root.ChildModules, mod)
		}

		base := r.Type + "." + r.Name
		if r.Mode == string(tfjson.DataResourceMode) {
			base = "data." + base
		}
		if r.Module != "" {
			base = r.Module + "." + base
		}

		for _, inst := range r.Instances {
			if inst.Deposed != "" {
				continue
			}
			mod.Resources = append(mod.Resources, &tfjson.StateResource{
				Address:         base + FormatInstanceKey(inst.IndexKey),
				Mode:            tfjson.ResourceMode(r.Mode),
				Type:            r.Type,
				Name:            r.Name,
				Index:           inst.IndexKey,
				ProviderName:    providerSource(r.Provider),
				AttributeValues: inst.Attributes,
			})
		}
	}

	return &tfjson.State{Values: &tfjson.StateValues{RootModule: root}}, nil
}

// providerSource extracts the provider source address from a raw state
// provider config address, e.g.
// `provider["registry.opentofu.org/hashicorp/azurerm"].alias` →
// `registry.opentofu.org/hashicorp/azurerm`, matching `tofu show -json`.
func providerSource(configAddr string) string {
	source, _, _ := strings.Cut(strings.TrimPrefix(configAddr, `provider["`), `"]`)
	return source
}
