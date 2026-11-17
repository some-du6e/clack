package ui

// Slick plugins through the shell's native views (ui-slick workflow): each
// TestSlickView<Plugin> installs the real built plugin unmodified
// (SLICK_PLUGINS_DIR), draws the whole window (sidebar, top nav, primary
// view) over a synthetic workspace through a real input router, and checks
// what the user would see: the host trees the patched boundaries drew,
// the rows the sidebar laid out, the menu a right-click opened. Synthetic
// data only; nothing reaches the network.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/slick"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	svAlice  = "UALICE0001"
	svBob    = "UBOB000001"
	svRandom = "C0RANDOM01"
	svDM     = "D0ALICE001"
)

type svMenu struct {
	ch    string
	items []popups.MenuItem
}

// svEnv is the shell over a synthetic workspace with Slick running.
type svEnv struct {
	t     *testing.T
	a     *App
	ca    *core.App
	s     *slick.Slick
	dir   string
	ops   *op.Ops
	rt    *input.Router
	sz    image.Point
	clock time.Duration // pointer event times

	mu    sync.Mutex
	trees map[uint64][]byte
	menus []svMenu
}

// newSV starts the shell; setup runs before Taut and Slick start (home is
// CLACK_HOME, so <home>/slick is Slick's directory).
func newSV(t *testing.T, setup ...func(home string, a *App, ca *core.App)) *svEnv {
	t.Helper()
	dir := os.Getenv("SLICK_PLUGINS_DIR")
	if dir == "" {
		t.Skip("SLICK_PLUGINS_DIR not set: the ui-slick workflow builds Slick's plugins and runs this")
	}
	return newSVIn(t, dir, setup...)
}

// newSVIn is newSV over the built plugins in dir: "" for a test that
// installs plugin code of its own (installCode), which runs anywhere.
func newSVIn(t *testing.T, dir string, setup ...func(home string, a *App, ca *core.App)) *svEnv {
	t.Helper()
	home := t.TempDir()
	a, ca := ttApp(t, home)
	ca.C.EdgeURL = "http://127.0.0.1:1/edge/" // never the real edge API
	ca.S.PutUsers([]slack.User{
		{ID: svAlice, Name: "alice", RealName: "Alice Example", Profile: slack.Profile{DisplayName: "alice", RealName: "Alice Example"}},
		{ID: svBob, Name: "bob", RealName: "Bob Example", Profile: slack.Profile{DisplayName: "bob", RealName: "Bob Example"}},
	})
	ca.S.PutConv(slack.Conversation{ID: svRandom, Name: "random", IsChannel: true, IsMember: true, NumMembers: 3})
	ca.S.PutConv(slack.Conversation{ID: svDM, IsIM: true, User: svAlice, IsMember: true})

	b := newBackend(ca, "")
	b.app = a
	b.ctx, b.cancel = context.WithCancel(context.Background())
	for _, ch := range []string{ttGen, svRandom, svDM} {
		b.fetched[ch] = time.Now() // no tabs/facepile fetches (offline)
	}
	a.data = b
	b.apply(a, ca.Snapshot())
	m := &b.m
	m.Sections = []sbSection{
		{ID: "Lchannels", Name: "Channels", Type: "channels", Rows: []string{ttGen, svRandom}},
		{ID: "Ldms", Name: "Direct messages", Type: "direct_messages", Rows: []string{svDM}},
	}
	m.version++

	for _, f := range setup {
		f(home, a, ca)
	}
	e := &svEnv{t: t, a: a, ca: ca, dir: dir, ops: new(op.Ops), rt: new(input.Router), sz: image.Pt(1280, 860), trees: map[uint64][]byte{}}
	deliverTap = e.record
	sbMenuTap = e.menu
	t.Cleanup(func() { deliverTap, sbMenuTap = nil, nil })
	b.startTaut(a)
	t.Cleanup(func() { b.stopTaut(); b.cancel() })
	if e.s = a.Slick(); e.s == nil {
		t.Fatal("slick didn't start")
	}
	a.Navigate("c/" + ttGen)
	for i := 0; i < 5; i++ {
		e.frame()
	}
	return e
}

