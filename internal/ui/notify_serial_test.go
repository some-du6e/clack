package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
)

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
// ShutUpSlackbot is on by default) and the plugin JS behind, the k-th of
// a burst of DMs came about 2k-1 backlogs after its message.
func TestNotificationsDontWaitForEachOther(t *testing.T) {
	e := newLatEnv(t, latEnvOpts{})
	e.until("Taut", 60*time.Second, func() bool { return e.a.Taut() != nil })
	tt := e.a.Taut()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	id, err := tt.Import(ctx, []byte(`(() => {
const TautPlugin = globalThis.TautPlugin
class NotifyPassThrough extends TautPlugin {
  static id = 'NotifyPassThrough'
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
	partner := e.fs.UserIDs()[1] // the DM's member (fakeslack's convs)
	var sent []time.Time
	for k := 0; k < 3; k++ {
		ts, ev := e.fs.PostQuiet(dm, partner, fmt.Sprintf("dm %d of the burst", k))
		_ = ts
		sent = append(sent, time.Now())
		e.fs.Push(ev)
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
