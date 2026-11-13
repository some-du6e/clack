package canvasview

import (
	"image"
	"image/color"
	"net/url"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Canvas typography: Slack's canvas-controller CSS (the Quip editor's
// stylesheet, canvas-controller-d97a8e501e2ab778.css) under
// body.use-slack-font: Lato 15px on a 22px line; headings
// (--heading-N-font-size/line-height) and the title section
// (data-section-style 48) in --doc-theme-header-weight:900.
const (
	bodySize, bodyLine   = 15, 22
	h1Size, h1Line       = 22, 30
	h2Size, h2Line       = 18, 27
	h3Size, h3Line       = 15, 22
	titleSize, titleLine = 28, 34
	codeLine             = 20 // --text-code-line-height
	// list items: li[data-indent=N] margin-left 30px per level; the marker
	// container is a 24px pseudo-before plus 4px padding-right
	indentDp = 30
	markerDp = 28
	itemGap  = 6 // --list-item-bottom-offset between items
)

// docText is the document's text colour (.document-text-color-default:
// rgb(var(--quipUserTextPrimary)): #333333 light, #e6e6e6 under
// body.dark-theme), not Slack's message ink.
func docText(dark bool) color.NRGBA {
	if dark {
		return color.NRGBA{R: 0xe6, G: 0xe6, B: 0xe6, A: 0xff}
	}
	return color.NRGBA{R: 0x33, G: 0x33, B: 0x33, A: 0xff}
}

// placeholderText is .editor-default-section-text-overlay's colour.
func placeholderText(dark bool) color.NRGBA {
	if dark {
		return color.NRGBA{R: 0x9a, G: 0x9b, B: 0x9e, A: 0xff}
	}
	return color.NRGBA{R: 0x77, G: 0x75, B: 0x7a, A: 0xff}
}

// styleSet is one richview.Style per text size, per window shaper.
type styleSet struct {
	shaper                      *text.Shaper
	dark                        bool
	gen                         uint64
	body, h1, h2, h3, title, sm *richview.Style
	ph                          *richview.Style // the empty canvas's placeholder
	titlePh                     *richview.Style // the empty title's placeholder
}

func (s *styleSet) sync(th *theme.Theme, h Host) {
	if s.body != nil && s.shaper == th.Shaper && s.dark == th.Dark && s.gen == th.Gen {
		return
	}
	s.shaper, s.dark, s.gen = th.Shaper, th.Dark, th.Gen
	var imgs richview.Images
	if im := h.Images(); im != nil {
		imgs = im
	}
	ink := docText(th.Dark)
	mk := func(size, line float32, heading bool) *richview.Style {
		st := richview.NewStyle(th.Shaper, imgs, richview.LightPalette())
		st.Face = theme.Typeface
		st.EmojiURL = th.EmojiURL
		st.TextSize, st.LineHeight = size, line
		st.PreLine = codeLine
		if heading {
			st.BoldWeight = font.Black
		}
		p := richview.LightPalette()
		if th.Dark {
			p = richview.DarkPalette()
		}
		if tp, ok := richview.Themed(th); ok { // stylesheet theme tokens
			p = tp
		}
		p.Text = ink
		st.SetPalette(p)
		st.Embolden = theme.BodyEmboldenFor(ink)
		return st
	}
	s.body = mk(bodySize, bodyLine, false)
	s.h1 = mk(h1Size, h1Line, true)
	s.h2 = mk(h2Size, h2Line, true)
	s.h3 = mk(h3Size, h3Line, true)
	s.title = mk(titleSize, titleLine, true)
	s.sm = mk(13, 18, false)
	s.ph = mk(bodySize, bodyLine, false)
	pp := s.ph.Palette
	pp.Text = placeholderText(th.Dark)
	s.ph.SetPalette(pp)
	s.titlePh = mk(titleSize, titleLine, true)
	s.titlePh.SetPalette(pp)
}

func (s *styleSet) forKind(k canvas.Kind) *richview.Style {
	switch k {
	case canvas.Heading1:
		return s.h1
	case canvas.Heading2:
		return s.h2
	case canvas.Heading3:
		return s.h3
	}
	return s.body
}

// blk is a laid-out block: its richview (inline text, code, tables, images,
// file and link cards) or its children (quotes, callouts, columns).
type blk struct {
	b     *canvas.Block
	key   string // what the block draws (reuse across refetches)
	rv    *richview.View
	kids  []*blk
	cols  [][]*blk
	emoji *richview.View // callout emoji
	check widget.Clickable
	hit   widget.Clickable // click to edit
	edOK  bool             // canvas.Editable: opens in the editor
	edWhy string           // else what is in the way

	parent *blk     // the quote or callout holding this block
	inCell bool     // a table cell's block: drawn, not edited (tables don't open in Clack's editor)
	cells  [][]cell // Table: the rows' cells
	tscr   tableScroll
}

// cell is one table cell's blocks.
type cell struct{ kids []*blk }

// rebuild makes blocks for a newly fetched doc, keeping the laid-out ones
// that didn't change (a live update re-fetches the whole export).
func (v *View) rebuild(d *core.CanvasDoc) {
	old := map[string]*blk{}
	var collect func(bs []*blk)
	collect = func(bs []*blk) {
		for _, b := range bs {
			if b.key != "" {
				old[b.key] = b
			}
		}
	}
	collect(v.blocks)
	if v.shown == nil || v.shown.Info.Title != d.Info.Title || !sameSpans(v.shown.Info.TitleSpans, d.Info.TitleSpans) {
		v.title = nil
	}
	v.shown = d
	v.blocks = v.makeBlocks(d, d.Doc.Blocks, old)
	v.edit.docChanged(v)
}

func sameSpans(a, b []richtext.Span) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (v *View) makeBlocks(d *core.CanvasDoc, bs []canvas.Block, old map[string]*blk) []*blk {
	out := make([]*blk, 0, len(bs))
	for i := range bs {
		b := &bs[i]
		key := b.ID + "\x00" + string(rune('A'+b.Kind)) + "\x00" + canvas.Markdown(*b) + fileKey(d, b)
		if o, ok := old[key]; ok && len(b.Children) == 0 && len(b.Columns) == 0 && b.Kind != canvas.Table {
			o.b = b
			o.edOK, o.edWhy = canvas.Editable(*b)
			out = append(out, o)
			continue
		}
		x := &blk{b: b, key: key}
		x.edOK, x.edWhy = canvas.Editable(*b)
		switch b.Kind {
		case canvas.Table:
			x.cells = v.tableCells(d, b)
		case canvas.Quote, canvas.Callout:
			x.kids = v.makeBlocks(d, b.Children, old)
			for _, k := range x.kids {
				k.parent = x
			}
			if b.Kind == canvas.Callout && b.Emoji != "" {
				x.emoji = richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{v.emojiSpan(b.Emoji)}}}}, richview.Options{})
			}
		case canvas.Columns:
			for _, c := range b.Columns {
				x.cols = append(x.cols, v.makeBlocks(d, c, old))
			}
		default:
			if doc, ok := v.richDoc(d, b); ok {
				x.rv = richview.New(doc, richview.Options{Secondary: b.Kind == canvas.Check && b.Checked, Canvas: true})
			}
		}
		out = append(out, x)
	}
	return out
}

