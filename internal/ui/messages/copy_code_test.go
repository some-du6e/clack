package messages

// Copying code (Karim: "I can't seem to copy code blocks `like this`"),
// against a synthetic message with text, inline code, a link, a mention,
// an emoji and a code block. What Slack's web client puts on the
// clipboard (build a84662d2d135, boot-shared): text/plain is the cleaned
// selection's textContent (Djgz getCleanedSelectionPlainText: no backticks
// or fences, mentions as their "@name", links as their text, emoji images
// as data-stringify-emoji ":name:"); code also has its own copy paths,
// the code block's Copy button and the right-click "Copy this code"
// items (richview/codecopy.go).

import (
	"encoding/json"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
)

const tsCopyCode = "1700000020.000100"

// Slack's composer ends a section before a code block with "\n".
const copyCodeBlocks = `[{"type":"rich_text","elements":[
{"type":"rich_text_section","elements":[
{"type":"text","text":"run "},
{"type":"text","text":"go vet","style":{"code":true}},
{"type":"text","text":" see "},
{"type":"link","url":"https://example.com/doc","text":"the doc"},
{"type":"text","text":" ask "},
{"type":"user","user_id":"UBOB"},
{"type":"text","text":" "},
{"type":"emoji","name":"smile","unicode":"1f604"},
{"type":"text","text":"\n"}]},
{"type":"rich_text_preformatted","elements":[{"type":"text","text":"line one\nline two"}]}]}]`

const (
	copyAll   = "run go vet see the doc ask @bob :smile:\nline one\nline two"
	copySpan  = "go vet"
	copyBlock = "line one\nline two"
)

func newCopyEnv(t *testing.T) *ctxEnv {
	t.Helper()
	m := slack.Message{Type: "message", TS: tsCopyCode, User: "UALICE",
		Text:   "run `go vet` see <https://example.com/doc|the doc> ask <@UBOB> :smile:\n```line one\nline two```",
		Blocks: json.RawMessage(copyCodeBlocks)}
	return newCtxEnvIn(t, tGen, []slack.Message{m})
}

// drag presses at a, drags to b and releases.
func (e *ctxEnv) drag(a, b f32.Point) {
	e.clock += time.Second
	mid := a.Add(b).Mul(.5)
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Position: a, Source: pointer.Mouse, Time: e.clock},
		pointer.Event{Kind: pointer.Press, Position: a, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: e.clock + 10*time.Millisecond},
		pointer.Event{Kind: pointer.Move, Position: a.Add(f32.Pt(8, 0)), Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: e.clock + 30*time.Millisecond},
		pointer.Event{Kind: pointer.Move, Position: mid, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: e.clock + 50*time.Millisecond},
		pointer.Event{Kind: pointer.Move, Position: b, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: e.clock + 70*time.Millisecond},
		pointer.Event{Kind: pointer.Release, Position: b, Source: pointer.Mouse, Time: e.clock + 90*time.Millisecond},
	)
	for k := 0; k < 3; k++ {
		e.frame()
	}
}

// taps clicks p n times in a row (a triple-click for n=3).
func (e *ctxEnv) taps(p f32.Point, n int) {
	e.clock += time.Second
	evs := []event.Event{pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: e.clock}}
	for i := 0; i < n; i++ {
		t := e.clock + time.Duration(10+i*80)*time.Millisecond
		evs = append(evs,
			pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: t},
			pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: t + 30*time.Millisecond})
	}
	e.r.Queue(evs...)
	for k := 0; k < 3; k++ {
		e.frame()
	}
}

// clip is the text last written to the clipboard ("" for none).
func (e *ctxEnv) clip() string {
	_, b, _ := e.r.WriteClipboard()
	return string(b)
}

// copyKeys presses Cmd/Ctrl+C and returns what went to the clipboard.
func (e *ctxEnv) copyKeys() string {
	e.r.Queue(key.Event{Name: "C", Modifiers: key.ModShortcut, State: key.Press})
	e.frame()
	c := e.clip()
	e.r.Queue(key.Event{Name: "C", Modifiers: key.ModShortcut, State: key.Release})
	e.frame()
	return c
}

