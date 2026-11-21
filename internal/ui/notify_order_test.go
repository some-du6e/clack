package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
)

// Notifications reach the system in the order their messages came (Slack
// shows each from messageReceived, in the socket event's task): the
// continuations of the ones that waited for lookups, their
// showNotification dispatches and their hand-over to the system each went
// through a goroutine of their own, and any two could swap.

// noteEvents brings evs over core's socket path and returns the message
// events core emits for them, in the order of evs, as the subscription
// hands them to the UI's backend.
func noteEvents(t *testing.T, ca *core.App, evs ...map[string]any) []core.Event {
	t.Helper()
	sub, stop := ca.Subscribe()
	defer stop()
	var mu sync.Mutex
	got := map[string]core.Event{}
	go func() {
		for e := range sub {
			if e.Type == "message" {
				mu.Lock()
				got[e.TS] = e
				mu.Unlock()
			}
		}
	}()
	for _, ev := range evs {
		raw, _ := json.Marshal(ev)
		chRaw, _ := json.Marshal(ev["channel"])
		ca.Inject(slack.Event{Type: "message", Channel: chRaw, Raw: raw})
	}
	out := make([]core.Event, 0, len(evs))
	for dl := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		mu.Lock()
		out = out[:0]
		for _, ev := range evs {
			if e, ok := got[ev["ts"].(string)]; ok {
				out = append(out, e)
			}
		}
		mu.Unlock()
		if len(out) == len(evs) {
			return out
		}
		if time.Now().After(dl) {
			t.Fatalf("core emitted %d of the %d messages", len(out), len(evs))
		}
	}
}

// noteSocket returns a func that brings ev over core's socket path and
// hands the message event core emits for it to the UI's backend, as the
// subscription does.
func noteSocket(t *testing.T, a *App, ca *core.App) func(ev map[string]any) {
	return func(ev map[string]any) {
		t.Helper()
		for _, e := range noteEvents(t, ca, ev) {
			a.data.handle(a, e)
		}
	}
}

// noteBodies is what the notifications rec holds read, in order.
func noteBodies(rec *noteRec) []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make([]string, len(rec.notes))
	for i, n := range rec.notes {
		out[i] = n.Body
	}
	return out
}

// A burst of messages notifies in the order the messages came, whether
// there is nothing to wait for or every notification waits for its
// sender (one users/info for all of them, which answers them together).
func TestNotificationBurstInOrder(t *testing.T) {
	for _, wait := range []bool{false, true} {
		name := "nothing to wait for"
		if wait {
			name = "each waits for its sender"
		}
		t.Run(name, func(t *testing.T) {
			a, ca, fs, rec := noteApp(t)
			ch := fs.ChannelIDs()[2]
			ca.S.PutConv(slack.Conversation{ID: ch, Name: "burst", IsChannel: true, IsMember: true})
			sender := fs.UserIDs()[3]
			users := []slack.User{{ID: fakeslack.SelfID, Name: "self"}}
			if wait {
				sender = fakeslack.SynthMemberID(61) // someone the store doesn't hold
				fs.SetFault("edge/users/info", fakeslack.Fault{Delay: 200 * time.Millisecond})
			} else {
				users = append(users, slack.User{ID: sender, Name: "ada", Profile: slack.Profile{DisplayName: "ada"}})
			}
			ca.S.PutUsers(users)
			const n = 12
			var evs []map[string]any
			for k := 0; k < n; k++ {
				ts := fmt.Sprintf("1762520399.0001%02d", k)
				evs = append(evs, map[string]any{"type": "message", "channel": ch, "user": sender, "ts": ts, "event_ts": ts,
					"text": fmt.Sprintf("<@%s> burst %02d", fakeslack.SelfID, k)})
			}
			for _, e := range noteEvents(t, ca, evs...) { // back to back, as the pump hands a burst over
				a.data.handle(a, e)
			}
			rec.wait(t, a, n)
			time.Sleep(50 * time.Millisecond) // any straggler
			a.drain()
			got := noteBodies(rec)
			if len(got) != n {
				t.Fatalf("%d notifications for %d messages: %q", len(got), n, got)
			}
			for k, b := range got {
				if want := fmt.Sprintf("burst %02d", k); !strings.Contains(b, want) {
					t.Errorf("notification %d reads %q, want %q: out of order (all: %q)", k, b, want, got)
					break
				}
			}
		})
	}
}

