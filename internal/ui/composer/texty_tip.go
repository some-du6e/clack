package composer

// Texty's tooltips over the composer's atoms: TextyInput's onMouseOver on
// a .c-member_slug, an img.emoji, a ts-slug or a link calls showTooltip
// (none while text is selected) and TextyTooltip (boot-shared, build
// 32bb3cc8205b, read from the public CDN without a cookie) picks what
// opens, in this order:
//
//   - a person (getMemberIsHuman: a member who isn't an app or a bot):
//     renderHoverCard(renderMemberProfileHoverCard({memberId,
//     hideButtons, hideCoveragePlanLink})), a Popover at "top" over the
//     slug with offsetY -12 and allowance 0. Clack draws it through the
//     root's hover card (hovercard Request.Texty, popups'
//     NewTextyHoverCard), so it opens at once, keeps while the pointer is
//     on the card and closes 250ms after it leaves both
//     (hideTooltipDelayed).
//   - a channel: renderHoverCard(ChannelHoverCard) without buttons, in
//     the same Popover (hovercard Request.ChannelID with Texty: at once,
//     kept while the pointer is on the card, closed 250ms after it leaves
//     both; popups.ChannelHoverCard).
//   - an app or bot member: renderComposerAppHoverCard, whose agent card
//     needs revamp_agent_app_hover_card (popups.MemberTipApplies), so
//     its fallback: a Popover at "top" (allowance 0, no delay) with the
//     c-tooltip__tip--member tip around SmallMemberEntity (avatar, name,
//     APP badge, presence: popups.MemberTip); mouseout hides it at once.
//   - a user group: TextyTooltipUserGroup (group_tip.go).
//   - anything else with a data-title: a tooltip with the title. An
//     emoji's is its text (SlackEmoji.create: data-title ":smile:"), in a
//     Popover at "top" with allowance 0 opened from the mouseover (no
//     delay), the c-tooltip__tip--top with its arrow; mouseout hides it
//     at once (no hover card in the document).

import (
	"image"
	"strings"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// textyTip is the composer's title tooltip state (an emoji's :name:).
type textyTip struct {
	tip  popups.TipState
	mtip popups.TipState // an app's or bot's member tooltip
}

// textyKey identifies a slug's hover card trigger across frames.
type textyKey struct {
	c  *Composer
	id string
}

// layoutTextyTip shows what Texty shows for the atom under the pointer,
// the editor laid out at the origin.
func (c *Composer) layoutTextyTip(gtx layout.Context) {
	c.layoutGroupTip(gtx)
	at, r, ok := c.ed.HoverAtom()
	th := c.h.Theme()
	t := &c.ttip
	t.tip.Texty = true
	if ok && at.Kind == AtomUser {
		c.reportSlugCard(at, r)
	}
	if ok && at.Kind == AtomChannel {
		c.reportChannelCard(at, r)
	}
	c.layoutMemberTip(gtx, at, r, ok)
	if !ok || at.Kind != AtomEmoji {
		popups.Tooltip(gtx, th, &t.tip, false, r.Size(), popups.Above, "", "")
		return
	}
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	popups.Tooltip(gtx, th, &t.tip, true, r.Size(), popups.Above, emojiTitle(at), "")
}

// reportSlugCard reports a person's slug to the root's hover card
// (TextyTooltip: renderMemberProfileHoverCard for a human member).
func (c *Composer) reportSlugCard(at *Atom, r image.Rectangle) {
	app := c.api.a
	ctl := hovercard.Default()
	if app == nil || ctl == nil || at.ID == "" {
		return
	}
	if u, ok := app.User(at.ID); !ok || u.IsBot {
		return // getMemberIsHuman: unknown or an app/bot member
	}
	pt, in := c.ed.HoverPoint()
	if !in {
		return
	}
	ctl.ReportTexty(textyKey{c, at.ID}, at.ID, r, f32.Pt(float32(pt.X), float32(pt.Y)))
}

// reportChannelCard reports a channel slug to the root's hover card
// (TextyTooltip: renderHoverCard(ChannelHoverCard)).
func (c *Composer) reportChannelCard(at *Atom, r image.Rectangle) {
	ctl := hovercard.Default()
	if ctl == nil || at.ID == "" {
		return
	}
	pt, in := c.ed.HoverPoint()
	if !in {
		return
	}
	ctl.ReportTextyChannel(textyKey{c, at.ID}, at.ID, r, f32.Pt(float32(pt.X), float32(pt.Y)))
}

// layoutMemberTip shows renderComposerAppHoverCard's fallback tooltip over
// an app's or bot's slug, the editor laid out at the origin.
func (c *Composer) layoutMemberTip(gtx layout.Context, at *Atom, r image.Rectangle, ok bool) {
	t := &c.ttip
	t.mtip.Texty = true
	th := c.h.Theme()
	app := c.api.a
	show := ok && at.Kind == AtomUser && at.ID != "" && app != nil
	if show {
		// not getMemberIsHuman: an app or bot, or a member not loaded yet
		// (SmallMemberEntity's MissingText)
		u, known := app.User(at.ID)
		show = !known || popups.MemberTipApplies(u)
	}
	if !show {
		popups.TooltipBox(gtx, th, &t.mtip, false, image.Point{}, popups.Above, nil)
		return
	}
	id := at.ID
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	popups.TooltipBox(gtx, th, &t.mtip, true, r.Size(), popups.Above, func(gtx layout.Context) layout.Dimensions {
		return popups.MemberTip(gtx, c.h, id)
	})
}

// emojiTitle is an emoji atom's data-title: its text (":smile:",
// ":wave::skin-tone-3:").
func emojiTitle(at *Atom) string {
	if strings.HasPrefix(at.Text, ":") {
		return at.Text
	}
	return ":" + at.ID + ":"
}
