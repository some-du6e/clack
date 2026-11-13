package ui

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/views"
)

// The rail's DMs, Activity, Later and Files peeks (rail_peek_pages.go)
// against internal/fakeslack: synthetic DMs, activity, to-dos and files
// only. RAIL_PEEK_SHOTS_DIR saves screenshots.

func pagePeekEnv(t *testing.T) *perfEnv {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.SetPrefs(map[string]any{"has_starred_item": true}) // the Later tab (OpEF shouldShowLaterLink)
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("RAIL_PEEK_SHOTS_DIR") != "" && e.gpu == nil {
		if w, err := pixeltest.NewWindow(e.size.X, e.size.Y); err == nil {
			e.gpu = w
		} else {
			t.Logf("no GPU renderer (%v): no shots", err)
		}
	}
	e.waitReady()
	e.settle(600 * time.Millisecond)
	return e
}

// tabTrigger waits for key's tab button in the rail.
func tabTrigger(e *perfEnv, key string) *tabPeekState {
	e.until("the "+key+" button", 5*time.Second, func() bool {
		p := e.a.tabPeek(key)
		return p.mounted && !p.trig.Empty()
	})
	return e.a.tabPeek(key)
}

// openTabPeekByHover hovers key's button until its peek is open.
func openTabPeekByHover(e *perfEnv, key string, away image.Point) *tabPeekState {
	p := tabTrigger(e, key)
	e.movePtr(away)
	e.movePtr(p.trig.Min.Add(p.trig.Size().Div(2)))
	e.until("the "+key+" peek", 3*time.Second, func() bool { return e.a.tabPeekOpen(key) })
	e.settle(200 * time.Millisecond) // past the animate-in
	return p
}

func closeTabPeekAway(e *perfEnv, away image.Point) {
	e.movePtr(away)
	e.waitFor(60 * time.Millisecond)
}

// checkPeekCard: right-bottom of the button (offsetX 6), 360 wide, 65vh.
func checkPeekCard(t *testing.T, e *perfEnv, key string, p *tabPeekState) image.Rectangle {
	t.Helper()
	card := p.pop.Rect()
	if card.Min.X != p.trig.Max.X+6 {
		t.Errorf("%s: card at x %d, want the button's right edge + 6 = %d", key, card.Min.X, p.trig.Max.X+6)
	}
	if card.Dx() != 360 {
		t.Errorf("%s: card %d wide, want 360 (.p-peek_card--as-card)", key, card.Dx())
	}
	if want := min(int(float32(e.size.Y)*.65), e.size.Y-8); card.Dy() != want {
		t.Errorf("%s: card %d tall, want 65vh = %d", key, card.Dy(), want)
	}
	if e.r.Source().Focused(p.pop) {
		t.Errorf("%s: the hover peek took the keyboard focus", key)
	}
	return card
}

func pageView[T any](e *perfEnv, r Route) T {
	var zero T
	v, ok := e.a.views[r].(T)
	if !ok {
		return zero
	}
	return v
}

