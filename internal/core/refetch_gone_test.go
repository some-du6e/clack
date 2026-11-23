package core

import (
	"context"
	"encoding/json"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A message RefetchMessages can't get (its every fetch fails: a server
// error, an API error, the client online throughout) is never "gone":
// a failed fetch says nothing about it, and a gone message's Activity row
// hides, its saved item reads deleted (and "Remove deleted messages"
// unsaves it) and its thread reply drops. Core gives up locally
// (MessageGaveUp: its row stops loading), with its tries counted afresh,
// and asks again when the client comes back online; the answer brings
// the message back.
func TestRefetchGivesUpLocally(t *testing.T) {
	for _, c := range []struct {
		name  string
		fault fakeslack.Fault
	}{{"5xx", fakeslack.Fault{Status: 500}}, {"internal_error", fakeslack.Fault{Error: "internal_error"}}} {
		t.Run(c.name, func(t *testing.T) {
			fs, err := fakeslack.Start(fakeslack.Small)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = fs.Close() })
			const ch, ts = "C0FAKE00000", "1700000000.000100"
			fs.SetHook(func(method string, _ url.Values) (any, bool) {
				if method != "messages.list" {
					return nil, false
				}
				return map[string]any{"ok": true, "messages": map[string][]slack.Message{ch: {{Type: "message", TS: ts, User: "U0FAKE00001", Text: "a synthetic message"}}}}, true
			})
			fs.SetFault("messages.list", c.fault)
			a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
			a.SetRefetchBackoff(func(int) time.Duration { return 5 * time.Millisecond }) // this App's only
			t.Cleanup(a.Shutdown)
			a.setClientOnline(true)
			evs, stop := a.Subscribe()
			defer stop()
			a.RefetchMessages(ch, []string{ts})
			wait := time.After(10 * time.Second)
			for !a.MessageGaveUp(ch, ts) {
				select {
				case ev := <-evs:
					if ev.Type == "gone" && ev.Channel == ch && ev.TS == ts {
						t.Fatal("a message whose fetches failed is gone")
					}
				case <-time.After(5 * time.Millisecond):
				case <-wait:
					t.Fatal("core never gave up on a message whose every fetch failed: its row loads for ever")
				}
			}
			if ch, ok := a.MessageChange(ch, ts); ok {
				t.Errorf("a given-up message has a change %+v", ch)
			}
			a.msgLog.mu.Lock()
			r := a.msgLog.refetching[MsgKey{ch, ts}]
			a.msgLog.mu.Unlock()
			if r.fails != 0 || r.out {
				t.Errorf("after the give-up the message has %d fails (out %v), want its tries afresh", r.fails, r.out)
			}
			// back online, with the fetch working again: asked for at once
			fs.SetFault("messages.list", fakeslack.Fault{})
			a.setClientOnline(false)
			a.setClientOnline(true)
			wait = time.After(10 * time.Second)
			for {
				if _, ok := a.HeldMessage(ch, ts); ok && !a.MessageGaveUp(ch, ts) {
					break
				}
				select {
				case ev := <-evs:
					if ev.Type == "gone" && ev.Channel == ch && ev.TS == ts {
						t.Fatal("a message whose fetches failed is gone")
					}
				case <-time.After(5 * time.Millisecond):
				case <-wait:
					t.Fatal("back online, core didn't ask for the message it gave up on")
				}
			}
		})
	}
}

// A messages.list answer without a message, for a channel that answered,
// is what makes it gone (Slack's isNonExistent), unless the store got the
// message meanwhile.
func TestRefetchGoneOnlyFromAnAnswer(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	const ch, ts = "C0FAKE00000", "1700000000.000100"
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method != "messages.list" {
			return nil, false
		}
		return map[string]any{"ok": true, "messages": map[string][]slack.Message{ch: {}}}, true
	})
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	evs, stop := a.Subscribe()
	defer stop()
	a.RefetchMessages(ch, []string{ts})
	wait := time.After(10 * time.Second)
	for {
		select {
		case ev := <-evs:
			if ev.Type == "gone" && ev.Channel == ch && ev.TS == ts {
				if c, ok := a.MessageChange(ch, ts); !ok || !c.Deleted {
					t.Errorf("the gone message's change %+v %v, want Deleted", c, ok)
				}
				if a.MessageGaveUp(ch, ts) {
					t.Error("an answered message reads given up")
				}
				return
			}
		case <-wait:
			t.Fatal("a channel answered without the message, and it isn't gone")
		}
	}
}

// A failed messages.list group (one channel the viewer can't read, one
// that times out or errors) is asked again channel by channel, at once:
// the other channels' messages come, and the failing channel isn't
// answered (its messages aren't read as deleted: a refusal says nothing
// about them), its error returned.
func TestMessagesListSplitsAFailedGroup(t *testing.T) {
	for _, fail := range []map[string]any{
		{"ok": false, "error": "channel_not_found"},
		{"ok": false, "error": "internal_error"},
		nil, // a 5xx
	} {
		fs, err := fakeslack.Start(fakeslack.Small)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = fs.Close() })
		const bad = "C0REFUSED1"
		var mu sync.Mutex
		var single []string // the one-channel calls, as they came
		fs.SetHook(func(method string, f url.Values) (any, bool) {
			if method != "messages.list" {
				return nil, false
			}
			var want []struct {
				Channel    string   `json:"channel"`
				Timestamps []string `json:"timestamps"`
			}
			_ = json.Unmarshal([]byte(f.Get("message_ids")), &want)
			if len(want) == 1 {
				mu.Lock()
				single = append(single, want[0].Channel)
				mu.Unlock()
			}
			out := map[string][]slack.Message{}
			for _, w := range want {
				if w.Channel == bad {
					if fail == nil {
						return fakeslack.Reply{Status: 500}, true
					}
					return fail, true
				}
				for _, ts := range w.Timestamps {
					out[w.Channel] = append(out[w.Channel], slack.Message{Type: "message", TS: ts, User: "U0FAKE00001", Text: "a synthetic message"})
				}
			}
			return map[string]any{"ok": true, "messages": out}, true
		})
		c := slack.New(fs.Creds())
		got, answered, err := c.MessagesList(context.Background(), map[string][]string{
			"C0GOOD0001": {"1700000000.000100"}, bad: {"1700000000.000200"}, "C0GOOD0002": {"1700000000.000300"}})
		if err == nil {
			t.Errorf("%v: no error for the failing channel", fail)
		}
		for _, ch := range []string{"C0GOOD0001", "C0GOOD0002"} {
			if !answered[ch] {
				t.Errorf("%v: %s not answered", fail, ch)
			}
		}
		if answered[bad] {
			t.Errorf("%v: the failing channel is answered: its messages would read as deleted", fail)
		}
		if len(got["C0GOOD0001"]) != 1 || len(got["C0GOOD0002"]) != 1 || len(got[bad]) != 0 {
			t.Errorf("%v: messages %v", fail, got)
		}
		mu.Lock()
		if len(single) != 3 {
			t.Errorf("%v: one-channel calls %v, want each of the group's 3", fail, single)
		}
		mu.Unlock()
	}
}
