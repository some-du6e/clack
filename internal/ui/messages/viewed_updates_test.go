package messages

// A row that draws the plugins' view of its message (a messages-slice
// patch: Message Logger, ShowRealUser) takes every update to it, not only
// the fields a plugin's change shows in. A broadcast's "View newer
// replies" is worked out by core at each conversion, from its thread's
// newest reply, with no change to the plugins' JSON; a row that kept its
// view whenever text, blocks and a few other fields matched never showed
// it, and a row an update had made viewed (its first read adopted JS's copy
// from before the update) kept the revision before. Synthetic data on
// internal/fakeslack.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const viewedChan = "C0VIEWED01"

// viewedPatchJS adds a context line under every message of viewedChan in
// the messages slice, so every row there draws a view.
var viewedPatchJS = pluginJS(`
class LogEvery extends TautPlugin {
  static id = 'LogEvery'
  static pluginName = 'Log every message (test)'
  static description = 'test: a log line under every message of one channel'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const log = (ts, m) => (m ? { ...m, blocks: [...(m.blocks || []), { type: 'context', block_id: 'log', elements: [{ type: 'mrkdwn', text: '` + voteLog + `' }] }] } : m)
    const { patchSlice, mapEntries } = this.api.redux
    patchSlice('messages', (ch, bucket) => (ch === '` + viewedChan + `' && bucket && typeof bucket === 'object' ? mapEntries(bucket, log) : bucket))
  }
}
return LogEvery`)

func TestViewedRowTakesUpdates(t *testing.T) {
	app, _, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: viewedChan, Name: "lunch", IsChannel: true, IsMember: true, NumMembers: 12})
	base := time.Now().Add(-time.Hour).Unix()
	rootTS, r1, bTS := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000200", base+60), fmt.Sprintf("%d.000300", base+120)
	r2 := fmt.Sprintf("%d.000400", base+180)
	root := slack.Message{Type: "message", TS: rootTS, User: "UANA00001", Text: "Lunch at noon?", Blocks: shotRich(shotSection("Lunch at noon?"))}
	root.ThreadTS, root.ReplyCount, root.ReplyUsers, root.LatestReply = rootTS, 2, []string{"UBEN00001"}, bTS
	reply := slack.Message{Type: "message", TS: r1, ThreadTS: rootTS, User: "UBEN00001", Text: "sure", Blocks: shotRich(shotSection("sure"))}
	// a reply also sent to the channel, the thread's newest
	bcast := slack.Message{Type: "message", Subtype: "thread_broadcast", TS: bTS, ThreadTS: rootTS, User: "UBEN00001",
		Text: "booked a table", Blocks: shotRich(shotSection("booked a table"))}
	rootCopy := root
	bcast.Root = &rootCopy
	app.S.SetLatest(viewedChan, []slack.Message{root, bcast}, false)
	app.S.SetThread(viewedChan, rootTS, []slack.Message{root, reply, bcast})

	patchedEnv(t, app, viewedPatchJS)
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	h := coreHost{app: app, th: theme.New(false)}
	cv := NewChannelView(h, viewedChan)
	cv.Preview = true
	frame := frameFn(cv)
	row := func(ts string) *row { return cv.s.rows[ts] }
	viewed := func(ts string) bool {
		r := row(ts)
		return r != nil && r.viewed && strings.Contains(rowText(r), voteLog)
	}
	until(t, 10*time.Second, "both rows drawing the plugins' view", frame, func() bool { return viewed(rootTS) && viewed(bTS) })
	if r := row(bTS); r.m.NewerReplies {
		t.Fatal("the broadcast is the thread's newest reply and shows View newer replies")
	}
	h0 := itemHeight(cv.s, bTS)
	inject := func(typ, subtype string, body map[string]any) { injectEvent(app, viewedChan, typ, subtype, body) }
	// someone replies in the thread: the root's count and newest reply
	// move, and the broadcast is no longer the newest (View newer replies)
	inject("message", "", map[string]any{"user": "UCAR00001", "text": "count me in", "ts": r2, "thread_ts": rootTS})
	parent := map[string]any{"type": "message", "user": "UANA00001", "text": root.Text, "ts": rootTS, "thread_ts": rootTS,
		"reply_count": 3, "reply_users_count": 2, "latest_reply": r2, "reply_users": []string{"UBEN00001", "UCAR00001"}}
	inject("message", "message_replied", map[string]any{"message": parent, "ts": r2, "hidden": true})
	until(t, 10*time.Second, "the root's new reply count and the broadcast's View newer replies", frame, func() bool {
		rr, rb := row(rootTS), row(bTS)
		return rr != nil && rb != nil && rr.m.ReplyCount == 3 && rr.m.LatestReply == r2 && rb.m.NewerReplies
	})
	frame()
	if !viewed(rootTS) || !viewed(bTS) {
		t.Error("after the reply a row no longer draws the plugins' view")
	}
	if hb := itemHeight(cv.s, bTS); hb <= h0 {
		t.Errorf("the broadcast is %dpx tall with View newer replies, %dpx without", hb, h0)
	}
	// a reaction and a pin
	inject("reaction_added", "", map[string]any{"user": "UCAR00001", "reaction": "eyes", "item": map[string]any{"type": "message", "channel": viewedChan, "ts": bTS}})
	until(t, 10*time.Second, "the broadcast's reaction", frame, func() bool {
		r := row(bTS)
		return r != nil && len(r.m.Reactions) == 1 && r.m.Reactions[0].Name == "eyes"
	})
	inject("pin_added", "", map[string]any{"user": "UCAR00001", "channel_id": viewedChan,
		"item": map[string]any{"type": "message", "channel": viewedChan, "created_by": "UCAR00001", "created": time.Now().Unix(),
			"message": map[string]any{"type": "message", "user": "UANA00001", "text": root.Text, "ts": rootTS}}})
	until(t, 10*time.Second, "the root's pin", frame, func() bool {
		r := row(rootTS)
		return r != nil && r.m.Pinned
	})
	frame()
	if !viewed(rootTS) || !viewed(bTS) {
		t.Error("after the reaction and the pin a row no longer draws the plugins' view")
	}
}
