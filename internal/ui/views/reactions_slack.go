package views

import (
	"image"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/ctxclick"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A message's reactions in these views are Slack's ReactionBar, as in a
// channel (messages/row.go): the Threads view's roots and replies
// (ThreadRootBodyGeneric / ThreadReplyBodyGeneric), the Unreads view's
// messages and the Activity pane's message all render MessageReactionBar
// (gantry-v2 boot-shared G+LV: className c-message_kit__reaction_bar), whose
// ReactionBar (Ltiy) lays out the Reaction pills and then, with
// addReactionButton "right" and canReact, the ReactionAddButton. A
// plugin's ReactionBar patch (CopyReacted's reactors context) reaches the
// add button inside it, and what a ReactionAddButton patch renders after
// it (CopyReacted's copy chip) sits in the bar's row. Unpatched and with
// no CSS it's the native pills and add button, no props built.

var (
	actBarClass = slackc.Class("c-message_kit__reaction_bar c-reaction_bar", "data-qa", "reaction_bar")
	actAddClass = slackc.Class("c-button-unstyled c-reaction_add", "data-qa", "add_reaction_button").Tag("button")
	// the pill (Reaction's UnstyledButton data-qa="reactji"); mods: the
	// viewer reacted
	actReactClass = slackc.Class("c-button-unstyled c-reaction", "data-qa", "reactji").Tag("button").Mods("c-reaction--reacted")
	// the pill's count (span.c-reaction__count), drawn inside the pill
	actCountClass = slackc.Class("c-reaction__count").Tag("span")
)

// actBar is one message's ReactionBar.
type actBar struct {
	b   *base
	m   *core.Message
	key string // Slack's reaction key, "message-<ts>-<channel>"
	// noAdd is --add_button_hidden (.c-reaction_add display: none), as
	// Activity's rows render the bar (SKIQ ActivityRowContentReactions)
	noAdd bool
	// groups are getReactionGroups' (a pill for every skin tone of one
	// emoji); can is canReact (canAddReaction): without it the pills are
	// disabled and the bar has no add button
	groups []messages.ReactionGroup
	can    bool
}

// actReactions lays msg's ReactionBar out: the pills left to right,
// wrapping, then the add-reaction button.
func (b *base) actReactions(gtx C, msg *core.Message) D { return b.actReactionsBar(gtx, msg, false) }

// actReactionsBar is actReactions, without the add button when noAdd.
func (b *base) actReactionsBar(gtx C, msg *core.Message, noAdd bool) D {
	msg = b.withLocalReactions(msg)
	a := &actBar{b: b, m: msg, key: "message-" + msg.TS + "-" + msg.Channel, noAdd: noAdd,
		groups: messages.ReactionGroups(msg.Reactions), can: b.canReact(msg)}
	return slackc.Component(gtx, "ReactionBar", a, (*actBar).props, (*actBar).layout)
}

// canReact is ReactionBar's canReact: canAddReaction (core.CanAddReaction).
func (b *base) canReact(m *core.Message) bool {
	c := b.h.Core()
	return c == nil || c.CanAddReaction(m)
}

// add reports whether the bar has its add button: addReactionButton
// "right" with canReact, unless Activity hides it.
func (a *actBar) add() bool { return a.can && !a.noAdd }

func (a *actBar) props() slackc.Props {
	m := a.m
	kids := make([]any, 0, len(a.groups)+1)
	rs := make([]any, len(m.Reactions))
	for i := range m.Reactions {
		rs[i] = actReactionProps(&m.Reactions[i])
	}
	for i := range a.groups {
		pl := &actReact{b: a.b, m: m, g: a.groups[i], key: a.key, can: a.can}
		kids = append(kids, slackc.Child("Reaction", pl.props(), pl.layout))
	}
	if a.add() {
		kids = append(kids, slackc.Child("ReactionAddButton", a.addProps(), a.layoutAdd))
	}
	return slackc.Props{"$key": a.key, "reactionKey": a.key, "reactions": rs, "currentUser": a.b.self(), "channelId": m.Channel,
		"className": "c-message_kit__reaction_bar c-reaction_bar", "canReact": a.can, "children": kids}
}

// actReactionProps is one reaction as the reactions store has it.
func actReactionProps(r *core.Reaction) slackc.Props {
	users := make([]any, len(r.Users))
	for j, u := range r.Users {
		users[j] = u
	}
	return slackc.Props{"name": r.Name, "baseName": strings.SplitN(r.Name, "::", 2)[0], "count": r.Count, "users": users, "url": r.URL}
}

// self is the viewer's id.
func (b *base) self() string {
	if c := b.h.Core(); c != nil {
		return c.Self().ID
	}
	return ""
}

func (a *actBar) addProps() slackc.Props {
	return slackc.Props{"$key": a.key, "channelId": a.m.Channel, "shouldShowReacjiTriggers": false}
}

// layout is ReactionBar's native (.c-reaction_bar is a flex-wrap row):
// the children a plugin passed on (the Reaction and add-button slots, its
// own elements), each placed like a pill, or the native pills and button.
// The bar takes the className a plugin passes on (OwlMode's class, which
// its .c-reaction_bar.<class> .c-reaction rules need), as in a channel.
func (a *actBar) layout(gtx C, p slackc.Props) D {
	st := actBarClass.EnterClass(gtx, 0, 0, slackc.ExtraClass(p))
	defer st.Leave()
	if st.Hidden() {
		return D{}
	}
	lay := func(gtx C) D {
		if e, ok := slackc.PropChildren(p); ok {
			items := slackc.Items(e)
			return a.b.actFlow(gtx, len(items), func(gtx C, i int) D { return slackc.LayoutElement(gtx, items[i]) })
		}
		n := len(a.groups)
		all := n
		if a.add() {
			all++
		}
		return a.b.actFlow(gtx, all, func(gtx C, i int) D {
			if i < n {
				return a.b.actReaction(gtx, a.m, a.groups[i], a.can)
			}
			return slackc.Component(gtx, "ReactionAddButton", a, (*actBar).addProps, (*actBar).layoutAdd)
		})
	}
	if st.Active() {
		return st.Wrap(gtx, lay)
	}
	return lay(gtx)
}

// actFlow places n pill-like items left to right, 4px apart, wrapping, on
// the bar's 24px rows, as a channel's bar places them (messages'
// flowPills): a pill drawn through a plugin's tree (a patched bar's
// children, a patched pill) came back as its line box, 4px taller (the
// strut under its baseline). An item a plugin rendered as nothing takes
// no place.
func (b *base) actFlow(gtx C, n int, item func(gtx C, i int) D) D {
	maxW := gtx.Constraints.Max.X
	gap := gtx.Dp(4)
	rowH := gtx.Dp(24)
	x, y, lineH := 0, 0, 0
	for i := 0; i < n; i++ {
		m := op.Record(gtx.Ops)
		cg := gtx
		cg.Constraints.Min = image.Point{}
		d := item(cg, i)
		call := m.Stop()
		if d.Size.X == 0 && d.Size.Y == 0 {
			continue
		}
		if x > 0 && x+d.Size.X > maxW {
			x, y, lineH = 0, y+lineH+gap, 0
		}
		st := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		st.Pop()
		x += d.Size.X + gap
		lineH = rowH
	}
	return D{Size: image.Pt(maxW, y+lineH)}
}

// addBtn is m's add-reaction button state.
func (b *base) addBtn(m *core.Message) *widget.Clickable {
	k := m.Channel + "/" + m.TS
	if b.addBtns == nil || len(b.addBtns) > 2000 {
		b.addBtns = map[string]*widget.Clickable{}
		b.addTips = nil
	}
	c := b.addBtns[k]
	if c == nil {
		c = new(widget.Clickable)
		b.addBtns[k] = c
	}
	return c
}

// addTip is m's add-reaction button's tooltip state.
func (b *base) addTip(m *core.Message) *popups.TipState {
	k := m.Channel + "/" + m.TS
	if b.addTips == nil {
		b.addTips = map[string]*popups.TipState{}
	}
	t := b.addTips[k]
	if t == nil {
		t = &popups.TipState{Delay: 300 * time.Millisecond}
		b.addTips[k] = t
	}
	return t
}

// layoutAdd is ReactionAddButton (.c-reaction_add), as in a channel: a
// 34×24 pill with the add-reaction glyph, its hover outlined with the
// glyph in Slack's yellow. A click opens the reaction picker, which
// reacts through submitAddOrRemoveReaction.
func (a *actBar) layoutAdd(gtx C, _ slackc.Props) D {
	b := a.b
	btn := b.addBtn(a.m)
	if btn.Clicked(gtx) {
		b.openReactionPicker(a.m)
	}
	st := actAddClass.Enter(gtx, hoverState(btn.Hovered()), 0)
	defer st.Leave()
	if st.Hidden() {
		return D{}
	}
	h := gtx.Dp(24)
	size := image.Pt(gtx.Dp(34), h)
	draw := func(gtx C) D {
		gtx.Constraints = layout.Exact(size)
		return btn.Layout(gtx, func(gtx C) D {
			rr := image.Rectangle{Max: size}
			col := b.p.Text
			if btn.Hovered() {
				hb := rgb(0x7c7a7f)
				if b.th.Dark {
					hb = argb(0x4de8e8e8)
				}
				border(gtx, rr, h/2, max(1, gtx.Dp(1)), hb)
				col = rgb(0xecb22e)
			} else {
				rrect(gtx, rr, h/2, st.Background(b.p.Chip))
			}
			col = st.Color(col)
			is := gtx.Dp(18)
			off := op.Offset(image.Pt((size.X-is)/2, (size.Y-is)/2)).Push(gtx.Ops)
			b.icon(gtx, "add-reaction", 18, col)
			off.Pop()
			defer clip.Rect(rr).Push(gtx.Ops).Pop()
			pointer.CursorPointer.Add(gtx.Ops)
			return D{Size: size}
		})
	}
	var d D
	if st.Active() {
		d = st.Wrap(gtx, draw)
	} else {
		d = draw(gtx)
	}
	// ReactionAddButton's Tooltip (BaseTooltip, top, 300 ms)
	popups.Tooltip(gtx, b.th, b.addTip(a.m), btn.Hovered(), size, popups.Above, "Add reaction…", "")
	return d
}

// openReactionPicker opens the emoji picker for reacting to m at the
// pointer, as the add-reaction button does in a channel.
func (b *base) openReactionPicker(m *core.Message) {
	ph, ok := b.h.(popups.Host)
	if !ok {
		return
	}
	var at image.Point
	if pp, ok := b.h.(interface{ PointerPos() image.Point }); ok {
		at = pp.PointerPos()
	}
	ph.ShowOverlay(popups.NewReactionPicker(ph, at, m.Channel, m.TS, m.ThreadTS, nil))
}

// Reaction pills in the views at Slack's component boundaries, as in a
// channel (messages/row.go): Reaction with Slack's props for one
// getReactionGroups group, whose child is the pill button (UnstyledButton
// data-qa="reactji", .c-reaction) holding the group's emoji (the first
// ten, then "…") and the count. A plugin's Reaction patch then reaches the
// button through React context (Slick WhoReacted's reactors), and what it
// appends to the button's children is drawn after the count. Unpatched
// it's the native pill and no props are built.
//
// A click is handleReactionClick (messages.ClickReactionGroup, as a
// channel's pill: the viewer's reactions of the group off, or the group's
// emoji in their skin tone, optimistically). Without canReact the button
// is disabled: .c-reaction:disabled is pointer-events none, so the pill
// takes no click, hover, cursor or ReactionTip.

type actReact struct {
	b   *base
	m   *core.Message
	g   messages.ReactionGroup
	key string // the message's ReactionBar key, "message-<ts>-<channel>"
	can bool   // canReact
}

// actReaction is one reaction group g of m.
func (b *base) actReaction(gtx C, m *core.Message, g messages.ReactionGroup, can bool) D {
	a := &actReact{b: b, m: m, g: g, key: "message-" + m.TS + "-" + m.Channel, can: can}
	if !slackc.Patched("Reaction") && !slackc.Patched("UnstyledButton") {
		return a.button(gtx, func(gtx C) D { return b.actPill(gtx, m, g, a.reacted(), a.hovered()) })
	}
	return slackc.Component(gtx, "Reaction", a, (*actReact).props, (*actReact).layout)
}

// reacted reports whether the viewer reacted with any of the group's
// reactions (Reaction's users include the bar's current user).
func (a *actReact) reacted() bool { return a.g.Reacted(a.m.Reactions, a.b.self()) }

// btn is the pill's button state.
func (a *actReact) btn() *widget.Clickable {
	k := a.key + ":" + a.g.Name
	b := a.b
	if b.pillBtns == nil || len(b.pillBtns) > 2000 {
		b.pillBtns = map[string]*widget.Clickable{}
	}
	c := b.pillBtns[k]
	if c == nil {
		c = new(widget.Clickable)
		b.pillBtns[k] = c
	}
	return c
}

// hovered is .c-reaction:hover (never on a disabled pill).
func (a *actReact) hovered() bool { return a.can && a.btn().Hovered() }

// button is the pill's UnstyledButton around w: its click toggles the
// group (a macOS Ctrl+click is the pill's context click, not a toggle),
// it shows the hand, and it has the pill's ReactionTip; disabled, w alone.
// w draws unclipped (a plugin's box-shadow ring is outside the pill, and
// a Clickable clips what it lays out), with the button's area over it.
func (a *actReact) button(gtx C, w layout.Widget) D {
	if !a.can {
		return w(gtx)
	}
	btn := a.btn()
	if ctxclick.Clicked(gtx, btn) {
		messages.ClickReactionGroup(a.b.h, a.m.Channel, a.m.TS, a.g.Name, &rxnLocalCopy{b: a.b, m: *a.m})
	}
	if h := btn.History(); len(h) > 0 && h[len(h)-1].Start.Equal(gtx.Now) {
		a.b.pillPress = gtx.Now // Reaction's click stops there (pressedOn)
	}
	d := w(gtx)
	g := gtx
	g.Constraints = layout.Exact(d.Size)
	btn.Layout(g, func(gtx C) D {
		defer clip.Rect{Max: d.Size}.Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		return D{Size: d.Size}
	})
	a.b.reactTip(gtx, a.key, a.m, a.g, d.Size)
	return d
}

// pillTip is a pill's ReactionTip trigger: its own pointer handler,
// which any Leave or Cancel ends (whatever pointer: a handler made again
// starts with a Cancel of pointer 0, and on Windows the mouse is 1).
type pillTip struct {
	tag   int
	hov   bool
	since time.Time
	gen   uint64 // the TooltipManager generation the hover began in
	org   theme.TipOrigin
}

// reactTip is group g's ReactionTip, as a channel's pills have it
// (messages.NewReactionGroupTooltip): over the pill (size, at the current
// offset), after ReactionTipDelay, deferred above the view. Every enabled
// pill gets it, the native one and the one a plugin's Reaction or
// ReactionBar tree drew (actReact.layout).
func (b *base) reactTip(gtx C, key string, m *core.Message, g messages.ReactionGroup, size image.Point) {
	k := key + ":" + g.Name
	if b.pillTips == nil || len(b.pillTips) > 2000 {
		b.pillTips = map[string]*pillTip{}
	}
	t := b.pillTips[k]
	if t == nil {
		t = new(pillTip)
		b.pillTips[k] = t
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &t.tag, Kinds: pointer.Enter | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			if e.Kind == pointer.Enter && !t.hov {
				t.since, t.gen = gtx.Now, theme.TipGen()
			}
			t.hov = e.Kind == pointer.Enter
		}
	}
	rect := image.Rectangle{Max: size}
	t.org.Track(gtx, rect)
	ar := clip.Rect(rect).Push(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &t.tag)
	pass.Pop()
	ar.Pop()
	if !t.hov || size.X <= 0 || t.gen != theme.TipGen() { // hidden (theme.HideTooltips) until the pointer leaves
		return
	}
	if wait := t.since.Add(messages.ReactionTipDelay); gtx.Now.Before(wait) {
		gtx.Execute(op.InvalidateCmd{At: wait})
		return
	}
	tip := messages.NewReactionGroupTooltip(b.h, m.Reactions, g, b.self())
	rec := op.Record(gtx.Ops)
	tip.Draw(gtx, b.h, rect, t.org.Bounds())
	op.Defer(gtx.Ops, rec.Stop())
}

