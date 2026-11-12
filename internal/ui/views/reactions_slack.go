package views

import (
	"image"
	"strconv"
	"strings"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
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
)

// actBar is one message's ReactionBar.
type actBar struct {
	b   *base
	m   *core.Message
	key string // Slack's reaction key, "message-<ts>-<channel>"
	// noAdd is --add_button_hidden (.c-reaction_add display: none), as
	// Activity's rows render the bar (SKIQ ActivityRowContentReactions)
	noAdd bool
}

// actReactions lays msg's ReactionBar out: the pills left to right,
// wrapping, then the add-reaction button.
func (b *base) actReactions(gtx C, msg *core.Message) D { return b.actReactionsBar(gtx, msg, false) }

// actReactionsBar is actReactions, without the add button when noAdd.
func (b *base) actReactionsBar(gtx C, msg *core.Message, noAdd bool) D {
	a := &actBar{b: b, m: msg, key: "message-" + msg.TS + "-" + msg.Channel, noAdd: noAdd}
	return slackc.Component(gtx, "ReactionBar", a, (*actBar).props, (*actBar).layout)
}

func (a *actBar) props() slackc.Props {
	m := a.m
	kids := make([]any, 0, len(m.Reactions)+1)
	rs := make([]any, len(m.Reactions))
	for i := range m.Reactions {
		r := &m.Reactions[i]
		users := make([]any, len(r.Users))
		for j, u := range r.Users {
			users[j] = u
		}
		rs[i] = slackc.Props{"name": r.Name, "count": r.Count, "users": users}
		pl := &actReact{b: a.b, r: r, key: a.key}
		kids = append(kids, slackc.Child("Reaction", pl.props(), pl.layout))
	}
	if !a.noAdd {
		kids = append(kids, slackc.Child("ReactionAddButton", a.addProps(), a.layoutAdd))
	}
	self := ""
	if c := a.b.h.Core(); c != nil {
		self = c.Self().ID
	}
	return slackc.Props{"$key": a.key, "reactionKey": a.key, "reactions": rs, "currentUser": self, "channelId": m.Channel,
		"className": "c-message_kit__reaction_bar c-reaction_bar", "canReact": true, "children": kids}
}

func (a *actBar) addProps() slackc.Props {
	return slackc.Props{"$key": a.key, "channelId": a.m.Channel, "shouldShowReacjiTriggers": false}
}

// layout is ReactionBar's native (.c-reaction_bar is a flex-wrap row):
// the children a plugin passed on (the Reaction and add-button slots, its
// own elements), each placed like a pill, or the native pills and button.
func (a *actBar) layout(gtx C, p slackc.Props) D {
	st := actBarClass.Enter(gtx, 0, 0)
	defer st.Leave()
	if st.Hidden() {
		return D{}
	}
	lay := func(gtx C) D {
		if e, ok := slackc.PropChildren(p); ok {
			items := slackc.Items(e)
			return a.b.actFlow(gtx, len(items), func(gtx C, i int) D { return slackc.LayoutElement(gtx, items[i]) })
		}
		n := len(a.m.Reactions)
		all := n + 1
		if a.noAdd {
			all = n
		}
		return a.b.actFlow(gtx, all, func(gtx C, i int) D {
			if i < n {
				return a.b.actReaction(gtx, a.m, &a.m.Reactions[i])
			}
			return slackc.Component(gtx, "ReactionAddButton", a, (*actBar).addProps, (*actBar).layoutAdd)
		})
	}
	if st.Active() {
		return st.Wrap(gtx, lay)
	}
	return lay(gtx)
}

// actFlow places n pill-like items left to right, 4px apart, wrapping; an
// item a plugin rendered as nothing takes no place.
func (b *base) actFlow(gtx C, n int, item func(gtx C, i int) D) D {
	maxW := gtx.Constraints.Max.X
	gap := gtx.Dp(4)
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
		lineH = max(lineH, d.Size.Y)
	}
	return D{Size: image.Pt(maxW, y+lineH)}
}

