package messages

import (
	"image"
	"image/color"
	"sync"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255}
}

func rgba(v uint32, a float32) color.NRGBA {
	c := rgb(v)
	c.A = uint8(a*255 + 0.5)
	return c
}

// colors are the message-pane colors measured from the 2026 client.
type colors struct {
	Bg, Hover, Text, Text2, Text3                     color.NRGBA
	Edited, PinIcon, Muted70, TimeSystem              color.NRGBA
	Pinned, PinnedHover, Highlight, ActiveThread      color.NRGBA
	BadgeBg, BadgeText                                color.NRGBA
	ReactBg, ReactHoverBorder                         color.NRGBA
	ReactSelfBg, ReactSelfHoverBg                     color.NRGBA
	ReactSelfBorder, ReactSelfText                    color.NRGBA
	Link, NewLine, DayLine, PillBorder                color.NRGBA
	Shadow, ToolbarIcon, ToolbarHoverBg               color.NRGBA
	Tooltip, TooltipText, Skeleton                    color.NRGBA
	ThreadBorder, ThreadBg, JumpBg                    color.NRGBA
	JumpText, FailedText, BorderStrong                color.NRGBA
	SystemText, HeaderBorder, CloseHover              color.NRGBA
	Editing, ScrollThumb, ScrollThumbHover, Selection color.NRGBA
}

var lightColors = colors{
	Bg: rgb(0xffffff), Hover: rgb(0xf8f8f8), Text: rgb(0x1d1c1d), Text2: rgb(0x616061), Text3: rgb(0x454447),
	Edited: rgb(0x868686), PinIcon: rgb(0xe8912d), Muted70: rgba(0x1d1c1d, 0.7), TimeSystem: rgb(0x5e5d60),
	Pinned: rgba(0xf2c744, 0.1), PinnedHover: rgba(0xf2c744, 0.14), Highlight: rgba(0xf2c744, 0.2),
	BadgeBg: rgba(0x1d1c1d, 0.06), BadgeText: rgb(0x454447),
	ReactBg: rgba(0x1d1c1d, 0.06), ReactHoverBorder: rgb(0x7c7a7f),
	// .c-reaction--reacted(:hover): base-hgl-1, content-hgl-1 bold and a
	// 1px base-inv-hgl-1 ring; hover changes nothing
	ReactSelfBg: rgb(0xe3f8ff), ReactSelfHoverBg: rgb(0xe3f8ff),
	ReactSelfBorder: rgb(0x1264a3), ReactSelfText: rgb(0x1264a3),
	Link: rgb(0x1264a3), NewLine: rgb(0xe01e5a), DayLine: rgb(0xdddddd), PillBorder: rgba(0x1d1c1d, 0.13),
	Shadow: rgba(0x000000, 0.08), ToolbarIcon: rgb(0x616061), ToolbarHoverBg: rgba(0x1d1c1d, 0.04),
	Tooltip: rgb(0x1d1c1d), TooltipText: rgb(0xffffff), Skeleton: rgb(0xeaeaea),
	ThreadBorder: rgba(0x1d1c1d, 0.13), ThreadBg: rgb(0xffffff), JumpBg: rgb(0x1264a3),
	JumpText: rgb(0xffffff), FailedText: rgb(0xe01e5a), BorderStrong: rgba(0x1d1c1d, 0.3),
	Editing: rgba(0xf2c744, 0.2), ScrollThumb: rgba(0x1d1c1d, 0.3), ScrollThumbHover: rgba(0x1d1c1d, 0.5), Selection: rgba(0x1d9bd1, 0.3),
	SystemText: rgb(0x454447), HeaderBorder: rgba(0x1d1c1d, 0.13), CloseHover: rgba(0x1d1c1d, 0.06),

	// --dt_color-base-hgl-3 (slack-kit-gantry-v2-styles): the selected thread root (active_thread.go)
	ActiveThread: rgb(0xfffae0),
}

