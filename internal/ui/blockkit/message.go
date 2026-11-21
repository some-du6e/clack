package blockkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// maxVisible is how many elements of an actions block a message shows
// before "+ N more", and truncateAt how many it takes to truncate: Slack's
// Actions block shows every element up to six and, from seven, the first
// five and "+ {len-5} more".
const (
	maxVisible = 5
	truncateAt = 7
)

// attachment is a legacy attachment's interactive parts.
type attachment struct {
	ID         int
	CallbackID string
	Blocks     []Block
	Actions    []legacyAction
}

// legacyAction is an attachment action (button or menu) as sent.
type legacyAction struct {
	ID         json.RawMessage `json:"id"`
	Name       string          `json:"name"`
	Text       string          `json:"text"`
	Type       string          `json:"type"`
	Value      string          `json:"value"`
	Style      string          `json:"style"`
	URL        string          `json:"url"`
	DataSource string          `json:"data_source"`
	Options    []struct {
		Text  string `json:"text"`
		Value string `json:"value"`
	} `json:"options"`
	SelectedOptions []struct {
		Text  string `json:"text"`
		Value string `json:"value"`
	} `json:"selected_options"`
	Confirm *struct {
		Title       string `json:"title"`
		Text        string `json:"text"`
		OkText      string `json:"ok_text"`
		DismissText string `json:"dismiss_text"`
	} `json:"confirm"`
	Raw json.RawMessage `json:"-"`
}

func (a *legacyAction) UnmarshalJSON(b []byte) error {
	type plain legacyAction
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*a = legacyAction(p)
	a.Raw = append(json.RawMessage(nil), b...)
	return nil
}

// Message is the live element host of one message body
// (richview.Elements): it maps richview's buttons back to the message's
// Block Kit elements and draws them.
type Message struct {
	h Host
	m core.Message
	// inst is the drawing instance a call is for (InView: a row, a view's
	// list; 0, the shared default), whose agent block state is its own
	inst   uint64
	sig    []byte
	blocks []Block
	atts   []attachment
	vals   Values
	ws     map[string]*ew
	legacy map[*ew]*legacyRef
	more   map[string]bool   // actions block expanded
	errs   map[string]string // block id (or "att:N") → the failed action's server message ("" for none)
	busy   map[string]int    // block id (or "att:N") → actions in flight
	errTap map[string]*tap   // the warning icon (hover shows the message)
	tips   map[string]*popups.TipState
	// statusAt is where block bid's status icons were drawn in its slot;
	// runAt where its elements were (RunRects).
	statusAt map[string]image.Point
	runAt    map[string][]image.Rectangle
	// gen counts the message's block revisions: an action sent before the
	// blocks changed settles into nothing (useBlockKitInteractiveState's
	// resetKey generation).
	gen uint32
	ver atomic.Uint32
	// acu is the agent_channel_unfurl props by channel (agentChannel)
	acu map[string]popups.EntityProps
	gtx layout.Context
	ops op.Ops
	// used is when the host was last looked up or drawn, drawn the frame
	// (gtx.Now) it was last drawn at, both unix ns: the registry lets go
	// of the one used longest ago, never one drawn in the last frame
	used, drawn atomic.Int64
}

type legacyRef struct {
	att *attachment
	act *legacyAction
}

var (
	msgsMu sync.Mutex
	msgs   = map[string]*Message{}
	// cur is each message's (channel/ts) host key looked up last: its
	// current blocks
	cur = map[string]string{}
	// frameNow and framePrev are the latest two frames (gtx.Now, unix ns)
	// a host was drawn in
	frameNow, framePrev atomic.Int64
)

// maxHosts bounds the registry.
const maxHosts = 400

// noteFrame records that a host is drawn in the frame at now.
func noteFrame(now int64) {
	for {
		n := frameNow.Load()
		if now <= n {
			return
		}
		if frameNow.CompareAndSwap(n, now) {
			framePrev.Store(n)
			return
		}
	}
}

// drawnLately reports whether me was drawn in either of the latest two
// frames a host was drawn in.
func (me *Message) drawnLately() bool {
	d := me.drawn.Load()
	return d != 0 && d >= framePrev.Load()
}

