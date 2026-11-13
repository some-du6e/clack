package popups

import (
	"context"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// GIFs tab of the emoji picker (Slack's Giphy tab), measured from
// app.slack.com (2026-11, gifs.png): the category row and the footer are
// gone; the search input ("Search all GIFs on Giphy", same style as the
// emoji search) sits at x 11, y 61, full width minus 22, 38 tall; the grid
// starts at y 116: 3 columns of 99×99 tiles (radius 6, 1px ring, cover
// cropped) at x 12 with 12px gaps, scrolling to the card's bottom.
const (
	gifInputX   = 11
	gifInputY   = 61
	gifListY    = 110 // viewport top; rows pad gifGap/2 above each tile
	gifTile     = 99
	gifGap      = 12
	gifX0       = 12
	gifCols     = 3
	gifPageSize = 30
	gifDebounce = 300 * time.Millisecond
)

// gifPanel is the GIF tab's state (part of EmojiPicker).
type gifPanel struct {
	ed      widget.Editor
	focused bool
	typed   string    // trimmed editor text
	due     time.Time // debounce deadline for typed (zero: none)
	started bool

	query   string // query of the results shown / loading
	seq     int    // bumped per query; stale results are dropped
	loading bool   // first page in flight
	paging  bool   // next page in flight
	err     string
	items   []Gif
	cursor  string // next page cursor ("" = no more)
	tiles   []widget.Clickable

	list  layout.List
	rowFn layout.ListElement
	viewH int
}

type gifResult struct {
	gifs []Gif
	next string
}

// SetGifHandler makes picking a GIF call f (and close the picker) instead
// of sending it.
func (p *EmojiPicker) SetGifHandler(f func(g Gif)) { p.onGif = f }

// ShowGifs opens the picker on its GIFs tab (EmojiAndGIFPickerPopover's
// selectedTabId GIFS: the composer's + menu GIF row).
func (p *EmojiPicker) ShowGifs() { p.setGifMode(true) }

// SetGifTarget makes picking a GIF send it to channel (in threadTS's thread
// when set) when no GIF handler is set.
func (p *EmojiPicker) SetGifTarget(channel, threadTS string) {
	p.gifCh, p.gifTS = channel, threadTS
}

// setGifMode switches between the Emoji and GIFs tabs.
func (p *EmojiPicker) setGifMode(on bool) {
	if p.gifMode == on {
		return
	}
	p.gifMode = on
	p.hover, p.press, p.pressed, p.ptrIn = epNoCell, epNoCell, false, false
	p.toneUI.open = false
	p.prevID = -1
	if on {
		g := &p.gif
		g.focused = false
		if g.rowFn == nil {
			g.ed = widget.Editor{SingleLine: true, Submit: true}
			g.list.Axis = layout.Vertical
			g.rowFn = p.gifRow
		}
	}
}

// gifFetch loads the first page for q, or (more) the next page of the
// current query. Results arriving for an older query are dropped.
func (p *EmojiPicker) gifFetch(q string, more bool) {
	g := &p.gif
	if more {
		if g.loading || g.paging || g.cursor == "" {
			return
		}
		g.paging = true
		q = g.query
	} else {
		g.seq++
		g.query = q
		g.loading, g.paging = true, false
		g.err = ""
	}
	seq, cursor, h := g.seq, g.cursor, p.h
	if !more {
		cursor = ""
	}
	run(h, func(ctx context.Context) (gifResult, error) {
		gifs, next, err := cxGifs(ctx, h, q, cursor, gifPageSize)
		return gifResult{gifs, next}, err
	}, func(r gifResult, err error) {
		if seq != g.seq {
			return // a newer query took over
		}
		if more {
			g.paging = false
			if err != nil {
				g.cursor = "" // stop paging; keep what we have
				return
			}
			g.items = append(g.items, r.gifs...)
		} else {
			g.loading = false
			if err != nil {
				g.items, g.cursor = g.items[:0], ""
				g.err = errText(err)
				return
			}
			g.items = append(g.items[:0], r.gifs...)
			g.list.Position = layout.Position{}
		}
		g.cursor = r.next
		if len(r.gifs) == 0 {
			g.cursor = ""
		}
		for len(g.tiles) < len(g.items) {
			g.tiles = append(g.tiles, widget.Clickable{})
		}
	})
}

// pickGif hands g to the GIF handler, sends it to the target, or explains.
func (p *EmojiPicker) pickGif(g Gif) {
	switch {
	case p.onGif != nil:
		p.Dismiss()
		p.onGif(g)
	case p.gifCh != "":
		h, ch, ts := p.h, p.gifCh, p.gifTS
		p.Dismiss()
		do(h, "GIF sent", func(ctx context.Context) error {
			return cxSendGif(ctx, h, ch, ts, g)
		})
	default:
		p.h.Toast("Open the GIF picker from a composer to send GIFs")
	}
}

// layoutGif draws the GIF tab below the top bar (card-local coordinates,
// sz is the card size).
func (p *EmojiPicker) layoutGif(gtx layout.Context, th *theme.Theme, cl *Colors, sz image.Point) {
	g := &p.gif
	for {
		e, ok := g.ed.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			g.typed = strings.TrimSpace(g.ed.Text())
			g.due = gtx.Now.Add(gifDebounce)
		case widget.SubmitEvent:
			g.typed = strings.TrimSpace(g.ed.Text())
			g.due = gtx.Now // search now
		}
	}
	switch {
	case !g.started:
		g.started = true
		g.due = time.Time{}
		p.gifFetch(g.typed, false)
	case !g.due.IsZero():
		if gtx.Now.Before(g.due) {
			gtx.Execute(op.InvalidateCmd{At: g.due})
		} else {
			g.due = time.Time{}
			if g.typed != g.query {
				p.gifFetch(g.typed, false)
			}
		}
	}
	if !g.focused || (isTop(p) && !gtx.Focused(&g.ed)) {
		gtx.Execute(key.FocusCmd{Tag: &g.ed})
		g.focused = true
	}

	at(gtx, image.Pt(dp(gtx, gifInputX), dp(gtx, gifInputY)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(sz.X-2*dp(gtx, gifInputX), dp(gtx, epInputH)))
		return field(gtx, th, &g.ed, nsGifPicker.T("Search all GIFs on Giphy"), "", epInputH, 15)
	})

	r := image.Rect(0, dp(gtx, gifListY), sz.X, sz.Y)
	vw, vh := r.Dx(), r.Dy()
	g.viewH = vh
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	defer clip.Rect{Max: r.Size()}.Push(gtx.Ops).Pop()

	if len(g.items) == 0 {
		if g.loading {
			p.gifSkeleton(gtx, th, vh)
		} else {
			p.gifEmpty(gtx, th, cl, vw, vh)
		}
		return
	}
	rows := (len(g.items) + gifCols - 1) / gifCols
	n := rows
	if g.paging {
		n++ // skeleton row while the next page loads
	}
	lg := gtx
	lg.Constraints = layout.Exact(image.Pt(vw, vh))
	g.list.Layout(lg, n, g.rowFn)
	pos := g.list.Position
	if pos.First+pos.Count >= rows-2 && g.cursor != "" {
		p.gifFetch("", true)
	}

	// scrollbar thumb (rows are uniform, so the content height is exact)
	rowH := dp(gtx, gifTile+gifGap)
	if total := n * rowH; total > vh {
		scroll := pos.First*rowH + pos.Offset
		tb := maxInt(dp(gtx, 24), vh*vh/total)
		span := total - vh
		ty := minInt(maxInt(scroll, 0), span) * (vh - tb) / span
		col := rgba(0x1d1c1d, .35)
		if th.Dark {
			col = rgba(0xffffff, .3)
		}
		x := vw - dp(gtx, 12)
		rrect(gtx, image.Rect(x, ty, x+dp(gtx, 8), ty+tb), dp(gtx, 4), col)
	}

	// prefetch the next two rows
	if im := p.h.Images(); im != nil {
		last := (pos.First + pos.Count) * gifCols
		for i := last; i < len(g.items) && i < last+2*gifCols; i++ {
			gf := g.items[i]
			im.Prefetch(gifStill(gf), gifImgPx(gtx, gf))
		}
	}
}