// The DMs peek: 300 ms in, "Direct messages" with the tab's Unreads
// toggle, the DMs on the card; a row closes it and opens the DM in the
// DMs tab.
func TestRailPeekDMs(t *testing.T) {
	e := pagePeekEnv(t)
	a := e.a
	away := image.Pt(e.size.X-20, e.size.Y-20)
	p := tabTrigger(e, "dms")
	mid := p.trig.Min.Add(p.trig.Size().Div(2))
	e.movePtr(away)
	e.movePtr(mid)
	t0 := time.Now()
	e.waitFor(150 * time.Millisecond)
	if a.tabPeekOpen("dms") {
		t.Fatal("the DMs peek opened before the 300 ms trigger delay")
	}
	e.until("the DMs peek", 3*time.Second, func() bool { return a.tabPeekOpen("dms") })
	if d := time.Since(t0); d < tabPeekDelay {
		t.Fatalf("opened after %v, before the %v delay", d, tabPeekDelay)
	}
	e.settle(200 * time.Millisecond)
	card := checkPeekCard(t, e, "dms", p)
	d := pageView[*views.DMs](e, "dms")
	if d == nil {
		t.Fatal("the peek didn't use the DMs tab's view")
	}
	if title := d.PeekTitle(); title != "Direct messages" {
		t.Errorf("header %q, want Direct messages", title)
	}
	e.until("the DMs on the card", 5*time.Second, func() bool { return d.PeekCount() > 0 })
	e.settle(300 * time.Millisecond) // avatars
	e.railPeekShot("dms-peek-light")
	if r := a.router.Current(); r.Page() == "dms" {
		t.Fatalf("route %q: the peek is for another tab", r)
	}

	// the header's toggle is the tab's Unreads filter
	toggle := image.Pt(card.Max.X-8-14, card.Min.Y+22) // the switch, at the header's right
	e.click(toggle)
	e.frame()
	e.frame()
	if !d.Unreads() {
		t.Error("the Unreads toggle on the card didn't set the DMs filter")
	}
	if !a.tabPeekOpen("dms") {
		t.Fatal("the toggle closed the peek")
	}
	e.click(toggle)
	e.frame()
	e.frame()
	if d.Unreads() {
		t.Fatal("the second click left Unreads on")
	}
	e.until("all DMs again", 3*time.Second, func() bool { return d.PeekCount() > 0 })

	// a row: the peek closes, the DM opens in the DMs tab
	first := d.PeekFirst()
	e.click(image.Pt(card.Min.X+120, card.Min.Y+44+34))
	e.frame()
	e.frame()
	if a.tabPeekOpen("dms") {
		t.Error("the row left the peek open")
	}
	if r := a.router.Current(); r.Page() != "dms" {
		t.Errorf("route %q, want the DMs tab", r)
	}
	if got := d.Selected(); got != first {
		t.Errorf("DMs tab selected %q, want the clicked DM %q", got, first)
	}
	// no peek on the tab you're on
	e.movePtr(away)
	e.movePtr(mid)
	e.waitFor(450 * time.Millisecond)
	if a.tabPeekOpen("dms") {
		t.Error("the DMs peek opened on the DMs tab")
	}
}

// The Activity peek: "Activity", the page's Unreads toggle and its list;
// a row closes it and opens the item in the Activity tab. With Activity in
// Home (activity_in_home_prototype) Home has the peek.
func TestRailPeekActivity(t *testing.T) {
	e := pagePeekEnv(t)
	a := e.a
	away := image.Pt(e.size.X-20, e.size.Y-20)
	p := openTabPeekByHover(e, "activity", away)
	card := checkPeekCard(t, e, "activity", p)
	v := pageView[*views.Activity](e, "activity")
	if v == nil {
		t.Fatal("the peek didn't use the Activity tab's view")
	}
	if v.PeekTitle() != "Activity" {
		t.Errorf("header %q", v.PeekTitle())
	}
	e.until("the activity on the card", 5*time.Second, func() bool { return v.PeekCount() > 0 })
	e.settle(300 * time.Millisecond)
	e.railPeekShot("activity-peek-light")
	if a.hasTabPeek("home") {
		t.Error("Home has a peek without activity_in_home_prototype")
	}
	e.click(image.Pt(card.Min.X+150, card.Min.Y+44+40))
	e.frame()
	e.frame()
	if a.tabPeekOpen("activity") {
		t.Error("the row left the peek open")
	}
	if r := a.router.Current(); r.Page() != "activity" {
		t.Errorf("route %q, want the Activity tab", r)
	}

	// Activity in Home: Home's button gets ActivityPeekTrigger
	a.Core().SetExperimentGroup("activity_in_home_prototype", "on")
	e.settle(400 * time.Millisecond)
	if !a.hasTabPeek("home") {
		t.Fatal("no Home peek with activity_in_home_prototype")
	}
	a.Navigate("dms")
	e.settle(300 * time.Millisecond)
	openTabPeekByHover(e, "home", away)
	e.railPeekShot("activity-in-home-peek-light")
	closeTabPeekAway(e, away)
	a.Core().ClearExperimentGroup("activity_in_home_prototype")
}

