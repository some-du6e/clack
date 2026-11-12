package views

import (
	"context"
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The page's hero: HuddlesPageNewUserHero (Igs2) in Slack's HeroBanner
// (shared-js_modern_community_hero-banner chunk, m8E3), its CSS from
// client-boot-styles:
//
//   - hidden while users.interactions.list loads, once
//     EXTERNAL_HUDDLES_AWARENESS_BANNER or HUDDLE_PAGE_BANNER has a
//     Dismiss, and after its close button (which records
//     HUDDLE_PAGE_BANNER Dismiss with users.interactions.set);
//   - the banner: plt-mojito-0, 1px otl-ter above and below, padding 4px,
//     a 12px band (heroBannerFocusWrapper), then a centred row at most
//     968 wide with a 20px gap: the text (60%, at least 200 tall, padding
//     44px 0, plus 44px on the left up to 1124 wide; the whole width
//     under 768) and the picture (400 wide, only above 768: Slack's
//     huddles_page_hero PNG, at most 360 wide; Slack plays its Lottie
//     in its place while the a11y_animations pref is on, which Clack
//     can't play);
//   - "Instantly connect over audio or video" (22px/30px 900, 8px
//     under), the body (15px/22px at foreground_max .7, p's 16px below)
//     and the primary "Start a Huddle" with the headphones (20px, 1px
//     down) that opens HuddleInviteStartModal (entry point
//     huddles_page_hero_banner); the close button (36px, close icon) 16px
//     from the top right.
//
// The external-huddles variants (isEligibleForExternalHuddlesAwareness)
// need external_huddles and the MANAGE_EXTERNAL_HUDDLES permission, which
// Clack doesn't read, so the page shows this one.

// hdHeroPNG is the hero's picture (806×440), from Slack's CDN.
const hdHeroPNG = "https://a.slack-edge.com/bv1-13-br/huddles_page_hero-e8cc7fe.png"

// huddleStartOpenerFor is the ui root's HuddleInviteStartModal with a
// suggestion picked (channelSuggestion / memberSuggestionId).
type huddleStartOpenerFor interface {
	OpenHuddleStartWith(entry, channel, member string)
}

// openStart is HuddleInviteStartModal (Oh2x) from one of the page's
// entry points, with the suggestion it came from picked.
func (v *Huddles) openStart(entry, channel, member string) {
	if o, ok := v.h.(huddleStartOpenerFor); ok {
		o.OpenHuddleStartWith(entry, channel, member)
		return
	}
	if o, ok := v.h.(huddleStartOpener); ok {
		o.OpenHuddleStart(entry)
		return
	}
	newHuddle(v.h, v.tt)
}

func (v *Huddles) layoutHero(gtx C) D {
	w := gtx.Constraints.Max.X
	none := D{Size: image.Pt(w, 0)}
	v.mu.Lock()
	loaded := v.interOK
	v.mu.Unlock()
	if !loaded || v.heroOff || v.dismissed(hdBannerExternal) || v.dismissed(hdBannerPage) {
		return none
	}
	if v.clk("hero-start").Clicked(gtx) {
		v.openStart("huddles_page_hero_banner", "", "")
	}
	if v.clk("hero-close").Clicked(gtx) {
		v.heroOff = true
		app := v.h.Core()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_ = app.RecordUserInteraction(ctx, hdBannerPage, hdDismiss)
		}()
		v.mu.Lock()
		if v.inter == nil {
			v.inter = map[string]bool{}
		}
		v.inter[hdBannerPage+"-"+hdDismiss+"-DEFAULT"] = true
		v.mu.Unlock()
		return none
	}

	bg := v.tok("dt_color-plt-mojito-0")
	px1 := max(1, gtx.Dp(1))
	pad := gtx.Dp(4)
	band := gtx.Dp(12)
	cw := min(w-2*pad, gtx.Dp(968))
	wide := w > gtx.Dp(768)
	// the two columns' widths (flex: 60% and 400, gap 20; shrunk in
	// proportion when they don't fit, the picture no narrower than it is)
	leftW, rightW, gap := cw, 0, 0
	if wide {
		leftW, rightW, gap = cw*60/100, gtx.Dp(400), gtx.Dp(20)
		if over := leftW + gap + rightW - cw; over > 0 {
			minRight := gtx.Dp(360)
			if w <= gtx.Dp(1124) {
				minRight += gtx.Dp(44)
			}
			cut := over * rightW / (leftW + rightW)
			rightW = max(minRight, rightW-cut)
			leftW = cw - gap - rightW
		}
	}
	padL, padR := 0, 0
	if w <= gtx.Dp(1124) && wide {
		padL = gtx.Dp(44)
	}
	if !wide {
		padL, padR = gtx.Dp(44), gtx.Dp(44)
	}
	textW := max(0, leftW-padL-padR)
	// the text column, recorded to measure it
	fg := v.tok("dt_color-content-pry")
	body := flatten(theme.WithAlpha(fg, 0xb3), bg) // rgba(foreground_max, .7)
	m := recordTo(gtx, func(gtx C) D {
		gtx.Constraints = layout.Constraints{Max: image.Pt(textW, 1<<20)}
		return col(gtx,
			layout.Rigid(func(gtx C) D {
				d := v.para(gtx, 22, 30, wBlack, fg, 0, nsHuddles.T("Instantly connect over audio or video"))
				return D{Size: image.Pt(d.Size.X, d.Size.Y+gtx.Dp(8))}
			}),
			layout.Rigid(func(gtx C) D {
				d := v.para(gtx, 15, 22, wReg, body, 0, nsHuddles.T("Talk it out in real time on a huddle, with screen-sharing, expressive reactions and a message thread that automatically saves for later reference."))
				return D{Size: image.Pt(d.Size.X, d.Size.Y+gtx.Dp(16))}
			}),
			layout.Rigid(func(gtx C) D {
				return v.primaryButton(gtx, v.clk("hero-start"), "headphones", nsHuddles.T("Start a Huddle"))
			}),
		)
	})
	leftH := max(gtx.Dp(200), m.d.Size.Y+2*gtx.Dp(44))
	imgW := 0
	imgH := 0
	if wide {
		rpad := 0
		if w <= gtx.Dp(1124) {
			rpad = gtx.Dp(44)
		}
		imgW = min(gtx.Dp(360), max(0, rightW-rpad))
		imgH = imgW * 440 / 806
	}
	rowH := max(leftH, imgH)
	h := px1 + pad + band + rowH + band + pad + px1
	fill(gtx, image.Rect(0, 0, w, h), bg)
	line := v.tokOver("dt_color-otl-ter", bg)
	fill(gtx, image.Rect(0, 0, w, px1), line)
	fill(gtx, image.Rect(0, h-px1, w, h), line)
	total := leftW + gap + rightW
	x0 := (w-cw)/2 + (cw-total)/2
	y0 := px1 + pad + band
	func() {
		defer opOffset(gtx, image.Pt(x0+padL, y0+(rowH-leftH)/2+(leftH-m.d.Size.Y)/2)).Pop()
		m.call.Add(gtx.Ops)
	}()
	if wide && imgW > 0 {
		ix := x0 + leftW + gap + (rightW-imgW)/2
		if w <= gtx.Dp(1124) {
			ix = x0 + leftW + gap + (rightW-gtx.Dp(44)-imgW)/2
		}
		func() {
			defer opOffset(gtx, image.Pt(ix, y0+(rowH-imgH)/2)).Pop()
			v.cdnImage(gtx, hdHeroPNG, image.Pt(imgW, imgH))
		}()
	}
	// the close button (GfK5 icon button, size medium) 16px in
	func() {
		s := gtx.Dp(36)
		defer opOffset(gtx, image.Pt(w-gtx.Dp(16)-s, px1+gtx.Dp(16))).Pop()
		v.iconButton(gtx, v.clk("hero-close"), "close", 36, 20, v.tok("dt_color-content-sec"))
	}()
	return D{Size: image.Pt(w, h)}
}

