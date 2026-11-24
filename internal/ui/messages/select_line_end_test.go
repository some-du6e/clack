package messages

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Drags past a line's end select as Chromium does (Karim, alpha.102: "it
// seems i cant drag to select the end of the line", a bot's multi-line
// rich_text under plugins that patch Blocks; backlog P54). Slack's
// rich_text sections are blocks across the message's text column, so a
// point right of a line hit-tests the block, and LayoutBlockFlow's
// PositionForPoint takes the nearest line by its block offset and the end
// of that line (InlineCursor::PositionForPointInInlineFormattingContext):
// a drag from mid-line to well past the right edge selects through the
// line's end, a drag below the last message to the end of the text, and
// a press past a line's end starts the selection at that end. Clack
// started no selection for a press right of the widest line in a row
// drawn natively (its body box was only as wide as its text), and the
// same in a plugin's tree that wraps the text in an inline element.
// Plain, patched (pass-through) and inline-wrapped, a one-block and a
// two-block message, the copied text checked. Synthetic text only.

const (
	sleOne = "1700000500.000100"
	sleTwo = "1700000501.000100"
)

// sleInlinePlugin wraps Blocks in a span, as a plugin that decorates the
// text inline would.
var sleInlinePlugin = pluginJS(`
class InlineBlocks extends TautPlugin {
  static id = 'InlineBlocks'
  static pluginName = 'Inline blocks (test)'
  static description = 'test: Blocks inside an inline element'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const R = React
    this.api.patchComponent('Blocks', (O) => (p) => R.createElement('span', { className: 'x-inline-blocks' }, R.createElement(O, p)))
  }
}
return InlineBlocks`)

func TestSelectLineEnd(t *testing.T) {
	for _, mode := range []string{"plain", "patched", "inline"} {
		t.Run(mode, func(t *testing.T) {
			e := sleEnv(t, mode)
			for _, c := range []struct {
				ts, sub, next         string
				toEnd, below, fromEnd string
			}{
				{sleOne, "rows", "all done", "ws written: 120 (data/meta)", "ws written: 120 (data/meta)\nall done\n", "\nal"},
				{sleTwo, "first", "second block", "rst block line one", "rst block line one\nshort\nsecond block text", "\nshort\nse"},
			} {
				p := e.selAtIn(c.ts, c.sub, 1)
				far := f32.Pt(880, p.Y) // well past the line's right end, inside the view
				if got, _ := e.sleDrag(p, far); got != c.toEnd {
					t.Errorf("%s %s: mid-line → past its end selected %q, want %q", mode, c.ts, got, c.toEnd)
				}
				got, b := e.sleDrag(p, f32.Pt(p.X, 695)) // below the last message
				if !strings.HasPrefix(got, c.below) || !strings.HasSuffix(got, "second block text") {
					t.Errorf("%s %s: mid-line → below the list selected %q, want %q… through the last message's end", mode, c.ts, got, c.below)
				}
				if b.key != sleTwo || b.off != e.row(sleTwo).rv.PlainLen() {
					t.Errorf("%s %s: below the list ended at %v, want the last message's end", mode, c.ts, b)
				}
				if got, _ := e.sleDrag(far, e.selAtIn(c.ts, c.next, 1)); got != c.fromEnd {
					t.Errorf("%s %s: a press past the line's end dragged to the next line selected %q, want %q", mode, c.ts, got, c.fromEnd)
				}
			}
		})
	}
}

// sleDrag presses at from, drags to to, releases, and returns what's
// selected and where the selection ends; then a click elsewhere clears it.
func (e *ctxEnv) sleDrag(from, to f32.Point) (string, selPoint) {
	e.selEvents(selMove(from, 0), selPress(from, 10*time.Millisecond), selDrag(from.Add(f32.Pt(6, 0)), 30*time.Millisecond),
		selDrag(to, 60*time.Millisecond), selRelease(to, 90*time.Millisecond))
	got, b := e.selected(), e.v.s.sel.b
	e.selEvents(selClicks(f32.Pt(5, 5), 1, 0)...)
	return got, b
}

// sleEnv shows a bot's three-line rich_text and a two-block message in
// #general: drawn natively ("plain"), under a pass-through plugin on
// Blocks, MessageText and MessageWrapper ("patched"), or with Blocks
// wrapped in an inline span ("inline").
func sleEnv(t *testing.T, mode string) *ctxEnv {
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
	if mode == "plain" {
		return newCtxEnvIn(t, tGen, msgs)
	}
	app, _, tt, _, ph := pluginEnv(t)
	if mode == "inline" {
		install(t, tt, sleInlinePlugin)
		until(t, 5*time.Second, "the patch", func() {}, func() bool { return slackc.Patched("Blocks") })
	} else {
		install(t, tt, pwPlugin)
		until(t, 5*time.Second, "the patches", func() {}, func() bool {
			return slackc.Patched("Blocks") && slackc.Patched("MessageText") && slackc.Patched("MessageWrapper")
		})
	}
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
// maps points (bodyRect, OffsetAt).
func (e *ctxEnv) selAtIn(ts, sub string, off int) f32.Point {
	e.t.Helper()
	s := e.v.s
	r := e.row(ts)
	top := s.pos[s.sel.keyIdx[ts]]
	pl := r.rv.TextRange(0, r.rv.PlainLen())
	i := strings.Index(pl, sub)
	if i < 0 {
		e.t.Fatalf("%q not in %q", sub, pl)
	}
	want := int32(i + off)
	for y := 1; y < r.bodyRect.Dy(); y += 2 {
		for x := 1; x < r.bodyRect.Dx(); x++ {
			p := f32.Pt(float32(x)+.5, float32(y)+.5)
			if r.rv.OffsetAt(p) == want+1 && r.rv.OffsetAt(p.Sub(f32.Pt(1, 0))) <= want {
				return p.Add(f32.Pt(float32(r.bodyRect.Min.X), float32(top+r.bodyRect.Min.Y)))
			}
		}
	}
	e.t.Fatalf("no point for %q+%d in %s (plain %q)", sub, off, ts, pl)
	return f32.Point{}
}