// Interactive reports whether m has elements blockkit makes live.
func Interactive(m *core.Message) bool {
	return bytes.Contains(m.Blocks, []byte(`"action_id"`)) || bytes.Contains(m.Blocks, []byte(`"actions"`)) ||
		hasAgentBlocks(m.Blocks) || hasAgentBlocks(m.Attachments) || // agent.go
		bytes.Contains(m.Attachments, []byte(`"actions"`)) || bytes.Contains(m.Attachments, []byte(`"action_id"`))
}

// MessageElements returns the element host for m, or nil when m has
// nothing interactive: one per message and blocks, kept across rows (a
// row drawing the same blocks finds the same host, its widgets and their
// state). Keyed by the message alone, two rows drawing two revisions of
// it (the thread pane's root a vote behind the channel row, one drawing
// the plugins' view and one its source) took turns resetting the one host
// to their own blocks, and the other row's buttons found no element: one
// pane's buttons went at each vote. The blocks are compared as JSON
// values, not bytes: JS's copy of a message (a view) re-encodes the same
// blocks as Slack sent them. A message's earlier blocks back (current
// again after another revision, not drawn in the last frame) start over,
// as Slack's resetKey does on each change of blocks; the registry lets go
// of the host used longest ago, never one drawn in the last frame.
func MessageElements(h Host, m *core.Message) *Message {
	if m == nil || m.Pending || !Interactive(m) {
		return nil
	}
	sig := blocksSig(m)
	base := m.Channel + "/" + m.TS
	k := base + "/" + string(sigHash(sig))
	msgsMu.Lock()
	defer msgsMu.Unlock()
	me := msgs[k]
	switch {
	case me == nil:
		me = &Message{h: h}
		msgs[k] = me
		if len(msgs) > maxHosts {
			evictHostLocked(k)
		}
		// a message's older blocks' hosts: the rows still drawing them
		// keep theirs; the registry keeps the last few
		if ks := append(revs[base], k); len(ks) > maxRevHosts {
			if old := msgs[ks[0]]; old == nil || !old.drawnLately() {
				delete(msgs, ks[0])
			}
			revs[base] = ks[1:]
		} else {
			revs[base] = ks
		}
		if len(revs) > 800 {
			clear(revs) // bounded: the hosts stay in msgs
		}
	case me.h != h:
		// another host's (tests run one app after another): this host's
		// actions must go through its own core
		me = &Message{h: h}
		msgs[k] = me
	case cur[base] != "" && cur[base] != k && !me.drawnLately():
		// the message's earlier blocks back (the app replaced the message,
		// then put them back): Slack's resetKey moved twice, so their
		// state starts over (idle, the initial values) rather than come
		// back with the error, spinner and picks they held then. A host
		// drawn in the last frame is a row's still (a pane a revision
		// behind): it keeps its state
		me.sig = nil
	}
	if cur[base] != k {
		if len(cur) > 2*maxHosts {
			clear(cur) // bounded: what's drawn looks up again on its next change
		}
		cur[base] = k
	}
	me.used.Store(time.Now().UnixNano())
	me.setMessage(m, sig)
	return me
}

// evictHostLocked lets go of the host used longest ago (looked up or
// drawn), never one drawn in the last frame, nor keep (msgsMu held).
// Evicted by creation order, the host a row drew frame after frame went
// as other messages made theirs (a busy vote message one per vote); the
// next lookup made another, and its state went with it.
func evictHostLocked(keep string) {
	var oldest string
	var at int64
	for k, me := range msgs {
		if k == keep || me.drawnLately() {
			continue
		}
		u := max(me.used.Load(), me.drawn.Load())
		if oldest == "" || u < at {
			oldest, at = k, u
		}
	}
	if oldest != "" {
		delete(msgs, oldest)
	}
}

// maxRevHosts is how many of a message's blocks' hosts the registry keeps.
const maxRevHosts = 3

// revs are each message's hosts' keys in the registry, oldest first.
var revs = map[string][]string{}

