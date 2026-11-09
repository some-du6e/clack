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
	"sync/atomic"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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

// AttachmentRemover is a Handler that can remove a message's link
// preview (the unfurl's remove button): Slack's "Remove preview?" dialog,
// then chat.deleteAttachment.
type AttachmentRemover interface {
	RemoveAttachment(id string)
}

// LinkTextHandler is a Handler that also wants a link's text when it is
// clicked (Slack's suspicious-link check compares it with the URL).
type LinkTextHandler interface {
	OpenLink(url, text string)
}

// Elements draws Block Kit interactive elements as live widgets
// (internal/ui/blockkit). With Options.Elements set, every run of
// elements (an actions block, a section accessory, an attachment's action
// buttons) gets a slot in the cached layout that the owner sizes and
// draws each frame. Without it richview draws them as static buttons that
// call Handler.Button.
type Elements interface {
	// ElementsSize is the run's size within maxW px. A section accessory
	// (accessory true) that sits under the section's text instead of at
	// its right (checkboxes, radio buttons, multi-selects) reports stack.
	ElementsSize(bs []richtext.Button, maxW int, accessory bool, m unit.Metric) (size image.Point, stack bool)
	// LayoutElements draws the run in gtx.Constraints (exactly the slot).
	LayoutElements(gtx layout.Context, bs []richtext.Button, accessory bool) layout.Dimensions
	// ElementsVersion changes whenever sizes may have changed (relayout).
	ElementsVersion() uint32
}

// Palette holds every color richview paints with. Defaults are Slack's
// 2026 light theme; use DarkPalette for dark mode.
type Palette struct {
	Text, Text2, Text3, Link       color.NRGBA
	Edited, CardText, CardFooter   color.NRGBA
	MentionText, MentionBg         color.NRGBA
	MentionHoverBg                 color.NRGBA
	SlugHoverText                  color.NRGBA // a hovered message_mention pill's text (0: unchanged)
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
	TooltipBg, TooltipText         color.NRGBA // emoji :name: tooltips
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
		SlugHoverText:   rgb(0x0b, 0x4c, 0x8c), // .p-rich_text_slug:hover
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
		TooltipBg: rgb(29, 28, 29), TooltipText: rgb(255, 255, 255),
	}.flat()
}

// DarkPalette is Slack's dark theme.
func DarkPalette() Palette {
	p := LightPalette()
	p.Text, p.Text2, p.Text3, p.Link = rgb(185, 186, 189), rgb(171, 171, 173), rgb(209, 210, 211), rgb(29, 155, 209)
	p.Edited, p.CardText, p.CardFooter = rgb(134, 134, 134), rgb(209, 210, 211), rgb(171, 171, 173)
	p.FileIconBg, p.FileIconText = rgba(232, 232, 232, 0.06), rgb(171, 171, 173)
	p.MentionText, p.MentionBg, p.MentionHoverBg = rgb(29, 155, 209), rgba(29, 155, 209, 0.1), rgba(29, 155, 209, 0.2)
	p.SlugHoverText = color.NRGBA{} // the dark theme's hover color isn't in the async CSS chunks: keep the pill's own
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
	p.TooltipBg, p.TooltipText = rgb(232, 232, 232), rgb(26, 29, 33)
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
	// EmojiURL maps a standard emoji image stem to its URL (the theme's
	// emoji style); nil uses Slack's apple set.
	EmojiURL func(stem string) string

	// LightTextFade cancels the extra weight Gio's linear-light glyph
	// blending gives light text on dark backgrounds (luminance >= 0.6)
	// compared with Chrome: such glyphs are drawn with alpha
	// 1 - LightTextFade/pxPerDp. The excess is an edge effect, so it halves
	// at 2x. Measured on Lato 15px #d1d2d3 on #1a1d21 against Chrome: 0.235
	// matches ink within ±1% at 1x and 2x. 0 = off.
	LightTextFade float32
	// LightEmbolden grows light glyphs (light-on-dark bodies) by
	// LightEmbolden·(1 - 1/pxPerDp) px: nothing at 1x, where linear-light
	// blending already makes them heavy enough, and up to it at high scales,
	// where that excess fades and dark-mode bodies came out ~7% light at 2x.
	LightEmbolden float32

	// Embolden strokes glyph outlines with this width (in pixels, at any
	// scale) to match the
	// browser's heavier text rendering; 0 disables it.
	Embolden float32

	gen   uint32 // from styleGens on SetPalette/Invalidate; Views re-record when it changes
	words map[wordKey]*shaped
	prev  map[wordKey]*shaped // previous generation (see word)
	space map[faceKey]spaceMetrics
}

