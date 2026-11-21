package ui

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// Karim, 2026-11-21 (Windows, α.113): "got a message from system slack and
// didnt notice until i opened normal slack", then "not slack bot but
// straight up slack". It was their DM with the Slack system user (USLACK,
// "Slack", served is_bot false), where apps post bot_message with no user
// (Hack Club's welcome app does). fakeslack's Size.SystemDM serves that DM
// in the real account's shape.
//
// Slack's rules for its sidebar row and its counts (web client build
// 35b42de8b870):
//   - The row goes under Apps: uSoP getDefaultSectionType files a DM whose
//     member qSRE isAppOrBot calls an app (is_app_user, is_bot or the id
//     "USLACK") there, and lJBI keeps an open DM, or a closed one that is
//     unread.
//   - Its badge is its unread count: fgSa shouldCountUnreadsInModel counts
//     an unmuted IM's unreads and shouldUseUnreadCountAsMentionCount makes
//     them its highlights (9dui refreshFilteredUnreadChannelCounts). Only
//     the rail's DMs badge leaves bot DMs out (boot-shared rHEx); the row
//     doesn't.
//   - client.counts (mtwS applyCountsToChannels): an IM's or group DM's
//     unread count is mention_count || has_unreads.
//   - A post on the socket counts (0CqO updateUnreadCounts,
//     countUnreadMessages over qxvW canCountAsUnreadForCurrentUser): a
//     bot_message with no user isn't the viewer's own.
//   - im_marked (boot-render conversationMarked) moves last_read; read
//     through the newest post, the DM is read.

// systemDMFake starts Small with the system DM; set runs on the fake
// before the client starts.
func systemDMFake(t *testing.T, set func(fs *fakeslack.Server)) *fakeslack.Server {
	t.Helper()
	sz := fakeslack.Small
	sz.SystemDM = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	if set != nil {
		set(fs)
	}
	return fs
}

// systemDMRow is the system DM's row as the sidebar draws it: the type of
// the section it sits in ("" when it has no row), whether its name is
// bold and the badge it draws (0: none).
func systemDMRow(a *App) (section string, bold bool, badge int) {
	m := a.model()
	for _, r := range a.sidebar.rows {
		if r.kind != sbConv || r.key != fakeslack.SystemDMID {
			continue
		}
		section = "?" + r.section
		for _, s := range m.Sections {
			if s.ID == r.section {
				section = s.Type
			}
		}
		break
	}
	if c := m.conv(fakeslack.SystemDMID); c != nil {
		bold, badge = c.Unread && !c.Muted && !a.focusQuiet(), c.Mentions
	}
	return section, bold, badge
}

// waitSystemDMRow waits for the system DM's row to sit under Apps with
// its name bold or not and badge, failing with what it read last.
func waitSystemDMRow(t *testing.T, e *perfEnv, what string, bold bool, badge int, d time.Duration) {
	t.Helper()
	dl := time.Now().Add(d)
	for {
		e.frame()
		sec, b, n := systemDMRow(e.a)
		if (sec == "apps" || sec == "recent_apps") && b == bold && n == badge {
			return
		}
		if time.Now().After(dl) {
			t.Fatalf("%s: the system DM's row is in section %q, bold %v, badge %d; want it under Apps, bold %v, badge %d",
				what, sec, b, n, bold, badge)
		}
		time.Sleep(time.Millisecond)
	}
}

// systemDMInject delivers a realtime event for the system DM as the
// socket would (the UI tests run without one).
func systemDMInject(e *perfEnv, ev map[string]any) {
	raw, _ := json.Marshal(ev)
	typ, _ := ev["type"].(string)
	e.a.Core().Inject(slack.Event{Type: typ, Channel: json.RawMessage(strconv.Quote(fakeslack.SystemDMID)), Raw: raw})
}

// systemDMPost has the app post into the DM on the socket, in the real
// posts' shape; it returns the post's ts.
func systemDMPost(e *perfEnv, text string) string {
	ts, ev := e.fs.PostSystemDMQuiet(text)
	systemDMInject(e, ev)
	return ts
}

// systemDMRecount has core read client.counts again (an
// update_thread_state asks for it) and waits for the answer to land.
func systemDMRecount(t *testing.T, e *perfEnv) {
	t.Helper()
	before := e.fs.CallCount("client.counts")
	e.a.Core().Inject(slack.Event{Type: "update_thread_state", Raw: json.RawMessage(`{"type":"update_thread_state"}`)})
	e.until("client.counts read again", 15*time.Second, func() bool { return e.fs.CallCount("client.counts") > before })
	e.settle(time.Second) // the fake counts the call before it answers
}

// The DM's row over a session and a restart: under Apps, plain while read;
// each post on the socket makes it bold and counts one more on its badge;
// client.counts read again agrees; read in another client (im_marked) it
// is plain again; and after a restart a post made while Clack was closed
// is bold with a 1 from boot's client.counts.
func TestSystemDMRowLikeSlack(t *testing.T) {
	fs := systemDMFake(t, nil)
	home := freezeHome(t)
	e := startEnv(t, fs, home)
	e.waitReady()
	waitSystemDMRow(t, e, "both of the app's posts read", false, 0, 20*time.Second)
	if c := e.a.model().conv(fakeslack.SystemDMID); c == nil || c.Name != "Slack" {
		t.Fatalf("the system DM's row %+v, want it named Slack", c)
	}

	systemDMPost(e, "a synthetic sign-in notice")
	waitSystemDMRow(t, e, "one post on the socket", true, 1, 10*time.Second)
	second := systemDMPost(e, "another synthetic notice")
	waitSystemDMRow(t, e, "two posts on the socket", true, 2, 10*time.Second)
	systemDMRecount(t, e)
	waitSystemDMRow(t, e, "client.counts read again after two posts", true, 2, 10*time.Second)

	systemDMInject(e, fs.ReadSystemDMElsewhere(second))
	waitSystemDMRow(t, e, "read in another client (im_marked)", false, 0, 10*time.Second)

	<-e.a.data.stop()
	e.shutdown()
	fs.PostSystemDMQuiet("a synthetic notice while Clack was closed")
	e2 := startEnv(t, fs, home)
	e2.waitReady()
	waitSystemDMRow(t, e2, "after a restart, one post made while Clack was closed", true, 1, 20*time.Second)
}

// client.counts can answer an IM unread with no mentions (has_unreads
// true, mention_count 0). Slack's applyCountsToChannels reads it as one
// unread message, mention_count || has_unreads, and an IM's badge is its
// unread count: the row is bold with a 1, at boot and after client.counts
// is read again.
func TestSystemDMUnreadWithoutMentionsLikeSlack(t *testing.T) {
	fs := systemDMFake(t, func(fs *fakeslack.Server) {
		fs.SetSystemDMMentions(false)
		fs.PostSystemDMQuiet("a synthetic notice before Clack started")
		fs.PostSystemDMQuiet("another synthetic notice before Clack started")
	})
	e := startEnv(t, fs, freezeHome(t))
	e.waitReady()
	waitSystemDMRow(t, e, "unread at boot, client.counts with no mentions", true, 1, 20*time.Second)
	systemDMRecount(t, e)
	waitSystemDMRow(t, e, "client.counts read again", true, 1, 10*time.Second)
}
