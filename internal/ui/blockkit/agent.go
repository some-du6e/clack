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
	"log"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
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
	// Icon is the task's icon ({"name": ...}, a Block Kit icon name): kept
	// raw, so a stray shape costs only the icon, never the block
	Icon json.RawMessage `json:"icon"`
}

// taskIconNames is Qw/u deriveIconName: the Block Kit icon names a task's
// icon may use, as Slack's icon set names them.
var taskIconNames = map[string]string{
	"archive":     "archive",
	"book":        "book",
	"bookmark":    "bookmark",
	"bot":         "bot",
	"bug":         "bug",
	"calendar":    "calendar",
	"call":        "call",
	"caret-left":  "caret-left",
	"caret-right": "caret-right",
	"check":       "check",
	"clipboard":   "copy",
	"code":        "code",
	"comment":     "comment",
	"compass":     "explore",
	"copy":        "copy",
	"cube":        "blocks",
	"download":    "download",
	"edit":        "edit",
	"email":       "email",
	"eye-closed":  "eye-closed",
	"eye-open":    "eye-open",
	"file":        "file",
	"flag":        "flag",
	"folder":      "folder",
	"gear":        "settings",
	"globe":       "globe",
	"heart":       "heart",
	"help":        "help",
	"image":       "image",
	"info":        "info",
	"key":         "key",
	"lightbulb":   "emoji-objects",
	"link":        "link",
	"map":         "map",
	"mobile":      "mobile",
	"new-window":  "new-window",
	"pin":         "pin",
	"plus":        "plus",
	"refine":      "refine",
	"refresh":     "refresh",
	"rocket":      "rocket",
	"save":        "save",
	"screen":      "screen",
	"share":       "share",
	"sparkle":     "sparkles",
	"star":        "star",
	"star-filled": "star-filled",
	"tag":         "tag",
	"thumbs-down": "thumbs-down",
	"thumbs-up":   "thumbs-up",
	"trash":       "trash",
	"upload":      "file-upload",
	"user":        "user",
	"warning":     "warning",
}

// taskIconName is the icon TaskIcon (IDPB) draws for t instead of its
// status glyph: the task's own icon, when it has one Block Kit knows and
// the task is neither in progress nor failed; "" otherwise.
func taskIconName(t *AgentTask) string {
	if len(t.Icon) == 0 || t.Status == StatusInProgress || t.Status == StatusError {
		return ""
	}
	var ic struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(t.Icon, &ic) != nil {
		return ""
	}
	return taskIconNames[ic.Name]
}

// agentIconHook, when set (tests), sees each task icon drawn, by name.
var agentIconHook func(name string)

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
	return k == "plan" || k == "task_card" || k == "context_actions" || k == richtext.AgentChannelUnfurlKind
}

