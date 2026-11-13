package core

import (
	"context"
	"encoding/json"
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

// Which channel-tab slugs open the channel card (NY16 → zDOB/D3FY): a
// tab with a peek under session_channels_slug_peek doesn't; a tab the
// channel doesn't have, one without a peek, any tab with the experiment
// off, and a channel nobody knows do.
func TestChannelCardTabSlug(t *testing.T) {
	sz := fakeslack.Small
	sz.CodeChannels = true
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
	if g := a.Experiment("session_channels_slug_peek"); g != "on" {
		t.Fatalf("session_channels_slug_peek %q, want the account's on", g)
	}
	ch := fakeslack.CodeWorkingID
	for _, c := range []struct {
		tab  string
		card bool
	}{
		{"code", false},                            // the Code tab: CodeTabPeek
		{fakeslack.CodeHTMLViewID, false},          // agent_view: AgentViewTabPeek
		{"Ct" + fakeslack.CodePlanCanvasID, false}, // canvas: CanvasTabPeek
		{"agent_view", false},                      // findTabById matches a type too
		{"Ct0GONE000001", true},                    // not one of its tabs: BaseMrkdwnChannel
	} {
		if got := a.ChannelTabSlugCard(ch, c.tab); got != c.card {
			t.Errorf("tab %q: card %v, want %v", c.tab, got, c.card)
		}
	}
	if !a.ChannelTabSlugCard("C0NOPE00001", "code") {
		t.Error("an unknown channel's slug: no card (BaseMrkdwnChannel shows it)")
	}
	// a tab type without a peek (huddles)
	if got, ok := tabzType(json.RawMessage(`{"tabz":[{"id":"Ct1","type":"huddles"}]}`), "Ct1"); !ok || got != "huddles" || slugPeekTabs[got] {
		t.Errorf("huddles tab: %q %v", got, ok)
	}
	a.SetExperimentGroup("session_channels_slug_peek", "")
	if !a.ChannelTabSlugCard(ch, "code") {
		t.Error("slug peek off: the Code tab's slug has no card")
	}
}

// useCodeChannelSummaryText: summary_message.message_ts, read from the
// store only (nothing until the history is loaded), as the last rich_text
// block's plain text with unicode emoji and the sender's name.
func TestChannelCardCodeSummary(t *testing.T) {
	sz := fakeslack.Small
	sz.CodeChannels = true
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
	if s, ok := a.CodeChannelSummary(fakeslack.CodeDoneID); ok && a.S.Timeline(fakeslack.CodeDoneID) == nil {
		t.Errorf("a summary %+v before the history loaded (Slack reads its store only)", s)
	}
	if _, err := a.History(ctx, fakeslack.CodeDoneID, ""); err != nil {
		t.Fatal(err)
	}
	a.EnsureUsers(ctx, []string{fakeslack.CodeAgentID})
	s, ok := a.CodeChannelSummary(fakeslack.CodeDoneID)
	if !ok || s.Text != "Upgraded every dependency to its latest minor 🎉 and the suite passes." || s.Author != "Codebot" || s.TS == "" {
		t.Errorf("summary %+v %v", s, ok)
	}
	if _, ok := a.CodeChannelSummary(fakeslack.CodeWorkingID); ok {
		t.Error("the working channel has no summary_message")
	}
	if got := normalizePreview("a\n\n\nb\n"); got != "a\nb" {
		t.Errorf("normalizePreview %q", got)
	}
	if err := a.LoadChannelTopMembers(ctx, fakeslack.CodeDoneID); err != nil {
		t.Fatal(err)
	}
	if ms, ok := a.ChannelTopMembers(fakeslack.CodeDoneID); !ok || len(ms) == 0 {
		t.Errorf("top members %q %v", ms, ok)
	}
	if f := fs.UserListFilters(); len(f) == 0 || f[len(f)-1] != "everyone AND NOT bots AND NOT apps" {
		t.Errorf("users/list filters %q", f)
	}
}
