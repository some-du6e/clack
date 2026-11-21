package blockkit

import (
	"encoding/json"
	"image"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// TestCardButtonsOneRow: a card's buttons in a message are Card's one
// flex row (7Xkb: .actionsRight has an 8px column gap, its buttons flex
// 0 auto with min-width 0): every action shows and they shrink rather
// than wrap, with no "+ N more" (that's the Actions block's) and no
// status icons (the Card passes no loading state).
func TestCardButtonsOneRow(t *testing.T) {
	h, app := visibleHost()
	msgsMu.Lock()
	msgs, cur, revs = map[string]*Message{}, map[string]string{}, map[string][]string{}
	msgsMu.Unlock()
	var acts []any
	for _, id := range []string{"c0", "c1", "c2", "c3", "c4", "c5", "c6"} {
		acts = append(acts, visBtn(id))
	}
	raw, _ := json.Marshal([]any{map[string]any{"type": "card", "block_id": "k", "title": map[string]any{"type": "plain_text", "text": "Seven"},
		"actions": acts}})
	sm := slack.Message{TS: "1762520000.000902", User: "U2", Blocks: raw}
	m := core.Message{TS: sm.TS, Channel: "C0CARDROW", User: "U2", Blocks: raw, Doc: richtext.FromMessage(app.S, &sm)}
	me := MessageElements(h, &m)
	if me == nil || len(m.Doc.Blocks) != 1 || m.Doc.Blocks[0].Card == nil {
		t.Fatalf("no interactive card: %+v", m.Doc.Blocks)
	}
	right := m.Doc.Blocks[0].Card.Right
	if len(right) != 7 {
		t.Fatalf("%d actions in the right group, want 7", len(right))
	}
	gtx := visGtx(720)
	one, _ := me.ElementsSize(right[:1], 720, false, gtx.Metric)
	sz, _ := me.ElementsSize(right, 300, false, gtx.Metric)
	if sz.Y != one.Y || sz.X > 300 {
		t.Errorf("seven buttons in 300px: %v, want one row (%dpx tall) no wider than 300", sz, one.Y)
	}
	me.LayoutElements(fixed(gtx, sz), right, false)
	n := 0
	for _, bt := range right {
		if w := me.ws["k\x00"+bt.ActionID]; w != nil && w.main.sz != (image.Point{}) {
			n++
			if w.slot.Y != 0 {
				t.Errorf("%s on a second row (%v)", bt.ActionID, w.slot)
			}
		}
	}
	if n != 7 {
		t.Errorf("%d of 7 buttons drawn, want every one (no \"+ N more\")", n)
	}
	if rs := me.RunRects("k"); len(rs) != 0 {
		t.Errorf("an actions-block run %v (with its \"+ N more\" and status icons) for a card", rs)
	}
}
