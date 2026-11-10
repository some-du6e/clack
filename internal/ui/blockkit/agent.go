package blockkit

// Slack's agent blocks (2025–2026), drawn into the message's element
// slots (richtext.Agent → richview's slot → Message.ElementsSize /
// LayoutElements):
//
//   - plan: a collapsible "thinking" line (✓ Thinking completed ›) whose
//     tasks expand as a step timeline (PlanBlock, E5Xj).
//   - task_card: one task as the same toggle line (TaskCard, wdtw).
//   - context_actions: feedback_buttons (👍/👎, ESSv) and icon_button
//     (sh7I) in a row (ContextActions, Ubrj).
//
// Slack draws these in "slim" mode when agent_streaming_slimmer_task_cards
// is on (it is on Hack Club, 2026-11-08: the reference screenshot shows
// the inline check and caret); off, or not assigned, it draws them as
// cards (agent_card.go). Geometry and colours are Slack's CSS
// modules kFCn/Dl0Z (modern.vendor css, 2026-11-08):
//
//	plan / task_card   .block.blockSlim: no card, margin 8px 0 (0 on top
//	                   when first), height transition .25s
//	toggle line        13px/1.5 --dt_color-content-ter; status icon 12px
//	                   wide + 8px; caret-right/down 13px, 4px after the
//	                   title; complete/error icons --dt_color-content-sec,
//	                   in progress a 12px blue InfiniteSpinner
//	plan task (slim)   12px step gutter + 8px gap: 1px --dt_color-base-ter
//	                   line, 6px (first 12px) above the step dot, 4px gaps;
//	                   title 13px ter, padding 4px 0, caret after it;
//	                   details/output/sources under it, 4px apart; tasks
//	                   start expanded
//	task_card body     details and output beside a 1px line at 6px
//	                   (padding-left 20px); output gets the 8px dot
//	feedback / icon    32px .c-icon_button (radius 8, 18px icon,
//	button             --dt_color-content-sec; hover content-pry on
//	                   base-ter); the picked rating shows the -filled icon
//
// Status mapping is Slack's: a plan is in progress while any task is
// pending or in progress, else errored when any task errored, else
// complete; a task_card that is neither in progress nor errored shows
// complete. Plan tasks are collapsible one by one.

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Task statuses (gXOk).
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusComplete   = "complete"
	StatusError      = "error"
)

// ---- model ----

// AgentSource is a task source link.
type AgentSource struct {
	Type string `json:"type"` // url
	URL  string `json:"url"`
	Text string `json:"text"`
}

// AgentTask is a plan task or a task_card's task.
type AgentTask struct {
	TaskID  string          `json:"task_id"`
	Title   string          `json:"title"`
	Status  string          `json:"status"`
	Details json.RawMessage `json:"details"` // rich_text block
	Output  json.RawMessage `json:"output"`  // rich_text block
	Sources []AgentSource   `json:"sources"`
	// HideTitle is hide_title (hideTitle once camel-cased, KjNg): under
	// plan_block_coding_enhancements a plan task shows its content
	// without its title line.
	HideTitle bool `json:"hide_title"`
}

func (t *AgentTask) hasContent() bool {
	return isRichText(t.Details) || isRichText(t.Output) || len(t.Sources) > 0
}

// FeedbackButton is one side of feedback_buttons.
type FeedbackButton struct {
	Text               Text   `json:"text"`
	AccessibilityLabel string `json:"accessibility_label"`
	Value              string `json:"value"`
}

// AgentElement is a context_actions element.
type AgentElement struct {
	Type     string `json:"type"` // feedback_buttons | icon_button
	ActionID string `json:"action_id"`
	// feedback_buttons
	Positive *FeedbackButton `json:"positive_button"`
	Negative *FeedbackButton `json:"negative_button"`
	// icon_button
	Text               *Text    `json:"text"`
	Icon               string   `json:"icon"`
	Value              string   `json:"value"`
	AccessibilityLabel string   `json:"accessibility_label"`
	Confirm            *Confirm `json:"confirm"`
	VisibleTo          []string `json:"visible_to_user_ids"`
}

// AgentBlock is a plan, task_card or context_actions block.
type AgentBlock struct {
	Type    string `json:"type"`
	BlockID string `json:"block_id"`
	AgentTask
	Tasks    []AgentTask    `json:"tasks"`    // plan
	Elements []AgentElement `json:"elements"` // context_actions
}

// ParseAgentBlock decodes one agent block.
func ParseAgentBlock(raw json.RawMessage) (*AgentBlock, bool) {
	var b AgentBlock
	if json.Unmarshal(raw, &b) != nil {
		return nil, false
	}
	switch b.Type {
	case "plan", "task_card", "context_actions":
		return &b, true
	}
	return nil, false
}

