package messages

// A thread on a file channel (a canvas's or a list's hidden conversation:
// a document mention's thread view, a comment's thread) names the file,
// never the conversation's own ":shortcode:" name behind a private
// channel's lock. Slack's thread Header (boot-shared O+gL, build
// 548618a2169f2a90) reads the file channel's file: for a canvas
// (canvasFileId: deprecatedIsFileChannel, getFileById, fileIsCanvas) it
// draws ku69 SmallFileEntity (useLeadingEmojiAsIcon, no detail text, no
// emoji tooltips), a link to the canvas at the thread (getFilePermalink
// with focus_thread_ts: Clack's c/<C>/m/<root> jump, which opens the
// canvas). Any other file channel is EXeY InlineChannelEntity, whose icon
// is getChannelIconType's file channel case (/HhV: "lists" for a list,
// "file" while the file is unknown). ThreadSubtitle draws no name beside a
// canvas in the primary view (hideChannelName), for any thread, and always
// draws it in the split view.

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// FileThreadHost is optional: the canvas a canvas's file channel's
// thread header names, as Slack's SmallFileEntity draws it, and whether
// the primary view is a canvas.
type FileThreadHost interface {
	// ThreadCanvas is canvas file channel ch's canvas: its icon (a Slack
	// icon name), the image of its title's leading emoji drawn instead
	// (emoji, "" for the icon) and its title without that emoji. ok is
	// false unless ch is a canvas's file channel whose file is known.
	ThreadCanvas(ch string) (icon, emoji string, title []richtext.Span, ok bool)
	// PrimaryCanvas reports whether the primary view is a canvas: a
	// canvas's full view, or the Activity panel's pane showing one.
	PrimaryCanvas() bool
}

// besideCanvas reports whether ThreadSubtitle hides the name beside the
// primary view: the thread's own channel there, or a canvas (not in the
// split view, where Slack never hides it).
func (v *ThreadView) besideCanvas() (hide bool) {
	if sv, ok := v.h.(interface{ InSplitView() bool }); ok && sv.InSplitView() {
		return false
	}
	if ph, ok := v.h.(PrimaryHost); ok && ph.PrimaryChannel() == v.ch {
		return true
	}
	fh, ok := v.h.(FileThreadHost)
	return ok && fh.PrimaryCanvas()
}

// fileSubtitle is the subtitle of a thread on file channel cv: the
// canvas's SmallFileEntity, else the channel entity with the file
// channel's icon. ok is false for an archived file channel that isn't a
// known canvas: getChannelIconType's archived icons come first (subtitle).
func (v *ThreadView) fileSubtitle(app *core.App, cv core.Conv) (HeaderSubtitle, bool) {
	if fh, ok := v.h.(FileThreadHost); ok {
		if icon, emoji, title, ok := fh.ThreadCanvas(v.ch); ok {
			if len(title) == 0 {
				title = []richtext.Span{{Kind: richtext.Text, Text: i18n.T("files", "Untitled")}}
			}
			return HeaderSubtitle{File: true, Icon: icon, Emoji: emoji, Spans: title, Text: richtext.PlainSpans(title)}, true
		}
	}
	if cv.IsArchived {
		return HeaderSubtitle{}, false
	}
	sub := HeaderSubtitle{Icon: "file", Text: cv.Name, Spans: richtext.EmojiText(app.S, cv.Name)}
	if _, kind, known := app.CanvasTitleOf(cv.FileID); known && kind == "list" {
		sub.Icon = "lists"
	}
	return sub, true
}

// fileEntityText is the file entity's title style: c-file_entity__text
// is bold, in the subtitle's 13px on c-base_entity--small's 20px line.
func fileEntityText(col color.NRGBA) textStyle {
	return textStyle{size: 13, weight: theme.Bold, col: col, lineH: 20}
}

