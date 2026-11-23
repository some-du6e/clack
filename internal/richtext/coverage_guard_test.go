package richtext

import (
	"encoding/json"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/chart"
	"github.com/some-du6e/clack/internal/slack"
)

// The coverage guard: every type Slack's current web client can render in
// a message (its SUI renderables), every block and element type slack.ts
// declares (Karim's checklist), every data_table cell type and chart type
// Slack's components draw, has a renderer in Clack, or a written reason
// here why it needn't. testdata/slack_block_types.json is
// tools/bkregistry.py's output for one client build: run it over a newer
// boot-shared chunk and this test names each type the new build added.

type slackTypes struct {
	Build          string   `json:"build"`
	Chunk          string   `json:"chunk"`
	Renderables    []string `json:"renderables"`
	DataTableCells []string `json:"data_table_cells"`
	ChartTypes     []string `json:"chart_types"`
	SlackTS        struct {
		Blocks   []string `json:"blocks"`
		Elements []string `json:"elements"`
	} `json:"slack_ts"`
}

func loadSlackTypes(t *testing.T) slackTypes {
	t.Helper()
	b, err := os.ReadFile("testdata/slack_block_types.json")
	if err != nil {
		t.Fatal(err)
	}
	var st slackTypes
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	if st.Build == "" || len(st.Renderables) < 100 {
		t.Fatalf("testdata/slack_block_types.json: build %q, %d renderables", st.Build, len(st.Renderables))
	}
	return st
}

// notDrawn are the types Clack draws no renderer for, each with why:
// Slack has none either, or the part of this work that will draw it
// ("pending"). An entry Clack draws now fails the test, so each part
// takes its types off as it lands.
var notDrawn = map[string]string{
	// Slack registers no renderable for these: its surfaces handle them
	// before or instead of a lookup
	"condition":       "no renderable in Slack: useExpandedConditionBlocks (CRur) replaces it with its first matching case's blocks before any lookup (richtext.ExpandConditions)",
	"fallback_canary": "no renderable in Slack: resolveFallback always draws its fallback (Clack's fallback chain, fallback.go)",
}

// pendingParts are the types Slack draws that later parts of the
// data-blocks work will draw, by part (docs/BLOCKKIT-COVERAGE.md).
var pendingParts = map[string][]string{
	"part 2b (sideways scrolling: Slack's LSo8 scroller and its arrows)": {"carousel"},
	"part 2d (rich text's inline types)": {"_canvas_mention", "_theme_install_button", "attachment_mention", "canvas_message_unfurl",
		"canvas_user_mention", "citation", "folder_mention", "salesforce_data_field", "skill", "tag", "unknown", "variable",
		"work_object_mention", "workflowtoken"},
	"part 2e (interactive elements)": {"feedback_buttons", "file_input", "password", "split_button"},
	"part 2f (the remaining blocks)": {"attachment_reference", "auth_provider", "canvas_mermaid", "canvas_table", "channel_section",
		"channel_set", "layout", "mcp_app", "message", "page", "prototype", "related_automation_decorator", "salesforce_record_card",
		"search_top_results", "share_skill", "share_workflow"},
	"part 2a (Karim's named blocks, in progress)": {"contact_card", "event", "share_prompt", "timeline"},
}

func init() {
	for part, ts := range pendingParts {
		for _, t := range ts {
			notDrawn[t] = "pending: " + part
		}
	}
}

// elementTypes are the renderables that are interactive elements (they
// sit in an actions block, a section's accessory, an input or a cell).
var elementTypes = map[string]bool{}

// richTextPieces are rich_text's own sub-blocks.
var richTextPieces = map[string]bool{
	"rich_text_section": true, "rich_text_quote": true, "rich_text_preformatted": true,
	"rich_text_list": true, "rich_text_header": true, "rich_text_divider": true,
}

func init() {
	for _, e := range strings.Fields(`button channels_select checkboxes conversations_select datepicker datetimepicker
		email_text_input external_select feedback_buttons file_input icon_button multi_channels_select
		multi_conversations_select multi_external_select multi_static_select multi_users_select number_input
		overflow password plain_text_input radio_buttons rich_text_input split_button static_select timepicker
		url_text_input users_select workflow_button`) {
		elementTypes[e] = true
	}
}

