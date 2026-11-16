package messages

import (
	"image"
	"image/color"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// A member core can't name, as the message list draws one (Slack's rules
// are in internal/core/lazy_users.go). Core loads messages without their
// people and the rows look them up as they draw, so a sender, a mention, a
// reactor or a typer whose member isn't loaded yet is the UnknownFetchManager's
// stub until users/info answers, and core's "names" event draws the row
// again. Karim (2026-11-14, macOS) saw the sender drawn as its raw id in
// that gap, where Slack draws BaseMissingMember (client-boot FToV):
//
//   - MissingText (sSUQ) at its medium size: an empty span.c-missing_text,
//     display inline-block, 120px by 8, border-radius 1000px, so its bottom
//     sits on the line's baseline; .c-missing_text--unknown fills it with
//     --dt_color-base-ter, .c-missing_text--non-existent outlines it with
//     1px of rgb(--sk_foreground_low_solid) (Slack's CSS is border-box
//     throughout); --subtler (the reaction tip's TooltipMemberName) is
//     opacity .4.
//   - its Tooltip (F2XN: above, BaseTooltip's 150 ms): member_token's
//     "Loading…" for the stub, "Private user info" for a non-existent
//     member.
//
// BaseMessageSender returns that alone in the sender's place (no button,
// badge or status) for the stub, even under an override of the name
// ("Name hidden", "This message is hidden"), and for a non-existent member
// when nothing names them. A huddle message's or a custom response's
// sender is a username (getMsgSenderEntity, 33X/: no userId), never a
// member's bar. The id is never drawn.

// missingBarW is MissingText's width: BaseMissingMember passes no size,
// so medium (sSUQ: small 80, medium 120, large 160).
const missingBarW = 120

// missingTipDelay is MissingText's Tooltip delay: BaseTooltip's default.
const missingTipDelay = 150 * time.Millisecond

// missingTip is BaseMissingMember's tooltip for the stub or a
// non-existent member (gone).
func missingTip(gone bool) string {
	if gone {
		return memberTokNS.T("Private user info")
	}
	return memberTokNS.T("Loading…")
}

// paintMissing paints MissingText in r: fill (the unknown stub's base-ter)
// or, for a non-existent member, a 1px line inside r.
func paintMissing(gtx layout.Context, r image.Rectangle, gone bool, fill, line color.NRGBA) {
	rad := r.Dy() / 2
	if gone {
		border(gtx.Ops, r, rad, max(1, dp(gtx, 1)), line)
		return
	}
	rrect(gtx.Ops, r, rad, fill)
}

// missingBar is MissingText's rectangle on a line whose baseline is bl px
// from its top, at most maxW wide.
func missingBar(gtx layout.Context, bl, maxW int) image.Rectangle {
	w := dp(gtx, missingBarW)
	if maxW > 0 {
		w = min(w, maxW)
	}
	return image.Rect(0, bl-dp(gtx, 8), w, bl)
}

// missingName draws BaseMissingMember where a name in style s would go
// (one line, s's line box: lineH, else Slack's line-height 1.46668), and
// returns that line's size and baseline.
func missingName(gtx layout.Context, s textStyle, gone bool, c *colors) layout.Dimensions {
	lh := s.lineH
	if lh <= 0 {
		lh = s.size * 1.46668
	}
	bl := cssBaseline(gtx, s.size, lh)
	h := max(gtx.Sp(unit.Sp(lh)), bl)
	r := missingBar(gtx, bl, gtx.Constraints.Max.X)
	paintMissing(gtx, r, gone, c.Missing, c.MissingLine)
	return layout.Dimensions{Size: image.Pt(r.Max.X, h), Baseline: h - bl}
}

// missingTipState is a sender bar's tooltip: its hover and timing.
type missingTipState struct {
	hovered bool
	tip     popups.TipState
}

// senderMiss is what BaseMessageSender draws instead of the sender, if
// anything: the stub's bar (over a hidden sender's label too), a
// non-existent member's when nothing names them. A huddle message's
// status and a custom response's team are username senders: no member.
func (r *row) senderMiss() core.MemberState {
	if r.hmName || r.custom {
		return core.MemberKnown
	}
	switch r.miss {
	case core.MemberUnknown:
		return core.MemberUnknown
	case core.MemberNonExistent:
		if r.name == "" {
			return core.MemberNonExistent
		}
	}
	return core.MemberKnown
}

// senderBar is BaseMissingMember in the sender's place, on the name's
// 15px/22px line box (the timestamp keeps its baseline), with its tooltip.
func (r *row) senderBar(gtx layout.Context, miss core.MemberState) layout.Dimensions {
	rc := r.rc
	if r.missBar == nil {
		r.missBar = &missingTipState{tip: popups.TipState{Delay: missingTipDelay}}
	}
	mb := r.missBar
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: mb, Kinds: pointer.Enter | pointer.Leave | pointer.Cancel})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			mb.hovered = e.Kind == pointer.Enter
		}
	}
	gone := miss == core.MemberNonExistent
	bl := cssBaseline(gtx, 15, mt.nameLine)
	h := max(gtx.Sp(unit.Sp(mt.nameLine)), bl)
	bar := missingBar(gtx, bl, gtx.Constraints.Max.X)
	paintMissing(gtx, bar, gone, rc.c.Missing, rc.c.MissingLine)
	t := op.Offset(bar.Min).Push(gtx.Ops)
	area := clip.Rect{Max: bar.Size()}.Push(gtx.Ops)
	event.Op(gtx.Ops, mb)
	area.Pop()
	popups.Tooltip(gtx, rc.th, &mb.tip, mb.hovered, bar.Size(), popups.Above, missingTip(gone), "")
	t.Pop()
	r.nameShown = ""
	return layout.Dimensions{Size: image.Pt(bar.Max.X, h), Baseline: h - bl}
}