// PlanStatus is Slack's plan status (E5Xj q).
func PlanStatus(ts []AgentTask) string {
	err := false
	for _, t := range ts {
		switch t.Status {
		case StatusInProgress, StatusPending:
			return StatusInProgress
		case StatusError:
			err = true
		}
	}
	if err {
		return StatusError
	}
	return StatusComplete
}

// CardStatus is Slack's task_card header status (wdtw V).
func CardStatus(s string) string {
	switch s {
	case StatusInProgress, StatusError:
		return s
	}
	return StatusComplete
}

// PlanTitle is the plan's title, else Slack's getPlanTitle fallback.
func PlanTitle(b *AgentBlock) string {
	if b.Title != "" {
		return b.Title
	}
	last, prog := "", ""
	for _, t := range b.Tasks {
		if s := strings.TrimSpace(t.Title); s != "" {
			last = s
			if t.Status == StatusInProgress {
				prog = s
			}
		}
	}
	if prog != "" {
		return prog
	}
	return last
}

func isRichText(raw json.RawMessage) bool {
	return len(raw) > 2 && bytes.Contains(raw, []byte(`"rich_text"`))
}

// isAgentKind reports whether a richview button names an agent block.
func isAgentKind(k string) bool {
	return k == "plan" || k == "task_card" || k == "context_actions"
}

// hasAgentBlocks reports whether blocks (or attachments) carry one.
func hasAgentBlocks(raw json.RawMessage) bool {
	return bytes.Contains(raw, []byte(`"plan"`)) || bytes.Contains(raw, []byte(`"task_card"`)) ||
		bytes.Contains(raw, []byte(`"context_actions"`))
}

// ---- payloads (blocks.actions actions[0]) ----

// FeedbackPayload is the feedback_buttons action Slack's client sends
// (ESSv: onAction({type, actionId, blockId, value, text}) → snake_case).
func FeedbackPayload(blockID string, e *AgentElement, positive bool) json.RawMessage {
	b := e.Negative
	if positive {
		b = e.Positive
	}
	m := map[string]any{"type": "feedback_buttons", "action_id": e.ActionID, "block_id": blockID}
	if b != nil {
		m["value"] = b.Value
		m["text"] = b.Text
	}
	out, _ := json.Marshal(m)
	return out
}

// IconButtonPayload is the icon_button action (sh7I: {type, actionId,
// blockId, icon, text, value}).
func IconButtonPayload(blockID string, e *AgentElement) json.RawMessage {
	m := map[string]any{"type": "icon_button", "action_id": e.ActionID, "block_id": blockID, "icon": e.Icon}
	if e.Text != nil {
		m["text"] = e.Text
	}
	if e.Value != "" {
		m["value"] = e.Value
	}
	out, _ := json.Marshal(m)
	return out
}

// ---- per-message state ----

type agentAnim struct {
	from, to int
	start    time.Time
}

type agentState struct {
	parsed map[string]*agentParsed
	open   map[string]bool // toggle key → expanded (absent: the default)
	taps   map[string]*tap
	tips   map[string]*popups.TipState
	rating map[string]string // block\x00action → positive | negative
	views  map[string]*agentText
	anim   map[string]*agentAnim
	shown  map[string]int // block → height last drawn (animation start)
}

type agentParsed struct {
	raw []byte
	b   *AgentBlock
}

var (
	agentMu     sync.Mutex
	agentStates = map[string]*agentState{}
	agentOrder  []string
)

func (me *Message) agentState() *agentState {
	k := me.m.Channel + "/" + me.m.TS
	agentMu.Lock()
	defer agentMu.Unlock()
	st := agentStates[k]
	if st == nil {
		st = &agentState{parsed: map[string]*agentParsed{}, open: map[string]bool{}, taps: map[string]*tap{},
			tips: map[string]*popups.TipState{}, rating: map[string]string{}, views: map[string]*agentText{},
			anim: map[string]*agentAnim{}, shown: map[string]int{}}
		agentStates[k] = st
		agentOrder = append(agentOrder, k)
		if len(agentOrder) > 400 {
			delete(agentStates, agentOrder[0])
			agentOrder = agentOrder[1:]
		}
	}
	return st
}

func (st *agentState) tap(k string) *tap {
	t := st.taps[k]
	if t == nil {
		t = &tap{}
		st.taps[k] = t
	}
	return t
}

func (st *agentState) tip(k string) *popups.TipState {
	t := st.tips[k]
	if t == nil {
		t = &popups.TipState{Delay: 150 * time.Millisecond}
		st.tips[k] = t
	}
	return t
}

// isOpen is a toggle's state: plans and task cards start collapsed
// (task cards of MCP tasks, block ids "mcp_task_…", start open); plan
// tasks start open (TaskItem F).
func (st *agentState) isOpen(k string, def bool) bool {
	if v, ok := st.open[k]; ok {
		return v
	}
	return def
}