func fileKey(d *core.CanvasDoc, b *canvas.Block) string {
	if b.FileID == "" {
		return ""
	}
	if f, ok := d.Files[b.FileID]; ok {
		return "\x00" + f.Thumb + f.Title
	}
	return ""
}

func (v *View) emojiSpan(name string) richtext.Span {
	sp := richtext.Span{Kind: richtext.Emoji, ID: name}
	if a := v.h.Core(); a != nil {
		if u, t, ok := a.Emoji(name); ok {
			sp.URL, sp.Text = u, t
		}
	}
	return sp
}

// richDoc is a block as the richtext.Doc richview draws.
func (v *View) richDoc(d *core.CanvasDoc, b *canvas.Block) (richtext.Doc, bool) {
	one := func(rb richtext.Block) (richtext.Doc, bool) { return richtext.Doc{Blocks: []richtext.Block{rb}}, true }
	switch b.Kind {
	case canvas.Paragraph, canvas.Bullet, canvas.Numbered:
		return one(richtext.Block{Kind: richtext.Paragraph, Spans: v.spans(b.Spans)})
	case canvas.Check:
		sp := v.spans(b.Spans)
		if b.Checked {
			sp = withStyle(sp, richtext.Strike)
		}
		return one(richtext.Block{Kind: richtext.Paragraph, Spans: sp})
	case canvas.Heading1, canvas.Heading2, canvas.Heading3:
		return one(richtext.Block{Kind: richtext.Paragraph, Spans: withStyle(v.spans(b.Spans), richtext.Bold)})
	case canvas.Code:
		var sp []richtext.Span
		for i, l := range strings.Split(b.Code, "\n") {
			if i > 0 {
				sp = append(sp, richtext.Span{Kind: richtext.Break})
			}
			if l != "" {
				sp = append(sp, richtext.Span{Kind: richtext.Text, Text: l})
			}
		}
		return one(richtext.Block{Kind: richtext.Preformatted, Spans: sp})
	case canvas.Image:
		if b.Image == nil {
			return richtext.Doc{}, false
		}
		im := &richtext.Image{URL: b.Image.URL, Alt: b.Image.Alt, W: b.Image.W, H: b.Image.H, Title: b.Image.Alt}
		if f, ok := d.Files[b.FileID]; ok && f.Thumb != "" {
			im.URL, im.Proxy = f.Thumb, true
			if f.ThumbW > 0 {
				im.W, im.H = f.ThumbW, f.ThumbH
			}
			if im.Title == "" {
				im.Title = f.Title
			}
		} else if strings.HasPrefix(im.URL, "/") || slackPage(im.URL) {
			// Quip's relative blob URL, or a Slack page (a file's
			// permalink): never fetched, only the file's own thumbnail
			return richtext.Doc{}, false
		}
		if im.W == 0 {
			im.W, im.H = 640, 360
		}
		return one(richtext.Block{Kind: richtext.ImageBlock, Image: im})
	case canvas.File:
		f := &richtext.File{ID: b.FileID, Name: b.Title, Title: b.Title, PrettyType: nsCanvas.T("File")}
		if cf, ok := d.Files[b.FileID]; ok {
			f.Name, f.Title, f.Mime, f.PrettyType, f.Permalink = cf.Name, cf.Title, cf.Mime, cf.PrettyType, cf.Permalink
			f.IsImage = cf.IsImage
		}
		return one(richtext.Block{Kind: richtext.FileBlock, File: f})
	case canvas.Embed:
		u := b.URL
		host := u
		if pu, err := url.Parse(u); err == nil && pu.Host != "" {
			host = strings.TrimPrefix(pu.Host, "www.")
		}
		title := b.Title
		if title == "" {
			title = u
		}
		att := &richtext.Attachment{IsUnfurl: true, ServiceName: host, TitleLink: u, FromURL: u,
			Title: []richtext.Span{{Kind: richtext.Link, Text: title, URL: u}}}
		if u == "" {
			att.Title = []richtext.Span{{Kind: richtext.Text, Text: title}}
		}
		return one(richtext.Block{Kind: richtext.AttachmentBlock, Att: att})
	}
	return richtext.Doc{}, false
}

