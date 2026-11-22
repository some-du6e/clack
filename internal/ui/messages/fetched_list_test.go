package messages

// A message fetched by id (Activity's hydration, Saved, the Threads view's
// getView) is in the plugins' messages slice, as Slack's one messages
// store holds it, but never in a conversation's list: the list draws the
// conversation's history (Slack's channelHistory), which a message fetched
// by id never joins. Under any plugin patching the slice (ShowRealUser is
// on by default in Hack Club), the fetched copy of a message older than
// the loaded page came back as one a plugin injected: the list drew it
// glued above the latest page, and a jump to it placed it there at once
// instead of paging back to it (the reviews, 941a25a0). Synthetic data on
// internal/fakeslack.

import (
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// drawsTS reports whether the list's built items draw message ts.
func drawsTS(v *ChannelView, ts string) bool {
	s := v.s
	for _, it := range s.items {
		if it.kind == itMsg && it.idx < len(s.snap.msgs) && s.snap.msgs[it.idx].m.TS == ts {
			return true
		}
	}
	return false
}

func TestFetchedMessageStaysOutOfTheList(t *testing.T) {
	app, fs, _ := shotsApp(t)
	const ch = "C0FETCHOLD1"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "general", IsChannel: true, IsMember: true, NumMembers: 40})
	// an old message, then more than the latest page (50) after it
	oldTS, _ := fs.PostQuiet(ch, "UANA00001", "a synthetic message from last week")
	for i := 0; i < 80; i++ {
		fs.PostQuiet(ch, "UBEN00001", fmt.Sprintf("a synthetic filler message %d", i))
	}
	// someone's reaction to it brought it to Activity, which fetched it by id
	old := slack.Message{Type: "message", TS: oldTS, User: "UANA00001", Text: "a synthetic message from last week"}
	app.S.PutFetchedAsked(ch, []string{oldTS}, []slack.Message{old}, 0)

	patchedEnv(t, app, maskJS(ch, false))
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
	frame := frameFn(v)
	until(t, 20*time.Second, "the latest page", frame, func() bool { return v.s.snap.loaded && len(v.s.snap.msgs) > 0 })
	if modelHas(v, oldTS, func(*core.Message) bool { return true }) {
		t.Fatal("the latest page holds the old message: the history isn't longer than a page")
	}
	// the plugins' slice holds the fetched copy, as Slack's store does
	until(t, 10*time.Second, "JS's copy of the fetched message", frame, func() bool {
		_, ok := tautOf().MessageView(ch, oldTS)
		return ok
	})

	// the list draws the channel's history: never the fetched message
	for dl := time.Now().Add(3 * time.Second); time.Now().Before(dl); time.Sleep(15 * time.Millisecond) {
		frame()
		if drawsTS(v, oldTS) {
			t.Errorf("the list drew the message fetched by id above its latest page (%d rows): the plugins' copy taken for one a plugin injected", len(v.s.snap.msgs))
			break
		}
	}

	// a jump to it reads the history to its place: the window around it
	// (Slack's channelHistory slice: conversations.history around the
	// message), or pages back to it, never the fetched copy placed at once
	v.Highlight(oldTS)
	until(t, 30*time.Second, "the list reading the history to the message", frame, func() bool {
		return modelHas(v, oldTS, func(*core.Message) bool { return true }) && drawsTS(v, oldTS)
	})
	if !v.InWindow() && v.s.jumpPages == 0 {
		t.Error("the jump placed the message without reading the history to it (no window around it, no page back)")
	}
}
