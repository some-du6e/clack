package messages

import (
	"image/color"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// redirHost is a Host whose thread panes hand a reply-rooted thread on
// (ReplyRootRedirector), counting the hand-overs.
type redirHost struct {
	coreHost
	mu    sync.Mutex
	calls []string
}

func (h *redirHost) ReplyAsThreadRoot(ch, reply, root string) {
	h.mu.Lock()
	h.calls = append(h.calls, ch+" "+reply+" "+root)
	h.mu.Unlock()
}

func (h *redirHost) handOvers() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.calls...)
}

// TestReplyRootedPaneHandsOnEachOpen: a thread pane opened with a reply's
// ts hands the thread on to the reply's parent once each time it opens
// (Reopen: Slack mounts a new pane), not once for the life of its list:
// the list of a pane that opens with the reply's ts again, its messages
// cached, hands on again instead of drawing "Loading thread…" for good.
// Synthetic thread fixture on fakeslack.
func TestReplyRootedPaneHandsOnEachOpen(t *testing.T) {
	SetTaut(nil)
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	app := core.New(slack.New(fs.Creds()), store.New(""))
	t.Cleanup(app.Shutdown)
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	h := &redirHost{coreHost: coreHost{app: app, th: th}}
	pub, root, reply := fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, fakeslack.ThreadReplyTS(1)
	v := NewThreadView(h, pub, reply)
	frame := frameFn(v)
	until(t, 15*time.Second, "the pane handing the thread on", frame, func() bool { return len(h.handOvers()) == 1 })
	for range 5 {
		frame() // still drawing its loading state: one hand-over per open
	}
	if got := h.handOvers(); len(got) != 1 {
		t.Fatalf("hand-overs while the pane stayed open: %q, want one", got)
	}
	v.Reopen() // the pane opens with the reply's ts again
	until(t, 10*time.Second, "the pane handing the thread on after it opened again", frame, func() bool { return len(h.handOvers()) == 2 })
	if got, want := h.handOvers()[1], pub+" "+reply+" "+root; got != want {
		t.Errorf("the second hand-over %q, want %q", got, want)
	}
}

// TestJumpHighlightKeyframes: a jump's highlight is Slack's
// c-message_kit__background--highlight, `animation: 3s forwards` over
// keyframes 20% and 60% at #f2c74433 (CSS's default ease, no dark-mode
// override): it eases in over the first 0.6 s, holds until 1.8 s, eases
// out and is gone at 3 s. The curve at fixed times (highlightAlpha), and
// a jump's row drawn lit and then clear. Clack held it at full for 2 s
// and faded it until 4 s, and drew it at half strength in dark mode.
func TestJumpHighlightKeyframes(t *testing.T) {
	want := color.NRGBA{R: 0xf2, G: 0xc7, B: 0x44, A: 0x33}
	if lightColors.Highlight != want || darkColors.Highlight != want {
		t.Errorf("the highlight's colour: light %v, dark %v; want #f2c74433 in both", lightColors.Highlight, darkColors.Highlight)
	}
	for _, c := range []struct {
		el     time.Duration
		lo, hi float32
	}{
		{0, 0, 0},
		{300 * time.Millisecond, 0.01, 0.99}, // easing in
		{600 * time.Millisecond, 1, 1},       // the 20% keyframe
		{1200 * time.Millisecond, 1, 1},
		{1800 * time.Millisecond, 1, 1},       // the 60% keyframe
		{2400 * time.Millisecond, 0.01, 0.99}, // easing out
		{3 * time.Second, 0, 0},
		{3500 * time.Millisecond, 0, 0},
	} {
		if got := highlightAlpha(c.el); got < c.lo || got > c.hi {
			t.Errorf("%v in: %.3f, want %.2f…%.2f", c.el, got, c.lo, c.hi)
		}
	}
	app, _, ts := shotsApp(t)
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	v := NewChannelView(coreHost{app: app, th: th}, tShots)
	frame := frameFn(v)
	target := ts["b1"]
	drawn := func() float32 {
		for _, r := range v.Senders() {
			if r.TS == target {
				return r.Highlight
			}
		}
		return -1
	}
	until(t, 10*time.Second, "the conversation", frame, func() bool { return drawn() >= 0 })
	v.Highlight(target)
	until(t, 3*time.Second, "the jump's row lit", frame, func() bool { return drawn() > 0 })
	time.Sleep(highlightFor + 200*time.Millisecond)
	frame()
	if hl := drawn(); hl != 0 {
		t.Errorf("%.2f drawn after 3 s, want the highlight gone", hl)
	}
}
