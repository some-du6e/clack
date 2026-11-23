package popups

// The emoji picker's emoji pack call to action, Slack's EmojiPackCta
// (boot-shared +y5+, build a09b625d90aa; when it shows: core
// EmojiPickerPackCTA), between the search input and the list, hidden
// while searching (b && !searchResults) and in the huddle's embedded
// picker (forceHideEmojiPackCTA):
//
//   - .p-emoji_picker__pack_cta_wrapper: the primary background, padding
//     7px 17.5px 14px; inside, .p-emoji_picker__pack_cta_inner_wrapper:
//     foreground_min_solid, radius 8, padding 12px 8px 12px 12px, the
//     call to action and the dismiss button space-between, clipping what
//     overflows.
//   - the call to action (an unstyled button, line-height 1.4667: 15/22):
//     three of the pack's emoji (lodash values(emoji), nth 0..2), each at
//     most 30x30, absolute in the box: (-5,-5) turned -5°, (15,10) 15°,
//     (-5,20) -20°; then 40px in, "{packName} emoji for your team" bold
//     ("{packName} emoji added!" once installed), " " and arrow-right,
//     hidden until the text is hovered (opacity 0 and margin-left -3px
//     to 1 and 0, .15s ease-in).
//   - the dismiss button: the smedium close icon button at 22x22.
//   - the Halloween pack's (in season): :spider_web: at (8,-4) and
//     :candle: 8px under the bottom, 25x25; 24px in, "Get the Halloween
//     emoji pack" ("Try out a new Halloween emoji" once installed) and
//     HalloweenBannerArrow 2px after; its dismiss at .5 opacity, 1
//     hovered.
//
// A click records the call to action's pref (ClickedEmojiPickerCTA) and,
// before the install, closes the picker (onAddEmojiButtonClick) and opens
// the Add emoji dialog on its Emoji packs tab (initialTab PACKS); after
// it, jumps to the pack's group. The dismiss button records the pref
// alone.

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The call to action's box (dp): the wrapper's 7 + the inner box's 12 +
// the 22px line + 12, + the wrapper's 14.
const (
	packCTAH      = 67
	packCTAInnerH = 46
	packCTAPadX   = 17.5
)

// packCTAState is the picker's call to action.
type packCTAState struct {
	cta, dismiss widget.Clickable
	// art: the pack's emoji (or the Halloween banner's two), drawn inside
	// the button, so a click on one is the button's
	art       [3]widget.Clickable
	btn       image.Rectangle // the button's box, inner box coordinates (tests)
	textHover gesture.Hover
	// the arrow's transition: hovered now, the value it started from and
	// when
	hovered bool
	from    float32
	since   time.Time
	// what the last frame drew (tests): the pack and the kind
	drawn     core.EmojiPack
	drawnKind core.PackCTA
	rect      image.Rectangle // the inner box, picker coordinates
}

// packCTA is the call to action the picker shows now.
func (p *EmojiPicker) packCTA() (core.EmojiPack, core.PackCTA) {
	a := p.h.Core()
	if a == nil || p.embedded || p.searching || p.gifMode {
		return core.EmojiPack{}, core.PackCTANone
	}
	pk, kind := a.EmojiPickerPackCTA(p.packNames)
	if kind != core.PackCTANone && pk.Name == core.HalloweenEmojiPack && !a.HalloweenSeason() {
		return core.EmojiPack{}, core.PackCTANone // `if (h) { if (!_) return null }`
	}
	return pk, kind
}

// PackCTA reports the emoji pack call to action the last frame drew, its
// pack's name and kind, and its box in window coordinates (tests).
func (p *EmojiPicker) PackCTA() (name string, kind core.PackCTA, r image.Rectangle) {
	c := &p.ctaUI
	return c.drawn.Name, c.drawnKind, c.rect.Add(p.rect.Min)
}

// PackCTAButton is the call to action's button in window coordinates: its
// content box, the label's line (tests).
func (p *EmojiPicker) PackCTAButton() image.Rectangle {
	c := &p.ctaUI
	if c.rect.Empty() {
		return image.Rectangle{}
	}
	return c.btn.Add(c.rect.Min).Add(p.rect.Min)
}