func (a *actReact) props() slackc.Props {
	g := a.g
	users := make([]any, 0, g.Count)
	for _, u := range g.Users(a.m.Reactions) {
		users = append(users, u)
	}
	rs := make([]any, 0, len(g.Idx))
	for _, i := range g.Idx {
		rs = append(rs, actReactionProps(&a.m.Reactions[i]))
	}
	return slackc.Props{
		"$key": a.key + ":" + g.Name,
		"name": g.Name, "count": g.Count, "users": users, "reactions": rs,
		"currentUser": a.b.self(), "reactionKey": a.key, "canReact": a.can,
		// the button renders inside Reaction's React tree, so context a
		// Reaction patch provides reaches it; its native draws the pill's
		// box (fill, ring) itself, so a rule a plugin's className brings
		// is painted once, round (slackc.ChildBox)
		"children": []any{slackc.ChildBox("UnstyledButton", a.buttonProps(), a.layoutButton)},
	}
}

func (a *actReact) buttonProps() slackc.Props {
	g := a.g
	cls := "c-button-unstyled c-reaction"
	self := a.reacted()
	if self {
		cls += " c-reaction--reacted"
	}
	n := min(len(g.Idx), messages.ReactionEmojiMax)
	kids := make([]any, 0, n+2)
	for _, i := range g.Idx[:n] {
		kids = append(kids, slackc.Child("Emoji", slackc.Props{"emoji": a.m.Reactions[i].Name, "size": 16}, a.layoutEmoji))
	}
	if len(g.Idx) > messages.ReactionEmojiMax {
		kids = append(kids, slackc.Child("ReactionTruncation", slackc.Props{"data-qa": "reaction-truncation", "className": "c-reaction__truncation"}, a.layoutTruncation))
	}
	kids = append(kids, slackc.Child("ReactionCount", slackc.Props{"count": g.Count}, a.layoutCount))
	return slackc.Props{"$key": a.key + ":" + g.Name, "data-qa": "reactji", "className": cls, "aria-pressed": self,
		"disabled": !a.can, "aria-label": messages.ReactionAria(g.Name, g.Count), "children": kids}
}