// gifStill is the URL drawn for a tile: the still first frame (fast), else
// the animated preview (imgcache decodes its first frame).
func gifStill(g Gif) string {
	if g.Still != "" {
		return g.Still
	}
	return g.Preview
}

// gifImgPx is the decode size for a tile: big enough that the short side
// still covers the tile after cropping.
func gifImgPx(gtx layout.Context, g Gif) int {
	px := dp(gtx, gifTile)
	if g.W > 0 && g.H > 0 {
		lo, hi := g.W, g.H
		if lo > hi {
			lo, hi = hi, lo
		}
		return minInt(px*hi/lo, 3*px)
	}
	return 2 * px
}

// gifRow draws grid row i (layout.List callback).
func (p *EmojiPicker) gifRow(gtx layout.Context, i int) layout.Dimensions {
	th := p.h.Theme()
	cl := colorsOf(th)
	g := &p.gif
	w := gtx.Constraints.Max.X
	t, gap := dp(gtx, gifTile), dp(gtx, gifGap)
	h := t + gap
	rad := dp(gtx, 6)
	for c := 0; c < gifCols; c++ {
		k := i*gifCols + c
		tp := image.Pt(dp(gtx, gifX0)+c*(t+gap), gap/2)
		if k >= len(g.items) {
			if i*gifCols >= len(g.items) { // the loading row
				at(gtx, tp, func(gtx layout.Context) layout.Dimensions {
					return th.Placeholder(gtx, image.Pt(t, t), rad)
				})
			}
			continue
		}
		if k >= len(g.tiles) {
			continue
		}
		gf := g.items[k]
		if g.tiles[k].Clicked(gtx) {
			p.pickGif(gf)
			if p.done {
				return layout.Dimensions{Size: image.Pt(w, h)}
			}
		}
		at(gtx, tp, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(t, t))
			return slackc.RenderLazy(gtx, "GifListItem", func() slackc.Props { return gifItemProps(gf) }, func(gtx layout.Context, _ slackc.Props) layout.Dimensions {
				return p.gifTile(gtx, cl, gf, k, t, rad)
			})
		})
	}
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// gifTile draws GIF k (Slack's GifListItem native).
func (p *EmojiPicker) gifTile(gtx layout.Context, cl *Colors, gf Gif, k, t, rad int) layout.Dimensions {
	g := &p.gif
	th := p.h.Theme()
	im := p.h.Images()
	return clickBox(gtx, &g.tiles[k], nil, func(gtx layout.Context) layout.Dimensions {
		sz := image.Pt(t, t)
		// Accessibility > "Allow animations" (a11y_animations) off: stills.
		var an *gifAnim
		ok := false
		if !uipref.Get().NoAnimations {
			an, ok = gifAnimGet(p.h, gifAnimURL(gf), t, gtx.Now)
		}
		if ok {
			// animated (frames decoded off the UI goroutine; the
			// next redraw is scheduled only while this tile shows).
			theme.Image(gtx, an.frameAt(gtx), sz, rad)
		} else if img, ok := im.Get(gifStill(gf), gifImgPx(gtx, gf)); ok {
			theme.Image(gtx, img, sz, rad)
		} else {
			th.Placeholder(gtx, sz, rad)
		}
		if g.tiles[k].Hovered() {
			rrect(gtx, image.Rectangle{Max: sz}, rad, rgba(0x000000, .12))
		}
		stroke(gtx, image.Rectangle{Max: sz}, rad, border1(gtx), cl.Ring)
		return layout.Dimensions{Size: sz}
	})
}

