package views

// Karim, 2026-11-21 (α.113, Windows): "fix or die", over an Activity list
// whose newest row came last. A reply had just come into a thread they
// follow ("Just now"), and the thread's row kept its old place under three
// rows from minutes before, which weren't in order either (8, 7 and 8
// minutes). The server sends a thread's bundle (thread_v2, one key per
// thread) again with each new reply's ts as its feed_ts; Slack's reducer
// (tsND upsertActivityRtmItem, getUpsertedActivityRtmItems) takes the old
// copy out and puts the new one back at its feedTs, and Clack replaced it
// where it was. Here, over the socket: two threads you started, a mention
// and a reaction a minute apart, then a reply in each thread. The rows'
// order and labels (time, sender, text) after each event, after
// activity_invalidated's reload and after a restart on the saved cache.
// Synthetic data on internal/fakeslack.

import (
	"context"
	"fmt"
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// actOrderBoot boots a core on fs's views fixtures with its store in dir,
// realtime on and connected (vkFake's, on a dir of the test's: a second
// boot on it is a restart on the first one's saved cache).
func actOrderBoot(t *testing.T, fs *fakeslack.Server, dir string) *core.App {
	t.Helper()
	app := core.New(slack.New(fs.Creds()), store.New(dir))
	t.Cleanup(app.Shutdown)
	evs, stop := app.Subscribe()
	defer stop()
	if err := app.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	app.SetExperimentGroup("activity_inbox", "on") // Karim's inbox (activityUpdatedV2)
	app.S.PutUsers([]slack.User{
		{ID: "U0FAKE00001", Name: "ada", RealName: "Ada Example"},
		{ID: "U0FAKE00002", Name: "grace", RealName: "Grace Example"},
	})
	app.S.PutConv(slack.Conversation{ID: fakeslack.ViewsChannel, Name: "views-fixtures", IsChannel: true, IsMember: true, NumMembers: 3})
	app.SetRealtime(true)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev := <-evs:
			if ev.Type == "connection" && ev.Connected {
				return app
			}
		case <-deadline:
			t.Fatal("realtime didn't connect")
		}
	}
}

func actOrderView(app *core.App) (*Activity, *vaEnv) {
	h := &vaHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	v := NewActivity(h)
	e := &vaEnv{size: image.Pt(1200, 1400)}
	e.draw = func(gtx layout.Context) {
		v.Layout(gtx)
		for _, o := range h.open() {
			o.Layout(gtx)
		}
	}
	return v, e
}

// actOrderRow is a row the list should draw: its entry, the feed_ts its
// time is from, and for a thread the reply's author, the sender's "+N"
// and a piece of the reply's text.
type actOrderRow struct {
	key, feedTS string
	author      string
	others      int
	text        string
	thread      bool
}

// actOrderLabel is the time a row with feed_ts ts reads at now (0Rg1
// rK, activity_time.go).
func actOrderLabel(now time.Time, ts string) string {
	at := tsTime(ts)
	if age := now.Sub(at); age < time.Hour {
		s, _ := actRel(age)
		return s
	}
	return actWhen(now, at, false)
}

// checkActOrder checks the first rows v draws against want: the order,
// each row's time from its feed_ts and a thread's sender and text, and
// the times as a frame draws them, top to bottom.
func checkActOrder(t *testing.T, phase string, v *Activity, e *vaEnv, want []actOrderRow) {
	t.Helper()
	// the rows described again after what came in (rebuild), drawn
	for range 3 {
		e.frame()
	}
	var keys []string
	for _, w := range want {
		keys = append(keys, w.key)
	}
	shown := actShownKeys(v)
	if len(shown) < len(want) || !slices.Equal(shown[:len(want)], keys) {
		t.Errorf("%s: the list draws %v first, want %v (newest feed_ts first, as Slack's um/L Y sorts)", phase, shown[:min(len(shown), len(want)+2)], keys)
	}
	now := time.Now()
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: now, Constraints: layout.Exact(e.size)}
	var labels []string
	for _, w := range want {
		r := v.rows[w.key]
		if r == nil || !r.built {
			t.Errorf("%s: no row described for %s", phase, w.key)
			continue
		}
		label := actOrderLabel(now, w.feedTS)
		labels = append(labels, label)
		if got := v.rowWhen(gtx, &r.d, false); got != label {
			t.Errorf("%s: %s reads %q, want %q (its feed_ts %s)", phase, w.key, got, label, w.feedTS)
		}
		if !w.thread {
			continue
		}
		name := v.userName(w.author)
		if w.others > 0 {
			name += fmt.Sprintf(" +%d", w.others)
		}
		if r.d.name != name {
			t.Errorf("%s: %s's sender %q, want %q (the latest reply's author and the others in the thread)", phase, w.key, r.d.name, name)
		}
		if !strings.Contains(r.d.prev, w.text) {
			t.Errorf("%s: %s shows %q, want the latest reply %q", phase, w.key, r.d.prev, w.text)
		}
	}
	// the times as drawn, in the rows' order (a row laid out twice in a
	// frame draws its time twice in a row)
	want2 := map[string]bool{}
	for _, l := range labels {
		want2[l] = true
	}
	var drawn []string
	for _, s := range drawnStrings(e) {
		if want2[s] && len(drawn) < len(labels) && (len(drawn) == 0 || drawn[len(drawn)-1] != s) {
			drawn = append(drawn, s)
		}
	}
	if !slices.Equal(drawn, labels) {
		t.Errorf("%s: the frame draws the times %v, want %v", phase, drawn, labels)
	}
}

