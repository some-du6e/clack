package richtext

import (
	"strconv"
	"strings"
	"testing"
)

// Synthetic fixtures only: Slack's own examples (its Block Kit reference
// for data_visualization), a block shaped like the one this was written
// for, and made-up tables shaped like a bot's leaderboard.

// The startup chart: a bar chart between a section, a button and a
// context, as Karim's message has it.
const startupViz = `{"type":"data_visualization","block_id":"viz-bar-single","title":"startup loading time","chart":{"type":"bar","series":[{"name":"time to window (ms)","data":[{"label":"Slack","value":1059},{"label":"Slick","value":1816},{"label":"Taut","value":2579},{"label":"Clack","value":212}]}],"axis_config":{"categories":["Slack","Slick","Taut","Clack"],"x_label":"client","y_label":"time to window (ms)"}}}`

// slackVizExamples are the Block Kit reference's four examples
// (docs.slack.dev/reference/block-kit/blocks/data-visualization-block).
var slackVizExamples = []string{
	`{"type":"data_visualization","title":"My Favorite Candy Bars","chart":{"type":"pie","segments":[{"label":"Kit Kat","value":45},{"label":"Twix","value":28},{"label":"Crunch","value":18},{"label":"Milky Way","value":9}]}}`,
	`{"type":"data_visualization","title":"My Favorite Pies by Percentage of Tastiness","chart":{"type":"bar","series":[{"name":"Pies","data":[{"label":"Strawberry Rhubarb","value":85},{"label":"Pumpkin","value":70},{"label":"Lemon Meringue","value":72},{"label":"Blueberry","value":90},{"label":"Key Lime","value":56}]}],"axis_config":{"categories":["Strawberry Rhubarb","Pumpkin","Lemon Meringue","Blueberry","Key Lime"],"x_label":"Pies","y_label":"Percentage of Tastiness"}}}`,
	`{"type":"data_visualization","title":"Daily Active Users","chart":{"type":"area","series":[{"name":"Pied Piper Free Tier","data":[{"label":"Mon","value":12000},{"label":"Tues","value":13500},{"label":"Wed","value":15200},{"label":"Thurs","value":14800},{"label":"Fri","value":16400}]},{"name":"Pied Piper Paid Tier","data":[{"label":"Mon","value":4500},{"label":"Tues","value":4800},{"label":"Wed","value":5100},{"label":"Thurs","value":5600},{"label":"Fri","value":6200}]}],"axis_config":{"categories":["Mon","Tues","Wed","Thurs","Fri"],"x_label":"Day","y_label":"Users"}}}`,
	`{"type":"data_visualization","title":"Weekly Paper Sales","chart":{"type":"line","series":[{"name":"Dunder Mifflin Infinity Website","data":[{"label":"Week 1","value":32000},{"label":"Week 2","value":35000},{"label":"Week 3","value":29000},{"label":"Week 4","value":41000},{"label":"Week 5","value":45000}]},{"name":"Dunder Mifflin In-store","data":[{"label":"Week 1","value":32000},{"label":"Week 2","value":35000},{"label":"Week 3","value":29000},{"label":"Week 4","value":41000},{"label":"Week 5","value":45000}]}],"axis_config":{"categories":["Week 1","Week 2","Week 3","Week 4","Week 5"],"x_label":"Week","y_label":"Paper Sales (USD)"}}}`,
}