// ClickPackCTA and DismissPackCTA are the call to action's click and its
// dismiss button (tests).
func (p *EmojiPicker) ClickPackCTA()   { p.ctaUI.cta.Click() }
func (p *EmojiPicker) DismissPackCTA() { p.ctaUI.dismiss.Click() }

// ctaClicked is EmojiPackCta's onClick.
func (p *EmojiPicker) ctaClicked(pk core.EmojiPack, kind core.PackCTA) {
	a := p.h.Core()
	if a == nil {
		return
	}
	post := kind == core.PackCTAPostInstall
	a.ClickedEmojiPickerCTA(post)
	if !post {
		p.Dismiss()
		p.h.ShowOverlay(NewAddEmojiPacksModal(p.h))
		return
	}
	for k, n := range p.packNames { // jumpToGroup(packName)
		if n == pk.Name {
			p.jumpToList(epGroupSearch + 1 + k)
			return
		}
	}
}

// jumpToList scrolls the list to list's heading (a pack's group has no
// tab of its own).
func (p *EmojiPicker) jumpToList(list int) {
	if p.rowsDirty {
		p.rows, p.groupRow, p.totalH = epLayoutRows(p.rows, p.lists, p.searching, 1, 0, 1, 0)
		p.rowsDirty = true // laid out again at the frame's metrics
	}
	for i, r := range p.rows {
		if int(r.list) == list {
			p.list.Position = layout.Position{First: i, BeforeEnd: true}
			p.tabLock = -1
			return
		}
	}
}

