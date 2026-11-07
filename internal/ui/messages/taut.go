package messages

import (
	"context"
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
	if r.viewed && sameMessage(&r.m, &mv) && r.m.Text == mv.Text {
		return
	}
	if !r.viewed && sameMessage(&r.src, &mv) && r.src.Text == mv.Text {
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
	if s.thread || now.Sub(s.inj.at) < viewRefresh {
		return false
	}
	s.inj.at = now
	msgs := t.InjectedMessages(s.ch)
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

// mergeInjected inserts the injected messages into s.snap.msgs in ts order.
func (s *listState) mergeInjected() {
	if len(s.inj.msgs) == 0 {
		return
	}
	out := make([]entry, 0, len(s.snap.msgs)+len(s.inj.msgs))
	j := 0
	for i := range s.snap.msgs {
		for j < len(s.inj.msgs) && tsLess(s.inj.msgs[j].TS, s.snap.msgs[i].m.TS) {
			out = append(out, entry{m: s.inj.msgs[j], rev: 1})
			j++
		}
		out = append(out, s.snap.msgs[i])
	}
	for ; j < len(s.inj.msgs); j++ {
		out = append(out, entry{m: s.inj.msgs[j], rev: 1})
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
