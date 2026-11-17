package views

// Activity's rows draw their messages as they are now (activity_msgs.go).
// Slack's rows read the messages store as they render
// (getMessageFromActivityItem), so an app's chat.update, an edit, a
// reaction or a deletion shows on an open Activity page in the commit it
// lands in, beside the channel list and the thread pane, with no feed
// reload. Karim's vote message (Meta Anonymous Vote, α.102): its counts
// in Activity "go up and down like crazy". Activity kept the copy the
// feed was hydrated with until the feed loaded again, and a row's body
// layout was cached on the text's length.
//
// On internal/fakeslack with a made-up channel, people and messages: its
// hook answers activity.feed, messages.list and conversations.replies from
// the fake's state (the vote's revision, the mention's text), and the
// app's chat.update reaches the client as the socket's message_changed
// (core.Inject). The vote's channel is one nobody opened: its messages
// come by id (messages.list), as an Activity entry's usually do.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	lmChan        = "C0LIVE0001"
	lmBot         = "B0LIVE0001"
	lmApp         = "A0LIVE0001"
	lmRoot        = "1762790000.000100" // Ada's proposal: the vote's thread
	lmVote        = "1762790100.000100" // the vote app's message, the thread's latest reply (or first unread: lmOpts.latest)
	lmLatest      = "1762790150.000100" // a reply after the vote (lmOpts.latest): the bundle's latest
	lmMention     = "1762790200.000100" // Grace's mention of the viewer
	lmMention2    = "1762790300.000100" // a mention pushed while the page is off screen
	lmBusy        = "C0LIVE0BSY"        // a channel the socket is busy in
	lmThreadKey   = "lm-thread"
	lmMentionKey  = "lm-mention"
	lmMention2Key = "lm-mention-2"
)

// lmPatches are the Activity components a pass-through plugin wraps (the
// shape Show Real User's ActivityItem patch has: props.msg handed to
// Original).
var lmPatches = []string{"ActivityItem", "ActivityThread", "ActivityRowContentMultiLine", "ActivityRowContentDetailed"}

// lmVoteText is revision n's counts, the same length at every revision
// (the row keeps its height).
func lmVoteText(n int) string { return fmt.Sprintf("Upvotes: %d\nDownvotes: %d", n, 20+n) }

// lmVoteMsg is the vote app's message at revision n: the counts in a
// section, then an upvote and a downvote button, block_ids new at every
// update; an app's chat.update marks nothing edited.
func lmVoteMsg(n int) slack.Message {
	text := lmVoteText(n)
	button := func(id, label string) map[string]any {
		return map[string]any{"type": "button", "action_id": id, "value": id, "text": map[string]any{"type": "plain_text", "text": label}}
	}
	blocks, _ := json.Marshal([]any{
		map[string]any{"type": "section", "block_id": fmt.Sprintf("s%d", n), "text": map[string]any{"type": "mrkdwn", "text": text}},
		map[string]any{"type": "actions", "block_id": fmt.Sprintf("u%d", n), "elements": []any{button("up", "Upvote")}},
		map[string]any{"type": "actions", "block_id": fmt.Sprintf("d%d", n), "elements": []any{button("down", "Downvote")}},
	})
	return slack.Message{Type: "message", Subtype: "bot_message", TS: lmVote, ThreadTS: lmRoot, BotID: lmBot, Username: "Vote Bot",
		Text: text, Blocks: blocks, BotProfile: &slack.BotProfile{ID: lmBot, Name: "Vote Bot", AppID: lmApp}}
}

func lmRootMsg(latest bool) slack.Message {
	m := slack.Message{Type: "message", TS: lmRoot, User: "U0FAKE00001", Text: "Proposal: move the synthetic standup to 10am",
		ThreadTS: lmRoot, ReplyCount: 1, ReplyUsersN: 1, LatestReply: lmVote}
	if latest {
		m.ReplyCount, m.ReplyUsersN, m.LatestReply = 2, 2, lmLatest
	}
	return m
}

// lmFake is the server's side: the vote's revision, the mentions' texts
// and the deleted messages, which activity.feed, messages.list,
// conversations.replies and subscriptions.thread.getView answer from.
type lmFake struct {
	mu       sync.Mutex
	rev      int
	mention  string
	mention2 string
	gone     map[string]bool
	latest   bool   // the thread has lmLatest after the vote
	listGone bool   // the feed still lists the mention its message went from
	mentions string // the mention entry's type (at_user)
}

func (f *lmFake) msgLocked(ts string) (slack.Message, bool) {
	if f.gone[ts] {
		return slack.Message{}, false
	}
	switch ts {
	case lmRoot:
		return lmRootMsg(f.latest), true
	case lmVote:
		return lmVoteMsg(f.rev), true
	case lmLatest:
		if f.latest {
			return slack.Message{Type: "message", TS: lmLatest, ThreadTS: lmRoot, User: "U0FAKE00002", Text: "a synthetic reply after the vote"}, true
		}
	case lmMention:
		return slack.Message{Type: "message", TS: lmMention, User: "U0FAKE00002", Text: f.mention}, true
	case lmMention2:
		if f.mention2 != "" {
			return slack.Message{Type: "message", TS: lmMention2, User: "U0FAKE00002", Text: f.mention2}, true
		}
	}
	return slack.Message{}, false
}

