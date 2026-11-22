package messages

// The custom status's tooltip beside a sender's name: BaseMessageSender
// (boot-shared AC4g, CDN build a09b625d90aa) renders BaseCustomStatus
// (client-boot X/OW) with its defaults: tooltip on, Tooltip (HzHE
// BaseTooltip: position top, delay 150 ms) around .c-custom_status. The
// tip (BaseCustomStatusTooltipContent in .c-custom_status__tooltip):
//
//   - the status emoji (BaseCustomStatusEmoji, SMALL: 16px), then the
//     status text 4px after it (margin_left_25; fWSH: a preset's text by
//     status_text_canonical, "On a call", "In a huddle" for a huddle
//     shown as the status, else status_text); with no text, the emoji's
//     own name (displayEmojiTextAsFallbackIfNoText);
//   - TooltipSubtitle: BaseCustomStatusExpiration's "Until
//     {expirationDate}" (hbqN: status_expiration; none for a huddle).
//
// useMemberCustomStatusExpired hides the status altogether once its
// expiration has passed (row.go skips the emoji). The tooltip is drawn by
// the list (list.go) where the pointer found the icon, the way the
// reaction pills' are: it shows the same whether or not a plugin patched
// MessageSender or BaseMessageSender, since BaseMessageSender's native
// draws the status.

import (
	"image"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// statusTipDelay is BaseTooltip's default delay.
const statusTipDelay = 150 * time.Millisecond

// statusTipOf is the tooltip's text (fWSH), the emoji's name for a status
// without text, and the expiration (hbqN) of u's custom status.
func statusTipOf(u *core.User) (text, name string, exp int64) {
	text = strings.TrimSpace(statusUnescape(u.StatusText))
	if c := u.StatusCanonical; c != "" && strings.EqualFold(c, "on a call") {
		text = i18n.T("members", "On a call")
	}
	name = strings.Trim(u.StatusEmoji, ":")
	if i := strings.Index(name, "::"); i >= 0 { // a skin tone: the emoji's own name
		name = name[:i]
	}
	return text, name, u.StatusExp
}

var statusUnescapeR = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", "\"", "&#39;", "'")

// statusUnescape is lodash's unescape (BaseCustomStatusText's
// inputIsEscaped).
func statusUnescape(s string) string { return statusUnescapeR.Replace(s) }

// statusArea is BaseCustomStatus's Tooltip trigger over the status (or
// huddle) icon at rect: it follows the pointer over the icon, letting it
// through to the row and the name below.
func (r *row) statusArea(gtx layout.Context, rect image.Rectangle) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &r.statusTag, Kinds: pointer.Enter | pointer.Leave | pointer.Move | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Enter, pointer.Move:
			r.statusHov, r.statusPt = true, e.Position
		case pointer.Leave, pointer.Cancel:
			r.statusHov = false
		}
	}
	r.statusSize, r.statusDrawn = rect.Size(), true
	t := op.Offset(rect.Min).Push(gtx.Ops)
	a := clip.Rect{Max: rect.Size()}.Push(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, &r.statusTag)
	pass.Pop()
	a.Pop()
	t.Pop()
}

// statusTip is a hovered status's tooltip content.
type statusTip struct {
	url, text, sub string
}

// statusTipFor is r's hovered icon's tooltip: the huddle's "In a huddle",
// else the custom status (text or the emoji's name, and "Until …").
func (r *row) statusTipFor(now time.Time) statusTip {
	if r.huddle != "" {
		return statusTip{text: i18n.T("members", "In a huddle")}
	}
	t := statusTip{url: r.statusURL, text: r.statusText}
	if t.text == "" {
		t.text = r.statusName
	}
	if r.statusExp > 0 {
		t.sub = popups.StatusUntil(r.statusExp, now)
	}
	return t
}

// statusTooltip draws the tip over target (list coordinates) inside
// bounds: .c-tooltip__tip (padding 8px 12px, radius 8, 13px bold, at
// most 200px), the 16px emoji and the text on the first line, the
// subtitle (regular, the tooltip's secondary colour) under it, centred.
func statusTooltip(gtx layout.Context, h Host, th *theme.Theme, c *colors, st statusTip, target, bounds image.Rectangle) {
	tip := slackc.BodyScope().Child(tooltipClass, 0)
	bold := cssText(tip, textStyle{size: 13, weight: theme.Bold, col: c.TooltipText, lineH: 16.25})
	reg := bold
	reg.weight, reg.col = theme.Regular, tooltipSubtitle(th, c, bold.col)
	padX, padY := dp(gtx, 12), dp(gtx, 8)
	maxW := dp(gtx, 200) - 2*padX
	em, gap := 0, 0
	if st.url != "" {
		em, gap = dp(gtx, 16), dp(gtx, 4)
	}
	m := op.Record(gtx.Ops)
	td := flowTip(gtx, th, []tipRun{{st: bold, txt: st.text}}, max(1, maxW-em-gap), tipBars{})
	textCall := m.Stop()
	line1 := image.Pt(em+gap+td.X, max(td.Y, em))
	var sub image.Point
	var subCall op.CallOp
	if st.sub != "" {
		m := op.Record(gtx.Ops)
		sub = flowTip(gtx, th, []tipRun{{st: reg, txt: st.sub}}, maxW, tipBars{})
		subCall = m.Stop()
	}
	w := max(line1.X, sub.X) + 2*padX
	hgt := padY + line1.Y + sub.Y + padY
	pl := theme.PlaceTooltip(gtx, target, image.Pt(w, hgt), bounds, theme.BaseTooltip(gtx, theme.TipTop))
	b := pl.Box
	theme.TooltipShape(gtx, b, dp(gtx, 8), pl.Arrow, tip.Background(c.Tooltip), tipGlow(th))
	x := b.Min.X + (w-line1.X)/2
	y := b.Min.Y + padY
	if st.url != "" {
		drawEmoji(gtx, h, st.url, image.Rect(x, y+(line1.Y-em)/2, x+em, y+(line1.Y-em)/2+em))
	}
	t := op.Offset(image.Pt(x+em+gap, y+(line1.Y-td.Y)/2)).Push(gtx.Ops)
	textCall.Add(gtx.Ops)
	t.Pop()
	if st.sub != "" {
		t := op.Offset(image.Pt(b.Min.X+(w-sub.X)/2, y+line1.Y)).Push(gtx.Ops)
		subCall.Add(gtx.Ops)
		t.Pop()
	}
}
