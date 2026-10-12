// Package accountplan says which NodePing features need which NodePing plan,
// and refuses a setting that uses one the provider's declared plan lacks.
//
// NodePing sells some features with some of its plans only (its pricing
// page and API documentation, checked 2026-10-12). On an account without
// one, NodePing drops a setting that uses it without a word, or refuses it:
// the plan after the apply would show the setting again, every time, or the
// apply would fail. The provider's plan setting declares the account's
// plan. Left out, it stands for an older or unknown plan, such as a
// grandfathered Business plan, which is taken to have none of the features
// below. A configuration that uses a feature the declared plan lacks fails
// the plan instead.
//
// What each feature does on an account without it was observed on a
// grandfathered Business plan, the only kind of account the provider is
// tested on. The behaviour on the Personal, Professional and Premiere plans
// is untested.
package accountplan

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
)

// The plans the provider's plan setting takes.
const (
	Personal     = "personal"
	Professional = "professional"
	Premiere     = "premiere"
)

// Plans are the values the provider's plan setting takes.
var Plans = []string{Personal, Professional, Premiere}

// Feature is a NodePing feature that only some plans have.
type Feature struct {
	// Name is NodePing's name for it.
	Name string
	// Plans are the plans that have it.
	Plans []string
	// Refusal is NodePing's error for a setting that uses the feature on an
	// account without it. Empty where NodePing drops the setting instead.
	Refusal string
}

// The features some plans lack, and the settings that use them:
var (
	// Location: a check's homeloc, any value.
	Location = Feature{Name: "Specify/roaming location", Plans: []string{Premiere}}
	// AudioVolume: a check's verifyvolume and volumemin, any value.
	AudioVolume = Feature{Name: "AUDIO volume detection", Plans: []string{Premiere}}
	// Voice: a contact address of type voice. NodePing refuses it with HTTP
	// 400.
	Voice = Feature{Name: "Voice notifications", Plans: []string{Premiere}, Refusal: "Illegal address type."}
	// SubMinuteIntervals: a check's interval of 0.25 or 0.5.
	SubMinuteIntervals = Feature{Name: "Sub-minute intervals", Plans: []string{Professional, Premiere}, Refusal: "Invalid check interval"}
	// AutomatedDiagnostics: a check's autodiag = true, and a contact
	// address's suppress_diag = true. false is what NodePing holds when
	// neither is in use, and is always allowed.
	AutomatedDiagnostics = Feature{Name: "Automated Diagnostics", Plans: []string{Personal, Professional, Premiere}}
)

// Has reports whether plan has the feature. "", no plan declared, has none.
func (f Feature) Has(plan string) bool {
	return slices.Contains(f.Plans, plan)
}

// Use is a setting in a configuration that uses a feature.
type Use struct {
	Feature Feature
	// Path is the attribute the setting is on. The error points at it.
	Path path.Path
	// Setting is the setting as the configuration has it, such as homeloc
	// or `type = "voice"`.
	Setting string
	// Otherwise says what to do if the account does not have the feature.
	Otherwise string
}

// Refuse adds an error for each use whose feature plan lacks.
func Refuse(plan string, uses []Use, diags *diag.Diagnostics) {
	for _, u := range uses {
		if !u.Feature.Has(plan) {
			diags.AddAttributeError(u.Path, u.summary(), u.detail(plan))
		}
	}
}

func (u Use) summary() string {
	return fmt.Sprintf("%s needs NodePing's %s plan", u.Setting, list(titles(u.Feature.Plans), "or"))
}

func (u Use) detail(plan string) string {
	f := u.Feature
	plans := titles(f.Plans)

	has := fmt.Sprintf("only the %s plans have", list(plans, "and"))
	if len(plans) == 1 {
		has = fmt.Sprintf("only the %s plan has", plans[0])
	}

	declared := "The provider block does not set plan, which stands for an older or unknown plan without it."
	if plan != "" {
		declared = fmt.Sprintf("The provider block sets plan = %q, which does not have it.", plan)
	}

	without := "drops it without an error, so every plan after the apply would show it again"
	if f.Refusal != "" {
		without = fmt.Sprintf("refuses it with %q, so the apply would fail", f.Refusal)
	}

	set := fmt.Sprintf("If the account has the %s plan, set plan = %q in the provider block.", plans[0], f.Plans[0])
	if len(plans) > 1 {
		set = fmt.Sprintf("If the account has one of those plans, set plan to it in the provider block, such as plan = %q.", f.Plans[0])
	}

	return fmt.Sprintf("%s uses the NodePing feature %q, which %s. %s "+
		"On the grandfathered Business plan the provider is tested on, NodePing %s.\n\n%s %s",
		u.Setting, f.Name, has, declared, without, set, u.Otherwise)
}

// titles returns plans as NodePing names them: personal as Personal.
func titles(plans []string) []string {
	out := make([]string, len(plans))
	for i, p := range plans {
		out[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return out
}

// list joins words as prose: "a", "a or b", "a, b or c".
func list(words []string, conjunction string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " " + conjunction + " " + words[len(words)-1]
}
