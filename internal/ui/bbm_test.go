package ui

// Simplified layout mode (bbm.go) in the whole app against internal/
// fakeslack (synthetic data; nothing talks to Slack): the pref's round
// trip (this device only), each layout change, navigation in the mode,
// and with BBM_SHOTS_DIR set (the bbm-shots workflow) light and dark
// screenshots of its surfaces.

import (
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func bbmEnv(t *testing.T) (*perfEnv, *callLog) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	log := &callLog{}
	fs.SetHook(func(m string, f url.Values) (any, bool) { log.add(m, f); return nil, false })
	e := startEnv(t, fs, t.TempDir())
	if dir := os.Getenv("BBM_SHOTS_DIR"); dir != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	e.settle(200 * time.Millisecond)
	return e, log
}

// bbmShot saves the last frame as bbm-<name>-light/dark.png.
func (e *perfEnv) bbmShot(name string) {
	dir := os.Getenv("BBM_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	e.settle(400 * time.Millisecond)
	mode := "light"
	if e.a.th.Dark {
		mode = "dark"
	}
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		e.tb.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, "bbm-"+name+"-"+mode+".png"))
	if err != nil {
		e.tb.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		e.tb.Fatal(err)
	}
}

func crumbLabels(a *App) []string {
	var out []string
	for _, c := range a.bbmCrumbs() {
		out = append(out, c.label)
	}
	return out
}

func crumbAction(t *testing.T, a *App, key string) {
	t.Helper()
	for _, c := range a.bbmCrumbs() {
		if c.key == key && c.action != nil {
			c.action()
			return
		}
	}
	t.Fatalf("no %q breadcrumb in %v", key, crumbLabels(a))
}

// The pref is Slack's client-side store, not a users.prefs key: turning
// the mode on and off writes nothing to the API, ui.json keeps it, and a
// restart lands on the BBM tab (RedirectColdBootToBBM).
func TestBBMPrefRoundTrip(t *testing.T) {
	e, log := bbmEnv(t)
	a := e.a
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	rows := pageRows(t, a, p, "accessibility")
	if !hasRow(rows, "button", "Turn On") || hasRow(rows, "legend", "Choose how to display message actions") {
		t.Fatalf("off: %v", rows)
	}
	for _, r := range rows {
		if r.Kind == "button" && r.Label == "Turn On" && !r.Enabled {
			t.Fatal("Turn On is greyed")
		}
	}

	a.startBBM("accessibility_pref")
	e.settle(100 * time.Millisecond)
	if !a.bbmOn() || !a.prefs.BBMOnboarded || a.router.Current() != "bbm" {
		t.Fatalf("on: bbm %v onboarded %v route %q", a.bbmOn(), a.prefs.BBMOnboarded, a.router.Current())
	}
	// maybeOpenBBMNUXModal: the welcome dialog, and the mode's zoom 3 (150%)
	if _, ok := topModal(a); !ok {
		t.Fatal("no welcome dialog")
	}
	if a.prefs.BBMZoom == nil || *a.prefs.BBMZoom != 3 || a.prefs.Zoom != 3 {
		t.Fatalf("zoom: %v %d", a.prefs.BBMZoom, a.prefs.Zoom)
	}
	for a.dismissTop() {
	}
	rows = pageRows(t, a, p, "accessibility")
	for _, w := range []want{btn("Turn Off"), lg("Choose how to display message actions"), rd("Always show collapsed menu"), rd("Show full action menu on hover only")} {
		found := false
		for _, r := range rows {
			if w.matches(r) {
				found = true
			}
		}
		if !found {
			t.Errorf("on: no %v in %v", w, rows)
		}
	}
	if !a.bbmPersistActions() {
		t.Fatal("persistent actions are the default")
	}
	a.setBBMPersistActions(false)
	if a.bbmPersistActions() {
		t.Fatal("hover-only not applied")
	}
	a.setBBMPersistActions(true)

	a.flushPrefs()
	a2 := New(Options{ConfigDir: e.home, NoRealtime: true}, imgcache.New(e.fs.Fetch, "", nil))
	if !a2.prefs.BBM || a2.prefs.BBMHoverActions || !a2.prefs.BBMOnboarded || a2.router.Current() != "bbm" {
		t.Fatalf("restart: bbm %v hover %v onboarded %v route %q", a2.prefs.BBM, a2.prefs.BBMHoverActions, a2.prefs.BBMOnboarded, a2.router.Current())
	}

	a.stopBBM()
	e.frame()
	if a.bbmOn() || isBBMRoute(a.router.Current()) || a.prefs.Zoom != 0 {
		t.Fatalf("off: bbm %v route %q zoom %d", a.bbmOn(), a.router.Current(), a.prefs.Zoom)
	}
	// the mode's zoom comes back next time, without the dialog
	a.startBBM("accessibility_pref")
	if a.prefs.Zoom != 3 {
		t.Fatalf("second start zoom %d", a.prefs.Zoom)
	}
	if m, ok := topModal(a); ok {
		t.Fatalf("welcome dialog again: %v", m)
	}
	a.stopBBM()

	log.mu.Lock()
	for i, m := range log.calls {
		for k, vs := range log.forms[i] {
			for _, v := range vs {
				if l := strings.ToLower(k + "=" + v); strings.Contains(l, "bbm") || strings.Contains(l, "simplified") {
					t.Errorf("%s sent %s", m, l)
				}
			}
		}
	}
	log.mu.Unlock()

	// agentic_ia ("Slack 6"): no simplified layout
	a.prefs.BBM = true
	a.data.core.SetExperimentGroup("agentic_ia", "on")
	rows = pageRows(t, a, p, "accessibility")
	if a.bbmOn() || !hasRow(rows, "text", "Simplified Layout is not yet supported in Slack 6") {
		t.Fatalf("agentic_ia: on %v rows %v", a.bbmOn(), rows)
	}
	for _, r := range rows {
		if r.Kind == "button" && r.Label == "Turn On" && r.Enabled {
			t.Fatal("Turn On enabled under agentic_ia")
		}
	}
	a.data.core.SetExperimentGroup("agentic_ia", "off")
	a.prefs.BBM = false
}