// slackPage: u is a page on a Slack workspace host (a permalink), not
// an image: fetching it would load Slack's web app with the session.
func slackPage(u string) bool {
	pu, err := url.Parse(u)
	if err != nil {
		return true
	}
	h := strings.ToLower(pu.Hostname())
	return h == "slack.com" || strings.HasSuffix(h, ".slack.com") && h != "files.slack.com" ||
		strings.HasSuffix(h, ".enterprise.slack.com")
}

// spans fills in what the export leaves out: emoji images from the
// workspace, channel ids from names (so channel mentions open).
func (v *View) spans(sp []richtext.Span) []richtext.Span {
	a := v.h.Core()
	if a == nil {
		return sp
	}
	out := make([]richtext.Span, len(sp))
	copy(out, sp)
	for i := range out {
		s := &out[i]
		switch s.Kind {
		case richtext.Emoji:
			if s.URL == "" {
				if u, t, ok := a.Emoji(s.ID); ok {
					s.URL = u
					if s.Text == "" {
						s.Text = t
					}
				}
			}
		case richtext.Date:
			s.Kind = richtext.Text // the export has the formatted date only
		}
	}
	return out
}

// margins are a block's section margins in Dp (canvas-controller CSS):
// paragraphs 4/4 (--paragraph-margin-*), headings 28/6, 24/2, 20/2
// (--heading-N-margin-*), a list section 8/8 (section[data-section-type=1]),
// a blockquote 4/12 (--blockquote-margin-bottom), and everything else
// .document-content .section's default 20/20.
func margins(k canvas.Kind) (top, bottom int) {
	switch k {
	case canvas.Paragraph:
		return 4, 4
	case canvas.Heading1:
		return 28, 6
	case canvas.Heading2:
		return 24, 2
	case canvas.Heading3:
		return 20, 2
	case canvas.Bullet, canvas.Numbered, canvas.Check:
		return 8, 8
	case canvas.Quote:
		return 4, 12
	}
	return 20, 20
}

