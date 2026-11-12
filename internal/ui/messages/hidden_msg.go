package messages

import (
	"image"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Hidden people's messages, as Slack's message pane draws them (gantry-v2
// build d3de3a487b1c, 2026-11-12; CSS from client-boot-styles, whose
// class names are the same in every build):
//
//   - MessageWrapper (boot-shared EXGU) hands Message (NnKv) isSenderHidden
//     = isUserHidden(msg.user) (client-boot 0OuZ a: hidden_users, unless
//     the org locked hide_person_opt_out on). Message wraps its body in
//     MessageTombstoneContent (kzyb w, the reply bar passed as replyBar),
//     which for such a sender draws
//     div.c-message_kit__tombstone__text--hidden_message around
//     HiddenMessageBody (aOPx), then the reply bar. The body's own reply
//     bar and broadcast footer are off (`!isSenderHidden`).
//   - HiddenMessageBody, not shown anyway: MessageSender (ffJE), whose
//     name is "This message is hidden" (isMessageFromHiddenSender, eQJk),
//     "  " and the clickable Timestamp (not under Compact), a break,
//     the contents "You’re hiding messages from this person." or, when the
//     message mentions the viewer, "Someone you hid <b>mentioned you</b>
//     in this message.", a break, then "Show anyway" with an inline
//     caret-right-full (div.c-message_kit__hidden__message__link,
//     margin-bottom 8px). Under Compact there are no breaks: the parts are
//     inline-blocks on one line, " " before the link, and the tombstone
//     text has margin-bottom 4px.
//   - Shown anyway (msg.isUnhidden, unhideMessage z+/K FE): the message's
//     own body (contents--unhidden: sk_primary_foreground), a break, then
//     "Hide message" with caret-down-full (hideMessage), then the reply
//     bar.
//   - The sender's name is "Name hidden" (BaseMessageSender zWB7) and its
//     custom status is off (ffJE hideCustomStatus); the avatar is
//     HiddenUserAvatar (WbDp, BaseAvatar xc3R isUserHidden): a square in
//     rgba(foreground_soft, .06), border-radius 4px, the user-hide icon
//     (24px from a 36px avatar, 16px up to 24px) in
//     rgba(foreground_max, .7). The reply bar's avatars are BaseAvatars
//     too.
//
// .c-message_kit__tombstone__text--hidden_message is display: inline, so
// the base rule's min-height 44px and flex box don't apply: each part is
// a 15px/22px line in rgba(foreground_max, .7).

// hiddenSender is the row's hidden-sender state this frame: user, the
// sender is hidden (isUserHidden); unhidden, the message was shown
// anyway. A deleted message (the tombstone wins in kzyb) and an
// optimistic send aren't. A system message is (kzyb reads msg.user for
// every subtype, build 2cd9270e786c): a hidden person's joins draw
// HiddenMessageBody under the system glyph (row.go hiddenSystem).
func hiddenSender(h Host, m *core.Message, ch string) (user, unhidden bool) {
	app := h.Core()
	if app == nil || m.User == "" || m.Tombstone || m.Pending {
		return false, false
	}
	if !app.UserHidden(m.User) {
		return false, false
	}
	if m.Channel != "" {
		ch = m.Channel
	}
	return true, app.MessageUnhidden(ch, m.TS)
}

// hidChan is the message's channel (the list's when the message doesn't
// carry one), as hiddenSender keyed it.
func (r *row) hidChan() string {
	if r.m.Channel == "" && r.rc != nil && r.rc.list != nil {
		return r.rc.list.ch
	}
	return r.m.Channel
}

// hideSender swaps in the hidden sender's name for this frame: "This
// message is hidden" until shown anyway, then "Name hidden"; no custom
// status or huddle. The returned func restores what resolve set.
func (r *row) hideSender(rc *rowCtx) func() {
	name, status, huddle := r.name, r.statusURL, r.huddle
	r.name = msgNS.T("Name hidden")
	if !rc.unhidden {
		r.name = msgNS.T("This message is hidden")
	}
	r.statusURL, r.huddle = "", ""
	return func() { r.name, r.statusURL, r.huddle = name, status, huddle }
}

// hiddenDoc builds the contents line: the plain copy, or with the
// viewer mentioned the rich string, whose <b> is bold. It is rebuilt
// when the message or the locale changes.
func (r *row) hiddenDoc(rc *rowCtx) {
	mention := false
	if app := rc.h.Core(); app != nil {
		if r.hidRev != r.rev || !r.hidChecked {
			r.hidMention = app.MentionsViewerDirectly(&r.m)
			r.hidRev, r.hidChecked = r.rev, true
		}
		mention = r.hidMention
	}
	var spans []richtext.Span
	if mention {
		for _, p := range msgNS.RT("Someone you hid <b>mentioned you</b> in this message.") {
			sp := richtext.Span{Kind: richtext.Text, Text: p.Text}
			if p.Tag == "b" {
				sp.Style = richtext.Bold
			}
			spans = append(spans, sp)
		}
	} else {
		spans = []richtext.Span{{Kind: richtext.Text, Text: msgNS.T("You’re hiding messages from this person.")}}
	}
	key := ""
	for _, sp := range spans {
		key += sp.Text + "\x00"
	}
	if r.hidView == nil {
		r.hidView = richview.New(richtext.Doc{}, richview.Options{})
	}
	if key != r.hidKey {
		r.hidKey = key
		r.hidView.Set(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: spans}}}, richview.Options{})
	}
}

