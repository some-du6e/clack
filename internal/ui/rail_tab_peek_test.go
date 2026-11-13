package ui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// The rail's tab peeks against internal/fakeslack (Size.AgentSessions,
// Size.Workflows): synthetic sessions and workflows only, nothing from a
// real workspace. RAIL_PEEK_SHOTS_DIR saves screenshots.

func railPeekEnv(t *testing.T) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.AgentSessions, sz.Workflows = true, true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("RAIL_PEEK_SHOTS_DIR") != "" && e.gpu == nil {
		if w, err := pixeltest.NewWindow(e.size.X, e.size.Y); err == nil {
			e.gpu = w
		} else {
			t.Logf("no GPU renderer (%v): no shots", err)
		}
	}
	e.waitReady()
	// Agents & tools in the rail (Y2EP: agent_tasks), tabbed (HLQ9 rh)
	e.a.Core().SetExperimentGroup("agent_tasks", "on")
	e.a.Core().SetExperimentGroup("agents_landing_page", "on")
	e.settle(600 * time.Millisecond)
	return e, fs
}

// railPeekShot saves the last frame (RAIL_PEEK_SHOTS_DIR only).
func (e *perfEnv) railPeekShot(name string) {
	dir := os.Getenv("RAIL_PEEK_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	e.frame()
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		e.tb.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		e.tb.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		e.tb.Fatal(err)
	}
}

func (e *perfEnv) waitFor(d time.Duration) {
	for dl := time.Now().Add(d); time.Now().Before(dl); {
		e.frame()
		time.Sleep(3 * time.Millisecond)
	}
}

func platformPeek(e *perfEnv) *tabPeekState {
	e.until("the Agents & tools button", 5*time.Second, func() bool {
		p := e.a.tabPeek("platform")
		return p.mounted && !p.trig.Empty()
	})
	return e.a.tabPeek("platform")
}

// openPlatformPeek hovers Agents & tools until its peek is open.
func openPlatformPeek(e *perfEnv, away image.Point) *tabPeekState {
	p := platformPeek(e)
	e.movePtr(away)
	e.movePtr(p.trig.Min.Add(p.trig.Size().Div(2)))
	e.until("the Agents & tools peek", 3*time.Second, func() bool { return e.a.tabPeekOpen("platform") })
	e.settle(200 * time.Millisecond) // past the animate-in
	return p
}

