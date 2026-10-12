package check

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/accountplan"
)

// refuseOffPlan fails the plan for each setting of the check that uses a
// NodePing feature the provider's declared plan, accountPlan, lacks (see the
// accountplan package):
//
//   - homeloc, verifyvolume and volumemin, set to anything;
//   - an interval of 0.25 or 0.5;
//   - autodiag = true. false is what NodePing holds when it is not in use.
//
// NodePing would drop the setting without a word, so every plan after the
// apply would show it again, or refuse it, failing the apply.
//
// It reads the plan, which for these attributes is the configuration or a
// schema default. A value not known yet is let through: Terraform plans the
// check again during the apply, with the value known, and this fails the
// apply there, before anything is sent.
func refuseOffPlan(ctx context.Context, plan tfsdk.Plan, accountPlan string, diags *diag.Diagnostics) {
	var homeloc types.String
	var verifyvolume, autodiag types.Bool
	var volumemin types.Int64
	var interval types.Float64
	diags.Append(plan.GetAttribute(ctx, path.Root("homeloc"), &homeloc)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("verifyvolume"), &verifyvolume)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("volumemin"), &volumemin)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("interval"), &interval)...)
	diags.Append(plan.GetAttribute(ctx, path.Root("autodiag"), &autodiag)...)
	if diags.HasError() {
		return
	}

	var uses []accountplan.Use
	for _, v := range []struct {
		name  string
		value attr.Value
	}{
		{"homeloc", homeloc},
		{"verifyvolume", verifyvolume},
		{"volumemin", volumemin},
	} {
		if !known(v.value) {
			continue
		}
		feature := accountplan.AudioVolume
		if v.name == "homeloc" {
			feature = accountplan.Location
		}
		uses = append(uses, accountplan.Use{
			Feature:   feature,
			Path:      path.Root(v.name),
			Setting:   v.name,
			Otherwise: fmt.Sprintf("Otherwise remove %s.", v.name),
		})
	}

	if known(interval) && (interval.ValueFloat64() == 0.25 || interval.ValueFloat64() == 0.5) {
		uses = append(uses, accountplan.Use{
			Feature:   accountplan.SubMinuteIntervals,
			Path:      path.Root("interval"),
			Setting:   fmt.Sprintf("interval = %g", interval.ValueFloat64()),
			Otherwise: "Otherwise set interval to 1 or more.",
		})
	}

	if known(autodiag) && autodiag.ValueBool() {
		uses = append(uses, accountplan.Use{
			Feature:   accountplan.AutomatedDiagnostics,
			Path:      path.Root("autodiag"),
			Setting:   "autodiag = true",
			Otherwise: "Otherwise set autodiag to false, or leave it out.",
		})
	}

	accountplan.Refuse(accountPlan, uses, diags)
}

// known reports whether a value is set and known.
func known(v attr.Value) bool {
	return !v.IsNull() && !v.IsUnknown()
}
