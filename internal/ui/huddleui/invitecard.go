package huddleui

import (
	"image"
	"strings"

	"gioui.org/font"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The sidebar's invite card: Slack's HuddleInvite (client-boot FAXx), for
// an invite whose window isn't up (Do Not Disturb, or you closed it). From
// client-boot-styles:
//
//   - .p-huddle_invite: 8px above and below; __content: base-inv-pry,
//     content-inv-pry text, 12px corners and padding, a 1px ring.
//   - __message: the sender's 20px avatar, 8px, then 15px text with the
//     names bold: "Join huddle with {sender}" (a DM or group DM), else
//     "{sender} is inviting you to a huddle in #channel".
//   - __buttons: a column 12px under it, 6px apart, each 26px tall, full
//     width, 8px corners, 13px bold: Join (base-pry, content-ter), Be there
//     soon (until you said so), Decline (surf-inv-pry, content-inv-sec,
//     a 1px ring).
//   - Be there soon puts "Join when you're ready!" (13px, a clock) in a
//     surf-sec strip across the card's top for 6 s.

type cardState struct {
	join, soon, decline widget.Clickable
}

// span is a run of text, bold or not.
type span struct {
	Text string
	Bold bool
}

func (c *Controller) cardInvite() *incoming {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inv != nil && c.inv.card {
		return c.inv
	}
	return nil
}

// cardHeight is the card's height at width w (0: no card).
func (c *Controller) cardHeight(gtx C, th *theme.Theme, w int) int {
	inv := c.cardInvite()
	if inv == nil {
		return 0
	}
	return layoutCard(gtx, th, c, c.view(inv), &c.card, w, true).Size.Y
}

// layoutCard draws (or, measure, only sizes) the card at width w.
func layoutCard(gtx C, th *theme.Theme, c *Controller, v inviteView, st *cardState, w int, measure bool) D {
	if measure {
		m := op.Record(gtx.Ops)
		defer m.Stop()
	}
	k := invInkFor(th)
	pad := gtx.Dp(12)
	outer := gtx.Dp(8)
	x0 := gtx.Dp(8)
	cw := w - 2*x0
	inner := cw - 2*pad
	if inner <= gtx.Dp(40) {
		return D{}
	}
	// the strip
	stripH := 0
	soonNote := v.Note != "" && !v.NoteErr
	if soonNote {
		stripH = gtx.Dp(20) + gtx.Dp(16)
	}
	av := gtx.Dp(20)
	textX := av + gtx.Dp(8)
	lines := wrapSpans(gtx, th, 15, v.CardText, inner-textX)
	lineH := gtx.Sp(22)
	textH := max(av, lineH*len(lines))
	bh := gtx.Dp(26)
	btns := []struct {
		cl      *widget.Clickable
		label   string
		primary bool
	}{{&st.join, "Join", true}}
	if !v.JoiningSoon {
		btns = append(btns, struct {
			cl      *widget.Clickable
			label   string
			primary bool
		}{&st.soon, "Be there soon", false})
	}
	btns = append(btns, struct {
		cl      *widget.Clickable
		label   string
		primary bool
	}{&st.decline, "Decline", false})
	btnsH := len(btns)*bh + (len(btns)-1)*gtx.Dp(6)
	errH := 0
	if v.NoteErr {
		errH = gtx.Dp(28)
	}
	contentH := stripH + pad + textH + gtx.Dp(12) + btnsH + pad
	total := outer + contentH + errH + outer
	if measure {
		return D{Size: image.Pt(w, total)}
	}
	if st.join.Clicked(gtx) {
		c.post(func() { c.JoinInvite(EntrySidebarInvite) })
	}
	if st.soon.Clicked(gtx) {
		c.post(c.JoiningSoon)
	}
	if st.decline.Clicked(gtx) {
		c.post(c.DeclineInvite)
	}
	if !v.NoteUntil.IsZero() {
		gtx.Execute(op.InvalidateCmd{At: v.NoteUntil})
	}
	card := image.Rect(x0, outer, x0+cw, outer+contentH)
	ring := max(1, gtx.Dp(1))
	theme.RRect(gtx, card.Inset(-ring), gtx.Dp(12)+ring, tileRing)
	theme.RRect(gtx, card, gtx.Dp(12), k.bg)
	y := card.Min.Y
	if soonNote {
		func() {
			defer clipRRectTop(gtx, image.Rect(card.Min.X, y, card.Max.X, y+stripH), gtx.Dp(12)).Pop()
			theme.Fill(gtx, image.Rect(card.Min.X, y, card.Max.X, y+stripH), k.surfSec)
		}()
		ls := labelSize(gtx, th, 13, theme.Regular, v.Note)
		is := gtx.Dp(20)
		gw := is + gtx.Dp(4) + ls.X
		gx := card.Min.X + (cw-gw)/2
		at(gtx, image.Pt(gx, y+(stripH-is)/2), image.Pt(is, is), func(gtx C) D { return icons.GetSlack("clock")(gtx, 20, k.text) })
		at(gtx, image.Pt(gx+is+gtx.Dp(4), y+(stripH-ls.Y)/2), ls, func(gtx C) D {
			return th.Label(gtx, 13, theme.Regular, k.text, v.Note)
		})
		y += stripH
	}
	y += pad
	x := card.Min.X + pad
	at(gtx, image.Pt(x, y+(lineH-av)/2), image.Pt(av, av), func(gtx C) D {
		avatar(gtx, th, c.h.Images(), c.avatarURL(v.Sender, av), image.Pt(av, av), gtx.Dp(4))
		return D{Size: image.Pt(av, av)}
	})
	for i, ln := range lines {
		lx := x + textX
		for _, s := range ln {
			wt := theme.Regular
			if s.Bold {
				wt = theme.Bold
			}
			ls := labelSize(gtx, th, 15, wt, s.Text)
			at(gtx, image.Pt(lx, y+i*lineH+(lineH-ls.Y)/2), ls, func(gtx C) D { return th.Label(gtx, 15, wt, k.text, s.Text) })
			lx += ls.X
		}
	}
	y += textH + gtx.Dp(12)
	for _, b := range btns {
		b := b
		at(gtx, image.Pt(x, y), image.Pt(inner, bh), func(gtx C) D {
			return button(gtx, b.cl, image.Pt(inner, bh), func(gtx C, hov bool) {
				r := image.Rectangle{Max: image.Pt(inner, bh)}
				if b.primary {
					bg := k.join
					if hov {
						bg = k.joinHov
					}
					theme.RRect(gtx, r.Inset(-ring), gtx.Dp(8)+ring, tileRing)
					theme.RRect(gtx, r, gtx.Dp(8), bg)
					return
				}
				theme.RRect(gtx, r, gtx.Dp(8), theme.WithAlpha(k.text, 0x4d)) // 1px sk_foreground_mid .3
				bg := k.bg
				theme.RRect(gtx, r.Inset(ring), gtx.Dp(8)-ring, bg)
				fill := k.btn
				if hov {
					fill = k.btnHov
				}
				theme.RRect(gtx, r.Inset(ring), gtx.Dp(8)-ring, fill)
			}, func(gtx C) D {
				col := k.sec
				if b.primary {
					col = k.joinInk
				}
				if v.Busy && !b.primary {
					col.A /= 2
				}
				centerLabel(gtx, th, image.Pt(inner, bh), 13, theme.Bold, col, b.label)
				return D{}
			})
		})
		y += bh + gtx.Dp(6)
	}
	if v.NoteErr {
		ey := card.Max.Y + gtx.Dp(6)
		at(gtx, image.Pt(card.Min.X, ey), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx C) D {
			return icons.GetSlack("warning")(gtx, 16, hudWarn)
		})
		at(gtx, image.Pt(card.Min.X+gtx.Dp(20), ey), image.Pt(cw-gtx.Dp(20), gtx.Dp(20)), func(gtx C) D {
			return th.Label(gtx, 12, theme.Regular, hudWarn, v.Note)
		})
	}
	return D{Size: image.Pt(w, total)}
}

