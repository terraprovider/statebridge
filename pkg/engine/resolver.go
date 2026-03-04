package engine

import (
	"context"
	"fmt"

	tfjson "github.com/hashicorp/terraform-json"
	"github.com/redtenant/tfmigrate/pkg/state"
	tmpl "github.com/redtenant/tfmigrate/pkg/template"
)

// Resolver handles import ID resolution, template evaluation, and state lookups.
// It uses a StateReader to look up resource information from Terraform state
// and the template package to evaluate address and import ID expressions.
type Resolver struct {
	stateReader state.StateReader
}

// NewResolver creates a Resolver with the given StateReader.
func NewResolver(sr state.StateReader) *Resolver {
	return &Resolver{stateReader: sr}
}

// ReadState reads the raw Terraform state for a given layer path.
// Used by the engine for condition evaluation against state.
func (r *Resolver) ReadState(ctx context.Context, layerPath string) (*tfjson.State, error) {
	s, err := r.stateReader.ReadState(ctx, layerPath)
	if err != nil {
		return nil, fmt.Errorf("reading state for layer %q: %w", layerPath, err)
	}
	return s, nil
}

// LookupResources returns all for_each instances of a managed resource from state.
// The baseAddr should be the resource address without any key suffix
// (e.g., "aws_s3_bucket.data", not "aws_s3_bucket.data[*]").
// Data sources are excluded from the results since they are auto-computed
// and never need import/removed blocks.
func (r *Resolver) LookupResources(
	ctx context.Context,
	layerPath string,
	baseAddr string,
) ([]*state.ResourceInfo, error) {
	s, err := r.stateReader.ReadState(ctx, layerPath)
	if err != nil {
		return nil, fmt.Errorf("reading state for layer %q: %w", layerPath, err)
	}

	resources, err := state.LookupResourcesByPrefix(s, baseAddr)
	if err != nil {
		return nil, fmt.Errorf("looking up resources for %q: %w", baseAddr, err)
	}

	managed := filterManagedResources(resources)
	if len(managed) == 0 {
		return nil, fmt.Errorf("no managed resources matching %q found in state (data sources are excluded from migrations)", baseAddr)
	}

	return managed, nil
}

// LookupResource returns a single (non-for_each) managed resource from state.
// Returns an error if the resource is a data source.
func (r *Resolver) LookupResource(
	ctx context.Context,
	layerPath string,
	address string,
) (*state.ResourceInfo, error) {
	s, err := r.stateReader.ReadState(ctx, layerPath)
	if err != nil {
		return nil, fmt.Errorf("reading state for layer %q: %w", layerPath, err)
	}

	resource, err := state.LookupResource(s, address)
	if err != nil {
		return nil, fmt.Errorf("looking up resource %q: %w", address, err)
	}

	if resource.Mode == "data" {
		return nil, fmt.Errorf("resource %q is a data source (data sources are excluded from migrations)", address)
	}

	return resource, nil
}

// filterManagedResources returns only managed resources, filtering out data sources.
func filterManagedResources(resources []*state.ResourceInfo) []*state.ResourceInfo {
	var managed []*state.ResourceInfo
	for _, r := range resources {
		if r.Mode == "managed" {
			managed = append(managed, r)
		}
	}
	return managed
}

// LookupModuleResources returns all managed resource instances under a module
// prefix from state. Returns resources at any nesting depth, including
// individual for_each instances. Data sources are excluded.
// Returns an error if no managed resources are found under the module.
func (r *Resolver) LookupModuleResources(
	ctx context.Context,
	layerPath string,
	modulePrefix string,
) ([]*state.ResourceInfo, error) {
	s, err := r.stateReader.ReadState(ctx, layerPath)
	if err != nil {
		return nil, fmt.Errorf("reading state for layer %q: %w", layerPath, err)
	}

	idx := state.NewStateIndex(s)
	resources := idx.ManagedResourcesUnderModule(modulePrefix)
	if len(resources) == 0 {
		return nil, fmt.Errorf("no managed resources found under %q in layer %q", modulePrefix, layerPath)
	}

	return resources, nil
}

// LookupAllManagedResources returns all managed resource instances in a
// layer's state. Data sources are excluded.
// Returns an error if no managed resources are found.
func (r *Resolver) LookupAllManagedResources(
	ctx context.Context,
	layerPath string,
) ([]*state.ResourceInfo, error) {
	s, err := r.stateReader.ReadState(ctx, layerPath)
	if err != nil {
		return nil, fmt.Errorf("reading state for layer %q: %w", layerPath, err)
	}

	idx := state.NewStateIndex(s)
	resources := idx.AllManagedResources()
	if len(resources) == 0 {
		return nil, fmt.Errorf("no managed resources found in layer %q", layerPath)
	}

	return resources, nil
}

// EvaluateTemplate evaluates a Go template expression using the given resource
// as context. Returns the rendered string. If the expression contains no
// template directives ({{ }}), it is returned as-is.
func (r *Resolver) EvaluateTemplate(expr string, resource *state.ResourceInfo) (string, error) {
	if !tmpl.IsTemplate(expr) {
		return expr, nil
	}
	ctx := buildTemplateContext(resource)
	return tmpl.Evaluate(expr, ctx)
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

// buildExpandedTemplateContext creates a TemplateContext from a ResourceInfo
// with additional Item and ItemIndex fields for attribute expansion.
func buildExpandedTemplateContext(res *state.ResourceInfo, item interface{}, itemIndex int) *tmpl.TemplateContext {
	return &tmpl.TemplateContext{
		Address:    res.Address,
		Type:       res.Type,
		Name:       res.Name,
		Index:      res.Index,
		Key:        res.Key,
		Attributes: res.Attributes,
		Item:       item,
		ItemIndex:  itemIndex,
	}
}

// ExpandedInstance represents a single resource instance produced by expanding
// a keyed resource against state.
type ExpandedInstance struct {
	// SourceResource is the resource info from the source state.
	SourceResource *state.ResourceInfo

	// DestAddress is the rendered destination address for this instance.
	DestAddress string

	// ImportID is the resolved import ID for this instance.
	ImportID string
}
