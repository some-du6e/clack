package canvasview

// EmbeddedPublicLinkUnfurl (client-boot-shared 9kXl), the card Slack draws
// for a web link's own preview in a canvas, and EmptyUnfurl's "Double-check
// this link" banner (As10, Alert MlDl). Build 889ac449a32d; the notes are
// in /root/clack-ops/scratch/link-url-box-2/slack-cards.md §3, §6.

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// nsClassicUnfurl is 9kXl's namespace.
var nsClassicUnfurl = i18n.NS("classic-unfurl")

// cardInk are the kit tokens the cards' type takes (kit.css
// .sk-client-theme--light / --dark).
type cardInk struct {
	primary        color.NRGBA // --sk_primary_foreground
	hgl1           color.NRGBA // --dt_color-content-hgl-1: the base a's colour
	highlightHover color.NRGBA // --sk_highlight_hover
	high50         color.NRGBA // --sk_foreground_high at .5
	pry            color.NRGBA // --dt_color-content-pry
	alertBg        color.NRGBA // --dt_color-surf-hgl-3
	alertIcon      color.NRGBA // --dt_color-content-hgl-3
	thumbRule      color.NRGBA // --sk_primary_foreground at .13
}

func cardInks(dark bool) cardInk {
	if dark {
		return cardInk{primary: color.NRGBA{209, 210, 211, 255}, hgl1: color.NRGBA{0x2b, 0xa5, 0xce, 255},
			highlightHover: color.NRGBA{64, 179, 228, 255}, high50: color.NRGBA{232, 232, 232, 128},
			pry: color.NRGBA{0xf8, 0xf8, 0xf8, 255}, alertBg: color.NRGBA{0xa0, 0x73, 0x00, 0x2e},
			alertIcon: color.NRGBA{0xde, 0xa7, 0x00, 255}, thumbRule: color.NRGBA{209, 210, 211, 33}}
	}
	return cardInk{primary: color.NRGBA{29, 28, 29, 255}, hgl1: color.NRGBA{0x12, 0x64, 0xa3, 255},
		highlightHover: color.NRGBA{11, 76, 140, 255}, high50: color.NRGBA{29, 28, 29, 128},
		pry: color.NRGBA{0x1d, 0x1c, 0x1d, 255}, alertBg: color.NRGBA{0xff, 0xc6, 0x00, 0x2e},
		alertIcon: color.NRGBA{0x6b, 0x50, 0x00, 255}, thumbRule: color.NRGBA{29, 28, 29, 33}}
}

// publicCard is what EmbeddedPublicLinkUnfurl draws of an attachment
// (outside a hover card): the top row's title, the text, the bottom row's
// icon, source and when the link was added, and the thumbnail.
type publicCard struct {
	topIcon  string         // a tweet's author icon (the generic top row has none)
	title    *richview.View // the top row: the title, linked to title_link
	text     *richview.View // ContentText (nil with no text, or text previews off)
	lines    int            // the line cap text was last laid out with
	botIcon  string         // the bottom row's icon, service_icon || author_icon
	source   *richview.View // "sourceName | Added on …"
	thumb    string         // thumb_url || image_url
	thumbW   int            // its dims, for the box
	thumbH   int
	hasThumb bool
}

// readableTs is 9kXl's en: "Added Today", "Added Yesterday", "Added on Nov
// 3rd", "Added on Nov 3rd, 2025", of the container's ts (when the link was
// added to the canvas, not the attachment's time).
func readableTs(ts string, now time.Time) string {
	t, ok := tsTime(ts)
	if !ok {
		return ""
	}
	loc := i18n.Locale()
	d := i18n.RelativeDay(loc, t, now, false)
	if d == "" {
		days := now.Sub(t)
		exclude := t.Year() == now.Year() || t.Year() <= now.Year() && days >= 0 && days <= 31*24*time.Hour // shouldExcludeYear
		d = i18n.CalendarDate(loc, t, i18n.CalendarOpts{ShortenMonth: true, ExcludeYear: exclude})
	}
	if strings.Contains(d, "Today") || strings.Contains(d, "Yesterday") { // Slack tests the English words
		return nsClassicUnfurl.T("Added {readableTs}", "readableTs", d)
	}
	return nsClassicUnfurl.T("Added on {readableTs}", "readableTs", d)
}

// tsTime is a Slack ts as a time.
func tsTime(ts string) (time.Time, bool) {
	f, err := strconv.ParseFloat(ts, 64)
	if err != nil || f <= 0 {
		return time.Time{}, false
	}
	sec, frac := math.Modf(f)
	return time.Unix(int64(sec), int64(frac*1e9)), true
}

