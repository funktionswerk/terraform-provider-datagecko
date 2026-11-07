package provider

import (
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
)

// normalized wraps the API's bytes as a JSON attribute value. The framework
// compares it with the prior value by meaning, so the API's key order (jsonb
// sorts them) never shows as a diff. Absent bytes read as an empty object.
func normalized(raw json.RawMessage) jsontypes.Normalized {
	if len(raw) == 0 || string(raw) == "null" {
		return jsontypes.NewNormalizedValue("{}")
	}
	return jsontypes.NewNormalizedValue(string(raw))
}

// rawJSON is the attribute's bytes exactly as written; null or unknown is nil,
// which a request body then leaves out.
func rawJSON(v jsontypes.Normalized) json.RawMessage {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	return json.RawMessage(v.ValueString())
}

// jsonAttribute is an optional JSON object attribute, {} when unset.
func jsonAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{
		CustomType:  jsontypes.NormalizedType{},
		Optional:    true,
		Computed:    true,
		Default:     stringdefault.StaticString("{}"),
		Description: description + " A JSON object; write it with jsonencode().",
	}
}

// computedJSONAttribute is a read-only JSON attribute. It carries no plan
// modifier: its value moves whenever the inputs it is derived from do.
func computedJSONAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{
		CustomType:  jsontypes.NormalizedType{},
		Computed:    true,
		Description: description,
	}
}
