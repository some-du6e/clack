package core

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// getLatestChannelMessageDateText: today, yesterday, "on MMM Do" this
// year, "in YYYY" before; no message at all.
func TestChannelCardLastMessageText(t *testing.T) {
	now := time.Date(2026, 11, 13, 15, 0, 0, 0, time.UTC)
	tr := func(src string, args ...any) string {
		for i := 0; i+1 < len(args); i += 2 {
			src = strings.ReplaceAll(src, "{"+args[i].(string)+"}", args[i+1].(string))
		}
		return src
	}
	ts := func(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) + ".000100" }
	for _, c := range []struct {
		ts, want string
	}{
		{"", "No messages"},
		{ts(now.Add(-2 * time.Hour)), "Last message today"},
		{ts(time.Date(2026, 11, 12, 23, 59, 0, 0, time.UTC)), "Last message yesterday"},
		{ts(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)), "Last message on Nov 1st"},
		{ts(time.Date(2026, 3, 22, 9, 0, 0, 0, time.UTC)), "Last message on Mar 22nd"},
		{ts(time.Date(2026, 1, 13, 9, 0, 0, 0, time.UTC)), "Last message on Jan 13th"},
		{ts(time.Date(2025, 12, 31, 9, 0, 0, 0, time.UTC)), "Last message in 2025"},
	} {
		if got := LastMessageText(c.ts, now, tr); got != c.want {
			t.Errorf("%q: %q, want %q", c.ts, got, c.want)
		}
	}
}

// The card's last message skips isAutomatedSubtype's system messages and
// takes the newest of the rest.
func TestChannelCardLatestShown(t *testing.T) {
	msgs := []slack.Message{
		{TS: "1762516000.000100", Text: "hi"},
		{TS: "1762516120.000100", Subtype: "channel_join"},
		{TS: "1762516060.000100", Subtype: "bot_message"},
		{TS: "1762516200.000100", Subtype: "channel_topic"},
	}
	if got := latestShown(msgs); got != "1762516060.000100" {
		t.Errorf("latest %q, want the bot message (joins and topic changes are automated)", got)
	}
	if got := latestShown(msgs[1:2]); got != "" {
		t.Errorf("only a join: %q, want none (\"No messages\")", got)
	}
}

// A Slack Connect channel's teams leave out the viewer's own top-level
// team (the org on Grid) and repeats; membership counts the viewer's
// conversations and DMs.
func TestChannelCardConvFields(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cv, ok := a.Conv(fakeslack.ThreadSharedID)
	if !ok {
		t.Fatal("the shared fixture isn't in the viewer's conversations")
	}
	if !cv.Member || len(cv.ConnectedTeams) != 1 || cv.ConnectedTeams[0] != fakeslack.ExternalTeamID {
		t.Errorf("shared: member %v, teams %v; want a member, [%s] (the viewer's org dropped)", cv.Member, cv.ConnectedTeams, fakeslack.ExternalTeamID)
	}
	other, ok := a.ResolveConv(ctx, fakeslack.ThreadElsewhereID)
	if !ok || other.Member || other.Kind != "channel" {
		t.Errorf("elsewhere: %+v %v, want a public channel the viewer isn't in", other, ok)
	}
	if _, ok := a.ResolveConv(ctx, "C0NOPE00001"); ok {
		t.Error("a channel conversations.info doesn't know resolved")
	}
	c2 := &slack.Conversation{ID: "C9", ConnectedTeamIDs: []string{fakeslack.EnterpriseID, "T1", "", "T1", "E2"}}
	if got := a.connectedTeams(c2); len(got) != 2 || got[0] != "T1" || got[1] != "E2" {
		t.Errorf("connectedTeams %v, want [T1 E2]", got)
	}
	// the count and the last message, as the card reads them
	if err := a.LoadPeopleCount(ctx, fakeslack.ThreadSharedID); err != nil {
		t.Fatal(err)
	}
	if n, ok := a.ChannelPeopleCount(fakeslack.ThreadSharedID); !ok || n != 1100 {
		t.Errorf("people %d %v, want users/counts' 1100", n, ok)
	}
	cached := a.S.Timeline(fakeslack.ThreadSharedID) != nil
	var mu sync.Mutex
	limit1 := 0
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "conversations.history" && form.Get("channel") == fakeslack.ThreadSharedID && form.Get("limit") == "1" {
			mu.Lock()
			limit1++
			mu.Unlock()
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	ts, err := a.ChannelLatestTS(ctx, fakeslack.ThreadSharedID)
	if err != nil || ts != fakeslack.ThreadRootTS {
		t.Errorf("latest %q %v, want the fixture's root %s", ts, err, fakeslack.ThreadRootTS)
	}
	mu.Lock()
	n := limit1
	mu.Unlock()
	if cached && n != 0 || !cached && n != 1 {
		t.Errorf("%d conversations.history limit-1 reads (history cached: %v): want one only when core holds none", n, cached)
	}
}
