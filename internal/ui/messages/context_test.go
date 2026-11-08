package messages

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/ctxclick"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

const (
	tsCtxLink = "1700000010.000100"
	tsCtxFile = "1700000011.000100"
)

// ctxHost records the overlays the list shows.
type ctxHost struct {
	coreHost
	overlays []popups.Overlay
}

func (h *ctxHost) ShowOverlay(o popups.Overlay) { h.overlays = append(h.overlays, o) }

// menus are the menus shown (other overlays, like popups' async pump,
// are skipped).
func (h *ctxHost) menus() []*popups.Menu {
	var out []*popups.Menu
	for _, o := range h.overlays {
		if m, ok := o.(*popups.Menu); ok {
			out = append(out, m)
		}
	}
	return out
}

func labels(m *popups.Menu) []string {
	var out []string
	for _, it := range m.Items() {
		if it.Separator {
			out = append(out, "-")
			continue
		}
		out = append(out, it.Label)
	}
	return out
}

// ctxEnv is a channel view over a real input.Router with a link, a
// mention, a channel link and a file in synthetic messages.
type ctxEnv struct {
	t     *testing.T
	h     *ctxHost
	v     *ChannelView
	r     input.Router
	ops   op.Ops
	now   time.Time
	clock time.Duration
}

func newCtxEnv(t *testing.T) *ctxEnv {
	t.Helper()
	app, _ := newTestApp(t)
	app.S.SetLatest(tGen, []slack.Message{
		{Type: "message", TS: tsCtxLink, User: "UALICE", Text: "see <https://example.com/doc|the doc> and ask <@UBOB> in <#" + tGen + "> today please"},
		{Type: "message", TS: tsCtxFile, User: "UBOB", Files: []slack.File{{
			ID: "F0CTX1", Name: "report.pdf", Title: "report.pdf", Mimetype: "application/pdf", Filetype: "pdf", PrettyType: "PDF", Size: 2048,
			URLPrivate:         "https://files.slack.com/files-pri/T0266FRGM-F0CTX1/report.pdf",
			URLPrivateDownload: "https://files.slack.com/files-pri/T0266FRGM-F0CTX1/download/report.pdf",
			Permalink:          "https://hackclub.slack.com/files/UBOB/F0CTX1/report.pdf",
		}}},
	}, false)
	e := &ctxEnv{t: t, h: &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}, now: time.Now()}
	e.v = NewChannelView(e.h, tGen)
	e.v.Preview = true
	until(t, 5*time.Second, "both rows laid out", e.frame, func() bool {
		return e.row(tsCtxLink) != nil && e.row(tsCtxFile) != nil
	})
	for k := 0; k < 3; k++ {
		e.frame()
	}
	prev := openURL
	t.Cleanup(func() { openURL = prev })
	return e
}

func (e *ctxEnv) frame() {
	e.ops.Reset()
	e.now = e.now.Add(20 * time.Millisecond)
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(900, 700)), Now: e.now, Source: e.r.Source()}
	e.v.Layout(gtx)
	e.r.Frame(&e.ops)
}

// row is ts's laid-out, visible row.
func (e *ctxEnv) row(ts string) *row {
	s := e.v.s
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].key == ts && s.heights[i] > 0 {
			return s.rows[ts]
		}
	}
	return nil
}

// find is the first point (view coordinates) of ts's body where ok holds
// for the richview hit there.
func (e *ctxEnv) find(ts string, what string, ok func(richview.Hit) bool) f32.Point {
	e.t.Helper()
	s := e.v.s
	r := e.row(ts)
	if r == nil {
		e.t.Fatalf("row %s isn't on screen", ts)
	}
	top := s.pos[s.sel.keyIdx[ts]]
	for y := 1; y < r.bodyRect.Dy(); y += 2 {
		for x := 1; x < r.bodyRect.Dx(); x += 2 {
			p := f32.Pt(float32(x)+.5, float32(y)+.5)
			if ok(r.rv.HitAt(p)) {
				return p.Add(f32.Pt(float32(r.bodyRect.Min.X), float32(top+r.bodyRect.Min.Y)))
			}
		}
	}
	e.t.Fatalf("no %s in row %s", what, ts)
	return f32.Point{}
}

