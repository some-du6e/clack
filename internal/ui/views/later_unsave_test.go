package views

// "Remove deleted messages" (the Later page's clean-up item 3) unsaves
// tombstones only: a message that failed to load also has no Msg, and
// after a messages.list error (too_many_channels in Karim's clack.log)
// the old `it.Msg == nil` test would have unsaved every saved item on
// the page. On internal/fakeslack with made-up channels and messages.

import (
	"encoding/json"
	"fmt"
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
)

const (
	luHereTS = "1762800100.000100" // a message messages.list has
	luGoneTS = "1762800900.000100" // a deleted one: not in its channel's answer
)

// luChannel is the i-th made-up channel; the ids sort in i order, so
// channels 0-9 share messages.list's first group and 10-11 its second.
func luChannel(i int) string { return fmt.Sprintf("C0LU%05d", i) }

const luChannels = 12

// luFake serves saved.list with a to-do on luHereTS in each of 12
// channels, plus one on luGoneTS in channels 0 and 11, and messages.list
// with the luHereTS messages. A messages.list call carrying a channel
// failing() names answers too_many_channels. It returns the
// saved.delete calls' "channel/ts" and how many messages.list calls
// asked for these channels.
func luFake(t *testing.T, failing func(ch string) bool) (*core.App, func() []string, func() int) {
	t.Helper()
	now := time.Now().Unix()
	var items []map[string]any
	for i := range luChannels {
		items = append(items, map[string]any{"item_id": luChannel(i), "item_type": "message", "ts": luHereTS, "todo_state": "to_do", "date_created": now - int64(100+i)})
	}
	for _, i := range []int{0, 11} {
		items = append(items, map[string]any{"item_id": luChannel(i), "item_type": "message", "ts": luGoneTS, "todo_state": "to_do", "date_created": now - 50})
	}
	var mu sync.Mutex
	var deletes []string
	lists := 0
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		switch method {
		case "saved.list":
			return map[string]any{"ok": true, "saved_items": items}, true
		case "saved.delete":
			mu.Lock()
			deletes = append(deletes, f.Get("item_id")+"/"+f.Get("ts"))
			mu.Unlock()
			return map[string]any{"ok": true}, true
		case "messages.list":
			var want []struct {
				Channel    string   `json:"channel"`
				Timestamps []string `json:"timestamps"`
			}
			_ = json.Unmarshal([]byte(f.Get("message_ids")), &want)
			if len(want) > 0 && strings.HasPrefix(want[0].Channel, "C0LU") {
				mu.Lock()
				lists++
				mu.Unlock()
			}
			out := map[string][]map[string]any{}
			for _, w := range want {
				if failing(w.Channel) {
					return map[string]any{"ok": false, "error": "too_many_channels"}, true
				}
				for _, ts := range w.Timestamps {
					if ts == luHereTS {
						out[w.Channel] = append(out[w.Channel], map[string]any{"type": "message", "ts": ts, "user": "U0FAKE00001", "text": "a synthetic note in " + w.Channel})
					}
				}
			}
			return map[string]any{"ok": true, "messages": out}, true
		}
		return nil, false
	})
	for i := range luChannels {
		app.S.PutConv(slack.Conversation{ID: luChannel(i), Name: fmt.Sprintf("synthetic-%02d", i), IsChannel: true, IsMember: true, NumMembers: 3})
	}
	app.SetExperimentGroup("later_todos", "on")
	return app, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), deletes...)
		}, func() int {
			mu.Lock()
			defer mu.Unlock()
			return lists
		}
}

// luPage lays the To-dos page out until its 14 items load.
func luPage(t *testing.T, app *core.App) (*Later, *vaEnv) {
	t.Helper()
	h := newW26Host(app, false)
	l := NewLater(h)
	e := &vaEnv{size: image.Pt(620, 900)}
	e.draw = func(gtx layout.Context) { l.Layout(gtx) }
	e.until(t, "the to-dos", func() bool { return len(l.feed.items) == luChannels+2 })
	e.frame()
	return l, e
}

// luItem finds the page's item on ch/ts.
func luItem(t *testing.T, l *Later, ch, ts string) core.SavedItem {
	t.Helper()
	for _, it := range l.feed.items {
		if it.Channel == ch && it.TS == ts {
			return it
		}
	}
	t.Fatalf("no item %s/%s", ch, ts)
	return core.SavedItem{}
}

// Every messages.list call fails: no item has its message and none is
// Gone, so clean-up 3 finds nothing to remove and makes no saved.delete
// call at all.
func TestLaterCleanUpKeepsUnloadedMessages(t *testing.T) {
	app, deletes, lists := luFake(t, func(string) bool { return true })
	l, e := luPage(t, app)
	if n := lists(); n != 2 {
		t.Errorf("%d messages.list calls for %d channels, want 2 groups of up to 10", n, luChannels)
	}
	for _, it := range l.feed.items {
		if it.Msg != nil || it.Gone || laterTombstoned(&it) {
			t.Fatalf("an item whose lookup failed: Msg %v, Gone %v", it.Msg != nil, it.Gone)
		}
	}
	l.cleanUp(3)
	// a write would run off the frame loop: give one the time to land
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
	if d := deletes(); len(d) != 0 {
		t.Fatalf("clean-up unsaved %d messages that only failed to load: %v", len(d), d)
	}
	if len(l.feed.items) != luChannels+2 {
		t.Errorf("clean-up dropped rows: %d left", len(l.feed.items))
	}
}

// The second group (channels 10-11) fails: the first group's messages
// still load and draw, its deleted message is Gone, while channel 11's
// missing message says nothing (its group didn't answer). Clean-up 3
// unsaves channel 0's deleted message and nothing else.
func TestLaterCleanUpFailedGroupOnlyRemovesTombstones(t *testing.T) {
	failed := map[string]bool{luChannel(10): true}
	app, deletes, lists := luFake(t, func(ch string) bool { return failed[ch] })
	l, e := luPage(t, app)
	if n := lists(); n != 2 {
		t.Errorf("%d messages.list calls for %d channels, want 2 groups of up to 10", n, luChannels)
	}
	for i := range luChannels {
		it := luItem(t, l, luChannel(i), luHereTS)
		if loaded := i < 10; (it.Msg != nil) != loaded || it.Gone {
			t.Errorf("%s (its group answered %v): Msg %v, Gone %v", luChannel(i), loaded, it.Msg != nil, it.Gone)
		} else if loaded && it.Msg.Text != "a synthetic note in "+luChannel(i) {
			t.Errorf("%s draws %q", luChannel(i), it.Msg.Text)
		}
	}
	if it := luItem(t, l, luChannel(0), luGoneTS); !it.Gone || !laterTombstoned(&it) {
		t.Errorf("channel 0's deleted message isn't Gone: %+v", it)
	}
	if it := luItem(t, l, luChannel(11), luGoneTS); it.Gone || laterTombstoned(&it) {
		t.Errorf("channel 11's message, whose group failed, is Gone: %+v", it)
	}
	l.cleanUp(3)
	want := luChannel(0) + "/" + luGoneTS
	e.until(t, "the tombstone's saved.delete", func() bool { return len(deletes()) > 0 })
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
	if d := deletes(); len(d) != 1 || d[0] != want {
		t.Fatalf("saved.delete calls %v, want only %s", d, want)
	}
}
