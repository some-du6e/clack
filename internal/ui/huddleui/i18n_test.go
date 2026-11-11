package huddleui

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// The huddle UI in Slack's es-ES and ja-JP bundles (docs/I18N.md): the
// window with captions, the dock (full and collapsed), the invite window
// and card, the menus and the dialogs' strings, drawn in en-US and in each
// locale against the fake Chime. A string drawn unchanged fails when
// Slack's bundle translates it (the internal/ui checks' rule). The
// bundles come from the CDN (CLACK_I18N_NET=1, cached in
// CLACK_I18N_CACHE); I18N_SHOTS_DIR also saves the surfaces' pictures.

var (
	hudBundleMu sync.Mutex
	hudBundles  = map[string]*i18n.Catalog{}
)

func hudBundle(t *testing.T, loc string) *i18n.Catalog {
	t.Helper()
	if os.Getenv("CLACK_I18N_NET") == "" {
		t.Skip("CLACK_I18N_NET not set (Slack's bundles come from the CDN)")
	}
	hudBundleMu.Lock()
	defer hudBundleMu.Unlock()
	if c := hudBundles[loc]; c != nil {
		return c
	}
	l := &i18n.Loader{CacheDir: os.Getenv("CLACK_I18N_CACHE")}
	c, err := l.Load(context.Background(), loc)
	if err != nil {
		t.Fatalf("%s bundle: %v", loc, err)
	}
	hudBundles[loc] = c
	return c
}

// hudUnkeyed are strings the huddle UI draws unchanged that are the fake
// huddle's data: words of its caption ("Hello from the fake huddle"), of
// the remote user's name ("Remote Example") and of the channel's
// ("#huddle-test", which breaks after its hyphen), which wrapped text draws
// a piece at a time.
var hudUnkeyed = map[string]bool{"huddle": true, "Example": true, "Remote": true, "test": true}

var hudWordRE = regexp.MustCompile(`[A-Za-z]{3,}`)

// hudLeft is the internal/ui checks' englishLeft for strings Slack
// translates: drawn alike in en-US and loc, and some namespace has a
// different text for them.
func hudLeft(cat *i18n.Catalog, en, loc []string) []string {
	in := map[string]bool{}
	for _, s := range loc {
		in[s] = true
	}
	var out []string
	seen := map[string]bool{}
	for _, s := range en {
		if seen[s] || !in[s] || !hudWordRE.MatchString(s) || hudUnkeyed[s] || strings.Contains(s, AppName) {
			continue
		}
		seen[s] = true
		nss := cat.NamespacesOf(s)
		stands := len(nss) == 0
		for _, ns := range nss {
			if t, _ := cat.Lookup(ns, s); t == "" || t == s {
				stands = true
			}
		}
		if !stands {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// hudSurface is one surface: draw lays it out (two frames), and the
// labels of what it doesn't draw itself (its menu items).
type hudSurface struct {
	name   string
	w, h   int
	draw   func(gtx C)
	labels func() []string
}

// hudSurfaces sets the fake huddle up and lists the surfaces.
func hudSurfaces(t *testing.T) []hudSurface {
	e := newShotEnv(t, false)
	e.h.app.S.PutConv(slack.Conversation{ID: dmChannel, IsIM: true, User: huddle.FakeRemoteUser})
	s := e.join(nil, nil)
	e.c.ToggleClosedCaptions(s)
	waitState(t, s, "connected with a caption", func(st huddle.State) bool {
		return st.Phase == huddle.Connected && len(st.Captions) > 1 && len(st.Participants) > 1
	})
	c := e.c
	th := theme.New(false)
	var dec, soon, join widget.Clickable
	var card cardState
	dm := &incoming{Channel: dmChannel, Call: "R1", Sender: huddle.FakeRemoteUser, status: "pending", card: true}
	ch := &incoming{Channel: hcChannel, Call: "R1", Sender: huddle.FakeRemoteUser, status: "pending", card: true}
	return []hudSurface{
		{name: "window", w: 620, h: 400, draw: func(gtx C) {
			c.mu.Lock()
			c.win = e.w
			c.mu.Unlock()
			e.w.layout(gtx)
		}, labels: func() []string {
			var out []string
			for _, it := range c.moreMenu(s, s.State(), e.w) {
				out = append(out, it.Label, it.Header)
				for _, sub := range it.Sub {
					out = append(out, sub.Label, sub.Header)
				}
			}
			for _, it := range c.audioMenu(s) {
				out = append(out, it.Label, it.Header)
			}
			return out
		}},
		{name: "dock-collapsed", w: 1000, h: dockCollapsed + dockPad, draw: func(gtx C) {
			c.mu.Lock()
			c.win = e.w
			c.mu.Unlock()
			c.LayoutDock(gtx, e.h.th)
		}},
		{name: "dock", w: 1000, h: dockBar + dockPad, draw: func(gtx C) {
			c.mu.Lock()
			c.win = nil
			c.mu.Unlock()
			c.LayoutDock(gtx, e.h.th)
		}},
		{name: "invite-window-dm", w: 380, h: 280, draw: func(gtx C) {
			layoutInviteWindow(gtx, th, c, c.view(dm), &dec, &soon, &join)
		}},
		{name: "invite-window-channel", w: 380, h: 280, draw: func(gtx C) {
			layoutInviteWindow(gtx, th, c, c.view(ch), &dec, &soon, &join)
		}},
		{name: "invite-card", w: 260, h: 240, draw: func(gtx C) {
			layoutCard(gtx, th, c, c.view(ch), &card, gtx.Constraints.Max.X, false)
		}},
		{name: "share-picker", w: 900, h: 560, draw: func(gtx C) {
			// ScreenPickerModal's header, grid and footer, sources already listed
			p := &screenPicker{c: c, s: s, ph: winHost{e.w}, kind: huddle.ShareScreen, loaded: true, audioOK: true,
				screens: []*pickTile{{src: huddle.ShareSource{ID: "screen:1:0", Name: "Screen 1"}}},
				windows: []*pickTile{{src: huddle.ShareSource{ID: "window:9:0", Name: "zsh"}}}}
			hg := gtx
			hg.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X, gtx.Dp(70)))
			p.header(hg)
			bg := gtx
			bg.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X, gtx.Dp(400)))
			stk := op.Offset(image.Pt(0, gtx.Dp(70))).Push(gtx.Ops)
			p.body(bg)
			stk.Pop()
			fg := gtx
			fg.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X/2, gtx.Dp(36)))
			stk = op.Offset(image.Pt(0, gtx.Dp(480))).Push(gtx.Ops)
			p.audioBox(fg)
			stk.Pop()
		}, labels: func() []string {
			out := []string{nsHuddles.T("Cancel"), nsHuddles.T("Share")}
			for _, a := range []string{alertNoScreenShare, alertShareBusy} {
				parts, _ := alertParts(a)
				for _, p := range parts {
					out = append(out, p.Text)
				}
			}
			for _, st := range []huddle.State{{}, {Share: huddle.ShareStatus{State: huddle.ShareOn}},
				{Participants: []huddle.Participant{{Sharing: true}, {Sharing: true}}}} {
				out = append(out, c.shareCtl(st).tip)
			}
			return out
		}},
		{name: "share-mini-panel", w: panelW, h: panelH, draw: func(gtx C) {
			(&miniPanel{c: c}).layout(gtx, th)
		}},
		{name: "dialogs", labels: func() []string {
			// the dialogs' and notices' text
			out := []string{stillHereText(30), stillHereText(1)}
			for _, err := range []error{huddle.ErrEnded, context.Canceled} {
				if m, ok := endedModal(err); ok {
					out = append(out, m.title, m.body, m.button)
				}
			}
			m := joinErrorModal(nil)
			out = append(out, m.title, m.body, m.button)
			n := c.inviteNotification(ch)
			out = append(out, n.Body, n.PlainBody)
			for _, a := range n.Actions {
				out = append(out, a.Label)
			}
			return out
		}},
	}
}