// The Later peek: the tab's name and "{count} in progress", the to-dos on
// the card; a row closes it and shows the to-do in the To-dos tab.
func TestRailPeekLater(t *testing.T) {
	e := pagePeekEnv(t)
	a := e.a
	away := image.Pt(e.size.X-20, e.size.Y-20)
	p := openTabPeekByHover(e, "later", away)
	card := checkPeekCard(t, e, "later", p)
	l := pageView[*views.Later](e, "later")
	if l == nil {
		t.Fatal("the peek didn't use the To-dos tab's view")
	}
	e.until("the to-dos loaded", 5*time.Second, func() bool { return l.PeekCount() >= 0 })
	e.settle(300 * time.Millisecond)
	e.railPeekShot("later-peek-light")
	n := l.PeekCount()
	if sub := l.PeekSubtitle(); (n > 0) != (sub != "") {
		t.Errorf("%d in progress, subtitle %q", n, sub)
	}
	if n == 0 {
		t.Log("no to-dos in the fixture: the empty state only")
		return
	}
	e.click(image.Pt(card.Min.X+150, card.Min.Y+44+40))
	e.frame()
	e.frame()
	if a.tabPeekOpen("later") {
		t.Error("the row left the peek open")
	}
	if r := a.router.Current(); r.Page() != "later" {
		t.Errorf("route %q, want the To-dos tab", r)
	}
}

// The Files peek: "Files" with the Starred toggle, recently viewed files;
// a row opens the file and closes the peek.
func TestRailPeekFiles(t *testing.T) {
	e := pagePeekEnv(t)
	a := e.a
	away := image.Pt(e.size.X-20, e.size.Y-20)
	p := openTabPeekByHover(e, "files", away)
	card := checkPeekCard(t, e, "files", p)
	f := pageView[*views.Files](e, "files")
	if f == nil {
		t.Fatal("the peek didn't use the Files tab's view")
	}
	if f.PeekTitle() != "Files" {
		t.Errorf("header %q", f.PeekTitle())
	}
	e.until("the files loaded", 5*time.Second, func() bool { return f.PeekCount() >= 0 })
	e.settle(300 * time.Millisecond)
	e.railPeekShot("files-peek-light")
	// Starred
	e.click(image.Pt(card.Max.X-8-14, card.Min.Y+22))
	e.frame()
	e.frame()
	if !f.PeekStarred() {
		t.Error("the Starred toggle didn't switch the list")
	}
	e.until("the starred files", 5*time.Second, func() bool { return f.PeekCount() >= 0 })
	e.settle(200 * time.Millisecond)
	e.railPeekShot("files-peek-starred-light")
	e.click(image.Pt(card.Max.X-8-14, card.Min.Y+22))
	e.frame()
	e.until("the recent files", 5*time.Second, func() bool { return !f.PeekStarred() && f.PeekCount() >= 0 })
	if f.PeekCount() == 0 {
		t.Log("no files in the fixture: the empty state only")
		return
	}
	e.click(image.Pt(card.Min.X+150, card.Min.Y+44+31))
	e.frame()
	e.frame()
	if a.tabPeekOpen("files") {
		t.Error("the row left the peek open")
	}
}

// One peek at a time: opening another tab's closes the open one, and
// More's menu closes them too.
func TestRailPeekPagesOneAtATime(t *testing.T) {
	e := pagePeekEnv(t)
	a := e.a
	away := image.Pt(e.size.X-20, e.size.Y-20)
	keys := []string{"dms", "activity", "later", "files"}
	for i, k := range keys {
		openTabPeekByHover(e, k, away)
		for _, o := range keys[:i] {
			if a.tabPeekOpen(o) {
				t.Errorf("%s's peek still open with %s's", o, k)
			}
		}
		if got := a.anyTabPeekOpen(); got != k {
			t.Errorf("open peek %q, want %q", got, k)
		}
	}
	a.openRailMorePeek(true)
	e.frame()
	if k := a.anyTabPeekOpen(); k != "" {
		t.Errorf("More's menu opened over the %s peek", k)
	}
	a.peek.menu.Dismiss()
	e.frame()
}

// Light and dark shots of the four cards.
func TestRailPeekPagesShots(t *testing.T) {
	e := pagePeekEnv(t)
	away := image.Pt(e.size.X-20, e.size.Y-20)
	for _, dark := range []bool{false, true} {
		e.setDark(dark)
		e.settle(200 * time.Millisecond)
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		for _, k := range []string{"dms", "activity", "later", "files"} {
			openTabPeekByHover(e, k, away)
			e.waitFor(1200 * time.Millisecond) // the lists and their avatars
			e.railPeekShot("pages-" + k + "-peek-" + mode)
			closeTabPeekAway(e, away)
		}
	}
}