// agentBlock finds the agent block bt names in the message (or its
// attachments): by block_id, else the first of its type without one.
func (me *Message) agentBlock(bt *richtext.Button) *AgentBlock {
	st := me.agentState()
	var raw json.RawMessage
	look := func(bs []Block) bool {
		for i := range bs {
			b := &bs[i]
			if b.Type != bt.Kind {
				continue
			}
			if b.BlockID == bt.BlockID {
				raw = b.Raw
				return true
			}
		}
		return false
	}
	if !look(me.blocks) {
		for i := range me.atts {
			if look(me.atts[i].Blocks) {
				break
			}
		}
	}
	if raw == nil {
		return nil
	}
	key := bt.Kind + "\x00" + bt.BlockID
	if p := st.parsed[key]; p != nil && bytes.Equal(p.raw, raw) {
		return p.b
	}
	b, ok := ParseAgentBlock(raw)
	if !ok {
		return nil
	}
	st.parsed[key] = &agentParsed{raw: append([]byte(nil), raw...), b: b}
	return b
}

// ---- colours ----

type agentPal struct {
	pry, sec, ter, line, hover, blue, link, track color.NRGBA
}

// agentColors: Slack's tokens (light / dark), overridable by a theme.
func agentColors(th *theme.Theme) agentPal {
	var p agentPal
	if th.Dark {
		p = agentPal{pry: rgb(0xd1d2d3), sec: rgb(0xb9babd), ter: rgb(0x9a9b9e), line: rgb(0x35373b),
			hover: rgb(0x2c2e33), blue: rgb(0x1d9bd1), link: rgb(0x1d9bd1)}
	} else {
		p = agentPal{pry: rgb(0x1d1c1d), sec: rgb(0x454447), ter: rgb(0x5e5d60), line: rgb(0xdddddd),
			hover: rgb(0xf0f0f0), blue: rgb(0x1264a3), link: rgb(0x1264a3)}
	}
	if t := th.Tokens; t != nil && t.Overridden() {
		theme.SetTok(t, &p.sec, theme.Both("var(--dt_color-content-sec)"), th.Dark)
		theme.SetTok(t, &p.ter, theme.Both("var(--dt_color-content-ter)"), th.Dark)
		theme.SetTok(t, &p.blue, theme.Both("var(--dt_color-content-hgl-1)"), th.Dark)
	}
	p.track = p.ter
	p.track.A = 64
	return p
}

// ---- rich text (details, output) ----

// agentText is a task's rich_text drawn 13px in the tertiary colour.
type agentText struct {
	raw  []byte
	view *textView
}

func (me *Message) agentRich(st *agentState, key string, raw json.RawMessage) *textView {
	at := st.views[key]
	if at != nil && bytes.Equal(at.raw, raw) {
		return at.view
	}
	blocks, _ := json.Marshal([]json.RawMessage{raw})
	var r richtext.Resolver = nilResolver{}
	if c := me.h.Core(); c != nil && c.S != nil {
		r = c.S
	}
	doc := richtext.FromMessage(r, &slack.Message{Blocks: blocks})
	tv := &textView{h: me.h, hand: surfHandler{h: me.h}}
	tv.v = richview.New(doc, richview.Options{Compact: true})
	st.views[key] = &agentText{raw: append([]byte(nil), raw...), view: tv}
	return tv
}

type nilResolver struct{}

func (nilResolver) UserName(id string) string                { return id }
func (nilResolver) ChannelName(id string) string             { return id }
func (nilResolver) UsergroupName(id string) string           { return id }
func (nilResolver) Emoji(string) (url, text string, ok bool) { return "", "", false }
func (nilResolver) SelfID() string                           { return "" }

var (
	agentStyleMu sync.Mutex
	agentStyles  = map[*text.Shaper]*agentStyleEntry{}
)

type agentStyleEntry struct {
	st   *richview.Style
	dark bool
	ter  color.NRGBA
	init bool
}

// agentStyle is styleFor's richview style with body text in the
// tertiary colour (.blockContentSlim: --dt_color-content-ter, 13px).
func agentStyle(th *theme.Theme, h Host) *richview.Style {
	ter := agentColors(th).ter
	agentStyleMu.Lock()
	defer agentStyleMu.Unlock()
	e := agentStyles[th.Shaper]
	if e == nil {
		var imgs richview.Images
		if im := h.Images(); im != nil {
			imgs = im
		}
		e = &agentStyleEntry{st: richview.NewStyle(th.Shaper, imgs, richview.LightPalette())}
		e.st.Face = theme.Typeface
		e.st.EmojiURL = th.EmojiURL
		agentStyles[th.Shaper] = e
	}
	if !e.init || e.dark != th.Dark || e.ter != ter {
		e.init, e.dark, e.ter = true, th.Dark, ter
		pal := richview.LightPalette()
		e.st.Embolden = theme.BodyEmboldenFor(theme.LightPalette.Fg)
		e.st.LightTextFade = 0
		if th.Dark {
			pal = richview.DarkPalette()
			e.st.Embolden = theme.BodyEmboldenFor(theme.DarkPalette.Fg)
			e.st.LightTextFade = 0.235
		}
		pal.Text = ter
		e.st.SetPalette(pal)
	}
	return e.st
}

