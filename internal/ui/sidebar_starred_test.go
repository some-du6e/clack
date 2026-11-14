package ui

// The Starred section's empty states, External connections' place and the
// closed DMs a draft or a huddle keeps (sidebar_starred.go, core
// sidebar_rules.go) over fakeslack's Size.FewSections, Size.ConnectSection
// and closed DMs: synthetic data, nothing here talks to Slack. With
// PREVIEW_SHOTS_DIR set (the sidebar-experiments workflow) each state is
// saved light and dark (sidebar-starred-*.png, sidebar-connect-*.png).

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// sectionsEnv boots Clack over a fake of size sz (setup runs on it first),
// with a GPU window when screenshots are wanted.
func sectionsEnv(t *testing.T, sz fakeslack.Size, setup func(fs *fakeslack.Server)) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	if setup != nil {
		setup(fs)
	}
	e := startEnv(t, fs, t.TempDir())
	if organizeShotsDir() != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	e.settle(300 * time.Millisecond)
	return e, fs
}

// starredID is fakeslack's Starred section.
const starredID = "L0FAKE00"

// Nothing starred and two custom sections: Starred shows, and while it's
// open EmptyStarredSectionRow's banner sits under its heading, 44px, the
// label in the sidebar's colour from 32px in (the list's 8, the banner's
// 24), wrapped as Chrome wraps it; collapsing the section, organize mode,
// the text filter and a starred conversation each take it away.
func TestSidebarStarredBanner(t *testing.T) {
	sz := fakeslack.Small
	sz.FewSections = true
	e, _ := sectionsEnv(t, sz, nil)
	a := e.a
	e.until("the banner row", 15*time.Second, func() bool { return a.sidebar.sbPos(sbStarredBannerKey) >= 0 })
	if h := e.sbRowHeight(t, sbStarredBannerKey); h != 44 {
		t.Errorf("banner row %dpx, want 44", h)
	}
	var sec *sbSection
	for i := range a.model().Sections {
		if s := &a.model().Sections[i]; s.ID == starredID {
			sec = s
		}
	}
	if sec == nil || !sec.StarredBanner || len(sec.Rows) != 0 {
		t.Fatalf("Starred section %+v, want it empty with the banner", sec)
	}
	// the label, on one line or split between words on two
	words := strings.Fields("Drag and drop important stuff here")
	var d dropDrawn
	d.hook(t)
	e.settle(150 * time.Millisecond)
	drawn := false
	for k := 1; k <= len(words); k++ {
		first, rest := strings.Join(words[:k], " "), strings.Join(words[k:], " ")
		if d.has(first) && (rest == "" || d.has(rest)) {
			drawn = true
		}
	}
	if !drawn {
		t.Error("the banner's label isn't drawn")
	}
	if first, rest := a.sbBannerLines(e.context(), strings.Join(words, " "), 120); first == "" || rest == "" ||
		first+" "+rest != strings.Join(words, " ") {
		t.Errorf("at 120px the label splits %q / %q, want two lines of its words", first, rest)
	}
	e.organizeShot(t, "sidebar-starred-empty-light")
	a.setDark(true)
	e.settle(300 * time.Millisecond)
	e.organizeShot(t, "sidebar-starred-empty-dark")
	a.setDark(false)
	e.frame()

	// collapsed: no banner
	head := "sectionHeading-" + starredID
	e.clickWith(e.sbRowPoint(t, head), 0)
	e.until("collapsed", 5*time.Second, func() bool { return a.sidebar.sbPos(sbStarredBannerKey) < 0 })
	e.clickWith(e.sbRowPoint(t, head), 0)
	e.until("open again", 5*time.Second, func() bool { return a.sidebar.sbPos(sbStarredBannerKey) >= 0 })

	// starring a channel: Starred lists it, no banner
	c := a.Core()
	ch := e.fs.ChannelIDs()[0]
	if err := c.Star(context.Background(), ch, true); err != nil {
		t.Fatal(err)
	}
	e.until("starred row", 10*time.Second, func() bool {
		return a.sidebar.sbPos(ch) >= 0 && a.sidebar.sbPos(sbStarredBannerKey) < 0 && sbConvs(a, starredID) != nil
	})
	if err := c.Star(context.Background(), ch, false); err != nil {
		t.Fatal(err)
	}
	e.until("the banner back", 10*time.Second, func() bool { return a.sidebar.sbPos(sbStarredBannerKey) >= 0 })
}

