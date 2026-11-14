package core

import (
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// lookupApp is a core over fakeslack's thread-header conversations (the
// private th-secret-plans, the public th-launch-notes) with an empty
// store, counting conversations.info calls by channel; hold, when set,
// keeps conversations.info for that channel waiting until it's closed.
func lookupApp(t *testing.T, holdID string, hold chan struct{}) (*App, func(id string) int) {
	t.Helper()
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	var mu sync.Mutex
	calls := map[string]int{}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "conversations.info" {
			ch := form.Get("channel")
			mu.Lock()
			calls[ch]++
			mu.Unlock()
			if hold != nil && ch == holdID {
				<-hold
			}
		}
		return nil, false
	})
	a := New(slack.New(fs.Creds()), store.New(""))
	t.Cleanup(a.Shutdown)
	return a, func(id string) int {
		mu.Lock()
		defer mu.Unlock()
		return calls[id]
	}
}

// namesFor waits for core's "names" event naming id.
func namesFor(t *testing.T, ch <-chan Event, id string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Type != "names" {
				continue
			}
			for _, u := range ev.Users {
				if u == id {
					return
				}
			}
		case <-deadline:
			t.Fatalf("no names event for %s", id)
		}
	}
}

// A channel the store doesn't have is Slack's unknown stub while its
// lookup runs (getChannelById's fetch), then its name: a private
// channel's, marked private; "names" tells the views. One lookup per
// channel, however often it's read.
func TestInlineChannelLooksUpUnknown(t *testing.T) {
	hold := make(chan struct{})
	a, calls := lookupApp(t, fakeslack.ThreadPrivateID, hold)
	evs, stop := a.Subscribe()
	defer stop()
	const priv = fakeslack.ThreadPrivateID
	for range 3 {
		if ic := a.InlineChannel(priv); ic.State != ChannelUnknown || ic.Name != "" {
			t.Fatalf("while the lookup runs: %+v, want the unknown stub (no name, never the id)", ic)
		}
	}
	if !a.LookupConv(priv) {
		t.Error("LookupConv: not pending while conversations.info is held")
	}
	close(hold)
	namesFor(t, evs, priv)
	ic := a.InlineChannel(priv)
	if ic.State != ChannelKnown || ic.Name != "th-secret-plans" || !ic.Private {
		t.Errorf("after the lookup: %+v, want the private channel th-secret-plans", ic)
	}
	if a.LookupConv(priv) {
		t.Error("LookupConv: still pending after the lookup")
	}
	if n := calls(priv); n != 1 {
		t.Errorf("%d conversations.info calls for %s, want 1", n, priv)
	}
}

// A stub named by its id (a payload without the name) is looked up like
// an unknown channel; a named conversation needs no lookup.
func TestInlineChannelStubAndKnown(t *testing.T) {
	a, calls := lookupApp(t, "", nil)
	evs, stop := a.Subscribe()
	defer stop()
	const priv, pub = fakeslack.ThreadPrivateID, fakeslack.ThreadPublicID
	a.S.PutConv(slack.Conversation{ID: priv, Name: priv, IsGroup: true, IsPrivate: true, IsMember: true})
	if ic := a.InlineChannel(priv); ic.State != ChannelUnknown || ic.Name != "" {
		t.Fatalf("a stub named by its id: %+v, want the unknown stub", ic)
	}
	namesFor(t, evs, priv)
	if ic := a.InlineChannel(priv); ic.State != ChannelKnown || ic.Name != "th-secret-plans" || !ic.Private {
		t.Errorf("the stub after the lookup: %+v, want th-secret-plans", ic)
	}
	a.S.PutConv(slack.Conversation{ID: pub, Name: "th-launch-notes", IsChannel: true, IsMember: true})
	if ic := a.InlineChannel(pub); ic.State != ChannelKnown || ic.Name != "th-launch-notes" || ic.Private {
		t.Errorf("a public channel in the store: %+v", ic)
	}
	if a.LookupConv(pub) || calls(pub) != 0 {
		t.Errorf("a named channel was looked up (%d calls)", calls(pub))
	}
	// archived: the store says so
	a.S.PutConv(slack.Conversation{ID: "C0ARCHIVED1", Name: "old-plans", IsChannel: true, IsMember: true, IsArchived: true})
	if ic := a.InlineChannel("C0ARCHIVED1"); ic.State != ChannelKnown || !ic.Archived {
		t.Errorf("an archived channel: %+v", ic)
	}
}

// A channel conversations.info doesn't know (channel_not_found: the
// viewer can't see it) ends non-existent, recorded as hidden for every
// reader (channel mentions too), and isn't asked for again.
func TestInlineChannelNonExistent(t *testing.T) {
	a, calls := lookupApp(t, "", nil)
	evs, stop := a.Subscribe()
	defer stop()
	const gone = "C0NOPE00001"
	if ic := a.InlineChannel(gone); ic.State != ChannelUnknown {
		t.Fatalf("before the lookup: %+v, want the unknown stub", ic)
	}
	namesFor(t, evs, gone)
	if ic := a.InlineChannel(gone); ic.State != ChannelNonExistent || ic.Name != "" {
		t.Errorf("after channel_not_found: %+v, want non-existent", ic)
	}
	if acc := a.S.ChannelAccess(gone); acc != richtext.ChannelHidden {
		t.Errorf("ChannelAccess %v, want hidden (Slack's non-existent object)", acc)
	}
	for range 3 {
		a.InlineChannel(gone)
	}
	if a.LookupConv(gone) {
		t.Error("LookupConv pending for a channel known not to exist")
	}
	if n := calls(gone); n != 1 {
		t.Errorf("%d conversations.info calls, want 1", n)
	}
	// a name seen elsewhere (edge channels/info, search) needs no lookup
	a.S.PutName("C0SEENNAME1", "seen-elsewhere")
	if ic := a.InlineChannel("C0SEENNAME1"); ic.State != ChannelKnown || ic.Name != "seen-elsewhere" || ic.Private {
		t.Errorf("a stored name: %+v", ic)
	}
	if calls("C0SEENNAME1") != 0 {
		t.Error("a channel the store names was looked up")
	}
}
