package ui

// A canvas's or list's conversation in the sidebar (core/sidebar_docs.go):
// a document row draws the document's icon and its title with emoji
// (Un00 with documentType: FormatTextWithEmoji), is selected while its
// document is the main view (x6Wj getViewedDocumentFileId), and a click
// opens the document instead of the conversation (r5i1
// openFileChannelFromSidebar → openListFromSidebar / the canvas view).

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// sbDocIcon is a document row's prefix icon.
func (a *App) sbDocIcon(doc string) string {
	if c := a.Core(); c != nil {
		if ic := c.SidebarDocIcon(doc); ic != "" {
			return ic
		}
	}
	if doc == "list" {
		return "bullet-list"
	}
	return "canvas"
}

// openSidebarConv opens a sidebar row's conversation: a file channel's
// document in the main view when Slack opens it there, else the
// conversation.
func (a *App) openSidebarConv(id string) {
	if c := a.Core(); c != nil {
		switch kind, file := c.SidebarFileTarget(id); kind {
		case "list":
			a.OpenListFull(file)
			return
		case "canvas":
			a.OpenCanvasFull(file)
			return
		}
	}
	a.Navigate("c/" + id)
}

// sbDocSelected is a file channel row's selected state: its document is
// the main view.
func sbDocSelected(conv *convInfo, r Route) bool {
	if conv == nil || !conv.File || conv.FileID == "" {
		return false
	}
	return canvasRouteID(r) == conv.FileID || listRouteID(r) == conv.FileID
}

// sbEmojiSize is a document name's emoji (Slack's emojiSize SMALL next
// to the 15px name; theme.textWithEmoji's 6/5).
func sbEmojiSize(gtx layout.Context, size unit.Sp) int { return gtx.Sp(size * 6 / 5) }

// sbSpansWidth is the natural width of a document name.
func (a *App) sbSpansWidth(gtx layout.Context, spans []richtext.Span, size unit.Sp, weight font.Weight) int {
	es := sbEmojiSize(gtx, size)
	w := 0
	for _, s := range spans {
		if s.Kind == richtext.Emoji && s.URL != "" {
			w += es + gtx.Dp(1)
			continue
		}
		if t := sbSpanText(s); t != "" {
			w += a.th.Measure(gtx, size, weight, t)
		}
	}
	return w
}

func sbSpanText(s richtext.Span) string { return strings.ReplaceAll(s.Text, "\n", " ") }

// sbSpans draws a document name on one line of height h: text runs and
// emoji images, cut with an ellipsis at the constraint.
func (a *App) sbSpans(gtx layout.Context, spans []richtext.Span, size unit.Sp, weight font.Weight, col color.NRGBA, h int) layout.Dimensions {
	maxW := gtx.Constraints.Max.X
	es := sbEmojiSize(gtx, size)
	ell := a.th.Measure(gtx, size, weight, "…")
	x := 0
	for _, s := range spans {
		if s.Kind == richtext.Emoji && s.URL != "" {
			if x+es > maxW {
				if x+ell <= maxW {
					at(gtx, image.Pt(x, 0), image.Pt(ell, h), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
							return a.th.Label(gtx, size, weight, col, "…")
						})
					})
				}
				break
			}
			url := s.URL
			at(gtx, image.Pt(x, (h-es)/2), image.Pt(es, es), func(gtx layout.Context) layout.Dimensions {
				if img, ok := a.images.Get(url, es*2); ok {
					return theme.Image(gtx, img, image.Pt(es, es), 0)
				}
				return layout.Dimensions{Size: image.Pt(es, es)}
			})
			x += es + gtx.Dp(1)
			continue
		}
		txt := sbSpanText(s)
		if txt == "" {
			continue
		}
		tw := a.th.Measure(gtx, size, weight, txt)
		cut := x+tw > maxW
		if cut {
			tw = maxW - x
			if tw <= 0 {
				break
			}
		}
		at(gtx, image.Pt(x, 0), image.Pt(tw, h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, size, weight, col, txt) // MaxLines 1: the cut run ends in "…"
			})
		})
		x += tw
		if cut {
			break
		}
	}
	if x > maxW {
		x = maxW
	}
	return layout.Dimensions{Size: image.Pt(x, h)}
}
