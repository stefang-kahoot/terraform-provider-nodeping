package contact

import (
	"context"
	"testing"

	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestContactSchemaIsValid(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	resp := &fwresource.SchemaResponse{}

	NewContactResource().Schema(ctx, fwresource.SchemaRequest{}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("Schema() returned diagnostics: %+v", resp.Diagnostics)
	}

	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("schema validation failed: %+v", diags)
	}
}

func TestContactSchemaCoreAttributes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	resp := &fwresource.SchemaResponse{}
	NewContactResource().Schema(ctx, fwresource.SchemaRequest{}, resp)

	id, ok := resp.Schema.Attributes["id"]
	if !ok {
		t.Fatal("schema is missing the id attribute")
	}
	if !id.IsComputed() {
		t.Error("id must be Computed")
	}

	name, ok := resp.Schema.Attributes["name"]
	if !ok {
		t.Fatal("schema is missing the name attribute")
	}
	// NodePing cannot clear a contact's name, so a configuration cannot go
	// without one.
	if !name.IsRequired() {
		t.Error("name must be Required")
	}
}

// A contact is useless without at least one address, so the block has to exist
// and be a list of nested objects.
func TestContactSchemaHasAddressBlock(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	resp := &fwresource.SchemaResponse{}
	NewContactResource().Schema(ctx, fwresource.SchemaRequest{}, resp)

	block, ok := resp.Schema.Blocks["address"]
	if !ok {
		t.Fatal("schema is missing the address block")
	}

	nested := block.GetNestedObject()
	if nested == nil {
		t.Fatal("address block has no nested object")
	}

	for _, name := range []string{"address", "type", "id"} {
		if _, ok := nested.GetAttributes()[name]; !ok {
			t.Errorf("address block is missing the %q attribute", name)
		}
	}
}

func TestContactSchemaAttributesAreUsable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	resp := &fwresource.SchemaResponse{}
	NewContactResource().Schema(ctx, fwresource.SchemaRequest{}, resp)

	for name, attr := range resp.Schema.Attributes {
		if !attr.IsRequired() && !attr.IsOptional() && !attr.IsComputed() {
			t.Errorf("attribute %q is neither Required, Optional nor Computed", name)
		}
	}

	if block, ok := resp.Schema.Blocks["address"]; ok {
		for name, attr := range block.GetNestedObject().GetAttributes() {
			if !attr.IsRequired() && !attr.IsOptional() && !attr.IsComputed() {
				t.Errorf("address attribute %q is neither Required, Optional nor Computed", name)
			}
		}
	}
}
