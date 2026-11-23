package composer

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Mod+Z's unsend window (onCmdZWhenEmpty) opens only for a message that
// was posted: Slack's prepareAndSendMessage returns from
// handleSlashCommand before updateMessageEligibleForUnsending (s0Qa), so
// after a command Mod+Z in the empty composer is a plain undo. Opened by
// a command, it would unsend (delete) the user's previous real message.

// unsendHost records the unsends asked for, with the ui root's /huddle.
type unsendHost struct {
	cmdHost
	unsends []string
	huddles []string
}

func (h *unsendHost) UnsendLast(ch, ts string, _ func(*Doc)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unsends = append(h.unsends, ch+"/"+ts)
}

func (h *unsendHost) SlashHuddle(ch string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.huddles = append(h.huddles, ch)
}

func (h *unsendHost) seen() (overlays, huddles, unsends int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.overlays), len(h.huddles), len(h.unsends)
}

// unsendComposer is #general's composer over the fake API, its draft
// cleared before and after.
func unsendComposer(t *testing.T) (*Composer, *unsendHost, *fakeSlack) {
	t.Helper()
	app, fs := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	withTaut(t, nil)
	clearDraft(tChGen, "")
	h := &unsendHost{cmdHost: cmdHost{slickHost: slickHost{app: app, th: theme.New(false)}}}
	c := New(h, tChGen, "")
	t.Cleanup(func() {
		c.Close()
		clearDraft(tChGen, "")
	})
	return c, h, fs
}

// armedWithin runs what the send queued for the UI goroutine for up to d
// and reports whether the unsend window opened.
func armedWithin(c *Composer, d time.Duration) bool {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		runRestores()
		if !c.unsendBy.IsZero() {
			return true
		}
	}
	return false
}

// untilUI runs the UI goroutine's queue until cond holds.
func untilUI(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		runRestores()
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestCommandsDontArmUnsend: a typed command that succeeds, "/remind"
// alone (the Reminder dialog), /huddle and a command a plugin relayed
// (NativeAtChannel's /channel, through the job's native) post nothing,
// so Mod+Z in the empty composer unsends nothing after them.
func TestCommandsDontArmUnsend(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		done       func(h *unsendHost, fs *fakeSlack) bool
	}{
		{"typed command", "/shrug ok", func(_ *unsendHost, fs *fakeSlack) bool { return len(fs.called("chat.command")) == 1 }},
		{"/remind alone", "/remind", func(h *unsendHost, _ *fakeSlack) bool { o, _, _ := h.seen(); return o > 0 }},
		{"/huddle", "/huddle", func(h *unsendHost, _ *fakeSlack) bool { _, n, _ := h.seen(); return n == 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, h, fs := unsendComposer(t)
			c.ed.SetDoc(cmdDoc(tc.text))
			c.submit()
			untilUI(t, tc.name, func() bool { return tc.done(h, fs) })
			if armedWithin(c, time.Second) {
				t.Fatalf("%s opened the unsend window", tc.text)
			}
			if c.tryUnsend() {
				t.Error("Mod+Z would unsend")
			}
			if _, _, n := h.seen(); n != 0 || len(fs.called("chat.postMessage")) != 0 {
				t.Errorf("%d unsends, %d posts", n, len(fs.called("chat.postMessage")))
			}
		})
	}
	t.Run("relayed command", func(t *testing.T) {
		c, h, fs := unsendComposer(t)
		job, finish := c.newSendJob(&outgoing{doc: cmdDoc("ping the channel")})
		_, err := job.native(context.Background(), prepareSendArgs{ChannelID: tChGen,
			Delta: oracleDelta(t, `[{"insert":"/ticket relay-aaaaaaaa\n"}]`)})
		finish(err)
		if err != nil || len(fs.called("chat.command")) != 1 || len(fs.called("chat.postMessage")) != 0 {
			t.Fatalf("relayed: %v, %d commands, %d posts", err, len(fs.called("chat.command")), len(fs.called("chat.postMessage")))
		}
		if armedWithin(c, time.Second) || c.tryUnsend() {
			t.Fatal("the relayed command opened the unsend window")
		}
		if _, _, n := h.seen(); n != 0 {
			t.Errorf("%d unsends", n)
		}
	})
}

// TestMessageArmsUnsend: a posted message opens the window (Mod+Z
// unsends it once); a post that fails doesn't.
func TestMessageArmsUnsend(t *testing.T) {
	c, h, fs := unsendComposer(t)
	c.ed.SetDoc(cmdDoc("hello"))
	c.submit()
	untilUI(t, "the post", func() bool { return len(fs.called("chat.postMessage")) == 1 })
	if !armedWithin(c, 5*time.Second) {
		t.Fatal("a posted message didn't open the unsend window")
	}
	if !c.tryUnsend() || c.tryUnsend() {
		t.Error("Mod+Z should unsend once")
	}
	if _, _, n := h.seen(); n != 1 {
		t.Errorf("%d unsends, want 1", n)
	}

	fs.mu.Lock()
	if fs.reply == nil {
		fs.reply = map[string]any{}
	}
	fs.reply["chat.postMessage"] = map[string]any{"ok": false, "error": "msg_too_long"}
	fs.mu.Unlock()
	c.ed.SetDoc(cmdDoc("again"))
	c.submit()
	untilUI(t, "the failed post", func() bool { return len(fs.called("chat.postMessage")) == 2 })
	if armedWithin(c, time.Second) {
		t.Error("a failed post opened the unsend window")
	}
	untilUI(t, "the message back", func() bool { return c.ed.Doc().Text() == "again" })
	c.ed.SetDoc(NewDoc())
}
