package messages

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Taut plugin support (docs/PLUGINS-taut.md). The ui root calls SetTaut
// once the plugin system exists; until then (and while no plugin patches
// anything) every check below is one atomic load and a nil or flag test,
// so the list pays nothing.

var tautRef atomic.Pointer[taut.Taut]

// SetTaut installs (or with nil removes) the plugin system. Wire
// UIHooks.ThreadPane to ThreadPane as well.
func SetTaut(t *taut.Taut) { tautRef.Store(t) }

func tautOf() *taut.Taut { return tautRef.Load() }

// ---- thunks, with Slack's argument shapes ----

// markLastReadArgs is Slack's markLastRead {id, ts}.
type markLastReadArgs struct {
	ID       string `json:"id"`
	TS       string `json:"ts"`
	ThreadTS string `json:"threadTs,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// rxnUserArgs is changeReactionFromUserAction {key: _rxn_key, name, add?}.
type rxnUserArgs struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Add  *bool  `json:"add,omitempty"`
	// SkipFrecencyRecord: the emoji picker already recorded the pick.
	SkipFrecencyRecord bool `json:"skipFrecencyRecord,omitempty"`
	// ActionSource is "reaction_toggle_button" for a pill's click
	// (handleReactionClick, aHRx): Name is the group's, and the native
	// removes every tone of it the viewer reacted with or adds the
	// preferred one (reaction_groups.go).
	ActionSource string `json:"actionSource,omitempty"`
	// UseDefaultSkinToneForMultiSkinToneEmoji: aHRx sets it with the
	// toggle button's actionSource.
	UseDefaultSkinToneForMultiSkinToneEmoji bool `json:"useDefaultSkinToneForMultiSkinToneEmoji,omitempty"`
}

// rxnSubmitArgs is submitAddOrRemoveReaction {reactionKey, name, add}.
type rxnSubmitArgs struct {
	ReactionKey string `json:"reactionKey"`
	Name        string `json:"name"`
	Add         bool   `json:"add"`
}

var rxnKeyRe = regexp.MustCompile(`^message-(\d+\.\d+)-([A-Z0-9]+)$`)

// parseRxnKey splits Slack's reaction key "message-<ts>-<channel>".
func parseRxnKey(k string) (ch, ts string, ok bool) {
	m := rxnKeyRe.FindStringSubmatch(k)
	if m == nil {
		return "", "", false
	}
	return m[2], m[1], true
}

// markRead marks ch read up to ts (off the UI goroutine).
func markRead(ctx context.Context, app *core.App, ch, ts string) error {
	_, err := taut.Dispatch(ctx, tautOf(), "markLastRead", markLastReadArgs{ID: ch, TS: ts},
		func(ctx context.Context, a markLastReadArgs) (any, error) {
			if a.ThreadTS != "" {
				return nil, app.MarkThread(ctx, a.ID, a.ThreadTS, a.TS)
			}
			_, err := app.Mark(ctx, a.ID, a.TS)
			return nil, err
		})
	return err
}

// commitEditArgs is Slack's commitMessageEdit {channelId, ts, blocks,
// text, fileIds}; fileIds only from saveMessageEditWithFiles.
type commitEditArgs struct {
	ChannelID string          `json:"channelId"`
	TS        string          `json:"ts"`
	Blocks    json.RawMessage `json:"blocks,omitempty"`
	Text      string          `json:"text,omitempty"`
	FileIDs   *[]string       `json:"fileIds,omitempty"` // nil: unchanged; empty: every file removed
}

// commitEdit saves an inline edit (composer.SaveEdit) through Slack's
// commitMessageEdit; the native applies the plugins' send transforms like
// saveMessageEdit and calls core.Edit.
// fileIDs (non-nil) is an edit that attached files: chat.update's
// file_ids.
func commitEdit(ctx context.Context, app *core.App, ch, ts, text string, blocks json.RawMessage, fileIDs []string) error {
	t := tautOf()
	args := commitEditArgs{ChannelID: ch, TS: ts, Blocks: blocks, Text: text}
	if fileIDs != nil {
		args.FileIDs = &fileIDs
	}
	_, err := taut.Dispatch(ctx, t, "commitMessageEdit", args,
		func(ctx context.Context, a commitEditArgs) (any, error) {
			b := a.Blocks
			if t != nil && len(b) > 0 && t.HasSendTransforms() {
				b = t.TransformSendBlocks(ctx, b)
			}
			if a.FileIDs != nil {
				return nil, app.EditWithFiles(ctx, a.ChannelID, a.TS, a.Text, b, *a.FileIDs)
			}
			return nil, app.Edit(ctx, a.ChannelID, a.TS, a.Text, b)
		})
	return err
}

// deleteArgs is Slack's chatDeleteFetcher {channel, ts}.
type deleteArgs struct {
	Channel string `json:"channel"`
	TS      string `json:"ts"`
}

// deleteMessage is popups' delete hook (MessageMenuOpts.Delete) through
// Slack's chatDeleteFetcher over core.Delete.
func deleteMessage(ctx context.Context, app *core.App, ch, ts string) error {
	_, err := taut.Dispatch(ctx, tautOf(), "chatDeleteFetcher", deleteArgs{Channel: ch, TS: ts},
		func(ctx context.Context, a deleteArgs) (any, error) { return nil, app.Delete(ctx, a.Channel, a.TS) })
	return err
}

// ---- thread panes: api.threads.usePane() ----

// paneProps adds the $pane prop components inside a thread pane carry.
func (s *listState) paneProps(p slackc.Props) slackc.Props {
	if s.thread && p != nil {
		if s.pane == nil {
			s.pane = slackc.Props{"channelId": s.ch, "threadTs": s.ts, "windowId": "main"}
		}
		p["$pane"] = s.pane
	}
	return p
}

// pane is paneProps for a row's components.
func (r *row) pane(p slackc.Props) slackc.Props {
	if r.rc != nil && r.rc.list != nil {
		return r.rc.list.paneProps(p)
	}
	return p
}

// ThreadPane answers UIHooks.ThreadPane for the pane showing ch/threadTs:
// "loaded" → the loaded message timestamps (root first), "topReply" → the
// first reply's ts, "jumpToReply" (arg = ts) → scrolls to it and flashes
// it, true when the reply is loaded. Safe from any goroutine (it reads the
// shared models, never the UI-goroutine state).
func ThreadPane(op, ch, threadTS, arg string) any {
	m := threadModelFor(ch, threadTS)
	if m == nil {
		switch op {
		case "loaded":
			return []string{}
		case "jumpToReply":
			return false
		}
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch op {
	case "loaded":
		out := make([]string, 0, len(m.msgs))
		for i := range m.msgs {
			out = append(out, m.msgs[i].m.TS)
		}
		return out
	case "topReply":
		for i := range m.msgs {
			if m.msgs[i].m.TS != threadTS {
				return m.msgs[i].m.TS
			}
		}
		return nil
	case "jumpToReply":
		if m.find(arg) < 0 {
			return false
		}
		m.jump = arg
		m.rev++
		go invalidateAll()
		return true
	}
	return nil
}

// threadModelFor finds an open thread's model in any hub.
func threadModelFor(ch, ts string) *model {
	hubsMu.Lock()
	defer hubsMu.Unlock()
	for _, hb := range hubs {
		hb.mu.Lock()
		m := hb.thread[ch+"/"+ts]
		hb.mu.Unlock()
		if m != nil {
			return m
		}
	}
	return nil
}

func invalidateAll() {
	hubsMu.Lock()
	hs := make([]*hub, 0, len(hubs))
	for _, hb := range hubs {
		hs = append(hs, hb)
	}
	hubsMu.Unlock()
	for _, hb := range hs {
		hb.invalidate()
	}
}

// ---- patched message views ----

// viewRefresh bounds how often a visible row re-reads its patched view
// (taut computes views asynchronously and invalidates when they change).
const viewRefresh = 250 * time.Millisecond

// applyView shows the plugins' version of r's message
// (messages.patchMessageText, patchSlice('messages')) when one exists.
// Called only while the messages slice is patched.
func (s *listState) applyView(t *taut.Taut, r *row, now time.Time) {
	if r.edit != nil || r.m.Pending || r.m.Failed {
		return // the message being edited stays as the user wrote it
	}
	// re-read when taut's views changed since (a value fetched, a
	// conversion to be made again: ViewEpoch), else at most every
	// viewRefresh: the frame that came for a change within 250 ms of the
	// row's last read skipped it, and an idle window drew no other
	ep := t.ViewEpoch()
	if now.Sub(r.viewAt) < viewRefresh && r.viewRev == r.rev && r.viewEpoch == ep && (r.heldTil.IsZero() || now.Before(r.heldTil)) {
		return
	}
	// taut keeps each view converted until something it reads changes;
	// when core just changed the row's message, convert it again now
	// rather than show the old state until the mirror sees the event
	changed := r.viewRev != 0 && r.viewRev != r.rev
	first := r.viewRev == 0
	r.viewAt, r.viewRev, r.viewEpoch = now, r.rev, ep
	if changed {
		r.viewConv = 0 // a new revision: compared afresh
	}
	if (first || changed) && !r.viewed {
		// the new source, until JS's copy is of it (ready): a row's first
		// read too. A row made within a JS round trip of an update (the
		// thread pane opened on the vote message just voted on, a list
		// rebuilt) took JS's copy of the revision before, its pane a vote
		// behind the channel's, and the two rows' Block Kit host held the
		// old blocks. So what a filter plugin makes of an edit (a word
		// Censorship masks) shows one JS round trip late, as it does for a
		// new message; ready is the message's own, so that's one round
		// trip however busy the rest of the channel is
		r.viewWait = true
	}
	mv, conv, ready, ok := t.MessageViewState(r.src.Channel, r.src.TS, changed, r.need)
	wasReady := r.viewReady
	r.viewReady = ready && ok
	if !ok {
		if r.viewed {
			r.viewed, r.vmsg = false, nil
			r.recompose()
		}
		r.viewConv = 0
		return
	}
	// keep what only the native knows (pending state, resolved author)
	mv.Pending, mv.Failed, mv.ClientMsgID = r.src.Pending, r.src.Failed, r.src.ClientMsgID
	// and the reactions, core's: Slack keeps them out of the messages
	// slice, in the reactions store under the message's reaction key
	// (client-boot slurpReactions moves a fetched message's there, and
	// reaction_added / reaction_removed change them there: boot-shared
	// cBAo addReaction, yzJf removeReaction), and its ReactionBar reads
	// them from that store by the key (its connect: reactions = bk(state,
	// reactionKey)), not from the message. A plugin patching the messages
	// slice never touches them, and the view, converted from the mirror a
	// moment after core's change, can't hold one back: the row drew the
	// view's old reactions until something else asked for a frame after
	// it caught up (Karim, 2026-11-19: "reactions are not syncing", "in a
	// channel for another")
	mv.Reactions = r.src.Reactions
	if ready && mv.Text != r.src.Text && !t.TextRewrites() {
		// JS's copy of this revision says another text: the plugins rewrite
		// text. The text alone, which a rewrite changes (Censorship's mask
		// is in it, Message Logger's marks): the Doc of a view converted at
		// another moment can name a mention otherwise and say nothing
		t.NoteTextRewrite()
	}
	// A row drawing the view takes one converted again (or the fresh one
	// of a new revision) that differs only in its Doc, past what sameView
	// compares: a name in it (a file's uploader, a tab slug), which taut
	// converts again for just the messages that read a changed member or
	// channel. A row drawing its source doesn't: the lists convert their
	// own copies again for the same changes (hub.reconvert), and taut's
	// conversion, from the plugins' JSON, can differ from the source's in
	// ways that show nothing (every row switching to it redrew it, and
	// sent its components new props, after each "names").
	again := r.viewConv != 0 && conv != r.viewConv
	r.viewConv = conv
	if r.viewed && r.vmsg != nil {
		// The view it draws over (vmsg, not what it draws: that has the
		// components' overrides laid over it), unless a conversion made
		// since says anything else: all of it, not only what a plugin's
		// change shows in. What core works out at each conversion (a
		// broadcast's "View newer replies", reply users, a pin's author)
		// changes with no change to the plugins' JSON, and a row that kept
		// its view whenever text, blocks and the sameView fields matched
		// never showed it.
		// a ready read after one that wasn't is looked at whatever its
		// conversion: JS's answer of the same bytes as the copy it took
		// while not ready makes no new conversion, and the row stayed
		// viewed, its next update drawn late from JS's old copy
		// An edit of a message the plugins rewrite keeps its view of the
		// revision before while JS answers, however long (what it masks
		// never shows, a rewritten author never flashes back: the rows'
		// trade-off, held through a stalled JS). One the plugins left alone,
		// held for its old text under plugins that rewrite text (below),
		// holds for EditHold at most, then draws core's copy of the edit.
		switch {
		case ready:
			r.heldTil = time.Time{}
		case !r.heldTil.IsZero() && !now.Before(r.heldTil):
			r.heldTil = time.Time{}
			r.viewed, r.vmsg = false, nil
			r.recompose()
			return
		}
		turned := ready && !wasReady
		if !again && !changed && !turned {
			return
		}
		if ready && sameView(&r.src, &mv) && r.src.Text == mv.Text && reflect.DeepEqual(r.src.Doc.Blocks, mv.Doc.Blocks) {
			// JS's copy of this very revision draws as its source: the
			// plugins leave it as it is now, and the row draws its source,
			// so its next update draws at once
			r.viewed, r.vmsg = false, nil
			r.recompose()
			return
		}
		if sameDrawn(r.vmsg, &mv) {
			return
		}
		r.vmsg = &mv
		r.recompose()
		return
	}
	if sameView(&r.src, &mv) && r.src.Text == mv.Text {
		if ready {
			r.viewWait = false
		}
		return
	}
	if r.viewWait && !ready {
		// JS's copy may be from before the row's update (asked again, or a
		// change to it still queued for JS): the row draws its new source
		// until JS's copy of it comes, on every read, not the revision
		// before. On the read after the update only, a first reaction
		// showed, went, and came back; a vote's count went N, N-1, N.
		// Unless the plugins rewrite text (one rewrote a message's text:
		// taut TextRewrites): an edit may bring in what they rewrite (a word
		// Censorship masks) to a message they left alone, which Slack's
		// patched selector draws already rewritten in the render after the
		// edit. Its row draws JS's copy of the revision before, the text it
		// drew, until JS's copy of the edit comes or EditHold passes. A
		// patch that rewrites no text (ShowRealUser's sender) holds
		// nothing: the edit draws in the next frame
		if changed && t.TextRewrites() {
			r.viewed, r.vmsg, r.heldTil = true, &mv, now.Add(taut.EditHold)
			r.recompose()
		}
		return
	}
	r.viewWait = false
	r.viewed, r.vmsg = true, &mv
	r.recompose()
}

// sameDrawn reports whether two conversions of a message draw the same:
// all of them but the refs a conversion read.
func sameDrawn(a, b *core.Message) bool {
	x, y := *a, *b
	x.Refs, y.Refs = core.MsgRefs{}, core.MsgRefs{}
	return reflect.DeepEqual(x, y)
}

// clearViews drops patched views once no plugin patches messages.
func (s *listState) clearViews() {
	for _, r := range s.rows {
		if r.viewed {
			r.viewed, r.vmsg = false, nil
			r.recompose()
		}
	}
}

// injected merges messages plugins inject (messages.injectMessages) into
// the snapshot, re-checking at most every viewRefresh.
type injState struct {
	at    time.Time
	epoch uint64 // taut's ViewEpoch at the last check
	keys  string
	msgs  []core.Message
	revs  map[string]uint32 // each message's rev (refreshInjected)
	n     uint32
}

// rev is injected message ts's rev.
func (in *injState) rev(ts string) uint32 {
	if r, ok := in.revs[ts]; ok {
		return r
	}
	return injRevBase
}

func (s *listState) refreshInjected(t *taut.Taut, now time.Time) (changed bool) {
	ep := t.ViewEpoch()
	if now.Sub(s.inj.at) < viewRefresh && s.inj.epoch == ep {
		return false
	}
	s.inj.at, s.inj.epoch = now, ep
	all := t.InjectedMessages(s.ch)
	msgs := all[:0:0]
	for i := range all {
		if s.injVisible(&all[i]) {
			msgs = append(msgs, all[i])
		}
	}
	return s.inj.take(msgs)
}

// take makes msgs the injected messages, and reports whether any of them
// changed (one came, went, or was converted again with something new).
func (in *injState) take(msgs []core.Message) (changed bool) {
	keys := ""
	for i := range msgs {
		keys += msgs[i].TS + ","
	}
	if keys == in.keys && sameInjected(in.msgs, msgs) {
		// the same messages; keep the conversions taut hands back now
		// (and every rev), so the next check is by identity again and not
		// a deep compare
		in.msgs = msgs
		return false
	}
	// each message's rev: a new one for a message that changed (converted
	// again for a name in it, edited by the plugin), so its row draws it
	old := make(map[string]int, len(in.msgs))
	for i := range in.msgs {
		old[in.msgs[i].TS] = i
	}
	revs := make(map[string]uint32, len(msgs))
	for i := range msgs {
		if j, ok := old[msgs[i].TS]; ok && sameInjectedMsg(&in.msgs[j], &msgs[i]) {
			revs[msgs[i].TS] = in.revs[msgs[i].TS]
			continue
		}
		in.n++
		revs[msgs[i].TS] = injRevBase + in.n
	}
	in.keys, in.msgs, in.revs = keys, msgs, revs
	return true
}

// injRevBase starts the injected messages' revs, far from the models'
// (which count from 1): a message core gets later is another revision.
const injRevBase = 1 << 31

// sameInjected reports whether the lists hold the same messages as they
// were converted (same ts, in order).
func sameInjected(a, b []core.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameInjectedMsg(&a[i], &b[i]) {
			return false
		}
	}
	return true
}

// sameInjectedMsg reports whether b is a's message unchanged: the same
// conversion (taut hands back the cached one: same Doc), or one equal to
// it. InjectedMessages converts again when what the message read changed.
func sameInjectedMsg(a, b *core.Message) bool {
	if a.TS != b.TS {
		return false
	}
	if len(a.Doc.Blocks) > 0 && len(b.Doc.Blocks) > 0 && &a.Doc.Blocks[0] == &b.Doc.Blocks[0] && a.Text == b.Text {
		return true
	}
	return reflect.DeepEqual(a, b)
}

// injVisible reports whether an injected message belongs in this list, by
// the same rule as Slack's channelHistory (inHistory): the channel shows
// top-level messages and broadcasts, a thread its own replies. A deleted
// reply a logger keeps stays in its thread.
func (s *listState) injVisible(m *core.Message) bool {
	if s.thread {
		return m.ThreadTS == s.ts && m.TS != s.ts
	}
	return m.ThreadTS == "" || m.ThreadTS == m.TS || m.Broadcast
}

// mergeInjected inserts the injected messages into s.snap.msgs in ts order,
// skipping any the snapshot already has (same ts or client_msg_id).
func (s *listState) mergeInjected() {
	if len(s.inj.msgs) == 0 {
		return
	}
	have := make(map[string]bool, len(s.snap.msgs))
	for i := range s.snap.msgs {
		m := &s.snap.msgs[i].m
		have[m.TS] = true
		if m.ClientMsgID != "" {
			have["c:"+m.ClientMsgID] = true
		}
	}
	inj := make([]core.Message, 0, len(s.inj.msgs))
	for _, m := range s.inj.msgs {
		if have[m.TS] || m.ClientMsgID != "" && have["c:"+m.ClientMsgID] {
			continue
		}
		inj = append(inj, m)
	}
	if len(inj) == 0 {
		return
	}
	out := make([]entry, 0, len(s.snap.msgs)+len(inj))
	j := 0
	for i := range s.snap.msgs {
		for j < len(inj) && tsLess(inj[j].TS, s.snap.msgs[i].m.TS) {
			out = append(out, entry{m: inj[j], rev: s.inj.rev(inj[j].TS)})
			j++
		}
		out = append(out, s.snap.msgs[i])
	}
	for ; j < len(inj); j++ {
		out = append(out, entry{m: inj[j], rev: s.inj.rev(inj[j].TS)})
	}
	s.snap.msgs = out
}

// ---- actionable message (edit) ----

// pendingEdit is an edit waiting for its actionable message from JS.
type pendingEdit struct {
	mu sync.Mutex
	m  *core.Message
}

func (p *pendingEdit) put(m core.Message) {
	p.mu.Lock()
	p.m = &m
	p.mu.Unlock()
}

func (p *pendingEdit) take() *core.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := p.m
	p.m = nil
	return m
}