// Karim, 2026-11-12: "hovering over this isnt very agentic of you". In
// Slack, hovering the rail's Agents & tools opens AutomationsPeekView
// (TabPeekTrigger): 300 ms in, no scrim or focus, Agent conversations |
// Workflows; leaving the safe area or Escape closes it; a press on the
// button closes it and the click goes on to the tab.
func TestRailPeekAgentsHover(t *testing.T) {
	e, fs := railPeekEnv(t)
	a := e.a
	p := platformPeek(e)
	trig := p.trig
	mid := trig.Min.Add(trig.Size().Div(2))
	away := image.Pt(trig.Max.X+700, min(trig.Max.Y+300, e.size.Y-10))

	// the trigger delay
	e.movePtr(away)
	e.movePtr(mid)
	t0 := time.Now()
	e.waitFor(150 * time.Millisecond)
	if a.tabPeekOpen("platform") {
		t.Fatal("the peek opened before the 300 ms trigger delay")
	}
	e.until("the hover peek", 3*time.Second, func() bool { return a.tabPeekOpen("platform") })
	if d := time.Since(t0); d < tabPeekDelay {
		t.Fatalf("opened after %v, before the %v delay", d, tabPeekDelay)
	}
	if e.r.Source().Focused(p.pop) {
		t.Error("the hover peek took the keyboard focus")
	}
	e.settle(200 * time.Millisecond)
	card := p.pop.Rect()
	// right-bottom, offsetX 6 (offsetY -44, kept in the window)
	if card.Min.X != trig.Max.X+6 {
		t.Errorf("card at x %d, want the button's right edge + 6 = %d", card.Min.X, trig.Max.X+6)
	}
	if w := card.Dx(); w < 360 || w > 400 {
		t.Errorf("card %d wide, want 360…400 (.p-peek_card--automations)", w)
	}
	if want := min(int(float32(e.size.Y)*.65), e.size.Y-8); card.Dy() != want {
		t.Errorf("card %d tall, want 65vh = %d", card.Dy(), want)
	}

	// Agent conversations: ai.alpha.agents.sessions.list, archived left out
	v := a.agPeek
	if v.tab != agTabSessions {
		t.Errorf("opened on %q, want Agent conversations", v.tab)
	}
	e.until("the sessions", 5*time.Second, func() bool { return v.sessFetched })
	if !slices.Contains(fs.AgentSessionsReasons(), "recent_sessions_sidebar") {
		t.Errorf("sessions.list reasons %v, want recent_sessions_sidebar", fs.AgentSessionsReasons())
	}
	if len(v.sessions) != 6 {
		t.Fatalf("%d sessions, want 6 (the archived one left out)", len(v.sessions))
	}
	want := []struct{ title, sub string }{
		{"Plan the Q4 launch", "Updated 5 hours ago"},
		{"agent-fix-login", "Updated 3 hours ago"},
		{"Summarize the week", "Updated "},
		{"Draft the onboarding doc", "Working…"},
		{"Book the offsite", "Needs your input"},
		{"Triage the open bugs", "Updated 4 days ago"},
	}
	for i, w := range want {
		title, sub, _ := v.sessionText(v.sessions[i])
		if title != w.title || !strings.HasPrefix(sub, w.sub) {
			t.Errorf("session %d: %q / %q, want %q / %q", i, title, sub, w.title, w.sub)
		}
	}
	if _, _, unread := v.sessionText(v.sessions[0]); unread {
		t.Error("a session read up to its latest reply is unread")
	}
	if _, _, unread := v.sessionText(v.sessions[2]); !unread {
		t.Error("a session with a reply after its last read isn't unread")
	}
	if !v.sessions[0].Pinned || v.sessions[1].Pinned {
		t.Error("is_pinned lost")
	}
	e.railPeekShot("agents-peek-sessions")

	// the card is in the safe area; out of it, it closes at once
	e.movePtr(image.Pt(card.Min.X+20, card.Min.Y+card.Dy()/2))
	e.waitFor(60 * time.Millisecond)
	if !a.tabPeekOpen("platform") {
		t.Fatal("moving onto the card closed the peek")
	}
	e.movePtr(away)
	e.waitFor(60 * time.Millisecond)
	if a.tabPeekOpen("platform") {
		t.Fatal("the hover peek stayed open after the pointer left the button and the card")
	}

	// leaving before the delay cancels it
	e.movePtr(mid)
	e.waitFor(100 * time.Millisecond)
	e.movePtr(away)
	e.waitFor(400 * time.Millisecond)
	if a.tabPeekOpen("platform") {
		t.Fatal("the peek opened after the pointer left before the delay")
	}

	// Escape closes it; on the button it opens again only after two moves
	openPlatformPeek(e, away)
	e.key(key.NameEscape, 0)
	e.frame()
	e.frame()
	if a.tabPeekOpen("platform") {
		t.Fatal("Escape left the peek open")
	}
	e.movePtr(mid.Add(image.Pt(1, 0)))
	e.waitFor(450 * time.Millisecond)
	if a.tabPeekOpen("platform") {
		t.Fatal("one move on the button after Escape reopened the peek")
	}
	e.movePtr(mid.Add(image.Pt(2, 0)))
	e.until("the peek after the second move", 3*time.Second, func() bool { return a.tabPeekOpen("platform") })

	// a press on the button closes it, and the click goes to the tab
	peekPop := p.pop
	e.click(mid)
	e.frame()
	e.frame()
	if a.tabPeekOpen("platform") {
		t.Fatal("a click on the button left the peek open")
	}
	reached := false // the tab's click: Clack's tools menu stands in for Slack's page
	for _, o := range a.overlays {
		if d, ok := o.(interface{ Dismissed() bool }); ok && o != Overlay(peekPop) && !d.Dismissed() {
			reached = true
		}
	}
	if !reached {
		t.Error("the click on the button didn't reach the tab")
	}
	e.waitFor(450 * time.Millisecond)
	if a.tabPeekOpen("platform") {
		t.Fatal("the peek reopened under the pointer after the click")
	}
	a.dismissTop()
	e.frame()
}