// hasAgentBlocks reports whether blocks (or attachments) carry one.
func hasAgentBlocks(raw json.RawMessage) bool {
	return bytes.Contains(raw, []byte(`"plan"`)) || bytes.Contains(raw, []byte(`"task_card"`)) ||
		bytes.Contains(raw, []byte(`"context_actions"`)) || bytes.Contains(raw, []byte(`"agent_channel_unfurl"`))
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

// agentAnim is a running height transition.
type agentAnim struct {
	from, to int
	start    time.Time
	// CSS transitions: the reversing-adjusted start value (where a
	// transition back to it came from) and the reversing shortening factor
	// (the share of the duration it runs for)
	adj    int
	factor float64
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
	target map[string]int // block → the height it was last laid out to reach
	// cuts caches each task content's truncated copy (agent_truncate.go)
	cuts map[string]*agentCut
	// cards are the agent_channel_unfurl attachments' code channel cards
	cards map[string]*popups.ChannelHoverCard
	// touched: the viewer toggled the block (a task card the viewer
	// opened or closed isn't auto-collapsed when it completes)
	touched map[string]bool
}

// agentCut is a task content's truncated copy, for its raw bytes.
type agentCut struct {
	raw, cut  []byte
	truncated bool
}

type agentParsed struct {
	raw []byte
	b   *AgentBlock
}

// agentMsgState is a message's agent block state: one agentState per drawing
// instance (a message shows in the channel and, as a thread's root, in
// the thread pane; each of Slack's rendered PlanBlocks has its own React
// state: open, taps, text, height transition), sharing the parse and cut
// caches and the feedback ratings.
type agentMsgState struct {
	insts  map[uint64]*agentState
	order  []uint64
	parsed map[string]*agentParsed
	cuts   map[string]*agentCut
	rating map[string]string
}

var (
	agentMu     sync.Mutex
	agentStates = map[string]*agentMsgState{}
	agentOrder  []string
)

// agentState is the state of the agent blocks for the drawing instance
// the call is for (me.inst).
func (me *Message) agentState() *agentState {
	k := me.m.Channel + "/" + me.m.TS
	agentMu.Lock()
	defer agentMu.Unlock()
	am := agentStates[k]
	if am == nil {
		am = &agentMsgState{insts: map[uint64]*agentState{}, parsed: map[string]*agentParsed{}, cuts: map[string]*agentCut{},
			rating: map[string]string{}}
		agentStates[k] = am
		agentOrder = append(agentOrder, k)
		if len(agentOrder) > 400 {
			delete(agentStates, agentOrder[0])
			agentOrder = agentOrder[1:]
		}
	}
	st := am.insts[me.inst]
	if st != nil { // the order is by last use: a drawing still on screen isn't the one evicted
		if i := slices.Index(am.order, me.inst); i >= 0 && i != len(am.order)-1 {
			am.order = append(append(am.order[:i:i], am.order[i+1:]...), me.inst)
		}
	}
	if st == nil {
		st = &agentState{parsed: am.parsed, open: map[string]bool{}, taps: map[string]*tap{},
			tips: map[string]*popups.TipState{}, rating: am.rating, views: map[string]*agentText{},
			anim: map[string]*agentAnim{}, shown: map[string]int{}, target: map[string]int{}, cuts: am.cuts, touched: map[string]bool{},
			cards: map[string]*popups.ChannelHoverCard{}}
		am.insts[me.inst] = st
		am.order = append(am.order, me.inst)
		if len(am.order) > 8 { // the drawing used longest ago goes (its row is long gone)
			delete(am.insts, am.order[0])
			am.order = am.order[1:]
		}
	}
	return st
}

var viewSeq atomic.Uint64

// NewViewID is a fresh drawing-instance id for InView (a message row, a
// view's list).
func NewViewID() uint64 { return viewSeq.Add(1) }

// InView is el drawn as instance id: richview's calls go through it, so a
// plan's or task card's toggles, taps, text and height transition there
// are that drawing's own. el is returned as is when it isn't a message's
// element host, or id is 0.
func InView(el richview.Elements, id uint64) richview.Elements {
	if me, ok := el.(*Message); ok && me != nil && id != 0 {
		return elemView{me: me, id: id}
	}
	return el
}

// HostOf is the message element host behind el (InView's or the host
// itself), nil for anything else.
func HostOf(el richview.Elements) *Message {
	switch v := el.(type) {
	case *Message:
		return v
	case elemView:
		return v.me
	}
	return nil
}

// elemView is one drawing of a message's elements (InView).
type elemView struct {
	me *Message
	id uint64
}

func (v elemView) ElementsSize(bs []richtext.Button, maxW int, accessory bool, m unit.Metric) (image.Point, bool) {
	prev := v.me.inst
	v.me.inst = v.id
	defer func() { v.me.inst = prev }()
	return v.me.ElementsSize(bs, maxW, accessory, m)
}

func (v elemView) LayoutElements(gtx layout.Context, bs []richtext.Button, accessory bool) layout.Dimensions {
	prev := v.me.inst
	v.me.inst = v.id
	defer func() { v.me.inst = prev }()
	return v.me.LayoutElements(gtx, bs, accessory)
}

func (v elemView) ElementsVersion() uint32 { return v.me.ElementsVersion() }

var _ richview.Elements = elemView{}

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
// attachments): by block_id, else the first of its type.
func (me *Message) agentBlock(bt *richtext.Button) *AgentBlock {
	st := me.agentState()
	var raw, first json.RawMessage
	look := func(bs []Block) bool {
		for i := range bs {
			b := &bs[i]
			if b.Type != bt.Kind {
				continue
			}
			if first == nil {
				first = b.Raw
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
		// the body's copy of the blocks isn't the host's (a plugin's
		// msg.blocks): the first block of the type
		raw = first
	}
	key := bt.Kind + "\x00" + bt.BlockID
	if raw == nil {
		me.agentMiss(key, "not in the message's blocks")
		return nil
	}
	if p := st.parsed[key]; p != nil && bytes.Equal(p.raw, raw) {
		return p.b
	}
	b, ok := ParseAgentBlock(raw)
	if !ok {
		me.agentMiss(key, "doesn't parse")
		return nil
	}
	st.parsed[key] = &agentParsed{raw: append([]byte(nil), raw...), b: b}
	return b
}

var (
	agentMissMu sync.Mutex
	agentMissed = map[string]bool{}
)

// agentMiss logs, once per message and block, an agent block the host
// can't draw (richview then draws the block's text fallback).
func (me *Message) agentMiss(key, why string) {
	k := me.m.Channel + "/" + me.m.TS + "/" + key
	agentMissMu.Lock()
	seen := agentMissed[k]
	if !seen && len(agentMissed) < 200 {
		agentMissed[k] = true
	}
	agentMissMu.Unlock()
	if !seen {
		log.Printf("blockkit: agent block %q in %s/%s %s (%d blocks)", strings.ReplaceAll(key, "\x00", " "), me.m.Channel, me.m.TS, why, len(me.blocks))
	}
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
	if bt.Kind == richtext.AgentChannelUnfurlKind {
		return me.layoutAgentChannelCard(gtx, me.agentState(), bt.BlockID, maxW)
	}
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

// layoutAgentChannelCard is an agent_channel_unfurl attachment, the code
// channel an agent session made: MessageKitAttachments'
// CodeChannelEntityCard (surface MessageUnfurl), the classic card with its
// border (popups.ChannelHoverCard.EntityCard), one per drawing; nothing
// where Slack draws none.
func (me *Message) layoutAgentChannelCard(gtx C, st *agentState, ch string, maxW int) image.Point {
	key := "acu\x00" + ch
	card := st.cards[key]
	if card == nil {
		card = popups.NewChannelHoverCard(me.h, image.Rectangle{}, ch, false, false)
		st.cards[key] = card
	}
	d, ok := card.EntityCard(gtx, maxW, me.agentChannel(ch))
	if !ok {
		return image.Point{}
	}
	return d.Size
}

// agentChannel is the message's agent_channel_unfurl for channel ch as
// CodeChannelEntityCard's props, the attachments read once: a host is one
// revision of them (blocksSig).
func (me *Message) agentChannel(ch string) popups.EntityProps {
	if me.acu == nil {
		me.acu = map[string]popups.EntityProps{}
		var atts []struct {
			U *slack.AgentChannelUnfurl `json:"agent_channel_unfurl"`
		}
		_ = json.Unmarshal(me.m.Attachments, &atts)
		for _, a := range atts {
			if u := a.U; u != nil && u.ChannelID != "" {
				if _, dup := me.acu[u.ChannelID]; !dup {
					me.acu[u.ChannelID] = popups.EntityProps{Name: u.ChannelName, AgentBotUserID: u.AgentBotUserID,
						JoinableFrom: u.JoinableFromChannelID, Private: u.IsPrivate}
				}
			}
		}
	}
	return me.acu[ch]
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
		def = me.taskCardDefault(st, b, key)
	}
	open := hasContent && st.isOpen(key, def)

	// toggle line (the height transition follows in animated)
	t := st.tap(key)
	if hasContent && t.clicked(gtx, me.h) {
		st.open[key] = !open
		open = !open
		st.touched[key] = true
		if !open { // the content unmounts: its tasks and Show more start over when reopened
			st.resetUnder(b)
		}
		me.changed()
	}
	// the toggle line and, open, the content, drawn whole and then clipped
	// to the height on screen
	whole := op.Record(gtx.Ops)
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
		off := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		var ch int
		if b.Type == "plan" {
			ch = me.planTasks(gtx, st, pal, b, w)
		} else {
			ch = me.cardBody(gtx, st, pal, b, w)
		}
		off.Pop()
		full = y + ch
	}
	call := whole.Stop()
	h := me.animated(gtx, st, key, full)
	// .blockSlim: overflow clip, with overflow-clip-margin 8px (what runs
	// past the block during its height transition shows 8px further)
	m := dp(gtx, 8)
	cl := clip.Rect{Min: image.Pt(-m, top-m), Max: image.Pt(w+m, h+m)}.Push(gtx.Ops)
	call.Add(gtx.Ops)
	cl.Pop()
	st.shown[key] = h
	return image.Pt(w, h+bottom)
}

// animated is the block's height on screen for target, the height its
// content takes now. ExpandableContent (cQd1) sets .block's height to its
// content's measured height (slim, or open), and .block has transition:
// height .25s: so once the block has been drawn, every change of that
// height runs the transition from the height on screen, whatever made it
// (a toggle, an MCP card closing, an update that adds a task or wraps the
// title anew). A change during a transition starts a new one from where
// it got to; one back to where the running one came from runs for the
// share of it that had run (CSS's reversing shortening factor). It keeps
// frames and relayouts coming while one runs.
func (me *Message) animated(gtx C, st *agentState, key string, target int) int {
	// .block's transition: height .25s (slackc.NativeTransitionsCSS),
	// through the cascade: a rule over it (Slick's Snappy) shortens it
	dur, delay := slackc.CurrentScope().Child(clsAgentBlock, 0).Transition("height", 250*time.Millisecond, 0)
	a := st.anim[key]
	last, seen := st.target[key]
	st.target[key] = target
	if seen && last != target && st.shown[key] > 0 && !gtx.Now.IsZero() {
		cur := st.shown[key]
		n := &agentAnim{from: cur, to: target, start: gtx.Now, adj: cur, factor: 1}
		if a != nil {
			if p, done := slackc.TransitionProgress(a.start, gtx.Now, scaleDur(dur, a.factor), delay); !done {
				e := easeCSS(float64(p))
				cur = a.from + int(math.Round(float64(a.to-a.from)*e))
				n.from, n.adj = cur, cur
				if target == a.adj { // going back: the reversing shortening factor
					n.factor = math.Min(math.Abs(e*a.factor+1-a.factor), 1)
					n.adj = a.to
				}
			}
		}
		a = n
		st.anim[key] = a
	}
	if a == nil {
		return target
	}
	p, done := slackc.TransitionProgress(a.start, gtx.Now, scaleDur(dur, a.factor), delay)
	if done || a.from == 0 || gtx.Now.IsZero() {
		delete(st.anim, key)
		return target
	}
	f := easeCSS(float64(p))
	h := a.from + int(math.Round(float64(target-a.from)*f))
	// size changes: relayout and a frame next, asked of the host, not an
	// op.InvalidateCmd mid-layout (the router would replay the frame's
	// unread events)
	me.changed()
	return h
}

// IndicatorActive, when set (the ui package: its main window's focus), is
// whether a spinner may ask for its next frame: an inactive window's
// indicators hold still and ask for none (ui's indicatorFrame), and its
// activation draws them on again.
var IndicatorActive func() bool

// indicatorFrame asks for a spinner's or shimmer's next frame d from now
// while the window is active.
func indicatorFrame(gtx C, d time.Duration) {
	if IndicatorActive != nil && !IndicatorActive() {
		return
	}
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(d)})
}