var darkColors = colors{
	Bg: rgb(0x1a1d21), Hover: rgb(0x222529), Text: rgb(0xf8f8f8), // --dt_color-content-pry (sender names)
	Text2: rgb(0xababad), Text3: rgb(0xd1d2d3),
	Edited: rgb(0x868686), PinIcon: rgb(0xe8912d), Muted70: rgba(0xe8e8e8, 0.7), TimeSystem: rgb(0xababad),
	Pinned: rgba(0xf2c744, 0.1), PinnedHover: rgba(0xf2c744, 0.14), Highlight: rgba(0xf2c744, 0.1),
	BadgeBg: rgba(0xe8e8e8, 0.13), BadgeText: rgba(0xe8e8e8, 0.7),
	ReactBg: rgba(0xe8e8e8, 0.06), ReactHoverBorder: rgba(0xe8e8e8, 0.3),
	// .sk-client-theme--dark .c-reaction--reacted: a solid base-inv-hgl-1
	// pill, no ring, content-pry text (bold), the same under the pointer
	ReactSelfBg: rgb(0x0a77a7), ReactSelfHoverBg: rgb(0x0a77a7),
	ReactSelfBorder: color.NRGBA{}, ReactSelfText: rgb(0xf8f8f8),
	Link: rgb(0x1d9bd1), NewLine: rgb(0xe01e5a), DayLine: rgba(0xe8e8e8, 0.13), PillBorder: rgba(0xe8e8e8, 0.13),
	Shadow: rgba(0x000000, 0.3), ToolbarIcon: rgb(0xababad), ToolbarHoverBg: rgb(0x222529),
	Tooltip: rgb(0xe8e8e8), TooltipText: rgb(0x1a1d21), Skeleton: rgb(0x2c2d30),
	ThreadBorder: rgba(0xe8e8e8, 0.13), ThreadBg: rgb(0x1a1d21), JumpBg: rgb(0x1d9bd1),
	JumpText: rgb(0xffffff), FailedText: rgb(0xe01e5a), BorderStrong: rgba(0xe8e8e8, 0.3),
	Editing: rgba(0xf2c744, 0.14), ScrollThumb: rgba(0xe8e8e8, 0.3), ScrollThumbHover: rgba(0xe8e8e8, 0.5), Selection: rgba(0x1d9bd1, 0.4),
	SystemText: rgb(0xababad), HeaderBorder: rgba(0xe8e8e8, 0.13), CloseHover: rgba(0xe8e8e8, 0.06),

	// --dt_color-base-hgl-3, dark
	ActiveThread: rgb(0x2f1e00),
}

func colorsFor(th *theme.Theme) *colors {
	flatOnce.Do(func() {
		lightFlat, darkFlat = flatten(lightColors), flatten(darkColors)
	})
	if c := themedColors(th); c != nil {
		return c
	}
	if th.Dark {
		return &darkFlat
	}
	return &lightFlat
}

var (
	flatOnce            sync.Once
	lightFlat, darkFlat colors
)

// flatten pre-blends the translucent row/pill fills over the pane color in
// sRGB, the way browsers composite CSS rgba(). Gio blends in linear light,
// which makes e.g. rgba(242,199,68,.1) on the dark pane look far brighter
// than Slack's.
func flatten(c colors) colors {
	bg := c.Bg
	for _, p := range []*color.NRGBA{&c.Hover, &c.Pinned, &c.PinnedHover, &c.Highlight, &c.Editing,
		&c.BadgeBg, &c.ReactBg, &c.ReactSelfBg, &c.ReactSelfHoverBg, &c.ToolbarHoverBg, &c.CloseHover,
		&c.DayLine, &c.PillBorder, &c.ThreadBorder, &c.HeaderBorder, &c.BorderStrong, &c.ReactHoverBorder, &c.Muted70} {
		*p = richview.Over(*p, bg)
	}
	return c
}

