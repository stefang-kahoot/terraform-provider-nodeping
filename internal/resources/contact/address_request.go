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
//
// NodePing merges an update into an existing address field by field, keeping
// whatever the update leaves out, so what the configuration no longer sets
// has to be sent as cleared: suppress flags always go out, false included,
// and headers or query strings the prior state had and the plan has not go
// out as {}. A new address leaves out what is unset. Data cannot be cleared
// at all.
func addressRequest(ctx context.Context, addr AddressModel, prior *AddressModel) (client.AddressRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	priorHeaders := types.MapNull(types.StringType)
	priorQueryStrings := types.MapNull(types.StringType)
	if prior != nil {
		priorHeaders, priorQueryStrings = prior.Headers, prior.QueryStrings
	}

	req := client.AddressRequest{
		Address:       addr.Address.ValueString(),
		Type:          addr.Type.ValueString(),
		SuppressUp:    suppressFlag(addr.SuppressUp, prior),
		SuppressDown:  suppressFlag(addr.SuppressDown, prior),
		SuppressFirst: suppressFlag(addr.SuppressFirst, prior),
		SuppressDiag:  suppressFlag(addr.SuppressDiag, prior),
		SuppressAll:   suppressFlag(addr.SuppressAll, prior),
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
	req.Headers = sentMap(ctx, addr.Headers, priorHeaders, &diags)
	req.QueryStrings = sentMap(ctx, addr.QueryStrings, priorQueryStrings, &diags)

	return req, diags
}

// suppressFlag is a suppress flag as sent: for an existing address always,
// since NodePing keeps a flag an update leaves out, and for a new one only
// when true.
func suppressFlag(flag types.Bool, prior *AddressModel) *bool {
	v := flag.ValueBool()
	if prior == nil && !v {
		return nil
	}
	return &v
}

// sentMap is headers or query strings as sent. NodePing replaces the stored
// map with the one sent, and keeps it when the key is left out. So a map with
// entries is sent; one with none is sent as {} if the prior state had
// entries to clear, and left out (nil) otherwise.
func sentMap(ctx context.Context, planned, prior types.Map, diags *diag.Diagnostics) map[string]string {
	if m := stringMap(ctx, planned, diags); len(m) > 0 {
		return m
	}
	if len(prior.Elements()) > 0 {
		return map[string]string{}
	}
	return nil
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
