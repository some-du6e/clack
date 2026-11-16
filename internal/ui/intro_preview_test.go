package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/demo"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// waitReal draws frames without moving the virtual clock (the beat stays
// where it is) until ok, for at most d of real time: the preview's demo
// session, history and images arrive on their own goroutines.
func waitReal(t *testing.T, e *introEnv, what string, d time.Duration, ok func() bool) {
	t.Helper()
	dl := time.Now().Add(d)
	for !ok() {
		if time.Now().After(dl) {
			t.Fatalf("timed out after %v waiting for %s", d, what)
		}
		e.frame(0)
		time.Sleep(5 * time.Millisecond)
	}
}

// settleReal draws frames on a held clock for d of real time.
func settleReal(e *introEnv, d time.Duration) {
	for dl := time.Now().Add(d); time.Now().Before(dl); {
		e.frame(0)
		time.Sleep(5 * time.Millisecond)
	}
}

// writeShot saves img (cropped to r unless r is empty) as dir/name.png.
func writeShot(t *testing.T, dir, name string, img *image.RGBA, r image.Rectangle) {
	t.Helper()
	var out image.Image = img
	if !r.Empty() {
		out = img.SubImage(r.Intersect(img.Bounds()))
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, out); err != nil {
		t.Fatal(err)
	}
}