// metrics are the row geometry in dp, from Slack's CSS (bv1-13, 2026-11-08):
//
//   - channel rows: .p-message_pane_message__message .c-message_kit__gutter
//     { padding: 8px 20px } (chanPadX, content at 20+36+8 = 64)
//   - thread rows: .c-message_kit__thread_message .c-message_kit__gutter
//     { padding: 8px 24px 8px 16px }, root padding-top 20 (padX, contentX)
//   - .c-message_kit__gutter__right { margin: -12px -8px -16px -16px;
//     padding: 8px 8px 8px 16px }: the column starts 4px below the row top
//     and adds nothing below; .c-message_kit__blocks--rich_text
//     { margin-bottom: 4px } (bodyMarginBottom). A one-line continuation
//     row is 4 + 22 + 4 = 30, a header row max(4+22+22+4, 8+36+8) = 52.
var mt = struct {
	padX, avatar, gap, contentX   float32
	chanPadX, chanContentX        float32
	headPadTop, padTop, padBottom float32
	nameLine, bodyMarginBottom    float32
	tsGap, badgeGap               float32
	reactH, reactGap, reactTop    float32
	reactEmoji, reactBottom       float32
	threadH, threadAvatar         float32
	dayH, dayPillH, unreadH       float32
	toolbarBtn, toolbarPad        float32
	toolbarTop, toolbarRight      float32
	statusEmoji                   float32
	denseTS, indent, denseX       float32
}{
	padX: 16, avatar: 36, gap: 8, contentX: 60,
	chanPadX: 20, chanContentX: 64,
	headPadTop: 8, padTop: 8, padBottom: 8,
	nameLine: 22, bodyMarginBottom: 4,
	tsGap: 8, badgeGap: 4,
	reactH: 24, reactGap: 4, reactTop: 4, reactEmoji: 16, reactBottom: 4,
	threadH: 34, threadAvatar: 24,
	dayH: 40, dayPillH: 28, unreadH: 20,
	toolbarBtn: 32, toolbarPad: 4, toolbarTop: -16, toolbarRight: 17,
	statusEmoji: 16,
	// Compact (messages_theme dense): the gutter's left side is the
	// .p-message_pane_message__compact_timestamp box (width 56px, margin
	// -4px 0, text-align right) and its margin-right 8; the body sits in
	// .c-message_kit__indent (padding-left 8): 20+56+8+8 = 92, the sender
	// 8px before it (.p-message_pane_message__sender--compact).
	denseTS: 56, indent: 8, denseX: 92,
}

// ---- drawing helpers ----

func dp(gtx layout.Context, v float32) int { return gtx.Dp(unit.Dp(v)) }

func fill(ops *op.Ops, r image.Rectangle, c color.NRGBA) {
	if c.A == 0 || r.Empty() {
		return
	}
	paint.FillShape(ops, c, clip.Rect(r).Op())
}

func rrect(ops *op.Ops, r image.Rectangle, radius int, c color.NRGBA) {
	if c.A == 0 || r.Empty() {
		return
	}
	paint.FillShape(ops, c, clip.UniformRRect(r, radius).Op(ops))
}

func border(ops *op.Ops, r image.Rectangle, radius, w int, c color.NRGBA) {
	richview.StrokeRRect(ops, r, radius, w, c)
}

// shadowCard draws Slack's floating shadow (1px ring + soft drop) and a fill.
func shadowCard(ops *op.Ops, r image.Rectangle, radius, px int, bg, ring, shadow color.NRGBA) {
	// soft drop shadow: a few expanding translucent rounded rects
	for i := 3; i >= 1; i-- {
		s := shadow
		s.A = uint8(int(shadow.A) / (i + 1))
		rr := r.Add(image.Pt(0, px)).Inset(-i * px / 2)
		rrect(ops, rr, radius+i*px/2, s)
	}
	rrect(ops, r, radius, bg)
	border(ops, r, radius, px, ring)
}

// textStyle is a label style.
type textStyle struct {
	size     float32 // sp
	weight   font.Weight
	italic   bool
	col      color.NRGBA
	lineH    float32 // sp, 0 = font default
	maxLines int
}

// labelHook, when set (tests), sees every label drawn: the native parts'
// text and the style CSS left them.
var labelHook func(txt string, s textStyle)

// label draws txt; returns dims with Baseline set.
func label(gtx layout.Context, th *theme.Theme, s textStyle, txt string) layout.Dimensions {
	if labelHook != nil {
		labelHook(txt, s)
	}
	f := font.Font{Typeface: theme.Typeface, Weight: s.weight}
	if s.italic {
		f.Style = font.Italic
	}
	n := s.maxLines
	if n == 0 {
		n = 1
	}
	// shared renderer: Embolden, emoji images, ignorable-codepoint cleanup
	return th.Text(gtx, theme.LabelOpts{Font: f, Size: unit.Sp(s.size), LineHeight: unit.Sp(s.lineH), MaxLines: n, Color: s.col}, txt)
}

// cssBaseline is where Chrome puts a size/lineH line's baseline from the
// line box top: half the leading above the glyphs ((lineH - content)/2,
// content = the UI font's hhea ascent + descent, Lato's 0.987 + 0.213),
// then the ascent.
func cssBaseline(gtx layout.Context, size, lineH float32) int {
	sp := gtx.Metric.PxPerSp
	asc, desc := uiExt.Ascent, uiExt.Descent
	return int((lineH*sp-(asc+desc)*size*sp)/2 + asc*size*sp + 0.5)
}

// uiExt is the UI font's extents for cssBaseline, taken from the host's
// shaper each frame (styleForTheme): Lato's until a font is chosen.
var uiExt = theme.LatoExtents