// hudStrings draws sf twice (clicks and text measurement settle on the
// second frame) and returns what theme.Text drew and the surface's labels.
func hudStrings(sf hudSurface) []string {
	set := map[string]bool{}
	var mu sync.Mutex
	theme.DrawHook = func(s string) { mu.Lock(); set[s] = true; mu.Unlock() }
	if sf.draw != nil {
		var ops op.Ops
		for i := 0; i < 2; i++ {
			ops.Reset()
			sf.draw(shotCtx(&ops, sf.w, sf.h))
		}
	}
	theme.DrawHook = nil
	if sf.labels != nil {
		for _, l := range sf.labels() {
			if l != "" {
				set[l] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// TestHuddleNoEnglishLeft: every huddle surface in es-ES and ja-JP.
func TestHuddleNoEnglishLeft(t *testing.T) {
	cats := map[string]*i18n.Catalog{"es-ES": hudBundle(t, "es-ES"), "ja-JP": hudBundle(t, "ja-JP")}
	t.Cleanup(func() { i18n.Use(i18n.EnUS, nil) })
	for _, sf := range hudSurfaces(t) {
		i18n.Use(i18n.EnUS, nil)
		en := hudStrings(sf)
		for _, loc := range []string{"es-ES", "ja-JP"} {
			i18n.Use(loc, cats[loc])
			if left := hudLeft(cats[loc], en, hudStrings(sf)); len(left) > 0 {
				t.Errorf("%s %s: %d strings Slack translates left in English:\n  %s", loc, sf.name, len(left), strings.Join(left, "\n  "))
			}
		}
	}
}

// TestHuddleLocaleShots: the drawn surfaces in es-ES and ja-JP
// (I18N_SHOTS_DIR, i18n-<loc>-huddle-<name>.png).
func TestHuddleLocaleShots(t *testing.T) {
	dir := os.Getenv("I18N_SHOTS_DIR")
	if dir == "" {
		t.Skip("I18N_SHOTS_DIR not set")
	}
	cats := map[string]*i18n.Catalog{"es-ES": hudBundle(t, "es-ES"), "ja-JP": hudBundle(t, "ja-JP")}
	t.Cleanup(func() { i18n.Use(i18n.EnUS, nil) })
	for _, loc := range []string{"es-ES", "ja-JP"} {
		i18n.Use(loc, cats[loc])
		for _, sf := range hudSurfaces(t) {
			if sf.draw == nil {
				continue
			}
			var ops op.Ops
			for i := 0; i < 2; i++ {
				ops.Reset()
				gtx := shotCtx(&ops, sf.w, sf.h)
				theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
				sf.draw(gtx)
			}
			img := pixeltest.Render(t, sf.w*shotScale, sf.h*shotScale, &ops)
			f, err := os.Create(filepath.Join(dir, "i18n-"+loc+"-huddle-"+sf.name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}