// titleMargin is the title section's margin-bottom (--title-margin-bottom).
const titleMargin = 20

// gap is the space between prev and b, collapsed as CSS block margins
// collapse (the larger of prev's bottom and b's top). Items of one list
// are 6px apart (--list-item-bottom-offset); the first section has no top
// margin (body:not(.mobile) .document-content>.section[data-section-type=0]).
// prev nil: b is first; afterTitle: b follows the title section.
func gap(gtx layout.Context, prev, b *blk, afterTitle bool) int {
	if b == nil || b.b == nil {
		return 0
	}
	if prev != nil && prev.b == nil {
		prev = nil
	}
	top, _ := margins(b.b.Kind)
	switch {
	case prev == nil && afterTitle:
		return gtx.Dp(unit.Dp(max(top, titleMargin)))
	case prev == nil:
		return 0
	case prev.b.Kind.IsList() && prev.b.Kind == b.b.Kind:
		return gtx.Dp(itemGap)
	}
	_, pb := margins(prev.b.Kind)
	return gtx.Dp(unit.Dp(max(top, pb)))
}

// hidden: Slack draws an unedited default-content section transparent
// (.default-content:not(.customized-default-content)>.content{opacity:0});
// it keeps its line.
func hidden(b *blk) bool { return b.b.Kind == canvas.Paragraph && b.b.Default }