// wrapSpans breaks spans into lines of at most w px at spaces.
func wrapSpans(gtx C, th *theme.Theme, size unit.Sp, spans []span, w int) [][]span {
	type word struct {
		text string
		bold bool
	}
	var words []word
	for _, s := range spans {
		parts := strings.SplitAfter(s.Text, " ")
		for _, p := range parts {
			if p != "" {
				words = append(words, word{p, s.Bold})
			}
		}
	}
	var lines [][]span
	var cur []span
	cw := 0
	weight := func(b bool) font.Weight {
		if b {
			return theme.Bold
		}
		return theme.Regular
	}
	for _, wd := range words {
		ww := labelSize(gtx, th, size, weight(wd.bold), wd.text).X
		if cw > 0 && cw+labelSize(gtx, th, size, weight(wd.bold), strings.TrimRight(wd.text, " ")).X > w {
			lines = append(lines, cur)
			cur, cw = nil, 0
		}
		if n := len(cur); n > 0 && cur[n-1].Bold == wd.bold {
			cur[n-1].Text += wd.text
		} else {
			cur = append(cur, span{wd.text, wd.bold})
		}
		cw += ww
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

// clipRRectTop clips to r with only its top corners rounded.
func clipRRectTop(gtx C, r image.Rectangle, rad int) clip.Stack {
	return clip.RRect{Rect: r, NW: rad, NE: rad}.Push(gtx.Ops)
}