func (e *svEnv) record(root uint64, tree []byte) {
	e.mu.Lock()
	e.trees[root] = append([]byte(nil), tree...)
	e.mu.Unlock()
}

func (e *svEnv) menu(ch string, items []popups.MenuItem) {
	e.mu.Lock()
	e.menus = append(e.menus, svMenu{ch, items})
	e.mu.Unlock()
}

// install imports the plugin's built files as a user does, sets opts and
// enables it.
func (e *svEnv) install(id string, opts map[string]any) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, f := range []string{id + ".js", id + ".main.js"} {
		code, err := os.ReadFile(filepath.Join(e.dir, f))
		if err != nil {
			if f == id+".js" {
				e.t.Fatal(err)
			}
			continue // no main half
		}
		if _, err := e.s.Import(ctx, code); err != nil {
			e.t.Fatalf("import %s: %v", f, err)
		}
	}
	for k, v := range opts {
		if err := e.s.SetOption(ctx, id, k, v); err != nil {
			e.t.Fatalf("%s.%s: %v", id, k, err)
		}
	}
	if err := e.s.SetEnabled(ctx, id, true); err != nil {
		e.t.Fatalf("enable %s: %v", id, err)
	}
	for _, p := range e.s.Settings().Plugins {
		if p.ID == id && (!p.Running || p.StartError != "") {
			e.t.Fatalf("%s didn't start: running=%v error=%q", id, p.Running, p.StartError)
		}
	}
}

// installCode imports plugin code (a file as Slick's build:plugin writes
// one) as a user does and enables it.
func (e *svEnv) installCode(id, code string) {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := e.s.Import(ctx, []byte(code)); err != nil {
		e.t.Fatalf("import %s: %v", id, err)
	}
	if err := e.s.SetEnabled(ctx, id, true); err != nil {
		e.t.Fatalf("enable %s: %v", id, err)
	}
	for _, p := range e.s.Settings().Plugins {
		if p.ID == id && (!p.Running || p.StartError != "") {
			e.t.Fatalf("%s didn't start: running=%v error=%q", id, p.Running, p.StartError)
		}
	}
}

// frame draws the whole window once.
func (e *svEnv) frame() {
	e.ops.Reset()
	gtx := ttContext(e.ops, e.sz)
	gtx.Source = e.rt.Source()
	e.a.drain()
	slackc.Pump()
	e.a.title = e.a.windowTitle() // no window to retitle
	e.a.frame(gtx)
	e.rt.Frame(e.ops)
	if e.a.login != nil {
		e.t.Fatal("the shell fell back to the sign-in screen (the core signed out)")
	}
}

// until draws frames until cond holds.
func (e *svEnv) until(d time.Duration, what string, cond func() bool) {
	e.t.Helper()
	if !e.wait(d, cond) {
		e.dump()
		e.t.Fatalf("timed out waiting for %s", what)
	}
}

func (e *svEnv) wait(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		e.frame()
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// tree returns the host trees delivered so far that contain every sub.
func (e *svEnv) tree(subs ...string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, tr := range e.trees {
		s := string(tr)
		ok := true
		for _, sub := range subs {
			if !strings.Contains(s, sub) {
				ok = false
				break
			}
		}
		if ok {
			return s
		}
	}
	return ""
}

func (e *svEnv) dump() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for root, tr := range e.trees {
		s := string(tr)
		for i := 0; ; {
			j := strings.Index(s[i:], "getTemplate")
			if j < 0 {
				break
			}
			j += i
			e.t.Logf("tree %d getTemplate@%d: …%s…", root, j, s[max(j-200, 0):min(j+80, len(s))])
			i = j + 1
		}
		if len(s) > 3000 {
			s = s[:3000] + "…"
		}
		e.t.Logf("tree %d (%d bytes): %s", root, len(tr), s)
	}
	e.t.Logf("menus opened: %d", len(e.menus))
}