// layoutBlock draws b top pixels down in gtx.Constraints.Max.X and
// returns its height with the gap.
func (v *View) layoutBlock(gtx layout.Context, b *blk, top int) layout.Dimensions {
	w := gtx.Constraints.Max.X
	if v.edit.editing(b) {
		h := v.edit.layoutEditor(gtx, v, b, top, w)
		return layout.Dimensions{Size: image.Pt(w, h)}
	}
	defer op.Offset(image.Pt(0, top)).Push(gtx.Ops).Pop()
	th := v.h.Theme()
	c := &th.C
	h := 0
	switch k := b.b.Kind; {
	case k == canvas.Divider:
		// hr: 20px tall, its :before a 1px line at 50% in
		// rgba(var(--quipUiColorGray10), .2)
		ink := docText(th.Dark)
		ink.A = 0x33
		theme.Fill(gtx, image.Rect(0, gtx.Dp(9), w, gtx.Dp(10)), richview.Over(ink, c.Bg))
		h = gtx.Dp(20)
	case k.IsList():
		h = v.layoutItem(gtx, b, w)
	case k == canvas.Quote:
		h = v.layoutQuote(gtx, b, w)
	case k == canvas.Table:
		h = v.layoutTable(gtx, b, w)
	case k == canvas.Callout:
		pad := gtx.Dp(16)
		ex := pad
		if b.emoji != nil {
			ex += gtx.Dp(32)
		}
		m := op.Record(gtx.Ops)
		kh := v.layoutKids(gtx, b.kids, ex, w-ex-pad)
		if b.emoji != nil {
			func() {
				defer op.Offset(image.Pt(pad, 0)).Push(gtx.Ops).Pop() // the recording is drawn pad down
				g := gtx
				g.Constraints = layout.Constraints{Max: image.Pt(gtx.Dp(40), gtx.Dp(30))}
				b.emoji.Layout(g, v.styles.body, v.handler)
			}()
		}
		call := m.Stop()
		h = kh + 2*pad
		theme.RRect(gtx, image.Rect(0, 0, w, h), gtx.Dp(8), theme.WithAlpha(c.Accent, 0x1f))
		func() {
			defer op.Offset(image.Pt(0, pad)).Push(gtx.Ops).Pop()
			call.Add(gtx.Ops)
		}()
	case k == canvas.Columns:
		n := len(b.cols)
		gp := gtx.Dp(20)
		cw := (w - gp*(n-1)) / max(1, n)
		for i, col := range b.cols {
			hh := v.layoutKids(gtx, col, i*(cw+gp), cw)
			h = max(h, hh)
		}
	default:
		if b.rv == nil {
			return layout.Dimensions{Size: image.Pt(w, top)}
		}
		st := v.styles.forKind(k)
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(w, gtx.Constraints.Max.Y)}
		if k == canvas.Image {
			g.Constraints.Max.X = min(w, gtx.Dp(720))
		}
		if hidden(b) {
			m := op.Record(gtx.Ops) // measured, not drawn
			h = b.rv.Layout(g, st, v.handler).Size.Y
			m.Stop()
		} else {
			h = b.rv.Layout(g, st, v.handler).Size.Y
		}
		if v.edit.canEdit(v) && !b.inCell { // other kinds: a click explains they can't be edited yet
			v.edit.textArea(gtx, v, b, image.Rect(0, 0, w, h))
		}
	}
	return layout.Dimensions{Size: image.Pt(w, top+h)}
}

// layoutKids stacks child blocks at x within width w; returns their height.
func (v *View) layoutKids(gtx layout.Context, kids []*blk, x, w int) int {
	y := 0
	for i, k := range kids {
		func() {
			defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Min: image.Pt(w, 0), Max: image.Pt(w, gtx.Constraints.Max.Y)}
			var prev *blk
			if i > 0 {
				prev = kids[i-1]
			}
			d := v.layoutBlock(g, k, gap(gtx, prev, k, false))
			y += d.Size.Y
		}()
	}
	return y
}

// layoutItem draws a list item: its marker (bullet, number, checkbox) in
// the gutter at its level, then its text.
func (v *View) layoutItem(gtx layout.Context, b *blk, w int) int {
	x := gtx.Dp(indentDp) * b.b.Level
	lineH := gtx.Dp(bodyLine)
	tx := x + v.drawMarker(gtx, b, x, lineH)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(1, w-tx), gtx.Constraints.Max.Y)}
	h := lineH
	func() {
		defer op.Offset(image.Pt(tx, 0)).Push(gtx.Ops).Pop()
		if b.rv != nil {
			d := b.rv.Layout(g, v.styles.body, v.handler)
			h = max(h, d.Size.Y)
		}
		if v.edit.canEdit(v) && !b.inCell {
			v.edit.textArea(gtx, v, b, image.Rect(0, 0, w-tx, h))
		}
	}()
	return h
}

