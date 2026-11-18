package core

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// Tests for round-2 fixes that shipped without one (msg-latency round 4's
// review): each fails with its fix reverted.

// With no socket (realtime off or down), a history_invalid the counts
// moved marks the window stale instead of purging it: nothing advances the
// timestamps then (the socket's own changes do), so every deletion
// anywhere reset the conversation on screen, and its list dropped the
// messages its reader was on. The window opens on its cache and is read
// again behind it.
func TestCountsWithSocketDownMarkStale(t *testing.T) {
	a, fs, _ := rtApp(t)
	ch := fs.ChannelIDs()[22]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	base := "1794000000.000100"
	a.applyMutationTimes(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, HistoryInvalid: base, Updated: base}}})
	a.historyLanded(t, ctx, ch) // the conversation on screen
	a.SetRealtime(false)
	for dl := time.Now().Add(10 * time.Second); a.RTM.Connected(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("the socket never went down")
		}
	}
	a.applyMutationTimes(&slack.Counts{Channels: []slack.CountEntry{{ID: ch, HistoryInvalid: "1794000003.000100", Updated: base}}})
	if a.S.Timeline(ch) == nil {
		t.Fatal("a history_invalid with the socket down purged the window")
	}
	reads := fs.HistoryReads(ch)
	a.historyLanded(t, ctx, ch)
	if got := fs.HistoryReads(ch) - reads; got < 1 {
		t.Errorf("%d reads: the window the counts marked wasn't read again", got)
	}
}

// A users/info that callers share ends empty when its owner's deadline
// ends (the owner's ctx cancels the request): a caller that waited on it
// asks again under its own deadline, instead of going on without the
// member (a notification's sender left as its id).
func TestEnsureUsersWaiterOutlivesTheOwner(t *testing.T) {
	a, fs, _ := rtApp(t)
	id := fakeslack.SynthMemberID(7)
	if len(a.S.MissingUsers([]string{id})) == 0 {
		t.Fatalf("%s is already held", id)
	}
	fs.SetFault("edge/users/info", fakeslack.Fault{Delay: 400 * time.Millisecond})
	owner, cancelOwner := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelOwner()
	ownerDone := make(chan struct{})
	go func() {
		defer close(ownerDone)
		a.EnsureUsers(owner, []string{id})
	}()
	// the owner's request out (usersOut), then the waiter
	for dl := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		a.usersOutMu.Lock()
		_, out := a.usersOut[id]
		a.usersOutMu.Unlock()
		if out {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("the owner's users/info never went out")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.EnsureUsers(ctx, []string{id})
	<-ownerDone
	if len(a.S.MissingUsers([]string{id})) != 0 {
		t.Errorf("%s still missing after the waiter's EnsureUsers: it went on when the owner's request ended empty", id)
	}
}

// A message held for its conversation's lookup (a DM core doesn't know:
// conversations.info first) waits for that lookup alone, not for the
// lookups of other socket events (pumpLater's queue: a pref_change's
// users.prefs). Queued behind them, a new DM waited for a users.prefs
// that a frecency save brings about once a minute.
func TestHeldDMDoesntWaitForPrefs(t *testing.T) {
	a, f := testApp(t)
	evs, stop := a.Subscribe()
	defer stop()
	gate := make(chan struct{})
	defer func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	}()
	f.mu.Lock()
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"D9","is_im":true,"user":"U2"}}`
	f.reply["users.prefs.get"] = `{"ok":true,"prefs":{}}`
	f.hooks["users.prefs.get"] = func(url.Values) { <-gate }
	f.mu.Unlock()
	a.handle(rtm("pref_change", map[string]any{"name": "display_real_names", "value": true}))
	const ts = "1762516900.000100"
	a.handle(rtm("message", map[string]any{"channel": "D9", "ts": ts, "user": "U2", "text": "hi"}))
	deadline := time.After(5 * time.Second)
	for got := false; !got; {
		select {
		case ev := <-evs:
			got = ev.Type == "message" && ev.TS == ts
		case <-deadline:
			t.Fatal("the new DM's message waited for the pref_change's users.prefs")
		}
	}
	close(gate)
	a.laterDone(t)
}
