package check

import (
	"context"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/stefang-kahoot/terraform-provider-nodeping/internal/datasources/checkattr"
	"github.com/stefang-kahoot/terraform-provider-nodeping/testutil"
)

// The table as coded says exactly what the probe found, for every check type:
// testutil.NotStored is the probe's own output, not a copy of the table.
func TestStoredByMatchesTheProbe(t *testing.T) {
	t.Parallel()

	probe := testutil.NotStored()
	probed := make([]string, 0, len(probe))
	for typ := range probe {
		probed = append(probed, typ)
	}
	sort.Strings(probed)
	valid := append([]string(nil), ValidCheckTypes...)
	sort.Strings(valid)
	if !slices.Equal(probed, valid) {
		t.Fatalf("the probe covers %v, the schema allows %v", probed, valid)
	}

	for _, typ := range ValidCheckTypes {
		for _, name := range probe[typ] {
			if _, listed := storedBy[name]; !listed {
				t.Errorf("the probe found %s not stored on %s, and storedBy does not list it", name, typ)
			}
		}
		for name := range storedBy {
			want := !slices.Contains(probe[typ], name)
			if got := stores(typ, name); got != want {
				t.Errorf("stores(%s, %s) = %v, the probe found %v", typ, name, got, want)
			}
		}
	}
}

// Every attribute in the table is one of the resource's, and the provider
// sends it to NodePing under the name the probe sent.
func TestStoredByNamesAttributesThatAreSent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	attributes := CheckSchema().Attributes
	for name := range storedBy {
		if _, ok := attributes[name]; !ok {
			t.Errorf("%s is not an attribute of the check resource", name)
		}
	}

	headers := types.MapValueMust(types.StringType, map[string]attr.Value{"X-A": types.StringValue("1")})
	model := &CheckResourceModel{
		Type:           types.StringValue("HTTPADV"),
		Target:         types.StringValue("https://example.com"),
		Regex:          types.BoolValue(true),
		Method:         types.StringValue("POST"),
		StatusCode:     types.Int64Value(201),
		SendHeaders:    headers,
		ReceiveHeaders: headers,
		PostData:       types.StringValue("a=1"),
		Fields:         fieldsValue(t, map[string]checkattr.FieldModel{"A": field("status", num(1), nil, "")}),
		Username:       types.StringValue("svc"),
		Password:       types.StringValue("s3cret"),
		Secure:         types.StringValue("ssl"),
		DNSType:        types.StringValue("A"),
		DNSToResolve:   types.StringValue("example.com"),
		DNSSection:     types.StringValue("answer"),
		DNSRD:          types.BoolValue(true),
		Transport:      types.StringValue("tcp"),
		WarningDays:    types.Int64Value(21),
		ServerName:     types.StringValue("example.com"),
		Email:          types.StringValue("probe@example.com"),
		Database:       types.StringValue("app"),
		Query:          types.StringValue("SELECT 1"),
		Namespace:      types.StringValue("app.events"),
		SSHKey:         types.StringValue("KEY-1"),
		ClientCert:     types.StringValue("CERT-1"),
		SNMPv:          types.StringValue("2c"),
		SNMPCom:        types.StringValue("community"),
	}
	var diags diag.Diagnostics
	sent := requestJSON(t, (&CheckResource{}).buildCreateRequest(ctx, model, &diags))
	if diags.HasError() {
		t.Fatalf("build: %v", diags)
	}
	for name := range storedBy {
		if _, ok := sent[name]; !ok {
			t.Errorf("a create does not send %s under that name", name)
		}
	}
}