// NewStyle builds a Style with Slack's defaults.
func NewStyle(sh *text.Shaper, imgs Images, p Palette) *Style {
	return &Style{
		Shaper: sh, Images: imgs, Palette: p,
		Face:     theme.Typeface,
		Mono:     "Roboto Mono, " + theme.MonoTypeface + ", Monaco, Menlo, Consolas, \"Courier New\", monospace",
		TextSize: 15, LineHeight: 22, Embolden: 0.3,
		gen:   styleGens.Add(1),
		words: make(map[wordKey]*shaped, 4096),
		space: make(map[faceKey]spaceMetrics, 16),
	}
}

// SetPalette switches colors (e.g. dark mode) and makes every View re-record.
func (s *Style) SetPalette(p Palette) {
	s.Palette = p
	s.gen = styleGens.Add(1)
}

// styleGens numbers style generations across every Style: Views compare
// only the generation, and a View drawn with the dark style after the
// light one (one Style per shaper) must not see an equal number.
var styleGens atomic.Uint32

// Invalidate forces every View to re-layout (e.g. after the font changed).
func (s *Style) Invalidate() {
	s.gen = styleGens.Add(1)
	s.prev = nil
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
	// CompactFiles is MessageFiles' shouldUseCompactStack: the files
	// without FilesMetaTextWrapper's title line (filemeta.go).
	CompactFiles bool
	// Secondary draws the body in the secondary text color (Text3) but
	// keeps mentions as pills: read Activity rows. Muted is for system
	// messages, which also turn member mentions into plain names.
	Secondary bool
	// MaxWidth caps the content width in Dp (0 = no cap beyond the constraints).
	MaxWidth float32
	// Compact uses 13px/18px text (previews, reply bars, search results).
	Compact bool
	// Channel and TS identify the message, for the Slack-shaped props of
	// its attachment components (activeChannelId, messageTs).
	Channel, TS string
	// Elements, if set, draws the doc's interactive elements (see
	// Elements). Use a comparable (pointer) implementation.
	Elements Elements
	// Truncate cuts long Block Kit section text (and each field) at 300
	// characters or 5 line breaks with " Show more", as Slack's renderer
	// does in its thread, activity, threads, unreads, saved and pinned
	// views (never in a channel). Needs Channel and TS: expanding is kept
	// for the session under channel/ts/block_id.
	Truncate bool
	// Own: the viewer sent the message (and it isn't a pinned_item), so
	// its link previews get Slack's remove button (canDelete, 4BNQ).
	Own bool
}

// lightEmbolden is the extra outline (px) for glyph color c at scale pxPerDp.
func (s *Style) lightEmbolden(c color.NRGBA, pxPerDp float32) float32 {
	if s.LightEmbolden <= 0 || pxPerDp <= 1 {
		return 0
	}
	if y := (0.299*float32(c.R) + 0.587*float32(c.G) + 0.114*float32(c.B)) / 255; y < 0.6 {
		return 0
	}
	return s.LightEmbolden * (1 - 1/pxPerDp)
}

// textColor applies LightTextFade to light glyph colors at scale pxPerDp.
func (s *Style) textColor(c color.NRGBA, pxPerDp float32) color.NRGBA {
	if s.LightTextFade <= 0 || pxPerDp <= 0 {
		return c
	}
	if y := (0.299*float32(c.R) + 0.587*float32(c.G) + 0.114*float32(c.B)) / 255; y >= 0.6 {
		a := 1 - s.LightTextFade/pxPerDp
		if a < 0.5 {
			a = 0.5
		}
		c.A = uint8(float32(c.A)*a + 0.5)
	}
	return c
}