// scaleDur is d × f.
func scaleDur(d time.Duration, f float64) time.Duration {
	if f <= 0 || f >= 1 {
		return d
	}
	return time.Duration(float64(d) * f)
}

// taskCardDefault is whether a task_card starts open (TaskCard 1+vf, slim
// or not): an MCP task's card (block id mcp_task_…) does, unless it is
// already done under slackbot_mcp_tasks_card_auto_collapse_when_complete.
// Under it a card the viewer hasn't toggled also closes when its task
// completes or fails (once, when first seen done; animated runs the
// height transition from the height on screen).
func (me *Message) taskCardDefault(st *agentState, b *AgentBlock, key string) bool {
	mcp := strings.HasPrefix(b.BlockID, "mcp_task_")
	done := b.Status == StatusComplete || b.Status == StatusError
	auto := me.mcpAutoCollapse()
	if mcp && auto && done && !st.touched[key] && !st.touched[key+"\x00auto"] {
		st.touched[key+"\x00auto"] = true
		if st.isOpen(key, true) {
			st.open[key] = false
		}
	}
	return mcp && !(auto && done)
}

// clsAgentBlock is the plan / task_card block's element (CSS module
// kFCn's .block; slim mode adds .blockSlim).
var clsAgentBlock = slackc.Class("block__G68co")

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
		if len(lines) > 0 && lastW+caretW > max(w-ix, 1) {
			// the caret (.toggleBarCaretInline, inline-flex) is an atomic
			// inline: where the title's last line leaves it no room it
			// breaks onto a line of its own and the bar grows, never past
			// the block's edge (its clip would hide it)
			lastW = 0
			y += lh
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
	indicatorFrame(gtx, 33*time.Millisecond)
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
	keys := taskKeys(b)
	for i := range b.Tasks {
		task := &b.Tasks[i]
		first, last := i == 0, i == n-1
		key := keys[i]
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
			// .taskHeaderSlim in the step content: a fit-content flex row
			// (padding 4px 0, gap 4px) of the title (margin 2px 0), which
			// wraps in what the caret leaves, and the caret (.taskHeaderCaret:
			// align-self start, margin-top 6px), so 10px down, after the
			// title's box: the line's width, or all of it once it wraps
			lines := wrapText(gtx, th, 13, task.Title, max(cw-caret, 1), 0)
			if len(lines) == 0 {
				lines = []string{""}
			}
			ty := y + cy + pad + mar
			lineW := 0
			for _, ln := range lines {
				at(gtx, image.Pt(cx, ty), func(gtx C) D {
					return vcenter(gtx, lh, func(gtx C) D { return label(gtx, th, 13, font.Normal, pal.ter, ln) })
				})
				lineW = measure(gtx, th, 13, font.Normal, ln)
				ty += lh
			}
			titleW := lineW
			if len(lines) > 1 {
				titleW = max(cw-caret, 1)
			}
			headH := pad + mar + len(lines)*lh + mar + pad
			if has {
				name := "caret-right"
				if open {
					name = "caret-down"
				}
				at(gtx, image.Pt(cx+titleW+dp(gtx, 4), y+cy+pad+dp(gtx, 6)), func(gtx C) D { return agentIcon(gtx, name, 13, pal.ter) })
				at(gtx, image.Pt(cx, y+cy), func(gtx C) D {
					t.add(gtx, image.Pt(titleW+caret, headH))
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
		at(gtx, image.Pt(0, ic-dp(gtx, 6)), func(gtx C) D { return stepIcon(gtx, pal, task, gut) })
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
func stepIcon(gtx C, pal agentPal, task *AgentTask, gut int) D {
	h := dp(gtx, 12)
	status := task.Status
	if name := taskIconName(task); name != "" {
		// .taskIconSlim in the step indicator: the content's 13px, centred
		// on its 8px box (overflow visible); content-sec when complete,
		// else the block's content-ter
		c := pal.ter
		if status == StatusComplete {
			c = pal.sec
		}
		s := dp(gtx, 13)
		at(gtx, image.Pt((gut-s)/2, (h-s)/2), func(gtx C) D { return icon(gtx, name, 13, c) })
		if agentIconHook != nil {
			agentIconHook(name)
		}
		return D{Size: image.Pt(gut, h)}
	}
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
		add(me.slimRich(gtx, st, pal, key+"\x00d", t.Details, x, y+h+gapIf(h, gap), w))
	}
	if isRichText(t.Output) {
		add(me.slimRich(gtx, st, pal, key+"\x00o", t.Output, x, y+h+gapIf(h, gap), w))
	}
	if len(t.Sources) > 0 {
		add(me.sources(gtx, st, pal, key, t.Sources, x, y+h+gapIf(h, gap), w))
	}
	return h
}

// slimRich is a slim TaskItem's details or output (its q: ZJNP
// TruncatedBlocks, maxLines 3, maxChars 250, block-level expand button):
// the rich text cut by Slack's rules (agent_truncate.go) with "Show more"
// under it, the whole of it with "Show less" once opened
// (c-rich_text_expand_button / _collapse_button: c-link--button, the
// content's 13px in --dt_color-content-hgl-1, underlined on hover, a line
// of its own).
func (me *Message) slimRich(gtx C, st *agentState, pal agentPal, key string, raw json.RawMessage, x, y, w int) int {
	c := st.cuts[key]
	if c == nil || !bytes.Equal(c.raw, raw) {
		cut, ok := truncateTaskContent(raw)
		c = &agentCut{raw: append([]byte(nil), raw...), cut: cut, truncated: ok}
		st.cuts[key] = c
	}
	moreKey := key + "\x00more"
	open := st.isOpen(moreKey, false)
	src, vk := raw, key+"\x00full"
	if c.truncated && !open {
		src, vk = c.cut, key+"\x00cut"
	}
	tv := me.agentRich(st, vk, src)
	h := at(gtx, image.Pt(x, y), func(gtx C) D { return D{Size: image.Pt(w, tv.layoutAgent(gtx, w))} }).Size.Y
	if !c.truncated {
		return h
	}
	th := me.h.Theme()
	txt := searchNS.T("Show more")
	if open {
		txt = searchNS.T("Show less")
	}
	t := st.tap(moreKey)
	if t.clicked(gtx, me.h) {
		st.open[moreKey] = !open
		me.changed()
	}
	lh := dp(gtx, agentLH)
	lw := min(measure(gtx, th, 13, font.Normal, txt), w)
	at(gtx, image.Pt(x, y+h), func(gtx C) D {
		return vcenter(gtx, lh, func(gtx C) D { return label(gtx, th, 13, font.Normal, pal.link, txt) })
	})
	at(gtx, image.Pt(x, y+h), func(gtx C) D {
		t.add(gtx, image.Pt(max(lw, 1), lh))
		return D{}
	})
	if t.g.Hovered() {
		rrect(gtx, image.Rect(x, y+h+lh-dp(gtx, 3), x+lw, y+h+lh-dp(gtx, 2)), 0, pal.link)
	}
	return h + lh
}

// taskKeys are a plan's tasks' state keys (open, Show more): Slack's
// PlanTaskList keys its items by task_id (hTrF InteractiveList's key ??
// e.id), so a task's state follows it across an update wherever it moves;
// the index stands in only for a task with no task_id, or one that
// repeats in the plan.
func taskKeys(b *AgentBlock) []string {
	n := map[string]int{}
	for i := range b.Tasks {
		n[b.Tasks[i].TaskID]++
	}
	keys := make([]string, len(b.Tasks))
	for i := range b.Tasks {
		if id := b.Tasks[i].TaskID; id != "" && n[id] == 1 {
			keys[i] = "task\x00" + b.BlockID + "\x00" + id
		} else {
			keys[i] = "task\x00" + b.BlockID + "\x00#" + itoa(i)
		}
	}
	return keys
}

// resetUnder forgets the open states under a block that closed: Slack
// unmounts its content (PlanTaskList's TaskItems, their truncated text),
// so reopening it shows each task open and its text cut again.
func (st *agentState) resetUnder(b *AgentBlock) {
	pre := "task\x00" + b.BlockID + "\x00"
	if b.Type != "plan" {
		pre = "card\x00" + b.BlockID + "\x00" // not another card's whose id starts with this one's
	}
	for k := range st.open {
		if strings.HasPrefix(k, pre) {
			delete(st.open, k)
		}
	}
}

// mcpAutoCollapse is slackbot_mcp_tasks_card_auto_collapse_when_complete
// (mGJN sn).
func (me *Message) mcpAutoCollapse() bool {
	c := me.h.Core()
	return c != nil && c.Experiment("slackbot_mcp_tasks_card_auto_collapse_when_complete") == "on"
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
		h := me.slimRich(gtx, st, pal, key+"\x00d", b.Details, pad, y, cw)
		rrect(gtx, image.Rect(gut/2, y, gut/2+lineW, y+h), 0, pal.line)
		y += h
	}
	start := y
	h := 0
	if isRichText(b.Output) {
		h = me.slimRich(gtx, st, pal, key+"\x00o", b.Output, pad, y, cw)
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