func TestSendsUnstored(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	a := field("status", num(200), num(200), "")
	b := field("load.avg", nil, num(2.5), "")
	held := fieldsValue(t, map[string]checkattr.FieldModel{"A": a, "B": b})
	changed := fieldsValue(t, map[string]checkattr.FieldModel{"A": a, "B": field("load.avg", nil, num(3), "")})
	unknownMax := b
	unknownMax.Max = types.Float64Unknown()
	partlyUnknown := fieldsValue(t, map[string]checkattr.FieldModel{"A": a, "B": unknownMax})
	emptyMap := types.MapValueMust(types.StringType, map[string]attr.Value{})
	headers := types.MapValueMust(types.StringType, map[string]attr.Value{"X-A": types.StringValue("1")})

	tests := []struct {
		name    string
		planned attr.Value
		prior   attr.Value
		create  bool
		want    bool
	}{
		{"create: a string", types.StringValue("sni.example.com"), nil, true, true},
		{"create: true", types.BoolValue(true), nil, true, true},
		{"create: a number", types.Int64Value(201), nil, true, true},
		{"create: a map", headers, nil, true, true},
		{"create: fields", held, nil, true, true},
		{"create: fields not all known yet", partlyUnknown, nil, true, true},
		{"create: null", types.StringNull(), nil, true, false},
		{"create: empty string", types.StringValue(""), nil, true, false},
		{"create: false", types.BoolValue(false), nil, true, false},
		{"create: 0", types.Int64Value(0), nil, true, false},
		{"create: empty map", emptyMap, nil, true, false},
		{"create: unknown", types.StringUnknown(), nil, true, false},

		{"update: a held value unchanged", held, held, false, false},
		{"update: a held value changed", changed, held, false, true},
		{"update: a held value removed", fieldsValue(t, nil), held, false, true},
		{"update: a value added", types.StringValue("sni.example.com"), types.StringNull(), false, true},
		{"update: a held string removed", types.StringNull(), types.StringValue("sni.example.com"), false, true},
		{"update: false where there was none", types.BoolValue(false), types.BoolNull(), false, false},
		{"update: none where there was false", types.BoolNull(), types.BoolValue(false), false, false},
		{"update: true where there was false", types.BoolValue(true), types.BoolValue(false), false, true},
		{"update: unknown", types.StringUnknown(), types.StringValue("sni.example.com"), false, false},
		{"update: fields not all known yet", partlyUnknown, held, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sendsUnstored(ctx, tt.planned, tt.prior, tt.create); got != tt.want {
				t.Errorf("sendsUnstored = %v, want %v", got, tt.want)
			}
		})
	}
}

// checkObject returns a check, as a plan or state holds it, with the given
// attributes set and the rest null.
func checkObject(t *testing.T, values map[string]attr.Value) tftypes.Value {
	t.Helper()
	ctx := context.Background()
	s := CheckSchema()
	state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
	for name, value := range values {
		if diags := state.SetAttribute(ctx, path.Root(name), value); diags.HasError() {
			t.Fatalf("%s: %v", name, diags)
		}
	}
	return state.Raw
}