func TestActivityOrderLikeSlack(t *testing.T) {
	sz := fakeslack.Small
	sz.Views = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	ch := fakeslack.ViewsChannel
	self, ada, grace := fakeslack.SelfID, "U0FAKE00001", "U0FAKE00002"
	start := time.Now()
	at := func(ago time.Duration) string { return fmt.Sprintf("%d.000100", start.Add(-ago).Unix()) }

	// two threads you started, each with a reply you've read: the
	// bundles' feed_ts are those replies
	rootA, oldA := at(2*time.Hour), at(20*time.Minute)
	fs.PostAtQuiet(ch, rootA, self, "a synthetic plan for the release")
	fs.ReplyAtQuiet(ch, rootA, oldA, ada, "a synthetic first look at the plan")
	rootB, oldB := at(6*time.Hour), at(3*time.Hour)
	fs.PostAtQuiet(ch, rootB, self, "a synthetic pairing session")
	fs.ReplyAtQuiet(ch, rootB, at(5*time.Hour), ada, "a synthetic agenda")
	fs.ReplyAtQuiet(ch, rootB, oldB, grace, "a synthetic summary of the session")
	fs.UpsertActivity(fakeslack.ActivityThreadEntry(ch, rootA, oldA, "", 0))
	fs.UpsertActivity(fakeslack.ActivityThreadEntry(ch, rootB, oldB, "", 0))
	liked := at(30 * time.Minute) // your message the reaction is on
	fs.PostAtQuiet(ch, liked, self, "a synthetic note people liked")
	keyA, keyB := fakeslack.ActivityThreadKey(ch, rootA), fakeslack.ActivityThreadKey(ch, rootB)

	dir := t.TempDir()
	app := actOrderBoot(t, fs, dir)
	v, e := actOrderView(app)
	listed := func(key, ts string) func() bool {
		return func() bool {
			for _, it := range actPage(v).Items {
				if actKey(&it) == key {
					return ts == "" || it.FeedTS == ts
				}
			}
			return false
		}
	}
	e.until(t, "the feed", func() bool { return listed(keyA, oldA)() && listed(keyB, oldB)() && !v.data.busy() })
	rowA := actOrderRow{key: keyA, feedTS: oldA, author: ada, text: "a synthetic first look", thread: true}
	rowB := actOrderRow{key: keyB, feedTS: oldB, author: grace, others: 1, text: "a synthetic summary", thread: true}
	checkActOrder(t, "the feed", v, e, []actOrderRow{rowA, rowB})

	// a mention, then a minute later a reaction (Karim's rows 1 and 3)
	tsM := at(8*time.Minute + 50*time.Second)
	keyM := fs.PushMentionAtLive(ch, tsM, ada, "<@"+self+"> can you check the synthetic order?")
	e.until(t, "the mention", listed(keyM, tsM))
	rowM := actOrderRow{key: keyM, feedTS: tsM}
	checkActOrder(t, "a mention", v, e, []actOrderRow{rowM, rowA, rowB})
	tsR := at(7*time.Minute + 50*time.Second)
	keyR := fs.PushReactionAtLive(ch, liked, self, tsR, grace, "tada")
	e.until(t, "the reaction", listed(keyR, tsR))
	rowR := actOrderRow{key: keyR, feedTS: tsR}
	checkActOrder(t, "a reaction a minute later", v, e, []actOrderRow{rowR, rowM, rowA, rowB})

	// a reply in the first thread a minute after the reaction (Karim's
	// row 2, "clod +1"): its bundle comes again, the reply its feed_ts
	newA := at(6*time.Minute + 50*time.Second)
	fs.PushReplyAtLive(ch, rootA, newA, grace, "a synthetic answer, with the numbers", newA, 1)
	e.until(t, "the first thread's new reply", listed(keyA, newA))
	rowA = actOrderRow{key: keyA, feedTS: newA, author: grace, others: 1, text: "a synthetic answer, with the numbers", thread: true}
	checkActOrder(t, "a reply in the first thread", v, e, []actOrderRow{rowA, rowR, rowM, rowB})

	// a reply just now in the second thread, its last one three hours
	// old (Karim's row 4, "Just now")
	newB := at(5 * time.Second)
	fs.PushReplyAtLive(ch, rootB, newB, ada, "Deal. Three synthetic things, in order:", newB, 1)
	e.until(t, "the second thread's new reply", listed(keyB, newB))
	rowB = actOrderRow{key: keyB, feedTS: newB, author: ada, others: 1, text: "Deal. Three synthetic things", thread: true}
	all := []actOrderRow{rowB, rowA, rowR, rowM}
	checkActOrder(t, "a reply just now in the second thread", v, e, all)

	// activity_invalidated: the feed read again (activityInvalidated)
	feeds := fs.CallCount("activity.feed")
	fs.Push(map[string]any{"type": "activity", "subtype": "activity_invalidated"})
	e.until(t, "the reload", func() bool { return fs.CallCount("activity.feed") > feeds && !v.data.busy() && !v.wasBusy })
	checkActOrder(t, "after activity_invalidated's reload", v, e, all)

	// quit and start again on the saved cache: Activity reads the feed
	app.StopTraffic()
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	app.Shutdown()
	app2 := actOrderBoot(t, fs, dir)
	v2, e2 := actOrderView(app2)
	v = v2
	e2.until(t, "the feed after the restart", func() bool { return listed(keyB, newB)() && listed(keyM, tsM)() && !v2.data.busy() })
	checkActOrder(t, "after a restart on the saved cache", v2, e2, all)
}