// press presses b (with mods) at p and releases it.
func (e *ctxEnv) press(p f32.Point, b pointer.Buttons, mods key.Modifiers) {
	e.clock += time.Second
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: e.clock},
		pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: b, Modifiers: mods, Time: e.clock + 10*time.Millisecond},
		pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Modifiers: mods, Time: e.clock + 60*time.Millisecond},
	)
	for k := 0; k < 3; k++ {
		e.frame()
	}
}

func (e *ctxEnv) rightClick(p f32.Point) { e.press(p, pointer.ButtonSecondary, 0) }

// menu is the only menu shown since n menus, or fails.
func (e *ctxEnv) menu(n int, what string) *popups.Menu {
	e.t.Helper()
	ms := e.h.menus()
	if len(ms) != n+1 {
		e.t.Fatalf("%s: %d menus opened, want 1", what, len(ms)-n)
	}
	return ms[n]
}

func hasLabel(m *popups.Menu, l string) bool {
	for _, s := range labels(m) {
		if s == l {
			return true
		}
	}
	return false
}

var (
	onText = func(h richview.Hit) bool { return h.Kind == richview.HitNone && h.Text && h.EmojiURL == "" }
	onLink = func(h richview.Hit) bool { return h.Kind == richview.HitLink }
	onUser = func(h richview.Hit) bool { return h.Kind == richview.HitUser && h.ID == "UBOB" }
	onChan = func(h richview.Hit) bool { return h.Kind == richview.HitChannel }
	onFile = func(h richview.Hit) bool { return h.Kind == richview.HitFile }
)

// A secondary press anywhere on a message opens Slack's message actions
// context menu: the hover toolbar's group, then More actions.
func TestContextMenuOnMessageBody(t *testing.T) {
	e := newCtxEnv(t)
	e.rightClick(e.find(tsCtxLink, "plain text", onText))
	m := e.menu(0, "right-click on the body text")
	got := labels(m)
	want := []string{"Add reaction…", "Reply in thread", "Forward message…", "Add to-do", "-"}
	if len(got) < len(want) || strings.Join(got[:len(want)], "|") != strings.Join(want, "|") {
		t.Fatalf("menu starts %q, want %q", got, want)
	}
	for _, l := range []string{"Mark unread", "Copy link", "Copy message"} {
		if !hasLabel(m, l) {
			t.Errorf("no %q in %q", l, got)
		}
	}
	if e.v.s.sel.active {
		t.Error("the right-click started a selection")
	}
}

// On a link: the desktop shell's link menu (Copy Link, Open Link); Open
// Link opens it like a click. A plain click on the link still opens it
// and shows no menu.
func TestContextMenuOnLink(t *testing.T) {
	e := newCtxEnv(t)
	var opened []string
	openURL = func(u string) { opened = append(opened, u) }
	p := e.find(tsCtxLink, "a link", onLink)
	e.rightClick(p)
	m := e.menu(0, "right-click on a link")
	if got := labels(m); strings.Join(got, "|") != "Copy Link|Open Link" {
		t.Fatalf("link menu %q", got)
	}
	if len(opened) != 0 {
		t.Fatalf("the right-click opened %q", opened)
	}
	m.Items()[1].Do()
	if len(opened) != 1 || opened[0] != "https://example.com/doc" {
		t.Fatalf("Open Link opened %q", opened)
	}
	e.press(p, pointer.ButtonPrimary, 0)
	if len(opened) != 2 {
		t.Errorf("a click on the link opened %q (want it opened again)", opened)
	}
	if n := len(e.h.menus()); n != 1 {
		t.Errorf("the click opened a menu (%d menus)", n)
	}
}

// A user mention opens the member menu, a channel link the channel menu.
func TestContextMenuOnMentions(t *testing.T) {
	e := newCtxEnv(t)
	e.rightClick(e.find(tsCtxLink, "a user mention", onUser))
	if m := e.menu(0, "right-click on @bob"); !hasLabel(m, "Copy member ID") {
		t.Errorf("mention menu %q, want the member menu", labels(m))
	}
	e.rightClick(e.find(tsCtxLink, "a channel link", onChan))
	if m := e.menu(1, "right-click on #general"); !hasLabel(m, "Copy") || hasLabel(m, "Add reaction…") {
		t.Errorf("channel link menu %q, want the channel menu", labels(m))
	}
}

