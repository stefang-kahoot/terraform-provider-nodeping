package accountplan

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
)

// Which plans have which feature, from NodePing's pricing page, checked
// 2026-10-12. No plan declared has none of them.
func TestFeaturePlans(t *testing.T) {
	t.Parallel()

	tests := []struct {
		feature Feature
		has     map[string]bool
	}{
		{Location, map[string]bool{Premiere: true}},
		{AudioVolume, map[string]bool{Premiere: true}},
		{Voice, map[string]bool{Premiere: true}},
		{SubMinuteIntervals, map[string]bool{Professional: true, Premiere: true}},
		{AutomatedDiagnostics, map[string]bool{Personal: true, Professional: true, Premiere: true}},
	}
	for _, tt := range tests {
		for _, plan := range append([]string{"", "business", "bogus"}, Plans...) {
			if got := tt.feature.Has(plan); got != tt.has[plan] {
				t.Errorf("%s on plan %q: Has = %v, want %v", tt.feature.Name, plan, got, tt.has[plan])
			}
		}
	}
}

// The error names the setting and the plans that have the feature, says what
// NodePing does without it, and says to set plan in the provider block.
func TestRefuseExplains(t *testing.T) {
	t.Parallel()

	at := path.Root("address").AtListIndex(1).AtName("type")
	tests := []struct {
		name          string
		plan          string
		use           Use
		summary, text string
	}{
		{
			name:    "one plan has it, none declared",
			use:     Use{Feature: Location, Path: at, Setting: "homeloc", Otherwise: "Otherwise remove homeloc."},
			summary: "homeloc needs NodePing's Premiere plan",
			text: `homeloc uses the NodePing feature "Specify/roaming location", which only the Premiere plan has. ` +
				"The provider block does not set plan, which stands for an older or unknown plan without it. " +
				"On the grandfathered Business plan the provider is tested on, NodePing drops it without an error, " +
				"so every plan after the apply would show it again.\n\n" +
				`If the account has the Premiere plan, set plan = "premiere" in the provider block. Otherwise remove homeloc.`,
		},
		{
			name:    "two plans have it, another declared, NodePing refuses it",
			plan:    Personal,
			use:     Use{Feature: SubMinuteIntervals, Path: at, Setting: "interval = 0.5", Otherwise: "Otherwise set interval to 1 or more."},
			summary: "interval = 0.5 needs NodePing's Professional or Premiere plan",
			text: `interval = 0.5 uses the NodePing feature "Sub-minute intervals", which only the Professional and Premiere plans have. ` +
				`The provider block sets plan = "personal", which does not have it. ` +
				`On the grandfathered Business plan the provider is tested on, NodePing refuses it with "Invalid check interval", ` +
				"so the apply would fail.\n\n" +
				`If the account has one of those plans, set plan to it in the provider block, such as plan = "professional". ` +
				"Otherwise set interval to 1 or more.",
		},
		{
			name:    "three plans have it",
			use:     Use{Feature: AutomatedDiagnostics, Path: at, Setting: "autodiag = true", Otherwise: "Otherwise set autodiag to false, or leave it out."},
			summary: "autodiag = true needs NodePing's Personal, Professional or Premiere plan",
			text: `autodiag = true uses the NodePing feature "Automated Diagnostics", which only the Personal, Professional and Premiere plans have. ` +
				"The provider block does not set plan, which stands for an older or unknown plan without it. " +
				"On the grandfathered Business plan the provider is tested on, NodePing drops it without an error, " +
				"so every plan after the apply would show it again.\n\n" +
				`If the account has one of those plans, set plan to it in the provider block, such as plan = "personal". ` +
				"Otherwise set autodiag to false, or leave it out.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var diags diag.Diagnostics
			Refuse(tt.plan, []Use{tt.use}, &diags)
			if len(diags) != 1 || !diags.HasError() {
				t.Fatalf("got %v, want one error", diags)
			}
			withPath, ok := diags[0].(diag.DiagnosticWithPath)
			if !ok || !withPath.Path().Equal(at) {
				t.Errorf("error at %v, want %s", diags[0], at)
			}
			if got := diags[0].Summary(); got != tt.summary {
				t.Errorf("summary\n%s\nwant\n%s", got, tt.summary)
			}
			if got := diags[0].Detail(); got != tt.text {
				t.Errorf("detail\n%s\nwant\n%s", got, tt.text)
			}
		})
	}
}

// A use whose feature the plan has passes; each other one fails, in order.
func TestRefuseOnlyWhatThePlanLacks(t *testing.T) {
	t.Parallel()

	uses := []Use{
		{Feature: AutomatedDiagnostics, Path: path.Root("autodiag"), Setting: "autodiag = true"},
		{Feature: SubMinuteIntervals, Path: path.Root("interval"), Setting: "interval = 0.25"},
		{Feature: Location, Path: path.Root("homeloc"), Setting: "homeloc"},
	}
	tests := []struct {
		plan string
		want []string
	}{
		{"", []string{"autodiag", "interval", "homeloc"}},
		{Personal, []string{"interval", "homeloc"}},
		{Professional, []string{"homeloc"}},
		{Premiere, nil},
	}
	for _, tt := range tests {
		var diags diag.Diagnostics
		Refuse(tt.plan, uses, &diags)
		var got []string
		for _, d := range diags {
			got = append(got, d.(diag.DiagnosticWithPath).Path().String())
		}
		if len(got) != len(tt.want) {
			t.Errorf("plan %q refused %v, want %v", tt.plan, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("plan %q refused %v, want %v", tt.plan, got, tt.want)
				break
			}
		}
	}
}