// fileEntityWidth is SmallFileEntity's natural width: the 20px icon box,
// 8px, the title and its 8px margin.
func (v *ThreadView) fileEntityWidth(gtx layout.Context, th *theme.Theme, sub HeaderSubtitle) int {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(1<<20, gtx.Sp(20))}
	d := v.fileTitle(g, th, sub.Spans, fileEntityText(color.NRGBA{}))
	m.Stop()
	return dp(gtx, 20) + dp(gtx, 8) + d.Size.X + dp(gtx, 8)
}

// layoutFileEntity draws SmallFileEntity in col (the header link's 70%
// foreground) on a 20px line (c-base_entity--small, its parts centred):
// the 20px c-file_entity__icon box with the icon centred, 1em of the
// subtitle's 13px (SvgIcon's size inherit: [data-f1h] is 1em), or the
// 16px emoji at its left (c-file_entity_emoji_container, c-emoji__small),
// 8px, then the title on one line with an ellipsis. The line's baseline
// is the title's.
func (v *ThreadView) layoutFileEntity(gtx layout.Context, th *theme.Theme, sub HeaderSubtitle, col color.NRGBA) layout.Dimensions {
	h, box, es := gtx.Sp(20), dp(gtx, 20), dp(gtx, 16)
	switch {
	case sub.Emoji != "":
		drawEmoji(gtx, v.h, sub.Emoji, image.Rect(0, (h-es)/2, es, (h-es)/2+es))
	case sub.Icon != "":
		is := gtx.Sp(13)
		at(gtx, (box-is)/2, (h-is)/2, func(gtx layout.Context) layout.Dimensions {
			return icons.Get(icons.Slack(sub.Icon))(gtx, unit.Dp(float32(is)/gtx.Metric.PxPerDp), col)
		})
	}
	x := box + dp(gtx, 8)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(0, gtx.Constraints.Max.X-x), h)}
	var d layout.Dimensions
	at(gtx, x, 0, func(gtx layout.Context) layout.Dimensions {
		d = v.fileTitle(g, th, sub.Spans, fileEntityText(col))
		return d
	})
	return layout.Dimensions{Size: image.Pt(min(gtx.Constraints.Max.X, x+d.Size.X), h), Baseline: d.Baseline}
}

// fileTitle draws a file's title spans in s on one line box, cut with an
// ellipsis at the max width: the text, and the emoji as 16px images (an
// emoji the store couldn't name when the title was converted is resolved
// now, as Slack's Emoji component reads the store on every render, else
// drawn as its ":name:").
func (v *ThreadView) fileTitle(gtx layout.Context, th *theme.Theme, spans []richtext.Span, s textStyle) layout.Dimensions {
	maxW := gtx.Constraints.Max.X
	h := gtx.Sp(unit.Sp(s.lineH))
	es := dp(gtx, 16)
	app := v.h.Core()
	x, bl := 0, cssBaseline(gtx, s.size, s.lineH)
	for _, sp := range spans {
		if sp.Kind == richtext.Break || x >= maxW {
			break
		}
		if sp.Kind == richtext.Emoji {
			url := sp.URL
			if sp.KeptEmoji() && app != nil {
				url, _, _ = app.Emoji(sp.ID)
			}
			if url != "" {
				if x+es > maxW {
					break
				}
				drawEmoji(gtx, v.h, url, image.Rect(x, (h-es)/2, x+es, (h-es)/2+es))
				x += es
				continue
			}
		}
		txt := sp.Text
		if sp.Kind == richtext.Emoji && txt == "" {
			txt = ":" + sp.ID + ":"
		}
		if txt = strings.ReplaceAll(txt, "\n", " "); txt == "" {
			continue
		}
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(maxW-x, h)}
		d := at(g, x, 0, func(gtx layout.Context) layout.Dimensions {
			return lineBox(gtx, s, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, s, txt) })
		})
		x += d.Size.X
	}
	return layout.Dimensions{Size: image.Pt(min(x, maxW), h), Baseline: h - bl}
}