// rowPos is the window position of conversation ch's sidebar row (dp =
// px here), from the rows the sidebar laid out.
func (e *svEnv) rowPos(ch string) (f32.Point, bool) {
	s := &e.a.sidebar
	y := int(theme.TopNavH) + 49 + 28 + 10 // header, jump box, gap
	idx := s.shown
	n := len(s.rows)
	if idx != nil {
		n = len(idx)
	}
	for k := 0; k < n; k++ {
		i := k
		if idx != nil {
			i = idx[k]
		}
		r := &s.rows[i]
		h := int(theme.SidebarRowH)
		switch r.kind {
		case sbGap, sbDivider:
			h = r.gap
		case sbNote:
			h = 44
		}
		if r.kind == sbConv && r.key == ch {
			return f32.Pt(float32(int(theme.RailW)+60), float32(y+h/2)), true
		}
		y += h
	}
	return f32.Point{}, false
}

// rightClick presses the secondary button over ch's sidebar row.
func (e *svEnv) rightClick(ch string) { e.press(ch, pointer.ButtonSecondary) }

// click clicks ch's sidebar row.
func (e *svEnv) click(ch string) { e.press(ch, pointer.ButtonPrimary) }

func (e *svEnv) press(ch string, b pointer.Buttons) {
	e.t.Helper()
	p, ok := e.rowPos(ch)
	if !ok {
		e.t.Fatalf("no sidebar row for %s", ch)
	}
	e.clock += time.Second
	e.rt.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: e.clock},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: b, Position: p, Time: e.clock + 10*time.Millisecond},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: e.clock + 60*time.Millisecond},
	)
	e.frame()
	e.frame()
}

func (e *svEnv) lastMenu(ch string) []popups.MenuItem {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := len(e.menus) - 1; i >= 0; i-- {
		if e.menus[i].ch == ch {
			return e.menus[i].items
		}
	}
	return nil
}

func menuItem(items []popups.MenuItem, key string) (popups.MenuItem, bool) {
	for _, it := range items {
		if it.Key == key {
			return it, true
		}
	}
	return popups.MenuItem{}, false
}

// shownConvs lists the conversation rows the sidebar lays out.
func (e *svEnv) shownConvs() []string {
	s := &e.a.sidebar
	var out []string
	for k := range s.rows {
		i := k
		if s.shown != nil {
			if k >= len(s.shown) {
				break
			}
			i = s.shown[k]
		}
		if s.rows[i].kind == sbConv {
			out = append(out, s.rows[i].key)
		}
	}
	return out
}

func hasStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ChannelListTyping: someone typing in #random shows the indicator in
// #random's sidebar row (BaseChannelSuffix inside the row's Channel).
func TestSlickViewChannelListTyping(t *testing.T) {
	e := newSV(t)
	e.install("ChannelListTyping", nil)
	e.until(5*time.Second, "BaseChannelSuffix patched", func() bool { return slackc.Patched("BaseChannelSuffix") })
	// the row's suffix renders through the plugin
	e.until(5*time.Second, "#random's suffix through the plugin", func() bool {
		return e.tree(`"n":"BaseChannelSuffix"`, `"channelId":"`+svRandom+`"`) != ""
	})
	raw, _ := json.Marshal(map[string]any{"type": "user_typing", "channel": svRandom, "user": svAlice})
	e.ca.Inject(slack.Event{Type: "user_typing", Channel: json.RawMessage(`"` + svRandom + `"`), Raw: raw})
	if !slackc.Patched("Channel") {
		// isSidebarChannel accepts a component whose
		// Function.prototype.toString source includes
		// "getSidebarChannelUiInfo"; Taut's natives (internal/taut
		// js/04_react.js) carry source markers through an own toString
		// only, and none for Channel, so the row context never arrives.
		e.frame()
		t.Skip("gap (internal/taut): Clack's Channel native doesn't satisfy ChannelListTyping's isSidebarChannel source filter; the shell renders the row as Channel > … > BaseChannelSuffix, ready for it")
	}
	e.until(6*time.Second, "the typing indicator in #random's row", func() bool {
		return e.tree("slick-channel-typing", `"channelId":"`+svRandom+`"`) != ""
	})
	if e.tree("slick-channel-typing", `"channelId":"`+ttGen+`"`) != "" {
		t.Error("the indicator shows in #general too, where nobody types")
	}

	// where it is drawn: Slack's suffix sits at the row's right edge
	// (before the mention badge; #random has none), not right after the
	// name, and is only as wide as the indicator
	var g sbSuffixGeom
	sbSuffixTap = func(ch string, x sbSuffixGeom) {
		if ch == svRandom {
			g = x
		}
	}
	t.Cleanup(func() { sbSuffixTap = nil })
	e.until(3*time.Second, "#random's suffix laid out with the indicator", func() bool { return g.X1 > g.X0 })
	t.Logf("#random's row: name ends at %d, suffix [%d, %d), suffix area ends at %d, row %d wide", g.NameEnd, g.X0, g.X1, g.Right, g.RowW)
	if g.X1 != g.Right {
		t.Errorf("the indicator ends at x=%d, not at the suffix area's right edge %d", g.X1, g.Right)
	}
	if g.RowW-g.Right > 10 {
		t.Errorf("the suffix area ends at x=%d, %dpx short of the row's right edge (%d) with no badge", g.Right, g.RowW-g.Right, g.RowW)
	}
	if g.X0 < g.NameEnd+24 {
		t.Errorf("the indicator starts at x=%d, right after the name (ends at %d), not at the row's right part", g.X0, g.NameEnd)
	}
	if w := g.X1 - g.X0; w > g.RowW/3 {
		t.Errorf("the indicator is %dpx wide in a %dpx row: it fills the row instead of fitting its content", w, g.RowW)
	}
}