// layout is Reaction's native: the button its tree rendered, or the pill,
// as the pill's button (its click and ReactionTip).
func (a *actReact) layout(gtx C, p slackc.Props) D {
	return a.button(gtx, func(gtx C) D {
		if e, ok := slackc.PropChildren(p); ok {
			return slackc.LayoutElement(gtx, e)
		}
		// the pill draws its box itself (slackc.ComponentBox, as ChildBox)
		return slackc.ComponentBox(gtx, "UnstyledButton", a, (*actReact).buttonProps, (*actReact).layoutButton)
	})
}

// layoutButton is the pill (.c-reaction, --reacted for the viewer's
// own) around the button's children: the emoji, the count and whatever a
// plugin added after them, as .c-reaction's centred inline-flex items 4px
// apart (ClassDef.LayoutChildren), where the native pill (actPill) puts
// its emoji and count. The pill takes the className the button was
// rendered with, so a class a plugin adds to it is matched as in Slack; a
// rule's fill and box-shadow replace the pill's own and follow its round
// ends (WrapRounded).
func (a *actReact) layoutButton(gtx C, p slackc.Props) D {
	var mods uint64
	self := a.reacted()
	if self {
		mods = 1
	}
	hov := a.hovered()
	st := actReactClass.EnterClass(gtx, hoverState(hov), mods, slackc.ExtraClass(p))
	defer st.Leave()
	if st.Hidden() {
		return D{}
	}
	h := gtx.Dp(24)
	padL, padR := gtx.Dp(6), gtx.Dp(8)
	m := op.Record(gtx.Ops)
	cg := gtx
	cg.Constraints = layout.Constraints{Max: image.Pt(max(gtx.Constraints.Max.X-padL-padR, 0), h)}
	var cd D
	if e, ok := slackc.PropChildren(p); ok {
		cd = actReactClass.LayoutChildren(cg, mods, e)
	} else {
		cd = a.content(cg, st)
	}
	call := m.Stop()
	size := image.Pt(padL+cd.Size.X+padR, h)
	pill := func(gtx C) D {
		a.b.actPillBoxStyled(gtx, self, hov, size, st)
		defer op.Offset(image.Pt(padL, (h-cd.Size.Y)/2)).Push(gtx.Ops).Pop()
		call.Add(gtx.Ops)
		return D{Size: size}
	}
	if st.Active() {
		gtx.Constraints = layout.Exact(size)
		return st.WrapRounded(gtx, unit.Dp(12), pill)
	}
	return pill(gtx)
}

