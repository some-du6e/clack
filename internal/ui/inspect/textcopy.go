package inspect

import (
	"io"
	"strings"
	"sync"
	"time"

	"gioui.org/io/clipboard"
	"gioui.org/layout"
)

// A report that falls back to text (no shot, or a system that can't copy
// images) is copied through the window's clipboard, which only the UI
// goroutine writes and which takes a write as the frame it came in ends;
// an image goes to the system clipboard at once from its delivery's
// goroutine (clipimg). Deliveries copy in the order of their captures
// (deliver), so a text copy has to be on the clipboard before the next
// delivery writes its image, or the older capture's text would land last
// (review of 3a75558d: the text went through the host's CopyText, which
// only took it a frame or two later). The tool hands the text to the
// window itself, first thing in a frame (Frame, Begin: a session or not),
// and the delivery waits for a frame after that one: the window took the
// text as that frame ended.

// textCopies are the text copies on their way to the window's clipboard.
type textCopies struct {
	mu    sync.Mutex
	queue []*textCopy
	calls int64 // flushes with no frame time (tests): each counts as a frame
}

type textCopy struct {
	text   string
	handed bool      // given to the window's clipboard, in the frame at
	at     time.Time // (the frame's time)
	done   chan struct{}
}

// copyText queues text for the window's clipboard (any goroutine) and
// returns what closes once the window has taken it.
func (t *Tool) copyText(text string) <-chan struct{} {
	c := &textCopy{text: text, done: make(chan struct{})}
	t.texts.mu.Lock()
	t.texts.queue = append(t.texts.queue, c)
	t.texts.mu.Unlock()
	t.host.Invalidate()
	return c.done
}

// flushTexts hands the queued text copies to the window's clipboard in
// their order (the last one wins: the newest) and lets go of the ones
// handed in an earlier frame, which the window took as it ended (UI
// goroutine, first in every frame; a second call in one frame does
// nothing more).
func (t *Tool) flushTexts(gtx layout.Context) {
	tc := &t.texts
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if len(tc.queue) == 0 {
		return
	}
	now := gtx.Now
	if now.IsZero() {
		tc.calls++
		now = time.Unix(0, tc.calls)
	}
	keep := tc.queue[:0]
	for _, c := range tc.queue {
		switch {
		case !c.handed:
			gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(c.text))})
			c.handed, c.at = true, now
			keep = append(keep, c)
		case !now.Equal(c.at):
			close(c.done)
		default:
			keep = append(keep, c)
		}
	}
	clear(tc.queue[len(keep):])
	tc.queue = keep
	if len(keep) > 0 {
		t.host.Invalidate() // the frame that lets them go
	}
}