func TestRefuseUnstored(t *testing.T) {
	t.Parallel()

	a := field("status", num(200), num(200), "")
	b := field("content.400.defaultOutput", num(1), num(99), "")
	held := fieldsValue(t, map[string]checkattr.FieldModel{"A": a, "B": b})
	changed := fieldsValue(t, map[string]checkattr.FieldModel{"A": a, "B": field("content.400.defaultOutput", num(1), num(98), "")})
	oneLeft := fieldsValue(t, map[string]checkattr.FieldModel{"A": a})
	typ := types.StringValue

	tests := []struct {
		name string
		// prior is nil for a create.
		prior, planned map[string]attr.Value
		// errors lists the attributes refused, with the kind of their
		// detail: "create", "replace" or "update".
		errors map[string]string
		// replace lists the attributes whose removal replaces the check,
		// each with a warning.
		replace []string
	}{
		{
			name:    "a new HTTPADV check with servername",
			planned: map[string]attr.Value{"type": typ("HTTPADV"), "servername": types.StringValue("sni.example.com")},
			errors:  map[string]string{"servername": "create"},
		},
		{
			name:    "a new HTTP check with regex = false",
			planned: map[string]attr.Value{"type": typ("HTTP"), "regex": types.BoolValue(false), "contentstring": types.StringValue("ok")},
		},
		{
			name:    "a new HTTP check with fields and a method",
			planned: map[string]attr.Value{"type": typ("HTTP"), "fields": held, "method": types.StringValue("GET")},
			errors:  map[string]string{"fields": "create", "method": "create"},
		},
		{
			name:    "a new SSL check with servername",
			planned: map[string]attr.Value{"type": typ("SSL"), "servername": types.StringValue("sni.example.com")},
		},
		{
			name:    "a type not known yet",
			planned: map[string]attr.Value{"type": types.StringUnknown(), "servername": types.StringValue("sni.example.com")},
		},
		{
			name:    "an HTTP check holding fields, unchanged",
			prior:   map[string]attr.Value{"type": typ("HTTP"), "fields": held},
			planned: map[string]attr.Value{"type": typ("HTTP"), "fields": held},
		},
		{
			name:    "an HTTP check holding fields, one changed",
			prior:   map[string]attr.Value{"type": typ("HTTP"), "fields": held},
			planned: map[string]attr.Value{"type": typ("HTTP"), "fields": changed},
			errors:  map[string]string{"fields": "update"},
		},
		{
			name:    "an HTTP check holding fields, changed to HTTPPARSE with one changed",
			prior:   map[string]attr.Value{"type": typ("HTTP"), "fields": held},
			planned: map[string]attr.Value{"type": typ("HTTPPARSE"), "fields": changed},
		},
		{
			name:    "an HTTPPARSE check changed to HTTP, its fields unchanged",
			prior:   map[string]attr.Value{"type": typ("HTTPPARSE"), "fields": held},
			planned: map[string]attr.Value{"type": typ("HTTP"), "fields": held},
		},
		{
			name:    "an HTTPPARSE check changed to HTTP and a field changed",
			prior:   map[string]attr.Value{"type": typ("HTTPPARSE"), "fields": held},
			planned: map[string]attr.Value{"type": typ("HTTP"), "fields": changed},
			errors:  map[string]string{"fields": "update"},
		},
		{
			name:    "an HTTPADV check holding servername, removed",
			prior:   map[string]attr.Value{"type": typ("HTTPADV"), "servername": types.StringValue("sni.example.com")},
			planned: map[string]attr.Value{"type": typ("HTTPADV")},
			replace: []string{"servername"},
		},
		{
			name:    "an HTTPADV check holding servername, changed",
			prior:   map[string]attr.Value{"type": typ("HTTPADV"), "servername": types.StringValue("sni.example.com")},
			planned: map[string]attr.Value{"type": typ("HTTPADV"), "servername": types.StringValue("example.com")},
			errors:  map[string]string{"servername": "update"},
		},
		{
			name:    "an HTTP check holding regex = true, set to false",
			prior:   map[string]attr.Value{"type": typ("HTTP"), "regex": types.BoolValue(true)},
			planned: map[string]attr.Value{"type": typ("HTTP"), "regex": types.BoolValue(false)},
			replace: []string{"regex"},
		},
		{
			name:    "an SSL check changed to HTTPADV, servername removed",
			prior:   map[string]attr.Value{"type": typ("SSL"), "servername": types.StringValue("sni.example.com")},
			planned: map[string]attr.Value{"type": typ("HTTPADV")},
			replace: []string{"servername"},
		},
		{
			name:    "an HTTP check holding regex, changed to HTTPCONTENT and regex removed",
			prior:   map[string]attr.Value{"type": typ("HTTP"), "regex": types.BoolValue(true)},
			planned: map[string]attr.Value{"type": typ("HTTPCONTENT")},
		},
		{
			// The new check would not hold fields either.
			name:    "an HTTP check holding regex and fields, regex removed",
			prior:   map[string]attr.Value{"type": typ("HTTP"), "regex": types.BoolValue(true), "fields": held},
			planned: map[string]attr.Value{"type": typ("HTTP"), "fields": held},
			replace: []string{"regex"},
			errors:  map[string]string{"fields": "replace"},
		},
		{
			// replaceOnRemovedFields replaces the check, and warns; this
			// only treats the plan as a create.
			name:    "an HTTP check holding fields, all removed",
			prior:   map[string]attr.Value{"type": typ("HTTP"), "fields": held},
			planned: map[string]attr.Value{"type": typ("HTTP")},
		},
		{
			name:    "an HTTP check holding fields, one removed",
			prior:   map[string]attr.Value{"type": typ("HTTP"), "fields": held},
			planned: map[string]attr.Value{"type": typ("HTTP"), "fields": oneLeft},
			errors:  map[string]string{"fields": "replace"},
		},
		{
			name:    "an HTTP check holding regex = false, left out",
			prior:   map[string]attr.Value{"type": typ("HTTP"), "regex": types.BoolValue(false)},
			planned: map[string]attr.Value{"type": typ("HTTP")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			s := CheckSchema()

			state := tfsdk.State{Schema: s, Raw: tftypes.NewValue(s.Type().TerraformType(ctx), nil)}
			if tt.prior != nil {
				state.Raw = checkObject(t, tt.prior)
			}
			plan := tfsdk.Plan{Schema: s, Raw: checkObject(t, tt.planned)}
			resp := &resource.ModifyPlanResponse{Plan: plan}
			refuseUnstored(ctx, resource.ModifyPlanRequest{Plan: plan, State: state}, resp)

			got := map[string]string{}
			var warned []string
			for _, d := range resp.Diagnostics {
				withPath, ok := d.(diag.DiagnosticWithPath)
				if !ok {
					t.Fatalf("diagnostic %q has no path", d.Summary())
				}
				name := withPath.Path().String()
				if d.Severity() == diag.SeverityWarning {
					if want := "Removing " + name + " replaces the check"; d.Summary() != want {
						t.Errorf("warning %q, want %q", d.Summary(), want)
					}
					warned = append(warned, name)
					continue
				}
				if want := name + " is not stored for "; !strings.HasPrefix(d.Summary(), want) {
					t.Errorf("summary %q, want it to start %q", d.Summary(), want)
				}
				switch {
				case strings.Contains(d.Detail(), "This plan replaces the check"):
					got[name] = "replace"
				case strings.Contains(d.Detail(), "it accepts the value and drops it"):
					got[name] = "create"
				case strings.Contains(d.Detail(), "so an update cannot add or change it"):
					got[name] = "update"
				default:
					got[name] = d.Detail()
				}
				if want := "The check types that store " + name + ": " + strings.Join(storedBy[name], ", ") + "."; !strings.Contains(d.Detail(), want) {
					t.Errorf("%s: detail lacks %q:\n%s", name, want, d.Detail())
				}
			}
			var replaced []string
			for _, p := range resp.RequiresReplace {
				replaced = append(replaced, p.String())
			}
			if !slices.Equal(replaced, tt.replace) {
				t.Errorf("RequiresReplace %v, want %v", replaced, tt.replace)
			}
			if !slices.Equal(warned, tt.replace) {
				t.Errorf("warned about %v, want %v", warned, tt.replace)
			}

			want := tt.errors
			if want == nil {
				want = map[string]string{}
			}
			if len(got) != len(want) {
				t.Fatalf("errors %v, want %v", got, want)
			}
			for name, kind := range want {
				if got[name] != kind {
					t.Errorf("%s: %q, want %q", name, got[name], kind)
				}
			}
		})
	}
}

