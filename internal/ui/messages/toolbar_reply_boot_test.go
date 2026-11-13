package messages

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A channel the viewer is in, known from boot (users.conversations, no
// is_member in its objects): the hover toolbar has Slack's full set,
// Reply in thread second (t8wB). Karim's toolbar lost it, 2026-11-13:
// every public channel read as a preview.
func TestToolbarReplyInBootedChannel(t *testing.T) {
	SetTaut(nil)
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	app := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(app.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := app.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	ch := ""
	app.S.Lock()
	for id, c := range app.S.Convs {
		if c.IsChannel && !c.IsPrivate && !c.IsIM && !c.IsMPIM && !c.IsArchived && (ch == "" || id < ch) {
			ch = id
		}
	}
	app.S.Unlock()
	if ch == "" {
		t.Fatal("no public channel after boot")
	}
	ts := "1700000001.000100"
	app.S.SetLatest(ch, []slack.Message{{Type: "message", TS: ts, User: "U0FAKE00001", Text: "a synthetic hello"}}, false)
	h := coreHost{app: app, th: theme.New(false)}
	v := NewChannelView(h, ch)
	v.Preview = true
	f := expFrame(v, 900)
	until(t, 5*time.Second, "the row", f, func() bool { return v.s.rows[ts] != nil })
	got, qa := toolbarFor(t, v.s, app, ts)
	parts := strings.Split(got, "|")
	if len(parts) < 2 || parts[1] != "Reply in thread" || qa[tbReply] != "start_thread" {
		t.Errorf("toolbar %q, want Reply in thread second (start_thread)", got)
	}
}
