// Package richview lays out and paints a richtext.Doc (a Slack message body)
// with Gio: styled, wrapped text runs, inline emoji images, mention pills,
// inline code, pre blocks, quotes, lists, Block Kit blocks, attachments,
// unfurl cards and file cards. It supports clickable links/mentions, hover
// underline and text selection with copy.
//
// Layout is cached per (doc, width, scale) and painting replays a recorded
// op list, so a scrolled message costs almost nothing per frame.
package richview

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/op/paint"
	"gioui.org/text"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// Images is the subset of imgcache.Cache that richview needs. Get must not
// block; it returns ok=false until the image is decoded (and should
// invalidate the window when it arrives).
type Images interface {
	Get(url string, sizePx int) (paint.ImageOp, bool)
}

// Handler receives clicks on interactive content. Any method may be a no-op.
// Anchors are in the coordinates of the View that was clicked (see
// View.Origin for converting them).
type Handler interface {
	OpenURL(url string)
	OpenUser(id string, anchor image.Point)
	OpenChannel(id string)
	OpenUsergroup(id string, anchor image.Point)
	OpenImage(img richtext.Image)
	OpenFile(f *richtext.File)
	Button(b richtext.Button)
}

// Palette holds every color richview paints with. Defaults are Slack's
// 2026 light theme; use DarkPalette for dark mode.
type Palette struct {
	Text, Text2, Text3, Link       color.NRGBA
	Edited, CardText, CardFooter   color.NRGBA
	MentionText, MentionBg         color.NRGBA
	MentionHoverBg                 color.NRGBA
	SelfMentionText, SelfMentionBg color.NRGBA
	CodeText, CodeBg, CodeBorder   color.NRGBA
	PreBg, PreBorder               color.NRGBA
	QuoteBar                       color.NRGBA
	Border, BorderHover            color.NRGBA // cards
	Panel                          color.NRGBA // card background
	Placeholder                    color.NRGBA // image loading background
	Selection                      color.NRGBA
	Highlight                      color.NRGBA // Style Highlight
	Divider                        color.NRGBA
	ButtonBorder, ButtonHoverBg    color.NRGBA
	Primary, PrimaryText           color.NRGBA
	Danger, DangerText             color.NRGBA
	FileIconBg, FileIconPDF        color.NRGBA
	FileIconZip, FileIconText      color.NRGBA
	PlayBg, PlayFg                 color.NRGBA
}

func rgb(r, g, b uint8) color.NRGBA { return color.NRGBA{R: r, G: g, B: b, A: 255} }
func rgba(r, g, b uint8, a float32) color.NRGBA {
	return color.NRGBA{R: r, G: g, B: b, A: uint8(a*255 + 0.5)}
}

// Over composites c onto bg in sRGB space like a browser does with CSS
// rgba(), returning an opaque color. Gio blends in linear light, so
// translucent fills look lighter (and much brighter on dark) than in Slack.
func Over(c, bg color.NRGBA) color.NRGBA {
	if c.A == 255 {
		return c
	}
	a := float32(c.A) / 255
	mix := func(x, y uint8) uint8 { return uint8(float32(x)*a + float32(y)*(1-a) + 0.5) }
	return color.NRGBA{R: mix(c.R, bg.R), G: mix(c.G, bg.G), B: mix(c.B, bg.B), A: 255}
}

// flat pre-composites the palette's translucent fills over the card/pane
// color (see Over).
func (p Palette) flat() Palette {
	for _, c := range []*color.NRGBA{&p.MentionBg, &p.MentionHoverBg, &p.SelfMentionBg, &p.CodeBg, &p.PreBg,
		&p.Highlight, &p.FileIconBg, &p.Border, &p.BorderHover, &p.QuoteBar, &p.Divider, &p.ButtonBorder,
		&p.CodeBorder, &p.PreBorder} {
		*c = Over(*c, p.Panel)
	}
	return p
}

// LightPalette is Slack's light theme.
func LightPalette() Palette {
	return Palette{
		Text: rgb(29, 28, 29), Text2: rgb(97, 96, 97), Text3: rgb(69, 68, 71), Link: rgb(18, 100, 163),
		Edited: rgb(134, 134, 134), CardText: rgb(69, 68, 71), CardFooter: rgb(94, 93, 96),
		MentionText: rgb(18, 100, 163), MentionBg: rgba(54, 197, 240, 0.13), MentionHoverBg: rgba(29, 155, 209, 0.2),
		SelfMentionText: rgb(29, 28, 29), SelfMentionBg: rgba(255, 198, 0, 0.18),
		CodeText: rgb(192, 19, 67), CodeBg: rgba(29, 28, 29, 0.04), CodeBorder: rgba(29, 28, 29, 0.13),
		PreBg: rgba(29, 28, 29, 0.04), PreBorder: rgba(29, 28, 29, 0.13),
		QuoteBar: rgb(221, 221, 221),
		Border:   rgba(94, 93, 96, 0.13), BorderHover: rgba(94, 93, 96, 0.45),
		Panel: rgb(255, 255, 255), Placeholder: rgb(234, 234, 234),
		Selection: rgba(29, 155, 209, 0.3), Highlight: rgba(255, 198, 0, 0.18),
		Divider:      rgba(94, 93, 96, 0.45),
		ButtonBorder: rgba(94, 93, 96, 0.45), ButtonHoverBg: rgb(248, 248, 248),
		Primary: rgb(0, 122, 90), PrimaryText: rgb(255, 255, 255),
		Danger: rgb(224, 30, 90), DangerText: rgb(255, 255, 255),
		FileIconBg: rgba(29, 28, 29, 0.04), FileIconPDF: rgb(224, 30, 90), FileIconZip: rgb(97, 96, 97), FileIconText: rgb(97, 96, 97),
		PlayBg: rgba(0, 0, 0, 0.4), PlayFg: rgb(255, 255, 255),
	}.flat()
}