// blocksSig is m's blocks and attachments as canonical JSON (keys
// sorted, as Go encodes maps): what an element host is made of, the same
// for Slack's bytes and JS's encoding of them. Raw bytes where they
// don't parse.
func blocksSig(m *core.Message) []byte {
	canon := func(b []byte) []byte {
		if len(b) == 0 {
			return nil
		}
		var v any
		if json.Unmarshal(b, &v) != nil {
			return b
		}
		out, err := json.Marshal(v)
		if err != nil {
			return b
		}
		return out
	}
	out := append([]byte(nil), canon(m.Blocks)...) // never m's own bytes, which append could write past
	return append(append(out, 0), canon(m.Attachments)...)
}

// sigHash is FNV-1a over sig, as a registry key part.
func sigHash(sig []byte) []byte {
	h := uint64(14695981039346656037)
	for _, c := range sig {
		h ^= uint64(c)
		h *= 1099511628211
	}
	const hex = "0123456789abcdef"
	out := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		out[i] = hex[h&15]
		h >>= 4
	}
	return out
}

func (me *Message) setMessage(m *core.Message, sig []byte) {
	me.m = *m
	if bytes.Equal(sig, me.sig) {
		return
	}
	// Slack resets an element's state when the message's blocks change
	// (the app replaced the message): useBlockKitInteractiveState's
	// resetKey is msg.blocks, and its effect clears the loading and error
	// sets and bumps the generation an answer must match.
	me.sig = sig
	me.gen++
	me.blocks = ParseBlocks(m.Blocks)
	me.atts = parseAttachments(m.Attachments)
	me.vals = Values{}
	initValues(me.vals, me.blocks)
	for i := range me.atts {
		initValues(me.vals, me.atts[i].Blocks)
	}
	me.ws = map[string]*ew{}
	me.legacy = map[*ew]*legacyRef{}
	me.more = map[string]bool{}
	me.errs = map[string]string{}
	me.busy = map[string]int{}
	me.errTap = map[string]*tap{}
	me.tips = map[string]*popups.TipState{}
	me.statusAt = map[string]image.Point{}
	me.runAt = map[string][]image.Rectangle{}
	me.ver.Add(1)
}

// RunRects are where block bid's elements (its buttons, "+ N more") were
// last drawn, in their run's slot: an Activity row's own click skips the
// clicks they take (ControlledClick ignores a BUTTON target).
func (me *Message) RunRects(bid string) []image.Rectangle {
	return append([]image.Rectangle(nil), me.runAt[bid]...)
}

// Probe is for tests: the host's block generation (it moves whenever
// setMessage takes other blocks) and block bid's first element widget,
// whose identity changes when the host rebuilds its widgets, with its
// click gesture's press and hover state.
func (me *Message) Probe(bid string) (gen uint32, w any, pressed, hovered bool) {
	for _, x := range me.ws {
		if x.bid == bid {
			return me.gen, x, x.main.g.Pressed(), x.main.g.Hovered()
		}
	}
	return me.gen, nil, false, false
}

func parseAttachments(raw json.RawMessage) []attachment {
	if len(raw) == 0 {
		return nil
	}
	var as []struct {
		ID         int             `json:"id"`
		CallbackID string          `json:"callback_id"`
		Blocks     json.RawMessage `json:"blocks"`
		Actions    []legacyAction  `json:"actions"`
	}
	if json.Unmarshal(raw, &as) != nil {
		return nil
	}
	out := make([]attachment, len(as))
	for i, a := range as {
		out[i] = attachment{ID: a.ID, CallbackID: a.CallbackID, Blocks: ParseBlocks(a.Blocks), Actions: a.Actions}
		if out[i].ID == 0 {
			out[i].ID = i + 1
		}
	}
	return out
}

// ---- owner ----

func (me *Message) host() Host     { return me.h }
func (me *Message) values() Values { return me.vals }
func (me *Message) channel() string {
	return me.m.Channel
}
func (me *Message) modal() bool { return false }

// loading reports an action of block bid in flight.
func (me *Message) loading(bid string) bool {
	if me.busy[bid] > 0 {
		return true
	}
	for w, lr := range me.legacy {
		if w.bid == bid && me.busy["att:"+itoa(lr.att.ID)] > 0 {
			return true
		}
	}
	return false
}

