package ui

// StreamerMode (Slick's and Taut's) through the whole native window: its
// blur rules target the classes and attributes Clack's natives carry, and
// the pixels show it. A private channel's and a group DM's sidebar rows
// and the thread pane header's private channel name (Slack's
// InlineChannelEntity, whose declared lock icon its :has() looks for) are
// blurred; a public channel's row isn't. The mode's top-nav toggle, a
// plugin element with Slack's p-top_nav__button classes, draws with
// Slack's button rules (the host base sheet). Screenshots go to
// SLICK_SHOTS_DIR / TAUT_SHOTS_DIR. Synthetic data only.

import (
	"context"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	smPriv   = "C0SECRET01"
	smGroup  = "G0GROUP001"
	smRootTS = "1700000100.000100"
)

// smEnv is newSV with a private channel and a group DM in the sidebar.
func smEnv(t *testing.T) *svEnv {
	return newSV(t, func(home string, a *App, ca *core.App) {
		ca.S.PutConv(slack.Conversation{ID: smPriv, Name: "secret-plans", IsChannel: true, IsGroup: true, IsPrivate: true, IsMember: true, NumMembers: 3})
		ca.S.PutConv(slack.Conversation{ID: smGroup, Name: "mpdm-self--alice--bob-1", IsMPIM: true, IsMember: true,
			Members: []string{ttSelf, svAlice, svBob}})
		b := a.data
		for _, ch := range []string{smPriv, smGroup} {
			b.fetched[ch] = time.Now()
		}
		b.apply(a, ca.Snapshot())
		m := &b.m
		m.Sections = []sbSection{
			{ID: "Lchannels", Name: "Channels", Type: "channels", Rows: []string{ttGen, svRandom, smPriv}},
			{ID: "Ldms", Name: "Direct messages", Type: "direct_messages", Rows: []string{svDM, smGroup}},
		}
		m.version++
	})
}

// smThread opens a thread in the private channel (over #general) and
// returns its view once the header names the channel.
func (e *svEnv) smThread() *messages.ThreadView {
	e.t.Helper()
	e.a.OpenThread(smPriv, smRootTS)
	var tv *messages.ThreadView
	e.until(10*time.Second, "the thread header naming #secret-plans", func() bool {
		if c := e.a.convos[smPriv+"/"+smRootTS]; c != nil {
			if l, ok := c.list.(threadList); ok {
				tv = l.ThreadView
			}
		}
		if tv == nil {
			return false
		}
		sub, _, ok := tv.HeaderName()
		return ok && sub.Icon == "lock-filled"
	})
	return tv
}

// smInk measures how sharp the text in r is: the neighbouring pixel
// pairs (across and down) whose luma differs by more than 64. Crisp 15px
// text has hundreds; a 4px blur leaves its strokes' edges a few levels
// apart, so almost none.
func smInk(img *image.RGBA, r image.Rectangle) int {
	r = r.Intersect(img.Bounds())
	luma := func(x, y int) int {
		p := img.RGBAAt(x, y)
		return svLuma(color.NRGBA{p.R, p.G, p.B, 0xff})
	}
	n := 0
	for y := r.Min.Y; y < r.Max.Y-1; y++ {
		for x := r.Min.X; x < r.Max.X-1; x++ {
			l := luma(x, y)
			if d := luma(x+1, y) - l; d > 64 || d < -64 {
				n++
			}
			if d := luma(x, y+1) - l; d > 64 || d < -64 {
				n++
			}
		}
	}
	return n
}

// smLight counts the light pixels (luma over 170) in r.
func smLight(img *image.RGBA, r image.Rectangle) int {
	r = r.Intersect(img.Bounds())
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if svLuma(color.NRGBA{p.R, p.G, p.B, 0xff}) > 170 {
				n++
			}
		}
	}
	return n
}

// smRowRect is the text area of ch's sidebar row.
func (e *svEnv) smRowRect(ch string) image.Rectangle {
	e.t.Helper()
	p, ok := e.rowPos(ch)
	if !ok {
		e.t.Fatalf("no sidebar row for %s (rows %v)", ch, e.shownConvs())
	}
	x, y := int(p.X), int(p.Y)
	return image.Rect(x-24, y-9, x+120, y+9)
}