// recorded is a widget laid out into a macro.
type recorded struct {
	d    D
	call op.CallOp
}

// recordTo lays w out without drawing it, for measuring first.
func recordTo(gtx C, w W) recorded {
	m := op.Record(gtx.Ops)
	d := w(gtx)
	return recorded{d: d, call: m.Stop()}
}

// primaryButton is c-button--primary medium: 36px, padding 0 12px 1px,
// radius 8, base-inv-hgl-2, 15px bold content-inv-pry, an optional 20px
// icon (heroBannerButtonIcon: 20 wide, 1px down) before the label.
func (v *Huddles) primaryButton(gtx C, c *widget.Clickable, icon, label string) D {
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		tw := v.th.Measure(gtx, 15, wBold, label)
		iw := 0
		if icon != "" {
			iw = gtx.Dp(20)
		}
		sz := image.Pt(max(gtx.Dp(80), tw+iw+2*gtx.Dp(12)), gtx.Dp(36))
		bg := v.tok("dt_color-base-inv-hgl-2")
		if c.Hovered() {
			bg = v.p.GreenHover
		}
		rrect(gtx, image.Rectangle{Max: sz}, gtx.Dp(8), bg)
		fg := v.tok("dt_color-content-inv-pry")
		x := (sz.X - tw - iw) / 2
		if icon != "" {
			func() {
				defer opOffset(gtx, image.Pt(x, (sz.Y-gtx.Dp(20))/2+gtx.Dp(1))).Pop()
				v.icon(gtx, icon, 20, fg)
			}()
			x += iw
		}
		func() {
			defer opOffset(gtx, image.Pt(x, 0)).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(tw, sz.Y-gtx.Dp(1)))
			layout.W.Layout(g, func(gtx C) D { return v.label(gtx, 15, wBold, fg, label) })
		}()
		return D{Size: sz}
	})
}

// cdnImage draws one of Slack's CDN pictures at sz (nothing until it
// loads).
func (v *Huddles) cdnImage(gtx C, url string, sz image.Point) D {
	if ic := v.h.Images(); ic != nil {
		if img, ok := ic.Get(url, max(sz.X, sz.Y)); ok {
			return theme.Image(gtx, img, sz, 0)
		}
	}
	return D{Size: sz}
}