// guardSamples are a minimal payload for a type whose renderer reads a
// field before it draws anything ({"type": T} otherwise).
var guardSamples = map[string]string{
	"section":            `{"type":"section","text":{"type":"mrkdwn","text":"x"}}`,
	"header":             `{"type":"header","text":{"type":"plain_text","text":"x"}}`,
	"context":            `{"type":"context","elements":[{"type":"mrkdwn","text":"x"}]}`,
	"mrkdwn":             `{"type":"mrkdwn","text":"x"}`,
	"plain_text":         `{"type":"plain_text","text":"x"}`,
	"image":              `{"type":"image","image_url":"https://img.test/a.png","alt_text":"a","image_width":10,"image_height":10}`,
	"text":               `{"type":"text","text":"x"}`,
	"link":               `{"type":"link","url":"https://example.com","text":"x"}`,
	"emoji":              `{"type":"emoji","name":"smile"}`,
	"user":               `{"type":"user","user_id":"U1"}`,
	"channel":            `{"type":"channel","channel_id":"C1"}`,
	"usergroup":          `{"type":"usergroup","usergroup_id":"S1"}`,
	"color":              `{"type":"color","value":"#ff0000"}`,
	"date":               `{"type":"date","timestamp":1700000000,"format":"{date_short}"}`,
	"broadcast":          `{"type":"broadcast","range":"here"}`,
	"data_table":         `{"type":"data_table","rows":[[{"type":"raw_text","text":"a"}],[{"type":"raw_text","text":"b"}]]}`,
	"data_visualization": `{"type":"data_visualization","title":"t","chart":{"type":"pie","segments":[{"label":"a","value":1}]}}`,
	"table":              `{"type":"table","rows":[[{"type":"raw_text","text":"a"}]]}`,
	"message_embed":      `{"type":"message_embed","channel":"C1","ts":"1.0"}`,
}

// mustDraw are the block types whose builder may return nothing for a
// sample it can't use: drawn holds them to a block of their kind.
var mustDraw = map[string]BlockKind{
	"data_table":         DataTableBlock,
	"data_visualization": DataVizBlock,
	"message_embed":      AttachmentBlock,
}

// guardRes is the guard's resolver: the test resolver, holding the
// message message_embed's sample embeds.
var guardRes = embedRes{msgs: map[string]slack.Message{"C1/1.0": {TS: "1.0", Channel: "C1", User: "U1", Text: "embedded"}}}

func guardSample(typ string) string {
	if s, ok := guardSamples[typ]; ok {
		return s
	}
	return `{"type":"` + typ + `"}`
}

// drawn reports whether Clack draws typ: a block or a text object through
// its own case in the builder (no undrawn mark, no UnsupportedBlock
// Fallback card), an element as a Button, a rich_text piece or an inline
// element inside a rich_text.
func drawn(t *testing.T, typ string) bool {
	t.Helper()
	c := &builder{r: guardRes}
	switch {
	case elementTypes[typ]:
		var e bk
		if err := json.Unmarshal([]byte(guardSample(typ)), &e); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		_, ok := c.element(&e, "b")
		return ok
	case richTextPieces[typ]:
		var b bk
		if err := json.Unmarshal([]byte(`{"type":"rich_text","elements":[`+guardSample(typ)+`]}`), &b); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		c.block(nil, &b)
	case inlineTypes[typ] || strings.HasPrefix(typ, "_") || typ == "unknown":
		var b bk
		if err := json.Unmarshal([]byte(`{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[`+guardSample(typ)+`]}]}`), &b); err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		c.block(nil, &b)
	default:
		// as a message's blocks decode (a data block by its own keys)
		b := decodeBlock(json.RawMessage(guardSample(typ)))
		if b.Type == badBlock {
			t.Fatalf("%s: its sample doesn't decode", typ)
		}
		out := c.block(nil, &b)
		for _, o := range out {
			if o.Kind == UnknownBlock || o.Kind == Unsupported {
				return false
			}
		}
		if k, ok := mustDraw[typ]; ok && !slices.ContainsFunc(out, func(o Block) bool { return o.Kind == k && !drawsError(&o) }) {
			return false
		}
	}
	return !slices.Contains(c.undrawn, typ)
}