// layoutQuote draws a quote the way Quip does: each of its paragraphs is
// a section of its own (data-section-style 16, which the export groups
// into one <blockquote>), and each section draws its own bar
// (.section[data-section-style="16"]>.content:before: 4px wide, radius 3,
// 4px in from the section's top and bottom, in --dt_color-otl-pry;
// --dt_color-content-sec in dark) beside content padded 3px 0 3px 18px.
// Consecutive sections keep their collapsed margins (4px top, 12px
// bottom: 12px), so a two-paragraph quote shows two bars with a gap.
func (v *View) layoutQuote(gtx layout.Context, b *blk, w int) int {
	th := v.h.Theme()
	x := gtx.Dp(18)
	pad := gtx.Dp(3)
	y := 0
	for i, k := range b.kids {
		if i > 0 {
			y += gtx.Dp(12) // max(margin-bottom 12, margin-top 4)
		}
		top := y
		func() {
			defer op.Offset(image.Pt(x, y+pad)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Min: image.Pt(w-x, 0), Max: image.Pt(w-x, gtx.Constraints.Max.Y)}
			y += v.layoutBlock(g, k, 0).Size.Y + 2*pad
		}()
		if y-top > gtx.Dp(8) {
			theme.RRect(gtx, image.Rect(0, top+gtx.Dp(4), gtx.Dp(4), y-gtx.Dp(4)), gtx.Dp(3), quoteBar(th))
		}
	}
	return y
}

// quoteBar is the quote bar's colour: --dt_color-otl-pry, or
// --dt_color-content-sec in dark (.sk-client-theme--dark … :before).
func quoteBar(th *theme.Theme) color.NRGBA {
	if th.Dark {
		return th.C.FgMuted
	}
	return th.C.QuoteBar
}

// bullets are Quip's markers per level (section[data-section-style=5]
// li[data-indent=N] .pseudo-before:before: ● ○ ■ □, repeating).
var bullets = [...]string{"\u25cf", "\u25cb", "\u25a0", "\u25a1"}

// drawMarker draws a list item's bullet, number or checkbox at x and
// returns the marker container's width: the marker centred in a box at
// least 24px wide (numbers widen it), plus 4px padding-right. The text
// colour is the document's (.list-item-marker-container{color:
// var(--editor-document-text-color)}).
func (v *View) drawMarker(gtx layout.Context, b *blk, x, lineH int) int {
	th := v.h.Theme()
	c := &th.C
	ink := docText(th.Dark)
	box := gtx.Dp(24)
	switch b.b.Kind {
	case canvas.Bullet, canvas.Numbered:
		m := bullets[b.b.Level%len(bullets)]
		if b.b.Kind == canvas.Numbered {
			m = ordinal(b.b.Number, b.b.Level) + "."
		}
		mw := th.Measure(gtx, bodySize, theme.Regular, m)
		bw := max(box, mw)
		at(gtx, image.Pt(x+(bw-mw)/2, 0), image.Pt(mw+2, lineH), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, bodySize, theme.Regular, ink, m)
			})
		})
		return bw + gtx.Dp(4)
	case canvas.Check:
		// li.slackified-checkbox label: 14px, 5px in, radius 2, a 1px
		// #7c7a7f border (#bbbabb dark) on --dt_color-base-sec; checked:
		// --dt_color-base-inv-hgl-2 with a 2px check in --dt_color-base-pry.
		// Without slackified_checkbox (Quip's useSlackCheckboxes off) the
		// item has no label: the checkbox is the browser's own input.
		slack := v.exp("slackified_checkbox")
		r := v.checkRect(gtx, x)
		toggled := b.check.Clicked(gtx) // before Layout, which drains a Clickable's events
		at(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
			s := gtx.Constraints.Max
			editable := v.edit.canEdit(v) && !b.inCell // a table's checklist isn't edited in Clack
			if !slack {
				nativeCheckbox(gtx, th.Dark, b.b.Checked, editable && b.check.Hovered())
				if !editable {
					return layout.Dimensions{Size: s}
				}
				return b.check.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: s} })
			}
			if b.b.Checked {
				theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(2), c.Link)
				checkMark(gtx, c.Bg)
			} else {
				edge := color.NRGBA{R: 0x7c, G: 0x7a, B: 0x7f, A: 0xff}
				if th.Dark {
					edge = color.NRGBA{R: 0xbb, G: 0xba, B: 0xbb, A: 0xff}
				}
				theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(2), c.BgInset)
				theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(2), gtx.Dp(1), edge)
				if editable && b.check.Hovered() { // .checkbox:hover+label:after
					checkMark(gtx, c.BorderStrong)
				}
			}
			if !editable {
				return layout.Dimensions{Size: s}
			}
			return b.check.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: s} })
		})
		if toggled {
			v.edit.toggleCheck(v, b)
		}
		return box + gtx.Dp(4)
	}
	return 0
}

