package core

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A message RefetchMessages can't get (its every fetch fails, a server
// error) is gone once core asked for it the last time while the client is
// online, as Slack's UnknownFetchManager marks what its fetch didn't
// bring non-existent unless offline; offline it stays unknown (a view's
// next call asks again), so its row is never a loading skeleton for ever
// online.
func TestRefetchGivesUpOnline(t *testing.T) {
	for _, online := range []bool{true, false} {
		t.Run(map[bool]string{true: "online", false: "offline"}[online], func(t *testing.T) {
			oldProbe, oldBackoff := clientOnlineProbe, refetchBackoff
			clientOnlineProbe = func() bool { return online }
			refetchBackoff = func(int) time.Duration { return 5 * time.Millisecond }
			t.Cleanup(func() { clientOnlineProbe, refetchBackoff = oldProbe, oldBackoff })
			fs, err := fakeslack.Start(fakeslack.Small)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = fs.Close() })
			fs.SetFault("messages.list", fakeslack.Fault{Status: 500})
			a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
			t.Cleanup(a.Shutdown)
			evs, stop := a.Subscribe()
			defer stop()
			const ch, ts = "C0FAKE00000", "1700000000.000100"
			a.RefetchMessages(ch, []string{ts})
			wait := time.After(10 * time.Second)
			if !online {
				wait = time.After(3 * time.Second)
			}
			for {
				select {
				case ev := <-evs:
					if ev.Type == "gone" && ev.Channel == ch && ev.TS == ts {
						if !online {
							t.Fatal("offline, a message whose fetches failed is gone")
						}
						if c, ok := a.MessageChange(ch, ts); !ok || !c.Deleted {
							t.Errorf("the gone message's change %+v %v, want Deleted", c, ok)
						}
						return
					}
				case <-wait:
					if online {
						t.Fatal("online, a message whose every fetch failed never went: its row loads for ever")
					}
					return
				}
			}
		})
	}
}

// A messages.list group Slack refuses (one channel the viewer can't read)
// is asked again channel by channel: the other channels' messages come,
// and the refused channel answered none (its messages non-existent), so
// no row waits on it.
func TestMessagesListSplitsARefusedGroup(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	const bad = "C0REFUSED1"
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "messages.list" {
			return nil, false
		}
		var want []struct {
			Channel    string   `json:"channel"`
			Timestamps []string `json:"timestamps"`
		}
		_ = json.Unmarshal([]byte(f.Get("message_ids")), &want)
		out := map[string][]slack.Message{}
		for _, w := range want {
			if w.Channel == bad {
				return map[string]any{"ok": false, "error": "channel_not_found"}, true
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
	if err != nil {
		t.Errorf("err %v: the refused channel is an answer", err)
	}
	for _, ch := range []string{"C0GOOD0001", "C0GOOD0002", bad} {
		if !answered[ch] {
			t.Errorf("%s not answered", ch)
		}
	}
	if len(got["C0GOOD0001"]) != 1 || len(got["C0GOOD0002"]) != 1 || len(got[bad]) != 0 {
		t.Errorf("messages %v", got)
	}
}