// Status reports block bid's action state: in flight (the spinner, its
// elements disabled) and failed (the warning icon).
func (me *Message) Status(bid string) (loading, failed bool) {
	_, failed = me.errs[bid]
	return me.loading(bid), failed
}

// begin marks block key loading and clears its error; the returned func
// settles it (Slack's useBlockKitInteractiveState). An answer that comes
// after the message's blocks changed (the app's chat.update reached the
// client first, as message_changed) changes nothing: the new blocks start
// idle, as Slack's handlers drop a result whose generation is past.
func (me *Message) begin(key string) func(err error) {
	delete(me.errs, key)
	me.busy[key]++
	me.changed()
	gen := me.gen
	return func(err error) {
		if me.gen != gen {
			return
		}
		if me.busy[key]--; me.busy[key] <= 0 {
			delete(me.busy, key)
		}
		if err != nil {
			me.errs[key] = serverMessage(err)
		}
		me.changed()
	}
}
func (me *Message) changed() { me.ver.Add(1); me.h.Invalidate() }
func (me *Message) base() core.BlockAction {
	return me.h.Core().Interactive().MessageContainer(&me.m, 0)
}

// attOf returns the attachment holding block id bid (nil: the message).
func (me *Message) attOf(bid string) *attachment {
	for i := range me.atts {
		for j := range me.atts[i].Blocks {
			if me.atts[i].Blocks[j].BlockID == bid {
				return &me.atts[i]
			}
		}
	}
	return nil
}

func (me *Message) fire(w *ew, action json.RawMessage, done func(error)) {
	log.Printf("blockkit: %s action in %s/%s block=%s legacy=%v", w.e.Type, me.m.Channel, me.m.TS, w.bid, me.legacy[w] != nil)
	ix := me.h.Core().Interactive()
	key := w.bid
	if lr := me.legacy[w]; lr != nil {
		key = "att:" + itoa(lr.att.ID)
		settle := me.begin(key)
		me.sendLegacy(w, lr, func(err error) { settle(err); done(err) })
		return
	}
	settle := me.begin(key)
	attID := 0
	if a := me.attOf(w.bid); a != nil {
		attID = a.ID
	}
	b := ix.MessageContainer(&me.m, attID)
	b.Action = action
	b.State = me.vals.JSON(true)
	sendAction(me.h, b, func(err error) {
		settle(err)
		done(err)
	})
}

// sendLegacy posts a legacy attachment action (chat.attachmentAction).
func (me *Message) sendLegacy(w *ew, lr *legacyRef, done func(error)) {
	ix := me.h.Core().Interactive()
	act := lr.act.Raw
	if lr.act.Type == "select" {
		v := w.value()
		var m map[string]any
		_ = json.Unmarshal(lr.act.Raw, &m)
		if m == nil {
			m = map[string]any{}
		}
		sel := []map[string]string{}
		if v != nil && v.SelectedOption != nil {
			sel = append(sel, map[string]string{"value": v.SelectedOption.Value})
		}
		m["selected_options"] = sel
		act, _ = json.Marshal(m)
	}
	bc := ix.MessageContainer(&me.m, lr.att.ID)
	x := core.AttachmentAction{ServiceID: bc.ServiceID, AppID: bc.AppID, Action: act, AttachmentID: lr.att.ID,
		CallbackID: lr.att.CallbackID, ChannelID: me.m.Channel, MessageTS: me.m.TS, IsEphemeral: bc.Container.IsEphemeral,
		ThreadTS: me.m.ThreadTS}
	if me.m.BotProfile != nil {
		x.BotUserID = me.m.BotProfile.UserID
	}
	run(me.h, 15*time.Second, func(ctx context.Context) (string, error) {
		return ix.AttachmentActionSend(ctx, x)
	}, func(_ string, err error) {
		done(err)
	})
}

// ---- mapping buttons to elements ----