// lineBox draws a one-line label (w, in style s) as a CSS line box lineH
// tall. Gio starts the first line at the font's ascent and puts all of
// LineHeight's leading below it; Chrome splits it above and below, which
// for 15px/22px Lato is 2px lower. Returns the box with its baseline.
func lineBox(gtx layout.Context, s textStyle, w layout.Widget) layout.Dimensions {
	if s.lineH <= 0 {
		return w(gtx)
	}
	m := op.Record(gtx.Ops)
	d := w(gtx)
	call := m.Stop()
	if d.Size.Y == 0 {
		call.Add(gtx.Ops)
		return d
	}
	bl := cssBaseline(gtx, s.size, s.lineH)
	t := op.Offset(image.Pt(0, bl-(d.Size.Y-d.Baseline))).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	h := max(gtx.Sp(unit.Sp(s.lineH)), bl)
	return layout.Dimensions{Size: image.Pt(d.Size.X, h), Baseline: h - bl}
}

// measure returns the width of single-line text.
func measure(gtx layout.Context, th *theme.Theme, s textStyle, txt string) image.Point {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	d := label(g, th, s, txt)
	m.Stop()
	return d.Size
}

// at runs w at offset (x, y) and returns its dims.
func at(gtx layout.Context, x, y int, w layout.Widget) layout.Dimensions {
	t := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	gtx.Constraints.Min = image.Point{}
	d := w(gtx)
	t.Pop()
	return d
}

// drawAvatar paints an avatar (or a placeholder while it loads).
func drawAvatar(gtx layout.Context, h Host, url string, r image.Rectangle, c *colors) {
	paintAvatar(gtx, h.Images(), url, r, c)
}

func paintAvatar(gtx layout.Context, imgs *imgcache.Cache, url string, r image.Rectangle, c *colors) {
	radius := avatarRadius(r.Dx())
	if isDefaultBotAvatar(url) {
		if img, ok := defaultBotAvatarImage(); ok {
			richview.DrawImage(gtx.Ops, img, r, radius, true)
			return
		}
	}
	if url != "" && imgs != nil {
		if img, ok := imgs.Get(url, r.Dx()); ok {
			richview.DrawImage(gtx.Ops, img, r, radius, true)
			return
		}
	}
	rrect(gtx.Ops, r, radius, c.Skeleton)
}

// drawEmoji paints an emoji image contained in r.
func drawEmoji(gtx layout.Context, h Host, url string, r image.Rectangle) bool {
	if url == "" || h.Images() == nil {
		return false
	}
	if img, ok := h.Images().Get(url, r.Dx()); ok {
		richview.DrawImage(gtx.Ops, img, r, 0, false)
		return true
	}
	return false
}

func shaperOf(h Host) *text.Shaper { return h.Theme().Shaper }

func f32pt(p image.Point) f32.Point { return f32.Pt(float32(p.X), float32(p.Y)) }

// cssText is s under a native's CSS: color, font-weight and font-style
// where a rule wins over the native's own (slackc's base sheet decides).
func cssText(st slackc.Styled, s textStyle) textStyle {
	if st.Style() == nil {
		return s
	}
	s.col = st.Color(s.col)
	if st.Has("font-weight") {
		s.weight = font.Weight(st.FontWeight(400) - 400)
	}
	s.italic = st.Italic(s.italic)
	return s
}

// decoLabel is label in cssText(st, s), with the text-decoration CSS
// gives the text (its own or propagated from the row).
func decoLabel(gtx layout.Context, th *theme.Theme, st slackc.Styled, s textStyle, txt string) layout.Dimensions {
	if st.Style() == nil {
		return label(gtx, th, s, txt)
	}
	s = cssText(st, s)
	d := label(gtx, th, s, txt)
	if lines, dc := st.Deco(); lines.Lines() != 0 && d.Size.X > 0 {
		if dc.A == 0 {
			dc = s.col
		}
		slackc.DrawDecoration(gtx.Ops, lines, dc, 0, float32(d.Size.Y-d.Baseline), float32(d.Size.X), s.size*gtx.Metric.PxPerSp)
	}
	return d
}

// styledLabel is decoLabel inside the native's box effects (st.Wrap:
// opacity, background, margins…). The zero Styled is label.
func styledLabel(gtx layout.Context, th *theme.Theme, st slackc.Styled, s textStyle, txt string) layout.Dimensions {
	if !st.Active() {
		return decoLabel(gtx, th, st, s, txt)
	}
	return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions { return decoLabel(gtx, th, st, s, txt) })
}