// newPublicCard builds the card's views from the container's attachment.
func newPublicCard(r richtext.Resolver, a *slack.Attachment, containerTS string, now time.Time) *publicCard {
	p := &publicCard{}
	twitter := a.ServiceName == "twitter"
	// the top row: a tweet's author, else the attachment's title
	title, titleURL := a.Title, a.TitleLink
	if twitter {
		p.topIcon = firstNonEmpty(a.AuthorIcon, a.ServiceIcon)
		title, titleURL = a.AuthorName, ""
		if a.AuthorSubname != "" {
			titleURL = "https://twitter.com/" + a.AuthorSubname
		}
	}
	para := func(sp ...richtext.Span) richtext.Doc {
		return richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: sp}}}
	}
	if title != "" {
		sp := richtext.Span{Kind: richtext.Text, Text: title, Style: richtext.Bold}
		if u := richtext.SafeURL(titleURL); u != "" {
			sp = richtext.Span{Kind: richtext.Link, Text: title, URL: u, Style: richtext.Bold}
		}
		p.title = richview.New(para(sp), richview.Options{MaxLines: 1})
	}
	if a.Text != "" && !uipref.Get().NoTextPreviews { // expand_non_media_attachments: ContentText's Mrkdwn
		var text []richtext.Block
		if r != nil {
			att := *a
			att.Title, att.TitleLink, att.Fields, att.Blocks = "", "", nil, nil
			for _, b := range richtext.FromMessage(r, &slack.Message{Type: "message", Attachments: []slack.Attachment{att}}).Blocks {
				if b.Kind == richtext.AttachmentBlock && b.Att != nil {
					text = append(text, b.Att.Text...)
				}
			}
		}
		if len(text) == 0 {
			text = para(richtext.Span{Kind: richtext.Text, Text: a.Text}).Blocks
		}
		p.text = richview.New(richtext.Doc{Blocks: text}, richview.Options{MaxLines: 6})
		p.lines = 6
	}
	// the bottom row: the source and when the link was added
	srcName, srcURL := firstNonEmpty(a.ServiceName, a.AuthorName), firstNonEmpty(a.ServiceURL, a.AuthorLink)
	p.botIcon = firstNonEmpty(a.ServiceIcon, a.AuthorIcon)
	if twitter {
		srcName, p.botIcon = nsClassicUnfurl.T("Twitter"), ""
		srcURL = ""
		if a.AuthorSubname != "" {
			srcURL = "https://twitter.com/" + a.AuthorSubname
		}
	}
	stamp := readableTs(containerTS, now)
	var sp []richtext.Span
	if srcName != "" {
		if u := richtext.SafeURL(srcURL); u != "" {
			sp = append(sp, richtext.Span{Kind: richtext.Link, Text: srcName, URL: u})
		} else {
			sp = append(sp, richtext.Span{Kind: richtext.Text, Text: srcName})
		}
	}
	if stamp != "" {
		if len(sp) > 0 {
			sp = append(sp, richtext.Span{Kind: richtext.Text, Text: " | "})
		}
		sp = append(sp, richtext.Span{Kind: richtext.Text, Text: stamp})
	}
	if len(sp) > 0 {
		p.source = richview.New(para(sp...), richview.Options{MaxLines: 1})
	}
	switch {
	case a.ThumbURL != "":
		p.thumb, p.thumbW, p.thumbH = a.ThumbURL, a.ThumbWidth, a.ThumbHeight
	case a.ImageURL != "":
		p.thumb, p.thumbW, p.thumbH = a.ImageURL, a.ImageWidth, a.ImageHeight
	}
	p.hasThumb = p.thumb != ""
	return p
}

// views are the card's text views.
func (p *publicCard) views() []*richview.View {
	if p == nil {
		return nil
	}
	return []*richview.View{p.title, p.text, p.source}
}

