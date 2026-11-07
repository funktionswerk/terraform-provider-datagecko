package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type chartModel struct {
	Title   types.String      `tfsdk:"title"`
	Series  []chartEntryModel `tfsdk:"series"`
	Markers []markerModel     `tfsdk:"marker"`
}

type chartEntryModel struct {
	ID          types.String  `tfsdk:"id"`
	Color       types.String  `tfsdk:"color"`
	Width       types.Float64 `tfsdk:"width"`
	Dash        types.String  `tfsdk:"dash"`
	Fill        types.Bool    `tfsdk:"fill"`
	PointRadius types.Float64 `tfsdk:"point_radius"`
	PointStyle  types.String  `tfsdk:"point_style"`
}

type markerModel struct {
	ID    types.String `tfsdk:"id"`
	Color types.String `tfsdk:"color"`
}

// Style fields carry no default and no validator: the API reads back only
// what was set, so absent ⇔ null is the clean round-trip, and its bounds and
// vocabularies are surfaced verbatim rather than copied.
var chartsShape = listShape[Chart, chartModel]{
	name:        "charts",
	singular:    "chart",
	description: "A stream's declared charts, in order, replaced whole; the first is the hero. Destroying it returns the stream to its implicit chart.",
	block: schema.ListNestedBlock{
		Description: "One chart; at most 20 per stream.",
		NestedObject: schema.NestedBlockObject{
			Attributes: map[string]schema.Attribute{
				"title": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			},
			Blocks: map[string]schema.Block{
				"series": schema.ListNestedBlock{
					Description: "A measure series drawn on the chart, in order.",
					NestedObject: schema.NestedBlockObject{
						Attributes: map[string]schema.Attribute{
							"id":           schema.StringAttribute{Required: true, Description: "A series of this stream."},
							"color":        schema.StringAttribute{Optional: true, Description: "#rrggbb."},
							"width":        schema.Float64Attribute{Optional: true, Description: "0-8."},
							"dash":         schema.StringAttribute{Optional: true, Description: "solid, dashed or dotted."},
							"fill":         schema.BoolAttribute{Optional: true},
							"point_radius": schema.Float64Attribute{Optional: true, Description: "0-8."},
							"point_style":  schema.StringAttribute{Optional: true, Description: "circle, rect, triangle, cross or star."},
						},
					},
				},
				"marker": schema.ListNestedBlock{
					Description: "A marker series shown on the chart.",
					NestedObject: schema.NestedBlockObject{
						Attributes: map[string]schema.Attribute{
							"id":    schema.StringAttribute{Required: true},
							"color": schema.StringAttribute{Optional: true, Description: "#rrggbb."},
						},
					},
				},
			},
		},
	},
	get:     (*Client).GetCharts,
	replace: (*Client).ReplaceCharts,
	toWire:  toChartInputs,
	toModel: toChartModels,
}

func NewStreamChartsResource() resource.Resource {
	return newListResource(chartsShape)
}

func toChartInputs(models []chartModel) []Chart {
	charts := make([]Chart, len(models))
	for i, m := range models {
		chart := Chart{Title: m.Title.ValueString(), Series: []ChartEntry{}, Markers: []ChartEntry{}}
		for _, e := range m.Series {
			chart.Series = append(chart.Series, ChartEntry{
				ID:          e.ID.ValueString(),
				Color:       e.Color.ValueStringPointer(),
				Width:       e.Width.ValueFloat64Pointer(),
				Dash:        e.Dash.ValueStringPointer(),
				Fill:        e.Fill.ValueBoolPointer(),
				PointRadius: e.PointRadius.ValueFloat64Pointer(),
				PointStyle:  e.PointStyle.ValueStringPointer(),
			})
		}
		for _, m := range m.Markers {
			chart.Markers = append(chart.Markers, ChartEntry{ID: m.ID.ValueString(), Color: m.Color.ValueStringPointer()})
		}
		charts[i] = chart
	}
	return charts
}

func toChartModels(charts []Chart) []chartModel {
	models := make([]chartModel, len(charts))
	for i, c := range charts {
		model := chartModel{Title: types.StringValue(c.Title), Series: []chartEntryModel{}, Markers: []markerModel{}}
		for _, e := range c.Series {
			model.Series = append(model.Series, chartEntryModel{
				ID:          types.StringValue(e.ID),
				Color:       types.StringPointerValue(e.Color),
				Width:       types.Float64PointerValue(e.Width),
				Dash:        types.StringPointerValue(e.Dash),
				Fill:        types.BoolPointerValue(e.Fill),
				PointRadius: types.Float64PointerValue(e.PointRadius),
				PointStyle:  types.StringPointerValue(e.PointStyle),
			})
		}
		for _, m := range c.Markers {
			model.Markers = append(model.Markers, markerModel{ID: types.StringValue(m.ID), Color: types.StringPointerValue(m.Color)})
		}
		models[i] = model
	}
	return models
}