// The megaphone's starred_section_new_badge with the banner's condition
// (MGQu): the Starred heading carries EducationTag's "New" and the section
// is held collapsed; clicking the heading opens it at once (the banner
// shows) and clears the badge 500 ms later (recordAndClearNewBadge:
// megaphone.setNotificationAsSeen dismiss, step the badge).
func TestSidebarStarredNewBadge(t *testing.T) {
	sz := fakeslack.Small
	sz.FewSections = true
	e, fs := sectionsEnv(t, sz, func(fs *fakeslack.Server) { fs.SetNewBadges(starredNewBadge) })
	a := e.a
	e.until("the badge", 15*time.Second, func() bool { return a.sidebar.starBadgeID == starredID })
	if a.sidebar.starForcedID != starredID {
		t.Fatalf("Starred isn't held collapsed (forced %q)", a.sidebar.starForcedID)
	}
	head := &sbRow{kind: sbHeading, key: starredID}
	if !a.sbCollapsed(head) || a.sidebar.sbPos(sbStarredBannerKey) >= 0 {
		t.Error("the held section is open")
	}
	if w := a.sbStarredTagWidth(e.context(), head); w <= 8 {
		t.Errorf("no room for the New tag (%d)", w)
	}
	var d dropDrawn
	d.hook(t)
	e.settle(150 * time.Millisecond)
	if !d.has(starredNewLabel()) {
		t.Error("the heading didn't draw New")
	}
	e.organizeShot(t, "sidebar-starred-new-light")
	a.setDark(true)
	e.settle(300 * time.Millisecond)
	e.organizeShot(t, "sidebar-starred-new-dark")

	e.clickWith(e.sbRowPoint(t, "sectionHeading-"+starredID), 0)
	e.until("open with the banner", 5*time.Second, func() bool {
		return a.sidebar.starForcedID == "" && a.sidebar.sbPos(sbStarredBannerKey) >= 0
	})
	e.until("the badge cleared", 5*time.Second, func() bool { return !a.Core().MegaphoneNewBadge(starredNewBadge) })
	e.until("the dismissal", 5*time.Second, func() bool {
		for _, c := range fs.UnreadsCalls() {
			if c.Method == "megaphone.setNotificationAsSeen" && c.Form.Get("step") == starredNewBadge && c.Form.Get("action") == "dismiss" {
				return true
			}
		}
		return false
	})
	e.until("no tag", 5*time.Second, func() bool { return a.sidebar.starBadgeID == "" })
	e.organizeShot(t, "sidebar-starred-opened-dark")
}

// External connections (should_show_connect_section and a slack_connect
// section): the externally shared channel moves there, the section sits
// where the user's list puts it (after Channels, before Direct messages
// here) and reads Slack's static name.
func TestSidebarConnectPlacement(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader, sz.ConnectSection = true, true
	e, _ := sectionsEnv(t, sz, nil)
	a := e.a
	e.until("External connections", 15*time.Second, func() bool {
		return slices.Contains(sbConvs(a, fakeslack.ConnectSectionID), fakeslack.ThreadSharedID)
	})
	var types []string
	for _, s := range a.model().Sections {
		types = append(types, s.Type)
		if s.Type == "slack_connect" && (s.Name != "External connections" || !s.DefaultName) {
			t.Errorf("section name %q (default %v), want Slack's static External connections", s.Name, s.DefaultName)
		}
	}
	ch, sc, dm := slices.Index(types, "channels"), slices.Index(types, "slack_connect"), slices.Index(types, "direct_messages")
	if ch < 0 || sc != ch+1 || dm != sc+1 {
		t.Errorf("sections %v, want Channels, External connections, Direct messages in a row", types)
	}
	if slices.Contains(sbConvs(a, "L0FAKECH"), fakeslack.ThreadSharedID) {
		t.Error("the shared channel is still in Channels")
	}
	e.sbRowPoint(t, "sectionHeading-"+fakeslack.ConnectSectionID) // scrolled into view
	e.organizeShot(t, "sidebar-connect-light")
	a.setDark(true)
	e.settle(300 * time.Millisecond)
	e.organizeShot(t, "sidebar-connect-dark")
}

// A closed DM with a draft stays in Direct messages (lJBI's
// hasDraftByChannelId), its draft pencil on, and leaves when the draft is
// deleted; one with a huddle on stays until the huddle ends (the huddle
// UI's rooms, which core's rules read).
func TestSidebarClosedDMKept(t *testing.T) {
	const drafted, huddling = "D0CLOSED0002", "D0CLOSED0003"
	users := []string{"U0FAKE00051", "U0FAKE00052"}
	var dr string
	e, _ := filterEnv(t, func(fs *fakeslack.Server) {
		fs.AddClosedDM(drafted, users[0])
		fs.AddClosedDM(huddling, users[1])
		dr = fs.AddDraft(fakeslack.Draft{Blocks: draftBlocks("see you then"), Destinations: draftDest(drafted, "")})
	})
	a := e.a
	// sidebar.dms answered (until then every DM counts as open), the
	// closed DM without a draft or a huddle leaves; the drafted one stays
	e.until("sidebar.dms", 15*time.Second, func() bool { return a.Core().SidebarDMsKnown() })
	e.until("the bare closed DM out", 10*time.Second, func() bool { return !e.sidebarHas(huddling) })
	e.until("the drafted DM", 15*time.Second, func() bool { return e.sidebarHas(drafted) && a.data.draftChans[drafted] })
	if err := a.Core().DeleteDraft(context.Background(), dr); err != nil {
		t.Fatal(err)
	}
	e.until("the drafted DM gone", 10*time.Second, func() bool { return !e.sidebarHas(drafted) })

	room := func(name string, ended bool) core.Event {
		raw, _ := json.Marshal(map[string]any{"user": users[1], "room": map[string]any{"id": "R0KEEP", "channels": []string{huddling},
			"participants": []string{users[1]}, "has_ended": ended}})
		return core.Event{Type: "huddle", Name: name, Channel: huddling, Raw: raw}
	}
	a.huddles().Handle(room("sh_room_join", false))
	e.until("the huddling DM", 10*time.Second, func() bool { return e.sidebarHas(huddling) })
	a.huddles().Handle(room("sh_room_update", true))
	e.until("the huddle over", 10*time.Second, func() bool { return !e.sidebarHas(huddling) })
}
