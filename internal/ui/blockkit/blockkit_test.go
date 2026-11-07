package blockkit

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

// Synthetic fixtures only (scraping policy).

const testBlocks = `[
 {"type":"section","block_id":"s1","text":{"type":"mrkdwn","text":"Hi"},"accessory":{"type":"button","action_id":"b1","text":{"type":"plain_text","text":"Click"},"value":"v1","style":"primary"}},
 {"type":"actions","block_id":"a1","elements":[
   {"type":"static_select","action_id":"sel","placeholder":{"type":"plain_text","text":"Pick"},"options":[{"text":{"type":"plain_text","text":"A"},"value":"a"},{"text":{"type":"plain_text","text":"B"},"value":"b","description":{"type":"plain_text","text":"bee"}}]},
   {"type":"overflow","action_id":"ov","options":[{"text":{"type":"plain_text","text":"Edit"},"value":"e"}]},
   {"type":"datepicker","action_id":"dp","initial_date":"2026-11-07"},
   {"type":"checkboxes","action_id":"cb","options":[{"text":{"type":"mrkdwn","text":"*One*"},"value":"1"}]}
 ]},
 {"type":"input","block_id":"i1","label":{"type":"plain_text","text":"Name"},"element":{"type":"plain_text_input","action_id":"t","initial_value":"Ada"}},
 {"type":"input","block_id":"i2","optional":true,"label":{"type":"plain_text","text":"Who"},"element":{"type":"multi_users_select","action_id":"u"}}
]`

func TestParseAndFind(t *testing.T) {
	bs := ParseBlocks(json.RawMessage(testBlocks))
	if len(bs) != 4 || len(bs[1].Elements) != 4 {
		t.Fatalf("parse: %d blocks, %d elements", len(bs), len(bs[1].Elements))
	}
	r, ok := find(bs, "a1", "sel")
	if !ok || r.el.Type != "static_select" || len(r.el.Options) != 2 || r.el.Options[1].Description.Text != "bee" {
		t.Fatalf("find: %+v", r)
	}
	if len(r.el.Options[0].Raw) == 0 {
		t.Error("option raw kept")
	}
	if _, ok := find(bs, "s1", "b1"); !ok {
		t.Error("accessory")
	}
	if _, ok := find(bs, "i1", "t"); !ok {
		t.Error("input element")
	}
}

func TestActionPayloads(t *testing.T) {
	bs := ParseBlocks(json.RawMessage(testBlocks))
	btn, _ := find(bs, "s1", "b1")
	got := string(actionPayload("s1", btn.el, nil))
	want := `{"action_id":"b1","block_id":"s1","style":"primary","text":{"type":"plain_text","text":"Click"},"type":"button","value":"v1"}`
	if got != want {
		t.Errorf("button:\n got %s\nwant %s", got, want)
	}
	sel, _ := find(bs, "a1", "sel")
	v := initialValue(sel.el)
	o := sel.el.Options[1]
	v.SelectedOption = &o
	got = string(actionPayload("a1", sel.el, v))
	if !strings.Contains(got, `"selected_option":{"text":{"type":"plain_text","text":"B"},"value":"b","description":{"type":"plain_text","text":"bee"}}`) ||
		!strings.Contains(got, `"placeholder":{"type":"plain_text","text":"Pick"}`) || !strings.Contains(got, `"type":"static_select"`) {
		t.Errorf("select: %s", got)
	}
	ov, _ := find(bs, "a1", "ov")
	got = string(overflowPayload("a1", ov.el, &ov.el.Options[0]))
	if got != `{"action_id":"ov","block_id":"a1","selected_option":{"text":{"type":"plain_text","text":"Edit"},"value":"e"},"type":"overflow"}` {
		t.Errorf("overflow: %s", got)
	}
	dp, _ := find(bs, "a1", "dp")
	got = string(actionPayload("a1", dp.el, initialValue(dp.el)))
	if !strings.Contains(got, `"selected_date":"2026-11-07"`) || !strings.Contains(got, `"initial_date":"2026-11-07"`) {
		t.Errorf("datepicker: %s", got)
	}
}

func TestValuesJSON(t *testing.T) {
	bs := ParseBlocks(json.RawMessage(testBlocks))
	vs := Values{}
	initValues(vs, bs)
	// message state: only set values, plus checkboxes (Slack always
	// reports them)
	var msg map[string]map[string]map[string]any
	_ = json.Unmarshal(vs.JSON(true), &msg)
	if _, ok := msg["a1"]["sel"]; ok {
		t.Error("unset select in message state")
	}
	if cb, ok := msg["a1"]["cb"]; !ok || cb["type"] != "checkboxes" {
		t.Errorf("checkboxes: %v", msg)
	}
	if msg["a1"]["dp"]["selected_date"] != "2026-11-07" {
		t.Errorf("date: %v", msg["a1"]["dp"])
	}
	// modal state: every input, in view.state.values shape
	var all map[string]map[string]map[string]any
	_ = json.Unmarshal(vs.JSON(false), &all)
	if all["i1"]["t"]["value"] != "Ada" || all["i1"]["t"]["type"] != "plain_text_input" {
		t.Errorf("text: %v", all["i1"])
	}
	if us, ok := all["i2"]["u"]["selected_users"].([]any); !ok || len(us) != 0 {
		t.Errorf("multi users: %v", all["i2"])
	}
	if all["a1"]["sel"]["selected_option"] != nil {
		t.Errorf("empty select: %v", all["a1"]["sel"])
	}
}