func TestDataVizDecodesSlacksExamples(t *testing.T) {
	want := []struct {
		title, typ string
		series     int
	}{
		{"My Favorite Candy Bars", "pie", 0},
		{"My Favorite Pies by Percentage of Tastiness", "bar", 1},
		{"Daily Active Users", "area", 2},
		{"Weekly Paper Sales", "line", 2},
		{"startup loading time", "bar", 1},
	}
	for i, js := range append(append([]string{}, slackVizExamples...), startupViz) {
		d := blocksMsg(t, `[`+js+`]`)
		if len(d.Blocks) != 1 || d.Blocks[0].Kind != DataVizBlock || d.Blocks[0].DataViz == nil {
			t.Fatalf("%d: blocks %+v", i, d.Blocks)
		}
		dv := d.Blocks[0].DataViz
		w := want[i]
		if dv.Err || !dv.Chart.Supported || dv.Title != w.title || dv.Chart.Type != w.typ || len(dv.Chart.Series) != w.series {
			t.Errorf("%d: err %v supported %v title %q type %q series %d, want %+v", i, dv.Err, dv.Chart.Supported, dv.Title, dv.Chart.Type, len(dv.Chart.Series), w)
		}
		if dv.AriaLabel == "" || !strings.HasPrefix(dv.AriaLabel, strings.ToUpper(w.typ[:1])+w.typ[1:]+" chart: "+w.title) {
			t.Errorf("%d: aria-label %q", i, dv.AriaLabel)
		}
	}
	d := blocksMsg(t, `[`+startupViz+`]`)
	if dv := d.Blocks[0].DataViz; dv.BlockID != "viz-bar-single" || dv.Chart.XLabel != "client" || dv.Chart.YLabel != "time to window (ms)" ||
		strings.Join(dv.Chart.Categories, ",") != "Slack,Slick,Taut,Clack" {
		t.Errorf("startup chart %+v", dv)
	}
}

// A mistyped field never fails the message: the other blocks draw, and
// the chart is what Slack's render makes of it.
func TestDataVizMistypedFields(t *testing.T) {
	for _, c := range []struct {
		name, block string
		err         bool
		title       string
	}{
		{"a number title draws its digits", `{"type":"data_visualization","title":7,"chart":{"type":"pie","segments":[]}}`, false, "7"},
		{"a null title draws nothing", `{"type":"data_visualization","title":null,"chart":{"type":"pie","segments":[]}}`, false, ""},
		{"an object title throws", `{"type":"data_visualization","title":{"text":"x"},"chart":{"type":"pie","segments":[]}}`, true, ""},
		{"a number block_id", `{"type":"data_visualization","block_id":5,"title":"t","chart":{"type":"pie","segments":[]}}`, false, "t"},
		{"an odd size and unknown keys", `{"type":"data_visualization","title":"t","size":{"w":1},"extra":[1,2],"chart":{"type":"pie","segments":[]}}`, false, "t"},
		{"no chart throws", `{"type":"data_visualization","title":"t"}`, true, "t"},
		{"an unknown chart type draws the title alone", `{"type":"data_visualization","title":"t","chart":{"type":"scatter"}}`, false, "t"},
	} {
		d := blocksMsg(t, `[{"type":"section","text":{"type":"mrkdwn","text":"before"}},`+c.block+`,{"type":"section","text":{"type":"mrkdwn","text":"after"}}]`)
		if len(d.Blocks) != 3 || d.Blocks[1].Kind != DataVizBlock {
			t.Errorf("%s: blocks %+v", c.name, d.Blocks)
			continue
		}
		dv := d.Blocks[1].DataViz
		if dv.Err != c.err || (!c.err && dv.Title != c.title) {
			t.Errorf("%s: err %v title %q, want err %v title %q", c.name, dv.Err, dv.Title, c.err, c.title)
		}
		if PlainText(Doc{Blocks: d.Blocks[:1]}) != "before" || PlainText(Doc{Blocks: d.Blocks[2:]}) != "after" {
			t.Errorf("%s: the other blocks: %+v", c.name, d.Blocks)
		}
	}
}

// boardTable is a header row and n rows of raw_text cells, the shape of a
// bot's leaderboard.
func boardTable(n int, extra string) string {
	var b strings.Builder
	b.WriteString(`{"type":"data_table","block_id":"dtlb1","caption":"Full points leaderboard"` + extra + `,"rows":[[{"type":"raw_text","text":"Rank"},{"type":"raw_text","text":"Name"},{"type":"raw_text","text":"Points"}]`)
	for i := 0; i < n; i++ {
		b.WriteString(`,[{"type":"raw_text","text":"` + strconv.Itoa(i+1) + `"},{"type":"raw_text","text":"member ` + strconv.Itoa(i+1) + `"},{"type":"raw_text","text":"` + strconv.Itoa(900-i*8) + `"}]`)
	}
	b.WriteString(`]}`)
	return b.String()
}