// content is the native pill's emoji, "…" past ten and count, 4px apart,
// in the 24px pill's height, as actPill draws them; st is the pill's.
func (a *actReact) content(gtx C, st slackc.Styled) D {
	h, es, gap := gtx.Dp(24), gtx.Dp(16), gtx.Dp(4)
	x := 0
	for k, i := range a.g.Idx {
		if k == messages.ReactionEmojiMax {
			break
		}
		off := op.Offset(image.Pt(x, (h-es)/2)).Push(gtx.Ops)
		a.b.emojiImg(gtx, a.m.Reactions[i].URL, 16)
		off.Pop()
		x += es + gap
	}
	if len(a.g.Idx) > messages.ReactionEmojiMax {
		off := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		d := a.truncation(gtx, st.Child(actCountClass, 0))
		off.Pop()
		x += d.Size.X + gap
	}
	off := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
	d := a.count(gtx, st.Child(actCountClass, 0))
	off.Pop()
	return D{Size: image.Pt(x+d.Size.X, h)}
}

// layoutEmoji is one of the pill's Emoji children: the reaction its
// "emoji" prop names (the group's first without it).
func (a *actReact) layoutEmoji(gtx C, p slackc.Props) D {
	name := p.StrOr("emoji", "")
	url := ""
	for k, i := range a.g.Idx {
		if r := &a.m.Reactions[i]; k == 0 || r.Name == name {
			url = r.URL
			if r.Name == name {
				break
			}
		}
	}
	return a.b.emojiImg(gtx, url, 16)
}