// ChatTabs: its layout around ClientPrimaryView, its tab bar first inside
// the primary view element (RenderedViewContents, before the view's body),
// padding the view by its measured height; its "Open in new Slick tab"
// entry on a sidebar channel's right-click menu, and that entry opening
// the channel (handleDeepLink → Clack route) as a second tab.
func TestSlickViewChatTabs(t *testing.T) {
	e := newSV(t)
	e.install("ChatTabs", nil) // thread tabs "Thread only", its default
	e.until(5*time.Second, "ChatTabs' patches", func() bool {
		return slackc.Patched("ClientPrimaryView") && slackc.Patched("ConnectedChannel") &&
			slackc.Patched("ChannelContextMenu") && slackc.Patched("ContextMenuTriggerWithKeyboardFocus")
	})
	// the layout around the primary view, the view itself inside it
	e.until(8*time.Second, "the tab layout around the primary view", func() bool {
		return e.tree("slick-chat-tabs-layout", `"n":"ClientPrimaryView"`) != ""
	})
	e.until(8*time.Second, "the tab bar", func() bool { return e.tree(`slick-chat-tabs`, `data-tab-count`) != "" })
	// Slick since f52f03a: the bar inside the view element (RenderedViewContents),
	// ahead of the body (older ChatTabs, as ui-taut's pinned Slick, draw it
	// in ClientPrimaryView's tree)
	inView := slackc.Patched("RenderedViewContents")
	if inView {
		e.until(8*time.Second, "the tab bar inside .p-view_contents--primary", func() bool {
			tr := e.tree("p-view_contents--primary", `slick-chat-tabs`, `data-tab-count`, `"n":"`+viewBodySlot+`"`)
			return tr != "" && strings.Index(tr, `data-tab-count`) < strings.Index(tr, `"n":"`+viewBodySlot+`"`)
		})
	}
	if len(e.a.convos) == 0 {
		t.Error("the channel view didn't draw inside the patched primary view")
	}

	// #random's row inside ChatTabs' channel context: its trigger gets
	// ChatTabs' getTemplate (a JS function wrapping Clack's)
	e.until(8*time.Second, "ChatTabs' getTemplate on #random's trigger", func() bool {
		return e.tree(`"$key":"ct:`+svRandom+`"`, `"getTemplate":{"$fn"`) != "" && e.a.sbMenu.gts[svRandom].ID != 0
	})

	// right-click #random in the sidebar
	e.rightClick(svRandom)
	e.until(5*time.Second, "#random's context menu", func() bool { return e.lastMenu(svRandom) != nil })
	items := e.lastMenu(svRandom)
	open, ok := menuItem(items, "slick-chat-tabs-open")
	if !ok || open.Label != "Open in new Slick tab" || open.Do == nil {
		var labels []string
		for _, it := range items {
			labels = append(labels, it.Key+"="+it.Label)
		}
		t.Fatalf("no ChatTabs entry in the sidebar menu: %v", labels)
	}
	if _, ok := menuItem(items, "leave"); !ok {
		t.Error("Clack's own items are gone from the menu")
	}
	// it goes in before Leave (the first danger item), like Slack
	for i, it := range items {
		if it.Key == "leave" && (i == 0 || items[i-1].Key != "slick-chat-tabs-open") {
			t.Errorf("entry not right before Leave: %v", items)
		}
	}

	// choosing it: ChatTabs dispatches handleDeepLink slack://channel?…,
	// which UIHooks.DeepLink opens as Clack's route
	open.Do()
	e.until(8*time.Second, "the route c/"+svRandom, func() bool { return string(e.a.router.Current()) == "c/"+svRandom })
	e.until(8*time.Second, "two tabs, general and random", func() bool {
		return e.tree("slick-chat-tabs__tab", `data-tab-count":2`) != "" || e.tree("slick-chat-tabs__tab", "random", "general") != ""
	})

	// with two tabs the strip unfolds (one tab: an empty, collapsed bar):
	// its height measured (ResizeObserver over the bar Clack laid out) into
	// the variable the view's :has(> .slick-chat-tabs) padding reads, on
	// the layout's inline style (the geometry: slackc's TestViewContentsTabBar)
	heightVar := regexp.MustCompile(`"--slick-chat-tabs-height":"([0-9.]+)px"`)
	if inView {
		var barH float64
		e.until(8*time.Second, "the bar's laid-out height in --slick-chat-tabs-height", func() bool {
			m := heightVar.FindStringSubmatch(e.tree("slick-chat-tabs-layout", "--slick-chat-tabs-height"))
			if m == nil {
				return false
			}
			barH, _ = strconv.ParseFloat(m[1], 64)
			return barH > 0
		})
		if barH < 20 || barH > 120 {
			t.Errorf("--slick-chat-tabs-height %vpx, want the bar's row height", barH)
		}
	}

	// "Open in new Slick tab" on a message's More actions menu opens the
	// message's thread as a third tab: since Slick #473 ChatTabs' message
	// action (api.menu.addMessageActions), before it its patch on the
	// menu's renderer (MAMenu, found by a filter on its source). Thread
	// only tabs go there through Slack's navigate thunk ({viewType:
	// "Thread"}), which Clack routes as the thread's deep link: the thread
	// beside its channel (Clack draws no thread as a primary view)
	// It's ChatTabs' current tab while Clack shows it: Taut's view state
	// has it as the primary Thread view navigate asked for, so ChatTabs
	// adds no tab for the thread's channel (here a DM without one).
	openThreadTab := func(ch, ts string, tabs int) {
		t.Helper()
		e.dismissOverlays()
		e.a.OpenMessageMenu(image.Pt(900, 300), core.Message{Channel: ch, TS: ts, User: svBob, Text: "a thread to open"})
		var mm *popups.Menu
		i := -1
		e.until(8*time.Second, "Open in new Slick tab on the message's More actions", func() bool {
			if mm = e.topMenu(); mm == nil {
				return false
			}
			i = menuIndex(mm.Items(), "slick-chat-tabs-open-thread")
			return i >= 0 && len(mm.RowRects()) == len(mm.Items()) && !mm.RowRects()[i].Empty()
		})
		if tab := mm.Items()[i]; tab.Label != "Open in new Slick tab" {
			t.Fatalf("message action %+v", tab)
		}
		e.clickRect(mm.RowRects()[i])
		count := fmt.Sprintf(`data-tab-count":%d`, tabs)
		e.until(8*time.Second, "the message's thread as tab "+strconv.Itoa(tabs), func() bool { return e.tree("slick-chat-tabs__tab", count) != "" })
		e.until(8*time.Second, "the thread's route (navigate)", func() bool { return string(e.a.router.Current()) == "c/"+ch+"/t/"+ts })
		e.until(8*time.Second, "the thread tab current", func() bool { return svSelectedTab(e.tree("slick-chat-tabs__tab", count)) == ch+":"+ts })
		for k := 0; k < 20; k++ { // ChatTabs' route poll and its tab tracking
			e.frame()
			time.Sleep(20 * time.Millisecond)
		}
		if e.tree("slick-chat-tabs__tab", count) == "" {
			t.Errorf("tabs other than %d after the thread tab opened: a tab for its channel?", tabs)
		}
	}
	openThreadTab(svRandom, "1700000005.000500", 3)
	openThreadTab(svDM, "1700000007.000700", 4)

	// the rows still navigate on click while they're inside ChatTabs' tree
	e.dismissOverlays()
	e.click(ttGen)
	e.until(5*time.Second, "a click on #general's row opening it", func() bool { return string(e.a.router.Current()) == "c/"+ttGen })
}

