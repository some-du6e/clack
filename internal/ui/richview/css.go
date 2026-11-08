package richview

// Plugin and user CSS on the parts of a message body richview draws itself
// (docs/PLUGINS-components.md §5.2, "Native parts"). In Slack's DOM a
// message body is
//
//	div.p-rich_text_block
//	  div.p-rich_text_section   text, b, i, s, code.c-mrkdwn__code, a.c-link, span.c-emoji…
//	  ul.p-rich_text_list > li
//	  blockquote.c-mrkdwn__quote
//	  pre.c-mrkdwn__pre
//
// and attachments are div.c-message_attachment with __title, __text,
// __footer… parts. Every text run, inline box and list marker the layout
// makes is tagged with its part: the path of Slack elements from the
// View's parent element (slackc.CurrentScope) down to it. A part's style
// (color, background, opacity, text-decoration, font-weight, font-style)
// is the cascade for an element at that path (slackc's Styled.Child), so
// selectors, specificity, !important and inheritance work as in Slack,
// and Slack's own rules (slackc's base sheet: link and code colors…) keep
// a part native where they'd win in the browser.
//
// With no CSS installed nothing here runs: parts are 0 and paint is
// unchanged. With CSS, parts are resolved at layout (fonts) and applied
// when the static ops are recorded (colors, decorations); a change of the
// parent element (the row hovered, a class a plugin added) re-resolves
// them, and lays out again only if a font changed.

import (
	"image"
	"image/color"

	"gioui.org/font"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc/css"
)

// Slack's elements inside a message body (live client classes and
// data-stringify-type attributes, docs/PLUGINS-props-live.md).
var (
	listBulletClass  = slackc.Class("p-rich_text_list p-rich_text_list__bullet", "data-stringify-type", "unordered-list").Tag("ul")
	listOrderedClass = slackc.Class("p-rich_text_list p-rich_text_list__ordered", "data-stringify-type", "ordered-list").Tag("ol")
	listItemClass    = slackc.Class("").Tag("li")
	quoteClass       = slackc.Class("c-mrkdwn__quote", "data-stringify-type", "quote").Tag("blockquote")
	preClass         = slackc.Class("c-mrkdwn__pre", "data-stringify-type", "pre").Tag("pre")
	headerClass      = slackc.Class("p-header_block")
	sectionBlkClass  = slackc.Class("p-section_block")
	sectionTextClass = slackc.Class("p-section_block__text")
	contextBlkClass  = slackc.Class("p-context_block")
	mrkdwnElemClass  = slackc.Class("p-mrkdwn_element").Tag("span")
	tombstoneClass   = slackc.Class("c-message__body c-message__body--tombstone")
	editedClass      = slackc.Class("c-message__edited_label").Tag("span")

	boldClass      = slackc.Class("", "data-stringify-type", "bold").Tag("b")
	italicClass    = slackc.Class("", "data-stringify-type", "italic").Tag("i")
	strikeClass    = slackc.Class("", "data-stringify-type", "strike").Tag("s")
	underlineClass = slackc.Class("").Tag("u")
	codeClass      = slackc.Class("c-mrkdwn__code", "data-stringify-type", "code").Tag("code")
	linkClass      = slackc.Class("c-link", "data-stringify-link", "true", "target", "_blank", "rel", "noopener noreferrer").Tag("a")
	usergroupClass = slackc.Class("c-mrkdwn__subteam c-mrkdwn__subteam--link", "data-stringify-type", "mention").Tag("span")
	broadcastClass = slackc.Class("c-mrkdwn__broadcast c-mrkdwn__broadcast--link c-mrkdwn__broadcast--mention", "data-stringify-type", "mention").Tag("span")
	emojiClass     = slackc.Class("c-emoji c-emoji__medium c-emoji--inline", "data-qa", "emoji").Tag("span")
	highlightClass = slackc.Class("c-search__highlight").Tag("span")

	attTextClass    = slackc.Class("c-message_attachment__text")
	attPretextClass = slackc.Class("c-message_attachment__pretext")
	attTitleClass   = slackc.Class("c-message_attachment__title", "data-qa", "message_attachment_title").Tag("span")
	attAuthorClass  = slackc.Class("c-message_attachment__author_name").Tag("span")
	attServiceClass = slackc.Class("c-message_attachment__service_name").Tag("span")
	attFieldsClass  = slackc.Class("c-message_attachment__fields")
	attFooterClass  = slackc.Class("c-message_attachment__footer").Tag("span")
	fileMetaClass   = slackc.Class("c-message_kit__file c-file_container")
)

// bodyPath is the container of text outside any block.
var bodyPath = partPath{}.with(richBlockClass)