// checkMark draws the checkbox's tick (label:after: a 10x6 box's left and
// bottom 2px borders turned -45deg, 1px left and 2px down in the 14px box).
func checkMark(gtx layout.Context, col color.NRGBA) {
	k := float32(gtx.Dp(100)) / 100
	pt := func(x, y float32) f32.Point { return f32.Pt(x*k, y*k) }
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(pt(1.6, 6.1))
	p.LineTo(pt(4.9, 9.4))
	p.LineTo(pt(11.6, 2.9))
	paint.FillShape(gtx.Ops, col, clip.Stroke{Path: p.End(), Width: 2 * k}.Op())
}

// checkRect is the checkbox of an item whose marker container is at x:
// Slack's label (slackified_checkbox), else the browser's input.
func (v *View) checkRect(gtx layout.Context, x int) image.Rectangle {
	if !v.exp("slackified_checkbox") {
		return nativeCheckRect(gtx, x)
	}
	bs := gtx.Dp(14)
	return image.Rect(x+gtx.Dp(5), gtx.Dp(5), x+gtx.Dp(5)+bs, gtx.Dp(5)+bs)
}

// nativeCheckRect is where Quip's own checkbox sits without
// slackified_checkbox: the list's <input class="checkbox"> (display:
// block, min-width 24px, margin-top 2px, the UA's 4px margin-left) in the
// marker container at x, which Chromium paints as a 13px square centred
// in the input's box.
func nativeCheckRect(gtx layout.Context, x int) image.Rectangle {
	bs := gtx.Dp(13)
	cx := x + gtx.Dp(4) + gtx.Dp(12)
	return image.Rect(cx-bs/2, gtx.Dp(2), cx-bs/2+bs, gtx.Dp(2)+bs)
}

// nativeCheckbox paints Chromium's checkbox (the form-controls refresh,
// NativeThemeBase::PaintCheckbox) filling gtx.Constraints.Max: radius 2,
// a 1px #767676 border on white (hovered #4f4f4f); checked, the default
// accent #0075ff (hovered #005cc8) with a white tick. Quip's document
// sets color-scheme: dark under its dark theme, where Chromium uses
// #3b3b3b, a #858585 border (hovered #acacac) and an #99c8ff accent
// (hovered #d1e6ff) with a #3b3b3b tick.
func nativeCheckbox(gtx layout.Context, dark, checked, hovered bool) {
	s := gtx.Constraints.Max
	rgb := func(v uint32) color.NRGBA {
		return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
	}
	bg, edge, edgeHover := rgb(0xffffff), rgb(0x767676), rgb(0x4f4f4f)
	accent, accentHover, tick := rgb(0x0075ff), rgb(0x005cc8), rgb(0xffffff)
	if dark {
		bg, edge, edgeHover = rgb(0x3b3b3b), rgb(0x858585), rgb(0xacacac)
		accent, accentHover, tick = rgb(0x99c8ff), rgb(0xd1e6ff), rgb(0x3b3b3b)
	}
	r := image.Rectangle{Max: s}
	if checked {
		fill := accent
		if hovered {
			fill = accentHover
		}
		theme.RRect(gtx, r, gtx.Dp(2), fill)
		// the tick: from (.2, .5) down to (.4, .7), then up to (.8, .2) of
		// the box, stroked at .16 of the inset box's height
		w, h := float32(s.X), float32(s.Y)
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(w*0.2, h*0.5))
		p.LineTo(f32.Pt(w*0.4, h*0.7))
		p.LineTo(f32.Pt(w*0.8, h*0.2))
		paint.FillShape(gtx.Ops, tick, clip.Stroke{Path: p.End(), Width: max(1, h*0.6*0.16)}.Op())
		return
	}
	theme.RRect(gtx, r, gtx.Dp(2), bg)
	b := edge
	if hovered {
		b = edgeHover
	}
	theme.Border(gtx, r, gtx.Dp(2), gtx.Dp(1), b)
}