// addBtn is m's add-reaction button state.
func (b *base) addBtn(m *core.Message) *widget.Clickable {
	k := m.Channel + "/" + m.TS
	if b.addBtns == nil || len(b.addBtns) > 2000 {
		b.addBtns = map[string]*widget.Clickable{}
	}
	c := b.addBtns[k]
	if c == nil {
		c = new(widget.Clickable)
		b.addBtns[k] = c
	}
	return c
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
	if st.Active() {
		return st.Wrap(gtx, draw)
	}
	return draw(gtx)
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

// Reaction pills in the Threads, Unreads and Activity views at Slack's
// component boundaries, as in a channel (messages/row.go): Reaction with
// Slack's props, whose child is the pill button (UnstyledButton
// data-qa="reactji", .c-reaction) holding the emoji and the count. A
// plugin's Reaction patch then reaches the button through React context
// (Slick WhoReacted's reactors), and what it appends to the button's
// children is drawn after the count. Unpatched it's the native pill and no
// props are built.

type actReact struct {
	b   *base
	r   *core.Reaction
	key string // the message's ReactionBar key, "message-<ts>-<channel>"
}

// actReaction is one reaction of m.
func (b *base) actReaction(gtx C, m *core.Message, r *core.Reaction) D {
	if !slackc.Patched("Reaction") && !slackc.Patched("UnstyledButton") {
		return b.actPill(gtx, r)
	}
	a := &actReact{b: b, r: r, key: "message-" + m.TS + "-" + m.Channel}
	return slackc.Component(gtx, "Reaction", a, (*actReact).props, (*actReact).layout)
}

func (a *actReact) props() slackc.Props {
	r := a.r
	users := make([]any, len(r.Users))
	for i, u := range r.Users {
		users[i] = u
	}
	return slackc.Props{
		"$key": a.key + ":" + r.Name,
		"name": r.Name, "baseName": strings.SplitN(r.Name, "::", 2)[0], "count": r.Count, "users": users,
		"reactionKey": a.key,
		// the button renders inside Reaction's React tree, so context a
		// Reaction patch provides reaches it
		"children": []any{slackc.Child("UnstyledButton", a.buttonProps(), a.layoutButton)},
	}
}

func (a *actReact) buttonProps() slackc.Props {
	r := a.r
	cls := "c-button-unstyled c-reaction"
	if r.Self {
		cls += " c-reaction--reacted"
	}
	word := "reactions"
	if r.Count == 1 {
		word = "reaction"
	}
	return slackc.Props{"$key": a.key + ":" + r.Name, "data-qa": "reactji", "className": cls, "aria-pressed": r.Self,
		"aria-label": strconv.Itoa(r.Count) + " " + word + ", react with " + strings.NewReplacer("_", " ", "-", " ").Replace(r.Name) + " emoji",
		"children": []any{
			slackc.Child("Emoji", slackc.Props{"emoji": r.Name, "size": 16}, a.layoutEmoji),
			slackc.Child("ReactionCount", slackc.Props{"count": r.Count}, a.layoutCount),
		}}
}

// layout is Reaction's native: the button its tree rendered, or the pill.
func (a *actReact) layout(gtx C, p slackc.Props) D {
	if e, ok := slackc.PropChildren(p); ok {
		return slackc.LayoutElement(gtx, e)
	}
	return slackc.Component(gtx, "UnstyledButton", a, (*actReact).buttonProps, (*actReact).layoutButton)
}

// layoutButton is the pill around the button's children: the emoji, the
// count and whatever a plugin added after them.
func (a *actReact) layoutButton(gtx C, p slackc.Props) D {
	e, ok := slackc.PropChildren(p)
	if !ok {
		return a.b.actPill(gtx, a.r)
	}
	h := gtx.Dp(24)
	padL, padR := gtx.Dp(6), gtx.Dp(8)
	m := op.Record(gtx.Ops)
	cg := gtx
	cg.Constraints = layout.Constraints{Max: image.Pt(max(gtx.Constraints.Max.X-padL-padR, 0), h)}
	cd := slackc.LayoutElement(cg, e)
	call := m.Stop()
	w := padL + cd.Size.X + padR
	a.b.actPillBox(gtx, a.r, image.Pt(w, h))
	defer op.Offset(image.Pt(padL, (h-cd.Size.Y)/2)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
	return D{Size: image.Pt(w, h)}
}

func (a *actReact) layoutEmoji(gtx C, _ slackc.Props) D { return a.b.emojiImg(gtx, a.r.URL, 16) }

// layoutCount is the count after the emoji (4px gap, 11px bold).
func (a *actReact) layoutCount(gtx C, _ slackc.Props) D {
	_, fg := a.b.actPillColors(a.r)
	cnt := strconv.Itoa(a.r.Count)
	tw := a.b.th.Measure(gtx, 11, wBold, cnt)
	h := gtx.Dp(24)
	defer op.Offset(image.Pt(gtx.Dp(4), 0)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(tw, h))
	layout.W.Layout(g, func(gtx C) D { return a.b.label(gtx, 11, wBold, fg, cnt) })
	return D{Size: image.Pt(gtx.Dp(4)+tw, h)}
}
