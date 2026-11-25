package core

import (
	"context"
	"net/url"
	"sync"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A channel's history reads ask what Slack's client asks: its newest page
// (fetchLatest: the open, a prefetch, a refresh) and an older page are
// conversations.history {limit: 28, inclusive: true}, as Slack's message
// pane reads every page (fetchChannelHistory passes no limit, so
// fetchHistory takes determineDefaultHistoryLimit's 28). Clack read 50 and
// 100: opening a channel of giant messages (#giant-wall's shape, Karim's
// #i-love-slack-squared) read about 90 MB where Slack's read 50. The older
// page brings the message it was read before back (inclusive); the list
// gets it once.
func TestHistoryPagesLikeSlack(t *testing.T) {
	ctx := context.Background()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ch := fs.ChannelIDs()[2]
	var mu sync.Mutex
	var forms []url.Values
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "conversations.history" && form.Get("channel") == ch {
			mu.Lock()
			forms = append(forms, form)
			mu.Unlock()
		}
		return nil, false
	})
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	reads := func() []url.Values {
		mu.Lock()
		defer mu.Unlock()
		return append([]url.Values(nil), forms...)
	}
	check := func(what string, f url.Values) {
		t.Helper()
		if f.Get("limit") != "28" || f.Get("inclusive") != "true" {
			t.Errorf("%s: conversations.history limit %q, inclusive %q; want Slack's 28 and true (determineDefaultHistoryLimit)", what, f.Get("limit"), f.Get("inclusive"))
		}
	}
	p, err := a.History(ctx, ch, "")
	if err != nil {
		t.Fatal(err)
	}
	rs := reads()
	if len(rs) == 0 {
		t.Fatal("opening the channel read no history")
	}
	for _, f := range rs {
		check("the newest page", f)
	}
	if len(p.Messages) == 0 || !p.HasMore {
		t.Fatalf("the newest page: %d messages, has_more %v; the fake's channel holds %d", len(p.Messages), p.HasMore, fakeslack.Small.History)
	}
	before := p.Messages[0].TS
	n := len(rs)
	older, err := a.History(ctx, ch, before)
	if err != nil {
		t.Fatal(err)
	}
	rs = reads()
	if len(rs) != n+1 {
		t.Fatalf("an older page read %d times", len(rs)-n)
	}
	check("an older page", rs[n])
	if rs[n].Get("latest") != before {
		t.Errorf("the older page read latest %q, want %q", rs[n].Get("latest"), before)
	}
	for _, m := range older.Messages {
		if m.TS == before {
			t.Errorf("the older page gave back the message it was read before (%s)", before)
		}
	}
	if len(older.Messages) == 0 {
		t.Error("the older page is empty")
	}
}