// BetterMessageMenu's editor (api.settings.panel, Slick #473) in the
// Customize card of its Preferences > Slick page, where Slick's
// PluginDetail puts it (after any Settings card, before Workspaces), drawn through
// the slackc tree path the other plugin UI takes; the card goes when the
// plugin stops.
func TestSlickViewBetterMessageMenuPanel(t *testing.T) {
	e := newSV(t)
	if _, err := os.Stat(filepath.Join(e.dir, "BetterMessageMenu.js")); err != nil {
		t.Skip("this Slick has no BetterMessageMenu (it came in #473)")
	}
	e.install("BetterMessageMenu", nil)
	p := newSlickPage(e.a)
	defer p.close()
	p.layoutTab("slick_plugins")
	p.open = "BetterMessageMenu"
	// the labels the page draws, in order (theme.DrawHook)
	var mu sync.Mutex
	var drawn []string
	theme.DrawHook = func(s string) { mu.Lock(); drawn = append(drawn, s); mu.Unlock() }
	t.Cleanup(func() { theme.DrawHook = nil })
	page := func() []string {
		mu.Lock()
		drawn = drawn[:0]
		mu.Unlock()
		gtx := ttContext(new(op.Ops), image.Pt(800, 1200))
		e.a.slackcFrame(gtx)
		p.layout(gtx, "slick_plugins")
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), drawn...)
	}
	ready := func() bool {
		sub := p.k.subs["slickpanel.BetterMessageMenu"]
		return sub != nil && sub.Ready()
	}
	var labels []string
	e.until(10*time.Second, "the editor in the Customize card", func() bool {
		labels = page()
		return ready() && e.tree("slick-bmm-editor", "Big buttons", "Organize submenu", "Reset to defaults") != ""
	})
	// (Better Message Menu has no settings schema, so no Settings card)
	c, w := slices.Index(labels, "Customize"), slices.Index(labels, "Workspaces")
	if c < 0 || w < 0 || c > w || slices.Index(labels[c+1:], "Customize") >= 0 {
		t.Errorf("want one Customize card, before Workspaces: %q", labels)
	}
	// a stopped plugin has no panel to show, and its card goes
	if err := e.s.SetEnabled(context.Background(), "BetterMessageMenu", false); err != nil {
		t.Fatal(err)
	}
	e.until(5*time.Second, "no panel after stop", func() bool {
		for _, pl := range e.s.Settings().Plugins {
			if pl.ID == "BetterMessageMenu" {
				return !svPanelOf(pl)
			}
		}
		return false
	})
	e.until(5*time.Second, "no Customize card after stop", func() bool {
		labels = page()
		return !slices.Contains(labels, "Customize") && slices.Contains(labels, "Workspaces")
	})
}