// ---- layout ----

const agentLH = 19 // 13px × Slack's 1.46668 line height

// agentSize / agentDraw: one function lays out (and with a real gtx,
// draws) an agent block; ElementsSize calls it on scratch ops.
func (me *Message) agentRun(gtx C, bt *richtext.Button, maxW int) image.Point {
	b := me.agentBlock(bt)
	if b == nil {
		return image.Point{}
	}
	st := me.agentState()
	switch b.Type {
	case "plan", "task_card":
		if !me.slimTaskCards() { // agent_streaming_slimmer_task_cards off: Slack's cards (agent_card.go)
			return me.layoutTaskCard(gtx, st, b, bt.Style == richtext.AgentFirst, maxW)
		}
		return me.layoutTaskBlock(gtx, st, b, bt.Style == richtext.AgentFirst, maxW)
	case "context_actions":
		return me.layoutContextActions(gtx, st, b, maxW)
	}
	return image.Point{}
}

// layoutTaskBlock draws a plan or task_card (slim): the toggle line and,
// open, its content, clipped to the animated height.
func (me *Message) layoutTaskBlock(gtx C, st *agentState, b *AgentBlock, first bool, w int) image.Point {
	th := me.h.Theme()
	pal := agentColors(th)
	top := dp(gtx, 4) // margin 8px; richview's block gap gives the other 4
	if first {
		top = 0
	}
	bottom := dp(gtx, 4)
	key := b.Type + "\x00" + b.BlockID
	var status, title string
	var hasContent bool
	def := false
	if b.Type == "plan" {
		status, title, hasContent = PlanStatus(b.Tasks), PlanTitle(b), len(b.Tasks) > 0
	} else {
		status, title, hasContent = CardStatus(b.Status), b.Title, b.hasContent()
		def = strings.HasPrefix(b.BlockID, "mcp_task_")
	}
	open := hasContent && st.isOpen(key, def)

	// toggle line
	t := st.tap(key)
	if hasContent && t.clicked(gtx, me.h) {
		st.open[key] = !open
		open = !open
		st.anim[key] = &agentAnim{from: st.shown[key], start: gtx.Now}
		me.changed()
	}
	y := top
	td := at(gtx, image.Pt(0, y), func(gtx C) D {
		return me.toggleLine(gtx, pal, status, title, hasContent, open, w)
	})
	hh := td.Size.Y
	if hasContent {
		at(gtx, image.Pt(0, y), func(gtx C) D {
			t.add(gtx, image.Pt(max(td.Size.X, 1), hh))
			return D{}
		})
	}
	y += hh
	full := y
	if open {
		m := op.Record(gtx.Ops)
		var ch int
		if b.Type == "plan" {
			ch = me.planTasks(gtx, st, pal, b, w)
		} else {
			ch = me.cardBody(gtx, st, pal, b, w)
		}
		call := m.Stop()
		full = y + ch
		h := me.animated(gtx, st, key, y, full)
		cl := clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops)
		off := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		off.Pop()
		cl.Pop()
		st.shown[key] = h
		return image.Pt(w, h+bottom)
	}
	h := me.animated(gtx, st, key, y, y)
	st.shown[key] = h
	return image.Pt(w, h+bottom)
}

// animated is the block height during Slack's 250ms height transition
// (from what was last shown to target); it keeps frames and relayouts
// coming while it runs.
func (me *Message) animated(gtx C, st *agentState, key string, min0, target int) int {
	a := st.anim[key]
	if a == nil {
		return target
	}
	if a.to != target {
		a.to = target
	}
	el := gtx.Now.Sub(a.start)
	const dur = 250 * time.Millisecond
	if el >= dur || a.from == 0 || gtx.Now.IsZero() {
		delete(st.anim, key)
		return target
	}
	f := easeCSS(float64(el) / float64(dur))
	h := a.from + int(math.Round(float64(target-a.from)*f))
	me.ver.Add(1) // size changes: relayout next frame
	gtx.Execute(op.InvalidateCmd{})
	return max(h, min0)
}

// easeCSS is CSS's default "ease" (cubic-bezier(.25,.1,.25,1)).
func easeCSS(x float64) float64 { return cubicBezier(0.25, 0.1, 0.25, 1, x) }

// cubicBezier evaluates a CSS timing function at x in [0,1].
func cubicBezier(x1, y1, x2, y2, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	bez := func(a, b, t float64) float64 { u := 1 - t; return 3*u*u*t*a + 3*u*t*t*b + t*t*t }
	lo, hi := 0.0, 1.0
	t := x
	for i := 0; i < 30; i++ {
		v := bez(x1, x2, t)
		if math.Abs(v-x) < 1e-5 {
			break
		}
		if v < x {
			lo = t
		} else {
			hi = t
		}
		t = (lo + hi) / 2
	}
	return bez(y1, y2, t)
}

