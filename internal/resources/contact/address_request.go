package contact

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
)

// addressRequest builds what a create or an update writes for one address
// block. It is the only place a block becomes a request: Create and Update
// used to build it in three copies, which drifted apart.
//
// prior is the address as the prior state holds it, when the update keeps it
// under its ID in `addresses`. It is nil for an address NodePing has yet to
// create, in `newaddresses`.
func addressRequest(ctx context.Context, addr AddressModel, prior *AddressModel) (client.AddressRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	req := client.AddressRequest{
		Address:       addr.Address.ValueString(),
		Type:          addr.Type.ValueString(),
		SuppressUp:    addr.SuppressUp.ValueBool(),
		SuppressDown:  addr.SuppressDown.ValueBool(),
		SuppressFirst: addr.SuppressFirst.ValueBool(),
		SuppressDiag:  addr.SuppressDiag.ValueBool(),
		SuppressAll:   addr.SuppressAll.ValueBool(),
	}

	// A new address sends mute either way, an existing one only when true:
	// an update cannot unmute an address. Muting is left to the web UI.
	if mute := addr.Mute.ValueBool(); prior == nil || mute {
		req.Mute = &mute
	}

	if !addr.Action.IsNull() {
		req.Action = addr.Action.ValueString()
	}
	if !addr.Data.IsNull() {
		req.Data = addr.Data.ValueString()
	}
	if !addr.Priority.IsNull() {
		priority := int(addr.Priority.ValueInt64())
		req.Priority = &priority
	}
	req.Headers = stringMap(ctx, addr.Headers, &diags)
	req.QueryStrings = stringMap(ctx, addr.QueryStrings, &diags)

	return req, diags
}

// stringMap returns a map attribute's elements, or nil for a null map.
func stringMap(ctx context.Context, m types.Map, diags *diag.Diagnostics) map[string]string {
	if m.IsNull() {
		return nil
	}
	out := make(map[string]string, len(m.Elements()))
	diags.Append(m.ElementsAs(ctx, &out, false)...)
	return out
}