// hiddenColumn is the body column of a hidden sender's message not shown
// anyway: HiddenMessageBody, then MessageTombstoneContent's reply bar.
func (r *row) hiddenColumn(gtx layout.Context) layout.Dimensions {
	rc := r.rc
	m := &r.m
	cw := gtx.Constraints.Max.X
	if r.hidLink.Clicked(gtx) {
		if app := rc.h.Core(); app != nil {
			app.SetMessageUnhidden(r.hidChan(), m.TS, m.User, true) // unhideMessage
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	r.reactRects = r.reactRects[:0]
	r.addRect = image.Rectangle{}
	r.hiddenDoc(rc)
	st := *rc.st
	st.Palette.Text = rc.c.Muted70 // .c-message_kit__tombstone__text
	r.handler.h, r.handler.m, r.handler.anchor = rc.h, m, rc.origin
	contents := func(gtx layout.Context, maxW int) (op.CallOp, layout.Dimensions) {
		rec := op.Record(gtx.Ops)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(max(maxW, 1), 1<<20)}
		d := r.hidView.Layout(g, &st, &r.handler)
		return rec.Stop(), d
	}
	lh := dp(gtx, mt.nameLine)
	linkMB := dp(gtx, 8) // .c-message_kit__hidden__message__link
	y := 0
	if rc.dense {
		// inline-blocks on one line (wrapping whole when they don't fit),
		// from the column's left edge: no .c-message_kit__indent here
		x0 := -dp(gtx, mt.indent)
		r.nameX = x0
		w := cw - x0
		rec := op.Record(gtx.Ops)
		nd := r.nameLineGap(gtx, false, 0)
		name := rec.Stop()
		placeCall(gtx, name, x0, 0)
		r.nameBL = nd.Size.Y - nd.Baseline
		x := x0 + nd.Size.X + nbspWidth(gtx, rc.th)
		call, d := contents(gtx, w)
		if x+d.Size.X > cw && x > x0 {
			x, y = x0, y+lh
		}
		placeCall(gtx, call, x, y)
		r.bodyRect = image.Rectangle{Min: image.Pt(x, y), Max: image.Pt(x+d.Size.X, y+d.Size.Y)}.Add(r.col)
		x += d.Size.X + nbspWidth(gtx, rc.th) // the " " before the link
		rec = op.Record(gtx.Ops)
		ld := r.hiddenLink(gtx, msgNS.T("Show anyway"), "caret-right-full")
		link := rec.Stop()
		if d.Size.Y > lh {
			// a contents box that wrapped is as wide as the line: the
			// link goes under its last line
			x, y = x0, y+d.Size.Y
		} else if x+ld.Size.X > cw && x > x0 {
			x, y = x0, y+lh
		}
		placeCall(gtx, link, x, y)
		y += lh + linkMB
		y += dp(gtx, mt.bodyMarginBottom) // .c-message_kit__tombstone__text--compact
	} else {
		nd := r.nameLineGap(gtx, true, dp(gtx, mt.tsGap)+nbspWidth(gtx, rc.th)) // " ", then "  " before the time
		r.nameBL = nd.Size.Y - nd.Baseline
		y += max(nd.Size.Y, lh)
		call, d := contents(gtx, cw)
		placeCall(gtx, call, 0, y)
		r.bodyRect = image.Rectangle{Min: image.Pt(0, y), Max: image.Pt(d.Size.X, y+d.Size.Y)}.Add(r.col)
		y += max(d.Size.Y, lh)
		at(gtx, 0, y, func(gtx layout.Context) layout.Dimensions {
			return r.hiddenLink(gtx, msgNS.T("Show anyway"), "caret-right-full")
		})
		y += lh + linkMB
	}
	y += r.hiddenReplyBar(gtx, y, cw)
	return layout.Dimensions{Size: image.Pt(cw, y)}
}

// hideMessageLink is the shown-anyway message's "Hide message" line
// under its body (hideMessage: isUnhidden false); returns its height.
func (r *row) hideMessageLink(gtx layout.Context, y int) int {
	rc := r.rc
	m := &r.m
	if r.hidLink.Clicked(gtx) {
		if app := rc.h.Core(); app != nil {
			app.SetMessageUnhidden(r.hidChan(), m.TS, m.User, false)
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	x := 0
	if rc.dense {
		x = -dp(gtx, mt.indent)
	}
	at(gtx, x, y, func(gtx layout.Context) layout.Dimensions {
		return r.hiddenLink(gtx, msgNS.T("Hide message"), "caret-down-full")
	})
	return dp(gtx, mt.nameLine) + dp(gtx, 8)
}

// hiddenReplyBar is MessageTombstoneContent's replyBar: the reply bar
// the body would have drawn.
func (r *row) hiddenReplyBar(gtx layout.Context, y, cw int) int {
	m := &r.m
	if r.rc.thread || (m.ThreadTS != "" && m.ThreadTS != m.TS) || !r.replyBarWanted(m.ReplyCount) {
		return 0
	}
	d := at(gtx, 0, y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
		return slackc.Component(gtx, "ThreadReplies", r, (*row).repliesProps, (*row).layoutThreadReplies)
	})
	return d.Size.Y
}

// hiddenLink is HiddenMessageBody's link (a Link rendered as a span,
// c-link: the link colour, underlined under the pointer): txt, a no-break
// space, then an inline SvgIcon ([data-crz] 1em square, .is-inline
// vertical-align -.125em) in the link's colour.
func (r *row) hiddenLink(gtx layout.Context, txt, icon string) layout.Dimensions {
	rc := r.rc
	c := rc.c
	r.hidLinkText = txt
	return r.hidLink.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		s := textStyle{size: 15, col: c.Link, lineH: 22}
		d := lineBox(gtx, s, func(gtx layout.Context) layout.Dimensions { return label(gtx, rc.th, s, txt+" ") })
		base := d.Size.Y - d.Baseline
		em := dp(gtx, 15)
		at(gtx, d.Size.X, base+em/8-em, func(gtx layout.Context) layout.Dimensions {
			return icons.Get(icon)(gtx, unit.Dp(15), c.Link)
		})
		if r.hidLink.Hovered() {
			fill(gtx.Ops, image.Rect(0, base+dp(gtx, 1), d.Size.X, base+dp(gtx, 2)), c.Link)
		}
		size := image.Pt(d.Size.X+em, d.Size.Y)
		defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		return layout.Dimensions{Size: size, Baseline: d.Baseline}
	})
}

// hiddenAvatarDrawn is what replyAv.drawn records for HiddenUserAvatar.
const hiddenAvatarDrawn = "hidden-user"

// drawHiddenAvatar is HiddenUserAvatar in rect: .c-hidden_user_avatar's
// rgba(foreground_soft, .06) square (the reaction pill's fill in both
// modes), radius 4px, the user-hide icon centred in rgba(foreground_max,
// .7): 24px, or 16px for an avatar up to 24px.
func drawHiddenAvatar(gtx layout.Context, rect image.Rectangle, c *colors) {
	rrect(gtx.Ops, rect, dp(gtx, 4), c.ReactBg)
	is := float32(24)
	if rect.Dx() <= dp(gtx, 24) {
		is = 16
	}
	ip := dp(gtx, is)
	at(gtx, rect.Min.X+(rect.Dx()-ip)/2, rect.Min.Y+(rect.Dy()-ip)/2, func(gtx layout.Context) layout.Dimensions {
		return icons.Get("user-hide")(gtx, unit.Dp(is), c.Muted70)
	})
}