// A file card opens Slack's file menu.
func TestContextMenuOnFile(t *testing.T) {
	e := newCtxEnv(t)
	e.rightClick(e.find(tsCtxFile, "the file card", onFile))
	m := e.menu(0, "right-click on a file")
	if got := strings.Join(labels(m), "|"); got != "Share file…|Copy link|Download" {
		t.Errorf("file menu %q", got)
	}
}

// With text selected, a right-click on the message opens the text
// selection menu and keeps the selection.
func TestContextMenuOnSelection(t *testing.T) {
	e := newCtxEnv(t)
	a := e.find(tsCtxLink, "plain text", onText)
	b := a.Add(f32.Pt(60, 0))
	e.clock += time.Second
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Position: a, Source: pointer.Mouse, Time: e.clock},
		pointer.Event{Kind: pointer.Press, Position: a, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: e.clock + 10*time.Millisecond},
		pointer.Event{Kind: pointer.Move, Position: a.Add(f32.Pt(20, 0)), Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: e.clock + 30*time.Millisecond},
		pointer.Event{Kind: pointer.Move, Position: b, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: e.clock + 50*time.Millisecond},
		pointer.Event{Kind: pointer.Release, Position: b, Source: pointer.Mouse, Time: e.clock + 70*time.Millisecond},
	)
	for k := 0; k < 3; k++ {
		e.frame()
	}
	sel := e.v.s.selectedText(e.h)
	if sel == "" {
		t.Fatal("the drag selected nothing")
	}
	e.rightClick(b)
	m := e.menu(0, "right-click on the selection")
	if got := strings.Join(labels(m), "|"); got != "Copy text|Search in Slack" {
		t.Errorf("selection menu %q", got)
	}
	if got := e.v.s.selectedText(e.h); got != sel {
		t.Errorf("the right-click changed the selection from %q to %q", sel, got)
	}
}

// Right-clicking the sender's name opens only the member menu.
func TestContextMenuOnSenderName(t *testing.T) {
	e := newCtxEnv(t)
	s := e.v.s
	i := s.sel.keyIdx[tsCtxLink]
	for _, dy := range []int{8, 12, 16, 20} {
		n := len(e.h.menus())
		e.rightClick(f32.Pt(70, float32(s.pos[i]+dy)))
		ms := e.h.menus()[n:]
		if len(ms) > 1 {
			t.Fatalf("one right-click opened %d menus: %q and %q", len(ms), labels(ms[0]), labels(ms[1]))
		}
		if len(ms) == 1 && hasLabel(ms[0], "Copy member ID") {
			return
		}
	}
	t.Fatal("no menu from a right-click on the sender's name")
}

// macOS: Ctrl+click is a right-click. It opens the menus and doesn't
// select text or open the link.
func TestContextMenuMacCtrlClick(t *testing.T) {
	prev := ctxclick.Mac
	ctxclick.Mac = true
	t.Cleanup(func() { ctxclick.Mac = prev })
	e := newCtxEnv(t)
	var opened []string
	openURL = func(u string) { opened = append(opened, u) }

	e.press(e.find(tsCtxLink, "plain text", onText), pointer.ButtonPrimary, key.ModCtrl)
	if m := e.menu(0, "Ctrl+click on the body"); !hasLabel(m, "Add reaction…") {
		t.Errorf("Ctrl+click menu %q, want the message menu", labels(m))
	}
	if e.v.s.sel.active {
		t.Error("Ctrl+click selected text")
	}
	e.press(e.find(tsCtxLink, "a link", onLink), pointer.ButtonPrimary, key.ModCtrl)
	if m := e.menu(1, "Ctrl+click on a link"); !hasLabel(m, "Copy Link") {
		t.Errorf("Ctrl+click on a link: %q", labels(m))
	}
	if len(opened) != 0 {
		t.Errorf("Ctrl+click opened the link %q", opened)
	}
	e.press(e.find(tsCtxFile, "the file card", onFile), pointer.ButtonPrimary, key.ModCtrl)
	if m := e.menu(2, "Ctrl+click on a file"); !hasLabel(m, "Download") {
		t.Errorf("Ctrl+click on a file: %q", labels(m))
	}
}
