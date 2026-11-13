package slackc

import (
	"image/color"
	"sync"

	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Compositing as Chromium does it. Gio blends in linear light; Chromium
// blends in sRGB, so a translucent color, and everything under CSS
// opacity, comes out too faint over light colors and too strong over
// dark ones when handed to Gio as is (MessageLogger's .62 edit history
// with its rgba(224, 30, 90, .25) words, 2026-11-12). The host painter
// therefore paints:
//
//   - a translucent color with the alpha that lands on Chromium's sRGB
//     blend over the color under it (theme.LinearAlpha), the color under
//     being tracked as boxes paint their backgrounds;
//   - an opacity group flattened: Chromium composites the group's content
//     and blends the result over what is under the group, so an opaque
//     color in it shows as that color at the group's opacity over the
//     color under the group, whatever else the group drew beneath it
//     (text over its own background shows as text). It is painted as that
//     final color, opaque. A translucent color in it gets the group's
//     opacity in its alpha. Images, SVG and native widgets can't be
//     recolored, so they keep Gio's opacity layer.

// underColor is the color under what is painted now.
func (p *painter) underColor() color.NRGBA {
	if p.uset {
		return p.under
	}
	if th := env().Theme; th != nil && th.C.Bg.A == 0xff {
		return th.C.Bg
	}
	return color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
}

// srgb is c as Gio must paint it to land where Chromium's blend does.
func (p *painter) srgb(c color.NRGBA) color.NRGBA {
	if c.A == 0 {
		return c
	}
	if p.gAlpha > 0 && p.gAlpha < 1 {
		if c.A == 0xff {
			return theme.Over(theme.WithAlpha(c, u8(p.gAlpha*255)), p.gUnder)
		}
		return linearAlpha(theme.WithAlpha(c, u8(float32(c.A)*p.gAlpha)), p.underColor())
	}
	if c.A == 0xff {
		return c
	}
	return linearAlpha(c, p.underColor())
}

// linearAlpha is theme.LinearAlpha, remembered: painting asks for the
// same few pairs every frame.
var linearMemo struct {
	sync.Mutex
	m map[[2]color.NRGBA]color.NRGBA
}

func linearAlpha(c, bg color.NRGBA) color.NRGBA {
	k := [2]color.NRGBA{c, bg}
	linearMemo.Lock()
	defer linearMemo.Unlock()
	if v, ok := linearMemo.m[k]; ok {
		return v
	}
	if linearMemo.m == nil || len(linearMemo.m) > 4096 {
		linearMemo.m = map[[2]color.NRGBA]color.NRGBA{}
	}
	v := theme.LinearAlpha(c, bg)
	linearMemo.m[k] = v
	return v
}

// enterGroup starts an opacity group of opacity o inside the current one.
func (p *painter) enterGroup(o float32) {
	o = max(o, 0)
	if p.gAlpha > 0 && p.gAlpha < 1 {
		p.gAlpha *= o
		return
	}
	p.gAlpha, p.gUnder = o, p.underColor()
	if p.gAlpha == 0 {
		p.gAlpha = 1e-6 // opacity 0: nothing shows (u8 rounds it to 0)
	}
}

// coverWith records that bg was just painted over the box: its content is
// painted over the result.
func (p *painter) coverWith(bg color.NRGBA) {
	if bg.A == 0 {
		return
	}
	under := p.underColor()
	a := float32(bg.A) / 255
	if p.gAlpha > 0 && p.gAlpha < 1 {
		a *= p.gAlpha
	}
	p.under, p.uset = theme.Over(theme.WithAlpha(bg, u8(a*255)), under), true
}

// groupLayer pushes the group's opacity as Gio's layer for what can't be
// recolored (images, SVG, native widgets); the returned func pops it.
func (p *painter) groupLayer() func() {
	if !(p.gAlpha > 0 && p.gAlpha < 1) {
		return func() {}
	}
	s := paint.PushOpacity(p.ops, p.gAlpha)
	return s.Pop
}