// A socket message from someone core never fetched goes to the views at
// once (core announces it without asking: the rows ask for the people
// they draw), and its desktop notification waits for them, as Slack's
// showNotification awaits ensureModelsArePresent for the sender and the
// people the text mentions (at most 5 s): it names both, never the id or
// nobody. The message comes the socket's way into a conversation nobody
// opened and into a thread that isn't cached, where no window or thread
// holds it: most notifications. The body is built from the socket's own
// copy, converted again once the people are in.
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
			ca.S.PutUsers([]slack.User{{ID: fakeslack.SelfID, Name: "self"}}) // the viewer, as every session holds them (a reply's mention reads it)
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
// out). Both waits end together, and the goroutine each waited on could
// post the second first.
func TestNotificationsFromNewPersonInOrder(t *testing.T) {
	a, ca, fs, rec := noteApp(t)
	socket := noteSocket(t, a, ca)
	fs.SetFault("edge/users/info", fakeslack.Fault{Delay: 700 * time.Millisecond, Times: 1})
	sender := fakeslack.SynthMemberID(41)
	ch := fs.ChannelIDs()[2]
	asked := fs.CallCount("edge/users/info")
	texts := []string{"first", "second", "third", "fourth"}
	for i, text := range texts {
		ts := "1762520299.00010" + string(rune('0'+i))
		socket(map[string]any{"type": "message", "channel": ch, "user": sender, "ts": ts, "event_ts": ts,
			"text": "<@" + fakeslack.SelfID + "> " + text})
		time.Sleep(20 * time.Millisecond) // the next comes while the first waits
	}
	rec.wait(t, a, len(texts))
	got := noteBodies(rec)
	for i, text := range texts {
		if !strings.Contains(got[i], text) {
			t.Errorf("the notifications came as %q", got)
			break
		}
	}
	if n := fs.CallCount("edge/users/info") - asked; n != 1 {
		t.Errorf("%d users/info calls for the one new person, want 1", n)
	}
}

// Every notification of a conversation keeps its place: one with nothing
// to look up waits behind an earlier one of the same conversation still
// waiting for its sender (noteOrder, by conversation). Shown at once, the
// known sender's toast went out ahead of the new person's before it.
func TestNotificationsOfAConversationKeepTheirOrder(t *testing.T) {
	a, ca, fs, rec := noteApp(t)
	ch := fs.ChannelIDs()[2]
	ca.S.PutConv(slack.Conversation{ID: ch, Name: "order", IsChannel: true, IsMember: true})
	known := fs.UserIDs()[3]
	ca.S.PutUsers([]slack.User{{ID: fakeslack.SelfID, Name: "self"}, {ID: known, Name: "ada", Profile: slack.Profile{DisplayName: "ada"}}})
	fs.SetFault("edge/users/info", fakeslack.Fault{Delay: 300 * time.Millisecond})
	newbie := fakeslack.SynthMemberID(71)
	var evs []map[string]any
	for i, c := range []struct{ user, text string }{{newbie, "first, from someone new"}, {known, "second, from someone known"}} {
		ts := fmt.Sprintf("1762520499.0001%02d", i)
		evs = append(evs, map[string]any{"type": "message", "channel": ch, "user": c.user, "ts": ts, "event_ts": ts,
			"text": "<@" + fakeslack.SelfID + "> " + c.text})
	}
	for _, e := range noteEvents(t, ca, evs...) {
		a.data.handle(a, e)
	}
	rec.wait(t, a, 2)
	got := noteBodies(rec)
	if len(got) != 2 || !strings.Contains(got[0], "first") || !strings.Contains(got[1], "second") {
		t.Errorf("the notifications came as %q, want the new person's first", got)
	}
}

// timedNotes records when each desktop notification reached the system.
type timedNotes struct {
	mu    sync.Mutex
	notes []*desktopNote
	at    []time.Time
}

func (r *timedNotes) Notify(title, body string) error {
	return r.NotifyNote(&desktopNote{Title: title, Body: body})
}
func (r *timedNotes) NotifyActions(title, body string, _ []string, _ func(string)) error {
	return r.Notify(title, body)
}
func (r *timedNotes) NotifyNote(n *desktopNote) error {
	r.mu.Lock()
	r.notes = append(r.notes, n)
	r.at = append(r.at, time.Now())
	r.mu.Unlock()
	return nil
}

