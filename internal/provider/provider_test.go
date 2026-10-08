package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
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