// layoutPublic draws EmbeddedPublicLinkUnfurl w wide and returns its
// height: the content padded 12 (the top row, the text 10 above and below,
// the bottom row), and at 400 px or more with images on, the thumbnail on
// the right (120 wide, 12 from the content, a 1px rule on its left, the
// image covering it).
func (v *View) layoutPublic(gtx layout.Context, c *linkCard, w int) int {
	p := c.pub
	if p == nil {
		return 0
	}
	ink := cardInks(v.h.Theme().Dark)
	pad := gtx.Dp(12)
	// r: at 400 px or more with a thumb_url or image_url the text takes 2
	// lines (whatever the images pref), and the thumbnail shows with
	// images on
	wide := p.hasThumb && w >= gtx.Dp(400)
	thumb := wide && !uipref.Get().NoInlineImgs
	thumbW := 0
	if thumb {
		thumbW = gtx.Dp(120) + gtx.Dp(12)
	}
	if lines := map[bool]int{true: 2, false: 6}[wide]; p.text != nil && lines != p.lines {
		// the same view, its cap changed: a new view every frame would be
		// a new pointer tag every frame (a frame loop under a resting
		// pointer, its links dead)
		p.text.SetOptions(richview.Options{MaxLines: lines})
		p.lines = lines
	}
	cw := max(0, w-2*pad-thumbW)
	y := pad
	row := func(rv *richview.View, st *richview.Style, icon string, iconSz int) int {
		x := 0
		if icon != "" {
			x = iconSz + gtx.Dp(6) // __left's margin-right
		}
		var h int
		func() {
			defer op.Offset(image.Pt(pad+x, y)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(max(0, cw-x), gtx.Constraints.Max.Y)}
			h = rv.Layout(g, st, v.handler).Size.Y
		}()
		if icon != "" {
			h = max(h, iconSz)
			if u := slack.ProxyImage(icon); u != "" {
				if img, ok := v.h.Images().Get(u, gtx.Dp(32)); ok {
					func() {
						defer op.Offset(image.Pt(pad, y+(h-iconSz)/2)).Push(gtx.Ops).Pop() // align-items: center
						theme.Image(gtx, img, image.Pt(iconSz, iconSz), gtx.Dp(4))
					}()
				}
			}
		}
		return h
	}
	if p.title != nil {
		st := v.styles.pubTitle
		if c.hover { // __title--on_hover while the card is hovered
			st = v.styles.pubTitleHot
		}
		y += row(p.title, st, p.topIcon, gtx.Dp(20))
	}
	if p.text != nil {
		y += gtx.Dp(10)
		func() {
			defer op.Offset(image.Pt(pad, y)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(cw, gtx.Constraints.Max.Y)}
			y += p.text.Layout(g, v.styles.card, v.handler).Size.Y
		}()
		y += gtx.Dp(10)
	}
	if p.source != nil {
		y += row(p.source, v.styles.pubSmall, p.botIcon, gtx.Dp(16))
	}
	h := y + pad
	if thumb {
		x := w - gtx.Dp(120)
		theme.Fill(gtx, image.Rect(x, 0, x+max(1, gtx.Dp(1)), h), ink.thumbRule)
		if u := slack.ProxyImage(p.thumb); u != "" {
			if img, ok := v.h.Images().Get(u, gtx.Dp(240)); ok {
				func() {
					// the card's overflow:hidden rounds the thumbnail's right corners
					defer clip.UniformRRect(image.Rect(0, 0, w, h), gtx.Dp(12)).Push(gtx.Ops).Pop()
					defer op.Offset(image.Pt(x+max(1, gtx.Dp(1)), 0)).Push(gtx.Ops).Pop()
					theme.Image(gtx, img, image.Pt(gtx.Dp(120)-max(1, gtx.Dp(1)), h), 0)
				}()
			}
		}
	}
	return h
}

// layoutPublicSkeleton is LoadingPublicLinkUnfurl: four MissingText lines
// (large, large, medium, medium: 100%, 100%, 50%, 50%) in a column padded
// 12, and a 120px MissingImage, shimmering as the fallback's skeleton.
func (v *View) layoutPublicSkeleton(gtx layout.Context, w int, now time.Time) int {
	sz := gtx.Dp(120)
	h := v.layoutCardSkeletonIn(gtx, max(0, w-sz), now)
	col := v.skeletonInk(gtx, now)
	func() { // the card's overflow:hidden rounds its right corners
		defer clip.UniformRRect(image.Rect(0, 0, w, max(h, sz)), gtx.Dp(12)).Push(gtx.Ops).Pop()
		theme.RRect(gtx, image.Rect(w-sz, 0, w, sz), 0, col)
	}()
	return max(h, sz)
}

// alertHeight lays out EmptyUnfurl's "Double-check this link" Alert at y,
// w wide (boxed, level warning, aligned left: surf-hgl-3 behind, radius 8,
// 12px 16px padding, the 20px warning icon in content-hgl-3 12 left of
// its 15px message in content-pry) and returns its height.
func (v *View) layoutAlert(gtx layout.Context, c *linkCard, x, y, w int) int {
	if c.alert == nil {
		c.alert = richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph,
			Spans: []richtext.Span{{Kind: richtext.Text, Text: nsFiles.T("Double-check this link")}}}}}, richview.Options{})
	}
	ink := cardInks(v.h.Theme().Dark)
	padX, padY, icon := gtx.Dp(16), gtx.Dp(12), gtx.Dp(20)
	tx := padX + icon + gtx.Dp(12)
	m := op.Record(gtx.Ops)
	var th int
	func() {
		defer op.Offset(image.Pt(x+tx, y+padY)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(max(0, w-tx-padX), gtx.Constraints.Max.Y)}
		th = c.alert.Layout(g, v.styles.cardAlert, v.handler).Size.Y
	}()
	call := m.Stop()
	h := th + 2*padY
	theme.RRect(gtx, image.Rect(x, y, x+w, y+h), gtx.Dp(8), ink.alertBg)
	func() {
		defer op.Offset(image.Pt(x+padX, y+padY+gtx.Dp(1))).Push(gtx.Ops).Pop() // translateY(1px)
		g := gtx
		g.Constraints = layout.Exact(image.Pt(icon, icon))
		icons.GetSlack("warning")(g, unit.Dp(float32(icon)/gtx.Metric.PxPerDp), ink.alertIcon)
	}()
	call.Add(gtx.Ops)
	return h
}