// A row closes the peek and opens its conversation: a thread session's
// thread, a channel session's channel; a workflow its details view.
func TestRailPeekAgentsClickThrough(t *testing.T) {
	e, _ := railPeekEnv(t)
	a := e.a
	away := image.Pt(e.size.X-20, e.size.Y-20)
	p := openPlatformPeek(e, away)
	v := a.agPeek
	e.until("the sessions", 5*time.Second, func() bool { return v.sessFetched })
	e.settle(100 * time.Millisecond)
	card := p.pop.Rect()
	body := card.Min.Y + 44 + 26                 // the header, the tabs
	first := body + (16 + 19 + 8) + (12+41+12)/2 // "Pinned agent conversations", then its row
	e.click(image.Pt(card.Min.X+120, first))
	e.frame()
	e.frame()
	if a.tabPeekOpen("platform") {
		t.Error("the row left the peek open")
	}
	if a.secondary.kind != secThread || a.secondary.channel != fakeslack.AgentSessionsDMID || a.secondary.ts != fakeslack.AgentSessionTS(0) {
		t.Errorf("thread pane %v %s/%s, want the pinned session's thread", a.secondary.kind, a.secondary.channel, a.secondary.ts)
	}

	// the channel session: the first Recent row
	p = openPlatformPeek(e, away)
	card = p.pop.Rect()
	second := body + (16 + 19 + 8) + (12 + 41 + 12) + (16 + 19 + 8) + (12+41+12)/2
	e.click(image.Pt(card.Min.X+120, second))
	e.frame()
	e.frame()
	e.settle(100 * time.Millisecond)
	if r := a.router.Current(); r.Channel() != fakeslack.AgentSessionsChannelID {
		t.Errorf("route %q, want the channel session's channel", r)
	}

	// Workflows: workflows.triggers.listRecentlyRunForUser, newest first
	p = openPlatformPeek(e, away)
	card = p.pop.Rect()
	gtx := e.context()
	w1 := a.th.Measure(gtx, 13, theme.Bold, "Agent conversations") + 8
	w2 := a.th.Measure(gtx, 13, theme.Bold, "Workflows") + 8
	e.click(image.Pt(card.Min.X+12+w1+8+w2/2, card.Min.Y+44+13))
	e.frame()
	e.frame()
	if v.tab != agTabWorkflows {
		t.Fatalf("tab %q after clicking Workflows", v.tab)
	}
	e.until("the workflows", 5*time.Second, func() bool { return v.wfFetched })
	var ids []string
	for _, tr := range v.trigs {
		ids = append(ids, tr.ID)
	}
	if !slices.Equal(ids, fakeslack.RecentlyRunTriggers) {
		t.Errorf("workflows %v, want %v", ids, fakeslack.RecentlyRunTriggers)
	}
	e.settle(150 * time.Millisecond)
	e.railPeekShot("agents-peek-workflows")
	e.click(image.Pt(card.Min.X+120, body+(8+22+18+8)/2))
	e.frame()
	e.frame()
	if a.tabPeekOpen("platform") {
		t.Error("the workflow row left the peek open")
	}
	if pane, ok := a.secondary.view.(*wfDetailsPane); !ok || a.secondary.kind != secWorkflow || pane.trigger != fakeslack.TriggerDeploy {
		t.Errorf("secondary %v, want Run deploy's workflow details", a.secondary.kind)
	}
	// the next open starts on Agent conversations again
	openPlatformPeek(e, away)
	if v.tab != agTabSessions {
		t.Errorf("reopened on %q, want Agent conversations", v.tab)
	}
}