// gifItemProps are Slack's GifListItem props for g (BetterGifs adds a
// favourite star to Giphy items it can validate).
func gifItemProps(g Gif) slackc.Props {
	return slackc.Props{
		"$key":       "gif:" + g.ID,
		"id":         g.ID,
		"name":       g.Title,
		"url":        g.URL,
		"previewUrl": gifStill(g),
		"width":      g.W,
		"height":     g.H,
	}
}

// layoutGifPicker draws the GIF tab through Slack's GifPicker (the
// picker's body below the tabs): {onGifSelected(event, payload),
// onClosed(event), emojiSearchQuery}. A plugin that replaces it (BetterGifs)
// calls onGifSelected with Slack's payload, which is sent like a picked
// tile.
func (p *EmojiPicker) layoutGifPicker(gtx layout.Context, th *theme.Theme, cl *Colors, sz image.Point) {
	top := dp(gtx, epTopH)
	at(gtx, image.Pt(0, top), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(sz.X, maxInt(0, sz.Y-top)))
		return slackc.RenderLazy(gtx, "GifPicker", p.gifPickerProps, func(gtx layout.Context, _ slackc.Props) layout.Dimensions {
			defer op.Offset(image.Pt(0, -top)).Push(gtx.Ops).Pop()
			p.layoutGif(gtx, th, cl, sz)
			return layout.Dimensions{Size: image.Pt(sz.X, maxInt(0, sz.Y-top))}
		})
	})
}

