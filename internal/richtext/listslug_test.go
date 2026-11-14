package richtext

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// List slugs (0MEu ListRecord with QFv5 getListSlugAttributes) and file
// channels (lH2I renderFileChannelSlug). Synthetic fixtures only, in the
// shape of a rich_text list_record element (keys only).

type listRes struct {
	canvasRes
	recs  map[string]lrName
	files map[string]FileChannel
}

type lrName struct {
	raw   json.RawMessage
	state string
}

func (r *listRes) ListRecordName(f, rec string) (json.RawMessage, string, bool) {
	n, ok := r.recs[f+"_"+rec]
	return n.raw, n.state, ok
}

func (r *listRes) FileChannel(id string) (FileChannel, bool) {
	fc, ok := r.files[id]
	return fc, ok
}

func lrMessage(el string) *slack.Message {
	return &slack.Message{Text: "x", Blocks: json.RawMessage(`[{"type":"rich_text","block_id":"b1","elements":[{"type":"rich_text_section","elements":[` + el + `]}]}]`)}
}

func TestListSlugVariants(t *testing.T) {
	name := json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Write the launch announcement for Tuesday"}]}]}]`)
	r := &listRes{canvasRes: canvasRes{refs: map[string]CanvasRef{
		"F0LIST01": {Title: "Launch tracker :rocket:", Status: "normal", Kind: "list", Views: map[string]string{"View1": "Board"}},
		"F0GONE01": {Status: "deleted"},
		"F0PRIV01": {Status: "hidden"},
	}}, recs: map[string]lrName{
		"F0LIST01_Rec1":  {raw: name, state: "base"},
		"F0LIST01_Rec0":  {state: "base"},
		"F0LIST01_RecX":  {state: "deleted"},
		"F0LIST01_RecNE": {state: "missing"},
	}}
	cases := []struct {
		el, text, icon string
		off            bool
	}{
		{`{"type":"list_record","file_id":"F0LIST01"}`, "Launch tracker :rocket:", "lists", false},
		{`{"type":"list_record","file_id":"F0LIST01","record_id":"Rec1"}`, "Write the launch announce…", "list-item", false},
		{`{"type":"list_record","file_id":"F0LIST01","record_id":"Rec0"}`, "Untitled item", "list-item", false},
		{`{"type":"list_record","file_id":"F0LIST01","record_id":"RecX"}`, "Deleted item", "trash", true},
		{`{"type":"list_record","file_id":"F0LIST01","record_id":"RecNE"}`, "Item not found", "list-item", false},
		{`{"type":"list_record","file_id":"F0LIST01","view_id":"View1"}`, "Launch tracker :rocket:: Board", "lists", false},
		{`{"type":"list_record","file_id":"F0LIST01","view_id":"ViewGone"}`, "Deleted list view", "trash", true},
		{`{"type":"list_record","file_id":"F0GONE01","record_id":"Rec1"}`, "Deleted list", "trash", true},
		{`{"type":"list_record","file_id":"F0PRIV01"}`, "Private list", "lock", false},
	}
	for _, c := range cases {
		sp := slugOf(t, FromMessage(r, lrMessage(c.el)))
		if sp.Text != c.text || sp.Icon != c.icon || sp.Off != c.off {
			t.Errorf("%s: %q %q off %v, want %q %q %v", c.el, sp.Text, sp.Icon, sp.Off, c.text, c.icon, c.off)
		}
	}
	sp := slugOf(t, FromMessage(r, lrMessage(`{"type":"list_record","file_id":"F0LIST01","record_id":"Rec1"}`)))
	if sp.URL != "https://app.slack.com/lists/-/F0LIST01?record_id=Rec1" || sp.ID != "F0LIST01" {
		t.Errorf("link %q id %q", sp.URL, sp.ID)
	}
	// a list the resolver doesn't know yet (`if (!p) return null`) or a
	// record it doesn't (not_loaded: the shimmer): nothing drawn
	for _, el := range []string{`{"type":"list_record","file_id":"F0LIST01","record_id":"RecUnknown"}`, `{"type":"list_record","file_id":"F0NEW001"}`} {
		for _, b := range FromMessage(r, lrMessage(el)).Blocks {
			for _, s := range b.Spans {
				if s.Kind == Canvas {
					t.Errorf("%s drew %+v", el, s)
				}
			}
		}
	}
	if len(r.missed) == 0 || r.missed[len(r.missed)-1] != "fF0NEW001" {
		t.Errorf("unknown list not asked for: %v", r.missed)
	}
}

// renderFileChannelSlug: a file channel's <#C…> and its rich_text channel
// element draw as the canvas or list slug; other channels stay mentions.
func TestFileChannelSlugs(t *testing.T) {
	r := &listRes{canvasRes: canvasRes{refs: map[string]CanvasRef{
		"F0CANV01": {Title: "Plans", Status: "normal", Kind: "canvas"},
		"F0LIST01": {Title: "Launch tracker", Status: "normal", Kind: "list"},
	}}, files: map[string]FileChannel{"C0CANV01": {Kind: "canvas", FileID: "F0CANV01"}, "C0LIST01": {Kind: "list", FileID: "F0LIST01"}}}
	m := &slack.Message{Text: "see <#C0CANV01> and <#C0LIST01|tracker> and <#C1>"}
	var got []string
	for _, b := range FromMessage(r, m).Blocks {
		for _, s := range b.Spans {
			switch s.Kind {
			case Canvas:
				got = append(got, s.Icon+":"+s.Text)
			case Channel:
				got = append(got, "#:"+s.Text)
			}
		}
	}
	if want := []string{"canvas-content:Plans", "lists:Launch tracker", "#:#general"}; len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("mrkdwn: %q", got)
	}
	rt := lrMessage(`{"type":"channel","channel_id":"C0LIST01"}`)
	if sp := slugOf(t, FromMessage(r, rt)); sp.Icon != "lists" || sp.Text != "Launch tracker" {
		t.Errorf("rich_text channel element: %+v", sp)
	}
}