// toggleLine is ToggleBar (uPDO) in slim mode: inline status icon, the
// title (13px/1.5, tertiary, wrapping under itself) and a caret.
func (me *Message) toggleLine(gtx C, pal agentPal, status, title string, hasContent, open bool, w int) D {
	th := me.h.Theme()
	lh := dp(gtx, 19.5)
	ix := dp(gtx, 12) + dp(gtx, 8)
	// icon, vertically centred on the first line (top: -.5px)
	at(gtx, image.Pt(0, (lh-dp(gtx, 13))/2-dp(gtx, 0.5)), func(gtx C) D {
		return statusIcon(gtx, pal, status, true)
	})
	caretW := 0
	if hasContent {
		caretW = dp(gtx, 4) + dp(gtx, 13)
	}
	lines := wrapText(gtx, th, 13, title, max(w-ix, 1), caretW)
	y := 0
	lastW, maxW := 0, 0
	for _, ln := range lines {
		at(gtx, image.Pt(ix, y), func(gtx C) D {
			return vcenter(gtx, lh, func(gtx C) D { return label(gtx, th, 13, font.Normal, pal.ter, ln) })
		})
		lastW = measure(gtx, th, 13, font.Normal, ln)
		maxW = max(maxW, lastW)
		y += lh
	}
	if len(lines) == 0 {
		y = lh
	}
	if hasContent {
		name := "caret-right"
		if open {
			name = "caret-down"
		}
		cx := ix + lastW + dp(gtx, 4)
		at(gtx, image.Pt(cx, y-lh+(lh-dp(gtx, 13))/2), func(gtx C) D { return agentIcon(gtx, name, 13, pal.ter) })
		maxW = max(maxW, lastW+caretW)
	}
	return D{Size: image.Pt(min(ix+maxW, w), y)}
}

// statusIcon is TaskIcon/ToggleBar's icon: the 12px spinner while in
// progress, else the status glyph (primary: check-circle-filled /
// warning-filled in content-sec).
func statusIcon(gtx C, pal agentPal, status string, primary bool) D {
	sz := dp(gtx, 12)
	switch status {
	case StatusInProgress:
		agentSpinner(gtx, sz, pal)
		return D{Size: image.Pt(sz, sz)}
	case StatusError:
		return agentIcon(gtx, "warning-filled", 13, pal.sec)
	}
	if primary {
		return agentIcon(gtx, "check-circle-filled", 13, pal.sec)
	}
	return agentIcon(gtx, "check-filled", 13, pal.sec)
}

// agentSpinner is Slack's InfiniteSpinner (s6So) at size small (12px,
// blue): a .25-opacity tertiary track and two blue 90° arcs turning
// once per 1.2s on different easings (ring and tail).
func agentSpinner(gtx C, d int, pal agentPal) {
	sc := float32(d) / 100
	r := 35 * sc
	sw := 8 * sc
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	circle := func(a0, sweep float32, c color.NRGBA) {
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(ctr.X+r*float32(math.Cos(float64(a0))), ctr.Y+r*float32(math.Sin(float64(a0)))))
		p.ArcTo(ctr, ctr, sweep)
		st := clip.Stroke{Path: p.End(), Width: sw}.Op().Push(gtx.Ops)
		paint.ColorOp{Color: c}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
	}
	circle(0, 2*math.Pi-0.001, pal.track)
	ms := gtx.Now.UnixMilli()
	t := float64(ms%1200) / 1200
	// dash 55 of 2π·35 ≈ 220 starting 90 before 3 o'clock, ring at -45°
	dash := float32(55.0 / 219.9 * 2 * math.Pi)
	base := float32(-90.0/219.9*2*math.Pi) - float32(math.Pi/4)
	ring := float32(2 * math.Pi * cubicBezier(.25, .29, .54, .86, t))
	tail := float32(2 * math.Pi * cubicBezier(.41, .24, .64, .69, t))
	circle(base+ring, dash, pal.blue)
	circle(base+tail, dash, pal.blue)
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
}

