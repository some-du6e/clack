package messages

// A list in a second container (the split view) keeps its model when the
// hub lets it go; drawn again after another list made the hub a new model
// for the conversation, it switches to that one (adopt). Revisions count
// per model, and the new model's first load numbers the messages as the
// dropped one's did: a row whose message changed while its model was out
// gets the same number for the new copy. The switch made its rows take
// the new model's entries again (r.rev), but kept the caches keyed on
// those numbers: the row's view read compared equal, so the new copy
// didn't wait for JS's copy of it, and the row took JS's copy of the
// revision before as its view. Synthetic data on internal/fakeslack.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestSwitchedListWaitsForTheNewRevision(t *testing.T) {
	app, _, _ := shotsApp(t)
	const ch = "C0SWITCHW1"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "lunch", IsChannel: true, IsMember: true, NumMembers: 12})
	app.S.PutConv(slack.Conversation{ID: mirrorWaitOther, Name: "elsewhere", IsChannel: true, IsMember: true, NumMembers: 12})
	msg := lunchMsg()
	app.S.SetLatest(ch, []slack.Message{msg}, false)
	patchedEnv(t, app, maskJS(ch, false))
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	h := coreHost{app: app, th: theme.New(false)}
	split := NewChannelViewIn(h, ch, "split")
	sframe := frameFn(split)
	row := func() *row { return split.s.rows[msg.TS] }
	until(t, 10*time.Second, "the split list's row and its view", sframe, func() bool { r := row(); return r != nil && r.viewReady && !r.viewed })

	// 40 other conversations: the hub lets the split list's model go
	base := time.Now().Add(-2 * time.Hour).Unix()
	for i := 0; i < maxModels; i++ {
		oc := fmt.Sprintf("C0SWITOT%02d", i)
		ts := fmt.Sprintf("%d.000100", base+int64(i))
		app.S.PutConv(slack.Conversation{ID: oc, Name: fmt.Sprintf("other-%d", i), IsChannel: true, IsMember: true, NumMembers: 5})
		app.S.SetLatest(oc, []slack.Message{{Type: "message", TS: ts, User: "UANA00001", Text: "hello " + oc}}, false)
		ov := NewChannelView(h, oc)
		until(t, 5*time.Second, oc+" drawn", frameFn(ov), func() bool { return itemHeight(ov.s, ts) > 0 })
	}
	hb := split.s.hb
	hb.mu.Lock()
	held := hb.chans[ch] == split.s.m
	hb.mu.Unlock()
	if held {
		t.Fatal("the hub still keeps the split list's model after 40 other conversations")
	}

	// the hub's new model for the conversation (the main pane's, below)
	// numbers the message's next copy as the dropped one numbered the
	// copy the row drew: what a first load does whenever nothing bumped
	// the dropped model's number since its own (a conversion again, a
	// reaction), set here so it doesn't hang on that
	oldRev := row().rev
	nm := hb.channel(ch)
	nm.mu.Lock()
	nm.nextRev = oldRev - 1
	nm.mu.Unlock()

	// the message is edited meanwhile, the mirror behind the edit
	release := tautOf().HoldMirror()
	defer release()
	injectEvent(app, mirrorWaitOther, "user_typing", "", map[string]any{"user": "UBEN00001"})
	editMsg(app, ch, msg.TS, "UANA00001", "lunch at one?")
	// the main pane opens the conversation: a new model, the message
	// numbered as the dropped one numbered it
	main := NewChannelView(h, ch)
	until(t, 5*time.Second, "the main pane's row", frameFn(main), func() bool {
		r := main.s.rows[msg.TS]
		return r != nil && strings.Contains(rowText(r), "lunch at one?")
	})
	if main.s.m != nm {
		t.Fatal("the main pane doesn't draw the hub's new model")
	}
	if r := main.s.rows[msg.TS]; r.rev != oldRev {
		t.Fatalf("the new model numbers the edited copy %d, the dropped one numbered the old %d: no collision to test", r.rev, oldRev)
	}
	// the split list draws again, on the hub's model now: every frame
	// draws the edit until JS's copy of it comes
	for dl := time.Now().Add(600 * time.Millisecond); time.Now().Before(dl); {
		sframe()
		if r := row(); r == nil || !strings.Contains(rowText(r), "lunch at one?") {
			got := ""
			if r != nil {
				got = rowText(r)
			}
			t.Fatalf("the split list switched to the hub's model and drew %q: JS's copy of the message before the edit", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if split.s.m != main.s.m {
		t.Fatal("the split list didn't switch to the hub's model")
	}
	release()
	until(t, 5*time.Second, "JS's copy of the edit", sframe, func() bool {
		r := row()
		return r != nil && r.viewReady && strings.Contains(rowText(r), "lunch at one?")
	})
}