// partPath is a part's elements under the View's parent, outermost first.
type partPath struct {
	n    uint8
	defs [12]*slackc.ClassDef
}

func (p partPath) with(d *slackc.ClassDef) partPath {
	if int(p.n) < len(p.defs) {
		p.defs[p.n] = d
		p.n++
	}
	return p
}

// partStyle is what CSS gives a part (the zero value: nothing).
type partStyle struct {
	col     color.NRGBA
	hasCol  bool
	bg      color.NRGBA
	hasBg   bool
	alpha   float32 // product of the opacities richview applies itself (1 = none)
	deco    css.TextDecoration
	decoCol color.NRGBA // zero: the text's color
	weight  int16       // CSS font-weight, 0 = native
	italic  int8        // 1 italic, -1 normal, 0 native
}

type part struct {
	path partPath
	ps   partStyle
}

// wrapped are the elements View.Layout draws through Styled.Wrap around
// the whole body: their opacity and background aren't the parts' to apply.
func wrapped(d *slackc.ClassDef) bool { return d == richBlockClass || d == richSectionClass }

func resolvePart(sc slackc.Scope, p *partPath) partStyle {
	ps := partStyle{alpha: 1}
	var s slackc.Styled
	for i := 0; i < int(p.n); i++ {
		d := p.defs[i]
		if i == 0 {
			s = sc.Child(d, 0)
		} else {
			s = s.Child(d, 0)
		}
		if !wrapped(d) {
			ps.alpha *= s.Opacity()
		}
	}
	if s.Has("color") {
		ps.col, ps.hasCol = s.Color(color.NRGBA{}), true
	}
	if n := p.n; n > 0 && !wrapped(p.defs[n-1]) && s.Has("background-color") {
		ps.bg, ps.hasBg = s.Background(color.NRGBA{}), true
	}
	ps.deco, ps.decoCol = s.Deco()
	if s.Has("font-weight") {
		ps.weight = int16(s.FontWeight(400))
	}
	if s.Has("font-style") {
		ps.italic = -1
		if s.Italic(false) {
			ps.italic = 1
		}
	}
	return ps
}

// cssParts is a View's CSS state: the scope its parts were resolved in
// and the parts its layout made (index+1 is a frag's/box's part).
type cssParts struct {
	scope slackc.Scope
	parts []part
}

// sync re-resolves the parts when the View's parent element changed.
// relayout: a font changed; record: a color or decoration did.
func (c *cssParts) sync(sc slackc.Scope) (relayout, record bool) {
	if sc.Same(c.scope) {
		return false, false
	}
	was := c.scope.Active()
	c.scope = sc
	if was != sc.Active() {
		c.parts = c.parts[:0]
		return true, true
	}
	for i := range c.parts {
		pt := &c.parts[i]
		ps := resolvePart(sc, &pt.path)
		if ps == pt.ps {
			continue
		}
		if ps.weight != pt.ps.weight || ps.italic != pt.ps.italic {
			relayout = true
		}
		pt.ps = ps
		record = true
	}
	return relayout, record
}

// get returns part i (1-based), nil for 0.
func (c *cssParts) get(i uint8) *partStyle {
	if i == 0 || int(i) > len(c.parts) {
		return nil
	}
	return &c.parts[i-1].ps
}

// ---- layout side ----

// cssOn reports that this layout tags parts.
func (l *lay) cssOn() bool { return l.css != nil }

// enter makes d the current container (a block, a list item, an
// attachment's text); restore with leave.
func (l *lay) enter(d *slackc.ClassDef) partPath {
	old := l.ctr
	if l.css != nil {
		l.ctr = l.ctr.with(d)
		l.curPart = l.partFor(&l.ctr)
	}
	return old
}

func (l *lay) leave(old partPath) {
	if l.css != nil {
		l.ctr = old
		l.curPart = l.partFor(&l.ctr)
	}
}

// partFor finds or adds the part at path p.
func (l *lay) partFor(p *partPath) uint8 {
	if l.css == nil {
		return 0
	}
	if p.n == 0 {
		p = &bodyPath // text outside any block (an image's title, a gallery's names)
	}
	ps := l.css.parts
	for i := range ps {
		if ps[i].path == *p {
			return uint8(i + 1)
		}
	}
	if len(ps) >= 250 {
		return 0
	}
	l.css.parts = append(ps, part{path: *p, ps: resolvePart(l.css.scope, p)})
	return uint8(len(l.css.parts))
}