// Each message's notification goes through the plugins' showNotification
// patch on its own: in order, without waiting for the previous one's JS
// round trips. Slack's messageReceived dispatches showNotification for
// each message without awaiting the one before. With a patch (Taut's
// ShutUpSlackbot is on by default) and the plugin JS behind, a burst of
// DMs must neither come out of order nor queue each behind the last.
func TestNotificationsDontWaitForEachOther(t *testing.T) {
	// a CLACK_HOME of its own: Taut keeps its plugins in $CLACK_HOME/taut
	// (else ~/.config/clack/taut, shared by every test in the process and
	// on the runner), and enabled there the plugin ran in every later
	// test's App (TestPreviewFooterUnderComposerPlugin lost slackc's bridge
	// to its runtime) and -count's next run couldn't import it again
	t.Setenv("CLACK_HOME", t.TempDir())
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	c := e.a.data.core
	c.SetRealtime(true)
	e.until("the socket", 20*time.Second, func() bool { return c.RTM != nil && c.RTM.Connected() })
	e.until("Taut", 60*time.Second, func() bool { return e.a.Taut() != nil })
	tt := e.a.Taut()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// an id of its own each run too, whatever else outlives the App
	pluginID := fmt.Sprintf("NotifyPassThrough%d", time.Now().UnixNano())
	id, err := tt.Import(ctx, []byte(`(() => {
const TautPlugin = globalThis.TautPlugin
class NotifyPassThrough extends TautPlugin {
  static id = '`+pluginID+`'
  static pluginName = 'Notify pass-through'
  static description = 'Wraps showNotification and changes nothing'
  static category = 'notifications'
  static authors = ['test']
  static defaultConfig = { enabled: false }
  start() { this.off = this.api.redux.patchThunk('showNotification', (orig) => (...a) => orig(...a)) }
  stop() { this.off?.() }
}
return NotifyPassThrough
})()`), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	e.until("the showNotification patch", 10*time.Second, func() bool { return tt.ThunkPatched("showNotification") })
	rt, err := tt.JS()
	if err != nil || rt == nil {
		t.Fatalf("no JS runtime: %v", err)
	}
	rec := &timedNotes{}
	e.a.notify = rec

	// the plugin JS stays 400 ms behind: 40 tasks of 10 ms, topped up
	const each, depth = 10 * time.Millisecond, 40
	for i := 0; i < depth; i++ {
		rt.Post(func(*jsrt.Ctx) { time.Sleep(each) })
	}
	stop := make(chan struct{})
	var bg sync.WaitGroup
	bg.Add(1)
	go func() {
		defer bg.Done()
		for {
			select {
			case <-stop:
				return
			case <-time.After(each):
				rt.Post(func(*jsrt.Ctx) { time.Sleep(each) })
			}
		}
	}()
	defer func() {
		close(stop)
		bg.Wait()
		_ = rt.Do(func(*jsrt.Ctx) error { return nil })
	}()

	const dm = "D0FAKE00000"
	partner := fs.UserIDs()[1] // the DM's member (fakeslack's convs)
	var sent []time.Time
	for k := 0; k < 3; k++ {
		_, ev := fs.PostQuiet(dm, partner, fmt.Sprintf("dm %d of the burst", k))
		sent = append(sent, time.Now())
		fs.Push(ev)
		e.settle(100 * time.Millisecond)
	}
	if !e.untilSoft(15*time.Second, func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return len(rec.notes) >= 3
	}) {
		t.Fatalf("%d of the 3 notifications came", len(rec.notes))
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	for k := 0; k < 3; k++ {
		if !strings.Contains(rec.notes[k].Body, fmt.Sprintf("dm %d of the burst", k)) {
			t.Errorf("notification %d reads %q: out of order", k, rec.notes[k].Body)
		}
		d := rec.at[k].Sub(sent[k])
		t.Logf("notification %d: %v after its message (the JS %v behind)", k, d.Round(time.Millisecond), depth*each)
		if d > 3*depth*each {
			t.Errorf("notification %d came %v after its message, with the plugin JS %v behind: it waited for the ones before it", k, d.Round(time.Millisecond), depth*each)
		}
	}
}
