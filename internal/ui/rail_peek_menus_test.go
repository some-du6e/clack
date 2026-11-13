package ui

import (
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/views"
)

// The peeks' extras against internal/fakeslack (synthetic data only): the
// Activity peek's unread badges, and the rows' menus in every card with
// the card staying open while the pointer is in them (rail_tab_peek.go's
// eu). RAIL_PEEK_SHOTS_DIR saves screenshots.

// peekMenu is the newest menu open above key's card (nil for none).
func peekMenu(e *perfEnv, key string) *popups.Menu {
	p := e.a.tabPeek(key)
	if p.pop == nil {
		return nil
	}
	ms := e.a.tabPeekChildren(p.pop)
	if len(ms) == 0 {
		return nil
	}
	return ms[len(ms)-1]
}

func (e *perfEnv) rightClick(p image.Point) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonSecondary, Position: pt},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt},
	)
	e.frame()
	e.frame()
}

// hoverClick moves onto p (the row shows its hover actions), then clicks.
func (e *perfEnv) hoverClick(p image.Point) {
	e.movePtr(p)
	e.frame()
	e.click(p)
	e.frame()
	e.frame()
}

func peekMenuItem(m *popups.Menu, key string) (popups.MenuItem, bool) {
	for _, it := range m.Items() {
		if it.Key == key {
			return it, true
		}
	}
	return popups.MenuItem{}, false
}

// checkMenuKeepsPeek: with the pointer in the card's menu (outside the
// card where the menu is), the peek stays; leaving the menu for the
// window closes the peek, and the menu with it.
func checkMenuKeepsPeek(t *testing.T, e *perfEnv, key string, m *popups.Menu, away image.Point) {
	t.Helper()
	card := e.a.tabPeek(key).pop.Rect()
	r := m.Rect()
	in := r.Max.Sub(image.Pt(6, 6))
	if !in.In(card) && r.Max.X-6 > card.Max.X {
		in = image.Pt(r.Max.X-6, r.Min.Y+(r.Dy()/2))
	}
	e.movePtr(in)
	e.waitFor(60 * time.Millisecond)
	if !e.a.tabPeekOpen(key) {
		t.Fatalf("%s: the pointer in the row's menu closed the peek", key)
	}
	if !e.a.tabPeekInside(key) {
		t.Errorf("%s: eu not set with the pointer in the card's menu", key)
	}
	e.movePtr(away)
	e.waitFor(60 * time.Millisecond)
	e.frame()
	if e.a.tabPeekOpen(key) {
		t.Errorf("%s: leaving the menu for the window left the peek open", key)
	}
	if !m.Dismissed() {
		t.Errorf("%s: the peek closed and its menu stayed", key)
	}
}

// The Activity peek under the inbox: DMs, Mentions and Threads badges
// after the title (only the ones above 0), none without counts.
func TestRailPeekActivityBadges(t *testing.T) {
	e, _ := pagePeekEnvWith(t, func(fs *fakeslack.Server) {
		fs.SetActivityBadges(map[string]int{"dm": 2, "at_user": 3, "keyword": 1, "thread_v2": 0})
	})
	e.a.Core().SetExperimentGroup("activity_inbox", "on") // the test account's (account_experiments.json)
	e.settle(300 * time.Millisecond)
	away := image.Pt(e.size.X-20, e.size.Y-20)
	openTabPeekByHover(e, "activity", away)
	v := pageView[*views.Activity](e, "activity")
	if v == nil {
		t.Fatal("no Activity view")
	}
	e.until("the badges", 5*time.Second, func() bool { return v.PeekBadgeCounts() != [3]int{} })
	if got := v.PeekBadgeCounts(); got != [3]int{2, 4, 0} {
		t.Errorf("badges %v, want DMs 2, Mentions 4 (at_user + keyword), no Threads", got)
	}
	e.settle(400 * time.Millisecond)
	e.railPeekShot("activity-peek-badges-light")
	closeTabPeekAway(e, away)
	e.setDark(true)
	e.settle(200 * time.Millisecond)
	openTabPeekByHover(e, "activity", away)
	e.settle(400 * time.Millisecond)
	e.railPeekShot("activity-peek-badges-dark")
	closeTabPeekAway(e, away)
	e.setDark(false)

	// without the inbox ActivityPeekViewTabs isn't rendered
	e.a.Core().SetExperimentGroup("activity_inbox", "off")
	if got := v.PeekBadgeCounts(); got != [3]int{} {
		t.Errorf("badges %v without activity_inbox", got)
	}
	e.a.Core().SetExperimentGroup("activity_inbox", "on")
}

