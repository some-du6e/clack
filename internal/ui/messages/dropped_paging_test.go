package messages

// A list keeps its model when the hub lets it go, and core's message
// cache resets a conversation nobody opened for an hour. Shown again, the
// list's model reloads core's window from the server; while that reload
// was out the list paged back from the window it held (infinite scroll,
// a jump), and the older page landed in front of the reloaded one: what
// was read this morning and every unread message up to the newest page
// were gone, and paging back went on before them. A message of the old
// window that an event changed during the reload was put back above the
// gap. Synthetic data on internal/fakeslack.

import (
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestDroppedModelPagesBackAfterItsReload(t *testing.T) {
	app, fs, _ := shotsApp(t)
	h := coreHost{app: app, th: theme.New(false)}
	const ch = "C0GAPPAGE1"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "busy", IsChannel: true, IsMember: true, NumMembers: 40})
	// the server's history: what Karim read, 70 he hasn't (more than a
	// page past it), the newest 10
	post := func(text string) slack.Message {
		ts, _ := fs.PostQuiet(ch, "UANA00001", text)
		return slack.Message{Type: "message", TS: ts, User: "UANA00001", Text: text}
	}
	var read, unread, newest []slack.Message
	for i := 0; i < 40; i++ {
		read = append(read, post(fmt.Sprintf("read %d", i)))
	}
	for i := 0; i < 70; i++ {
		unread = append(unread, post(fmt.Sprintf("unread %d", i)))
	}
	for i := 0; i < 10; i++ {
		newest = append(newest, post(fmt.Sprintf("newest %d", i)))
	}
	app.S.SetLatest(ch, read, true) // this morning's window, with more before it
	v := NewChannelView(h, ch)
	frame := frameFn(v)
	until(t, 5*time.Second, "what Karim read", frame, func() bool { return itemHeight(v.s, read[39].TS) > 0 })

	// 40 other conversations: the hub lets the list's model go
	base := time.Now().Add(-time.Hour).Unix()
	for i := 0; i < maxModels; i++ {
		oc := fmt.Sprintf("C0GAPPOT%02d", i)
		ts := fmt.Sprintf("%d.000100", base+int64(i))
		app.S.PutConv(slack.Conversation{ID: oc, Name: fmt.Sprintf("other-%d", i), IsChannel: true, IsMember: true, NumMembers: 5})
		app.S.SetLatest(oc, []slack.Message{{Type: "message", TS: ts, User: "UANA00001", Text: "hello " + oc}}, false)
		ov := NewChannelView(h, oc)
		until(t, 5*time.Second, oc+" drawn", frameFn(ov), func() bool { return itemHeight(ov.s, ts) > 0 })
	}
	hb := v.s.hb
	hb.mu.Lock()
	held := hb.chans[ch] == v.s.m
	hb.mu.Unlock()
	if held {
		t.Fatal("the hub still keeps the list's model after 40 other conversations")
	}

	// core's cache resets the conversation whole (its 3000-message budget:
	// a busy channel's newer messages push it out)
	const filler = "C0GAPFILL1"
	app.S.PutConv(slack.Conversation{ID: filler, Name: "firehose", IsChannel: true, IsMember: true, NumMembers: 900})
	future := time.Now().Add(3 * time.Hour).Unix()
	var flood []slack.Message
	for i := 0; i < store.EvictMaximum; i++ {
		flood = append(flood, slack.Message{Type: "message", TS: fmt.Sprintf("%d.%06d", future+int64(i/1000), i%1000), User: "UBEN00001", Text: "flood"})
	}
	app.S.SetLatest(filler, flood, false)
	app.S.EvictMessages(store.EvictInput{Now: time.Now(), Accessed: map[string]time.Time{}, Keep: map[string]bool{}, Pending: map[string]bool{},
		Conv: func(string) (bool, bool) { return true, true }, WholeOK: func(string) bool { return true }, LastRead: func(string) string { return "" }})
	if app.S.Timeline(ch) != nil {
		t.Fatal("core's cache kept the conversation: the reload wouldn't go to the server")
	}

	// the server holds the reload, and any older page, until the test lets them go
	var once1, once2 sync.Once
	newestGate, olderGate := make(chan struct{}), make(chan struct{})
	openNewest := func() { once1.Do(func() { close(newestGate) }) }
	openOlder := func() { once2.Do(func() { close(olderGate) }) }
	defer openNewest()
	defer openOlder()
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "conversations.history" && form.Get("channel") == ch {
			if form.Get("latest") == "" {
				<-newestGate
			} else {
				<-olderGate
			}
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })

	// Karim opens it again and scrolls up while the reload is out; a
	// message of the old window changes meanwhile
	frame()
	for dl := time.Now().Add(1300 * time.Millisecond); time.Now().Before(dl); {
		v.s.list.Position = layout.Position{BeforeEnd: true}
		frame()
		time.Sleep(10 * time.Millisecond)
	}
	editMsg(app, ch, read[2].TS, "UANA00001", "read 2, edited")
	for i := 0; i < 10; i++ {
		frame()
		time.Sleep(5 * time.Millisecond)
	}
	openNewest()
	until(t, 5*time.Second, "the reloaded newest page", frame, func() bool {
		m := v.s.m
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.find(newest[9].TS) >= 0 && m.find(unread[69].TS) >= 0
	})
	openOlder() // an older page asked from the old window lands now, after the reload
	for dl := time.Now().Add(600 * time.Millisecond); time.Now().Before(dl); {
		frame()
		time.Sleep(10 * time.Millisecond)
	}

	// what the list holds now has no gap: nothing of what was read
	// without every unread message after it
	if r, u := listHas(v, read), listHas(v, unread); r > 0 && u < len(unread) {
		t.Errorf("the list draws %d of the messages Karim read above the reloaded page, and %d of the %d unread ones between: a gap", r, u, len(unread))
	}
	// scrolled up, every message comes, in order, and each draws
	all := append(append(append([]slack.Message(nil), read...), unread...), newest...)
	scrollUp(v, frame, all, 8*time.Second)
	drawnInOrder(t, v, frame, all)
}