// ordinal numbers an item the way Quip does per level: 1. a. i.
func ordinal(n, level int) string {
	n = max(1, n)
	switch level % 3 {
	case 1:
		s := ""
		for n > 0 {
			n--
			s = string(rune('a'+n%26)) + s
			n /= 26
		}
		return s
	case 2:
		return roman(n)
	}
	return itoa(n)
}

func roman(n int) string {
	vals := []int{1000, 900, 500, 400, 100, 90, 50, 40, 10, 9, 5, 4, 1}
	syms := []string{"m", "cm", "d", "cd", "c", "xc", "l", "xl", "x", "ix", "v", "iv", "i"}
	var b strings.Builder
	for i, v := range vals {
		for n >= v {
			b.WriteString(syms[i])
			n -= v
		}
	}
	return b.String()
}

// ---- richview handler ----

type handler struct{ v *View }

func (r *handler) OpenURL(u string) {
	v := r.v
	if id := CanvasLinkID(u); id != "" && v.act.Canvas != nil {
		if sec := CanvasLinkSection(u); id == v.id && sec != "" {
			v.FocusSection(sec) // openCanvas on the open canvas: focusSection
			return
		}
		v.act.Canvas(id)
		return
	}
	if ch, ts, ok := archivesLink(u); ok {
		if ts != "" {
			v.h.Navigate("c/" + ch + "/t/" + ts)
		} else {
			v.h.Navigate("c/" + ch)
		}
		return
	}
	if v.act.OpenURL != nil {
		v.act.OpenURL(u)
	}
}

func (r *handler) OpenUser(id string, anchor image.Point) {
	if id != "" {
		r.v.h.OpenProfile(anchor, id)
	}
}

func (r *handler) OpenChannel(id string) {
	if id != "" {
		r.v.h.Navigate("c/" + id)
	}
}

func (r *handler) OpenUsergroup(id string, anchor image.Point) {}

func (r *handler) OpenImage(img richtext.Image) {
	r.v.h.OpenLightbox(img, core.Message{})
}

func (r *handler) OpenFile(f *richtext.File) {
	if f.Permalink != "" {
		r.OpenURL(f.Permalink)
	}
}

func (r *handler) Button(b richtext.Button) {
	if b.URL != "" {
		r.OpenURL(b.URL)
	}
}

// CanvasLinkID returns the canvas id of a canvas link
// (https://<team>.slack.com/docs/<T…>/<F…>, Slack's permalink for a
// canvas; also /canvas/<F…>), or "".
func CanvasLinkID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !(strings.HasSuffix(u.Host, ".slack.com") || u.Host == "slack.com") {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case len(parts) >= 3 && parts[0] == "docs" && isID(parts[2], 'F'):
		return parts[2]
	case len(parts) >= 2 && parts[0] == "canvas" && isID(parts[1], 'F'):
		return parts[1]
	}
	return ""
}

// CanvasLinkSection is a canvas link's focus_section_id (the section it
// points at), or "".
func CanvasLinkSection(raw string) string {
	if CanvasLinkID(raw) == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Query().Get("focus_section_id")
}

func isID(s string, first byte) bool {
	if len(s) < 3 || len(s) > 24 || s[0] != first {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// archivesLink recognizes message/channel links.
func archivesLink(raw string) (ch, ts string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(u.Host, ".slack.com") {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "archives" {
		return "", "", false
	}
	ch = parts[1]
	if len(parts) >= 3 && strings.HasPrefix(parts[2], "p") && len(parts[2]) > 7 {
		p := parts[2][1:]
		ts = p[:len(p)-6] + "." + p[len(p)-6:]
	}
	return ch, ts, true
}