func TestDataTableDecode(t *testing.T) {
	d := blocksMsg(t, `[`+boardTable(110, "")+`]`)
	if len(d.Blocks) != 1 || d.Blocks[0].Kind != DataTableBlock {
		t.Fatalf("blocks %+v", d.Blocks)
	}
	dt := d.Blocks[0].DataTable
	if dt.Err || dt.Cols != 3 || len(dt.Body) != 110 || dt.Caption != "Full points leaderboard" || dt.Label != dt.Caption ||
		dt.PageSize != 5 || dt.RowHeader != 0 || dt.Salesforce || dt.BlockID != "dtlb1" {
		t.Fatalf("table: err %v cols %d rows %d caption %q page %v", dt.Err, dt.Cols, len(dt.Body), dt.Caption, dt.PageSize)
	}
	if h := dt.Header; !h[0].HeaderText || h[0].Text != "Rank" || h[2].Text != "Points" {
		t.Errorf("header %+v", h)
	}
	if c := dt.Body[109][1]; c.Kind != DTText || c.Text != "member 110" || c.Sort != "member 110" || c.TSV != "member 110" {
		t.Errorf("last row's name %+v", c)
	}
	if tsv := dt.TSV(); !strings.HasPrefix(tsv, "Rank\tName\tPoints\n1\tmember 1\t900\n") || strings.Count(tsv, "\n") != 110 {
		t.Errorf("TSV starts %q, %d lines", tsv[:min(len(tsv), 60)], strings.Count(tsv, "\n")+1)
	}
}

// Each cell type as renderCellContent, getCellSortValue,
// convertTableBlockToTsv and convertTableCellToPlainText read it.
func TestDataTableCells(t *testing.T) {
	d := blocksMsg(t, `[{"type":"data_table","block_id":"b","page_size":2,"row_header_column_index":1,"is_salesforce":true,"rows":[
		[{"type":"raw_text","text":"a"},{"type":"raw_text","text":"b"},{"type":"raw_text","text":"c"},{"type":"raw_text","text":"d"},null,{"type":"raw_text","text":5}],
		[{"type":"cell_text","text":"plain"},{"type":"raw_number","value":1250,"text":"1,250"},
		 {"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"bold","style":{"bold":true}},{"type":"text","text":" and "},{"type":"link","url":"https://example.com","text":"a link"}]}]},
		 {"type":"action_cell","element":{"type":"button","action_id":"go","text":{"type":"plain_text","text":"Go"},"value":"v"}},
		 null,{"type":"mystery","text":"m"}]]}]`)
	dt := d.Blocks[0].DataTable
	if dt.Err {
		t.Fatal("the render throws")
	}
	if dt.PageSize != 2 || dt.RowHeader != 1 || !dt.Salesforce || !dt.HasActions || dt.Cols != 6 || dt.Label != "" {
		t.Errorf("table %+v", dt)
	}
	// a header cell whose text isn't a string isn't drawn; null is skipped
	if h := dt.Header; h[4].Present || !h[5].Present || h[5].HeaderText {
		t.Errorf("header cells %+v %+v", h[4], h[5])
	}
	r := dt.Body[0]
	if r[0].Kind != DTText || r[0].Text != "plain" || r[0].Plain != "plain" {
		t.Errorf("cell_text %+v", r[0])
	}
	if r[1].Kind != DTNumber || r[1].Text != "1,250" || !r[1].NumberOK || r[1].Number != 1250 || r[1].Sort != "1,250" {
		t.Errorf("raw_number %+v", r[1])
	}
	if r[2].Kind != DTRich || r[2].Sort != "bold and a link" || len(r[2].Spans) == 0 || r[2].Spans[0].Style&Bold == 0 {
		t.Errorf("rich_text %+v", r[2])
	}
	if r[3].Kind != DTAction || r[3].Button == nil || r[3].Button.Text != "Go" || r[3].TSV != "Go" || r[3].Button.BlockID != "b" {
		t.Errorf("action_cell %+v", r[3])
	}
	if r[4].Present || r[5].Kind != DTNone || !r[5].Present || r[5].TSV != "" {
		t.Errorf("null and an unknown type: %+v %+v", r[4], r[5])
	}
	if got, want := dt.TSV(), "a\tb\tc\td\t\t5\nplain\t1,250\tbold and a link\tGo\t\t"; got != want {
		t.Errorf("TSV %q, want %q", got, want)
	}
}

