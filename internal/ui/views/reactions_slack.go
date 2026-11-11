package views

import (
	"image"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

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