func (p *EmojiPicker) gifPickerProps() slackc.Props {
	return slackc.Props{
		"$key":             "gif-picker",
		"emojiSearchQuery": p.gif.typed,
		"onClosed":         func(any) {},
		"onGifSelected": func(args ...any) {
			var pl slackc.Props
			if len(args) > 1 {
				pl = asProps(args[1])
			}
			if pl == nil {
				return
			}
			g := Gif{
				URL:     pl.StrOr("originalGifUrl", ""),
				Preview: pl.StrOr("previewUrl", ""),
				Title:   pl.StrOr("name", ""),
				W:       pl.IntOr("width", 0),
				H:       pl.IntOr("height", 0),
			}
			if g.URL == "" {
				return
			}
			g.Still = g.Preview
			g.Picked = &GifSelection{OriginalGifURL: g.URL, PreviewURL: g.Preview, Name: g.Title, Width: g.W, Height: g.H,
				Bytes: int64(pl.IntOr("bytes", 0)), PreviewBytes: int64(pl.IntOr("previewBytes", 0))}
			p.pickGif(g)
		},
	}
}

// gifSkeleton fills the viewport with placeholder tiles while the first
// page loads.
func (p *EmojiPicker) gifSkeleton(gtx layout.Context, th *theme.Theme, vh int) {
	t, gap := dp(gtx, gifTile), dp(gtx, gifGap)
	rad := dp(gtx, 6)
	for y := gap / 2; y < vh; y += t + gap {
		for c := 0; c < gifCols; c++ {
			at(gtx, image.Pt(dp(gtx, gifX0)+c*(t+gap), y), func(gtx layout.Context) layout.Dimensions {
				return th.Placeholder(gtx, image.Pt(t, t), rad)
			})
		}
	}
}

// gifEmpty is the no-results / error state.
func (p *EmojiPicker) gifEmpty(gtx layout.Context, th *theme.Theme, cl *Colors, w, h int) {
	g := &p.gif
	msg := nsGifPicker.T("No GIFs to show") // the GIF picker's empty state (DMNm), searched or not
	if g.err != "" {
		msg = "Couldn't load GIFs: " + g.err
	}
	ig := dp(gtx, 32)
	maxW := w - 2*dp(gtx, 16)
	tw := minInt(textW(gtx, th, 15, theme.Regular, msg), maxW)
	y := h/2 - dp(gtx, 36)
	at(gtx, image.Pt((w-ig)/2, y), func(gtx layout.Context) layout.Dimensions {
		return emojiGlyph(gtx, epGlyphSearch, 32, cl.Subtle)
	})
	at(gtx, image.Pt((w-tw)/2, y+ig+dp(gtx, 12)), func(gtx layout.Context) layout.Dimensions {
		return text(loose(gtx, image.Pt(tw+1, dp(gtx, 24))), th, 15, theme.Regular, cl.Fg2, msg)
	})
}

