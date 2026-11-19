package ui

// A known gap, tracked and run on request (flake-check only=slice-gaps,
// CLACK_SLICE_GAPS): a row newly shown under a plugin patching the
// messages slice draws the plugins' view from its first frame, as Slack
// renders the patched slice in the render that first shows the message.
// Clack's row draws core's copy until JS's copy of it is ready
// (messages.applyView's viewWait: one JS round trip), so a thread pane
// opened under a text-marking plugin (Message Logger's kind) first paints
// its reply unmarked for a few hundred milliseconds (218-266 ms, 12-15
// frames: runs 44591888895, 44596411773). Red until a row's first read
// waits for the view or takes it at once. Synthetic data only.

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
)

func TestSliceFirstDraw(t *testing.T) {
	if os.Getenv("CLACK_SLICE_GAPS") == "" {
		t.Skip("CLACK_SLICE_GAPS not set: a known gap, flake-check's slice-gaps job runs it on request")
	}
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	chs, ids := fs.ChannelIDs(), fs.UserIDs()
	ch := chs[2] // read: it opens at its newest, the root on screen (the second is unread, opening at its unread line)
	const rootText, replyText = "a synthetic first-draw root", "a synthetic first-draw reply"
	root, _ := fs.PostQuiet(ch, ids[2], rootText)
	fs.PostReplyQuiet(ch, root, ids[3], replyText)

	e := benchStart(t, fs).perfEnv
	e.waitReady()
	e.until("Taut", 60*time.Second, func() bool { return e.a.Taut() != nil })
	tt := e.a.Taut()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := tt.Import(ctx, []byte(reactSliceText), ""); err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, "ReactSliceText", true); err != nil {
		t.Fatal(err)
	}
	e.until("the plugin's patch", 30*time.Second, func() bool { return tt.SlicePatched("messages") })

	var mu sync.Mutex
	var bodies []string
	richview.DrawnHook = func(d richtext.Doc) {
		s := richtext.PlainText(d)
		mu.Lock()
		bodies = append(bodies, s)
		mu.Unlock()
	}
	t.Cleanup(func() { richview.DrawnHook = nil })
	// watch runs frames until one draws text marked, failing a frame that
	// draws it unmarked before
	watch := func(what, text string) {
		t.Helper()
		unmarked := 0
		var first time.Time
		for dl := time.Now().Add(20 * time.Second); time.Now().Before(dl); time.Sleep(16 * time.Millisecond) {
			mu.Lock()
			bodies = bodies[:0]
			mu.Unlock()
			e.frame()
			mu.Lock()
			marked, plain := false, false
			for _, b := range bodies {
				if strings.Contains(b, text+reactLogged) {
					marked = true
				} else if strings.Contains(b, text) {
					plain = true
				}
			}
			mu.Unlock()
			if plain && first.IsZero() {
				first = time.Now()
			}
			if plain {
				unmarked++
			}
			if marked {
				if unmarked > 0 {
					t.Errorf("%s: %d frames drew the message unmarked (core's copy, not the plugins' view) for %v before its first marked one",
						what, unmarked, time.Since(first).Round(time.Millisecond))
				}
				return
			}
		}
		t.Errorf("%s: never drawn marked", what)
	}
	e.openChannel(ch)
	e.until("the channel's history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.settle(2 * time.Second)
	e.a.OpenThread(ch, root)
	watch("the thread pane opened", replyText)
}
