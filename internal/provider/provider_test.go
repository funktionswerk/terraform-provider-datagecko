package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func configureWith(t *testing.T, endpoint, adminKey string, env map[string]string) *provider.ConfigureResponse {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}

	p := New("test")()
	schemaResp := &provider.SchemaResponse{}
	p.Schema(context.Background(), provider.SchemaRequest{}, schemaResp)

	values := map[string]tftypes.Value{
		"endpoint":  tftypes.NewValue(tftypes.String, nilIfEmpty(endpoint)),
		"admin_key": tftypes.NewValue(tftypes.String, nilIfEmpty(adminKey)),
	}
	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"endpoint": tftypes.String, "admin_key": tftypes.String,
	}}
	config := tfsdk.Config{
		Schema: schemaResp.Schema,
		Raw:    tftypes.NewValue(objType, values),
	}

	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), provider.ConfigureRequest{Config: config}, resp)
	return resp
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func TestConfigureUsesTheAdminKeyAttributeOverTheEnvVar(t *testing.T) {
	resp := configureWith(t, "http://example.test/api/v1/", "attr-key", map[string]string{"DATAGECKO_ADMIN_KEY": "env-key"})

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	client := resp.ResourceData.(*Client)
	if client.AdminKey != "attr-key" {
		t.Fatalf("expected the attribute's key to win, got %q", client.AdminKey)
	}
}

func TestConfigureFallsBackToTheEnvVar(t *testing.T) {
	resp := configureWith(t, "", "", map[string]string{"DATAGECKO_ADMIN_KEY": "env-key"})

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	client := resp.ResourceData.(*Client)
	if client.AdminKey != "env-key" {
		t.Fatalf("expected the env var's key, got %q", client.AdminKey)
	}
	if client.Endpoint != defaultEndpoint {
		t.Fatalf("expected the default endpoint, got %q", client.Endpoint)
	}
}

func TestConfigureFailsWithoutAnAdminKeyAnywhere(t *testing.T) {
	resp := configureWith(t, "", "", nil)

	if !resp.Diagnostics.HasError() {
		t.Fatal("expected a diagnostic error when no admin key is configured")
	}
}