// A plugin whose privileged half doesn't start (its ready threw: SlickMCP
// needs Node's http server) runs its renderer half, and its Preferences
// page says what failed and that what it does there doesn't happen.
func TestSlickViewMainErrorNotice(t *testing.T) {
	e := newSVIn(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, code := range []string{
		`// slick-plugin {"id":"MainFailSlick"}
(globalThis.__slickPlugins ??= []).push((require) => {
var import_slick = require("$slick");
class P extends import_slick.SlickPlugin {
  static id = "MainFailSlick";
  static pluginName = "MainFailSlick";
  static description = "test";
  static requiredAPIs = [];
  start() {}
}
return P;
});
`,
		`// slick-main {"id":"MainFailSlick"}
(globalThis.__slickMainPlugins ??= []).push((require) => ({
  id: "MainFailSlick",
  capabilities: [],
  ready() { throw new Error("its server needs node:http"); },
}));
`,
	} {
		if _, err := e.s.Import(ctx, []byte(code)); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.s.SetEnabled(ctx, "MainFailSlick", true); err != nil {
		t.Fatal(err)
	}
	p := newSlickPage(e.a)
	defer p.close()
	p.layoutTab("slick_plugins")
	p.open = "MainFailSlick"
	var mu sync.Mutex
	var drawn []string
	theme.DrawHook = func(s string) { mu.Lock(); drawn = append(drawn, s); mu.Unlock() }
	t.Cleanup(func() { theme.DrawHook = nil })
	want := "Its privileged half didn't start, so what it does there doesn't happen: its server needs node:http"
	e.until(10*time.Second, "the notice on its page", func() bool {
		mu.Lock()
		drawn = drawn[:0]
		mu.Unlock()
		gtx := ttContext(new(op.Ops), image.Pt(800, 1200))
		e.a.slackcFrame(gtx)
		p.layout(gtx, "slick_plugins")
		mu.Lock()
		defer mu.Unlock()
		return slices.ContainsFunc(drawn, func(s string) bool { return strings.Contains(s, want) })
	})
	for _, pl := range e.s.Settings().Plugins {
		if pl.ID == "MainFailSlick" && !pl.Running {
			t.Error("the renderer half stopped with its main half")
		}
	}
}

// dismissOverlays closes open menus (as Esc would).
func (e *svEnv) dismissOverlays() {
	for e.a.dismissTop() {
	}
	e.frame()
}

// svSelectedTab is the data-tab-key of ChatTabs' tab whose button is
// aria-selected in host tree tr ("" for none).
func svSelectedTab(tr string) string {
	var top any // a delivered tree is one node or a list of them
	if json.Unmarshal([]byte(tr), &top) != nil {
		return ""
	}
	nodes, ok := top.([]any)
	if !ok {
		nodes = []any{top}
	}
	var walk func(ns []any, key string) string
	walk = func(ns []any, key string) string {
		for _, x := range ns {
			n, _ := x.(map[string]any)
			if n == nil {
				continue
			}
			p, _ := n["p"].(map[string]any)
			k := key
			if v, ok := p["data-tab-key"].(string); ok {
				k = v
			}
			if sel := p["aria-selected"]; (sel == true || sel == "true") && k != "" { // a DOM attribute: the string
				return k
			}
			c, _ := n["c"].([]any)
			if got := walk(c, k); got != "" {
				return got
			}
		}
		return ""
	}
	return walk(nodes, "")
}

// topMenu is the topmost open menu.
func (e *svEnv) topMenu() *popups.Menu {
	for i := len(e.a.overlays) - 1; i >= 0; i-- {
		if m, ok := e.a.overlays[i].(*popups.Menu); ok && !m.Dismissed() {
			return m
		}
	}
	return nil
}

// menuIndex is the row keyed key, or whose Slack action id is key; -1
// for none.
func menuIndex(items []popups.MenuItem, key string) int {
	for i, it := range items {
		if it.Key == key || it.Action == key {
			return i
		}
	}
	return -1
}

// jsString is the string value of JS expression expr in the plugins'
// runtime.
func (e *svEnv) jsString(expr string) string {
	e.t.Helper()
	rt, err := e.a.Taut().JS()
	if err != nil {
		e.t.Fatal(err)
	}
	v, err := rt.Eval("probe.js", "String("+expr+")")
	if err != nil {
		e.t.Fatal(err)
	}
	return v.String()
}

// clickRect clicks the middle of window rectangle r and draws the frames
// that handle it.
func (e *svEnv) clickRect(r image.Rectangle) { e.clickRectMods(r, 0) }

// clickRectMods is clickRect with modifier keys held (Slack's split click).
func (e *svEnv) clickRectMods(r image.Rectangle, mods key.Modifiers) {
	e.t.Helper()
	c := r.Min.Add(r.Max).Div(2)
	p := f32.Pt(float32(c.X), float32(c.Y))
	e.clock += time.Second
	e.rt.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: e.clock, Modifiers: mods},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: e.clock + 10*time.Millisecond, Modifiers: mods},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: e.clock + 60*time.Millisecond, Modifiers: mods},
	)
	e.frame()
	e.frame()
}

