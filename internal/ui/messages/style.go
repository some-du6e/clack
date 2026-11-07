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

	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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
	Pinned, PinnedHover, Highlight                    color.NRGBA
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
	ReactSelfBg: rgb(0xe3f8ff), ReactSelfHoverBg: rgb(0xd5f3ff),
	ReactSelfBorder: rgb(0x1264a3), ReactSelfText: rgb(0x1264a3),
	Link: rgb(0x1264a3), NewLine: rgb(0xe01e5a), DayLine: rgb(0xdddddd), PillBorder: rgba(0x1d1c1d, 0.13),
	Shadow: rgba(0x000000, 0.08), ToolbarIcon: rgb(0x616061), ToolbarHoverBg: rgba(0x1d1c1d, 0.04),
	Tooltip: rgb(0x1d1c1d), TooltipText: rgb(0xffffff), Skeleton: rgb(0xeaeaea),
	ThreadBorder: rgba(0x1d1c1d, 0.13), ThreadBg: rgb(0xffffff), JumpBg: rgb(0x1264a3),
	JumpText: rgb(0xffffff), FailedText: rgb(0xe01e5a), BorderStrong: rgba(0x1d1c1d, 0.3),
	Editing: rgba(0xf2c744, 0.2), ScrollThumb: rgba(0x1d1c1d, 0.3), ScrollThumbHover: rgba(0x1d1c1d, 0.5), Selection: rgba(0x1d9bd1, 0.3),
	SystemText: rgb(0x454447), HeaderBorder: rgba(0x1d1c1d, 0.13), CloseHover: rgba(0x1d1c1d, 0.06),
}

var darkColors = colors{
	Bg: rgb(0x1a1d21), Hover: rgb(0x222529), Text: rgb(0xd1d2d3), Text2: rgb(0xababad), Text3: rgb(0xd1d2d3),
	Edited: rgb(0x868686), PinIcon: rgb(0xe8912d), Muted70: rgba(0xe8e8e8, 0.7), TimeSystem: rgb(0xababad),
	Pinned: rgba(0xf2c744, 0.1), PinnedHover: rgba(0xf2c744, 0.14), Highlight: rgba(0xf2c744, 0.1),
	BadgeBg: rgba(0xe8e8e8, 0.13), BadgeText: rgba(0xe8e8e8, 0.7),
	ReactBg: rgba(0xe8e8e8, 0.06), ReactHoverBorder: rgba(0xe8e8e8, 0.3),
	ReactSelfBg: rgba(0x1d9bd1, 0.2), ReactSelfHoverBg: rgba(0x1d9bd1, 0.3),
	ReactSelfBorder: rgb(0x1d9bd1), ReactSelfText: rgb(0x1d9bd1),
	Link: rgb(0x1d9bd1), NewLine: rgb(0xe01e5a), DayLine: rgba(0xe8e8e8, 0.13), PillBorder: rgba(0xe8e8e8, 0.13),
	Shadow: rgba(0x000000, 0.3), ToolbarIcon: rgb(0xababad), ToolbarHoverBg: rgb(0x222529),
	Tooltip: rgb(0xe8e8e8), TooltipText: rgb(0x1a1d21), Skeleton: rgb(0x2c2d30),
	ThreadBorder: rgba(0xe8e8e8, 0.13), ThreadBg: rgb(0x1a1d21), JumpBg: rgb(0x1d9bd1),
	JumpText: rgb(0xffffff), FailedText: rgb(0xe01e5a), BorderStrong: rgba(0xe8e8e8, 0.3),
	Editing: rgba(0xf2c744, 0.14), ScrollThumb: rgba(0xe8e8e8, 0.3), ScrollThumbHover: rgba(0xe8e8e8, 0.5), Selection: rgba(0x1d9bd1, 0.4),
	SystemText: rgb(0xababad), HeaderBorder: rgba(0xe8e8e8, 0.13), CloseHover: rgba(0xe8e8e8, 0.06),
}

func colorsFor(th *theme.Theme) *colors {
	flatOnce.Do(func() {
		lightFlat, darkFlat = flatten(lightColors), flatten(darkColors)
	})
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

// metrics are the row geometry in dp, measured from the 2026 client.
var mt = struct {
	padX, avatar, gap, contentX   float32
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
}{
	padX: 16, avatar: 36, gap: 8, contentX: 60,
	headPadTop: 8, padTop: 8, padBottom: 8,
	nameLine: 22, bodyMarginBottom: 4,
	tsGap: 8, badgeGap: 4,
	reactH: 24, reactGap: 4, reactTop: 4, reactEmoji: 16, reactBottom: 4,
	threadH: 34, threadAvatar: 24,
	dayH: 40, dayPillH: 28, unreadH: 20,
	toolbarBtn: 32, toolbarPad: 4, toolbarTop: -16, toolbarRight: 17,
	statusEmoji: 16,
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

// label draws txt; returns dims with Baseline set.
func label(gtx layout.Context, th *theme.Theme, s textStyle, txt string) layout.Dimensions {
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
