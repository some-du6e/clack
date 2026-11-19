package views

// A message list keeps its model when the hub lets it go (40 newer
// threads): out of the hub it takes none of core's events. Drawn again,
// the list takes it back (dropped until its reload lands), and the model
// holds the revision it had when it went. Activity's row reads the
// revision a list drawn since its last frame holds (messages.Live), and a
// re-adopted model's was older than the one the row drew: the vote's
// count went back until the reload landed. Synthetic data on
// internal/fakeslack.

import (
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

func TestActivityLiveDroppedModel(t *testing.T) {
	e := newLmEnv(t, lmOpts{pane: true})
	e.untilVote(1)
	pane := e.tv
	if got := e.paneRev(); got != 1 {
		t.Fatalf("the thread pane draws revision %d, want 1", got)
	}
	// 40 other threads drawn after it: the hub lets the pane's model go
	base := time.Now().Add(-2 * time.Hour).Unix()
	for i := 0; i < 40; i++ {
		root := fmt.Sprintf("%d.000100", base+int64(i)*60)
		reply := fmt.Sprintf("%d.000200", base+int64(i)*60+30)
		e.app.S.SetThread(lmBusy, root, []slack.Message{
			{Type: "message", TS: root, ThreadTS: root, User: "U0FAKE00001", Text: fmt.Sprintf("thread %d", i), ReplyCount: 1},
			{Type: "message", TS: reply, ThreadTS: root, User: "U0FAKE00002", Text: fmt.Sprintf("reply %d", i)},
		})
		e.tv = messages.NewThreadView(e.h, lmBusy, root)
		e.e.until(t, fmt.Sprintf("thread %d drawn", i), func() bool { return len(e.tv.RowRects()) > 0 })
	}
	// the vote moves on while the pane's model is out; Activity draws it
	e.tv = nil
	for n := 2; n <= 3; n++ {
		e.vote(n)
	}
	e.e.until(t, "Activity's row at revision 3", func() bool { return e.rev() == 3 })
	// the pane is shown again: its list takes the model back, its reload
	// held, the model on the revision it had when it went
	release := messages.HoldLoad(lmChan, lmRoot)
	defer release()
	e.tv = pane
	for dl := time.Now().Add(600 * time.Millisecond); time.Now().Before(dl); {
		e.frame()
		if got := e.rev(); got != 3 {
			t.Fatalf("with the pane's re-adopted model reloading, Activity's row drew revision %d (the pane %d): the model's copy from before it was dropped", got, e.paneRev())
		}
		time.Sleep(10 * time.Millisecond)
	}
	release()
	e.e.until(t, "the pane's reload at revision 3", func() bool { return e.paneRev() == 3 && e.rev() == 3 })
}