// Activity rows: a right-click opens the item menu over the card; the
// card stays while the pointer is in it; "Open in home" closes the peek.
func TestRailPeekActivityRowMenu(t *testing.T) {
	e := pagePeekEnv(t)
	a := e.a
	away := image.Pt(e.size.X-20, e.size.Y-20)
	p := openTabPeekByHover(e, "activity", away)
	v := pageView[*views.Activity](e, "activity")
	e.until("the activity on the card", 5*time.Second, func() bool { return v.PeekCount() > 0 })
	e.settle(300 * time.Millisecond)
	card := p.pop.Rect()
	// a row with a message (Open in home needs one): right-click down the
	// list until its menu has the row
	var row image.Point
	var m *popups.Menu
	for y := card.Min.Y + 44 + 60; y < card.Max.Y-20; y += 30 {
		e.rightClick(image.Pt(card.Min.X+150, y))
		mm := peekMenu(e, "activity")
		if mm == nil {
			continue
		}
		if !a.tabPeekOpen("activity") {
			t.Fatal("the right-click closed the peek")
		}
		if _, ok := peekMenuItem(mm, "open_in_home"); ok {
			row, m = image.Pt(card.Min.X+150, y), mm
			break
		}
		mm.Dismiss()
		e.frame()
	}
	if m == nil {
		t.Fatal("no row's right-click menu with Open in home on the card")
	}
	e.railPeekShot("activity-peek-row-menu-light")
	checkMenuKeepsPeek(t, e, "activity", m, away)

	// Open in home: maybeClosePeek
	openTabPeekByHover(e, "activity", away)
	e.rightClick(row)
	if m = peekMenu(e, "activity"); m == nil {
		t.Fatal("no menu the second time")
	}
	home, _ := peekMenuItem(m, "open_in_home")
	home.Do()
	e.frame()
	e.frame()
	if a.tabPeekOpen("activity") {
		t.Error("Open in home left the peek open")
	}
}

// DMs rows: Slack's hover actions (Mark as read while unread, the to-do
// button, ⋮) and the DMs tab's activity item menu, from the ⋮ and a
// right-click.
func TestRailPeekDMsRowMenu(t *testing.T) {
	e := pagePeekEnv(t)
	a := e.a
	away := image.Pt(e.size.X-20, e.size.Y-20)
	p := openTabPeekByHover(e, "dms", away)
	d := pageView[*views.DMs](e, "dms")
	e.until("the DMs on the card", 5*time.Second, func() bool { return d.PeekCount() > 0 })
	e.settle(300 * time.Millisecond)
	card := p.pop.Rect()
	// the ⋮: the bar's last button (top 4, right 12, padding 4)
	more := image.Pt(card.Max.X-12-4-16, card.Min.Y+44+4+4+16)
	e.hoverClick(more)
	m := peekMenu(e, "dms")
	if m == nil {
		t.Fatal("no menu from the row's ⋮")
	}
	labels := strings.Join(m.Labels(), "|")
	for _, want := range []string{"Remind me about this", "Copy", "Open in home"} {
		if !strings.Contains(labels, want) {
			t.Errorf("menu %q has no %q", labels, want)
		}
	}
	if strings.Contains(labels, "Mark as unread") {
		t.Errorf("menu %q: the DMs tab has no Mark as unread", labels)
	}
	if !a.tabPeekOpen("dms") {
		t.Fatal("the ⋮ closed the peek")
	}
	if cp, ok := peekMenuItem(m, "copy_link"); !ok || len(cp.Sub) < 2 || cp.Sub[0].Label != "Copy name" || cp.Sub[1].Label != "Copy link" {
		t.Errorf("Copy ▸ %+v, want Copy name, Copy link[, Copy huddle link]", cp.Sub)
	}
	e.railPeekShot("dms-peek-row-menu-light")
	checkMenuKeepsPeek(t, e, "dms", m, away)

	// a right-click on a row: the same menu at the pointer
	openTabPeekByHover(e, "dms", away)
	e.rightClick(image.Pt(card.Min.X+120, card.Min.Y+44+34))
	if m = peekMenu(e, "dms"); m == nil {
		t.Fatal("no menu after a right-click")
	}
	if !a.tabPeekOpen("dms") {
		t.Fatal("the right-click closed the peek")
	}
	// a menu that closes under the pointer leaves eu set (no mouseleave):
	// the card stays until the pointer comes back and leaves it
	r := m.Rect()
	e.movePtr(r.Max.Sub(image.Pt(6, 6)))
	m.Dismiss()
	e.frame()
	e.movePtr(away)
	e.waitFor(60 * time.Millisecond)
	if !a.tabPeekOpen("dms") {
		t.Error("the peek closed though its menu closed under the pointer (Slack's eu stays set)")
	}
	e.movePtr(image.Pt(card.Min.X+100, card.Min.Y+200))
	e.movePtr(away)
	e.waitFor(60 * time.Millisecond)
	if a.tabPeekOpen("dms") {
		t.Error("entering and leaving the card didn't close it")
	}
}