// layoutPackCTA draws the call to action at y (picker coordinates) in a
// w-wide picker and returns its height: packCTAH, or 0 when none shows.
func (p *EmojiPicker) layoutPackCTA(gtx layout.Context, th *theme.Theme, cl *Colors, y, w int) int {
	c := &p.ctaUI
	pk, kind := p.packCTA()
	c.drawn, c.drawnKind, c.rect = pk, kind, image.Rectangle{}
	if kind == core.PackCTANone {
		return 0
	}
	for c.cta.Clicked(gtx) {
		p.ctaClicked(pk, kind)
		return 0
	}
	for i := range c.art {
		if c.art[i].Clicked(gtx) {
			p.ctaClicked(pk, kind)
			return 0
		}
	}
	if c.dismiss.Clicked(gtx) {
		if a := p.h.Core(); a != nil {
			a.ClickedEmojiPickerCTA(kind == core.PackCTAPostInstall)
		}
		return 0
	}
	h := dp(gtx, packCTAH)
	fill(gtx, image.Rect(0, y, w, y+h), cl.EmojiBg)
	inner := image.Rect(dp(gtx, packCTAPadX), y+dp(gtx, 7), w-dp(gtx, packCTAPadX), y+dp(gtx, 7+packCTAInnerH))
	c.rect = inner
	defer op.Offset(inner.Min).Push(gtx.Ops).Pop()
	isz := inner.Size()
	rad := dp(gtx, 8)
	rrect(gtx, image.Rectangle{Max: isz}, rad, cl.EmojiFoot)
	halloween := pk.Name == core.HalloweenEmojiPack
	post := kind == core.PackCTAPostInstall
	label := nsEmojiPicker.T("{packName} emoji for your team", "packName", pk.Name)
	if post {
		label = nsEmojiPicker.T("{packName} emoji added!", "packName", pk.Name)
	}
	if halloween {
		label = nsEmojiPicker.T("Get the Halloween emoji pack")
		if post {
			label = nsEmojiPicker.T("Try out a new Halloween emoji")
		}
	}
	// the dismiss button, 8px from the right, centred
	bs := dp(gtx, 22)
	bx := isz.X - dp(gtx, 8) - bs
	ctaW := max(0, bx)
	func() {
		cs := clipRRect(gtx, image.Rectangle{Max: isz}, rad) // overflow: hidden
		defer cs.Pop()
		// the call to action: an unstyled button (no padding) sized to its
		// content in the flex row: the 22px label line, from the inner
		// box's 12px padding to the arrow's end; the art, absolute inside
		// it, takes its clicks too. The box's padding around it and the
		// space right of it take none.
		gg := gtx
		gg.Constraints = layout.Exact(image.Pt(ctaW, isz.Y))
		func(gtx layout.Context) {
			if halloween {
				p.ctaHalloweenArt(gtx)
			} else {
				p.ctaArt(gtx, pk)
			}
			tx := dp(gtx, 12+40)
			if halloween {
				tx = dp(gtx, 12+24)
			}
			lineY := (isz.Y - dp(gtx, 22)) / 2
			td := at(gtx, image.Pt(tx, lineY), func(gtx layout.Context) layout.Dimensions {
				g := gtx
				g.Constraints = layout.Constraints{Max: image.Pt(1<<20, dp(gtx, 22))}
				return text(g, th, 15, theme.Bold, cl.Fg, label)
			})
			// the text container's hover (the arrow's), over the text and arrow
			arrowW := dp(gtx, 20)
			hr := image.Rect(tx, lineY, tx+td.Size.X+arrowW, lineY+dp(gtx, 22))
			hs := clip.Rect(hr).Push(gtx.Ops)
			c.textHover.Add(gtx.Ops)
			hs.Pop()
			hov := c.textHover.Update(gtx.Source)
			if halloween {
				// HalloweenBannerArrow, 2px after the text, always shown
				at(gtx, image.Pt(tx+td.Size.X+dp(gtx, 2), lineY+(dp(gtx, 22)-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
					return halloweenArrow(gtx, dp(gtx, 20), cl.Fg)
				})
			} else {
				v := p.ctaArrow(gtx, hov)
				if v > 0 {
					sp := th.Measure(gtx, 15, theme.Bold, " ")
					ax := tx + td.Size.X + sp + int(math.Round(float64(float32(dp(gtx, -3))*(1-v))))
					col := cl.Fg
					col.A = uint8(float32(col.A)*v + .5)
					at(gtx, image.Pt(ax, lineY+(dp(gtx, 22)-dp(gtx, 15))/2), func(gtx layout.Context) layout.Dimensions {
						return icon(gtx, "arrow-right", 15, col)
					})
				}
			}
			// the button's box: the arrow keeps its room while hidden
			// (opacity 0; its margin-left -3px only while it fades)
			end := tx + td.Size.X + dp(gtx, 2) + dp(gtx, 20)
			if !halloween {
				end = tx + td.Size.X + th.Measure(gtx, 15, theme.Bold, " ") + dp(gtx, 15)
			}
			c.btn = image.Rect(dp(gtx, 12), lineY, min(end, gtx.Constraints.Max.X), lineY+dp(gtx, 22))
			at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions { // red: the whole box, as on emoji-packs-2
				g := gtx
				g.Constraints = layout.Exact(gtx.Constraints.Max)
				return c.cta.Layout(g, func(gtx layout.Context) layout.Dimensions {
					pointerCursor(gtx, gtx.Constraints.Min)
					return layout.Dimensions{Size: gtx.Constraints.Min}
				})
			})
		}(gg)
	}()
	// .p-emoji_picker__pack_cta_dismiss_btn: the smedium icon button
	// (its 16px glyph) at 22x22; the Halloween one at .5 until hovered
	ink := cl.Fg
	if halloween && !c.dismiss.Hovered() {
		ink.A /= 2
	}
	hover := rgba(0x1d1c1d, .04)
	if th.Dark {
		hover = rgba(0xffffff, .04)
	}
	at(gtx, image.Pt(bx, (isz.Y-bs)/2), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &c.dismiss, "close", 22, 16, ink, hover, 6)
	})
	return h
}

// ctaArrow is the arrow's opacity (and its margin's progress): 0 to 1 in
// .15s ease-in on the text's hover, back on its end; the frames of the
// transition are asked of the host.
func (p *EmojiPicker) ctaArrow(gtx layout.Context, hovered bool) float32 {
	c := &p.ctaUI
	const dur = 150 * time.Millisecond
	value := func(now time.Time) float32 {
		if c.since.IsZero() {
			if c.hovered {
				return 1
			}
			return 0
		}
		x := float64(now.Sub(c.since)) / float64(dur)
		if x >= 1 {
			x = 1
		}
		e := float32(cubicBezierY(.42, 0, 1, 1, x))
		to := float32(0)
		if c.hovered {
			to = 1
		}
		return c.from + (to-c.from)*e
	}
	if hovered != c.hovered {
		c.from = value(gtx.Now)
		c.hovered, c.since = hovered, gtx.Now
	}
	v := value(gtx.Now)
	if !c.since.IsZero() && gtx.Now.Sub(c.since) < dur {
		p.h.Invalidate()
	}
	return v
}