// layoutTruncation is the pill's "…" (span.c-reaction__truncation).
func (a *actReact) layoutTruncation(gtx C, _ slackc.Props) D {
	return a.truncation(gtx, slackc.CurrentScope().Child(actCountClass, 0))
}

func (a *actReact) truncation(gtx C, st slackc.Styled) D {
	return a.text(gtx, st, "\u2026")
}

// layoutCount is the pill's count slot (span.c-reaction__count), a flex
// item 4px after the emoji: 11px bold in the pill's 24px height, in the
// colour CSS gives the pill's text (OwlMode's reacted pill: content-pry).
func (a *actReact) layoutCount(gtx C, _ slackc.Props) D {
	return a.count(gtx, slackc.CurrentScope().Child(actCountClass, 0))
}

// count is the pill's count, centred in its 24px height, as actPill
// draws it; st is the count's.
func (a *actReact) count(gtx C, st slackc.Styled) D {
	return a.text(gtx, st, strconv.Itoa(a.g.Count))
}

func (a *actReact) text(gtx C, st slackc.Styled, s string) D {
	_, fg := a.b.actPillColors(a.reacted())
	tw := a.b.th.Measure(gtx, 11, wBold, s)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(tw, gtx.Dp(24)))
	layout.W.Layout(g, func(gtx C) D { return a.b.label(gtx, 11, wBold, st.Color(fg), s) })
	return D{Size: g.Constraints.Max}
}
