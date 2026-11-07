package provider

import (
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type panelModel struct {
	Title      types.String         `tfsdk:"title"`
	Action     types.String         `tfsdk:"action"`
	Parameters jsontypes.Normalized `tfsdk:"parameters"`
}

var panelsShape = listShape[QueryPanel, panelModel]{
	name:        "query_panels",
	singular:    "panel",
	description: "A stream's query panels, in order, replaced whole. A panel is a pointer to an action by name with fixed parameters, never a pre-authorization.",
	block: schema.ListNestedBlock{
		Description: "One panel; at most 8 per stream.",
		NestedObject: schema.NestedBlockObject{
			Attributes: map[string]schema.Attribute{
				"title":      schema.StringAttribute{Required: true},
				"action":     schema.StringAttribute{Required: true, Description: "A declared action's name. Its existence is not checked."},
				"parameters": jsonAttribute("Fixed parameters, at most 8 KB; never dt, which datagecko fills."),
			},
		},
	},
	get:     (*Client).GetQueryPanels,
	replace: (*Client).ReplaceQueryPanels,
	toWire: func(models []panelModel) []QueryPanel {
		panels := make([]QueryPanel, len(models))
		for i, p := range models {
			panels[i] = QueryPanel{Title: p.Title.ValueString(), Action: p.Action.ValueString(), Parameters: rawJSON(p.Parameters)}
		}
		return panels
	},
	toModel: func(panels []QueryPanel) []panelModel {
		models := make([]panelModel, len(panels))
		for i, p := range panels {
			models[i] = panelModel{
				Title:      types.StringValue(p.Title),
				Action:     types.StringValue(p.Action),
				Parameters: normalized(p.Parameters),
			}
		}
		return models
	},
}

func NewStreamQueryPanelsResource() resource.Resource {
	return newListResource(panelsShape)
}