// at is the view point of plain offset off in ts's body: the first point
// whose offset is off while the point left of it maps before off.
func (e *ctxEnv) at(ts string, off int32) f32.Point {
	e.t.Helper()
	s := e.v.s
	r := e.row(ts)
	top := s.pos[s.sel.keyIdx[ts]]
	for y := 1; y < r.bodyRect.Dy(); y += 2 {
		for x := 1; x < r.bodyRect.Dx(); x++ {
			p := f32.Pt(float32(x)+.5, float32(y)+.5)
			if r.rv.OffsetAt(p) == off && r.rv.OffsetAt(p.Sub(f32.Pt(1, 0))) < off {
				return p.Add(f32.Pt(float32(r.bodyRect.Min.X)+1, float32(top+r.bodyRect.Min.Y)))
			}
		}
	}
	e.t.Fatalf("no point for offset %d", off)
	return f32.Point{}
}

// bodyPt is p (row body coordinates) in view coordinates.
func (e *ctxEnv) bodyPt(ts string, p f32.Point) f32.Point {
	s := e.v.s
	r := e.row(ts)
	return p.Add(f32.Pt(float32(r.bodyRect.Min.X), float32(s.pos[s.sel.keyIdx[ts]]+r.bodyRect.Min.Y)))
}

func plainOf(e *ctxEnv) string {
	r := e.row(tsCopyCode)
	return r.rv.TextRange(0, r.rv.PlainLen())
}

func offOf(t *testing.T, pl, sub string) int32 {
	t.Helper()
	i := strings.Index(pl, sub)
	if i < 0 {
		t.Fatalf("%q not in %q", sub, pl)
	}
	return int32(i)
}

// The plain text is what Slack writes as text/plain for the message.
func TestCopyPlainMatchesSlack(t *testing.T) {
	e := newCopyEnv(t)
	if got := plainOf(e); got != copyAll {
		t.Fatalf("plain %q, want %q", got, copyAll)
	}
}

// Dragging across the whole message (text, code span, link, mention,
// emoji, code block) and pressing Cmd/Ctrl+C copies all of it.
func TestCopyDragAcrossCode(t *testing.T) {
	e := newCopyEnv(t)
	r := e.row(tsCopyCode)
	a := e.bodyPt(tsCopyCode, f32.Pt(0.5, 2))
	b := e.bodyPt(tsCopyCode, f32.Pt(float32(r.bodyRect.Dx())-40, float32(r.bodyRect.Dy())-2))
	e.drag(a, b)
	if got := e.copyKeys(); got != copyAll {
		t.Errorf("clipboard %q, want %q", got, copyAll)
	}
}

// A selection inside the code span or inside the code block copies just
// that.
func TestCopyInsideCode(t *testing.T) {
	e := newCopyEnv(t)
	pl := plainOf(e)
	i := offOf(t, pl, "go vet")
	e.drag(e.at(tsCopyCode, i), e.at(tsCopyCode, i+int32(len("go v"))))
	if got := e.copyKeys(); got != "go v" {
		t.Errorf("in the code span: clipboard %q, want %q", got, "go v")
	}
	j := offOf(t, pl, "one\nline")
	e.drag(e.at(tsCopyCode, j), e.at(tsCopyCode, j+int32(len("one\nline"))))
	if got := e.copyKeys(); got != "one\nline" {
		t.Errorf("in the code block: clipboard %q, want %q", got, "one\nline")
	}
}

// A triple-click inside code selects the code run or the whole block
// (selectContentsOnTripleMouseDown).
func TestCopyTripleClickCode(t *testing.T) {
	e := newCopyEnv(t)
	pl := plainOf(e)
	e.taps(e.at(tsCopyCode, offOf(t, pl, "vet")), 3)
	if got := e.copyKeys(); got != copySpan {
		t.Errorf("triple-click in the code span copied %q, want %q", got, copySpan)
	}
	e.taps(e.at(tsCopyCode, offOf(t, pl, "two")), 3)
	if got := e.copyKeys(); got != copyBlock {
		t.Errorf("triple-click in the code block copied %q, want %q", got, copyBlock)
	}
}

// The code block's Copy button copies the block without a selection and
// doesn't start one.
func TestCopyCodeBlockButton(t *testing.T) {
	e := newCopyEnv(t)
	r := e.row(tsCopyCode)
	var btn f32.Point
	found := false
	for y := 0; y < r.bodyRect.Dy() && !found; y++ {
		for x := r.bodyRect.Dx() - 1; x >= 0; x-- {
			if p := f32.Pt(float32(x)+.5, float32(y)+.5); r.rv.OnCopyButton(p) {
				btn, found = e.bodyPt(tsCopyCode, p.Add(f32.Pt(-10, 10))), true // inside the 28px button
				break
			}
		}
	}
	if !found {
		t.Fatal("no copy button on the code block")
	}
	e.press(btn, pointer.ButtonPrimary, 0)
	if got := e.clip(); got != copyBlock {
		t.Errorf("Copy button: clipboard %q, want %q", got, copyBlock)
	}
	if e.v.s.sel.active {
		t.Errorf("the button click selected %q", e.v.s.selectedText(e.h))
	}
}

