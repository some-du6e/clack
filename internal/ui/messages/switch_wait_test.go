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

	oldRev := row().rev // the number the dropped model gave the copy the row drew

	// JS's mirror holds the channel's messages as they are before the
	// edit: the 40 others' events may have had it let them go, and its
	// next read after the edit would take the edited copy from core
	until(t, 5*time.Second, "JS's copy of the message before the edit", func() {}, func() bool {
		_, _, ready, ok := tautOf().MessageViewState(ch, msg.TS, false, 0)
		return ok && ready
	})

	// the message is edited meanwhile, the mirror behind the edit
	release := tautOf().HoldMirror()
	defer release()
	injectEvent(app, mirrorWaitOther, "user_typing", "", map[string]any{"user": "UBEN00001"})
	editMsg(app, ch, msg.TS, "UANA00001", "lunch at one?")
	if mv, ok := tautOf().MessageView(ch, msg.TS); !ok || mv.Text != msg.Text {
		t.Fatalf("with the mirror behind the edit, JS's copy of the message is %q (ok %v): nothing stale to draw", mv.Text, ok)
	}
	// the main pane opens the conversation: a new model, which numbers the
	// edited copy as the dropped one numbered the copy the row drew (what
	// a first load does when nothing else bumped either count, an event
	// before the load or a conversion again: set here so the test doesn't
	// hang on that)
	main := NewChannelView(h, ch)
	mframe := frameFn(main)
	until(t, 5*time.Second, "the main pane's row", mframe, func() bool {
		r := main.s.rows[msg.TS]
		return r != nil && strings.Contains(rowText(r), "lunch at one?")
	})
	nm := main.s.m
	nm.mu.Lock()
	if i := nm.find(msg.TS); i >= 0 {
		nm.msgs[i].rev = oldRev
	}
	if nm.nextRev < oldRev {
		nm.nextRev = oldRev
	}
	nm.rev++
	nm.mu.Unlock()
	until(t, 5*time.Second, "the main pane on the renumbered copy", mframe, func() bool {
		r := main.s.rows[msg.TS]
		return r != nil && r.rev == oldRev && strings.Contains(rowText(r), "lunch at one?")
	})
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
