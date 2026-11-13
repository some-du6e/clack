package ui

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// The channel header's member facepile (Karim, 2026-11-10: "these 3 pfps
// at the top sometimes loads if they feel like it": three grey tiles and
// the count).

// The restored channel's header reads start before boot. When the members
// read finishes first, the facepile still lands once boot's snapshot makes
// the conversation's row. It used to be dropped (no row to put it on) and
// the 2-minute "already fetched" guard kept boot's second opened() from
// reading it again.
func TestHeaderFacepileReadBeforeBoot(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ch := fs.ChannelIDs()[1]
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "ui.json"), []byte(`{"route":"c/`+ch+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// boot's sidebar read is slow; the header's own reads answer at once
	fs.SetFault("users.conversations", fakeslack.Fault{Delay: 1500 * time.Millisecond, Times: 1})
	e := startEnv(t, fs, home)
	e.waitReady()
	if got := e.a.router.Current().Channel(); got != ch {
		t.Fatalf("restored route %q, want c/%s", got, ch)
	}
	e.until("the header facepile", 10*time.Second, func() bool {
		c := e.a.model().conv(ch)
		return c != nil && len(c.Members) > 0
	})
	c := e.a.model().conv(ch)
	for _, id := range c.Members {
		if e.a.userAvatar(id) == "" {
			t.Errorf("facepile member %s has no avatar URL", id)
		}
	}
}

// A failed members read (edge users/list rate limited) is tried again
// while the channel is open, instead of leaving the loading tiles until
// the channel is opened again two minutes later.
//
// The fault fails only ch's people read, once. A plain
// SetFault("edge/users/list", Times: 1) went to whichever users/list came
// first: boot's open channel's header read (C0FAKE00000), when waitReady
// returned before it was sent, took the fault, and ch's read worked
// (release run 43492399437, "the failed read set members"). The test
// now sends such a read itself after arming the fault, so that order
// happens every run.
func TestHeaderFacepileRetriesFailedRead(t *testing.T) {
	e, fs := previewEnv(t)
	ch, other := "", ""
	for _, s := range e.a.model().Sections {
		for _, id := range s.Rows {
			c := e.a.model().conv(id)
			if c == nil || c.Kind != "channel" || c.Preview || c.NumMembers == 0 {
				continue
			}
			if _, read := e.a.data.fetched[id]; ch == "" && !read {
				ch = id
			} else if other == "" && id != ch {
				other = id
			}
		}
	}
	if ch == "" || other == "" {
		t.Fatalf("no unopened member channel (%q) and a second one (%q)", ch, other)
	}
	var failed atomic.Int32
	failed.Store(1)
	fs.SetFault("edge/users/list", fakeslack.Fault{Error: "ratelimited", Times: 1}) // FORCED OLD FAULT (polish-1113c demo commit)
	// another channel's people read, sent before ch's (what a Times: 1
	// fault on every users/list used to spend itself on)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_, _, err := e.a.Core().Members(ctx, other, "", true)
	t.Logf("%s's members read: %v", other, err)
	cancel()
	e.openChannel(ch)
	e.until("ch's failed members read", 10*time.Second, func() bool { return failed.Load() == 1 })
	e.settle(300 * time.Millisecond)
	if c := e.a.model().conv(ch); c == nil || len(c.Members) > 0 {
		t.Fatalf("the failed read set members: %+v", c)
	}
	e.until("the facepile after the retry", 15*time.Second, func() bool {
		c := e.a.model().conv(ch)
		return c != nil && len(c.Members) > 0
	})
	e.shot("header-facepile-retried")
}
