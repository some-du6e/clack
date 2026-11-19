package blockkit

// visible_to_user_ids on every surface. Only the Actions block reads it
// (boot-shared /jJF maybeRenderActionsElement): a button or checkboxes
// whose list leaves out the viewer isn't drawn, but it still counts for
// "+ N more" (seven or more elements) and takes one of the first five
// places. A section's accessory and an input's element draw for everyone
// (Section bzDH, InputBlock h/Gw).

import (
	"encoding/json"
	"image"
	"slices"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// visibleHost is a host whose viewer is USELF.
func visibleHost() (*agentHost, *core.App) {
	app := core.New(slack.New(slack.Creds{Token: "xoxc-test", UserID: "USELF", Team: "T1"}), store.New(""))
	return &agentHost{app: app, th: theme.New(false)}, app
}

func visBtn(id string, vis ...string) map[string]any {
	b := map[string]any{"type": "button", "action_id": id, "text": map[string]any{"type": "plain_text", "text": id}}
	if len(vis) > 0 {
		b["visible_to_user_ids"] = vis
	}
	return b
}

func visGtx(w int) layout.Context {
	return layout.Context{Ops: new(op.Ops), Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(w, 1<<20)}}
}

// TestMessageVisibleTo: in a message, an actions block of seven with
// two hidden among the first five draws the three visible ones of those
// five and "+ 2 more"; a section's accessory listing someone else draws.
func TestMessageVisibleTo(t *testing.T) {
	h, app := visibleHost()
	msgsMu.Lock()
	msgs, cur, revs = map[string]*Message{}, map[string]string{}, map[string][]string{}
	msgsMu.Unlock()
	blocks := []any{
		map[string]any{"type": "actions", "block_id": "a", "elements": []any{
			visBtn("b0", "U2"), visBtn("b1"), visBtn("b2", "U2"), visBtn("b3"), visBtn("b4"), visBtn("b5"), visBtn("b6")}},
		map[string]any{"type": "section", "block_id": "s", "text": map[string]any{"type": "mrkdwn", "text": "Approve?"},
			"accessory": visBtn("acc", "U2")},
	}
	raw, _ := json.Marshal(blocks)
	sm := slack.Message{TS: "1762520000.000901", User: "U2", Blocks: raw}
	m := core.Message{TS: sm.TS, Channel: "C0VISIBLE", User: "U2", Blocks: raw, Doc: richtext.FromMessage(app.S, &sm)}
	me := MessageElements(h, &m)
	if me == nil {
		t.Fatal("not interactive")
	}
	if len(m.Doc.Blocks) != 2 {
		t.Fatalf("%d blocks, want the actions and the section", len(m.Doc.Blocks))
	}
	acts, sec := m.Doc.Blocks[0], m.Doc.Blocks[1]
	gtx := visGtx(720)
	sz, _ := me.ElementsSize(acts.Buttons, 720, false, gtx.Metric)
	me.LayoutElements(fixed(gtx, sz), acts.Buttons, false)
	var drawn []string
	for _, id := range []string{"b0", "b1", "b2", "b3", "b4", "b5", "b6"} {
		if w := me.ws["a\x00"+id]; w != nil && w.main.sz != (image.Point{}) {
			drawn = append(drawn, id)
		}
	}
	if want := []string{"b1", "b3", "b4"}; !slices.Equal(drawn, want) {
		t.Errorf("drawn %v, want %v", drawn, want)
	}
	rs := me.RunRects("a")
	moreW := measure(gtx, h.th, 13, theme.Bold, "+ 2 more") + 2*dp(gtx, 12)
	if len(rs) != 4 || rs[3].Dx() != moreW {
		t.Errorf("run %v, want three buttons and a %dpx \"+ 2 more\"", rs, moreW)
	}
	if len(sec.Buttons) != 1 || sec.Buttons[0].ActionID != "acc" {
		t.Fatalf("section accessory %+v, want acc", sec.Buttons)
	}
	asz, _ := me.ElementsSize(sec.Buttons, 720, true, gtx.Metric)
	me.LayoutElements(fixed(gtx, asz), sec.Buttons, true)
	if w := me.ws["s\x00acc"]; w == nil || w.main.sz == (image.Point{}) {
		t.Error("the section's accessory isn't drawn")
	}
}

// TestSurfaceVisibleTo: in a modal and in App Home, an actions block's
// hidden button gets no widget; a section's accessory and an input's
// checkboxes listing someone else draw.
func TestSurfaceVisibleTo(t *testing.T) {
	h, _ := visibleHost()
	cbs := map[string]any{"type": "checkboxes", "action_id": "in", "visible_to_user_ids": []string{"U2"},
		"options": []any{map[string]any{"text": map[string]any{"type": "plain_text", "text": "x"}, "value": "x"}}}
	blocks := []any{
		map[string]any{"type": "actions", "block_id": "a", "elements": []any{visBtn("mine", "USELF"), visBtn("theirs", "U2")}},
		map[string]any{"type": "section", "block_id": "s", "text": map[string]any{"type": "mrkdwn", "text": "Approve?"},
			"accessory": visBtn("acc", "U2")},
		map[string]any{"type": "input", "block_id": "i", "label": map[string]any{"type": "plain_text", "text": "Pick"}, "element": cbs},
	}
	raw, _ := json.Marshal(blocks)
	for _, typ := range []string{"modal", "home"} {
		s := newSurface(h, &core.View{Type: typ, Blocks: raw})
		s.Layout(visGtx(480))
		var got []string
		for _, k := range []string{"a\x00mine\x00button", "a\x00theirs\x00button", "s\x00acc\x00button", "i\x00in\x00checkboxes"} {
			if s.ws[k] != nil {
				got = append(got, k[2:])
			}
		}
		if want := []string{"mine\x00button", "acc\x00button", "in\x00checkboxes"}; !slices.Equal(got, want) {
			t.Errorf("%s: widgets %q, want %q", typ, got, want)
		}
	}
}