func TestValidate(t *testing.T) {
	bs := ParseBlocks(json.RawMessage(`[{"type":"input","block_id":"i","label":{"type":"plain_text","text":"L"},"element":{"type":"plain_text_input","action_id":"t"}},
	 {"type":"input","block_id":"e","optional":true,"label":{"type":"plain_text","text":"E"},"element":{"type":"email_text_input","action_id":"m"}}]`))
	s := &Surface{blocks: bs, vals: Values{}, errs: map[string]string{}, h: nil}
	initValues(s.vals, bs)
	s.vals.get("e", "m").Text = "not-an-email"
	ok := s.validate()
	if ok || s.errs["i"] == "" || s.errs["e"] == "" {
		t.Errorf("validate: ok=%v errs=%v", ok, s.errs)
	}
}

func TestDialogBlocks(t *testing.T) {
	out := dialogBlocks(json.RawMessage(`[{"type":"text","label":"Title","name":"title","value":"x","max_length":20},
		{"type":"textarea","label":"Body","name":"body","optional":true},
		{"type":"select","label":"Kind","name":"kind","value":"b","options":[{"label":"A","value":"a"},{"label":"B","value":"b"}]},
		{"type":"select","label":"Who","name":"who","data_source":"users"}]`))
	bs := ParseBlocks(out)
	if len(bs) != 4 || bs[0].Element.Type != "plain_text_input" || bs[0].Element.initialText() != "x" || bs[0].Element.MaxLength != 20 {
		t.Fatalf("text: %s", out)
	}
	if !bs[1].Element.Multiline || !bs[1].Optional {
		t.Errorf("textarea: %+v", bs[1])
	}
	if bs[2].Element.Type != "static_select" || bs[2].Element.InitialOption == nil || bs[2].Element.InitialOption.Value != "b" {
		t.Errorf("select: %+v", bs[2].Element)
	}
	if bs[3].Element.Type != "users_select" {
		t.Errorf("users: %+v", bs[3].Element)
	}
}

func TestStripMrkdwn(t *testing.T) {
	if got := stripMrkdwn("*bold* _it_ <https://x.y|link> `c`"); got != "bold it link c" {
		t.Errorf("got %q", got)
	}
	if timeLabel("15:30") != "3:30 PM" || timeLabel("00:05") != "12:05 AM" || dateLabel("2026-11-07") != "Nov 7, 2026" {
		t.Error("labels")
	}
}

func TestMergeRichText(t *testing.T) {
	in := `[{"type":"rich_text","block_id":"a","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"hi"}]}]},
	 {"type":"header","text":{"type":"plain_text","text":"Head"}},{"type":"divider"},
	 {"type":"rich_text","block_id":"b","elements":[{"type":"rich_text_list","style":"bullet","elements":[]}]}]`
	out := string(mergeRichText(json.RawMessage(in)))
	if !strings.HasPrefix(out, `{"elements":[{"type":"rich_text_section"`) || !strings.Contains(out, `"text":"Head"`) ||
		!strings.Contains(out, `rich_text_list`) || !strings.HasSuffix(out, `"type":"rich_text"}`) {
		t.Errorf("merge: %s", out)
	}
}

func TestFileInputState(t *testing.T) {
	v := &Value{Type: "file_input"}
	if !v.empty() || string(mustJSON(v.stateJSON())) != `{"files":[],"type":"file_input"}` {
		t.Errorf("empty: %s", mustJSON(v.stateJSON()))
	}
	v.Files = []json.RawMessage{json.RawMessage(`{"id":"F1"}`)}
	if v.empty() || string(mustJSON(v.stateJSON())) != `{"files":[{"id":"F1"}],"type":"file_input"}` {
		t.Errorf("files: %s", mustJSON(v.stateJSON()))
	}
}

func TestSpeedbumpDirty(t *testing.T) {
	bs := `[{"type":"input","block_id":"i","label":{"type":"plain_text","text":"L"},"element":{"type":"plain_text_input","action_id":"t","initial_value":"a"}}]`
	s := &Surface{}
	s.set(&core.View{ID: "V", Type: "modal", Blocks: json.RawMessage(bs)}, false)
	if s.dirty() {
		t.Fatal("fresh view dirty")
	}
	s.vals.get("i", "t").Text = "b"
	if !s.dirty() {
		t.Fatal("edit not dirty")
	}
	s.vals.get("i", "t").Text = "a"
	if s.dirty() {
		t.Fatal("reverted edit dirty")
	}
}
