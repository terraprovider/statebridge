package engine

import (
	"context"
	"fmt"

	"github.com/redtenant/tfmigrate/pkg/state"
	tmpl "github.com/redtenant/tfmigrate/pkg/template"
)

// Resolver handles import ID resolution and wildcard expansion.
// It uses a StateReader to look up resource information from Terraform state
// and a template engine to evaluate address and import ID expressions.
type Resolver struct {
	stateReader state.StateReader
}

// NewResolver creates a Resolver with the given StateReader.
func NewResolver(sr state.StateReader) *Resolver {
	return &Resolver{stateReader: sr}
}

// ResolveImportID determines the import ID for a resource instance.
// Resolution priority:
//  1. If importIDExpr is a Go template, evaluate it with the resource context
//  2. If importIDExpr is a literal string, use it directly
//  3. If importIDExpr is empty, extract the "id" attribute from the resource state
//
// Returns an error if the import ID cannot be determined.
func (r *Resolver) ResolveImportID(resource *state.ResourceInfo, importIDExpr string) (string, error) {
	if importIDExpr == "" {
		return r.importIDFromState(resource)
	}

	if tmpl.IsTemplate(importIDExpr) {
		ctx := buildTemplateContext(resource)
		return tmpl.Evaluate(importIDExpr, ctx)
	}

	return importIDExpr, nil
}

// ExpandWildcard expands a wildcard source address (ending with [*]) against
// state, resolves the destination address and import ID for each instance
// using Go templates, and returns the expanded instances.
func (r *Resolver) ExpandWildcard(
	ctx context.Context,
	sourceLayerPath string,
	sourceAddress string,
	destAddressExpr string,
	importIDExpr string,
) ([]ExpandedInstance, error) {
	baseAddr := BaseAddress(sourceAddress)

	s, err := r.stateReader.ReadState(ctx, sourceLayerPath)
	if err != nil {
		return nil, fmt.Errorf("reading state for layer %q: %w", sourceLayerPath, err)
	}

	resources, err := state.LookupResourcesByPrefix(s, baseAddr)
	if err != nil {
		return nil, fmt.Errorf("expanding wildcard %q: %w", sourceAddress, err)
	}

	var instances []ExpandedInstance
	for _, res := range resources {
		tctx := buildTemplateContext(res)

		destAddr, err := r.resolveAddress(destAddressExpr, tctx)
		if err != nil {
			return nil, fmt.Errorf("resolving destination address for %q: %w", res.Address, err)
		}

		importID, err := r.ResolveImportID(res, importIDExpr)
		if err != nil {
			return nil, fmt.Errorf("resolving import ID for %q: %w", res.Address, err)
		}

		instances = append(instances, ExpandedInstance{
			SourceResource: res,
			DestAddress:    destAddr,
			ImportID:       importID,
		})
	}

	return instances, nil
}

// ResolveSingleMove resolves a non-wildcard move operation, looking up the
// source resource in state to extract its import ID if needed.
func (r *Resolver) ResolveSingleMove(
	ctx context.Context,
	sourceLayerPath string,
	sourceAddress string,
	destAddress string,
	importIDExpr string,
) (*ExpandedInstance, error) {
	s, err := r.stateReader.ReadState(ctx, sourceLayerPath)
	if err != nil {
		return nil, fmt.Errorf("reading state for layer %q: %w", sourceLayerPath, err)
	}

	resource, err := state.LookupResource(s, sourceAddress)
	if err != nil {
		// If import_id is explicitly provided, we don't need the resource in state
		if importIDExpr != "" && !tmpl.IsTemplate(importIDExpr) {
			return &ExpandedInstance{
				SourceResource: &state.ResourceInfo{Address: sourceAddress},
				DestAddress:    destAddress,
				ImportID:       importIDExpr,
			}, nil
		}
		return nil, fmt.Errorf("looking up resource %q in state: %w", sourceAddress, err)
	}

	tctx := buildTemplateContext(resource)

	resolvedDest, err := r.resolveAddress(destAddress, tctx)
	if err != nil {
		return nil, fmt.Errorf("resolving destination address: %w", err)
	}

	importID, err := r.ResolveImportID(resource, importIDExpr)
	if err != nil {
		return nil, fmt.Errorf("resolving import ID: %w", err)
	}

	return &ExpandedInstance{
		SourceResource: resource,
		DestAddress:    resolvedDest,
		ImportID:       importID,
	}, nil
}

// resolveAddress evaluates an address expression, which may be a literal
// or a Go template.
func (r *Resolver) resolveAddress(addrExpr string, tctx *tmpl.TemplateContext) (string, error) {
	if tmpl.IsTemplate(addrExpr) {
		return tmpl.Evaluate(addrExpr, tctx)
	}
	return addrExpr, nil
}

// importIDFromState extracts the import ID from a resource's state attributes.
// It looks for the "id" attribute, which is present on most cloud resources.
func (r *Resolver) importIDFromState(resource *state.ResourceInfo) (string, error) {
	if resource.Attributes == nil {
		return "", fmt.Errorf("resource %q has no attributes in state; provide an explicit import_id", resource.Address)
	}

	id, ok := resource.Attributes["id"]
	if !ok {
		return "", fmt.Errorf("resource %q has no 'id' attribute in state; provide an explicit import_id", resource.Address)
	}

	idStr, ok := id.(string)
	if !ok {
		return "", fmt.Errorf("resource %q 'id' attribute is not a string; provide an explicit import_id", resource.Address)
	}

	if idStr == "" {
		return "", fmt.Errorf("resource %q has an empty 'id' attribute; provide an explicit import_id", resource.Address)
	}

	return idStr, nil
}

// buildTemplateContext creates a TemplateContext from a ResourceInfo.
func buildTemplateContext(res *state.ResourceInfo) *tmpl.TemplateContext {
	return &tmpl.TemplateContext{
		Address:    res.Address,
		Type:       res.Type,
		Name:       res.Name,
		Index:      res.Index,
		Key:        res.Key,
		Attributes: res.Attributes,
	}
}