// DarkPalette is Slack's dark theme.
func DarkPalette() Palette {
	p := LightPalette()
	p.Text, p.Text2, p.Text3, p.Link = rgb(209, 210, 211), rgb(171, 171, 173), rgb(209, 210, 211), rgb(29, 155, 209)
	p.Edited, p.CardText, p.CardFooter = rgb(134, 134, 134), rgb(209, 210, 211), rgb(171, 171, 173)
	p.FileIconBg, p.FileIconText = rgba(232, 232, 232, 0.06), rgb(171, 171, 173)
	p.MentionText, p.MentionBg, p.MentionHoverBg = rgb(29, 155, 209), rgba(29, 155, 209, 0.1), rgba(29, 155, 209, 0.2)
	p.SelfMentionText, p.SelfMentionBg = rgb(232, 232, 232), rgba(242, 199, 68, 0.3)
	p.CodeText, p.CodeBg, p.CodeBorder = rgb(232, 145, 45), rgba(232, 232, 232, 0.04), rgba(232, 232, 232, 0.13)
	p.PreBg, p.PreBorder = rgba(232, 232, 232, 0.04), rgba(232, 232, 232, 0.13)
	p.QuoteBar = rgba(232, 232, 232, 0.2)
	p.Border, p.BorderHover = rgba(232, 232, 232, 0.13), rgba(232, 232, 232, 0.3)
	p.Panel, p.Placeholder = rgb(26, 29, 33), rgb(34, 37, 41)
	p.Divider = rgba(232, 232, 232, 0.13)
	p.ButtonBorder, p.ButtonHoverBg = rgba(232, 232, 232, 0.3), rgb(34, 37, 41)
	p.Primary = rgb(0, 122, 90)
	p.Highlight = rgba(242, 199, 68, 0.3)
	return p.flat()
}

// Style is shared by every View of one window. It owns the word shaping
// cache, so it must only be used from the UI goroutine.
type Style struct {
	Shaper  *text.Shaper
	Images  Images
	Face    font.Typeface // body font, default "Lato"
	Mono    font.Typeface // code font
	Palette Palette

	// Body text size and line height in Sp/Dp (Slack: 15/22).
	TextSize, LineHeight float32
	// Embolden strokes glyph outlines with this width (dp) to match the
	// browser's heavier text rendering; 0 disables it.
	Embolden float32

	gen   uint32 // bumped by SetPalette; Views re-record when it changes
	words map[wordKey]*shaped
	space map[faceKey]spaceMetrics
}

// NewStyle builds a Style with Slack's defaults.
func NewStyle(sh *text.Shaper, imgs Images, p Palette) *Style {
	return &Style{
		Shaper: sh, Images: imgs, Palette: p,
		Face:     "Lato",
		Mono:     "Roboto Mono, Monaco, Menlo, Consolas, \"Courier New\", monospace",
		TextSize: 15, LineHeight: 22, Embolden: 0.3,
		gen:   1,
		words: make(map[wordKey]*shaped, 4096),
		space: make(map[faceKey]spaceMetrics, 16),
	}
}

// SetPalette switches colors (e.g. dark mode) and makes every View re-record.
func (s *Style) SetPalette(p Palette) {
	s.Palette = p
	s.gen++
}

// Invalidate forces every View to re-layout (e.g. after the font changed).
func (s *Style) Invalidate() {
	s.gen++
	for k := range s.words {
		delete(s.words, k)
	}
	for k := range s.space {
		delete(s.space, k)
	}
}

// Options are per-message rendering flags.
type Options struct {
	Edited bool // append "(edited)" after the last text block
	Muted  bool // system message: body in the secondary color
	Dim    bool // pending/failed: draw at reduced opacity
	// MaxWidth caps the content width in Dp (0 = no cap beyond the constraints).
	MaxWidth float32
	// Compact uses 13px/18px text (previews, reply bars, search results).
	Compact bool
}