// itemIndex is ts's item in the list (-1: none).
func itemIndex(v *ChannelView, ts string) int {
	for i, it := range v.s.items {
		if it.key == ts {
			return i
		}
	}
	return -1
}

// listHas is how many of msgs the list holds.
func listHas(v *ChannelView, msgs []slack.Message) int {
	n := 0
	for _, x := range msgs {
		if itemIndex(v, x.TS) >= 0 {
			n++
		}
	}
	return n
}

// scrollUp draws frames scrolled to the list's top, where it pages back,
// until it holds every one of msgs or d passes.
func scrollUp(v *ChannelView, frame func(), msgs []slack.Message, d time.Duration) {
	for dl := time.Now().Add(d); time.Now().Before(dl) && listHas(v, msgs) < len(msgs); {
		v.s.list.Position = layout.Position{BeforeEnd: true}
		frame()
		time.Sleep(20 * time.Millisecond)
	}
}

// drawnInOrder fails t unless the list holds msgs in their order and
// draws each scrolled to it.
func drawnInOrder(t *testing.T, v *ChannelView, frame func(), msgs []slack.Message) {
	t.Helper()
	last := -1
	for _, x := range msgs {
		i := itemIndex(v, x.TS)
		if i < 0 {
			t.Errorf("%q isn't in the list scrolled up to it (%d of %d messages there)", x.Text, listHas(v, msgs), len(msgs))
			return
		}
		if i <= last {
			t.Errorf("%q is drawn out of order", x.Text)
			return
		}
		last = i
		v.s.list.Position = layout.Position{BeforeEnd: true, First: i}
		frame()
		if itemHeight(v.s, x.TS) <= 0 {
			t.Errorf("%q isn't drawn scrolled to it", x.Text)
			return
		}
	}
}