// smCheck compares the frames with the mode off and on.
func smCheck(t *testing.T, e *svEnv, sh *svShooter, prefix string, on func()) {
	tv := e.smThread()
	e.dismissOverlays()
	for i := 0; i < 5; i++ {
		e.frame()
	}
	off := sh.shot()
	sh.save(prefix+"-off", off)
	// the top nav's right end: Slack's help button and, before it, the
	// plugin's toggle (HelpButton's patch renders both: they're the right
	// container's flex items), two 20px light glyphs on the aubergine bar
	nav := image.Rect(e.sz.X-140, 0, e.sz.X, int(theme.TopNavH))
	if n := smLight(off, nav); n < 140 { // the help glyph alone is ~96
		t.Errorf("the top nav's right end has %d light glyph pixels: the help button and the plugin's toggle aren't both drawn", n)
	} else {
		t.Logf("top nav glyph pixels: %d", n)
	}
	_, hr, _ := tv.HeaderName()
	regions := map[string]image.Rectangle{
		"private channel row": e.smRowRect(smPriv),
		"group DM row":        e.smRowRect(smGroup),
		"thread header name":  hr,
	}
	public := e.smRowRect(svRandom)
	before := map[string]int{}
	for name, r := range regions {
		if before[name] = smInk(off, r); before[name] < 15 {
			t.Fatalf("%s: only %d sharp text edges in %v before the mode is on", name, before[name], r)
		}
	}
	pubBefore := smInk(off, public)

	on()
	var img *image.RGBA
	deadline := time.Now().Add(15 * time.Second)
	for {
		img = sh.shot()
		blurred := true
		for name, r := range regions {
			if smInk(img, r)*3 > before[name] {
				blurred = false
			}
		}
		if blurred || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	sh.save(prefix+"-on", img)
	if sh.dir != "" {
		pane := e.a.secondary.drawnW
		if pane > 0 {
			sh.save(prefix+"-on-pane", img.SubImage(image.Rect(e.sz.X-pane, 0, e.sz.X, 140)).(*image.RGBA))
		}
		sh.save(prefix+"-on-topnav", img.SubImage(image.Rect(e.sz.X-320, 0, e.sz.X, 48)).(*image.RGBA))
	}
	for name, r := range regions {
		if got := smInk(img, r); got*3 > before[name] {
			t.Errorf("%s isn't blurred: %d of %d sharp text edges left", name, got, before[name])
		} else {
			t.Logf("%s blurred: %d → %d sharp text edges", name, before[name], got)
		}
	}
	if got := smInk(img, public); got*3 < pubBefore*2 {
		t.Errorf("the public channel's row lost its text (%d → %d): it must stay readable", pubBefore, got)
	}
}

func TestSlickViewStreamerMode(t *testing.T) {
	e := smEnv(t)
	sh := newSVShooter(t, e)
	e.install("StreamerMode", nil) // activation: while screen sharing (off)
	e.until(10*time.Second, "the HelpButton patched", func() bool { return e.tree("slick-streamer-mode__button") != "" })
	smCheck(t, e, sh, "streamer-mode-slick", func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := e.s.SetOption(ctx, "StreamerMode", "activation", "always"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestTautViewStreamerMode(t *testing.T) {
	dir := os.Getenv("TAUT_PLUGINS_DIR")
	if dir == "" {
		t.Skip("TAUT_PLUGINS_DIR not set: the ui-taut workflow builds Taut's plugins and runs this")
	}
	e := smEnv(t)
	sh := newSVShooter(t, e)
	sh.dir = os.Getenv("TAUT_SHOTS_DIR")
	tt := e.a.Taut()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	code, err := os.ReadFile(filepath.Join(dir, "StreamerMode.js"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := tt.Import(ctx, code, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	e.until(10*time.Second, "the HelpButton patched", func() bool { return e.tree("taut-streamer-mode__button") != "" })
	smCheck(t, e, sh, "streamer-mode-taut", func() {
		if err := tt.SetOption(ctx, id, "alwaysOn", true); err != nil {
			t.Fatal(err)
		}
	})
}
