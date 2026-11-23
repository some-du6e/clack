package fakeslack

import (
	"strconv"
	"strings"
)

// The data_visualization blocks (Slack's DataVisualizationBlock): every
// chart type, Slack's four examples from its Block Kit reference
// (docs.slack.dev/reference/block-kit/blocks/data-visualization-block),
// a message shaped like the one this was written for (a bar chart between
// a section, an actions button and a context), and what Slack does with
// data it can't draw. None of them animates (gallery fixtures idle).

func init() { RegisterGallery("14-dataviz", galleryDataViz) }

// GalleryDataVizStartup is the startup chart's block_id.
const GalleryDataVizStartup = "viz-bar-single"

func galleryDataViz(e GalleryEnv) []GalleryMessage {
	viz := func(block string) string {
		return strings.ReplaceAll(block, "{PREVIEW}", e.Img(1200, 720, "dataviz-preview"))
	}
	return []GalleryMessage{
		{Name: "dataviz/startup", Text: "startup loading time", Blocks: viz(`[
			{"type":"section","text":{"type":"mrkdwn","text":"*Startup*: time from launch to the first window, warm."}},
			{"type":"data_visualization","block_id":"viz-bar-single","title":"startup loading time","chart":{"type":"bar","series":[{"name":"time to window (ms)","data":[{"label":"Slack","value":1059},{"label":"Slick","value":1816},{"label":"Taut","value":2579},{"label":"Clack","value":212}]}],"axis_config":{"categories":["Slack","Slick","Taut","Clack"],"x_label":"client","y_label":"time to window (ms)"}},
				"preview_images":{"preview_image_light_mode":"{PREVIEW}","preview_image_dark_mode":"{PREVIEW}"}},
			{"type":"actions","elements":[{"type":"button","action_id":"viz_rerun","text":{"type":"plain_text","text":"Run again"},"value":"rerun"}]},
			{"type":"context","elements":[{"type":"mrkdwn","text":"Median of 10 launches"}]}]`)},
		// Slack's examples (the Block Kit reference)
		{Name: "dataviz/pie", Text: "My Favorite Candy Bars", Blocks: `[{"type":"data_visualization","title":"My Favorite Candy Bars","chart":{"type":"pie","segments":[{"label":"Kit Kat","value":45},{"label":"Twix","value":28},{"label":"Crunch","value":18},{"label":"Milky Way","value":9}]}}]`},
		{Name: "dataviz/bar rotated labels", Text: "My Favorite Pies by Percentage of Tastiness", Blocks: `[{"type":"data_visualization","title":"My Favorite Pies by Percentage of Tastiness","chart":{"type":"bar","series":[{"name":"Pies","data":[{"label":"Strawberry Rhubarb","value":85},{"label":"Pumpkin","value":70},{"label":"Lemon Meringue","value":72},{"label":"Blueberry","value":90},{"label":"Key Lime","value":56}]}],"axis_config":{"categories":["Strawberry Rhubarb","Pumpkin","Lemon Meringue","Blueberry","Key Lime"],"x_label":"Pies","y_label":"Percentage of Tastiness"}}}]`},
		{Name: "dataviz/area", Text: "Daily Active Users", Blocks: `[{"type":"data_visualization","title":"Daily Active Users","chart":{"type":"area","series":[{"name":"Pied Piper Free Tier","data":[{"label":"Mon","value":12000},{"label":"Tues","value":13500},{"label":"Wed","value":15200},{"label":"Thurs","value":14800},{"label":"Fri","value":16400}]},{"name":"Pied Piper Paid Tier","data":[{"label":"Mon","value":4500},{"label":"Tues","value":4800},{"label":"Wed","value":5100},{"label":"Thurs","value":5600},{"label":"Fri","value":6200}]}],"axis_config":{"categories":["Mon","Tues","Wed","Thurs","Fri"],"x_label":"Day","y_label":"Users"}}}]`},
		{Name: "dataviz/line", Text: "Weekly Paper Sales", Blocks: `[{"type":"data_visualization","title":"Weekly Paper Sales","chart":{"type":"line","series":[{"name":"Dunder Mifflin Infinity Website","data":[{"label":"Week 1","value":32000},{"label":"Week 2","value":35000},{"label":"Week 3","value":29000},{"label":"Week 4","value":41000},{"label":"Week 5","value":45000}]},{"name":"Dunder Mifflin In-store","data":[{"label":"Week 1","value":32000},{"label":"Week 2","value":35000},{"label":"Week 3","value":29000},{"label":"Week 4","value":41000},{"label":"Week 5","value":45000}]}],"axis_config":{"categories":["Week 1","Week 2","Week 3","Week 4","Week 5"],"x_label":"Week","y_label":"Paper Sales (USD)"}}}]`},
		// more series, gaps, negatives, large values
		{Name: "dataviz/bar three series", Text: "Revenue by region", Blocks: `[{"type":"data_visualization","title":"Revenue by region","chart":{"type":"bar","series":[{"name":"North","data":[{"label":"Q1","value":120},{"label":"Q2","value":135},{"label":"Q3","value":160},{"label":"Q4","value":170}]},{"name":"South","data":[{"label":"Q1","value":90},{"label":"Q2","value":110},{"label":"Q3","value":95},{"label":"Q4","value":130}]},{"name":"West","data":[{"label":"Q1","value":60},{"label":"Q2","value":80},{"label":"Q3","value":110},{"label":"Q4","value":150}]}],"axis_config":{"categories":["Q1","Q2","Q3","Q4"],"x_label":"Quarter","y_label":"Revenue (k$)"}}}]`},
		{Name: "dataviz/line gaps", Text: "Uptime checks", Blocks: `[{"type":"data_visualization","title":"Uptime checks","chart":{"type":"line","series":[{"name":"checks passed","data":[{"label":"Mon","value":5},{"label":"Wed","value":7},{"label":"Thu","value":8},{"label":"Sun","value":3}]}],"axis_config":{"categories":["Mon","Tue","Wed","Thu","Fri","Sat","Sun"],"x_label":"Day","y_label":"Checks"}}}]`},
		{Name: "dataviz/bar negative", Text: "Net change", Blocks: `[{"type":"data_visualization","title":"Net change","chart":{"type":"bar","series":[{"name":"delta","data":[{"label":"Q1","value":-35},{"label":"Q2","value":20},{"label":"Q3","value":-5},{"label":"Q4","value":48}]}],"axis_config":{"categories":["Q1","Q2","Q3","Q4"],"x_label":"Quarter","y_label":"Change"}}}]`},
		{Name: "dataviz/line compact values", Text: "Revenue", Blocks: `[{"type":"data_visualization","title":"Revenue","chart":{"type":"line","series":[{"name":"revenue","data":[{"label":"Jan","value":1250000},{"label":"Feb","value":2400000},{"label":"Mar","value":1800000},{"label":"Apr","value":3100000}]}],"axis_config":{"categories":["Jan","Feb","Mar","Apr"]}}}]`},
		{Name: "dataviz/pie twelve", Text: "Traffic sources", Blocks: `[{"type":"data_visualization","title":"Traffic sources","chart":{"type":"pie","segments":[` + vizPieSegs(12) + `]}}]`},
		// what Slack can't draw: the errorFallback, the title alone, gaps
		{Name: "dataviz/no chart", Text: "no chart", Blocks: `[{"type":"data_visualization","title":"no chart"}]`},
		{Name: "dataviz/duplicate category", Text: "duplicate category", Blocks: `[{"type":"data_visualization","title":"duplicate category","chart":{"type":"bar","series":[{"name":"s","data":[{"label":"a","value":1}]}],"axis_config":{"categories":["a","a"]}}}]`},
		{Name: "dataviz/unknown type", Text: "a scatter chart", Blocks: `[{"type":"data_visualization","title":"a scatter chart","chart":{"type":"scatter","series":[]}}]`},
		{Name: "dataviz/no values", Text: "nothing measured", Blocks: `[{"type":"data_visualization","title":"nothing measured","chart":{"type":"bar","series":[{"name":"s","data":[{"label":"a","value":"n/a"},{"label":"b","value":null}]}],"axis_config":{"categories":["a","b"],"x_label":"x","y_label":"y"}}}]`},
		{Name: "dataviz/too large", Text: "a chart past Slack's limits", Blocks: `[{"type":"data_visualization","title":"A chart past every one of Slack's limits: fourteen series over twenty-five categories","chart":{"type":"line","series":[` + vizLineSeries(14, 25) + `],"axis_config":{"categories":[` + vizCategories(25) + `],"x_label":"category","y_label":"value"}}}]`},
	}
}

func vizPieSegs(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"label":"source ` + strconv.Itoa(i+1) + `","value":` + strconv.Itoa(50-3*i) + `}`)
	}
	return b.String()
}

func vizCategories(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"c` + strconv.Itoa(i+1) + `"`)
	}
	return b.String()
}

func vizLineSeries(ns, n int) string {
	var b strings.Builder
	for s := 0; s < ns; s++ {
		if s > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"name":"series ` + strconv.Itoa(s+1) + `","data":[`)
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`{"label":"c` + strconv.Itoa(i+1) + `","value":` + strconv.Itoa((s*7+i*13)%40+s*3) + `}`)
		}
		b.WriteString(`]}`)
	}
	return b.String()
}