func TestSlackBlockTypesHaveRenderers(t *testing.T) {
	st := loadSlackTypes(t)
	// fallback.go's list is Slack's: a type it lacks takes the fallback
	// chain even though Slack draws it
	var lacks, stale []string
	have := map[string]bool{}
	for _, typ := range st.Renderables {
		have[typ] = true
		if !messageRenderables[typ] {
			lacks = append(lacks, typ)
		}
	}
	for typ := range messageRenderables {
		if !have[typ] {
			stale = append(stale, typ)
		}
	}
	sort.Strings(stale)
	if len(lacks) > 0 || len(stale) > 0 {
		t.Errorf("build %s: fallback.go's messageRenderables lacks %q and has %q Slack no longer registers", st.Build, lacks, stale)
	}

	all := map[string]string{} // type → where Slack names it
	for _, typ := range st.Renderables {
		all[typ] = "renderable"
	}
	for _, typ := range st.SlackTS.Blocks {
		if _, ok := all[typ]; !ok {
			all[typ] = "slack.ts block"
		}
	}
	for _, typ := range st.SlackTS.Elements {
		if _, ok := all[typ]; !ok {
			all[typ] = "slack.ts element"
		}
	}
	var missing []string
	for typ, from := range all {
		if reason, ok := notDrawn[typ]; ok {
			if drawn(t, typ) {
				t.Errorf("%s is drawn now: drop it from notDrawn (%q)", typ, reason)
			}
			continue
		}
		if !drawn(t, typ) {
			missing = append(missing, typ+" ("+from+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("build %s (%s): Slack draws %d types Clack has no renderer for; draw each, or give notDrawn the reason it needn't:\n\t%s",
			st.Build, st.Chunk, len(missing), strings.Join(missing, "\n\t"))
	}
	pending := 0
	for typ, why := range notDrawn {
		if _, ok := all[typ]; !ok {
			t.Errorf("notDrawn has %s, which Slack no longer names", typ)
		}
		if strings.HasPrefix(why, "pending: ") {
			pending++
		}
	}
	t.Logf("build %s: %d types, %d drawn by Clack, %d without a renderable in Slack, %d pending", st.Build, len(all), len(all)-len(notDrawn), len(notDrawn)-pending, pending)

	// data_table's cells (renderCellContent) and data_visualization's
	// charts (isSupportedChart)
	for _, typ := range st.DataTableCells {
		d := blocksMsg(t, `[{"type":"data_table","rows":[[{"type":"raw_text","text":"h"}],[`+dtCellSample(typ)+`]]}]`)
		if dt := d.Blocks[0].DataTable; dt == nil || dt.Err || dt.Body[0][0].Kind == DTNone {
			t.Errorf("data_table cell %s: no renderer", typ)
		}
	}
	for _, typ := range st.ChartTypes {
		js := `{"type":"` + typ + `","series":[],"axis_config":{"categories":["a"]}}`
		if typ == "pie" {
			js = `{"type":"pie","segments":[{"label":"a","value":1}]}`
		}
		if d := chart.ParseChart(json.RawMessage(js)); !d.Supported || d.Err != nil {
			t.Errorf("chart %s: no renderer (supported %v, err %v)", typ, d.Supported, d.Err)
		}
	}
}

func dtCellSample(typ string) string {
	switch typ {
	case "raw_text", "cell_text":
		return `{"type":"` + typ + `","text":"x"}`
	case "raw_number":
		return `{"type":"raw_number","value":1,"text":"1"}`
	case "rich_text":
		return `{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"x"}]}]}`
	case "action_cell":
		return `{"type":"action_cell","element":{"type":"button","action_id":"a","text":{"type":"plain_text","text":"Go"}}}`
	}
	return `{"type":"` + typ + `"}`
}

// drawsError reports a data block that draws its error state.
func drawsError(b *Block) bool {
	return b.DataViz != nil && b.DataViz.Err || b.DataTable != nil && b.DataTable.Err
}
