package popups

// Search in channel's header, loading skeleton and empty state, from
// Slack's client-search-in-channel-view chunk (build d3de3a487b1c):
//
//   - SearchInChannelView's headerTitle: "Search {channelName}" or
//     "Search {keywords} {channelName}" (the search I18n), its pieces flex
//     items 6px apart on a shared baseline
//     (.p-explorer_header__title--search_in_channel). channelName is a
//     13px bold content-ter span (.p-explorer_header__title--channel):
//     "in" (regular, --prefix), then for a DM the person's xx_small (16px)
//     avatar (4px left padding, 7px right margin, its bottom on the line's)
//     and for a channel InlineChannelIcon (N4L8: hash or lock, bold;
//     margin 0 -3px 0 -2px plus the --space 2px), then the name without
//     its # (W+Ke SB). Group DMs get neither.
//   - listLoadingFragment: a MissingText (sSUQ, size small: 80px) in a
//     .p-explorer_result--list, then seven SearchMessageSkeletons (T2HY),
//     each 10px apart. A skeleton is a borderless c-message_group--ia4
//     (padding 12 16 16; .p-explorer_result--message--loading drops the
//     card): a random bar, padding 8; a 36px MissingIcon in the gutter;
//     44px in and 4px up, a medium and a small bar (8px apart), two random
//     bars then one more, a break, two more (4px apart); then, 8px down,
//     two small bars 16px apart. Bars are c-missing_text: 8px tall pills
//     in base-ter, 80 / 120 / 160 wide (random picks one of the three),
//     pulsing 1 → .65 → 1 over a second (c-missing_text--shimmer) unless
//     animations are off (a11y_animations).
//   - the "empty-state" row: .p-explorer_empty_state__with_image (350 tall,
//     its EmptyState centred and, with --inchannel, at the bottom): the
//     image (empty-search-light-b73db25.svg in both modes; max 290×140, 24
//     below), the 18px black title (8 below) and the description with its
//     <strong> run bold.

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"sync"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

//go:embed assets/empty-search-light-b73db25@1x.png
var srcEmptyPNG1x []byte

//go:embed assets/empty-search-light-b73db25@2x.png
var srcEmptyPNG2x []byte

var (
	srcEmptyOnce sync.Once
	srcEmptyImg  [2]paint.ImageOp
	srcEmptyOK   bool
)

// srcEmptyImage is the empty-search illustration rasterised from Slack's
// SVG (rsvg-convert, its drop shadows kept) at 159×140 and twice that.
func srcEmptyImage(pxPerDp float32) (paint.ImageOp, bool) {
	srcEmptyOnce.Do(func() {
		a, err1 := png.Decode(bytes.NewReader(srcEmptyPNG1x))
		b, err2 := png.Decode(bytes.NewReader(srcEmptyPNG2x))
		if err1 != nil || err2 != nil {
			return
		}
		srcEmptyImg = [2]paint.ImageOp{paint.NewImageOp(a), paint.NewImageOp(b)}
		srcEmptyOK = true
	})
	if pxPerDp > 1 {
		return srcEmptyImg[1], srcEmptyOK
	}
	return srcEmptyImg[0], srcEmptyOK
}

// srcHeadPiece is one flex item of the header title.
type srcHeadPiece struct {
	call  op.CallOp
	d     layout.Dimensions
	base  int // baseline from the top
	isCh  bool
	ellip bool
}