// BetterBlock: a blocked person's DM disappears from the sidebar
// (ChannelSidebar's rows), the rest stays.
func TestSlickViewBetterBlock(t *testing.T) {
	e := newSV(t)
	if !hasStr(e.shownConvs(), svDM) {
		t.Fatalf("the DM isn't in the sidebar before blocking: %v", e.shownConvs())
	}
	e.install("BetterBlock", map[string]any{"blockedUsers": map[string]any{svAlice: "alice"}})
	e.until(5*time.Second, "ChannelSidebar patched", func() bool { return slackc.Patched("ChannelSidebar") })
	e.until(8*time.Second, "Alice's DM gone from the sidebar", func() bool {
		return e.a.sidebar.shown != nil && !hasStr(e.shownConvs(), svDM)
	})
	got := e.shownConvs()
	if !hasStr(got, ttGen) || !hasStr(got, svRandom) {
		t.Errorf("other rows went too: %v", got)
	}
	if n := len(e.a.sidebar.shown); n != len(e.a.sidebar.rows)-1 {
		t.Errorf("%d of %d rows shown, want all but the DM", n, len(e.a.sidebar.rows))
	}
	if _, ok := e.rowPos(svDM); ok {
		t.Error("the DM row is still laid out")
	}
}

// ShowSendingBot: its coachmark context wrap reaches the shell's empty
// ProgressiveDisclosureSuccessCoachmarkWrapper point, which draws nothing,
// and the shell keeps drawing.
func TestSlickViewShowSendingBot(t *testing.T) {
	e := newSV(t)
	e.install("ShowSendingBot", nil)
	e.until(5*time.Second, "the coachmark point patched", func() bool {
		return slackc.Patched("ProgressiveDisclosureSuccessCoachmarkWrapper")
	})
	e.until(8*time.Second, "the coachmark point rendered through the plugin", func() bool {
		return e.tree(`"n":"ProgressiveDisclosureSuccessCoachmarkWrapper"`) != ""
	})
	for i := 0; i < 5; i++ {
		e.frame()
	}
	if got := e.shownConvs(); !hasStr(got, ttGen) || !hasStr(got, svDM) {
		t.Errorf("sidebar rows %v", got)
	}
}

