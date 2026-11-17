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
	lmChan       = "C0LIVE0001"
	lmBot        = "B0LIVE0001"
	lmApp        = "A0LIVE0001"
	lmRoot       = "1762790000.000100" // Ada's proposal: the vote's thread
	lmVote       = "1762790100.000100" // the vote app's message, the thread's latest reply
	lmMention    = "1762790200.000100" // Grace's mention of the viewer
	lmThreadKey  = "lm-thread"
	lmMentionKey = "lm-mention"
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

func lmRootMsg() slack.Message {
	return slack.Message{Type: "message", TS: lmRoot, User: "U0FAKE00001", Text: "Proposal: move the synthetic standup to 10am",
		ThreadTS: lmRoot, ReplyCount: 1, ReplyUsersN: 1, LatestReply: lmVote}
}

// lmFake is the server's side: the vote's revision, the mention's text
// and the deleted messages, which activity.feed, messages.list and
// conversations.replies answer from.
type lmFake struct {
	mu      sync.Mutex
	rev     int
	mention string
	gone    map[string]bool
}

func (f *lmFake) msgLocked(ts string) (slack.Message, bool) {
	if f.gone[ts] {
		return slack.Message{}, false
	}
	switch ts {
	case lmRoot:
		return lmRootMsg(), true
	case lmVote:
		return lmVoteMsg(f.rev), true
	case lmMention:
		return slack.Message{Type: "message", TS: lmMention, User: "U0FAKE00002", Text: f.mention}, true
	}
	return slack.Message{}, false
}