// The detail names the types that store the attribute, from the table: for
// fields on an HTTP check, HTTPPARSE among them.
func TestUnstoredErrorNamesTheTypesThatStoreIt(t *testing.T) {
	t.Parallel()
	var diags diag.Diagnostics
	addUnstoredError(&diags, "fields", "HTTP", updatesCheck)
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics", len(diags))
	}
	d := diags[0]
	if d.Summary() != "fields is not stored for HTTP checks" {
		t.Errorf("summary %q", d.Summary())
	}
	if want := "The check types that store fields: HTTPPARSE, MONGODB, MYSQL, PGSQL, PUSH, SNMP."; !strings.Contains(d.Detail(), want) {
		t.Errorf("detail lacks %q:\n%s", want, d.Detail())
	}
	if p := d.(diag.DiagnosticWithPath).Path(); !p.Equal(path.Root("fields")) {
		t.Errorf("path %s, want fields", p)
	}
	for _, part := range []string{"cannot add or change it", "To change fields, change the check's type", "Removing fields from the configuration replaces the check."} {
		if !strings.Contains(d.Detail(), part) {
			t.Errorf("detail lacks %q:\n%s", part, d.Detail())
		}
	}
}

// Removing a value the check holds although its type does not store it
// replaces the check, and the warning says why, what it costs, and how to
// keep the check.
func TestHeldRemovedWarning(t *testing.T) {
	t.Parallel()
	var diags diag.Diagnostics
	addHeldRemovedWarning(context.Background(), priorState(t), &diags, "regex", "HTTP")
	if len(diags) != 1 {
		t.Fatalf("got %d diagnostics", len(diags))
	}
	w := diags[0]
	if w.Severity() != diag.SeverityWarning || w.Summary() != "Removing regex replaces the check" {
		t.Errorf("%v %q", w.Severity(), w.Summary())
	}
	if p := w.(diag.DiagnosticWithPath).Path(); !p.Equal(path.Root("regex")) {
		t.Errorf("path %s, want regex", p)
	}
	for _, part := range []string{
		`check "variables.json" (CHECK-1) removes regex`,
		"NodePing does not store regex on HTTP checks",
		"create a new one with a new ID",
		"without the old check's history",
		"put regex back in the configuration as it is",
		"The check types that store regex: HTTPCONTENT, HTTPADV.",
	} {
		if !strings.Contains(w.Detail(), part) {
			t.Errorf("detail lacks %q:\n%s", part, w.Detail())
		}
	}
}