// spanPart is the part of a span in the current container: Slack's
// inline element chain (link, then b, i, s, u, code).
func (l *lay) spanPart(sp *richtext.Span) uint8 {
	if l.css == nil {
		return 0
	}
	p := l.ctr
	switch sp.Kind {
	case richtext.Link:
		p = p.with(linkClass)
	case richtext.Date:
		if sp.URL != "" {
			p = p.with(linkClass)
		}
	case richtext.User:
		p = p.with(memberClass)
	case richtext.Channel:
		p = p.with(channelClass)
	case richtext.Usergroup:
		p = p.with(usergroupClass)
	case richtext.Broadcast:
		p = p.with(broadcastClass)
	case richtext.Emoji, richtext.InlineImage:
		p = p.with(emojiClass)
	}
	st := sp.Style
	if st&richtext.Bold != 0 {
		p = p.with(boldClass)
	}
	if st&richtext.Italic != 0 {
		p = p.with(italicClass)
	}
	if st&richtext.Strike != 0 {
		p = p.with(strikeClass)
	}
	if st&richtext.Underline != 0 {
		p = p.with(underlineClass)
	}
	if st&richtext.Code != 0 {
		p = p.with(codeClass)
	}
	if st&richtext.Highlight != 0 {
		p = p.with(highlightClass)
	}
	return l.partFor(&p)
}

// partFace applies a part's font-weight and font-style to k.
func (l *lay) partFace(k faceKey, pi uint8) faceKey {
	ps := l.css.get(pi)
	if ps == nil {
		return k
	}
	if ps.weight > 0 {
		k.f.Weight = font.Weight(ps.weight - 400)
	}
	switch ps.italic {
	case 1:
		k.f.Style = font.Italic
	case -1:
		k.f.Style = font.Regular
	}
	return k
}

// ---- paint side ----

// mulAlpha scales c's alpha by a (0..1).
func mulAlpha(c color.NRGBA, a float32) color.NRGBA {
	if a >= 1 {
		return c
	}
	c.A = uint8(float32(c.A)*max(a, 0) + 0.5)
	return c
}

// textCol is frag f's color under its part.
func (v *View) textCol(f *frag) color.NRGBA {
	ps := v.css.get(f.part)
	if ps == nil {
		return f.col
	}
	col := f.col
	if ps.hasCol {
		col = ps.col
	}
	return mulAlpha(col, ps.alpha)
}

// boxFill is box b's fill under its part.
func (v *View) boxFill(b *box) color.NRGBA {
	ps := v.css.get(b.part)
	if ps == nil {
		return b.fill
	}
	fill := b.fill
	switch {
	case b.colFill && ps.hasCol: // list markers: currentColor
		fill = ps.col
	case !b.colFill && ps.hasBg && b.comp != 0:
		fill = color.NRGBA{} // a pill: its component's Wrap paints the CSS background
	case !b.colFill && ps.hasBg:
		fill = ps.bg
	}
	return mulAlpha(fill, ps.alpha)
}

// partBackgrounds fills the CSS background of inline parts that have no
// box of their own (a b or a link a rule gave a background): per line,
// from the first to the last frag of a run of the same part.
func (v *View) partBackgrounds(paint func(r image.Rectangle, c color.NRGBA), comp int16) {
	fs := v.d.frags
	for i := 0; i < len(fs); {
		f := &fs[i]
		ps := v.css.get(f.part)
		if ps == nil || !ps.hasBg || f.comp != comp || f.boxed {
			i++
			continue
		}
		j := i + 1
		x1 := f.x + f.w
		for j < len(fs) && fs[j].part == f.part && fs[j].line == f.line && fs[j].comp == comp {
			x1 = fs[j].x + fs[j].w
			j++
		}
		paint(image.Rect(int(f.x), f.top, int(x1+0.999), f.bot), mulAlpha(ps.bg, ps.alpha))
		i = j
	}
}

// Run is one painted word of a View: its text and how it's painted.
type Run struct {
	Text  string
	Color color.NRGBA        // after CSS (a component's own color for pills)
	Deco  css.TextDecoration // CSS text-decoration lines on it
	Bold  bool               // drawn in a bold (≥600) or heavier face
	Comp  bool               // part of a component (pill, card) drawn per frame
}

// Runs calls fn for every word the last Layout laid out, in order, with
// the color it's painted in (tests, accessibility). Pills are reported in
// their part's color, which is the one their component paints.
func (v *View) Runs(fn func(Run)) {
	for i := range v.d.frags {
		f := &v.d.frags[i]
		if f.kind != fText || f.sh == nil {
			continue
		}
		r := Run{Text: f.sh.str, Color: f.col, Comp: f.comp != 0}
		if ps := v.css.get(f.part); ps != nil {
			r.Color = v.textCol(f)
			r.Deco = ps.deco
		}
		r.Bold = f.sh != nil && f.weight >= font.SemiBold
		fn(r)
	}
}