// header draws SearchInChannelView's title in w×hh at the origin.
func (v *SearchInChannel) header(gtx layout.Context, th *theme.Theme, cl *Colors, w, hh int) {
	c := v.h.Core()
	if c == nil {
		return
	}
	ref := c.ConvRef(v.ch)
	big := func(s string) srcHeadPiece {
		call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, hh)}
			return text(gtx, th, 18, theme.Black, cl.Fg, s)
		})
		return srcHeadPiece{call: call, d: d, base: d.Size.Y - d.Baseline}
	}
	if ref.ID == "" && ref.Name == "" {
		p := big(nsSearch.T("Search in channel"))
		at(gtx, image.Pt(0, (hh-p.d.Size.Y)/2), func(gtx layout.Context) layout.Dimensions { p.call.Add(gtx.Ops); return p.d })
		return
	}
	var parts []i18n.Part
	if v.query != "" {
		parts = nsSearch.RT("Search {keywords} {channelName}", "keywords", `"`+v.query+`"`, "channelName", "\x00")
	} else {
		parts = nsSearch.RT("Search {channelName}", "channelName", "\x00")
	}
	var pieces []srcHeadPiece
	for _, p := range parts {
		if p.Arg == "channelName" {
			pieces = append(pieces, v.channelPiece(gtx, th, cl, ref, w))
			continue
		}
		if s := strings.TrimSpace(p.Text); s != "" {
			pieces = append(pieces, big(s))
		}
	}
	gap := dp(gtx, 6)
	base := 0
	for _, p := range pieces {
		base = max(base, p.base)
	}
	// the 18px line's baseline, vertically centred in the header
	top := (hh - dp(gtx, 24)) / 2
	bl := top + base
	x := 0
	for _, p := range pieces {
		if x >= w {
			break
		}
		p := p
		at(gtx, image.Pt(x, bl-p.base), func(gtx layout.Context) layout.Dimensions {
			defer clip.Rect{Max: image.Pt(w-x, p.d.Size.Y+dp(gtx, 4))}.Push(gtx.Ops).Pop()
			p.call.Add(gtx.Ops)
			return p.d
		})
		x += p.d.Size.X + gap
	}
}

// channelPiece is the "in [avatar|#] name" span (13px bold content-ter).
func (v *SearchInChannel) channelPiece(gtx layout.Context, th *theme.Theme, cl *Colors, ref core.ConvRef, w int) srcHeadPiece {
	fg := cl.Fg3
	name := ref.Name
	user := ""
	if ref.Kind == "im" {
		user = ref.User
		if u, ok := lookupUser(v.h, user); ok {
			name = userName(u)
		}
	}
	call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, dp(gtx, 30))}
		pre := text(gtx, th, 13, theme.Regular, fg, nsSearch.T("in"))
		x := pre.Size.X
		lineH := pre.Size.Y
		base := pre.Size.Y - pre.Baseline
		switch {
		case user != "":
			// the avatar: padding-left 4, margin-right 7, its bottom on the line's
			x += dp(gtx, 4)
			av := ""
			if u, ok := lookupUser(v.h, user); ok {
				av = u.Avatar
			}
			at(gtx, image.Pt(x, lineH-dp(gtx, 16)), func(gtx layout.Context) layout.Dimensions { return avatar(gtx, v.h, av, 16) })
			x += dp(gtx, 16) + dp(gtx, 7)
		case ref.Kind == "mpim":
			x += textW(gtx, th, 13, theme.Bold, " ")
		default:
			// " ", the bold channel icon (margin 0 -3 0 -2, then 2), " "
			sp := textW(gtx, th, 13, theme.Bold, " ")
			x += sp - dp(gtx, 2)
			isz := 13
			at(gtx, image.Pt(x, base-dp(gtx, float32(isz))+dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, convIcon(ref.Kind), float32(isz), fg)
			})
			x += dp(gtx, float32(isz)) - dp(gtx, 3) + dp(gtx, 2) + sp
		}
		nd := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(max(1, w-x), lineH)}
			return text(gtx, th, 13, theme.Bold, fg, name)
		})
		return layout.Dimensions{Size: image.Pt(x+nd.Size.X, lineH), Baseline: pre.Baseline}
	})
	return srcHeadPiece{call: call, d: d, base: d.Size.Y - d.Baseline, isCh: true}
}

// ---- the loading skeleton ----

// srcSkelWidths are MissingText's sizes (small, medium, large).
var srcSkelWidths = [...]float32{80, 120, 160}

// srcBar is one c-missing_text: w×8 pill, its bottom on the line's
// baseline.
func srcBar(gtx layout.Context, th *theme.Theme, x, y int, w float32, alpha float32) int {
	c := th.C.Skeleton
	c.A = uint8(float32(c.A)*alpha + .5)
	r := image.Rect(x, y, x+dp(gtx, w), y+dp(gtx, 8))
	rrect(gtx, r, dp(gtx, 4), c)
	return dp(gtx, w)
}

// srcSkelRandom is MissingText size "random": one of the three widths,
// picked once per bar (seeded by the row and the bar).
func srcSkelRandom(row, bar int) float32 {
	h := uint32(row*7919+bar*104729) ^ 0x9e3779b9
	h ^= h >> 13
	h *= 0x5bd1e995
	h ^= h >> 15
	return srcSkelWidths[h%3]
}

