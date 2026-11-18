package ui

// The inspect tool's freeze (inspect/freeze.go) on the fake Slack
// (synthetic; nothing talks to Slack). In a session F holds the window on
// the frame on screen: the hover toolbar of the message the pointer was
// on stays drawn after the pointer leaves it, the picker outlines the
// toolbar's buttons from the frozen frame, and a Shift+click on one
// copies it from there at once. Nothing is laid out while frozen, so a
// message that lands meanwhile shows only once the freeze ends. F again
// or Esc unfreezes (Esc again ends the session), the composer keeps the
// focus, the frozen frame's ops and the animated images' hold go, and the
// window catches up: the message the pointer left loses its hover.

import (
	"bytes"
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"weak"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/inspect"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// list is the channel's message list.
func (e *inspectEnv) list(t *testing.T) channelList {
	t.Helper()
	cv := e.a.convos[e.ch+"/"]
	if cv == nil {
		t.Fatal("no conversation for the channel")
	}
	l, ok := cv.list.(channelList)
	if !ok {
		t.Fatalf("the channel's list is a %T", cv.list)
	}
	return l
}

// toolbar is the hover toolbar over the pill's message as the last
// layout drew it (window px).
func (e *inspectEnv) toolbar(t *testing.T) (image.Rectangle, bool) {
	t.Helper()
	ts, r, ok := e.list(t).ToolbarRect()
	return r, ok && ts == e.ts
}

// sidebarPoint is a point in the sidebar, off the message list.
func (e *inspectEnv) sidebarPoint() f32.Point {
	x := (e.a.sidebar.origin.X + e.a.primaryOrigin.X) / 2
	return f32.Pt(float32(x), float32(e.size.Y/2))
}

func (e *inspectEnv) freezeKey() {
	e.key(inspect.FreezeKey, 0)
	e.frame()
}

func TestInspectFreezeHoverToolbar(t *testing.T) {
	shots, copied := fakeShots(t)
	e := newInspectEnv(t)
	e.startSession(t) // the pointer on the pill: its message hovered, under its toolbar
	tb, ok := e.toolbar(t)
	if !ok {
		t.Fatal("no hover toolbar over the pill's message in the session")
	}
	more := image.Pt(tb.Max.X-tb.Dy()/2, tb.Min.Y+tb.Dy()/2) // the toolbar's last button
	morePt := f32.Pt(float32(more.X), float32(more.Y))
	away := e.sidebarPoint()

	// unfrozen, the toolbar goes with the pointer
	e.move(away)
	e.frame()
	if _, ok := e.toolbar(t); ok {
		t.Fatal("the toolbar stayed with the pointer in the sidebar: the test needs one that goes")
	}
	e.move(e.pillCenter())
	e.frame()
	if _, ok := e.toolbar(t); !ok {
		t.Fatal("no toolbar back over the pill's message")
	}

	e.freezeKey()
	if !e.a.insp.Frozen() || e.frozen == nil {
		t.Fatalf("after F: frozen %v, the frame drawn from the frozen ops %v", e.a.insp.Frozen(), e.frozen != nil)
	}
	seq := e.a.frameSeq.Load()
	// the pointer leaves the message for the sidebar: the frozen frame's
	// sidebar is outlined, and its toolbar stays
	e.move(away)
	if label, r, ok := e.a.insp.Hovered(); !ok || !image.Pt(int(away.X), int(away.Y)).In(r) || r.Max.X > e.a.primaryOrigin.X {
		t.Errorf("frozen, in the sidebar: outline %q %v (%v), want one in the sidebar around %v", label, r, ok, away)
	}
	// onto the toolbar's button in the frozen frame: outlined at once (a
	// window laid out with the pointer in the sidebar has no toolbar to
	// outline in the first frame)
	e.move(morePt)
	label, r, ok := e.a.insp.Hovered()
	if !ok || !strings.HasPrefix(label, "button.c-button-unstyled.c-icon_button") || !more.In(r) {
		t.Fatalf("frozen, over the toolbar's button: outline %q %v (%v), want the button around %v", label, r, ok, more)
	}
	e.key(key.NameUpArrow, 0)
	e.frame()
	if label, _, _ := e.a.insp.Hovered(); !strings.HasPrefix(label, "MessageActions") {
		t.Errorf("frozen, ↑ from the button: outline %q, want MessageActions", label)
	}
	e.key(key.NameDownArrow, 0)
	e.frame()
	if n := e.a.frameSeq.Load(); n != seq {
		t.Errorf("%d frames laid out while frozen", n-seq)
	}

	// Shift+click copies it at once, from the frozen frame; the session
	// and the freeze end
	e.pickAndCopy(t, more)
	if e.a.insp.Active() || e.a.insp.Frozen() {
		t.Fatalf("after the Shift+click: session %v, frozen %v", e.a.insp.Active(), e.a.insp.Frozen())
	}
	if *shots != 1 || *copied == nil || !fakeShotRegions[0].Overlaps(r) {
		t.Fatalf("after the Shift+click: %d shots %v, copied %v, want one around the button %v", *shots, fakeShotRegions, *copied != nil, r)
	}
	txt, _ := e.readReport(t)
	rows := reportRows(txt)
	for _, w := range []struct{ key, want string }{
		{"component", "MessageActions"},
		{"element", "button.c-button-unstyled.c-icon_button*"},
		{"region", "primary"},
	} {
		if got := rows[w.key]; !globMatch(w.want, got) {
			t.Errorf("report row %s = %q, want %q\n%s", w.key, got, w.want, txt)
		}
	}
	// the clicks never reached the window
	e.settle(300 * time.Millisecond)
	if got := e.reactions(); len(got) != 0 {
		t.Errorf("the clicks toggled a reaction: %v", got)
	}
	for _, o := range e.a.overlays {
		if o != nil && !o.Dismissed() {
			t.Errorf("the Shift+click on the toolbar's button opened %T", o)
		}
	}
}

// A plain click on the frozen frame opens the review over it, still
// frozen; Enter copies the report and ends the session and the freeze.
// A drag on the frozen frame grabs its region the same way.
func TestInspectFreezeClickReviews(t *testing.T) {
	shots, copied := fakeShots(t)
	e := newInspectEnv(t)
	e.startSession(t)
	e.freezeKey()
	if !e.a.insp.Frozen() {
		t.Fatal("F didn't freeze")
	}
	p := e.pillCenter()
	e.click(image.Pt(int(p.X), int(p.Y)))
	e.frame()
	e.a.inspectAfterFrame(&e.ops, e.size)
	if !e.a.insp.Reviewing() || !e.a.insp.Frozen() || *shots != 1 {
		t.Fatalf("after a plain click: reviewing %v, frozen %v, %d shots; want the review over the frozen frame", e.a.insp.Reviewing(), e.a.insp.Frozen(), *shots)
	}
	e.reviewAndCopy(t)
	if e.a.insp.Active() || e.a.insp.Frozen() || *copied == nil {
		t.Fatalf("after Enter: session %v, frozen %v, copied %v", e.a.insp.Active(), e.a.insp.Frozen(), *copied != nil)
	}
	if rows := reportRows(func() string { s, _ := e.readReport(t); return s }()); rows["component"] != "UnstyledButton" {
		t.Errorf("the reviewed report's component %q, want the pill's UnstyledButton", rows["component"])
	}

	// a drag on the frozen frame: its region, reviewed
	e.startSession(t)
	e.freezeKey()
	from := image.Pt(int(p.X)-60, int(p.Y)-40)
	to := from.Add(image.Pt(200, 90))
	e.drag(from, from.Add(image.Pt(3, 2)), to)
	e.release(to)
	e.frame()
	e.a.inspectAfterFrame(&e.ops, e.size)
	if !e.a.insp.Reviewing() || !e.a.insp.Frozen() || *shots != 2 || fakeShotRegions[1] != (image.Rectangle{Min: from, Max: to}) {
		t.Fatalf("after a drag: reviewing %v, frozen %v, shots %v, want the region %v reviewed over the frozen frame", e.a.insp.Reviewing(), e.a.insp.Frozen(), fakeShotRegions, image.Rectangle{Min: from, Max: to})
	}
	e.key(key.NameEscape, 0)
	e.frame()
	if e.a.insp.Active() || e.a.insp.Frozen() {
		t.Errorf("Esc in the review: session %v, frozen %v", e.a.insp.Active(), e.a.insp.Frozen())
	}
}

// F again unfreezes, and so does Esc, which again ends the session; so
// does Hyper+D. While frozen the animated images are held. When the
// freeze ends its ops go, the composer still has the focus, and the
// window catches up: the pointer left the message during the freeze, and
// the message's hover (its toolbar) goes with the next frames.
func TestInspectFreezeKeys(t *testing.T) {
	shots, _ := fakeShots(t)
	e := newInspectEnv(t)
	e.focusComposer(e.ch, "")
	focus := e.r.Source().FocusTag()
	if focus == nil {
		t.Fatal("no focus in the composer")
	}
	e.startSession(t)
	if _, ok := e.toolbar(t); !ok {
		t.Fatal("no hover toolbar over the pill's message in the session")
	}
	e.freezeKey()
	if !e.a.insp.Frozen() || !e.a.insp.Active() {
		t.Fatalf("after F: frozen %v, session %v", e.a.insp.Frozen(), e.a.insp.Active())
	}
	if !e.a.images.Stats().LiveHeld {
		t.Error("frozen, the animated images aren't held")
	}
	frozenOps := weak.Make(e.a.insp.FrozenOps())
	e.move(e.sidebarPoint())
	e.frame()

	e.freezeKey()
	if e.a.insp.Frozen() || !e.a.insp.Active() {
		t.Fatalf("after F again: frozen %v, session %v; want unfrozen, the session on", e.a.insp.Frozen(), e.a.insp.Active())
	}
	if e.a.images.Stats().LiveHeld {
		t.Error("unfrozen, the animated images are still held")
	}
	for range 3 {
		e.frame()
	}
	if _, ok := e.toolbar(t); ok {
		t.Error("after the freeze the toolbar stayed over the message the pointer left during it")
	}
	if got := e.r.Source().FocusTag(); got != focus {
		t.Errorf("after the freeze the focus is on %v, want the composer's %v", got, focus)
	}
	runtime.GC()
	runtime.GC()
	if frozenOps.Value() != nil {
		t.Error("the frozen frame's ops outlived the freeze")
	}

	// F, then Esc: unfrozen, the session on; Esc again ends it
	e.move(e.pillCenter())
	e.frame()
	e.freezeKey()
	if !e.a.insp.Frozen() {
		t.Fatal("F didn't freeze again")
	}
	e.key(key.NameEscape, 0)
	e.frame()
	if e.a.insp.Frozen() || !e.a.insp.Active() {
		t.Fatalf("after Esc: frozen %v, session %v; want unfrozen, the session on", e.a.insp.Frozen(), e.a.insp.Active())
	}
	e.key(key.NameEscape, 0)
	e.frame()
	if e.a.insp.Active() {
		t.Error("Esc again didn't end the session")
	}

	// Hyper+D ends a frozen session
	e.startSession(t)
	e.freezeKey()
	e.hyperD()
	e.frame()
	if e.a.insp.Active() || e.a.insp.Frozen() {
		t.Errorf("Hyper+D while frozen: session %v, frozen %v", e.a.insp.Active(), e.a.insp.Frozen())
	}
	if *shots != 0 {
		t.Errorf("%d shots without a capture", *shots)
	}
	// and the composer still takes what's typed
	e.frame()
	cb, ok := e.a.convos[e.ch+"/"].comp.(composerBox)
	if !ok {
		t.Fatalf("the composer is a %T", e.a.convos[e.ch+"/"].comp)
	}
	before := cb.DocText()
	e.typeAt("z")
	e.frame()
	if got := cb.DocText(); got == before {
		t.Errorf("after the freezes the composer took nothing typed (%q)", got)
	}
}

// A message that lands while the window is frozen shows nothing: no frame
// is laid out, every one is drawn from the frozen ops. When the freeze
// ends the window lays the message out.
func TestInspectFreezeMessageArrives(t *testing.T) {
	fakeShots(t)
	e := newInspectEnv(t)
	e.startSession(t)
	e.freezeKey()
	if !e.a.insp.Frozen() {
		t.Fatal("F didn't freeze")
	}
	seq := e.a.frameSeq.Load()
	ops := e.frozen
	ts := e.post(t, "a message while the window is frozen")
	e.settle(300 * time.Millisecond)
	if n := e.a.frameSeq.Load(); n != seq {
		t.Errorf("%d frames laid out while frozen, with a message landing", n-seq)
	}
	if e.frozen != ops {
		t.Error("a frame while frozen wasn't drawn from the frozen ops")
	}
	shown := func() bool {
		return slices.ContainsFunc(e.list(t).VisibleRows(), func(r messages.RowY) bool { return r.Key == ts })
	}
	if shown() {
		t.Error("the message that landed while frozen was laid out")
	}

	e.freezeKey()
	if e.a.insp.Frozen() {
		t.Fatal("F again didn't unfreeze")
	}
	e.until("the message laid out after the freeze", 10*time.Second, shown)
}

// post puts a message by the signed-in user in the channel, as the socket
// would bring it, and waits until core has it.
func (e *inspectEnv) post(t *testing.T, text string) string {
	t.Helper()
	ts, ev := e.fs.PostQuiet(e.ch, e.a.Core().Self().ID, text)
	b, _ := json.Marshal(ev)
	chJSON, _ := json.Marshal(e.ch)
	e.a.data.core.Inject(slack.Event{Type: "message", Channel: chJSON, Raw: b})
	e.until("the message in core", 10*time.Second, func() bool {
		if tl := e.a.data.core.S.Timeline(e.ch); tl != nil {
			for _, m := range tl.Msgs {
				if m.TS == ts {
					return true
				}
			}
		}
		return false
	})
	return ts
}

// sameIn reports that a and b have the same pixels in r.
func sameIn(a, b *image.RGBA, r image.Rectangle) bool {
	r = r.Intersect(a.Bounds()).Intersect(b.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i, j := a.PixOffset(r.Min.X, y), b.PixOffset(r.Min.X, y)
		if !bytes.Equal(a.Pix[i:i+4*r.Dx()], b.Pix[j:j+4*r.Dx()]) {
			return false
		}
	}
	return !r.Empty()
}

// freezeShots (TestInspectShots, through the real headless capture): the
// window frozen with the pointer on the pill, then with the pointer moved
// to the sidebar, where its message's hover toolbar is still drawn
// (unfrozen there is none), the same pixels after a message lands, and
// the image a Shift+click on the toolbar's button copies from the frozen
// frame.
func (e *inspectEnv) freezeShots(t *testing.T, mode, dir string, save func(string, image.Image)) {
	t.Helper()
	win := image.Rectangle{Max: e.size}
	shoot := func(what string, ops *op.Ops) *image.RGBA {
		t.Helper()
		img, err := inspect.Shoot(ops.Whole(), win, 1)
		if err != nil {
			t.Fatalf("%s, %s: the headless capture: %v", mode, what, err)
		}
		return img
	}
	e.startSession(t)
	tb, ok := e.toolbar(t)
	if !ok {
		t.Fatalf("%s: no hover toolbar over the pill's message", mode)
	}
	away := e.sidebarPoint()
	e.move(away)
	e.frame()
	live := shoot("unfrozen, the pointer in the sidebar", &e.ops)
	e.move(e.pillCenter())
	e.frame()
	e.freezeKey()
	if !e.a.insp.Frozen() || e.frozen == nil {
		t.Fatalf("%s: F didn't freeze", mode)
	}
	save("inspect-"+mode+"-frozen", shoot("frozen", e.frozen))
	e.move(away)
	frozenAway := shoot("frozen, the pointer in the sidebar", e.frozen)
	save("inspect-"+mode+"-frozen-away", frozenAway)
	if sameIn(live, frozenAway, tb) {
		t.Errorf("%s: frozen with the pointer in the sidebar, the toolbar's place %v is drawn as unfrozen, with no toolbar", mode, tb)
	}
	e.post(t, "a message while the window is frozen")
	e.settle(300 * time.Millisecond)
	if again := shoot("frozen, a message landed", e.frozen); !bytes.Equal(again.Pix, frozenAway.Pix) {
		save("inspect-"+mode+"-frozen-changed", again)
		t.Errorf("%s: the frozen window changed when a message landed", mode)
	}
	more := image.Pt(tb.Max.X-tb.Dy()/2, tb.Min.Y+tb.Dy()/2)
	e.move(f32.Pt(float32(more.X), float32(more.Y)))
	e.pickAndCopy(t, more)
	if e.a.insp.Active() || e.a.insp.Frozen() {
		t.Fatalf("%s: after the Shift+click: session %v, frozen %v", mode, e.a.insp.Active(), e.a.insp.Frozen())
	}
	if err := os.WriteFile(filepath.Join(dir, "inspect-"+mode+"-frozen-report.png"), e.newestReportPNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond) // the next report's file name (seconds) differs
}
