package canvasview

import (
	"image"
	"image/color"
	"net/url"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/canvas"
	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Canvas typography (Slack's canvas CSS: 15px body on a 24px line, bold
// headings at three sizes, a large title).
const (
	bodySize, bodyLine   = 15, 24
	h1Size, h1Line       = 26, 34
	h2Size, h2Line       = 21, 28
	h3Size, h3Line       = 17, 24
	titleSize, titleLine = 32, 40
	indentDp             = 26 // per list level
)

// styleSet is one richview.Style per text size, per window shaper.
type styleSet struct {
	shaper                      *text.Shaper
	dark                        bool
	gen                         uint64
	body, h1, h2, h3, title, sm *richview.Style
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
	mk := func(size, line float32) *richview.Style {
		st := richview.NewStyle(th.Shaper, imgs, richview.LightPalette())
		st.Face = theme.Typeface
		st.EmojiURL = th.EmojiURL
		st.TextSize, st.LineHeight = size, line
		st.Embolden = theme.BodyEmboldenFor(theme.LightPalette.Fg)
		if th.Dark {
			st.SetPalette(richview.DarkPalette())
			st.Embolden = theme.BodyEmboldenFor(theme.DarkPalette.Fg)
		}
		if p, ok := richview.Themed(th); ok { // stylesheet theme tokens
			st.SetPalette(p)
		}
		return st
	}
	s.body = mk(bodySize, bodyLine)
	s.h1 = mk(h1Size, h1Line)
	s.h2 = mk(h2Size, h2Line)
	s.h3 = mk(h3Size, h3Line)
	s.title = mk(titleSize, titleLine)
	s.sm = mk(13, 18)
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
}

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
		if o, ok := old[key]; ok && len(b.Children) == 0 && len(b.Columns) == 0 {
			o.b = b
			out = append(out, o)
			continue
		}
		x := &blk{b: b, key: key}
		switch b.Kind {
		case canvas.Quote, canvas.Callout:
			x.kids = v.makeBlocks(d, b.Children, old)
			if b.Kind == canvas.Callout && b.Emoji != "" {
				x.emoji = richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{v.emojiSpan(b.Emoji)}}}}, richview.Options{})
			}
		case canvas.Columns:
			for _, c := range b.Columns {
				x.cols = append(x.cols, v.makeBlocks(d, c, old))
			}
		default:
			if doc, ok := v.richDoc(d, b); ok {
				x.rv = richview.New(doc, richview.Options{Secondary: b.Kind == canvas.Check && b.Checked})
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
	case canvas.Table:
		var rows [][][]richtext.Span
		for _, r := range b.Rows {
			var cells [][]richtext.Span
			for _, c := range r {
				var sp []richtext.Span
				for j, cb := range c.Blocks {
					if j > 0 {
						sp = append(sp, richtext.Span{Kind: richtext.Break})
					}
					switch {
					case cb.Kind == canvas.Check && cb.Checked:
						sp = append(sp, richtext.Span{Kind: richtext.Text, Text: "☑ "})
					case cb.Kind == canvas.Check:
						sp = append(sp, richtext.Span{Kind: richtext.Text, Text: "☐ "})
					case cb.Kind == canvas.Bullet:
						sp = append(sp, richtext.Span{Kind: richtext.Text, Text: "• "})
					case cb.Kind == canvas.Numbered:
						sp = append(sp, richtext.Span{Kind: richtext.Text, Text: itoa(max(1, cb.Number)) + ". "})
					}
					st := richtext.Style(0)
					if cb.Kind == canvas.Heading1 || cb.Kind == canvas.Heading2 || cb.Kind == canvas.Heading3 {
						st = richtext.Bold
					}
					sp = append(sp, withStyle(v.spans(cb.Spans), st)...)
				}
				cells = append(cells, sp)
			}
			rows = append(rows, cells)
		}
		if !b.Header && len(rows) > 0 { // richview bolds row 0 as Block Kit does; Quip didn't
			rows = append([][][]richtext.Span{make([][]richtext.Span, len(rows[0]))}, rows...)
		}
		return one(richtext.Block{Kind: richtext.Table, Rows: rows})
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
		} else if strings.HasPrefix(im.URL, "/") {
			return richtext.Doc{}, false // Quip's relative blob URL: never fetched
		}
		if im.W == 0 {
			im.W, im.H = 640, 360
		}
		return one(richtext.Block{Kind: richtext.ImageBlock, Image: im})
	case canvas.File:
		f := &richtext.File{ID: b.FileID, Name: b.Title, Title: b.Title, PrettyType: "File"}
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