// skeleton draws listLoadingFragment: the results label's bar and seven
// SearchMessageSkeletons, w wide from the origin.
func (v *SearchInChannel) skeleton(gtx layout.Context, th *theme.Theme, w, h int) {
	alpha := float32(1)
	c := v.h.Core()
	animate := c == nil || c.S == nil || !c.Prefs().NoAnimations
	if animate {
		if v.skelStart.IsZero() {
			v.skelStart = gtx.Now
		}
		// c-missing_text--shimmer: opacity 1 → .65 → 1, 1s ease-in-out
		t := float64(gtx.Now.Sub(v.skelStart)%time.Second) / float64(time.Second)
		e := (1 - math.Cos(2*math.Pi*t)) / 2 // 0 at the ends, 1 at 50%
		alpha = float32(1 - .35*e)
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
	}
	pad := dp(gtx, 16)
	lineH := dp(gtx, 22)
	barTop := dp(gtx, 11) // an 8px bar sitting on a 22px line's baseline
	y := 0
	// the results label's MissingText (small) in its list row, 10 below
	srcBar(gtx, th, pad, y+barTop, srcSkelWidths[0], alpha)
	y += lineH + dp(gtx, 10)
	for i := 0; i < 7 && y < h; i++ {
		x0 := pad + dp(gtx, 16) // the group's padding
		cw := max(1, w-2*pad-2*dp(gtx, 16))
		ty := y + dp(gtx, 12)
		// header: a random bar, then padding-bottom 8
		srcBar(gtx, th, x0, ty+barTop, srcSkelRandom(i, 0), alpha)
		ty += lineH + dp(gtx, 8)
		// the gutter's 36px MissingIcon
		r := image.Rect(x0, ty, x0+dp(gtx, 36), ty+dp(gtx, 36))
		ic := th.C.Skeleton
		ic.A = uint8(float32(ic.A)*alpha + .5)
		rrect(gtx, r, dp(gtx, 8), ic)
		cx := x0 + dp(gtx, 44)
		cy := ty - dp(gtx, 4)
		inner := max(1, cw-dp(gtx, 44))
		// content header: medium, small (margin-right 8)
		bx := cx
		bx += srcBar(gtx, th, bx, cy+barTop, srcSkelWidths[1], alpha) + dp(gtx, 8)
		srcBar(gtx, th, bx, cy+barTop, srcSkelWidths[0], alpha)
		cy += lineH
		// body: random ×3, <br>, random ×2 (margin-right 4, wrapping)
		flow := func(bars []float32) {
			bx := cx
			for _, bw := range bars {
				px := dp(gtx, bw)
				if bx > cx && bx+px > cx+inner {
					bx, cy = cx, cy+lineH
				}
				bx += srcBar(gtx, th, bx, cy+barTop, bw, alpha) + dp(gtx, 4)
			}
			cy += lineH
		}
		flow([]float32{srcSkelRandom(i, 1), srcSkelRandom(i, 2), srcSkelRandom(i, 3)})
		flow([]float32{srcSkelRandom(i, 4), srcSkelRandom(i, 5)})
		// footer: padding-top 8, small, small (margin-right 16)
		cy += dp(gtx, 8)
		bx = cx
		bx += srcBar(gtx, th, bx, cy+barTop, srcSkelWidths[0], alpha) + dp(gtx, 16)
		srcBar(gtx, th, bx, cy+barTop, srcSkelWidths[0], alpha)
		cy += lineH
		y = max(cy, ty+dp(gtx, 36)) + dp(gtx, 16) + dp(gtx, 10)
	}
}

// ---- the empty state ----