// epTabIcon draws the "Emoji | GIFs" tab icons (tab 0 Emoji, 1 GIFs) the
// way Slack's EmojiIcon and GifIcon do (p-emoji_and_gif_picker_custom_icons,
// client-boot-shared, 2026-11): the active tab, and an inactive one while
// hovered, shows the coloured icon, a skin-tone disc (#f2c744 by default)
// with a turned grin or a #9ed6fa tile with a wink, inked in ink (#1d1c1d;
// the page background for a hovered inactive tab in dark mode). Other
// inactive tabs show the line icon (a smile, a tile with a folded corner)
// in ink (--sk_foreground_max_solid).
func epTabIcon(gtx layout.Context, tab int, coloured bool, ink color.NRGBA, tone int) layout.Dimensions {
	px := dp(gtx, 20)
	e := &epPen{gtx: gtx, s: float32(px) / 20, col: ink}
	switch {
	case tab == 0 && coloured:
		e.begin()
		e.circle(10, 10, 9)
		e.fillC(epToneDisc(tone))
		e.begin()
		e.circle(10, 10, 8.25)
		e.stroke()
		// the face turned ~18° clockwise: eyes and an open grin
		e.dot(8.58, 6.88, 1.5)
		e.dot(13.34, 8.4, 1.5)
		e.begin()
		e.move(5.9, 9.75)
		e.cube(6.05, 9.48, 6.36, 9.33, 6.67, 9.42)
		e.line(13.43, 11.58)
		e.cube(13.85, 11.71, 14.04, 12.14, 13.92, 12.52)
		e.cube(13.6, 13.52, 11.8, 15.78, 8.69, 14.79)
		e.cube(5.61, 13.81, 5.39, 10.95, 5.72, 9.91)
		e.close()
		e.fill()
	case tab == 0:
		e.begin()
		e.circle(10, 10, 8.25)
		e.stroke()
		e.dot(7.5, 8, 1.5)
		e.dot(12.5, 8, 1.5)
		e.begin()
		e.move(6.9, 12)
		e.cube(7.4, 13.5, 8.71, 14.25, 10.03, 14.25)
		e.cube(11.34, 14.25, 12.66, 13.5, 13.15, 12)
		e.stroke()
	case coloured:
		e.begin()
		e.rrect(1.5, 1.5, 18.5, 18.5, 3)
		e.fillC(rgb(0x9ed6fa))
		e.begin()
		e.rrect(1.5, 1.5, 18.5, 18.5, 3)
		e.stroke()
		e.dot(12.5, 8.5, 1.5)
		e.begin()
		e.move(6.9, 12.5)
		e.cube(7.4, 14, 8.71, 14.75, 10.03, 14.75)
		e.cube(11.34, 14.75, 12.66, 14, 13.15, 12.5)
		e.stroke()
		e.begin() // winking eye
		e.move(7.04, 7.54)
		e.line(9.04, 8.68)
		e.line(7.04, 9.54)
		e.strokeW(1)
	default:
		// a tile with its top-right corner folded down
		e.begin()
		e.move(4.5, 1.63)
		e.line(12.42, 1.63)
		e.cube(13.32, 1.63, 14.18, 2.03, 14.75, 2.73)
		e.line(17.61, 6.23)
		e.cube(18.05, 6.77, 18.29, 7.44, 18.29, 8.13)
		e.line(18.29, 15.42)
		e.cube(18.29, 17.07, 16.95, 18.42, 15.29, 18.42)
		e.line(4.5, 18.42)
		e.cube(2.84, 18.42, 1.5, 17.07, 1.5, 15.42)
		e.line(1.5, 4.63)
		e.cube(1.5, 2.97, 2.84, 1.63, 4.5, 1.63)
		e.close()
		e.stroke()
		e.begin() // the flap
		e.move(12.86, 3.6)
		e.line(12.86, 6.9)
		e.cube(12.86, 7.53, 13.37, 8.05, 14, 8.05)
		e.line(16.48, 8.05)
		e.cube(17.4, 8.05, 17.95, 6.95, 17.4, 6.2)
		e.line(14.93, 2.91)
		e.cube(14.25, 2, 12.86, 2.5, 12.86, 3.6)
		e.close()
		e.strokeW(1.7)
		e.dot(7.43, 8.54, 1.48)
		e.dot(12.36, 8.54, 1.48)
		e.begin()
		e.move(6.84, 12.49)
		e.cube(7.32, 13.97, 8.62, 14.71, 9.92, 14.71)
		e.cube(11.22, 14.71, 12.52, 13.97, 13.01, 12.49)
		e.stroke()
	}
	return layout.Dimensions{Size: image.Pt(px, px)}
}

// epToneDisc is the Emoji tab's disc for the preferred skin tone
// (p-emoji_and_gif_picker_custom_icons--skin_tone_N; 0 and 1 are yellow).
func epToneDisc(tone int) color.NRGBA {
	switch tone {
	case 2:
		return rgb(0xffdbbe)
	case 3:
		return rgb(0xe6b892)
	case 4:
		return rgb(0xd4a37c)
	case 5:
		return rgb(0xc18e65)
	case 6:
		return rgb(0xb47f55)
	}
	return rgb(0xf2c744)
}