// What throws in DataTableBlock's render draws the errorFallback; the
// message's other blocks still draw. A mistyped field Slack reads without
// throwing is read as Slack reads it.
func TestDataTableErrors(t *testing.T) {
	for _, c := range []struct {
		name, block string
		err         bool
	}{
		{"no rows", `{"type":"data_table"}`, true},
		{"rows an object", `{"type":"data_table","rows":{"a":1}}`, true},
		{"rows a string", `{"type":"data_table","rows":"ab"}`, true},
		{"a row that is null", `{"type":"data_table","rows":[[{"type":"raw_text","text":"a"}],null]}`, true},
		{"a header cell that is a string", `{"type":"data_table","rows":[["Name"],[{"type":"raw_text","text":"a"}]]}`, true},
		{"a caption that is an object", `{"type":"data_table","caption":{"x":1},"rows":[]}`, true},
		{"a raw_text whose text is a number", `{"type":"data_table","rows":[[{"type":"raw_text","text":"n"}],[{"type":"raw_text","text":5}]]}`, true},
		{"a raw_number whose text is an object", `{"type":"data_table","rows":[[{"type":"raw_text","text":"n"}],[{"type":"raw_number","value":1,"text":{}}]]}`, true},
		{"empty rows", `{"type":"data_table","rows":[]}`, false},
		{"a string page_size and odd keys", `{"type":"data_table","caption":7,"page_size":"2","row_header_column_index":"x","is_salesforce":"no","rows":[[{"type":"raw_text","text":"n"}],[{"type":"raw_number","value":"1","text":1}],[{"type":"raw_text","text":null}]]}`, false},
		{"a body cell that is a string", `{"type":"data_table","rows":[[{"type":"raw_text","text":"n"}],["x"]]}`, false},
	} {
		d := blocksMsg(t, `[{"type":"section","text":{"type":"mrkdwn","text":"before"}},`+c.block+`,{"type":"section","text":{"type":"mrkdwn","text":"after"}}]`)
		if len(d.Blocks) != 3 || d.Blocks[1].Kind != DataTableBlock {
			t.Errorf("%s: blocks %+v", c.name, d.Blocks)
			continue
		}
		if dt := d.Blocks[1].DataTable; dt.Err != c.err {
			t.Errorf("%s: err %v, want %v", c.name, dt.Err, c.err)
		}
	}
	d := blocksMsg(t, `[{"type":"data_table","caption":7,"page_size":"2","is_salesforce":"no","rows":[[{"type":"raw_text","text":"n"}],[{"type":"raw_number","value":"1","text":1}]]}]`)
	if dt := d.Blocks[0].DataTable; dt.Caption != "7" || dt.PageSize != 5 || !dt.Salesforce || dt.Body[0][0].Text != "1" || dt.Body[0][0].NumberOK {
		t.Errorf("mistyped fields read as %+v", dt)
	}
}

// A notification is convertTableBlockToPlainText's rows (cells joined by
// commas) and convertDataVisualizationBlockToPlainText's title.
func TestDataBlocksNotificationText(t *testing.T) {
	if got, want := notificationTextOf(t, `{"ts":"1.0","text":"fallback","blocks":[`+boardTable(2, "")+`]}`),
		"Rank, Name, Points\n1, member 1, 900\n2, member 2, 892"; got != want {
		t.Errorf("table: %q, want %q", got, want)
	}
	if got := notificationTextOf(t, `{"ts":"1.0","text":"fallback","blocks":[`+startupViz+`]}`); got != "startup loading time" {
		t.Errorf("chart: %q, want its title", got)
	}
	// a block that throws converts nothing: the message text stands in
	if got := notificationTextOf(t, `{"ts":"1.0","text":"fallback","blocks":[{"type":"data_table","rows":{"a":1}}]}`); got != "fallback" {
		t.Errorf("a broken table: %q, want the message text", got)
	}
}