// The window in the mode, one container at a time, and how to move
// around it; light and dark.
func TestBBMLayoutAndNavigation(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) { bbmScenario(t, dark) })
	}
}

func bbmScenario(t *testing.T, dark bool) {
	e, _ := bbmEnv(t)
	a := e.a
	e.setDark(dark)
	a.prefs.BBMOnboarded = true // no welcome dialog (its own shots below)
	a.startBBM("accessibility_pref")
	e.settle(200 * time.Millisecond)

	// the BBM tab: no rail, the panel 1200 wide and centred under the 40px
	// top bar, no breadcrumbs
	if got := a.primaryOrigin; got != image.Pt(120, 40) {
		t.Fatalf("panel at %v, want (120,40)", got)
	}
	g := ttContext(new(op.Ops), e.size)
	rows := a.bbmMainRows(g)
	var labels []string
	for _, r := range rows {
		labels = append(labels, r.label)
	}
	if len(rows) < 3 || rows[0].key != "home" || rows[1].key != "dms" {
		t.Fatalf("BBM list %v", labels)
	}
	e.bbmShot("main")
	a.Navigate("bbm/more")
	e.frame()
	if r := a.bbmMoreRows(); len(r) > 0 && r[len(r)-1].key != "preferences" {
		t.Errorf("More ends with %+v", r[len(r)-1])
	}
	e.bbmShot("more")
	a.Navigate("bbm/workspaces")
	e.frame()
	e.bbmShot("workspaces")
	a.Navigate("bbm")
	e.frame()

	// the first row (Home): a real click at its centre (header 60, the
	// list's 8px, the row's 1px margin, half its 62px)
	e.click(image.Pt(720, 40+60+8+1+31))
	for range 4 {
		e.frame()
	}
	r := a.router.Current()
	if a.routeTab(r) != "home" || a.bbm.cont != bbmSidebar {
		t.Fatalf("after Home: route %q container %d", r, a.bbm.cont)
	}
	if got := a.primaryOrigin; got != image.Pt(120, 72) {
		t.Errorf("panel under the breadcrumbs at %v, want (120,72)", got)
	}
	if c := crumbLabels(a); len(c) != 2 || c[1] != "Home" {
		t.Errorf("crumbs %v", c)
	}
	// the channel list's rows: 44px boxes, 12px apart
	var conv *sbRow
	for i := range a.sidebar.rows {
		if a.sidebar.rows[i].kind == sbConv {
			conv = &a.sidebar.rows[i]
			break
		}
	}
	if conv == nil {
		t.Fatal("no conversation row")
	}
	a.sidebarPalette()
	if d := a.layoutSidebarRow(ttContext(new(op.Ops), image.Pt(600, 200)), conv); d.Size.Y != 56 {
		t.Errorf("BBM conversation row %d tall, want 44 + 12", d.Size.Y)
	}
	e.bbmShot("home-list")

	// Alt+↓ doesn't step conversations from the list; Ctrl+Shift+D doesn't
	// hide the sidebar
	before := a.router.Current()
	e.press(key.NameDownArrow, key.ModAlt)
	e.press("D", key.ModShortcut|key.ModShift)
	if a.router.Current() != before || a.sidebar.hidden {
		t.Errorf("keys in the list: route %q → %q, sidebar hidden %v", before, a.router.Current(), a.sidebar.hidden)
	}

	// a conversation: the primary view fills the panel
	ch := conv.key
	a.Navigate("c/" + ch)
	e.settle(300 * time.Millisecond)
	if a.bbm.cont != bbmPrimary {
		t.Fatalf("channel: container %d", a.bbm.cont)
	}
	if c := crumbLabels(a); len(c) != 3 {
		t.Errorf("channel crumbs %v", c)
	}
	e.bbmShot("channel")

	// a thread: the secondary pane fills it
	var tch, tts string
	for _, id := range e.fs.ChannelIDs() {
		a.Navigate("c/" + id)
		e.until("history "+id, 10*time.Second, func() bool { return e.loadedMessages(id) > 0 })
		for _, m := range a.data.core.S.Timeline(id).Msgs {
			if m.ReplyCount > 0 {
				tch, tts = id, m.TS
				break
			}
		}
		if tts != "" {
			break
		}
	}
	if tts == "" {
		t.Fatal("no thread in the fake")
	}
	a.OpenThread(tch, tts)
	e.settle(300 * time.Millisecond)
	if a.bbm.cont != bbmSecondary {
		t.Fatalf("thread: container %d", a.bbm.cont)
	}
	if c := crumbLabels(a); c[len(c)-1] != "Thread" {
		t.Errorf("thread crumbs %v", c)
	}
	e.bbmShot("thread")
	crumbAction(t, a, "primary")
	e.frame()
	if a.bbm.cont != bbmPrimary || !a.secondary.open() {
		t.Errorf("primary crumb: container %d, thread open %v", a.bbm.cont, a.secondary.open())
	}
	a.CloseSecondary()
	e.frame()
	crumbAction(t, a, "tab")
	e.frame()
	if a.bbm.cont != bbmSidebar {
		t.Errorf("tab crumb: container %d", a.bbm.cont)
	}
	crumbAction(t, a, "main")
	e.frame()
	if a.router.Current() != "bbm" {
		t.Errorf("main crumb: route %q", a.router.Current())
	}

	// DMs: the list, then the conversation picked in it
	a.bbmPick(bbmRow{kind: "named-tab", key: "dms"}, image.Point{})
	e.settle(300 * time.Millisecond)
	if a.router.Current() != "dms" || a.bbm.cont != bbmSidebar {
		t.Fatalf("DMs: route %q container %d", a.router.Current(), a.bbm.cont)
	}
	e.bbmShot("dms-list")
	a.PaneChosen()
	e.settle(200 * time.Millisecond)
	if a.bbm.cont != bbmPrimary {
		t.Errorf("DM picked: container %d", a.bbm.cont)
	}
	e.bbmShot("dms-conversation")

	// Preferences in the mode, and the welcome dialog's three panes
	a.showPrefs("accessibility")
	e.settle(200 * time.Millisecond)
	e.bbmShot("prefs")
	for a.dismissTop() {
	}
	for i := range 3 {
		a.showBBMNuxPane(&bbmNux{}, i)
		e.settle(200 * time.Millisecond)
		e.bbmShot("welcome-" + itoa(i+1))
		for a.dismissTop() {
		}
	}

	// Exit: the rail is back and the history skips the BBM tab
	a.Navigate("bbm")
	e.frame()
	e.click(image.Pt(1440-120-4-16, 20)) // ExitBBMButton, the right-most button
	for range 4 {
		e.frame()
	}
	if a.bbmOn() || isBBMRoute(a.router.Current()) {
		t.Fatalf("after Exit: on %v route %q", a.bbmOn(), a.router.Current())
	}
	if a.primaryOrigin.X < 70 {
		t.Errorf("rail not back: panel at %v", a.primaryOrigin)
	}
	for range 3 {
		a.Back()
		if isBBMRoute(a.router.Current()) && a.router.CanBack() {
			t.Fatalf("Back landed on %q outside the mode", a.router.Current())
		}
	}
	_ = popups.BtnPrimary
}