func (f *lmFake) threadLocked() []slack.Message {
	msgs := []slack.Message{lmRootMsg(f.latest)}
	for _, ts := range []string{lmVote, lmLatest} {
		if m, ok := f.msgLocked(ts); ok {
			msgs = append(msgs, m)
		}
	}
	return msgs
}

func (f *lmFake) hook(method string, form url.Values) (any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch method {
	case "activity.feed":
		items := []any{}
		if !f.gone[lmMention] || f.listGone { // a read mention of the viewer
			items = append(items, map[string]any{"key": lmMentionKey, "feed_ts": lmMention, "is_unread": false, "item": map[string]any{
				"type": firstNonEmpty(f.mentions, "at_user"), "message": map[string]any{"ts": lmMention, "channel": lmChan, "author_user_id": "U0FAKE00002"}}})
		}
		if !f.gone[lmVote] { // the thread the vote is the latest (and first unread) reply of
			entry := map[string]any{"channel_id": lmChan, "thread_ts": lmRoot, "latest_ts": lmVote, "min_unread_ts": lmVote, "unread_msg_count": 1}
			if f.latest {
				entry["latest_ts"], entry["unread_msg_count"] = lmLatest, 2
			}
			items = append(items, map[string]any{"key": lmThreadKey, "feed_ts": entry["latest_ts"], "is_unread": true, "item": map[string]any{
				"type": "thread_v2", "bundle_info": map[string]any{"payload": map[string]any{"thread_entry": entry}}}})
		}
		return map[string]any{"ok": true, "items": items, "response_metadata": map[string]any{"next_cursor": ""}}, true
	case "messages.list":
		var want []struct {
			Channel    string   `json:"channel"`
			Timestamps []string `json:"timestamps"`
		}
		if json.Unmarshal([]byte(form.Get("message_ids")), &want) != nil {
			return nil, false
		}
		out := map[string][]slack.Message{}
		for _, w := range want {
			if w.Channel != lmChan {
				continue
			}
			for _, ts := range w.Timestamps {
				if m, ok := f.msgLocked(ts); ok {
					out[lmChan] = append(out[lmChan], m)
				}
			}
		}
		return map[string]any{"ok": true, "messages": out}, true
	case "conversations.replies":
		if form.Get("channel") != lmChan || form.Get("ts") != lmRoot {
			return nil, false
		}
		return map[string]any{"ok": true, "messages": f.threadLocked(), "has_more": false}, true
	case "subscriptions.thread.getView":
		msgs := f.threadLocked()
		root := msgs[0]
		root.Channel = lmChan
		return map[string]any{"ok": true, "threads": []any{map[string]any{"root_msg": root, "latest_replies": msgs[1:], "unread_replies": []any{}}},
			"has_more": false, "total_unread_replies": 0, "threads_state": map[string]any{"has_unreads": false, "mention_count": 0}}, true
	}
	return nil, false
}

// lmHost is the views Host, with the message lists' copies when the
// thread pane is open beside the page (the ui root's LiveMessages).
type lmHost struct {
	*vaHost
	lists bool
}

func (h *lmHost) LiveMessage(ch, ts, threadTS string, since time.Time, have uint64) (core.Message, uint64, bool) {
	if !h.lists {
		return core.Message{}, 0, false
	}
	return messages.Live(h.app, ch, ts, threadTS, since, have)
}

// lmOpts are an env's differences from the plain page.
type lmOpts struct {
	pane     bool     // the thread pane beside the page
	patch    []string // the components a pass-through plugin wraps
	lockstep bool     // JS answers every render before the next frame
	texts    bool     // a plugin's text patch over the messages slice (patchMessageText, as Message Logger's kind)
	latest   bool     // a reply after the vote: the bundle's latest_ts, the vote its first unread
	setup    func(e *lmEnv)
}

type lmEnv struct {
	t   *testing.T
	fs  *fakeslack.Server
	app *core.App
	f   *lmFake
	h   *lmHost
	v   *Activity
	tv  *messages.ThreadView // the thread pane beside the page (nil: none)
	rt  *jsrt.Runtime        // the plugins' JS, when any
	e   *vaEnv
}

