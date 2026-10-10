package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

func TestProviderSchema(t *testing.T) {
	t.Parallel()

	resp, err := providerserver.NewProtocol6WithError(New("test")())()
	if err != nil {
		t.Fatalf("failed to create provider server: %v", err)
	}

	ctx := context.Background()
	schemaResp, err := resp.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatalf("failed to get provider schema: %v", err)
	}

	if schemaResp.Provider == nil {
		t.Fatal("provider schema is nil")
	}

	requiredAttrs := []string{"api_token", "customer_id", "api_url", "rate_limit", "max_retries", "ignore_mute"}
	for _, attr := range requiredAttrs {
		found := false
		for _, block := range schemaResp.Provider.Block.Attributes {
			if block.Name == attr {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected attribute %q not found in provider schema", attr)
		}
	}
}

// Each setting not known yet fails the configuration, at its own path, rather
// than being read as "" or 0. One with an environment variable names it.
func TestUnknownSettings(t *testing.T) {
	known := NodePingProviderModel{
		APIToken:     types.StringValue("token"),
		CustomerID:   types.StringNull(),
		APIURL:       types.StringNull(),
		RateLimit:    types.Float64Null(),
		MaxRetries:   types.Int64Value(0),
		RetryWaitMin: types.Int64Null(),
		RetryWaitMax: types.Int64Null(),
		DefaultTags:  types.ListValueMust(types.StringType, []attr.Value{types.StringValue("a")}),
		IgnoreMute:   types.BoolNull(),
	}
	if diags := unknownSettings(known); diags.HasError() {
		t.Fatalf("known settings failed: %v", diags)
	}

	tests := []struct {
		name    string
		setting string
		env     string
		unknown func(*NodePingProviderModel)
	}{
		{"api_token", "api_token", "NODEPING_API_TOKEN", func(m *NodePingProviderModel) { m.APIToken = types.StringUnknown() }},
		{"customer_id", "customer_id", "NODEPING_CUSTOMER_ID", func(m *NodePingProviderModel) { m.CustomerID = types.StringUnknown() }},
		{"api_url", "api_url", "NODEPING_API_URL", func(m *NodePingProviderModel) { m.APIURL = types.StringUnknown() }},
		{"rate_limit", "rate_limit", "", func(m *NodePingProviderModel) { m.RateLimit = types.Float64Unknown() }},
		{"max_retries", "max_retries", "", func(m *NodePingProviderModel) { m.MaxRetries = types.Int64Unknown() }},
		{"retry_wait_min", "retry_wait_min", "", func(m *NodePingProviderModel) { m.RetryWaitMin = types.Int64Unknown() }},
		{"retry_wait_max", "retry_wait_max", "", func(m *NodePingProviderModel) { m.RetryWaitMax = types.Int64Unknown() }},
		{"default_tags", "default_tags", "", func(m *NodePingProviderModel) { m.DefaultTags = types.ListUnknown(types.StringType) }},
		{"a tag in default_tags", "default_tags", "", func(m *NodePingProviderModel) {
			m.DefaultTags = types.ListValueMust(types.StringType, []attr.Value{types.StringValue("a"), types.StringUnknown()})
		}},
		{"ignore_mute", "ignore_mute", "", func(m *NodePingProviderModel) { m.IgnoreMute = types.BoolUnknown() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := known
			tt.unknown(&config)

			diags := unknownSettings(config)

			if len(diags) != 1 || !diags.HasError() {
				t.Fatalf("expected one error, got %v", diags)
			}
			withPath, ok := diags[0].(diag.DiagnosticWithPath)
			if !ok || !withPath.Path().Equal(path.Root(tt.setting)) {
				t.Errorf("expected the error at %s, got %v", tt.setting, diags[0])
			}
			if !strings.HasPrefix(diags[0].Detail(), tt.setting+" is known only after apply") {
				t.Errorf("expected the detail to name %s, got %q", tt.setting, diags[0].Detail())
			}
			if strings.Contains(diags[0].Detail(), "environment variable") != (tt.env != "") ||
				!strings.Contains(diags[0].Detail(), tt.env) {
				t.Errorf("expected the detail to name the environment variable %q, if any, got %q", tt.env, diags[0].Detail())
			}
		})
	}
}