func (f *lmFake) hook(method string, form url.Values) (any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch method {
	case "activity.feed":
		items := []any{}
		if !f.gone[lmMention] { // a read mention of the viewer
			items = append(items, map[string]any{"key": lmMentionKey, "feed_ts": lmMention, "is_unread": false, "item": map[string]any{
				"type": "at_user", "message": map[string]any{"ts": lmMention, "channel": lmChan, "author_user_id": "U0FAKE00002"}}})
		}
		if !f.gone[lmVote] { // the thread the vote is the latest (and first unread) reply of
			items = append(items, map[string]any{"key": lmThreadKey, "feed_ts": lmVote, "is_unread": true, "item": map[string]any{
				"type": "thread_v2", "bundle_info": map[string]any{"payload": map[string]any{"thread_entry": map[string]any{
					"channel_id": lmChan, "thread_ts": lmRoot, "latest_ts": lmVote, "min_unread_ts": lmVote, "unread_msg_count": 1}}}}})
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
		msgs := []slack.Message{lmRootMsg()}
		if m, ok := f.msgLocked(lmVote); ok {
			msgs = append(msgs, m)
		}
		return map[string]any{"ok": true, "messages": msgs, "has_more": false}, true
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

type lmEnv struct {
	t   *testing.T
	fs  *fakeslack.Server
	app *core.App
	f   *lmFake
	v   *Activity
	tv  *messages.ThreadView // the thread pane beside the page (nil: none)
	e   *vaEnv
}

// newLmEnv is the Activity page on the fixture's feed, with the thread
// pane beside it when pane is set; patch names the components a
// pass-through plugin wraps (nil: none), and lockstep has JS answer every
// render before the next frame (the timing that bounced patched rows
// between revisions in the message lists).
func newLmEnv(t *testing.T, pane bool, patch []string, lockstep bool) *lmEnv {
	t.Helper()
	app, fs := vaFake(t)
	f := &lmFake{rev: 1, mention: "<@" + fakeslack.SelfID + "> please read the synthetic standup notes", gone: map[string]bool{}}
	fs.SetHook(f.hook)
	app.S.PutConv(slack.Conversation{ID: lmChan, Name: "live-votes", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.PutBot(lmBot, store.BotInfo{Name: "Vote Bot"})
	sh := &slickHost{app: app, th: theme.New(false)}
	var step func()
	if patch != nil {
		var tt *taut.Taut
		tt, step = lmPatch(t, app, patch)
		sh.tt = tt
		if !lockstep {
			step = nil
		}
	}
	h := &lmHost{vaHost: &vaHost{slickHost: sh}, lists: pane}
	env := &lmEnv{t: t, fs: fs, app: app, f: f, v: NewActivity(h), e: &vaEnv{size: image.Pt(1400, 900)}}
	if pane {
		env.tv = messages.NewThreadView(h, lmChan, lmRoot)
	}
	env.e.draw = func(gtx layout.Context) {
		if patch != nil {
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

// lmPatch runs a pass-through plugin over names, as the ui root runs
// Taut, and returns its runtime and a step that waits for JS to have
// answered everything sent to it so far.
func lmPatch(t *testing.T, app *core.App, names []string) (*taut.Taut, func()) {
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
	id, err := tt.Import(ctx, lmPassThroughJS(names), "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatalf("enable %s: %v", id, err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		all := true
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
	return tt, func() {
		if err := rt.Do(func(*jsrt.Ctx) error { return nil }); err != nil {
			t.Errorf("waiting for JS: %v", err)
		}
	}
}

// lmPassThroughJS patches names without changing anything: each hands
// props.msg on to Original.
func lmPassThroughJS(names []string) []byte {
	patches := ""
	for _, n := range names {
		patches += "    this.api.patchComponent('" + n + "', (Original) => (props) => {\n" +
			"      const msg = React.useMemo(() => props.msg, [props.msg])\n" +
			"      return React.createElement(Original, { ...props, msg })\n" +
			"    })\n"
	}
	return []byte("(() => {\nconst TautPlugin = globalThis.TautPlugin, opt = globalThis.TautOpt;\n" +
		"class LivePassThrough extends TautPlugin {\n" +
		"  static id = 'LivePassThrough'\n  static pluginName = 'Activity pass-through (test)'\n" +
		"  static description = 'test: wraps the Activity components and changes nothing'\n" +
		"  static authors = ['clack']\n  static category = 'messages'\n  static defaultConfig = { enabled: true }\n" +
		"  start() {\n" + patches + "  }\n}\nreturn LivePassThrough\n})()")
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

var lmUpRe = regexp.MustCompile(`Upvotes: (\d+)`)

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
	ch, _ := json.Marshal(lmChan)
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

// The vote app's chat.update reaches the open Activity page in the frame
// after it lands, natively and under a plugin wrapping Activity's
// components; no frame draws an older revision after a newer one, the row
// keeps its height, and no activity.feed goes out for it.
func TestActivityLiveVoteCounts(t *testing.T) {
	t.Run("native", func(t *testing.T) { checkActivityLiveVotes(t, nil, false) })
	t.Run("patched", func(t *testing.T) { checkActivityLiveVotes(t, lmPatches, false) })
	// JS answers every render before the next frame
	t.Run("patched lockstep", func(t *testing.T) { checkActivityLiveVotes(t, lmPatches, true) })
}

func checkActivityLiveVotes(t *testing.T, patch []string, lockstep bool) {
	e := newLmEnv(t, false, patch, lockstep)
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
			if !lockstep {
				time.Sleep(5 * time.Millisecond) // JS answers when it does
			}
		}
	}
	if n := e.fs.CallCount("activity.feed") - feeds; n != 0 {
		t.Errorf("%d activity.feed calls while the vote changed; Slack's rows read the store, no feed", n)
	}
}

// The same vote open in Activity and in the thread pane beside it:
// Activity never draws a revision the pane hasn't drawn, and draws the
// pane's within a frame, through the socket's updates and through a
// refresh that brings a revision no event announced (after a socket gap
// the pane reads its thread again).
func TestActivityLiveVoteBesideThread(t *testing.T) {
	t.Run("native", func(t *testing.T) { checkActivityVoteBeside(t, nil) })
	t.Run("patched", func(t *testing.T) { checkActivityVoteBeside(t, lmPatches) })
}

func checkActivityVoteBeside(t *testing.T, patch []string) {
	e := newLmEnv(t, true, patch, false)
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
	// no event: the server's revision moves, and the pane's next read
	// (a socket gap's resync) brings it
	e.setRev(6)
	messages.ApplyEvent(e.app, core.Event{Type: "resync"})
	inStep(6, "the pane reading its thread again")
}

// An edit of an Activity entry's message draws in the next frame; an edit
// that takes the viewer's mention out takes the entry off the page, as
// Slack's messageChanged does (checkForMentionInMessage of the copy before
// and after, then "Remove activity item"), and the other entries stay.
func TestActivityLiveEdit(t *testing.T) {
	e := newLmEnv(t, false, nil, false)
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
}

// A deleted message's entries leave Activity in the next frame (Slack's
// messageDeleted dispatches "Remove activity item" {channelId,
// messageTs}): the mention, and the thread's bundle whose first unread
// reply the deleted vote was. Nothing comes back.
func TestActivityLiveDelete(t *testing.T) {
	e := newLmEnv(t, false, nil, false)
	e.untilVote(1)
	e.del(lmMention)
	e.frame()
	if e.shown(lmMentionKey) {
		t.Fatal("the frame after its message was deleted still lists the mention")
	}
	if !e.shown(lmThreadKey) || e.rev() != 1 {
		t.Fatalf("the thread's entry: listed %v, its vote at revision %d", e.shown(lmThreadKey), e.rev())
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

// Your reaction to an Activity entry's message from another client
// (reaction_added) marks its read row "You reacted" in the next frame: the
// message came by id for Activity alone (nobody opened its channel), into
// the store where the socket's reactions reach it, as in Slack's messages
// store; the reaction's removal takes the mark off.
func TestActivityLiveReaction(t *testing.T) {
	e := newLmEnv(t, false, nil, false)
	e.untilVote(1)
	if r := e.v.rows[lmMentionKey]; r == nil || r.markOK {
		t.Fatalf("the mention's row before the reaction: %+v", r)
	}
	react := func(add bool) {
		typ := "reaction_removed"
		if add {
			typ = "reaction_added"
		}
		e.inject(map[string]any{"type": typ, "user": fakeslack.SelfID, "reaction": "tada",
			"item": map[string]any{"type": "message", "channel": lmChan, "ts": lmMention}})
	}
	react(true)
	e.frame()
	if r := e.v.rows[lmMentionKey]; !r.markOK || !strings.Contains(r.mark.tip, ":tada:") {
		t.Fatalf("the frame after your reaction: mark %v %+v, want You reacted: :tada:", r.markOK, r.mark)
	}
	react(false)
	e.frame()
	if r := e.v.rows[lmMentionKey]; r.markOK {
		t.Fatalf("the frame after your reaction went: mark %+v", r.mark)
	}
}