// newLmEnv is the Activity page on the fixture's feed, as o says.
func newLmEnv(t *testing.T, o lmOpts) *lmEnv {
	t.Helper()
	app, fs := vaFake(t)
	f := &lmFake{rev: 1, mention: "<@" + fakeslack.SelfID + "> please read the synthetic standup notes", gone: map[string]bool{}, latest: o.latest}
	fs.SetHook(f.hook)
	app.S.PutConv(slack.Conversation{ID: lmChan, Name: "live-votes", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.PutConv(slack.Conversation{ID: lmBusy, Name: "live-busy", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.PutBot(lmBot, store.BotInfo{Name: "Vote Bot"})
	sh := &slickHost{app: app, th: theme.New(false)}
	env := &lmEnv{t: t, fs: fs, app: app, f: f, e: &vaEnv{size: image.Pt(1400, 900)}}
	var step func()
	if o.patch != nil || o.texts {
		var tt *taut.Taut
		tt, env.rt, step = lmPatch(t, app, o.patch, o.texts)
		sh.tt = tt
		if !o.lockstep {
			step = nil
		}
	}
	env.h = &lmHost{vaHost: &vaHost{slickHost: sh}, lists: o.pane}
	if o.setup != nil {
		o.setup(env)
	}
	env.v = NewActivity(env.h)
	if o.pane {
		env.tv = messages.NewThreadView(env.h, lmChan, lmRoot)
	}
	env.e.draw = func(gtx layout.Context) {
		if o.patch != nil || o.texts {
			slackc.Pump() // queued JS→Go calls, as the ui root's frame runs them
		}
		g := gtx
		g.Constraints = layout.Exact(image.Pt(900, gtx.Constraints.Max.Y))
		env.v.Layout(g)
		if env.tv != nil {
			st := op.Offset(image.Pt(900, 0)).Push(gtx.Ops)
			g.Constraints = layout.Exact(image.Pt(500, gtx.Constraints.Max.Y))
			env.tv.Layout(g)
			st.Pop()
		}
		if step != nil {
			step()
		}
	}
	return env
}

// lmPatch runs a pass-through plugin over names (with texts, a text
// patch of the messages slice too: "Upvotes" drawn as "Ups"), as the ui
// root runs Taut, and returns its runtime, its JS and a step that waits
// for JS to have answered everything sent to it so far.
func lmPatch(t *testing.T, app *core.App, names []string, texts bool) (*taut.Taut, *jsrt.Runtime, func()) {
	t.Helper()
	ui := &slickUI{trees: map[uint64][]byte{}}
	dir := t.TempDir()
	tt, err := taut.New(taut.Options{App: app, Dir: filepath.Join(dir, "taut"), CredsPath: filepath.Join(dir, "creds.json"), UI: ui.hooks(),
		NoAutoStart: true, CacheDir: filepath.Join(dir, "cache"), Log: func(level, msg string) {
			if level == "error" {
				t.Logf("js %s: %s", level, msg)
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	slackc.SetBridge(tt.Bridge())
	slackc.Init(slackc.Env{Theme: theme.New(false), Core: app, Invalidate: func() {}})
	t.Cleanup(func() {
		slackc.Init(slackc.Env{})
		_ = tt.Close()
		slackc.Reset()
		slackc.SetBridge(nil)
		slackc.SetPatched(nil)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id, err := tt.Import(ctx, lmPassThroughJS(names, texts), "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatalf("enable %s: %v", id, err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		all := !texts || tt.SlicePatched("messages")
		for _, n := range names {
			all = all && slackc.Patched(n)
		}
		if all {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%v never patched", names)
		}
	}
	rt, err := tt.JS()
	if err != nil {
		t.Fatal(err)
	}
	return tt, rt, func() {
		if err := rt.Do(func(*jsrt.Ctx) error { return nil }); err != nil {
			t.Errorf("waiting for JS: %v", err)
		}
	}
}

// lmPassThroughJS patches names without changing anything: each hands
// props.msg on to Original. With texts it also patches the messages'
// text (patchMessageText), "Upvotes" to "Ups".
func lmPassThroughJS(names []string, texts bool) []byte {
	patches := ""
	for _, n := range names {
		patches += "    this.api.patchComponent('" + n + "', (Original) => (props) => {\n" +
			"      const msg = React.useMemo(() => props.msg, [props.msg])\n" +
			"      return React.createElement(Original, { ...props, msg })\n" +
			"    })\n"
	}
	if texts {
		patches += "    this.api.messages.patchMessageText((s) => s.replace(/Upvotes/g, 'Ups'))\n"
	}
	return []byte("(() => {\nconst TautPlugin = globalThis.TautPlugin, opt = globalThis.TautOpt;\n" +
		"class LivePassThrough extends TautPlugin {\n" +
		"  static id = 'LivePassThrough'\n  static pluginName = 'Activity pass-through (test)'\n" +
		"  static description = 'test: wraps the Activity components and changes nothing'\n" +
		"  static authors = ['clack']\n  static category = 'messages'\n  static defaultConfig = { enabled: true }\n" +
		"  start() {\n" + patches + "  }\n}\nreturn LivePassThrough\n})()")
}

// holdJS keeps the plugins' JS busy until release: no tree is drawn
// again meanwhile, so every one the page reads is from before.
func (e *lmEnv) holdJS() (release func()) {
	started, done := make(chan struct{}), make(chan struct{})
	go func() {
		_ = e.rt.Do(func(*jsrt.Ctx) error {
			close(started)
			<-done
			return nil
		})
	}()
	<-started
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

func (e *lmEnv) frame() { e.e.frame() }

// drawn is the text the Activity row of the message ts laid out in the
// last frame: its body (ActivityMessage, msgBodySub's richview).
func (e *lmEnv) drawn(ts string) string {
	suffix := "\x00" + lmChan + "/" + ts
	for k, r := range e.v.rich {
		if strings.HasPrefix(k, "sub:") && strings.HasSuffix(k, suffix) {
			return r.v.TextRange(0, r.v.PlainLen())
		}
	}
	return ""
}

var lmUpRe = regexp.MustCompile(`(?:Upvotes|Ups): (\d+)`)

// lmRev is the vote revision text shows (0: none).
func lmRev(text string) int {
	m := lmUpRe.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// rev is the vote revision the Activity row drew in the last frame.
func (e *lmEnv) rev() int { return lmRev(e.drawn(lmVote)) }

// paneRev is the vote revision the thread pane drew in the last frame.
func (e *lmEnv) paneRev() int {
	for _, s := range e.tv.Senders() {
		if s.TS == lmVote {
			return lmRev(s.Text)
		}
	}
	return 0
}

// shown reports whether the page lists the entry key.
func (e *lmEnv) shown(key string) bool {
	p := actPage(e.v)
	for _, i := range e.v.vis {
		if i >= 0 && i < len(p.Items) && actKey(&p.Items[i]) == key {
			return true
		}
	}
	return false
}

// rowH is the entry key's row height in the last frame (-1: not drawn).
func (e *lmEnv) rowH(key string) int {
	p := actPage(e.v)
	for li, i := range e.v.vis {
		if i >= 0 && i < len(p.Items) && actKey(&p.Items[i]) == key {
			if h, ok := e.v.stk.h[li]; ok {
				return h
			}
		}
	}
	return -1
}

// untilVote waits for the page (and the pane, when open) to draw the vote
// at revision n.
func (e *lmEnv) untilVote(n int) {
	e.t.Helper()
	e.e.until(e.t, fmt.Sprintf("the vote drawn at revision %d", n), func() bool {
		return e.rev() == n && (e.tv == nil || e.paneRev() == n)
	})
	for i := 0; i < 5; i++ {
		e.frame()
	}
}

// inject is the socket bringing ev (core.Inject).
func (e *lmEnv) inject(ev map[string]any) {
	evTS := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
	ev["ts"], ev["event_ts"] = evTS, evTS
	raw, _ := json.Marshal(ev)
	sub, _ := ev["subtype"].(string)
	typ, _ := ev["type"].(string)
	chID, _ := ev["channel"].(string)
	ch, _ := json.Marshal(firstNonEmpty(chID, lmChan))
	e.app.Inject(slack.Event{Type: typ, Subtype: sub, Channel: ch, Raw: raw})
}

// setRev moves the server's vote to revision n, with no event.
func (e *lmEnv) setRev(n int) {
	e.f.mu.Lock()
	e.f.rev = n
	e.f.mu.Unlock()
}

// vote is the app's chat.update to revision n: the server's message, then
// the socket's message_changed.
func (e *lmEnv) vote(n int) {
	prev := lmVoteMsg(n - 1)
	e.setRev(n)
	e.inject(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": lmChan,
		"message": lmVoteMsg(n), "previous_message": prev})
}

// editMention edits the mention to text.
func (e *lmEnv) editMention(text string) {
	e.f.mu.Lock()
	prev := slack.Message{Type: "message", TS: lmMention, User: "U0FAKE00002", Text: e.f.mention}
	e.f.mention = text
	e.f.mu.Unlock()
	now := slack.Message{Type: "message", TS: lmMention, User: "U0FAKE00002", Text: text,
		Edited: &slack.Edited{User: "U0FAKE00002", TS: fmt.Sprintf("%d.000001", time.Now().Unix())}}
	e.inject(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": lmChan,
		"message": now, "previous_message": prev})
}

// changeMention is a message_changed of the mention to now (its subtype:
// a tombstone, a moderated message), the server's copy going with it.
func (e *lmEnv) changeMention(now slack.Message) {
	e.f.mu.Lock()
	prev := slack.Message{Type: "message", TS: lmMention, User: "U0FAKE00002", Text: e.f.mention}
	e.f.mention = now.Text
	e.f.mu.Unlock()
	now.Type, now.TS = "message", lmMention
	e.inject(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": lmChan,
		"message": now, "previous_message": prev})
}

// del deletes the message ts: gone from the server, then the socket's
// message_deleted.
func (e *lmEnv) del(ts string) {
	e.f.mu.Lock()
	prev, _ := e.f.msgLocked(ts)
	e.f.gone[ts] = true
	e.f.mu.Unlock()
	e.inject(map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "channel": lmChan,
		"deleted_ts": ts, "previous_message": prev})
}

// invalidate is activity_invalidated: the page reads the feed again.
func (e *lmEnv) invalidate() {
	raw, _ := json.Marshal(map[string]any{"type": "activity", "subtype": "activity_invalidated"})
	e.app.Inject(slack.Event{Type: "activity", Subtype: "activity_invalidated", Raw: raw})
}

// react is user's tada on the message ts (reaction_added, or with add
// false reaction_removed).
func (e *lmEnv) react(ch, ts, user, name string, add bool) {
	typ := "reaction_removed"
	if add {
		typ = "reaction_added"
	}
	e.inject(map[string]any{"type": typ, "user": user, "reaction": name, "channel": ch,
		"item": map[string]any{"type": "message", "channel": ch, "ts": ts}})
}

// The vote app's chat.update reaches the open Activity page in the frame
// after it lands, natively and under a plugin wrapping Activity's
// components; no frame draws an older revision after a newer one, the row
// keeps its height, and no activity.feed goes out for it. A reload after
// the server's copy moved on with no event (a socket gap) draws the
// reload's revision: a change the page took earlier doesn't outrank it.
func TestActivityLiveVoteCounts(t *testing.T) {
	t.Run("native", func(t *testing.T) { checkActivityLiveVotes(t, lmOpts{}) })
	t.Run("patched", func(t *testing.T) { checkActivityLiveVotes(t, lmOpts{patch: lmPatches}) })
	// JS answers every render before the next frame
	t.Run("patched lockstep", func(t *testing.T) { checkActivityLiveVotes(t, lmOpts{patch: lmPatches, lockstep: true}) })
}

func checkActivityLiveVotes(t *testing.T, o lmOpts) {
	e := newLmEnv(t, o)
	e.untilVote(1)
	h0 := e.rowH(lmThreadKey)
	if h0 <= 0 {
		t.Fatalf("the thread's row has height %d", h0)
	}
	feeds := e.fs.CallCount("activity.feed")
	shown := 1
	for n := 2; n <= 7; n++ {
		e.vote(n)
		for i := 0; i < 12; i++ {
			e.frame()
			got := e.rev()
			if i == 0 && got != n {
				t.Fatalf("revision %d: the first frame after its chat.update drew revision %d (%q); Slack's row reads the store the update landed in",
					n, got, e.drawn(lmVote))
			}
			if got < shown {
				t.Fatalf("revision %d, frame %d: revision %d drawn after revision %d", n, i, got, shown)
			}
			shown = max(shown, got)
			if h := e.rowH(lmThreadKey); h != h0 {
				t.Errorf("revision %d, frame %d: the row is %d px high, was %d", n, i, h, h0)
			}
			if !o.lockstep {
				time.Sleep(5 * time.Millisecond) // JS answers when it does
			}
		}
	}
	if n := e.fs.CallCount("activity.feed") - feeds; n != 0 {
		t.Errorf("%d activity.feed calls while the vote changed; Slack's rows read the store, no feed", n)
	}
	// the server's vote moves on unheard; the feed is read again
	e.setRev(9)
	e.invalidate()
	e.e.until(t, "the reload's revision 9", func() bool {
		if got := e.rev(); got < 7 {
			t.Fatalf("revision %d drawn while the reload was out", got)
		}
		return e.rev() == 9
	})
	for i := 0; i < 10; i++ {
		e.frame()
		if got := e.rev(); got != 9 {
			t.Fatalf("frame %d after the reload drew revision %d", i, got)
		}
	}
}

// With the plugin's JS busy through six revisions, every tree it hands
// back is a revision or more behind the one the row sent: each one is the
// row's own props, not the plugin's text, so the row draws every revision
// in the frame after it landed, and still the newest once JS answers.
func TestActivityLiveVoteJSHeld(t *testing.T) {
	e := newLmEnv(t, lmOpts{patch: lmPatches})
	e.untilVote(1)
	release := e.holdJS()
	defer release()
	for n := 2; n <= 8; n++ {
		e.vote(n)
		for i := 0; i < 4; i++ {
			e.frame()
			if got := e.rev(); got != n {
				t.Fatalf("revision %d, frame %d with JS busy: drew revision %d (%q)", n, i, got, e.drawn(lmVote))
			}
		}
	}
	release()
	e.untilVote(8)
	for i := 0; i < 20; i++ {
		e.frame()
		if got := e.rev(); got != 8 {
			t.Fatalf("frame %d after JS answered drew revision %d", i, got)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Under a plugin's text patch of the messages slice (Message Logger's
// kind of patch: patchMessageText), the row draws the plugin's text for
// every revision, as the message lists do: within a few frames of the
// chat.update, never an older revision after a newer one, and never the
// text without the patch once the patch drew.
func TestActivityLiveVoteSlicePatch(t *testing.T) {
	e := newLmEnv(t, lmOpts{texts: true, setup: func(e *lmEnv) {
		// the vote's thread is in the store (opened earlier), so the
		// plugins' slice holds the vote
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := e.app.RefreshReplies(ctx, lmChan, lmRoot); err != nil {
			t.Fatal(err)
		}
	}})
	e.e.until(t, "the plugin's text drawn", func() bool { return strings.Contains(e.drawn(lmVote), "Ups: 1") })
	shown := 1
	for n := 2; n <= 6; n++ {
		e.vote(n)
		e.e.until(t, fmt.Sprintf("revision %d with the plugin's text", n), func() bool {
			got := e.drawn(lmVote)
			if strings.Contains(got, "Upvotes") {
				t.Fatalf("revision %d: the row drew the text without the plugin's patch: %q", n, got)
			}
			if r := lmRev(got); r < shown {
				t.Fatalf("revision %d: revision %d drawn after %d", n, r, shown)
			}
			shown = max(shown, lmRev(got))
			return lmRev(got) == n
		})
	}
}

// The same vote open in Activity and in the thread pane beside it:
// Activity never draws a revision the pane hasn't drawn, and draws the
// pane's within a frame, through the socket's updates and through a
// refresh that brings a revision no event announced (after a socket gap
// the pane reads its thread again). When the pane closes the row keeps
// that revision: the store has it, and nothing older takes its place.
func TestActivityLiveVoteBesideThread(t *testing.T) {
	t.Run("native", func(t *testing.T) { checkActivityVoteBeside(t, nil) })
	t.Run("patched", func(t *testing.T) { checkActivityVoteBeside(t, lmPatches) })
}

func checkActivityVoteBeside(t *testing.T, patch []string) {
	e := newLmEnv(t, lmOpts{pane: true, patch: patch})
	e.untilVote(1)
	inStep := func(n int, what string) {
		t.Helper()
		behind := 0
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
			e.frame()
			a, p := e.rev(), e.paneRev()
			switch {
			case a > p:
				t.Fatalf("%s: Activity drew revision %d while the thread pane drew %d", what, a, p)
			case a < p:
				if behind++; behind > 1 {
					t.Fatalf("%s: Activity drew revision %d for %d frames after the thread pane drew %d", what, a, behind, p)
				}
			default:
				behind = 0
				if a == n {
					for i := 0; i < 8; i++ {
						e.frame()
						if a, p := e.rev(), e.paneRev(); a != n || p != n {
							t.Fatalf("%s: after both drew revision %d, Activity drew %d and the pane %d", what, n, a, p)
						}
					}
					return
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("%s: timed out: Activity at revision %d, the pane at %d, want %d", what, e.rev(), e.paneRev(), n)
	}
	for n := 2; n <= 5; n++ {
		e.vote(n)
		inStep(n, fmt.Sprintf("the chat.update to revision %d", n))
	}
	// no event: the server's revision moves, the thread is read again (a
	// socket gap's), and the pane's list reloads it; no change of core's
	// says revision 6, only the list on screen and the store have it
	e.setRev(6)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := e.app.RefreshReplies(ctx, lmChan, lmRoot); err != nil {
		t.Fatal(err)
	}
	messages.ApplyEvent(e.app, core.Event{Type: "resync"})
	inStep(6, "the pane reading its thread again")
	// the pane closes
	e.h.lists, e.tv = false, nil
	for i := 0; i < 12; i++ {
		e.frame()
		if got := e.rev(); got != 6 {
			t.Fatalf("frame %d after the pane closed drew revision %d; the pane and the store had 6", i, got)
		}
		time.Sleep(5 * time.Millisecond)
	}
	e.vote(7)
	e.frame()
	if got := e.rev(); got != 7 {
		t.Fatalf("the chat.update after the pane closed: drew revision %d", got)
	}
}

// An edit of an Activity entry's message draws in the next frame; an edit
// that takes the viewer's mention out takes the entry off the page, as
// Slack's messageChanged does (checkForMentionInMessage of previous_message
// and the new copy, then "Remove activity item"), and the other entries
// stay. So does a copy that is a tombstone, or moderated, or thread_hidden.
func TestActivityLiveEdit(t *testing.T) {
	e := newLmEnv(t, lmOpts{})
	e.untilVote(1)
	e.e.until(t, "the mention drawn", func() bool { return strings.Contains(e.drawn(lmMention), "please read the synthetic standup notes") })
	e.editMention("<@" + fakeslack.SelfID + "> please read the new synthetic notes")
	e.frame()
	if got := e.drawn(lmMention); !strings.Contains(got, "please read the new synthetic notes") {
		t.Fatalf("the frame after the edit drew %q", got)
	}
	if !e.shown(lmMentionKey) {
		t.Fatal("an edit that keeps the mention took the entry off the page")
	}
	e.editMention("the synthetic standup notes are up")
	e.frame()
	if e.shown(lmMentionKey) {
		t.Fatalf("the frame after an edit took the mention out still lists the mention (drawn %q)", e.drawn(lmMention))
	}
	if !e.shown(lmThreadKey) {
		t.Fatal("the thread's entry went with the mention's")
	}
	for i := 0; i < 10; i++ {
		e.frame()
		if e.shown(lmMentionKey) {
			t.Fatalf("frame %d: the mention came back", i)
		}
	}
	for _, c := range []struct {
		name string
		now  slack.Message
		text string // the mention's text before (default: the viewer's)
		typ  string
	}{
		{name: "@channel edited out", text: "<!channel> please read the synthetic standup notes", typ: "at_channel",
			now: slack.Message{User: "U0FAKE00002", Text: "please read the synthetic standup notes"}},
		{name: "@here edited out", text: "<!here> please read the synthetic standup notes", typ: "at_channel",
			now: slack.Message{User: "U0FAKE00002", Text: "please read the synthetic standup notes"}},
		{name: "tombstone", now: slack.Message{Subtype: "tombstone", User: "USLACKBOT", Text: "This message was deleted."}},
		{name: "moderated", now: slack.Message{Subtype: "moderated", User: "U0FAKE00002", Text: ""}},
		{name: "thread_hidden", now: slack.Message{Subtype: "thread_hidden", User: "U0FAKE00002", Text: ""}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newLmEnv(t, lmOpts{setup: func(e *lmEnv) {
				if c.text != "" {
					e.f.mention = c.text
				}
				e.f.mentions = c.typ
			}})
			e.untilVote(1)
			e.e.until(t, "the mention listed", func() bool { return e.shown(lmMentionKey) && e.drawn(lmMention) != "" })
			e.changeMention(c.now)
			e.frame()
			if e.shown(lmMentionKey) {
				t.Fatalf("the frame after the change still lists the mention (drawn %q)", e.drawn(lmMention))
			}
			if !e.shown(lmThreadKey) {
				t.Fatal("the thread's entry went too")
			}
		})
	}
}

// A deleted message's entries leave Activity in the next frame (Slack's
// messageDeleted dispatches "Remove activity item" {channelId, messageTs}):
// the mention, and a thread bundle whose messageTs (/29I: its first
// unread reply, min_unread_ts) it was. The bundle whose latest reply, the
// one it draws, was deleted stays, and draws no message and no loading
// shimmer. Nothing comes back.
func TestActivityLiveDelete(t *testing.T) {
	e := newLmEnv(t, lmOpts{latest: true})
	e.e.until(t, "the thread's latest reply drawn", func() bool { return strings.Contains(e.drawn(lmLatest), "a synthetic reply after the vote") })
	e.del(lmMention)
	e.frame()
	if e.shown(lmMentionKey) {
		t.Fatal("the frame after its message was deleted still lists the mention")
	}
	e.del(lmLatest)
	e.frame()
	if !e.shown(lmThreadKey) {
		t.Fatal("the thread left with its latest reply; its messageTs is the first unread one")
	}
	if r := e.v.rows[lmThreadKey]; r == nil || !r.msgGone || r.msg() != nil {
		t.Fatalf("the thread's row after its latest reply went: %+v", r)
	}
	e.del(lmVote)
	e.frame()
	if e.shown(lmThreadKey) {
		t.Fatal("the frame after its first unread reply was deleted still lists the thread")
	}
	for i := 0; i < 10; i++ {
		e.frame()
		if e.shown(lmMentionKey) || e.shown(lmThreadKey) {
			t.Fatalf("frame %d: a deleted message's entry came back", i)
		}
	}
}

// An entry whose message messages.list doesn't have (Slack's isNonExistent
// stub, after its UFM fetch) draws no body and no loading shimmer.
func TestActivityLiveMissingMessage(t *testing.T) {
	e := newLmEnv(t, lmOpts{setup: func(e *lmEnv) { e.f.gone[lmMention], e.f.listGone = true, true }})
	e.untilVote(1)
	e.e.until(t, "the mention's row", func() bool { return e.shown(lmMentionKey) && e.v.rows[lmMentionKey] != nil })
	for i := 0; i < 5; i++ {
		e.frame()
	}
	if r := e.v.rows[lmMentionKey]; !r.msgGone || r.msg() != nil || e.drawn(lmMention) != "" {
		t.Fatalf("the mention whose message there is none of: gone %v, message %v, drawn %q; want no body (no shimmer)", r.msgGone, r.msg(), e.drawn(lmMention))
	}
}

// Your reaction to an Activity entry's message from another client
// (reaction_added) marks its read row "You reacted" in the next frame: the
// message came by id for Activity alone (nobody opened its channel), into
// the store where the socket's reactions reach it, as in Slack's messages
// store; the reaction's removal takes the mark off.
func TestActivityLiveReaction(t *testing.T) {
	e := newLmEnv(t, lmOpts{})
	e.untilVote(1)
	if r := e.v.rows[lmMentionKey]; r == nil || r.markOK {
		t.Fatalf("the mention's row before the reaction: %+v", r)
	}
	e.react(lmChan, lmMention, fakeslack.SelfID, "tada", true)
	e.frame()
	if r := e.v.rows[lmMentionKey]; !r.markOK || !strings.Contains(r.mark.tip, ":tada:") {
		t.Fatalf("the frame after your reaction: mark %v %+v, want You reacted: :tada:", r.markOK, r.mark)
	}
	e.react(lmChan, lmMention, fakeslack.SelfID, "tada", false)
	e.frame()
	if r := e.v.rows[lmMentionKey]; r.markOK {
		t.Fatalf("the frame after your reaction went: mark %+v", r.mark)
	}
}

// A message a window held when the page loaded (its channel open
// earlier) still takes the socket's reactions after core lets the window
// go: the store kept it for Activity, as Slack's store keeps every
// message a view reads.
func TestActivityLiveWindowEvicted(t *testing.T) {
	e := newLmEnv(t, lmOpts{setup: func(e *lmEnv) {
		e.f.mu.Lock()
		m, _ := e.f.msgLocked(lmMention)
		e.f.mu.Unlock()
		e.app.S.SetLatest(lmChan, []slack.Message{m}, false)
	}})
	e.untilVote(1)
	e.e.until(t, "the mention drawn", func() bool { return e.drawn(lmMention) != "" })
	e.app.S.SetLatest(lmChan, []slack.Message{}, false) // the window evicted
	e.react(lmChan, lmMention, fakeslack.SelfID, "tada", true)
	e.frame()
	if r := e.v.rows[lmMentionKey]; !r.markOK || !strings.Contains(r.mark.tip, ":tada:") {
		t.Fatalf("the frame after your reaction to a message its window no longer holds: mark %v %+v", r.markOK, r.mark)
	}
}

// An entry the socket pushes while the page is off screen, then an edit of
// its message, also unseen: the first frame that draws the entry draws
// the edit (it takes what core changed after it was read).
func TestActivityLiveEntryWhileHidden(t *testing.T) {
	e := newLmEnv(t, lmOpts{})
	e.untilVote(1)
	// off screen from here: no frames
	e.f.mu.Lock()
	e.f.mention2 = "<@" + fakeslack.SelfID + "> the synthetic retro is at 3"
	e.f.mu.Unlock()
	raw, _ := json.Marshal(map[string]any{"type": "activity", "subtype": "activity_updated", "key": lmMention2Key,
		"entry": map[string]any{"key": lmMention2Key, "feed_ts": lmMention2, "is_unread": true, "item": map[string]any{
			"type": "at_user", "message": map[string]any{"ts": lmMention2, "channel": lmChan, "author_user_id": "U0FAKE00002"}}}})
	e.app.Inject(slack.Event{Type: "activity", Subtype: "activity_updated", Raw: raw})
	time.Sleep(50 * time.Millisecond) // the page's event goroutine takes the entry
	e.f.mu.Lock()
	prev := slack.Message{Type: "message", TS: lmMention2, User: "U0FAKE00002", Text: e.f.mention2}
	e.f.mention2 = "<@" + fakeslack.SelfID + "> the synthetic retro moved to 4"
	now := slack.Message{Type: "message", TS: lmMention2, User: "U0FAKE00002", Text: e.f.mention2,
		Edited: &slack.Edited{User: "U0FAKE00002", TS: fmt.Sprintf("%d.000001", time.Now().Unix())}}
	e.f.mu.Unlock()
	e.inject(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": lmChan,
		"message": now, "previous_message": prev})
	// on screen again
	e.e.until(t, "the pushed entry drawn", func() bool {
		if !e.shown(lmMention2Key) || e.drawn(lmMention2) == "" {
			return false
		}
		if got := e.drawn(lmMention2); !strings.Contains(got, "moved to 4") {
			t.Fatalf("the first frame drawing the pushed entry drew %q, not the edit made before", got)
		}
		return true
	})
}

// While the page is off screen the mention is edited to leave the viewer
// out and the thread's first unread reply is deleted, then the socket
// changes thousands of other messages: when the page draws again, both
// entries are gone and nothing was read again.
func TestActivityLiveManyChangesHidden(t *testing.T) {
	e := newLmEnv(t, lmOpts{})
	e.untilVote(1)
	e.e.until(t, "the mention drawn", func() bool { return e.drawn(lmMention) != "" })
	busy := slack.Message{Type: "message", TS: "1762790500.000100", User: "U0FAKE00001", Text: "a synthetic busy message"}
	e.app.S.SetLatest(lmBusy, []slack.Message{busy}, false)
	feeds := e.fs.CallCount("activity.feed")
	// off screen from here
	e.editMention("the synthetic standup notes are up")
	e.del(lmVote)
	for i := 0; i < 4500; i++ {
		e.react(lmBusy, busy.TS, "U0FAKE00001", "eyes", i%2 == 0)
	}
	e.frame()
	if e.shown(lmMentionKey) || e.shown(lmThreadKey) {
		t.Fatalf("after 4500 other changes: the mention listed %v, the thread %v; Slack removed both", e.shown(lmMentionKey), e.shown(lmThreadKey))
	}
	for i := 0; i < 10; i++ {
		e.frame()
		if e.shown(lmMentionKey) || e.shown(lmThreadKey) {
			t.Fatalf("frame %d: an entry came back", i)
		}
	}
	if n := e.fs.CallCount("activity.feed") - feeds; n != 0 {
		t.Errorf("%d activity.feed calls; the changes said all there was", n)
	}
}

// An entry removed while a feed load is out (its message deleted after
// the request went out) stays removed when the answer, read before the
// deletion, lands.
func TestActivityLiveRemovedDuringLoad(t *testing.T) {
	e := newLmEnv(t, lmOpts{})
	e.untilVote(1)
	e.fs.SetFault("activity.feed", fakeslack.Fault{ReplyDelay: 700 * time.Millisecond, Times: 1})
	feeds := e.fs.CallCount("activity.feed")
	e.invalidate()
	e.e.until(t, "the reload out", func() bool { return e.fs.CallCount("activity.feed") > feeds })
	e.inject(map[string]any{"type": "message", "subtype": "message_deleted", "hidden": true, "channel": lmChan,
		"deleted_ts": lmMention}) // the server's answer, read before, still lists it
	e.frame()
	if e.shown(lmMentionKey) {
		t.Fatal("the frame after the deletion still lists the mention")
	}
	e.e.until(t, "the reload landed", func() bool { return !e.v.data.busy() && !e.v.wasBusy })
	for i := 0; i < 10; i++ {
		e.frame()
		if e.shown(lmMentionKey) {
			t.Fatalf("frame %d after the reload: the mention whose message was deleted came back", i)
		}
	}
	if !e.shown(lmThreadKey) {
		t.Fatal("the reload lost the thread's entry")
	}
}

// The in-view preview of an opened entry draws its message as it is now:
// an edit shows there in the next frame too.
func TestActivityLivePreview(t *testing.T) {
	e := newLmEnv(t, lmOpts{})
	e.untilVote(1)
	e.e.until(t, "the mention drawn", func() bool { return e.drawn(lmMention) != "" })
	page := actPage(e.v)
	for i := range page.Items {
		if actKey(&page.Items[i]) == lmMentionKey {
			e.v.sel, e.v.selKey, e.v.selOK = page.Items[i], lmMentionKey, true
		}
	}
	pane := func() string {
		if r := e.v.rich["pane:"+lmChan+"/"+lmMention]; r != nil {
			return r.v.TextRange(0, r.v.PlainLen())
		}
		return ""
	}
	e.e.until(t, "the preview's message", func() bool { return strings.Contains(pane(), "please read the synthetic standup notes") })
	e.editMention("<@" + fakeslack.SelfID + "> please read the new synthetic notes")
	e.frame()
	if got := pane(); !strings.Contains(got, "please read the new synthetic notes") {
		t.Fatalf("the preview the frame after the edit drew %q", got)
	}
}