// widgetFor resolves a richview button to its element widget.
func (me *Message) widgetFor(bt *richtext.Button) *ew {
	if bt.BlockID == "" {
		return me.legacyWidget(bt)
	}
	k := bt.BlockID + "\x00" + bt.ActionID
	if w := me.ws[k]; w != nil {
		return w
	}
	if r, ok := find(me.blocks, bt.BlockID, bt.ActionID); ok {
		w := newEW(me, r.block, r.el, r.block.Type == "input")
		me.ws[k] = w
		return w
	}
	for i := range me.atts {
		if r, ok := find(me.atts[i].Blocks, bt.BlockID, bt.ActionID); ok {
			w := newEW(me, r.block, r.el, r.block.Type == "input")
			me.ws[k] = w
			return w
		}
	}
	return nil
}

// legacyWidget wraps a legacy attachment action as an element widget.
func (me *Message) legacyWidget(bt *richtext.Button) *ew {
	k := "legacy\x00" + bt.ActionID + "\x00" + bt.Value + "\x00" + bt.Text
	if w := me.ws[k]; w != nil {
		return w
	}
	for i := range me.atts {
		a := &me.atts[i]
		for j := range a.Actions {
			la := &a.Actions[j]
			id := string(bytes.Trim(la.ID, `"`))
			if (la.Name != bt.ActionID && id != bt.ActionID) || la.Value != bt.Value {
				continue
			}
			e := &Element{Type: "button", ActionID: firstNonEmpty(la.Name, id), Text: &Text{Type: "plain_text", Text: la.Text},
				Value: la.Value, URL: la.URL, Style: la.Style}
			if la.Type == "select" {
				e.Type = "static_select"
				e.Placeholder = &Text{Type: "plain_text", Text: la.Text}
				for _, o := range la.Options {
					e.Options = append(e.Options, Option{Text: Text{Type: "plain_text", Text: o.Text}, Value: o.Value})
				}
				if len(la.SelectedOptions) > 0 {
					o := la.SelectedOptions[0]
					e.InitialOption = &Option{Text: Text{Type: "plain_text", Text: o.Text}, Value: o.Value}
				}
				if la.DataSource != "" && la.DataSource != "static" {
					switch la.DataSource {
					case "users":
						e.Type = "users_select"
					case "channels":
						e.Type = "channels_select"
					case "conversations":
						e.Type = "conversations_select"
					}
				}
			}
			if c := la.Confirm; c != nil {
				e.Confirm = &Confirm{Title: &Text{Type: "plain_text", Text: c.Title}, Text: &Text{Type: "mrkdwn", Text: c.Text},
					Confirm: &Text{Type: "plain_text", Text: c.OkText}, Deny: &Text{Type: "plain_text", Text: c.DismissText}}
			}
			blk := &Block{Type: "actions", BlockID: "att:" + itoa(a.ID)}
			w := newEW(me, blk, e, false)
			me.ws[k] = w
			me.legacy[w] = &legacyRef{att: a, act: la}
			return w
		}
	}
	return nil
}

// ---- richview.Elements ----

func (me *Message) ElementsVersion() uint32 { return me.ver.Load() }

func (me *Message) scratch(m unit.Metric, maxW int) layout.Context {
	me.ops.Reset()
	me.gtx = layout.Context{Ops: &me.ops, Metric: m, Now: time.Now(), Constraints: layout.Constraints{Max: image.Pt(maxW, 1<<20)}}
	return me.gtx
}

func (me *Message) ElementsSize(bs []richtext.Button, maxW int, accessory bool, m unit.Metric) (image.Point, bool) {
	gtx := me.scratch(m, maxW)
	if len(bs) == 1 && isAgentKind(bs[0].Kind) { // agent.go
		return me.agentRun(gtx, &bs[0], maxW), false
	}
	if accessory && len(bs) == 1 {
		w := me.widgetFor(&bs[0])
		if w == nil {
			return image.Point{}, false
		}
		if stacks(w.e) && !w.dialogMulti() {
			return image.Pt(maxW, w.size(gtx, maxW, true).Y+me.statusRowH(gtx, w.bid)), true
		}
		sz := w.size(gtx, maxW/2, true)
		if iw := me.statusW(gtx, w.bid, maxW); iw > 0 {
			sz.X += iw
		}
		return sz, false
	}
	return me.run(gtx, bs, maxW, false), false
}

