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
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/icons"
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
	clear widget.Clickable // the no-results' Clear Search
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

// gifEmpty is the no-results / error state: the GIF picker's no-results
// (boot-shared xVsA, searched or not), whose Clear Search (the picker's
// clearSearchResults: the results and the query cleared, the input
// focused) loads the trending GIFs again.
func (p *EmojiPicker) gifEmpty(gtx layout.Context, th *theme.Theme, cl *Colors, w, h int) {
	g := &p.gif
	if g.clear.Clicked(gtx) {
		g.ed.SetText("")
		g.typed, g.due = "", gtx.Now
		gtx.Execute(key.FocusCmd{Tag: &g.ed})
		gtx.Execute(op.InvalidateCmd{})
	}
	header, sub := nsGifPicker.T("No GIFs to show"), nsGifPicker.T("You\u2019ve stumped the GIF search. Impressive.")
	if g.err != "" {
		header, sub = "Couldn't load GIFs: "+g.err, ""
	}
	p.noResults(gtx, th, cl, w, h, header, sub, &g.clear, nsGifPicker.T("Clear Search"))
}

// epTabIcon draws the "Emoji | GIFs" tab icons (tab 0 Emoji, 1 GIFs):
// Slack's EmojiIcon and GifIcon (boot-shared Y5Hl, qS+L;
// p-emoji_and_gif_picker_custom_icons). The active tab, and an inactive
// one while hovered, shows the coloured icon: the skin-tone disc (#f2c744
// by default, EmojiIcon's r 9 circle) under emoji-tab-active, or the
// #9ed6fa tile (GifIcon's r 3 square) under gif-tab-active, inked in ink
// (#1d1c1d; the page background for a hovered inactive tab in dark mode).
// Other inactive tabs show the line icon (emoji's, or gif-tab's tile
// with a folded corner) in ink (--sk_foreground_max_solid).
func epTabIcon(gtx layout.Context, tab int, coloured bool, ink color.NRGBA, tone int) layout.Dimensions {
	px := dp(gtx, 20)
	k := float32(px) / 20
	switch {
	case tab == 0 && coloured:
		paint.FillShape(gtx.Ops, epToneDisc(tone), clip.Outline{Path: icons.ClipPath(gtx, epEmojiDisc, k)}.Op())
		icons.Get("emoji-tab-active")(gtx, 20, ink)
	case tab == 0:
		icons.Get("emoji")(gtx, 20, ink)
	case coloured:
		paint.FillShape(gtx.Ops, rgb(0x9ed6fa), clip.Outline{Path: icons.ClipPath(gtx, epGifTile, k)}.Op())
		icons.Get("gif-tab-active")(gtx, 20, ink)
	default:
		icons.Get("gif-tab")(gtx, 20, ink)
	}
	return layout.Dimensions{Size: image.Pt(px, px)}
}

// epEmojiDisc and epGifTile are EmojiIcon's skin-tone disc and GifIcon's
// tile (their 20-unit paths).
const (
	epEmojiDisc = "M19 10C19 5.03 14.97 1 10 1C5.03 1 1 5.03 1 10C1 14.97 5.03 19 10 19C14.97 19 19 14.97 19 10Z"
	epGifTile   = "M4.5 1.5H15.5C17.1569 1.5 18.5 2.84315 18.5 4.5V15.5C18.5 17.1569 17.1569 18.5 15.5 18.5H4.5C2.84315 18.5 1.5 17.1569 1.5 15.5V4.5C1.5 2.84315 2.84315 1.5 4.5 1.5Z"
)

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
