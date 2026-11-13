package ui

import (
	"image"
	"image/color"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Messages & media's example messages (gantry-v2-async-prefs-dialog,
// build a84662d2d135): MessageThemePref and MessageNamesPref end with
// "Here’s an example:" (p.p-prefs_dialog__message_example_label: 13px/18,
// rgba(var(--sk_foreground_max),.7), margin 16px 0 12px) over
// .p-prefs_dialog__message_example_pillow (radius 4, padding 12, a 1px
// rgba(var(--sk_foreground_low),.13) ring and 0 1px 3px #00000014). In it,
// under Compact ("dense"), CompactMessageExample:
//
//	<div class="p-prefs_modal__message_example --compact">  (flex, align-items flex-start)
//	  <div class="…__gutter"><span class="c-timestamp">{time}</span></div>
//	  <div class="…__message_content">                      (margin-left 8px)
//	    <span class="c-message__sender">{name}</span>        (margin-left -8px)
//	    <span>{message}</span>
//
// and otherwise MessageWithAvatarExample (4fBz):
//
//	<div class="p-prefs_modal__message_example">             (flex, align-items center)
//	  <div class="…__avatar"><img class="c-avatar__image"></div>  (36×36, margin 0 10px 0 5px)
//	  <div class="…__message_body">
//	    <span class="c-message__sender">{name}</span><span class="c-timestamp">{time}</span><br>
//	    <span class="c-message">{message}</span>
//
// .c-timestamp and .c-message__sender have margin-right 7px there; the
// sender is .c-message__sender (900, content-pry), the time .c-timestamp
// (12px, sk_foreground_max_solid) on the 15px/22px line's baseline, and
// the picture .c-avatar__image (radius 4px). {time} is now ("{time}",
// which follows time24).

// The pictures the examples load from Slack's CDN (webpack assets of the
// prefs chunk: SquF slackbot_72, avatar_meg@2x).
const (
	exampleSlackbot = "https://a.slack-edge.com/bv1-13-br/slackbot_72-5b8ea22.png"
	exampleMeg      = "https://a.slack-edge.com/bv1-13-br/avatar_meg@2x-f702276.png"
)

// examplePart is a run of an example message: text, an emoji (":wave:"
// as .c-emoji__medium, 22px) or a member slug (.c-member_slug--link).
type examplePart struct {
	text, emoji string
	slug        bool
}

// messageExample draws the label and pillow for one example.
func (p *prefsModal) messageExample(f *prefsForm, dense bool, name, avatar string, msg []examplePart) {
	gtx, a := f.gtx, p.a
	pc := p.colors()
	f.record("text", "", pt("Here’s an example:"), false, true)
	f.y += gtx.Dp(16)
	at(gtx, image.Pt(0, f.y), image.Pt(f.w, gtx.Dp(18)+gtx.Dp(2)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Layout(gtx, theme.TextStyle{Size: 13, LineHeight: 18, Color: pc.on(a.th, pc.note), MaxLines: 1}, pt("Here’s an example:"))
	})
	f.y += gtx.Dp(18) + gtx.Dp(12)
	kind := "example-clean"
	if dense {
		kind = "example-compact"
	}
	f.record(kind, "", name, dense, true)

	pad := gtx.Dp(12)
	lh := gtx.Dp(22)
	inner := 2 * lh // sender line + message line
	if dense {
		inner = lh
	}
	h := inner + 2*pad
	r := image.Rect(0, f.y, f.w, f.y+h)
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(1))), gtx.Dp(4), gtx.Dp(3), hex8(0x00000014))
	theme.Border(gtx, r, gtx.Dp(4), max(1, gtx.Dp(1)), pc.on(a.th, pc.line))

	senderCol := pc.text
	tsCol := hex8(0x616061ff) // rgba(var(--sk_foreground_max_solid)) (.c-timestamp)
	if a.th.Dark {
		tsCol = hex8(0xababadff)
	}
	when := uipref.Clock(time.Now())
	gap := gtx.Dp(7)
	bl := cssBaselinePx(gtx, 15, 22)
	x, y := pad, f.y+pad
	if dense {
		tw := exampleText(gtx, a.th, x, y+bl, 12, theme.Regular, tsCol, when)
		x += tw + gap // the gutter, then the content's 8px margin less the sender's -8px
	} else {
		as := gtx.Dp(36)
		ax := x + gtx.Dp(5)
		ay := y + (inner-as)/2
		if img, ok := a.images.Get(avatar, as*2); ok {
			at(gtx, image.Pt(ax, ay), image.Pt(as, as), func(gtx layout.Context) layout.Dimensions {
				return theme.Image(gtx, img, image.Pt(as, as), gtx.Dp(4))
			})
		}
		x = ax + as + gtx.Dp(10)
	}
	x += exampleText(gtx, a.th, x, y+bl, 15, theme.Black, senderCol, name) + gap
	if !dense {
		exampleText(gtx, a.th, x, y+bl, 12, theme.Regular, tsCol, when)
		x = pad + gtx.Dp(5) + gtx.Dp(36) + gtx.Dp(10)
		y += lh
	}
	for _, pt := range msg {
		switch {
		case pt.emoji != "":
			es := gtx.Dp(22) // .c-emoji__medium, vertical-align top
			if co := a.Core(); co != nil {
				if url, _, _ := co.Emoji(pt.emoji); url != "" {
					if img, ok := a.images.Get(url, es); ok {
						at(gtx, image.Pt(x, y), image.Pt(es, es), func(gtx layout.Context) layout.Dimensions {
							return theme.Image(gtx, img, image.Pt(es, es), 0)
						})
					}
				}
			}
			x += es
		case pt.slug:
			// .c-member_slug--link: content-hgl-1 on surf-hgl-1, padding
			// 1px 2px, radius 3px
			fg, sbg := hex8(0x1264a3ff), hex8(0x36c5f021)
			if a.th.Dark {
				fg, sbg = hex8(0x2ba5ceff), hex8(0x03669640)
			}
			tw := a.th.Measure(gtx, 15, theme.Regular, pt.text)
			p2 := gtx.Dp(2)
			asc := int(0.987*15*gtx.Metric.PxPerSp + 0.5)
			desc := int(0.213*15*gtx.Metric.PxPerSp + 0.5)
			top := y + bl - asc - gtx.Dp(1)
			theme.RRect(gtx, image.Rect(x, top, x+tw+2*p2, y+bl+desc+gtx.Dp(1)), gtx.Dp(3), sbg)
			x += p2
			x += exampleText(gtx, a.th, x, y+bl, 15, theme.Regular, fg, pt.text) + p2
		default:
			x += exampleText(gtx, a.th, x, y+bl, 15, theme.Regular, pc.text, pt.text)
		}
	}
	f.y += h
}

// exampleText draws one line of txt with its baseline at (x, base) and
// returns its width.
func exampleText(gtx layout.Context, th *theme.Theme, x, base int, size unit.Sp, w font.Weight, col color.NRGBA, txt string) int {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	d := th.Label(g, size, w, col, txt)
	call := m.Stop()
	defer op.Offset(image.Pt(x, base-(d.Size.Y-d.Baseline))).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
	return d.Size.X
}

// cssBaselinePx is where Chrome puts a size/lineH Lato line's baseline
// from the line box top (half the leading, then the ascent; Lato's hhea
// 1974/-426 of 2000).
func cssBaselinePx(gtx layout.Context, size, lineH float32) int {
	sp := gtx.Metric.PxPerSp
	return int((lineH*sp-(0.987+0.213)*size*sp)/2 + 0.987*size*sp + 0.5)
}