// emptyState draws the "empty-state" row from y: the 350px box with the
// illustration, "Nothing to show yet." and the description at its bottom.
func (v *SearchInChannel) emptyState(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int) {
	boxH := dp(gtx, 350)
	cw := min(w-2*dp(gtx, 16), dp(gtx, 380)) // c-empty_state max-width 380
	cw = max(cw, 1)
	// measure: image 140 (+24), title 24 (+8), the description (+8)
	img, ok := srcEmptyImage(gtx.Metric.PxPerDp)
	imgH := 0
	var isz image.Point
	if ok {
		// max-width 290, max-height 140, the 188×166 aspect
		ih := dp(gtx, 140)
		iw := int(float32(ih)*188/166 + .5)
		if iw > dp(gtx, 290) {
			iw = dp(gtx, 290)
			ih = int(float32(iw)*166/188 + .5)
		}
		isz = image.Pt(iw, ih)
		imgH = ih + dp(gtx, 24)
	}
	title := nsSearch.T("Nothing to show yet.")
	desc := nsSearch.RT("Type a keyword or <strong>from: @name</strong> to find that message you’re looking for.")
	descCall, dd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		return srRichCentered(gtx, th, cl.Fg, desc, cw)
	})
	total := imgH + dp(gtx, 24) + dp(gtx, 8) + dd.Size.Y + dp(gtx, 8)
	top := y + max(0, boxH-total) // align-self: flex-end
	x0 := (w - cw) / 2
	if ok {
		at(gtx, image.Pt((w-isz.X)/2, top), func(gtx layout.Context) layout.Dimensions {
			return containImage(gtx, img, isz)
		})
	}
	ty := top + imgH
	at(gtx, image.Pt(x0, ty), func(gtx layout.Context) layout.Dimensions {
		return paraCenteredW(gtx, th, 18, 24, theme.Black, cl.Fg, title, cw)
	})
	ty += dp(gtx, 24) + dp(gtx, 8)
	at(gtx, image.Pt(x0, ty), func(gtx layout.Context) layout.Dimensions {
		descCall.Add(gtx.Ops)
		return dd
	})
}

// paraCenteredW is paraCentered in weight wt (one line; longer titles
// wrap at words).
func paraCenteredW(gtx layout.Context, th *theme.Theme, size, lh float32, wt font.Weight, c color.NRGBA, s string, w int) layout.Dimensions {
	lines := srWrapWords(gtx, th, size, func(string) font.Weight { return wt }, strings.Fields(s), w)
	y := 0
	for _, ln := range lines {
		lw := textW(gtx, th, size, wt, ln)
		l := ln
		at(gtx, image.Pt((w-lw)/2, y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, dp(gtx, lh), func(gtx layout.Context) layout.Dimensions { return text(gtx, th, size, wt, c, l) })
		})
		y += dp(gtx, lh)
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// srWrapWords greedily fills lines of words no wider than w.
func srWrapWords(gtx layout.Context, th *theme.Theme, size float32, wt func(string) font.Weight, words []string, w int) []string {
	var lines []string
	cur := ""
	for _, wd := range words {
		try := wd
		if cur != "" {
			try = cur + " " + wd
		}
		if cur != "" && textW(gtx, th, size, wt(wd), try) > w {
			lines = append(lines, cur)
			cur = wd
			continue
		}
		cur = try
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// srRichCentered draws parts as a centred 15/22 paragraph w wide, the
// <strong> runs bold.
func srRichCentered(gtx layout.Context, th *theme.Theme, c color.NRGBA, parts []i18n.Part, w int) layout.Dimensions {
	type word struct {
		s  string
		wt font.Weight
		sp bool // a space before it
	}
	var words []word
	space := false
	for _, p := range parts {
		wt := theme.Regular
		if p.Tag == "strong" || p.Tag == "b" {
			wt = theme.Bold
		}
		t := p.Text
		for t != "" {
			i := strings.IndexAny(t, " \n\t")
			if i == 0 {
				space = true
				t = t[1:]
				continue
			}
			wd := t
			if i > 0 {
				wd, t = t[:i], t[i:]
			} else {
				t = ""
			}
			words = append(words, word{wd, wt, space && len(words) > 0})
			space = false
		}
	}
	spW := textW(gtx, th, 15, theme.Regular, " ")
	// lines of words
	var lines [][]word
	var cur []word
	x := 0
	for _, wd := range words {
		ww := textW(gtx, th, 15, wd.wt, wd.s)
		add := ww
		if wd.sp && len(cur) > 0 {
			add += spW
		}
		if len(cur) > 0 && wd.sp && x+add > w {
			lines = append(lines, cur)
			cur, x = nil, 0
			add = ww
		}
		cur = append(cur, wd)
		x += add
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	lh := dp(gtx, 22)
	for li, ln := range lines {
		lw := 0
		for i, wd := range ln {
			if i > 0 && wd.sp {
				lw += spW
			}
			lw += textW(gtx, th, 15, wd.wt, wd.s)
		}
		x := (w - lw) / 2
		for i, wd := range ln {
			if i > 0 && wd.sp {
				x += spW
			}
			wd := wd
			d := at(gtx, image.Pt(x, li*lh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, wd.wt, c, wd.s) })
			})
			x += d.Size.X
		}
	}
	return layout.Dimensions{Size: image.Pt(w, len(lines)*lh)}
}
