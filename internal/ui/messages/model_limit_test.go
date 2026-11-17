package messages

// The hub keeps 40 conversations' models (maxModels): the ones core's
// events, polls and resyncs change. Each list draws its own. Opening a
// conversation past 40 dropped the model it had just made (its last use
// was set after the eviction ran, so the new one read as the oldest):
// every channel and thread opened after the 40th took no new message,
// edit, reaction or typing until its own refresh 30 s on (the perf
// harness's bench channel, opened after 40 others, never took a live
// event). And a list whose model the eviction dropped while it was off
// screen took none when it showed again. Synthetic data.

import (
	"fmt"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestListsPastTheModelLimitTakeLiveMessages(t *testing.T) {
	base := time.Now().Add(-time.Hour).Unix()
	// drawn reports whether the list draws a new message within 2 s of
	// its event (no refresh falls due meanwhile)
	drawn := func(t *testing.T, s *listState, frame func(), ts string) bool {
		t.Helper()
		for dl := time.Now().Add(2 * time.Second); time.Now().Before(dl); {
			frame()
			if itemHeight(s, ts) > 0 {
				return true
			}
			time.Sleep(15 * time.Millisecond)
		}
		return false
	}
	newTS := func() string {
		now := time.Now()
		return fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)
	}
	t.Run("channels", func(t *testing.T) {
		app, _, _ := shotsApp(t)
		h := coreHost{app: app, th: theme.New(false)}
		type opened struct {
			v     *ChannelView
			ch    string
			frame func()
		}
		open := func(i int) opened {
			ch := fmt.Sprintf("C0LIMIT%03d", i)
			ts := fmt.Sprintf("%d.000100", base+int64(i))
			app.S.PutConv(slack.Conversation{ID: ch, Name: fmt.Sprintf("limit-%d", i), IsChannel: true, IsMember: true, NumMembers: 5})
			app.S.SetLatest(ch, []slack.Message{{Type: "message", TS: ts, User: "UANA00001", Text: "hello " + ch}}, false)
			v := NewChannelView(h, ch)
			o := opened{v: v, ch: ch, frame: frameFn(v)}
			until(t, 5*time.Second, ch+" drawn", o.frame, func() bool { return itemHeight(v.s, ts) > 0 })
			return o
		}
		post := func(o opened, what string) {
			t.Helper()
			ts := newTS()
			injectEvent(app, o.ch, "message", "", map[string]any{"user": "UBEN00001", "text": "new in " + o.ch, "ts": ts})
			if !drawn(t, o.v.s, o.frame, ts) {
				t.Errorf("%s: a new message in %s not drawn in 2 s (%d messages in its list)", what, o.ch, len(o.v.s.snap.msgs))
			}
		}
		first := open(0)
		for i := 1; i < maxModels; i++ {
			open(i)
		}
		post(open(maxModels), "the conversation opened past the limit")
		// the eviction took the first one's model (drawn longest ago)
		post(first, "the first conversation, shown again")
	})
	t.Run("threads", func(t *testing.T) {
		app, _, _ := shotsApp(t)
		h := coreHost{app: app, th: theme.New(false)}
		const ch = "C0LIMTHRD"
		app.S.PutConv(slack.Conversation{ID: ch, Name: "limit-threads", IsChannel: true, IsMember: true, NumMembers: 5})
		type opened struct {
			v     *ThreadView
			root  string
			frame func()
		}
		var roots []slack.Message
		for i := 0; i <= maxModels; i++ {
			ts := fmt.Sprintf("%d.000100", base+int64(i))
			roots = append(roots, slack.Message{Type: "message", TS: ts, ThreadTS: ts, User: "UANA00001", Text: fmt.Sprintf("thread %d", i), ReplyCount: 0})
		}
		app.S.SetLatest(ch, roots, false)
		open := func(i int) opened {
			root := roots[i]
			app.S.SetThread(ch, root.TS, []slack.Message{root})
			v := NewThreadView(h, ch, root.TS)
			o := opened{v: v, root: root.TS, frame: frameFn(v)}
			until(t, 5*time.Second, "thread "+root.TS+" drawn", o.frame, func() bool { return itemHeight(v.s, root.TS) > 0 })
			return o
		}
		reply := func(o opened, what string) {
			t.Helper()
			ts := newTS()
			injectEvent(app, ch, "message", "", map[string]any{"user": "UBEN00001", "text": "a reply", "ts": ts, "thread_ts": o.root})
			if !drawn(t, o.v.s, o.frame, ts) {
				t.Errorf("%s: a new reply in %s not drawn in 2 s (%d messages in its list)", what, o.root, len(o.v.s.snap.msgs))
			}
		}
		first := open(0)
		for i := 1; i < maxModels; i++ {
			open(i)
		}
		reply(open(maxModels), "the thread opened past the limit")
		reply(first, "the first thread, shown again")
	})
}