func (me *Message) LayoutElements(gtx layout.Context, bs []richtext.Button, accessory bool) layout.Dimensions {
	drain()
	if now := gtx.Now.UnixNano(); !gtx.Now.IsZero() {
		noteFrame(now)
		me.drawn.Store(now)
	}
	sz := gtx.Constraints.Max
	if len(bs) == 1 && isAgentKind(bs[0].Kind) { // agent.go
		me.agentRun(gtx, &bs[0], sz.X)
		return D{Size: sz}
	}
	if accessory && len(bs) == 1 {
		w := me.widgetFor(&bs[0])
		if w == nil {
			return D{Size: sz}
		}
		esz := sz
		if stacks(w.e) && !w.dialogMulti() {
			// checkboxes / radios: the status icons go under the options
			esz.Y -= me.statusRowH(gtx, w.bid)
			w.slot = image.Point{}
			w.layout(fixed(gtx, esz), esz)
			at(gtx, image.Pt(0, esz.Y+dp(gtx, 4)), func(gtx C) D { me.status(gtx, w.bid, sz.X, dp(gtx, 20)); return D{} })
			return D{Size: sz}
		}
		// a section accessory: the icons sit before the element
		if iw := me.statusW(gtx, w.bid, sz.X); iw > 0 {
			esz.X -= iw
			me.status(gtx, w.bid, iw, esz.Y)
			w.slot = image.Pt(iw, 0)
			at(gtx, image.Pt(iw, 0), func(gtx C) D { return w.layout(fixed(gtx, esz), esz) })
			return D{Size: sz}
		}
		w.slot = image.Point{}
		w.layout(fixed(gtx, esz), esz)
		return D{Size: sz}
	}
	me.run(gtx, bs, sz.X, true)
	return D{Size: sz}
}

// statusW is the width of block bid's status icons (spinner, warning
// with a known error's text) plus their 8px gap; 0 when idle.
func (me *Message) statusW(gtx C, bid string, maxW int) int {
	w := 0
	if me.busy[bid] > 0 {
		w += dp(gtx, 20)
	}
	if msg, ok := me.errs[bid]; ok {
		if w > 0 {
			w += dp(gtx, 4)
		}
		w += dp(gtx, 16)
		if t := knownActionError(msg); t != "" {
			w += dp(gtx, 4) + measure(gtx, me.h.Theme(), 13, theme.Regular, t)
		}
	}
	if w == 0 {
		return 0
	}
	return min(w+dp(gtx, 8), maxW)
}

func (me *Message) statusRowH(gtx C, bid string) int {
	if me.statusW(gtx, bid, 1<<20) > 0 {
		return dp(gtx, 24)
	}
	return 0
}

// status draws block bid's icons in a w×h slot (vertically centred):
// Slack's p-block-kit-element__loading spinner while its action is in
// flight, and after a failure the p-block_kit_element__error warning,
// whose tooltip is the server's message (none without one) and whose
// text is shown only for the errors Slack knows (action_disabled, …).
func (me *Message) status(gtx C, bid string, w, h int) {
	th := me.h.Theme()
	cl := colorsOf(th)
	x := 0
	if me.busy[bid] > 0 {
		s := dp(gtx, 20)
		at(gtx, image.Pt(x, (h-s)/2), func(gtx C) D { spinner(gtx, s, cl.Text2); return D{} })
		x += s + dp(gtx, 4)
	}
	msg, ok := me.errs[bid]
	if !ok {
		return
	}
	is := dp(gtx, 16)
	t := me.errTap[bid]
	if t == nil {
		t = &tap{}
		me.errTap[bid] = t
	}
	tip := me.tips[bid]
	if tip == nil {
		tip = &popups.TipState{MaxWidth: 200}
		me.tips[bid] = tip
	}
	at(gtx, image.Pt(x, (h-is)/2), func(gtx C) D {
		icon(gtx, "warning", 16, cl.ErrorText)
		t.clicked(gtx, me.h)
		t.add(gtx, image.Pt(is, is))
		if msg != "" {
			popups.Tooltip(gtx, th, tip, t.g.Hovered(), image.Pt(is, is), popups.Above, firstNonEmpty(knownActionError(msg), msg), "")
		}
		return D{}
	})
	if kt := knownActionError(msg); kt != "" {
		at(gtx, image.Pt(x+is+dp(gtx, 4), 0), func(gtx C) D {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(0, w-x-is-dp(gtx, 4)), h)}
			return vcenter(gtx, h, func(gtx C) D { return label(gtx, th, 13, theme.Regular, cl.ErrorText, kt) })
		})
	}
}