// ctaArt is the call to action's three emoji of the pack.
func (p *EmojiPicker) ctaArt(gtx layout.Context, pk core.EmojiPack) {
	spots := [3]struct {
		x, y, deg float32
	}{{-5, -5, -5}, {15, 10, 15}, {-5, 20, -20}}
	for i, s := range spots {
		if i >= len(pk.Emoji) || pk.Emoji[i].URL == "" || p.h.Images() == nil {
			continue
		}
		img, ok := p.h.Images().Get(pk.Emoji[i].URL, dp(gtx, 30))
		if !ok {
			continue
		}
		// max-width/max-height 30px: the image at its size, no larger
		sz := img.Size()
		mx := dp(gtx, 30)
		if sz.X > mx || sz.Y > mx {
			k := math.Min(float64(mx)/float64(sz.X), float64(mx)/float64(sz.Y))
			sz = image.Pt(int(float64(sz.X)*k+.5), int(float64(sz.Y)*k+.5))
		}
		// rotated about its centre (transform-origin 50% 50%)
		ctr := f32.Pt(float32(dp(gtx, s.x))+float32(sz.X)/2, float32(dp(gtx, s.y))+float32(sz.Y)/2)
		tr := f32.Affine2D{}.Offset(f32.Pt(float32(dp(gtx, s.x)), float32(dp(gtx, s.y)))).
			Rotate(ctr, float32(float64(s.deg)*math.Pi/180))
		st := op.Affine(tr).Push(gtx.Ops)
		containImage(gtx, img, sz)
		p.ctaArtArea(gtx, i, sz)
		st.Pop()
	}
}

// ctaHalloweenArt is the Halloween call to action's :spider_web: and
// :candle:, 25px.
func (p *EmojiPicker) ctaHalloweenArt(gtx layout.Context) {
	if p.idx == nil {
		return
	}
	isz := gtx.Constraints.Min
	s := dp(gtx, 25)
	for i, e := range []struct {
		name string
		at   image.Point
	}{{"spider_web", image.Pt(dp(gtx, 8), dp(gtx, -4))}, {"candle", image.Pt(dp(gtx, 8), isz.Y+dp(gtx, 8)-s)}} {
		id, ok := p.idx.lookup(e.name)
		if !ok {
			continue
		}
		at(gtx, e.at, func(gtx layout.Context) layout.Dimensions {
			d := p.drawEmoji(gtx, id, 0, 25)
			p.ctaArtArea(gtx, i, image.Pt(s, s))
			return d
		})
	}
}

// ctaArtArea is art image i's share of the button: its box (sz, turned
// with it) takes the button's clicks and its cursor.
func (p *EmojiPicker) ctaArtArea(gtx layout.Context, i int, sz image.Point) {
	g := gtx
	g.Constraints = layout.Exact(sz)
	p.ctaUI.art[i].Layout(g, func(gtx layout.Context) layout.Dimensions {
		pointerCursor(gtx, gtx.Constraints.Min)
		return layout.Dimensions{Size: gtx.Constraints.Min}
	})
}

// halloweenArrow is HalloweenBannerArrow: "M8 10h6.5L11.5 6.75M14.5 10
// 11.5 13.25" in a 20-unit box, stroked 1.5 with round caps and joins, in
// a d-pixel square.
func halloweenArrow(gtx layout.Context, d int, c color.NRGBA) layout.Dimensions {
	k := float32(d) / 20
	pt := func(x, y float32) f32.Point { return f32.Pt(x*k, y*k) }
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(pt(8, 10))
	path.LineTo(pt(14.5, 10))
	path.LineTo(pt(11.5, 6.75))
	path.MoveTo(pt(14.5, 10))
	path.LineTo(pt(11.5, 13.25))
	paint.FillShape(gtx.Ops, c, clip.Stroke{Path: path.End(), Width: 1.5 * k}.Op())
	return layout.Dimensions{Size: image.Pt(d, d)}
}
