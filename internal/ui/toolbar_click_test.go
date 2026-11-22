package ui

// The message actions bar clicked the way a hand clicks it: the press and
// the release ~100 ms apart, in frames of their own, the pointer resting
// on the button after. Karim (2026-11-22, α.136 on Windows): "i hover
// over this icon, i click and it disappears as if you are not hovering
// it", and under their plugins (MessageActions and
// MessageActionButtonReaction patched) Reply in thread didn't open the
// thread at all.
//
// Slack's row hover is MessageHover (client-boot-shared mDIZ): a
// mouseover/mouseout pair on the row (relatedTarget outside it), a
// MutationObserver that keeps the hover when a re-render replaces the
// hovered element while the row still matches :hover, and a hold count
// (setHover) that MessageActions raises while a popover it opened is up
// (RwAt setPopoverHold: More actions, the emoji picker, quick share) and
// drops when the popover closes. So a click on a button leaves the row
// hovered and its bar shown while the pointer stays, and while the emoji
// picker or the More actions menu it opened is up; a modal (Forward, Add
// to list) covers the row until it closes, then the pointer is on the bar
// again.
//
// Each mode runs on fakeslack's thread header fixtures (#th-launch-notes,
// a root with two replies; synthetic, nothing talks to Slack): no plugin;
// a plugin wrapping both components (OwlMode's shape); one whose wrapper
// renders again on every mousedown and mouseup, so a new tree from JS
// lands between each press and its release; and Taut's OwlMode itself
// (TAUT_PLUGINS_DIR), which Karim runs.