// Snappy through the shell: its transitions scan finds Slack's stylesheet
// in the page (buildSlick passes slackc.NativeTransitionsCSS) and its
// override reaches the CSS engine, so the natives that draw Slack's
// transitions change at once (the join banner's slide-down here); its
// main half turns the composer spellcheck off. Turning it off gives both
// back.
func TestSlickViewSnappy(t *testing.T) {
	e := newSV(t)
	slide := func() time.Duration {
		d, _ := slackc.CurrentScope().Child(clsJoinBanner, 1).Transition("transform", jbSlide, 0)
		return d
	}
	near := func(d time.Duration) bool { return d > jbSlide-time.Millisecond && d < jbSlide+time.Millisecond }
	if d := slide(); !near(d) {
		t.Fatalf("before Snappy the join banner slides in %v, want Slack's %v", d, jbSlide)
	}
	e.install("Snappy", map[string]any{"disableSpellcheck": true})
	e.until(15*time.Second, "the join banner's transition collapsed", func() bool { return slide() < time.Millisecond })
	e.until(5*time.Second, "the composer spellcheck off", func() bool { return !e.a.spellcheckOn() })

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := e.s.SetEnabled(ctx, "Snappy", false); err != nil {
		t.Fatal(err)
	}
	e.until(5*time.Second, "the composer spellcheck back on", e.a.spellcheckOn)
	e.until(5*time.Second, "the join banner's own transition back", func() bool { return near(slide()) })
}
