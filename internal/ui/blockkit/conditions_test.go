package blockkit

import (
	"encoding/json"
	"image"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// TestConditionElements: every surface expands condition blocks before it
// looks its elements up (CRur useExpandedConditionBlocks in VoyB, +Yyz
// and WSwB), so the desktop case's actions button is a drawn element
// widget in a message, a modal and App Home.
func TestConditionElements(t *testing.T) {
	h, app := visibleHost()
	msgsMu.Lock()
	msgs, cur, revs = map[string]*Message{}, map[string]string{}, map[string][]string{}
	msgsMu.Unlock()
	when := func(client string, bid, aid string) map[string]any {
		return map[string]any{"conditions": []any{map[string]any{"type": "client", "op": "=", "value": client}},
			"blocks": []any{map[string]any{"type": "actions", "block_id": bid, "elements": []any{visBtn(aid)}}}}
	}
	raw, _ := json.Marshal([]any{map[string]any{"type": "condition", "cases": []any{when("ios", "a0", "ios"), when("desktop", "a1", "go")}}})
	sm := slack.Message{TS: "1762520000.000902", User: "U2", Blocks: raw}
	m := core.Message{TS: sm.TS, Channel: "C0COND", User: "U2", Blocks: raw, Doc: richtext.FromMessage(app.S, &sm)}
	me := MessageElements(h, &m)
	if me == nil || len(m.Doc.Blocks) != 1 || m.Doc.Blocks[0].Kind != richtext.Actions {
		t.Fatalf("host %v, blocks %+v", me != nil, m.Doc.Blocks)
	}
	gtx := visGtx(720)
	bs := m.Doc.Blocks[0].Buttons
	sz, _ := me.ElementsSize(bs, 720, false, gtx.Metric)
	me.LayoutElements(fixed(gtx, sz), bs, false)
	if w := me.ws["a1\x00go"]; w == nil || w.main.sz == (image.Point{}) {
		t.Error("message: the desktop case's button isn't a drawn widget")
	}
	for _, typ := range []string{"modal", "home"} {
		s := newSurface(h, &core.View{Type: typ, Blocks: raw})
		s.Layout(visGtx(480))
		if w := s.ws["a1\x00go\x00button"]; w == nil || w.main.sz == (image.Point{}) {
			t.Errorf("%s: the desktop case's button isn't a drawn widget", typ)
		}
		if s.ws["a0\x00ios\x00button"] != nil {
			t.Errorf("%s: the ios case drew", typ)
		}
	}
}
