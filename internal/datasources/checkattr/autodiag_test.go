package checkattr

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/client"
)

// autodiag reads the same whether NodePing holds the boolean or the string it
// documents; a check without it reads as false, as before.
func TestFromAPIReadsAutoDiagInEitherShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		autodiag interface{}
		want     bool
	}{
		{true, true},
		{"true", true},
		{false, false},
		{"false", false},
		{nil, false},
	}
	for _, tt := range tests {
		var diags diag.Diagnostics
		m := FromAPI(context.Background(), &client.Check{ID: "CHK1", AutoDiag: tt.autodiag}, &diags)
		if diags.HasError() {
			t.Fatalf("autodiag %#v: %v", tt.autodiag, diags)
		}
		if m.AutoDiag.IsNull() || m.AutoDiag.ValueBool() != tt.want {
			t.Errorf("autodiag %#v read as %s, want %v", tt.autodiag, m.AutoDiag, tt.want)
		}
	}
}
