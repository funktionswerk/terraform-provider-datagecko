package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type sectionModel struct {
	Title types.String `tfsdk:"title"`
	Body  types.String `tfsdk:"body"`
}

var sectionsShape = listShape[Section, sectionModel]{
	name:        "sections",
	singular:    "section",
	description: "A stream's About tab: its sections, in order, replaced whole. Destroying it empties the tab.",
	block: schema.ListNestedBlock{
		Description: "One section; at most 20 per stream.",
		NestedObject: schema.NestedBlockObject{
			Attributes: map[string]schema.Attribute{
				"title": schema.StringAttribute{Required: true, Description: "At most 128 printable characters."},
				"body": schema.StringAttribute{
					Optional: true, Computed: true, Default: stringdefault.StaticString(""),
					Description: "Markdown, at most 20,000 characters, stored exactly as given: a heredoc's trailing newline is part of it.",
				},
			},
		},
	},
	get:     (*Client).GetSections,
	replace: (*Client).ReplaceSections,
	toWire: func(models []sectionModel) []Section {
		sections := make([]Section, len(models))
		for i, m := range models {
			sections[i] = Section{Title: m.Title.ValueString(), Body: m.Body.ValueString()}
		}
		return sections
	},
	toModel: func(sections []Section) []sectionModel {
		models := make([]sectionModel, len(sections))
		for i, s := range sections {
			models[i] = sectionModel{Title: types.StringValue(s.Title), Body: types.StringValue(s.Body)}
		}
		return models
	},
}

func NewStreamSectionsResource() resource.Resource {
	return newListResource(sectionsShape)
}
