package ui

import (
	"os"
	"path/filepath"
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
func TestHeaderFacepileRetriesFailedRead(t *testing.T) {
	e, fs := previewEnv(t)
	ch := ""
	for _, s := range e.a.model().Sections {
		for _, id := range s.Rows {
			c := e.a.model().conv(id)
			if _, read := e.a.data.fetched[id]; ch == "" && !read && c != nil && c.Kind == "channel" && !c.Preview && c.NumMembers > 0 {
				ch = id
			}
		}
	}
	if ch == "" {
		t.Fatal("no unopened member channel")
	}
	fs.SetFault("edge/users/list", fakeslack.Fault{Error: "ratelimited", Times: 1})
	e.openChannel(ch)
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
