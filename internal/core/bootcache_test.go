package core

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A warm boot paints the boot snapshot, then Slack's answers replace it:
// a channel left in another client meanwhile leaves the sidebar, counts go
// live, and the cached page of the last conversation is refetched.
func TestWarmBootReconciles(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	dir := t.TempDir()
	ctx := context.Background()
	ids := fs.ChannelIDs()
	open, gone := ids[1], ids[2]

	a := New(slack.New(fs.Creds()), store.New(dir))
	if a.LoadBootCache() {
		t.Fatal("a fresh cache dir was warm")
	}
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := a.History(ctx, open, ""); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a.Shutdown()
	// meanwhile, in another client: leave a channel
	other := New(slack.New(fs.Creds()), store.New(""))
	if err := other.Leave(ctx, gone); err != nil {
		t.Fatal(err)
	}
	other.Shutdown()

	b := New(slack.New(fs.Creds()), store.New(dir))
	t.Cleanup(b.Shutdown)
	snap, ok := b.CachedSnapshot()
	if !ok {
		t.Fatal("no boot snapshot after a full session")
	}
	if snap.CountsLive || b.CountsLive() {
		t.Fatal("cached counts reported as live")
	}
	if !hasConv(snap, gone) {
		t.Fatal("the cached sidebar lacks the channel (the fixture needs it there)")
	}
	if b.S.Timeline(open) == nil {
		t.Fatal("history.json didn't restore the last conversation")
	}
	if b.RestoredHistory() != open {
		t.Fatalf("restored %q, want %s", b.RestoredHistory(), open)
	}
	nHist := fs.CallCount("conversations.history")
	if err := b.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	live := b.Snapshot()
	if hasConv(live, gone) {
		t.Error("a channel left elsewhere stayed in the sidebar after Slack answered")
	}
	if !live.CountsLive {
		t.Error("counts not live after boot")
	}
	dl := time.Now().Add(5 * time.Second)
	for time.Now().Before(dl) {
		if fs.CallCount("conversations.history") > nHist {
			return // the cached page was refetched
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the restored conversation was never refetched")
}

func hasConv(s Snapshot, id string) bool {
	for _, c := range s.Convs {
		if c.ID == id {
			return true
		}
	}
	return false
}