// Later rows: the ⋮ opens SavedItemActionsMenu (later_todos, peek
// mode): Mark complete, the reminder submenu, Move to saved, Open in Home,
// Clear; no split view in the peek.
func TestRailPeekLaterRowMenu(t *testing.T) {
	e := pagePeekEnv(t)
	a := e.a
	a.Core().SetExperimentGroup("later_todos", "on") // the test account's
	e.settle(300 * time.Millisecond)
	away := image.Pt(e.size.X-20, e.size.Y-20)
	p := openTabPeekByHover(e, "later", away)
	l := pageView[*views.Later](e, "later")
	e.until("the to-dos", 5*time.Second, func() bool { return l.PeekCount() > 0 })
	e.settle(300 * time.Millisecond)
	card := p.pop.Rect()
	more := image.Pt(card.Max.X-12-4-16, card.Min.Y+44+12+4+16) // .p-peek_card__later_list: top 12, right 12
	e.hoverClick(more)
	m := peekMenu(e, "later")
	if m == nil {
		t.Fatal("no menu from the row's ⋮")
	}
	got := m.Labels()
	if got[0] != "Mark complete" || !slices.Contains(got, "Move to saved") || got[len(got)-1] != "Clear" {
		t.Errorf("menu %v, want Mark complete … Move to saved[, Open in Home], ---, Clear", got)
	}
	if slices.Contains(got, "Open in split view") {
		t.Errorf("menu %v: no split view in the peek", got)
	}
	if !(slices.Contains(got, "Edit reminder") || slices.Contains(got, "Add reminder")) {
		t.Errorf("menu %v: no reminder submenu", got)
	}
	if !a.tabPeekOpen("later") {
		t.Fatal("the ⋮ closed the peek")
	}
	e.railPeekShot("later-peek-row-menu-light")
	checkMenuKeepsPeek(t, e, "later", m, away)
	// Open in Home: maybeClosePeek
	openTabPeekByHover(e, "later", away)
	e.hoverClick(more)
	if m = peekMenu(e, "later"); m == nil {
		t.Fatal("no menu the second time")
	}
	home, ok := peekMenuItem(m, "open_in_home")
	if !ok {
		t.Logf("the first to-do isn't a message (%v): no Open in Home", m.Labels())
		return
	}
	home.Do()
	e.frame()
	if a.tabPeekOpen("later") {
		t.Error("Open in Home left the peek open")
	}
}

