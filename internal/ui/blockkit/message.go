package blockkit

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// maxVisible is how many elements of an actions block a message shows
// before "+ N more" (Slack's renderer).
const maxVisible = 5

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
	h      Host
	m      core.Message
	sig    []byte
	blocks []Block
	atts   []attachment
	vals   Values
	ws     map[string]*ew
	legacy map[*ew]*legacyRef
	more   map[string]bool   // actions block expanded
	errs   map[string]string // block id (or "att:N") → action error
	ver    atomic.Uint32
	gtx    layout.Context
	ops    op.Ops
}

type legacyRef struct {
	att *attachment
	act *legacyAction
}

var (
	msgsMu sync.Mutex
	msgs   = map[string]*Message{}
	order  []string
)

// Interactive reports whether m has elements blockkit makes live.
func Interactive(m *core.Message) bool {
	return bytes.Contains(m.Blocks, []byte(`"action_id"`)) || bytes.Contains(m.Blocks, []byte(`"actions"`)) ||
		hasAgentBlocks(m.Blocks) || hasAgentBlocks(m.Attachments) || // agent.go
		bytes.Contains(m.Attachments, []byte(`"actions"`)) || bytes.Contains(m.Attachments, []byte(`"action_id"`))
}

// MessageElements returns the element host for m (one per message,
// kept across rows and refreshed when its blocks change), or nil when
// m has nothing interactive.
func MessageElements(h Host, m *core.Message) *Message {
	if m == nil || m.Pending || !Interactive(m) {
		return nil
	}
	k := m.Channel + "/" + m.TS
	msgsMu.Lock()
	defer msgsMu.Unlock()
	me := msgs[k]
	if me == nil {
		me = &Message{h: h}
		msgs[k] = me
		order = append(order, k)
		if len(order) > 400 {
			delete(msgs, order[0])
			order = order[1:]
		}
	}
	me.setMessage(m)
	return me
}

func (me *Message) setMessage(m *core.Message) {
	sig := append(append([]byte(nil), m.Blocks...), m.Attachments...)
	me.m = *m
	if bytes.Equal(sig, me.sig) {
		return
	}
	// Slack resets an element's state when the message's blocks change
	// (the app replaced the message).
	me.sig = sig
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
	me.ver.Add(1)
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
func (me *Message) changed()    { me.ver.Add(1); me.h.Invalidate() }
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
		delete(me.errs, key)
		me.sendLegacy(w, lr, done)
		return
	}
	delete(me.errs, key)
	attID := 0
	if a := me.attOf(w.bid); a != nil {
		attID = a.ID
	}
	b := ix.MessageContainer(&me.m, attID)
	b.Action = action
	b.State = me.vals.JSON(true)
	sendAction(me.h, b, func(err error) {
		if err != nil {
			me.errs[key] = errMessage(err)
		}
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
	key := "att:" + itoa(lr.att.ID)
	run(me.h, 15*time.Second, func(ctx context.Context) (string, error) {
		return ix.AttachmentActionSend(ctx, x)
	}, func(_ string, err error) {
		if err != nil {
			me.errs[key] = errMessage(err)
		}
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
		if stacks(w.e) {
			return image.Pt(maxW, w.size(gtx, maxW, true).Y+me.errH(gtx, w.bid)), true
		}
		sz := w.size(gtx, maxW/2, true)
		return sz, false
	}
	return me.run(gtx, bs, maxW, false), false
}

func (me *Message) LayoutElements(gtx layout.Context, bs []richtext.Button, accessory bool) layout.Dimensions {
	drain()
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
		if stacks(w.e) {
			esz.Y -= me.errH(gtx, w.bid)
		}
		w.layout(fixed(gtx, esz), esz)
		if msg := me.errs[w.bid]; msg != "" && stacks(w.e) {
			at(gtx, image.Pt(0, esz.Y+dp(gtx, 4)), func(gtx C) D {
				return errorLine(gtx, me.h.Theme(), colorsOf(me.h.Theme()), sz.X, msg)
			})
		}
		return D{Size: sz}
	}
	me.run(gtx, bs, sz.X, true)
	return D{Size: sz}
}

func (me *Message) errH(gtx C, bid string) int {
	if me.errs[bid] != "" {
		return dp(gtx, 24)
	}
	return 0
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
	if n > maxVisible && !expanded {
		hidden = n - maxVisible
		n = maxVisible
	}
	gap := dp(gtx, 8)
	x, y, rowH, wMax := 0, 0, 0, 0
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
	for _, w := range ws[:n] {
		w := w
		sz := w.size(gtx, maxW, false)
		if sz.X == 0 {
			continue
		}
		place(sz, func(gtx C) { w.layout(gtx, sz) })
	}
	if hidden > 0 {
		txt := "+ " + itoa(hidden) + " more"
		sz := image.Pt(measure(gtx, th, 13, theme.Bold, txt)+2*dp(gtx, 12), dp(gtx, 28))
		mw := ws[0]
		place(sz, func(gtx C) {
			if mw.more.clicked(gtx, me.h) {
				me.more[bid] = true
				me.changed()
			}
			drawButton(gtx, th, sz, "", txt, 4, 13, mw.more.g.Hovered(), false, false)
			mw.more.add(gtx, sz)
		})
	}
	h := y + rowH
	if msg := me.errs[bid]; msg != "" {
		if draw {
			at(gtx, image.Pt(0, h+dp(gtx, 4)), func(gtx C) D { return errorLine(gtx, th, colorsOf(th), maxW, msg) })
		}
		h += dp(gtx, 24)
		wMax = maxW
	}
	return image.Pt(max(wMax, 1), h)
}

var _ richview.Elements = (*Message)(nil)
