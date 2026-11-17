package views

// The Threads view and the Saved page draw a message's edit and deletion
// in the frame after core gets them, with no reload: Slack's Threads view
// draws its threads with the thread pane's components over the messages
// store, and its saved rows read the store as they render (LaterItemRow's
// getMessageByTimestamp). On internal/fakeslack's views fixtures (made-up
// people and messages); the edits reach the client as the socket's
// message_changed and message_deleted (core.Inject).

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
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// lvDrawn is the text b's cached body of ch/ts laid out last (msgBody's
// richview, keyed by the message).
func lvDrawn(b *base, ch, ts string) string {
	if r := b.rich[ch+"/"+ts]; r != nil {
		return r.v.TextRange(0, r.v.PlainLen())
	}
	return ""
}

// lvInject is the socket bringing ev in ch (core.Inject).
func lvInject(app *core.App, ch string, ev map[string]any) {
	evTS := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
	ev["ts"], ev["event_ts"], ev["channel"] = evTS, evTS, ch
	raw, _ := json.Marshal(ev)
	sub, _ := ev["subtype"].(string)
	typ, _ := ev["type"].(string)
	cj, _ := json.Marshal(ch)
	app.Inject(slack.Event{Type: typ, Subtype: sub, Channel: cj, Raw: raw})
}

// A followed thread's reply, edited and then deleted elsewhere: the card
// draws the edit in the next frame and leaves the reply out in the one
// after the deletion (its keyboard row too), with no getView.
func TestThreadsLiveMessages(t *testing.T) {
	app, fs := vaFake(t)
	v := NewThreads(&slickHost{app: app, th: theme.New(false)})
	e := &vaEnv{size: image.Pt(1000, 1100)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	const ch, reply = fakeslack.ViewsChannel, "1762600200.000100" // the unread thread's follow-up
	e.until(t, "the follow-up drawn", func() bool { return strings.Contains(lvDrawn(&v.base, ch, reply), "a synthetic follow-up") })
	e.frame()
	views := fs.CallCount("subscriptions.thread.getView")
	prev := slack.Message{Type: "message", TS: reply, ThreadTS: fakeslack.ViewsThreadUnread, User: "U0FAKE00002", Text: "a synthetic follow-up"}
	edited := prev
	edited.Text = "a synthetic follow-up, said better"
	edited.Edited = &slack.Edited{User: "U0FAKE00002", TS: fmt.Sprintf("%d.000001", time.Now().Unix())}
	lvInject(app, ch, map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "message": edited, "previous_message": prev})
	e.frame()
	if got := lvDrawn(&v.base, ch, reply); !strings.Contains(got, "said better") {
		t.Fatalf("the frame after the edit drew %q", got)
	}
	key := ch + "/" + fakeslack.ViewsThreadUnread + "-" + reply
	if _, ok := v.kbRows[key]; !ok {
		t.Fatalf("no keyboard row %s before the deletion (have %d)", key, len(v.kbRows))
	}
	lvInject(app, ch, map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "deleted_ts": reply, "previous_message": edited})
	e.frame()
	e.frame() // the keyboard's rows are the last frame's
	if _, ok := v.kbRows[key]; ok {
		t.Error("the deleted reply is still a row of the card")
	}
	for i := 0; i < 10; i++ {
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	if n := fs.CallCount("subscriptions.thread.getView") - views; n != 0 {
		t.Errorf("%d getView calls for an edit and a deletion; Slack's view reads the store", n)
	}
}

// A saved message edited, then deleted: the Saved page draws the edit in
// the next frame and the deletion as Later's "no longer available" in
// the one after, with no saved.list.
func TestSavedLiveMessages(t *testing.T) {
	const ch, ts = fakeslack.ViewsChannel, "1762800100.000100"
	var mu sync.Mutex
	text, gone := "a synthetic plan to look over", false
	app, fs := vkFake(t, func(method string, f url.Values) (any, bool) {
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "saved.list":
			return map[string]any{"ok": true, "saved_items": []map[string]any{{"item_id": ch, "item_type": "message", "ts": ts,
				"todo_state": "saved", "date_created": time.Now().Unix() - 100}}}, true
		case "messages.list":
			out := map[string][]map[string]any{}
			if !gone {
				out[ch] = []map[string]any{{"type": "message", "ts": ts, "user": "U0FAKE00001", "text": text}}
			}
			return map[string]any{"ok": true, "messages": out}, true
		}
		return nil, false
	})
	s := NewSaved(newKBHost(app, false))
	e := &vaEnv{size: image.Pt(1000, 900)}
	e.draw = func(gtx layout.Context) { s.Layout(gtx) }
	e.until(t, "the saved message drawn", func() bool { return strings.Contains(lvDrawn(&s.base, ch, ts), "a synthetic plan") })
	e.frame()
	lists := fs.CallCount("saved.list")
	prev := slack.Message{Type: "message", TS: ts, User: "U0FAKE00001", Text: text}
	edited := prev
	edited.Text = "a synthetic plan, revised"
	edited.Edited = &slack.Edited{User: "U0FAKE00001", TS: fmt.Sprintf("%d.000001", time.Now().Unix())}
	mu.Lock()
	text = edited.Text
	mu.Unlock()
	lvInject(app, ch, map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "message": edited, "previous_message": prev})
	e.frame()
	if got := lvDrawn(&s.base, ch, ts); !strings.Contains(got, "revised") {
		t.Fatalf("the frame after the edit drew %q", got)
	}
	mu.Lock()
	gone = true
	mu.Unlock()
	lvInject(app, ch, map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "deleted_ts": ts, "previous_message": edited})
	e.frame()
	if f, i := s.find(ch + "/" + ts); f == nil || f.items[i].Msg != nil || !f.items[i].Gone {
		t.Fatal("the frame after the deletion still has the saved message")
	}
	for i := 0; i < 10; i++ {
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	if n := fs.CallCount("saved.list") - lists; n != 0 {
		t.Errorf("%d saved.list calls for an edit and a deletion; Slack's rows read the store", n)
	}
}