// Agents & tools: a session row's ⋮ (shown on hover) opens the session
// menu; Pin and Close conversation write agents.threads.*.
func TestRailPeekAgentsRowMenu(t *testing.T) {
	e, fs := railPeekEnv(t)
	a := e.a
	// the test account's (account_experiments.json): the session strings
	// and thread naming
	a.Core().SetExperimentGroup("agent_ux_august2026_release_translations", "on")
	a.Core().SetExperimentGroup("agent_channel_thread_naming", "on")
	away := image.Pt(e.size.X-20, e.size.Y-20)
	p := openPlatformPeek(e, away)
	v := a.agPeek
	e.until("the sessions", 5*time.Second, func() bool { return v.sessFetched })
	e.settle(300 * time.Millisecond)
	card := p.pop.Rect()
	s0 := v.sessions[0] // pinned "Plan the Q4 launch"
	key := s0.Kind + "/" + s0.Channel + "/" + s0.TS
	x := card.Max.X - 16 - 12
	var at image.Point
	for y := card.Min.Y + 44; y < card.Min.Y+300; y += 6 {
		e.movePtr(image.Pt(x, y))
		e.frame()
		if v.menuBtn(key).Hovered() {
			at = image.Pt(x, y)
			break
		}
	}
	if at == (image.Point{}) {
		t.Fatal("no ⋮ on the first session's row")
	}
	e.railPeekShot("agents-peek-row-hover-light")
	e.click(at)
	e.frame()
	e.frame()
	m := peekMenu(e, "platform")
	if m == nil {
		t.Fatal("no menu from the session's ⋮")
	}
	want := []string{"Pin session", "Mute session", "Rename session", "---", "Open in split view", "Copy link", "---", "Close conversation"}
	if got := m.Labels(); !slices.Equal(got, want) {
		t.Errorf("menu %v, want %v", got, want)
	}
	if it, _ := peekMenuItem(m, "pin"); !it.Checked {
		t.Error("Pin session unchecked on a pinned session")
	}
	if !a.tabPeekOpen("platform") {
		t.Fatal("the ⋮ closed the peek")
	}
	e.railPeekShot("agents-peek-row-menu-light")
	pin, _ := peekMenuItem(m, "pin")
	pin.Do()
	e.until("agents.threads.unpin", 5*time.Second, func() bool {
		for _, w := range fs.AgentSessionEdits() {
			if w.Get("method") == "agents.threads.unpin" && w.Get("channel") == s0.Channel && w.Get("thread_ts") == s0.TS {
				return true
			}
		}
		return false
	})
	if v.sessions[0].Pinned {
		t.Error("the row is still pinned after Unpin")
	}
	m.Dismiss()
	e.frame()
	m = popups.NewMenu(a, at, v.sessionMenu(v.sessions[0]))
	cl, _ := peekMenuItem(m, "close")
	n := len(v.sessions)
	cl.Do()
	e.frame()
	if len(v.sessions) != n-1 {
		t.Errorf("%d sessions after Close conversation, want %d", len(v.sessions), n-1)
	}
	e.until("agents.threads.archive", 5*time.Second, func() bool {
		for _, w := range fs.AgentSessionEdits() {
			if w.Get("method") == "agents.threads.archive" && w.Get("thread_ts") == s0.TS && w.Get("reason") == "agent_session_sidebar" {
				return true
			}
		}
		return false
	})
	// a channel session has no Mute or Rename
	for _, s := range v.sessions {
		if s.Kind == "channel" {
			got := popups.NewMenu(a, at, v.sessionMenu(s)).Labels()
			if slices.Contains(got, "Mute session") || slices.Contains(got, "Rename session") {
				t.Errorf("channel session menu %v", got)
			}
		}
	}
	if err := popups.AgentThreadNameError("a:b"); err != "invalid_name_specials" {
		t.Errorf("a:b → %q", err)
	}
	if err := popups.AgentThreadNameError("..."); err != "invalid_name_punctuation" {
		t.Errorf("... → %q", err)
	}
	if err := popups.AgentThreadNameError(strings.Repeat("x", 81)); err != "name_too_long" {
		t.Errorf("81 chars → %q", err)
	}
}

// Light and dark shots of the menus over the cards.
func TestRailPeekMenusShots(t *testing.T) {
	e := pagePeekEnv(t)
	e.a.Core().SetExperimentGroup("activity_inbox", "on")
	e.a.Core().SetExperimentGroup("later_todos", "on")
	away := image.Pt(e.size.X-20, e.size.Y-20)
	for _, dark := range []bool{false, true} {
		e.setDark(dark)
		e.settle(200 * time.Millisecond)
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		p := openTabPeekByHover(e, "dms", away)
		e.waitFor(1200 * time.Millisecond)
		card := p.pop.Rect()
		e.hoverClick(image.Pt(card.Max.X-12-4-16, card.Min.Y+44+4+4+16))
		e.railPeekShot("menus-dms-" + mode)
		e.key("Escape", 0)
		closeTabPeekAway(e, away)
		p = openTabPeekByHover(e, "later", away)
		e.waitFor(1200 * time.Millisecond)
		card = p.pop.Rect()
		e.hoverClick(image.Pt(card.Max.X-12-4-16, card.Min.Y+44+12+4+16))
		e.railPeekShot("menus-later-" + mode)
		e.key("Escape", 0)
		closeTabPeekAway(e, away)
		p = openTabPeekByHover(e, "activity", away)
		e.waitFor(1200 * time.Millisecond)
		card = p.pop.Rect()
		e.rightClick(image.Pt(card.Min.X+150, card.Min.Y+44+130))
		e.railPeekShot("menus-activity-" + mode)
		e.key("Escape", 0)
		closeTabPeekAway(e, away)
	}
}