// One peek at a time (activePeekId): More's opening closes Agents &
// tools', and the other way round.
func TestRailPeekOneAtATime(t *testing.T) {
	e, _ := railPeekEnv(t)
	a := e.a
	away := image.Pt(e.size.X-20, e.size.Y-20)
	openPlatformPeek(e, away)
	a.openRailMorePeek(true)
	e.frame()
	if a.tabPeekOpen("platform") {
		t.Error("More's menu opened over the Agents & tools peek")
	}
	a.peek.menu.Dismiss()
	e.frame()
}

// Darker sidebars (Preferences › Appearance), Karim 2026-11-12: "this
// makes stuff wierd". Slack's scopes (udDV, y4F2): at a dark brightness
// the rail and top nav are inverted (white text) with the option off too,
// the sidebar only with it on; at brightness 3 or less nothing inverts
// and the option isn't offered. Shots in light and dark, on and off.
func TestDarkerSidebarsShots(t *testing.T) {
	e, _ := railPeekEnv(t)
	a := e.a
	set := func(inv bool, b int) { // as the account's ia_theme says (no write)
		th := theme.DefaultIATheme
		th.SidebarInverted, th.Brightness = inv, b
		a.setAcct(acctTheme{set: true, t: th, gradients: true})
		e.settle(300 * time.Millisecond)
	}
	for _, dark := range []bool{false, true} {
		e.setDark(dark)
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, inv := range []bool{true, false} {
			set(inv, 6)
			c := a.th.C
			if !dark {
				if c.ChromeText != theme.LightPalette.ChromeText {
					t.Errorf("light, darker %v: rail text %v, want the inverted scope's white", inv, c.ChromeText)
				}
				if got := a.sbInverted(); got != inv {
					t.Errorf("light, darker %v: sidebar inverted %v", inv, got)
				}
				if a.scopeMod() != 1 || a.sidebarScopeMod() != map[bool]uint64{true: 1, false: 0}[inv] {
					t.Errorf("light, darker %v: scope mods %d %d", inv, a.scopeMod(), a.sidebarScopeMod())
				}
			} else if a.sbInverted() || a.scopeMod() != 0 {
				t.Errorf("dark, darker %v: an inverted scope", inv)
			}
			name := "darker-on"
			if !inv {
				name = "darker-off"
			}
			e.railPeekShot(name + "-" + mode)
		}
	}
	// brightness 3 with sidebarInverted stored: nothing inverts (the old
	// washed-out pink sidebar and rail)
	e.setDark(false)
	set(true, 3)
	if a.sbInverted() || a.scopeMod() != 0 {
		t.Error("brightness 3 kept an inverted scope")
	}
	if a.th.C.SidebarTextUnread == (theme.LightPalette.SidebarTextUnread) {
		t.Error("brightness 3: the sidebar's unread text is still the inverted white")
	}
	e.railPeekShot("darker-on-brightness3-light")
	// and the peek over the default theme, light and dark
	set(true, 6)
	away := image.Pt(e.size.X-20, e.size.Y-20)
	for _, dark := range []bool{false, true} {
		e.setDark(dark)
		e.settle(200 * time.Millisecond)
		openPlatformPeek(e, away)
		e.until("the sessions", 5*time.Second, func() bool { return a.agPeek.sessFetched })
		e.settle(300 * time.Millisecond) // avatars
		e.railPeekShot("agents-peek-" + map[bool]string{false: "light", true: "dark"}[dark])
		e.movePtr(away)
		e.waitFor(60 * time.Millisecond)
	}
}