// space is the gap above a block (Slack's canvas section margins).
func space(gtx layout.Context, k canvas.Kind, first bool) int {
	if first {
		return 0
	}
	switch k {
	case canvas.Heading1:
		return gtx.Dp(22)
	case canvas.Heading2:
		return gtx.Dp(18)
	case canvas.Heading3:
		return gtx.Dp(14)
	case canvas.Bullet, canvas.Numbered, canvas.Check:
		return gtx.Dp(2)
	case canvas.Code, canvas.Table, canvas.Image, canvas.Quote, canvas.Callout, canvas.Columns, canvas.File, canvas.Embed:
		return gtx.Dp(10)
	case canvas.Divider:
		return gtx.Dp(12)
	}
	return gtx.Dp(4)
}

// layoutBlock draws b in gtx.Constraints.Max.X and returns its height.
func (v *View) layoutBlock(gtx layout.Context, b *blk, first bool) layout.Dimensions {
	w := gtx.Constraints.Max.X
	top := space(gtx, b.b.Kind, first)
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
		theme.Fill(gtx, image.Rect(0, gtx.Dp(8), w, gtx.Dp(9)), c.Border)
		h = gtx.Dp(17)
	case k.IsList():
		h = v.layoutItem(gtx, b, w)
	case k == canvas.Quote:
		x := gtx.Dp(16)
		h = v.layoutKids(gtx, b.kids, x, w-x)
		theme.RRect(gtx, image.Rect(0, 0, gtx.Dp(4), h), gtx.Dp(2), c.QuoteBar)
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
				defer op.Offset(image.Pt(pad, pad)).Push(gtx.Ops).Pop()
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
		gap := gtx.Dp(20)
		cw := (w - gap*(n-1)) / max(1, n)
		for i, col := range b.cols {
			hh := v.layoutKids(gtx, col, i*(cw+gap), cw)
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
		d := b.rv.Layout(g, st, v.handler)
		h = d.Size.Y
		if k.IsText() && v.edit.canEdit(v) {
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
			d := v.layoutBlock(g, k, i == 0)
			y += d.Size.Y
		}()
	}
	return y
}

// layoutItem draws a list item: its marker (bullet, number, checkbox) in
// the gutter at its level, then its text.
func (v *View) layoutItem(gtx layout.Context, b *blk, w int) int {
	th := v.h.Theme()
	c := &th.C
	x := gtx.Dp(indentDp) * b.b.Level
	gut := gtx.Dp(indentDp)
	tx := x + gut
	lineH := gtx.Dp(bodyLine)
	switch b.b.Kind {
	case canvas.Bullet:
		marks := []string{"•", "◦", "▪"}
		m := marks[b.b.Level%3]
		mw := th.Measure(gtx, bodySize, theme.Regular, m)
		at(gtx, image.Pt(x+(gut-mw)/2-gtx.Dp(3), 0), image.Pt(mw+2, lineH), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, bodySize, theme.Regular, c.Fg, m)
			})
		})
	case canvas.Numbered:
		m := ordinal(b.b.Number, b.b.Level) + "."
		mw := th.Measure(gtx, bodySize, theme.Regular, m)
		at(gtx, image.Pt(x+gut-mw-gtx.Dp(6), 0), image.Pt(mw+2, lineH), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lineH, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, bodySize, theme.Regular, c.FgMuted, m)
			})
		})
	case canvas.Check:
		bs := gtx.Dp(16)
		r := image.Rect(x+(gut-bs)/2-gtx.Dp(2), (lineH-bs)/2, x+(gut-bs)/2-gtx.Dp(2)+bs, (lineH-bs)/2+bs)
		at(gtx, r.Min, r.Size(), func(gtx layout.Context) layout.Dimensions {
			s := gtx.Constraints.Max
			editable := v.edit.canEdit(v)
			if b.b.Checked {
				theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(4), c.Accent)
				at(gtx, image.Pt(gtx.Dp(1), gtx.Dp(1)), image.Pt(s.X-gtx.Dp(2), s.Y-gtx.Dp(2)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get("check")(gtx, 14, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
				})
			} else {
				col := c.FgMuted
				if editable && b.check.Hovered() {
					col = c.Fg
				}
				theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(4), gtx.Dp(1)+gtx.Dp(1)/2, col)
			}
			if !editable {
				return layout.Dimensions{Size: s}
			}
			return b.check.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: s} })
		})
		if b.check.Clicked(gtx) {
			v.edit.toggleCheck(v, b)
		}
	}
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(max(1, w-tx), gtx.Constraints.Max.Y)}
	h := lineH
	func() {
		defer op.Offset(image.Pt(tx, 0)).Push(gtx.Ops).Pop()
		if b.rv != nil {
			d := b.rv.Layout(g, v.styles.body, v.handler)
			h = max(h, d.Size.Y)
		}
		if v.edit.canEdit(v) {
			v.edit.textArea(gtx, v, b, image.Rect(0, 0, w-tx, h))
		}
	}()
	return h
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
