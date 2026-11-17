package core

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// Socket events never wait on the network (msg-latency). Slack's socket
// handler puts a message into its store the moment the message arrives
// (boot-render messageReceived, after client-boot duFi's
// preprocessRtmMessage, which looks up only an unknown channel and the
// member count an @channel needs); the people a message names are asked
// for by the rows that draw them, and only the desktop notification
// waits for them (gaJC showNotification's ensureModelsArePresent, 5 s).
// An Activity entry and a typer go into their stores at once too
// (handleActivityRTMEvent; the typing line maps its typers through
// getMemberById as it renders). Clack's event pump asked users/info for
// a new message's sender and mentions, for a typer in the open
// conversation, and messages.list and users/info for an Activity entry,
// before it emitted anything: one event at a time, so every message
// behind them waited too (Karim on α.102: messages "a second" late and
// "clumped", while Activity was already up to date).
func TestSocketEventsDontWaitForLookups(t *testing.T) {
	a, fs, l := rtApp(t)
	ch := fs.ChannelIDs()[0]
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := a.History(ctx, ch, ""); err != nil { // the open conversation: watched
		t.Fatal(err)
	}
	// a busy Grid's lookups: users/info and messages.list take 3 s
	const slow = 3 * time.Second
	fs.SetFault("edge/users/info", fakeslack.Fault{Delay: slow})
	fs.SetFault("messages.list", fakeslack.Fault{Delay: slow})
	newcomer, mentioned, typer, author := fakeslack.SynthMemberID(1), fakeslack.SynthMemberID(2), fakeslack.SynthMemberID(3), fakeslack.SynthMemberID(4)

	now := time.Now()
	ts := func(k int) string { return fmt.Sprintf("%d.%06d", now.Unix()+int64(k), 100) }
	t0 := time.Now()
	// an Activity entry about a message core doesn't hold, from someone it
	// never fetched
	fs.Push(map[string]any{"type": "activity", "subtype": "activity_updated", "key": "at_user-" + ts(0),
		"entry": map[string]any{"key": "at_user-" + ts(0), "feed_ts": ts(0), "is_unread": true,
			"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": ts(0), "channel": ch, "author_user_id": author}}}})
	// a message from someone core never fetched, mentioning another
	fs.Push(map[string]any{"type": "message", "channel": ch, "user": newcomer, "text": "hello <@" + mentioned + ">", "ts": ts(1), "event_ts": ts(1)})
	// a typer in the open conversation core never fetched
	fs.Push(map[string]any{"type": "user_typing", "channel": ch, "user": typer})
	// and a message right after them from the viewer's own self
	fs.Push(map[string]any{"type": "message", "channel": ch, "user": fakeslack.SelfID, "text": "right behind", "ts": ts(2), "event_ts": ts(2)})

	// when each reached core's subscribers (the views), polled for long
	// enough that the old pump's waits show as numbers, not a timeout
	got := map[string]time.Duration{}
	var activity *Event
	for dl := time.Now().Add(15 * time.Second); time.Now().Before(dl) && (len(got) < 3 || activity == nil); time.Sleep(2 * time.Millisecond) {
		for _, ev := range l.all() {
			k := ""
			switch {
			case ev.Type == "message" && ev.TS == ts(1):
				k = "a new sender's message"
			case ev.Type == "message" && ev.TS == ts(2):
				k = "the message behind it"
			case ev.Type == "typing" && ev.UserID == typer:
				k = "a new typer"
			case ev.Type == "activity" && ev.ID == "at_user-"+ts(0) && activity == nil:
				ev := ev
				activity = &ev
			}
			if _, seen := got[k]; k != "" && !seen {
				got[k] = time.Since(t0)
			}
		}
	}
	for _, k := range []string{"a new sender's message", "the message behind it", "a new typer"} {
		d, ok := got[k]
		switch {
		case !ok:
			t.Errorf("%s never reached the views; events: %s", k, evTypes(l.all()))
		case d > time.Second:
			t.Errorf("%s reached the views %v after it arrived: it waited for users/info or messages.list (%v each here)", k, d.Round(time.Millisecond), slow)
		default:
			t.Logf("%s: %v", k, d.Round(time.Millisecond))
		}
	}
	// the message is stored at once; whatever draws its people asks for
	// them
	if _, ok := a.S.Find(ch, ts(1)); !ok {
		t.Error("the new sender's message isn't stored")
	}
	// the Activity entry still comes, with its message, once the lookups
	// answer
	if activity == nil {
		t.Fatalf("the Activity entry never came; events: %s", evTypes(l.all()))
	}
	if activity.Activity == nil {
		t.Error("the Activity entry came without its item")
	}
}