// wrapText breaks s into lines of at most w px (13px regular), keeping
// room for reserve px after the last line (the caret) when it fits.
func wrapText(gtx C, th *theme.Theme, size float32, s string, w, reserve int) []string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if s == "" {
		return nil
	}
	if measure(gtx, th, size, font.Normal, s)+reserve <= w {
		return []string{s}
	}
	var lines []string
	cur := ""
	for _, wd := range strings.Fields(s) {
		next := wd
		if cur != "" {
			next = cur + " " + wd
		}
		if cur != "" && measure(gtx, th, size, font.Normal, next) > w {
			lines = append(lines, cur)
			cur = wd
			continue
		}
		cur = next
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// planTasks is PlanTaskList (PJjx) of slim TaskItems (SkmW): a step
// timeline, each task collapsible. Under plan_block_coding_enhancements
// (FiNg.XI) a task with hide_title has no title line or caret: its
// content shows at once (taskExpandedContentNoTitle: margin 2px 0,
// padding-top 4px).
func (me *Message) planTasks(gtx C, st *agentState, pal agentPal, b *AgentBlock, w int) int {
	th := me.h.Theme()
	enh := false
	if c := me.h.Core(); c != nil {
		enh = c.Experiment("plan_block_coding_enhancements") == "on"
	}
	gut := dp(gtx, 12)
	cx := gut + dp(gtx, 8)
	cw := max(w-cx, 1)
	lineW := max(dp(gtx, 1), 1)
	y := 0
	n := len(b.Tasks)
	for i := range b.Tasks {
		task := &b.Tasks[i]
		first, last := i == 0, i == n-1
		key := "task\x00" + b.BlockID + "\x00" + task.TaskID + "\x00" + itoa(i)
		has := task.hasContent()
		noTitle := enh && task.HideTitle
		open := st.isOpen(key, true)
		t := st.tap(key)
		if has && !noTitle && t.clicked(gtx, me.h) {
			open = !open
			st.open[key] = open
			me.changed()
		}
		if noTitle {
			open = true // no header to collapse it from
		}
		// content column
		cy := 0
		if first {
			cy = dp(gtx, 6)
		}
		if noTitle && has {
			cy += dp(gtx, 2) + dp(gtx, 4) // taskExpandedContentNoTitle: margin-top 2px, padding-top 4px
		}
		if !noTitle {
			pad := dp(gtx, 4)
			mar := dp(gtx, 2)
			lh := dp(gtx, agentLH)
			caret := 0
			if has {
				caret = dp(gtx, 4) + dp(gtx, 13)
			}
			lines := wrapText(gtx, th, 13, task.Title, cw, caret)
			if len(lines) == 0 {
				lines = []string{""}
			}
			ty := y + cy + pad + mar
			lastW := 0
			for _, ln := range lines {
				at(gtx, image.Pt(cx, ty), func(gtx C) D {
					return vcenter(gtx, lh, func(gtx C) D { return label(gtx, th, 13, font.Normal, pal.ter, ln) })
				})
				lastW = measure(gtx, th, 13, font.Normal, ln)
				ty += lh
			}
			headH := pad + mar + len(lines)*lh + mar + pad
			if has {
				name := "caret-right"
				if open {
					name = "caret-down"
				}
				// align-self start, margin-top 6px
				at(gtx, image.Pt(cx+lastW+dp(gtx, 4), y+cy+dp(gtx, 6)), func(gtx C) D { return agentIcon(gtx, name, 13, pal.ter) })
				at(gtx, image.Pt(cx, y+cy), func(gtx C) D {
					t.add(gtx, image.Pt(lastW+caret, headH))
					return D{}
				})
			}
			cy += headH
		}
		if has && open {
			ch := me.taskContent(gtx, st, pal, key, task, cx, y+cy, cw)
			cy += ch
			if noTitle {
				cy += dp(gtx, 2) // margin-bottom 2px
			}
		}
		itemH := cy
		// step indicator: line above, dot, line below to the item's end
		top := dp(gtx, 6)
		if first {
			top = dp(gtx, 12)
		}
		gap := dp(gtx, 4)
		lx := (gut - lineW) / 2
		rrect(gtx, image.Rect(lx, y, lx+lineW, y+top), 0, pal.line)
		iy := y + top + gap + dp(gtx, 2) // icon box: margin 2px 0, height 8
		ic := iy + dp(gtx, 4)
		at(gtx, image.Pt(0, ic-dp(gtx, 6)), func(gtx C) D { return stepIcon(gtx, pal, task.Status, gut) })
		below := iy + dp(gtx, 8) + dp(gtx, 2) + gap
		showLine := !last || (open && has)
		if itemH < below-y {
			itemH = below - y
		}
		if showLine {
			rrect(gtx, image.Rect(lx, below, lx+lineW, y+itemH), 0, pal.line)
		}
		y += itemH
	}
	return y
}

// stepIcon is the secondary slim TaskIcon in a gutter-wide, 12px-tall
// box: spinner in progress, warning on error, else the placeholder dot
// (5px; content-sec when complete, tertiary when pending).
func stepIcon(gtx C, pal agentPal, status string, gut int) D {
	h := dp(gtx, 12)
	switch status {
	case StatusInProgress:
		at(gtx, image.Pt((gut-h)/2, 0), func(gtx C) D { agentSpinner(gtx, h, pal); return D{} })
	case StatusError:
		s := dp(gtx, 13)
		at(gtx, image.Pt((gut-s)/2, (h-s)/2), func(gtx C) D { return agentIcon(gtx, "warning-filled", 13, pal.sec) })
	default:
		c := pal.ter
		if status == StatusComplete {
			c = pal.sec
		}
		s := dp(gtx, 5)
		at(gtx, image.Pt((gut-s)/2, (h-s)/2), func(gtx C) D { agentIconPx(gtx, "icon-placeholder-filled", s, c); return D{} })
	}
	return D{Size: image.Pt(gut, h)}
}

// taskContent is a plan task's open content (.taskExpandedContent:
// column, 4px gap): details, output, sources.
func (me *Message) taskContent(gtx C, st *agentState, pal agentPal, key string, t *AgentTask, x, y, w int) int {
	gap := dp(gtx, 4)
	h := 0
	add := func(dh int) {
		if dh <= 0 {
			return
		}
		if h > 0 {
			h += gap
		}
		h += dh
	}
	if isRichText(t.Details) {
		tv := me.agentRich(st, key+"\x00d", t.Details)
		add(at(gtx, image.Pt(x, y+h+gapIf(h, gap)), func(gtx C) D { return D{Size: image.Pt(w, tv.layoutAgent(gtx, w))} }).Size.Y)
	}
	if isRichText(t.Output) {
		tv := me.agentRich(st, key+"\x00o", t.Output)
		add(at(gtx, image.Pt(x, y+h+gapIf(h, gap)), func(gtx C) D { return D{Size: image.Pt(w, tv.layoutAgent(gtx, w))} }).Size.Y)
	}
	if len(t.Sources) > 0 {
		add(me.sources(gtx, st, pal, key, t.Sources, x, y+h+gapIf(h, gap), w))
	}
	return h
}

func gapIf(h, gap int) int {
	if h > 0 {
		return gap
	}
	return 0
}

// sources lists a task's source links (13px, link colour, one a line).
func (me *Message) sources(gtx C, st *agentState, pal agentPal, key string, ss []AgentSource, x, y, w int) int {
	th := me.h.Theme()
	lh := dp(gtx, agentLH)
	h := 0
	for i, s := range ss {
		txt := strings.TrimSpace(s.Text)
		if txt == "" {
			txt = s.URL
		}
		u := richtext.SafeURL(s.URL)
		t := st.tap(key + "\x00src" + itoa(i))
		if u != "" && t.clicked(gtx, me.h) {
			openURL(u)
		}
		lines := wrapText(gtx, th, 13, txt, w, 0)
		for j, ln := range lines {
			ln := ln
			d := at(gtx, image.Pt(x, y+h), func(gtx C) D {
				return vcenter(gtx, lh, func(gtx C) D { return label(gtx, th, 13, font.Normal, pal.link, ln) })
			})
			if u != "" && j == 0 {
				at(gtx, image.Pt(x, y+h), func(gtx C) D {
					t.add(gtx, image.Pt(max(d.Size.X, 1), lh*len(lines)))
					return D{}
				})
			}
			if t.g.Hovered() {
				rrect(gtx, image.Rect(x, y+h+lh-dp(gtx, 3), x+d.Size.X, y+h+lh-dp(gtx, 2)), 0, pal.link)
			}
			h += lh
		}
	}
	return h
}

// cardBody is a task_card's open content (TaskItem, not a plan): details
// beside a 1px line (padding-left 20px, line at 6px); then output with
// the 8px dot over a line (taskContentIcons), and sources.
func (me *Message) cardBody(gtx C, st *agentState, pal agentPal, b *AgentBlock, w int) int {
	gut := dp(gtx, 12)
	lineW := max(dp(gtx, 1), 1)
	pad := gut + dp(gtx, 8)
	cw := max(w-pad, 1)
	key := "card\x00" + b.BlockID
	y := 0
	if isRichText(b.Details) {
		tv := me.agentRich(st, key+"\x00d", b.Details)
		h := at(gtx, image.Pt(pad, y), func(gtx C) D { return D{Size: image.Pt(cw, tv.layoutAgent(gtx, cw))} }).Size.Y
		rrect(gtx, image.Rect(gut/2, y, gut/2+lineW, y+h), 0, pal.line)
		y += h
	}
	start := y
	h := 0
	if isRichText(b.Output) {
		tv := me.agentRich(st, key+"\x00o", b.Output)
		h = at(gtx, image.Pt(pad, y), func(gtx C) D { return D{Size: image.Pt(cw, tv.layoutAgent(gtx, cw))} }).Size.Y
		h += dp(gtx, 4) // .taskContentOutput margin-bottom
	}
	if len(b.Sources) > 0 {
		h += me.sources(gtx, st, pal, key, b.Sources, pad, y+h, cw)
	}
	if h == 0 {
		return y
	}
	if isRichText(b.Output) {
		// dot (8px, tertiary) 7px down, then the divider line under it
		dy := start + dp(gtx, 7)
		s := dp(gtx, 7)
		at(gtx, image.Pt((gut-s)/2, dy), func(gtx C) D { agentIconPx(gtx, "icon-placeholder-filled", s, pal.ter); return D{} })
		ly := dy + s + dp(gtx, 2) + dp(gtx, 8)
		if ly < start+h {
			rrect(gtx, image.Rect(gut/2, ly, gut/2+lineW, start+h), 0, pal.line)
		}
	} else {
		rrect(gtx, image.Rect(gut/2, start, gut/2+lineW, start+h), 0, pal.line)
	}
	return start + h
}

// layoutAgent draws the text at width tw in the agent style.
func (tv *textView) layoutAgent(gtx C, tw int) int {
	g := gtx
	g.Constraints.Min = image.Point{}
	g.Constraints.Max = image.Pt(max(tw, 1), 1<<20)
	st := agentStyle(tv.h.Theme(), tv.h)
	d := tv.v.Layout(g, st, &tv.hand)
	return d.Size.Y
}

// ---- context_actions ----

// layoutContextActions is ContextActions (Ubrj): its elements in a row
// (align end, justify start); feedback_buttons are two 32px icon
// buttons, icon_button one.
func (me *Message) layoutContextActions(gtx C, st *agentState, b *AgentBlock, w int) image.Point {
	th := me.h.Theme()
	pal := agentColors(th)
	top := dp(gtx, 4)
	sz := dp(gtx, 32)
	x := 0
	self := ""
	if c := me.h.Core(); c != nil && c.S != nil {
		self = c.S.SelfID()
	}
	for i := range b.Elements {
		e := &b.Elements[i]
		k := b.BlockID + "\x00" + e.ActionID + "\x00" + itoa(i)
		switch e.Type {
		case "feedback_buttons":
			if e.Positive == nil || e.Negative == nil {
				continue
			}
			rating := st.rating[k]
			for side := 0; side < 2; side++ {
				pos := side == 0
				fb := e.Negative
				name := "thumbs-down"
				if pos {
					fb, name = e.Positive, "thumbs-up"
				}
				picked := (pos && rating == "positive") || (!pos && rating == "negative")
				if picked {
					name += "-filled"
				}
				tk := k + "\x00" + name[:9]
				t := st.tap(tk)
				if t.clicked(gtx, me.h) {
					if pos {
						st.rating[k] = "positive"
					} else {
						st.rating[k] = "negative"
					}
					me.sendAgent(b.BlockID, "feedback_buttons", FeedbackPayload(b.BlockID, e, pos))
				}
				at(gtx, image.Pt(x, top), func(gtx C) D {
					iconButton(gtx, pal, name, sz, t.g.Hovered())
					t.add(gtx, image.Pt(sz, sz))
					popups.Tooltip(gtx, th, st.tip(tk), t.g.Hovered(), image.Pt(sz, sz), popups.Above, textOf(&fb.Text), "")
					return D{}
				})
				x += sz
			}
		case "icon_button":
			if len(e.VisibleTo) > 0 && !slices.Contains(e.VisibleTo, self) {
				continue
			}
			name := blockKitIconNames[e.Icon]
			if name == "" {
				continue
			}
			t := st.tap(k)
			if t.clicked(gtx, me.h) {
				e := e
				act := func() { me.sendAgent(b.BlockID, "icon_button", IconButtonPayload(b.BlockID, e)) }
				if e.Confirm != nil {
					showConfirm(me.h, e.Confirm, act, nil)
				} else {
					act()
				}
			}
			at(gtx, image.Pt(x, top), func(gtx C) D {
				iconButton(gtx, pal, name, sz, t.g.Hovered())
				t.add(gtx, image.Pt(sz, sz))
				popups.Tooltip(gtx, th, st.tip(k), t.g.Hovered(), image.Pt(sz, sz), popups.Above, textOf(e.Text), "")
				return D{}
			})
			x += sz
		}
	}
	if x == 0 {
		return image.Point{}
	}
	return image.Pt(min(x, w), top+sz)
}

// iconButton is .c-icon_button--size_smedium --default.
func iconButton(gtx C, pal agentPal, name string, sz int, hovered bool) {
	col := pal.sec
	if hovered {
		rrect(gtx, image.Rect(0, 0, sz, sz), dp(gtx, 8), pal.hover)
		col = pal.pry
	}
	is := dp(gtx, 18)
	at(gtx, image.Pt((sz-is)/2, (sz-is)/2), func(gtx C) D { agentIconPx(gtx, name, is, col); return D{} })
}

// sendAgent posts an agent element's block action.
func (me *Message) sendAgent(blockID, typ string, payload json.RawMessage) {
	me.changed()
	if me.h.Core() == nil {
		return
	}
	w := &ew{o: me, bid: blockID, b: &Block{Type: "context_actions", BlockID: blockID}, e: &Element{Type: typ}}
	me.fire(w, payload, func(error) { me.changed() })
}