import (
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// tbClickPlugin wraps MessageActions and MessageActionButtonReaction;
// with rerender its wrapper (a display: contents div) renders again on
// every mousedown and mouseup. __tbRenders counts the wrappers' renders.
func tbClickPlugin(id string, rerender bool) string {
	return fmt.Sprintf(`(() => {
const TautPlugin = globalThis.TautPlugin
class P extends TautPlugin {
  static id = %q
  static pluginName = %q
  static description = 'test: wraps the message actions bar and its Add reaction button'
  static category = 'other'
  static authors = ['test']
  static defaultConfig = { enabled: false }
  start() {
    const rerender = %v
    globalThis.__tbRenders = 0
    const wrap = (Original) => (props) => {
      const [n, setN] = React.useState(0)
      globalThis.__tbRenders++
      if (!rerender) return React.createElement(Original, props)
      const bump = () => setN((x) => x + 1)
      return React.createElement('div', { style: { display: 'contents' }, 'data-tb-n': String(n), onMouseDown: bump, onMouseUp: bump },
        React.createElement(Original, props))
    }
    this.api.patchComponent('MessageActions', wrap)
    this.api.patchComponent('MessageActionButtonReaction', wrap)
  }
  stop() {}
}
return P
})()`, id, id, rerender)
}

// tcEnv is rfEnv with the bar's plugin (nil renders: none).
type tcEnv struct {
	*rfEnv
	renders func() int
}

func newTCEnv(t *testing.T, mode string) *tcEnv {
	t.Helper()
	// the plugin runtime's state (and its plugin ids) stay in the test's
	// own home
	t.Setenv("CLACK_HOME", t.TempDir())
	owl := ""
	if mode == "owl" {
		dir := os.Getenv("TAUT_PLUGINS_DIR")
		if dir == "" {
			t.Skip("TAUT_PLUGINS_DIR not set: flake-check's tb-click job builds Taut's OwlMode and runs this")
		}
		owl = filepath.Join(dir, "OwlMode.js")
	}
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	r := &rfEnv{t: t, ch: fakeslack.ThreadPublicID, root: fakeslack.ThreadRootTS, clk: time.Second}
	// messages after the root, each a row of its own with its own bar
	users := fs.UserIDs()[1:4]
	for n := 0; n < 3; n++ {
		fs.PostQuiet(r.ch, users[n%len(users)], fmt.Sprintf("a synthetic message after the root %d", n))
	}
	creds := fs.Creds()
	if owl != "" {
		creds.EnterpriseID = "E09V59WQY1E" // taut.HackClubIDs: OwlMode is hackClubOnly
	}
	r.perfEnv = startEnvCreds(t, fs, t.TempDir(), creds)
	t.Cleanup(func() {
		if r.a.data != nil {
			r.a.data.stopTaut()
		}
	})
	r.waitReady()
	r.openChannel(r.ch)
	r.until("history", 20*time.Second, func() bool { return r.loadedMessages(r.ch) > 3 })
	r.until("the root on screen", 15*time.Second, func() bool { _, ok := r.row(r.root); return ok })
	r.settle(300 * time.Millisecond)
	x := &tcEnv{rfEnv: r}
	if mode == "native" {
		return x
	}
	x.until("Taut", 60*time.Second, func() bool { return x.a.Taut() != nil })
	tt := x.a.Taut()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	code := []byte(tbClickPlugin("TbClick"+strings.ToUpper(mode[:1])+mode[1:], mode == "rerender"))
	if owl != "" { // Karim's: Taut's OwlMode, which patches both
		if code, err = os.ReadFile(owl); err != nil {
			t.Fatal(err)
		}
	}
	got, err := tt.Import(ctx, code, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, got, true); err != nil {
		t.Fatal(err)
	}
	x.until("the bar's patches", 30*time.Second, func() bool {
		return slackc.Patched("MessageActions") && slackc.Patched("MessageActionButtonReaction")
	})
	if owl != "" {
		x.settle(300 * time.Millisecond)
		return x
	}
	x.renders = func() int {
		rt, err := tt.JS()
		if err != nil || rt == nil {
			return -1
		}
		v, err := rt.Eval("tb-renders.js", "globalThis.__tbRenders|0")
		if err != nil {
			return -1
		}
		var n int
		fmt.Sscan(fmt.Sprint(v), &n)
		return n
	}
	x.settle(300 * time.Millisecond)
	return x
}

// toolbarLister is a message list whose hover toolbar the test reads.
type toolbarLister interface {
	rowsLister
	ToolbarRect() (string, image.Rectangle, bool)
}

// barButton is the bar's k-th button from its right end (1: More
// actions), its middle in window px: the card is 32px buttons 4px in.
func barButton(tb image.Rectangle, k int) image.Point {
	const pad, bs = 4, 32
	return image.Pt(tb.Max.X-pad-(k-1)*bs-bs/2, (tb.Min.Y+tb.Max.Y)/2)
}

// The buttons a channel root's bar has, from the right (prepareButtons:
// More actions, Add to list…, Add to-do, Forward message…, Reply in
// thread, Add reaction…, then the quick reactions).
const (
	barMore = 1 + iota
	barList
	barTodo
	barForward
	barReply
	barReact
)

// hoverBar rests the pointer on row ts of l away from the bar, then on
// the bar's k-th button from the right, and returns that point.
func (x *tcEnv) hoverBar(l func() toolbarLister, ts string, k int) image.Point {
	x.t.Helper()
	var r image.Rectangle
	for _, rr := range l().RowRects() {
		if rr.TS == ts {
			r = rr.R
		}
	}
	if r.Empty() {
		x.t.Fatalf("row %s isn't on screen", ts)
	}
	in := rowPoint(r, l().ListRect())
	x.move(f32.Pt(float32(in.X), float32(in.Y)))
	x.settle(300 * time.Millisecond)
	tts, tb, ok := l().ToolbarRect()
	if !ok || tts != ts {
		x.t.Fatalf("hovering row %s at %v drew the bar over %q (%v)", ts, in, tts, ok)
	}
	p := barButton(tb, k)
	x.move(f32.Pt(float32(p.X), float32(p.Y)))
	x.settle(200 * time.Millisecond)
	return p
}

// handClick clicks at p as a hand does: the press in one frame, frames
// for ~100 ms (the plugins' JS answers what the press sent it), the
// release in another, the pointer left there. It reports how many times
// the bar's plugin wrappers rendered while the button was down (-1: no
// plugin).
func (x *tcEnv) handClick(p image.Point) int {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	at := x.tick()
	x.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Time: at})
	x.frame()
	before := -1
	if x.renders != nil {
		before = x.renders()
	}
	x.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt, Time: at + 10*time.Millisecond})
	x.frame()
	for dl := time.Now().Add(100 * time.Millisecond); time.Now().Before(dl); {
		x.frame()
		time.Sleep(5 * time.Millisecond)
	}
	down := -1
	if x.renders != nil {
		down = x.renders() - before
	}
	x.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Time: at + 110*time.Millisecond})
	x.frame()
	x.frame()
	return down
}

// openOverlay reports an open overlay of o's kind (popups' async pump,
// an invisible overlay, may sit above it).
func (x *tcEnv) openOverlay(is func(Overlay) bool) bool {
	for _, o := range x.a.overlays {
		if o != nil && !o.Dismissed() && !invisible(o) && is(o) {
			return true
		}
	}
	return false
}

func (x *tcEnv) noOverlay() bool {
	return !x.openOverlay(func(Overlay) bool { return true })
}

// wantBar checks that l draws row ts hovered, its bar shown, with p on
// the bar or the row (a row that grew, a to-do's label added over it,
// takes its bar along).
func (x *tcEnv) wantBar(l toolbarLister, what, ts string, p image.Point) {
	x.t.Helper()
	tts, tb, ok := l.ToolbarRect()
	var row image.Rectangle
	for _, r := range l.RowRects() {
		if r.TS == ts {
			row = r.R
		}
	}
	switch {
	case !ok:
		x.t.Errorf("%s: the bar went, the pointer still on it at %v (Slack's row stays hovered)", what, p)
	case tts != ts:
		x.t.Errorf("%s: the bar is over %q, want the row it was clicked on (%s)", what, tts, ts)
	case !p.In(tb) && !p.In(row):
		x.t.Errorf("%s: the bar %v and its row %v moved off the pointer at %v", what, tb, row, p)
	}
}

func TestToolbarClicksKeepHover(t *testing.T) {
	for _, mode := range []string{"native", "patched", "rerender", "owl"} {
		t.Run(mode, func(t *testing.T) { toolbarClicks(t, mode) })
	}
}

