package huddleui

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/chanent"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The channel the huddle surfaces name, as Slack's client draws it
// (internal/ui/chanent; core/channel_lookup.go has the rules):
//
//   - the invite window's "{sender} in {channel}"
//     (HuddleInviteWindowText), the sidebar card's "{senderName} is
//     inviting you to a huddle in {channelName}" (useHuddleInviteProps,
//     boldIcon), the preview's "Start huddle in" / "Join huddle in"
//     (useGetTitlebarText, boldIcon) and the huddle button's tips
//     (getStartHuddleTip, getTipForUserJoinActiveHuddle) are
//     InlineChannelEntity: lock-filled before a private channel's name
//     (filled whether bold or not), the hash before a public one's (Clack
//     draws "#name"); the loading bar while core looks a channel up; the
//     Private channel chip for one the viewer can't see. The tips are
//     text, so they read getDisplayNameForChannel's "#name" / bare name.
//   - the invite's notification (sharedHuddleNotificationOptions) names
//     the channel with getDisplayNameForChannel, and only one that
//     exists (isExistent): the stub's reads "{memberName} is inviting you
//     to a huddle".
//
// Never the raw id: a channel core can't name is looked up
// (core.InlineChannel); its "names" event redraws the main window (the
// card, the header), the invite window redraws on its own tick.

// entMark stands for the channel in a huddles string until entSpans puts
// the channel's entity there.
const entMark = "\x03"

// channelEnt is ch as InlineChannelEntity draws it; ok false for a DM or
// a group DM, which the huddle surfaces name their own way.
func (c *Controller) channelEnt(ch string) (chanent.Ent, bool) {
	app := c.h.Core()
	if app == nil || ch == "" || strings.HasPrefix(ch, "D") {
		return chanent.Ent{}, false
	}
	if conv, ok := app.Conv(ch); ok && (conv.Kind == "im" || conv.Kind == "mpim") {
		return chanent.Ent{}, false
	}
	return chanent.Of(ch, app.InlineChannel(ch), nil), true
}

// entSpans is spans with the entMark run as e, bold as the run was.
func entSpans(spans []span, e chanent.Ent) []span {
	for i := range spans {
		if spans[i].Text == entMark {
			ent := e
			spans[i] = span{Text: e.Text, Bold: spans[i].Bold, Ent: &ent}
		}
	}
	return spans
}

// splitMark cuts s at entMark: the text before and after the channel.
func splitMark(s string) (pre, post string, ok bool) {
	return strings.Cut(s, entMark)
}

func spanWeight(s span) font.Weight {
	if s.Bold {
		return theme.Bold
	}
	return theme.Regular
}

// spanWidth is s's width at size: its text, or the channel entity it is.
func spanWidth(gtx C, th *theme.Theme, size unit.Sp, s span) int {
	if s.Ent != nil {
		return entWidth(gtx, th, size, spanWeight(s), *s.Ent)
	}
	return labelSize(gtx, th, size, spanWeight(s), s.Text).X
}

// entWidth is e's width drawn at size in wt (drawEnt): InlineChannelIcon
// is 1em with margin-right 2px; the chip's lock and label sit 4px apart
// with 2px of padding after; the bar follows the hash.
func entWidth(gtx C, th *theme.Theme, size unit.Sp, wt font.Weight, e chanent.Ent) int {
	tw := labelSize(gtx, th, size, wt, e.Text).X
	switch {
	case e.Chip:
		return gtx.Sp(size) + gtx.Dp(4) + tw + gtx.Dp(2)
	case e.Loading():
		return tw + gtx.Dp(e.Bar)
	case e.Icon != "":
		return gtx.Sp(size) + gtx.Dp(2) + tw
	}
	return tw
}

// entInk is how a surface colours the entity: the text, and the Private
// channel chip's text and background (BaseMissingChannel's
// rgba(foreground_max, .7) on rgba(foreground_low, .13), or a surface's
// own, as the invite window's surf-inv-pry with the text's colour).
type entInk struct {
	text, chipText, chipBg color.NRGBA
}

// defaultEntInk is entInk over text col.
func defaultEntInk(col color.NRGBA) entInk {
	return entInk{text: col, chipText: theme.WithAlpha(col, 179), chipBg: theme.WithAlpha(col, 33)}
}

// drawEnt draws e at size in wt with its left at x, in a line lineH tall
// from y (the text centred in it, like the line's other labels), and
// returns its width: the icon and the name, the hash and the MissingText
// bar (an 8px pill in base-ter, its bottom on the baseline), the chip.
func drawEnt(gtx C, th *theme.Theme, size unit.Sp, wt font.Weight, ink entInk, e chanent.Ent, x, y, lineH int) int {
	ls := labelSize(gtx, th, size, wt, e.Text)
	ty := y + (lineH-ls.Y)/2
	is := gtx.Sp(size)
	iy := y + (lineH-is)/2
	label := func(x int, col color.NRGBA) {
		txt := e.Text
		at(gtx, image.Pt(x, ty), ls, func(gtx C) D { return th.Label(gtx, size, wt, col, txt) })
	}
	icon := func(x int, col color.NRGBA) {
		name := icons.Slack(e.Icon)
		at(gtx, image.Pt(x, iy), image.Pt(is, is), func(gtx C) D { return icons.Get(name)(gtx, unit.Dp(size), col) })
	}
	switch {
	case e.Chip:
		w := entWidth(gtx, th, size, wt, e)
		theme.RRect(gtx, image.Rect(x, ty, x+w, ty+ls.Y+gtx.Dp(1)), gtx.Dp(3), ink.chipBg)
		icon(x, ink.chipText)
		label(x+is+gtx.Dp(4), ink.chipText)
		return w
	case e.Loading():
		label(x, ink.text)
		base := ty + ls.Y*4/5 // Lato's baseline, about 4/5 down its line
		bw := gtx.Dp(e.Bar)
		theme.RRect(gtx, image.Rect(x+ls.X, base-gtx.Dp(8), x+ls.X+bw, base), gtx.Dp(4), th.C.Skeleton)
		return ls.X + bw
	case e.Icon != "":
		icon(x, ink.text)
		label(x+is+gtx.Dp(2), ink.text)
		return is + gtx.Dp(2) + ls.X
	}
	label(x, ink.text)
	return ls.X
}

// drawSpanLine draws one line of spans from (x, y), lineH tall, at size
// in ink, and returns its width.
func drawSpanLine(gtx C, th *theme.Theme, size unit.Sp, ink entInk, ln []span, x, y, lineH int) int {
	x0 := x
	for _, s := range ln {
		wt := spanWeight(s)
		if s.Ent != nil {
			x += drawEnt(gtx, th, size, wt, ink, *s.Ent, x, y, lineH)
			continue
		}
		if s.Text == "" {
			continue
		}
		ls := labelSize(gtx, th, size, wt, s.Text)
		txt, col := s.Text, ink.text
		at(gtx, image.Pt(x, y+(lineH-ls.Y)/2), ls, func(gtx C) D { return th.Label(gtx, size, wt, col, txt) })
		x += ls.X
	}
	return x - x0
}