// tipMissing and tipMissingGone stand for a reactor core can't name in
// the reaction tip's text (TooltipMemberName's subtler BaseMissingMember:
// flowTip draws the bar), the stub and a non-existent member.
const (
	tipMissing     = "\x03"
	tipMissingGone = "\x04"
)

// tipMissingRune reports whether r stands for a missing reactor.
func tipMissingRune(r rune) bool { return r == '\x03' || r == '\x04' }

// subtlerMissing is MissingText's colours at --subtler's opacity .4 over
// the tooltip's background bg, as the browser blends them (sRGB).
func subtlerMissing(c *colors, bg color.NRGBA) (fill, line color.NRGBA) {
	at := func(x color.NRGBA) color.NRGBA {
		x.A = uint8(float32(x.A)*.4 + .5)
		return richview.Over(x, bg)
	}
	return at(c.Missing), at(c.MissingLine)
}

// SenderView is a visible message's sender as the last frame drew it
// (tests): its name, or BaseMissingMember's bar for a member core can't
// name, and the user mentions its text drew as bars.
type SenderView struct {
	TS, User string
	Name     string           // the sender label drawn ("" for the bar)
	Missing  core.MemberState // the bar drawn in the sender's place (MemberKnown: none)
	Mentions []string         // user mentions drawn as BaseMissingMember's bar
	Text     string           // the body's text as laid out (a bar adds none)
	// Body is the body's row-local rect, Slots its Block Kit element
	// slots (an agent block's is its whole block), as laid out.
	Body  image.Rectangle
	Slots []image.Rectangle
}

// Senders lists the senders of the visible messages as of the last frame.
func (v *ChannelView) Senders() []SenderView { return v.s.senders() }

// Senders is ChannelView.Senders for the thread.
func (v *ThreadView) Senders() []SenderView { return v.s.senders() }

func (s *listState) senders() []SenderView {
	var out []SenderView
	for i := s.visible[0]; i < s.visible[1] && i < len(s.items); i++ {
		if s.items[i].kind != itMsg || i >= len(s.heights) || s.heights[i] <= 0 {
			continue
		}
		r := s.rows[s.items[i].key]
		if r == nil {
			continue
		}
		sv := SenderView{TS: r.src.TS, User: r.m.User, Name: r.nameShown}
		if sv.Name == "" {
			sv.Missing = r.senderMiss()
		}
		if r.rv != nil {
			sv.Mentions = r.rv.MissingMembers()
			sv.Text = r.rv.TextRange(0, r.rv.PlainLen())
			sv.Slots = r.rv.ElementSlots()
		}
		sv.Body = r.bodyRect
		out = append(out, sv)
	}
	return out
}