func toolbarClicks(t *testing.T, mode string) {
	x := newTCEnv(t, mode)
	ch, root := x.ch, x.root
	cl := func() toolbarLister { return x.cv() }
	note := func(what string, down int) {
		if x.renders == nil {
			return
		}
		t.Logf("%s: the bar's wrappers rendered %d times while the button was down", what, down)
		if mode == "rerender" && down <= 0 {
			t.Errorf("%s: the plugin didn't render again while the button was down: the test proves nothing", what)
		}
	}

	// Add to-do: nothing opens; its toast says the click ran
	p := x.hoverBar(cl, root, barTodo)
	x.a.toast = toastState{}
	note("Add to-do", x.handClick(p))
	x.until("Add to-do's toast (the click ran)", 8*time.Second, func() bool { return x.a.toast.text != "" })
	x.settle(300 * time.Millisecond)
	x.wantBar(cl(), "after Add to-do", root, p)

	// Add reaction…: the picker opens and holds the row's hover
	p = x.hoverBar(cl, root, barReact)
	note("Add reaction", x.handClick(p))
	x.until("the emoji picker (the click ran)", 8*time.Second, func() bool {
		return x.openOverlay(func(o Overlay) bool { _, ok := o.(*popups.EmojiPicker); return ok })
	})
	x.settle(300 * time.Millisecond)
	x.wantBar(cl(), "with the emoji picker open", root, p)
	x.key(key.NameEscape, 0)
	x.until("the picker closed", 5*time.Second, x.noOverlay)
	x.settle(300 * time.Millisecond)
	x.wantBar(cl(), "after the emoji picker closed", root, p)

	// More actions: the menu opens and holds the row's hover
	p = x.hoverBar(cl, root, barMore)
	note("More actions", x.handClick(p))
	x.until("the More actions menu (the click ran)", 8*time.Second, func() bool { return x.topMenu() != nil })
	x.settle(300 * time.Millisecond)
	x.wantBar(cl(), "with the More actions menu open", root, p)
	x.key(key.NameEscape, 0)
	x.until("the menu closed", 5*time.Second, x.noOverlay)
	x.settle(300 * time.Millisecond)
	x.wantBar(cl(), "after the More actions menu closed", root, p)

	// Forward message… and Add to list…: modals, the bar back over the
	// row under the pointer once they close
	for _, b := range []struct {
		what string
		k    int
	}{{"Forward message", barForward}, {"Add to list", barList}} {
		p = x.hoverBar(cl, root, b.k)
		note(b.what, x.handClick(p))
		x.until(b.what+"'s modal (the click ran)", 8*time.Second, func() bool { return !x.noOverlay() })
		x.settle(300 * time.Millisecond)
		x.key(key.NameEscape, 0)
		x.until(b.what+"'s modal closed", 5*time.Second, x.noOverlay)
		x.settle(300 * time.Millisecond)
		x.wantBar(cl(), "after "+b.what+"'s modal closed", root, p)
	}

	// Reply in thread: the thread opens in the side pane
	p = x.hoverBar(cl, root, barReply)
	note("Reply in thread", x.handClick(p))
	x.waitThreadBox("the thread pane (Reply in thread's click ran)")
	x.settle(300 * time.Millisecond)
	// with the pane open the bar sits left of it: Reply in thread on
	// another message opens that one's thread in the pane, and the row
	// keeps its hover
	other := ""
	for _, r := range cl().RowRects() {
		if r.TS != root && r.R.Min.Y > cl().ListRect().Min.Y+60 {
			other = r.TS
			break
		}
	}
	if other == "" {
		t.Fatal("no other message on screen in the channel")
	}
	p = x.hoverBar(cl, other, barReply)
	note("Reply in thread, pane open", x.handClick(p))
	x.until("the pane on the other message's thread (Reply in thread's click ran)", 8*time.Second, func() bool {
		s := x.a.secondary
		return s.kind == secThread && !s.hidden && s.channel == ch && s.ts == other
	})
	x.settle(300 * time.Millisecond)
	x.wantBar(cl(), "after Reply in thread with the pane open", other, p)
	// back to the root's thread for the pane's own rows
	x.a.OpenThread(ch, root)
	x.waitThreadBox("the root's thread again")
	x.settle(300 * time.Millisecond)

	// in the thread pane: a reply's Add to-do keeps its row hovered
	tl := func() toolbarLister { return x.threadView(ch, root) }
	reply := ""
	for _, r := range tl().RowRects() {
		if r.TS != root {
			reply = r.TS
			break
		}
	}
	if reply == "" {
		t.Fatal("no reply drawn in the thread pane")
	}
	p = x.hoverBar(tl, reply, barTodo)
	x.a.toast = toastState{}
	note("Add to-do in the thread pane", x.handClick(p))
	x.until("Add to-do's toast in the thread pane (the click ran)", 8*time.Second, func() bool { return x.a.toast.text != "" })
	x.settle(300 * time.Millisecond)
	x.wantBar(tl(), "after Add to-do in the thread pane", reply, p)
}