// knownActionError is getKnownActionErrorMessage: the text Slack shows
// beside the warning for the error messages it knows.
func knownActionError(msg string) string {
	switch msg {
	case "workflow_shutdown":
		return "Legacy workflows are no longer available."
	case "seamless_user_not_mapped":
		return "You don’t have access to customer management"
	case "action_disabled":
		return "This action isn’t available"
	}
	return ""
}

// serverMessage is what an action error leaves for the tooltip: the
// response's message field (data.message in Slack's client).
func serverMessage(err error) string {
	var ae *core.ActionError
	if errors.As(err, &ae) {
		return ae.Server
	}
	return ""
}

// run lays out (and with draw, paints) an actions block / attachment
// buttons: wrapping rows 8px apart, the first five then "+ N more", and
// the error line.
func (me *Message) run(gtx C, bs []richtext.Button, maxW int, draw bool) image.Point {
	th := me.h.Theme()
	var ws []*ew
	for i := range bs {
		if w := me.widgetFor(&bs[i]); w != nil {
			ws = append(ws, w)
		}
	}
	if len(ws) == 0 {
		return image.Point{}
	}
	bid := ws[0].bid
	if lr := me.legacy[ws[0]]; lr != nil {
		bid = "att:" + itoa(lr.att.ID)
	}
	n := len(ws)
	expanded := me.more[bid]
	hidden := 0
	if n >= truncateAt && !expanded {
		hidden = n - maxVisible
		n = maxVisible
	}
	gap := dp(gtx, 8)
	x, y, rowH, wMax := 0, 0, 0, 0
	if draw {
		me.runAt[bid] = me.runAt[bid][:0]
	}
	place := func(sz image.Point, f func(gtx C)) {
		if x > 0 && x+sz.X > maxW {
			x = 0
			y += rowH + gap
			rowH = 0
		}
		if draw {
			at(gtx, image.Pt(x, y), func(gtx C) D { f(fixed(gtx, sz)); return D{} })
		}
		x += sz.X + gap
		wMax = max(wMax, x-gap)
		rowH = max(rowH, sz.Y)
	}
	button := func(sz image.Point) {
		if draw {
			me.runAt[bid] = append(me.runAt[bid], image.Rectangle{Min: image.Pt(x, y), Max: image.Pt(x+sz.X, y+sz.Y)})
		}
	}
	for _, w := range ws[:n] {
		w := w
		sz := w.size(gtx, maxW, false)
		if sz.X == 0 {
			continue
		}
		place(sz, func(gtx C) { w.slot = image.Pt(x, y); button(sz); w.layout(gtx, sz) })
	}
	if hidden > 0 {
		txt := "+ " + itoa(hidden) + " more"
		sz := image.Pt(measure(gtx, th, 13, theme.Bold, txt)+2*dp(gtx, 12), dp(gtx, 28))
		mw := ws[0]
		place(sz, func(gtx C) {
			mw.moreAt = image.Pt(x, y)
			button(sz)
			if mw.more.clicked(gtx, me.h) {
				me.more[bid] = true
				me.changed()
			}
			drawButton(gtx, th, sz, "", txt, 4, 13, mw.more.g.Hovered(), false, false)
			mw.more.add(gtx, sz)
		})
	}
	// the block's status icons follow its elements (p-action_block__icons)
	if iw := me.statusW(gtx, bid, maxW); iw > 0 {
		lh := rowH
		place(image.Pt(iw-gap, max(lh, dp(gtx, 28))), func(gtx C) {
			me.statusAt[bid] = image.Pt(x, y)
			me.status(gtx, bid, iw-gap, max(lh, dp(gtx, 28)))
		})
	}
	h := y + rowH
	return image.Pt(max(wMax, 1), h)
}

var _ richview.Elements = (*Message)(nil)
