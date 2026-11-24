package messages

import (
	"encoding/json"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// TestSelectLineEndProbe logs (and asserts nothing) how a drag past a
// line's end selects, plain and under a plugin that patches Blocks,
// MessageText and MessageWrapper (Karim's alpha.102 report, P54): the
// body's box against where its richview was drawn, and what each drag
// selected. A probe for the select-line-end fix.
func TestSelectLineEndProbe(t *testing.T) {
	for _, mode := range []string{"plain", "patched"} {
		t.Run(mode, func(t *testing.T) {
			e := sleEnv(t, mode == "patched")
			for _, c := range []struct{ ts, sub, next string }{{sleOne, "rows", "all done"}, {sleTwo, "first", "second block"}} {
				r := e.row(c.ts)
				s := e.v.s
				top := s.pos[s.sel.keyIdx[c.ts]]
				p := e.selAtIn(c.ts, c.sub, 1)
				e.selEvents(selMove(p, 0))
				org, ok := r.rv.WindowOrigin()
				t.Logf("%s %s: bodyRect %v (view y %d), richview drawn at %v (%v), plain %q", mode, c.ts, r.bodyRect, top+r.bodyRect.Min.Y, org, ok, r.rv.TextRange(0, r.rv.PlainLen()))
				far := f32.Pt(880, p.Y)
				for _, d := range []struct {
					name     string
					from, to f32.Point
				}{
					{"mid → past the line's end", p, far},
					{"mid → below the list", p, f32.Pt(p.X, 695)},
					{"past the end → the next line", far, e.selAtIn(c.ts, c.next, 1)},
				} {
					e.selEvents(selMove(d.from, 0), selPress(d.from, 10*time.Millisecond), selDrag(d.from.Add(f32.Pt(6, 0)), 30*time.Millisecond),
						selDrag(d.to, 60*time.Millisecond), selRelease(d.to, 90*time.Millisecond))
					a, b := s.sel.a, s.sel.b
					t.Logf("%s %s: %s %v→%v: selected %q (a %v b %v)", mode, c.ts, d.name, d.from, d.to, e.selected(), a, b)
					e.selEvents(selClicks(f32.Pt(5, 5), 1, 0)...)
				}
			}
		})
	}
}

const (
	sleOne = "1700000500.000100"
	sleTwo = "1700000501.000100"
)

// sleEnv shows a bot's three-line rich_text and a two-block message in
// #general, under a pass-through plugin when patched.
func sleEnv(t *testing.T, patched bool) *ctxEnv {
	t.Helper()
	one := shotRich(shotSection("Export started\nrows written: 120 (data/meta)\nall done"))
	two, _ := json.Marshal([]any{
		map[string]any{"type": "rich_text", "block_id": "a", "elements": []any{shotSection("first block line one\nshort")}},
		map[string]any{"type": "rich_text", "block_id": "b", "elements": []any{shotSection("second block text")}},
	})
	msgs := []slack.Message{
		{Type: "message", TS: sleOne, User: "UALICE", Text: "Export started\nrows written: 120 (data/meta)\nall done", Blocks: one},
		{Type: "message", TS: sleTwo, User: "UBOB", Text: "first block line one\nshort\nsecond block text", Blocks: two},
	}
	if !patched {
		return newCtxEnvIn(t, tGen, msgs)
	}
	app, _, tt, _, ph := pluginEnv(t)
	install(t, tt, pwPlugin)
	until(t, 5*time.Second, "the patches", func() {}, func() bool {
		return slackc.Patched("Blocks") && slackc.Patched("MessageText") && slackc.Patched("MessageWrapper")
	})
	app.S.SetLatest(tGen, msgs, false)
	e := &ctxEnv{t: t, h: &ctxHost{coreHost: ph}, now: time.Now()}
	e.v = NewChannelView(e.h, tGen)
	e.v.Preview = true
	until(t, 10*time.Second, "the rows laid out", e.frame, func() bool { return e.row(sleOne) != nil && e.row(sleTwo) != nil })
	for k := 0; k < 30; k++ { // the plugin trees come back from JS
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
	return e
}

// selAtIn is the view point in the middle of the rune after offset off
// of sub's first rune in message ts's plain text, found the way the list
// maps points (bodyRect, OffsetAt); the row's top left when there's none
// (logged, not fatal: this is a probe).
func (e *ctxEnv) selAtIn(ts, sub string, off int) f32.Point {
	e.t.Helper()
	s := e.v.s
	r := e.row(ts)
	top := s.pos[s.sel.keyIdx[ts]]
	pl := r.rv.TextRange(0, r.rv.PlainLen())
	i := -1
	for k := 0; k+len(sub) <= len(pl); k++ {
		if pl[k:k+len(sub)] == sub {
			i = k
			break
		}
	}
	want := int32(i + off)
	for y := 1; i >= 0 && y < r.bodyRect.Dy(); y += 2 {
		for x := 1; x < r.bodyRect.Dx(); x++ {
			p := f32.Pt(float32(x)+.5, float32(y)+.5)
			if r.rv.OffsetAt(p) == want+1 && r.rv.OffsetAt(p.Sub(f32.Pt(1, 0))) <= want {
				return p.Add(f32.Pt(float32(r.bodyRect.Min.X), float32(top+r.bodyRect.Min.Y)))
			}
		}
	}
	e.t.Logf("no point for %q+%d in %s (plain %q)", sub, off, ts, pl)
	return f32.Pt(float32(r.bodyRect.Min.X), float32(top+r.bodyRect.Min.Y))
}