// menuEnv lays out one menu over its own router.
type menuEnv struct {
	r   input.Router
	ops op.Ops
	m   *popups.Menu
	now time.Time
}

func (me *menuEnv) frame(f func(gtx layout.Context)) string {
	me.ops.Reset()
	me.now = me.now.Add(16 * time.Millisecond)
	gtx := layout.Context{Ops: &me.ops, Source: me.r.Source(), Now: me.now,
		Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1000, 800))}
	me.m.Layout(gtx)
	if f != nil {
		f(gtx)
	}
	me.r.Frame(&me.ops)
	_, b, _ := me.r.WriteClipboard()
	return string(b)
}

// choose runs the item labelled l as a click does and returns the
// clipboard.
func choose(t *testing.T, m *popups.Menu, l string) string {
	t.Helper()
	me := &menuEnv{m: m, now: time.Now()}
	me.frame(nil)
	for _, it := range m.Items() {
		if it.Label == l {
			if it.DoCtx == nil {
				t.Fatalf("%q has no DoCtx", l)
			}
			return me.frame(func(gtx layout.Context) { it.DoCtx(gtx) })
		}
	}
	t.Fatalf("no %q in %q", l, labels(m))
	return ""
}

// A right-click on inline code or a code block opens the shell's menu
// with Slack's one item: Copy this code / Copy this code block.
func TestCopyCodeContextMenu(t *testing.T) {
	e := newCopyEnv(t)
	pl := plainOf(e)
	e.rightClick(e.at(tsCopyCode, offOf(t, pl, "vet")))
	m := e.menu(0, "right-click on inline code")
	if got := strings.Join(labels(m), "|"); got != "Copy this code" {
		t.Fatalf("code menu %q", got)
	}
	if got := choose(t, m, "Copy this code"); got != copySpan {
		t.Errorf("Copy this code: %q, want %q", got, copySpan)
	}
	e.rightClick(e.at(tsCopyCode, offOf(t, pl, "two")))
	m = e.menu(1, "right-click on a code block")
	if got := strings.Join(labels(m), "|"); got != "Copy this code block" {
		t.Fatalf("code block menu %q", got)
	}
	if got := choose(t, m, "Copy this code block"); got != copyBlock {
		t.Errorf("Copy this code block: %q, want %q", got, copyBlock)
	}
	// plain text still gets the message menu
	e.rightClick(e.at(tsCopyCode, 1))
	if m := e.menu(2, "right-click on text"); hasLabel(m, "Copy this code") {
		t.Errorf("text menu %q", labels(m))
	}
}

// With a selection across code, the selection menu's Copy text (click,
// or its ⌘C / Ctrl+C key) copies the selection.
func TestCopyTextMenuWithCode(t *testing.T) {
	e := newCopyEnv(t)
	pl := plainOf(e)
	a, b := offOf(t, pl, "un "), offOf(t, pl, " see")
	e.drag(e.at(tsCopyCode, a), e.at(tsCopyCode, b))
	want := pl[a:b]
	e.rightClick(e.at(tsCopyCode, offOf(t, pl, "vet")))
	m := e.menu(0, "right-click on the selection")
	if got := strings.Join(labels(m), "|"); got != "Copy text|Search in Slack" {
		t.Fatalf("selection menu %q", got)
	}
	if got := choose(t, m, "Copy text"); got != want {
		t.Errorf("Copy text: %q, want %q", got, want)
	}
	// ⌘C / Ctrl+C on the open menu (menu keys)
	e.rightClick(e.at(tsCopyCode, offOf(t, pl, "vet")))
	m = e.menu(1, "right-click again")
	me := &menuEnv{m: m, now: time.Now()}
	me.frame(nil)
	me.frame(nil) // the first frame's FocusCmd holds that frame's events
	me.r.Queue(key.Event{Name: "C", Modifiers: key.ModShortcut, State: key.Press})
	got := me.frame(nil)
	me.r.Queue(key.Event{Name: "C", Modifiers: key.ModShortcut, State: key.Release})
	if c := me.frame(nil); got == "" {
		got = c
	}
	if got != want {
		t.Errorf("⌘C on the menu: %q, want %q", got, want)
	}
}
