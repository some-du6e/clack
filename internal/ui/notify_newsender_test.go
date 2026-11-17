package ui

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// A socket message from someone core never fetched goes to the views at
// once (core onMessage asks nobody first: the rows ask for the people
// they draw), and its desktop notification waits for them, as Slack's
// showNotification awaits ensureModelsArePresent for the sender and the
// people the text mentions (at most 5 s): it names both, never the id or
// nobody (msg-latency). The message comes the socket's way into a
// conversation nobody opened and into a thread that isn't cached, where
// no window or thread holds it: most notifications. The body is built
// from the socket's own copy, converted again once the people are in.

// noteSocket returns a func that brings ev over core's socket path and
// hands the message event core emits for it to the UI's backend, as the
// subscription does.
func noteSocket(t *testing.T, a *App, ca *core.App) func(ev map[string]any) {
	evs, stop := ca.Subscribe()
	t.Cleanup(stop)
	var mu sync.Mutex
	got := map[string]core.Event{}
	go func() {
		for ev := range evs {
			if ev.Type == "message" {
				mu.Lock()
				got[ev.TS] = ev
				mu.Unlock()
			}
		}
	}()
	return func(ev map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(ev)
		chRaw, _ := json.Marshal(ev["channel"])
		ca.Inject(slack.Event{Type: "message", Channel: chRaw, Raw: raw})
		ts := ev["ts"].(string)
		for dl := time.Now().Add(10 * time.Second); time.Now().Before(dl); time.Sleep(5 * time.Millisecond) {
			mu.Lock()
			e, ok := got[ts]
			mu.Unlock()
			if ok {
				a.data.handle(a, e)
				return
			}
		}
		t.Fatalf("core never emitted the message %s", ts)
	}
}

func TestNotificationWaitsForNewPeople(t *testing.T) {
	cases := []struct {
		name   string
		thread bool // a reply under a root no thread cache holds
	}{
		{"a conversation nobody opened", false},
		{"a thread that isn't cached", true},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, ca, fs, rec := noteApp(t)
			socket := noteSocket(t, a, ca)
			fs.SetFault("edge/users/info", fakeslack.Fault{Delay: 300 * time.Millisecond})
			sender, mentioned := fakeslack.SynthMemberID(7+10*i), fakeslack.SynthMemberID(8+10*i)
			ch := fs.ChannelIDs()[2]
			ts := "1762520199.00030" + string(rune('0'+i))
			ev := map[string]any{"type": "message", "channel": ch, "user": sender, "ts": ts, "event_ts": ts,
				"text": "<@" + fakeslack.SelfID + "> and <@" + mentioned + ">, can you two look?"}
			if tc.thread {
				ev["thread_ts"] = "1762520100.000100"
			}
			socket(ev)
			if _, held := ca.StoredMessage(ch, ts); held {
				t.Fatal("a window or thread holds the message: the case needs one nobody holds")
			}
			n := rec.wait(t, a, 1)
			senderName := strings.ToLower(strings.TrimPrefix(sender, "U0")) // synthNNNNNN, the synthetic member's display name
			mentionName := strings.ToLower(strings.TrimPrefix(mentioned, "U0"))
			if !strings.Contains(n.Title+" "+n.Body, senderName) {
				t.Errorf("the notification %q / %q doesn't name the sender %s", n.Title, n.Body, senderName)
			}
			if !strings.Contains(n.Body, mentionName) || strings.Contains(n.Body, mentioned) {
				t.Errorf("the body is %q, want the mention named %s (never its id)", n.Body, mentionName)
			}
		})
	}
}

// Two messages from the same new person notify in the order they came,
// and the person is asked for once: the second's wait shares the first's
// users/info (Slack's model fetch is one request per member while it is
// out), even when a second request would have answered first.
func TestNotificationsFromNewPersonInOrder(t *testing.T) {
	a, ca, fs, rec := noteApp(t)
	socket := noteSocket(t, a, ca)
	fs.SetFault("edge/users/info", fakeslack.Fault{Delay: 700 * time.Millisecond, Times: 1})
	sender := fakeslack.SynthMemberID(41)
	ch := fs.ChannelIDs()[2]
	asked := fs.CallCount("edge/users/info")
	for i, text := range []string{"first", "second"} {
		ts := "1762520299.00010" + string(rune('0'+i))
		socket(map[string]any{"type": "message", "channel": ch, "user": sender, "ts": ts, "event_ts": ts,
			"text": "<@" + fakeslack.SelfID + "> " + text})
		time.Sleep(50 * time.Millisecond) // the second comes while the first waits
	}
	rec.wait(t, a, 2)
	rec.mu.Lock()
	first, second := rec.notes[0].Body, rec.notes[1].Body
	rec.mu.Unlock()
	if !strings.Contains(first, "first") || !strings.Contains(second, "second") {
		t.Errorf("the notifications came as %q, then %q", first, second)
	}
	if got := fs.CallCount("edge/users/info") - asked; got != 1 {
		t.Errorf("%d users/info calls for the one new person, want 1", got)
	}
}