// "It's your Slack, 1:1." shows the real app: a second App over the demo
// workspace, laid out in the beat's card in the window's theme, with the
// sidebar's sections, #general's header, messages and composer, and a
// thread in the secondary pane. Nothing reaches it: clicks on the card,
// keys and typed text change nothing in it or in the window, and neither
// the demo workspace nor the signed-in account is sent anything. It is
// laid out only while the beat shows, and goes with the introduction.
// With LOGIN_SHOTS_DIR set (the login-shots workflow) the beat is saved,
// light and dark: intro-slack-<mode>.png and its card, -card.png.
func TestIntroSlackPreview(t *testing.T) {
	dir := os.Getenv("LOGIN_SHOTS_DIR")
	sz := image.Pt(1440, 900)
	var win *headless.Window
	if dir != "" {
		w, err := pixeltest.NewWindow(sz.X, sz.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		defer w.Release()
		win = w
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		t.Run(mode, func(t *testing.T) {
			a, _, hostFS := fakePrefsApp(t)
			a.motionOverride = -1
			if dark {
				a.th.SetDark(true)
			}
			e := newIntroEnv(t, a, sz)
			e.run(200 * time.Millisecond)
			a.replayIntro()
			e.frame(time.Second / 60)
			e.started()
			in := a.intro
			p := in.preview
			if p == nil {
				t.Fatal("the replay has no preview")
			}
			waitReal(t, e, "the preview's sidebar", 60*time.Second, p.ready)
			pa := p.app
			e.key(key.NameRightArrow)
			e.key(key.NameRightArrow)
			if in.cur != beatSlack {
				t.Fatalf("beat %d, want your Slack (%d)", in.cur, beatSlack)
			}
			waitReal(t, e, "#general and its thread in the preview", 30*time.Second, func() bool {
				c := pa.convos[demo.GeneralID+"/"]
				return c != nil && c.list != nil && c.comp != nil && p.thread && pa.secondary.kind == secThread
			})
			settleReal(e, 1500*time.Millisecond) // the header's reads, avatars, the thread's replies
			e.run(1200 * time.Millisecond)       // the card has risen, the preview faded in

			// one frame, recorded: the card and the preview fully there, and
			// the real components' text among what was drawn
			var rec []introDrawn
			in.rec = &rec
			var mu sync.Mutex
			drawn := map[string]bool{}
			prevHook := theme.DrawHook
			theme.DrawHook = func(s string) { mu.Lock(); drawn[s] = true; mu.Unlock() }
			seq := pa.frameSeq.Load()
			e.frame(time.Second / 60)
			theme.DrawHook = prevHook
			in.rec = nil
			if pa.frameSeq.Load() == seq {
				t.Fatal("the beat on screen didn't lay the preview out")
			}
			full := false
			for _, d := range rec {
				if d.name == "slack-preview" && d.a > 0.99 {
					full = true
				}
			}
			if !full {
				t.Errorf("the preview isn't fully drawn: %+v", rec)
			}
			var strs []string
			for s := range drawn {
				strs = append(strs, s)
			}
			sort.Strings(strs)
			t.Logf("drawn: %q", strs)
			for _, want := range []string{demo.TeamName, "general"} {
				if !drawn[want] {
					t.Errorf("%q isn't drawn in the preview", want)
				}
			}
			m := pa.model()
			if len(m.Sections) < 2 || m.TeamName != demo.TeamName {
				t.Errorf("the preview's sidebar: team %q, %d sections", m.TeamName, len(m.Sections))
			}
			if pa.sidebar.rowCount == 0 {
				t.Error("the preview's sidebar drew no rows")
			}
			conv := pa.convos[demo.GeneralID+"/"]
			comp, ok := conv.comp.(composerBox)
			if !ok {
				t.Fatalf("#general's composer is a %T, not the real composer", conv.comp)
			}
			if _, ok := conv.list.(channelList); !ok {
				t.Errorf("#general's list is a %T, not the real message list", conv.list)
			}
			if win != nil {
				e.win = win
				e.frame(time.Second / 60)
				e.win = nil
				writeShot(t, dir, "intro-slack-"+mode, e.img, image.Rectangle{})
				writeShot(t, dir, "intro-slack-"+mode+"-card", e.img, p.card)
			}

			// inert: clicks on the composer, a message, a sidebar row and the
			// thread; then keys and typed text
			card := p.card
			if card.Empty() {
				t.Fatal("no card was drawn")
			}
			route, sec := pa.router.Current(), pa.secondary.kind
			pts := []image.Point{
				{card.Min.X + card.Dx()*45/100, card.Max.Y - card.Dy()/14}, // the composer
				{card.Min.X + card.Dx()*45/100, card.Min.Y + card.Dy()/2},  // a message
				{card.Min.X + card.Dx()*12/100, card.Min.Y + card.Dy()/2},  // a sidebar row
				{card.Max.X - card.Dx()/8, card.Min.Y + card.Dy()/3},       // the thread
			}
			for _, pt := range pts {
				fp := f32.Pt(float32(pt.X), float32(pt.Y))
				e.r.Queue(
					pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: fp},
					pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: fp},
					pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: fp},
				)
				e.frame(time.Second / 60)
				e.frame(time.Second / 60)
			}
			e.r.Queue(key.EditEvent{Text: "hello from the introduction"})
			e.r.Queue(key.Event{Name: "K", Modifiers: key.ModShortcut, State: key.Press}, key.Event{Name: "K", Modifiers: key.ModShortcut, State: key.Release})
			e.frame(time.Second / 60)
			e.frame(time.Second / 60)
			if got := strings.TrimSpace(comp.DocText()); got != "" {
				t.Errorf("the preview's composer took text: %q", got)
			}
			if len(pa.overlays) != 0 || len(a.overlays) != 0 {
				t.Errorf("input opened overlays: %d in the preview, %d in the window", len(pa.overlays), len(a.overlays))
			}
			if pa.router.Current() != route || pa.secondary.kind != sec {
				t.Errorf("input moved the preview: route %q → %q, secondary %v → %v", route, pa.router.Current(), sec, pa.secondary.kind)
			}
			if in.cur != beatSlack {
				t.Errorf("the clicks moved the introduction to beat %d", in.cur)
			}
			writes := []string{"chat.postMessage", "chat.meMessage", "chat.update", "chat.delete", "chat.scheduleMessage",
				"reactions.add", "reactions.remove", "drafts.create", "drafts.update", "drafts.delete",
				"conversations.join", "conversations.leave", "files.upload", "files.completeUploadExternal", "pins.add", "stars.add"}
			for _, w := range writes {
				if n := p.sess.FS.CallCount(w); n != 0 {
					t.Errorf("the preview sent the demo workspace %s ×%d", w, n)
				}
				if n := hostFS.CallCount(w); n != 0 {
					t.Errorf("the account was sent %s ×%d", w, n)
				}
			}

			// off screen: no more layouts
			e.key(key.NameRightArrow)
			e.run(1200 * time.Millisecond)
			seq = pa.frameSeq.Load()
			e.run(600 * time.Millisecond)
			settleReal(e, 400*time.Millisecond)
			if n := pa.frameSeq.Load() - seq; n != 0 {
				t.Errorf("the preview was laid out %d times off its beat", n)
			}

			// the window's hooks are its own again, and the preview goes with
			// the introduction
			if f := richview.SwapMemberRead(nil); f != nil {
				t.Error("the preview's member read stayed installed")
			}
			if tt := messages.SwapTaut(nil); tt != nil {
				t.Error("a plugin system appeared in the message lists")
			}
			e.key(key.NameEscape)
			e.run(1500 * time.Millisecond)
			e.key(key.NameReturn)
			e.run(time.Second)
			if a.intro != nil {
				t.Fatal("the introduction is still up")
			}
			if !p.closed || p.app != nil {
				t.Error("the preview outlived the introduction")
			}
		})
	}
}
