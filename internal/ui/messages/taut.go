package messages

import (
	"context"
	"encoding/json"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
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

// commitEditArgs is Slack's commitMessageEdit {channelId, ts, blocks, text}.
type commitEditArgs struct {
	ChannelID string          `json:"channelId"`
	TS        string          `json:"ts"`
	Blocks    json.RawMessage `json:"blocks,omitempty"`
	Text      string          `json:"text,omitempty"`
}

// commitEdit saves an inline edit (composer.SaveEdit) through Slack's
// commitMessageEdit; the native applies the plugins' send transforms like
// saveMessageEdit and calls core.Edit.
func commitEdit(ctx context.Context, app *core.App, ch, ts, text string, blocks json.RawMessage) error {
	t := tautOf()
	_, err := taut.Dispatch(ctx, t, "commitMessageEdit", commitEditArgs{ChannelID: ch, TS: ts, Blocks: blocks, Text: text},
		func(ctx context.Context, a commitEditArgs) (any, error) {
			b := a.Blocks
			if t != nil && len(b) > 0 && t.HasSendTransforms() {
				b = t.TransformSendBlocks(ctx, b)
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
	if now.Sub(r.viewAt) < viewRefresh && r.viewRev == r.rev {
		return
	}
	r.viewAt, r.viewRev = now, r.rev
	mv, ok := t.MessageView(r.src.Channel, r.src.TS)
	if !ok {
		if r.viewed {
			r.viewed = false
			r.m = r.src
			r.setDoc()
		}
		return
	}
	// keep what only the native knows (pending state, resolved author)
	mv.Pending, mv.Failed, mv.ClientMsgID = r.src.Pending, r.src.Failed, r.src.ClientMsgID
	if r.viewed && sameView(&r.m, &mv) && r.m.Text == mv.Text {
		return
	}
	if !r.viewed && sameView(&r.src, &mv) && r.src.Text == mv.Text {
		return
	}
	r.viewed = true
	r.m = mv
	r.msg = nil
	r.setDoc()
}

// clearViews drops patched views once no plugin patches messages.
func (s *listState) clearViews() {
	for _, r := range s.rows {
		if r.viewed {
			r.viewed = false
			r.m = r.src
			r.setDoc()
		}
	}
}

// injected merges messages plugins inject (messages.injectMessages) into
// the snapshot, re-checking at most every viewRefresh.
type injState struct {
	at   time.Time
	keys string
	msgs []core.Message
}

func (s *listState) refreshInjected(t *taut.Taut, now time.Time) (changed bool) {
	if now.Sub(s.inj.at) < viewRefresh {
		return false
	}
	s.inj.at = now
	all := t.InjectedMessages(s.ch)
	msgs := all[:0:0]
	for i := range all {
		if s.injVisible(&all[i]) {
			msgs = append(msgs, all[i])
		}
	}
	keys := ""
	for i := range msgs {
		keys += msgs[i].TS + ","
	}
	if keys == s.inj.keys {
		return false
	}
	s.inj.keys, s.inj.msgs = keys, msgs
	return true
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
			out = append(out, entry{m: inj[j], rev: 1})
			j++
		}
		out = append(out, s.snap.msgs[i])
	}
	for ; j < len(inj); j++ {
		out = append(out, entry{m: inj[j], rev: 1})
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
