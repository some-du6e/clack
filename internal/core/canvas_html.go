package core

// Parser for the HTML rendering Slack serves for a canvas (url_private).
// Structure (shapes recorded from real canvases, 2026-11):
//
//	<div class="quip-canvas-content">
//	  <h1 id=S>title</h1>                      first h1: the canvas title
//	  <h1|h2|h3 id=S>…</h1>
//	  <p id=S class="line">…</p>                paragraph
//	  <p id=S class="prettyprint line">a<br>b</p> code block
//	  <p class="embedded-link">url</p>        link card
//	  <div data-section-style="5|6|7" class="list-numbering-restart-at">
//	    <ul id=S><li id=S value=1><span id=S>…</span><br></li><ul>…nested</ul></ul>
//	  </div>                                    5 bullets, 6 numbered, 7 checklist
//	  <blockquote><p id=S class="line">…</p>…</blockquote>
//	  <table><tr><td><p id=S class="line">…</p></td>…</tr></table>
//	  <hr>, <img src>
//	</div>
//
// Inline: text, <b>/<strong>, <i>/<em>, <s>/<del>, <u>, <code>, <a href>,
// <br>, and <control id=C> wrapping an emoji <img alt=name> or a link /
// mention <a>. Emoji images go through the slack-imgs.com proxy.

import (
	"bytes"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// canvasResolver is what the parser needs to name mentions.
type canvasResolver interface {
	UserName(id string) string
	ChannelName(id string) string
	Emoji(name string) (url, text string, ok bool)
	SelfID() string
}

func parseCanvasHTML(r canvasResolver, body []byte) Canvas {
	doc, err := html.Parse(bytes.NewReader(body))
	c := Canvas{Controls: map[string]string{}}
	if err != nil {
		return c
	}
	root := findClass(doc, "quip-canvas-content")
	if root == nil {
		root = findAtom(doc, atom.Body)
	}
	if root == nil {
		return c
	}
	p := &canvasParser{r: r, c: &c}
	p.blocks(root)
	return c
}

type canvasParser struct {
	r       canvasResolver
	c       *Canvas
	titled  bool
	numbers map[int]int // numbered lists: next ordinal per indent
}

func (p *canvasParser) add(b CanvasBlock) {
	if b.Kind != CanvasNumbered {
		p.numbers = nil
	}
	p.c.Blocks = append(p.c.Blocks, b)
}

// blocks walks block-level children.
func (p *canvasParser) blocks(n *html.Node) {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		p.block(ch)
	}
}

func (p *canvasParser) block(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		if t := strings.TrimSpace(n.Data); t != "" {
			p.add(CanvasBlock{Kind: CanvasParagraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: t}}})
		}
		return
	case html.ElementNode:
	default:
		return
	}
	id := attr(n, "id")
	cls := attr(n, "class")
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		spans := p.inline(n)
		if n.DataAtom == atom.H1 && !p.titled && len(p.c.Blocks) == 0 {
			p.titled = true
			p.c.Title, p.c.TitleID = strings.TrimSpace(richtext.SpansText(spans)), id
			return
		}
		k := CanvasH1
		switch n.DataAtom {
		case atom.H2:
			k = CanvasH2
		case atom.H3, atom.H4, atom.H5, atom.H6:
			k = CanvasH3
		}
		p.add(CanvasBlock{Kind: k, ID: id, Spans: spans})
	case atom.P, atom.Pre:
		p.para(n, id, cls)
	case atom.Hr:
		p.add(CanvasBlock{Kind: CanvasDivider, ID: id})
	case atom.Blockquote:
		found := false
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type == html.ElementNode && (ch.DataAtom == atom.P || ch.DataAtom == atom.Div) {
				found = true
				p.add(CanvasBlock{Kind: CanvasQuote, ID: attr(ch, "id"), Spans: p.inline(ch)})
			}
		}
		if !found {
			p.add(CanvasBlock{Kind: CanvasQuote, ID: id, Spans: p.inline(n)})
		}
	case atom.Ul, atom.Ol:
		k := CanvasBullet
		if n.DataAtom == atom.Ol {
			k = CanvasNumbered
		}
		if strings.Contains(cls, "checklist") {
			k = CanvasCheck
		}
		p.list(n, k, 0)
	case atom.Table:
		p.table(n, id)
	case atom.Img:
		if img := p.image(n); img != nil {
			p.add(CanvasBlock{Kind: CanvasImage, ID: id, Image: img, URL: img.URL})
		}
	case atom.Div, atom.Section, atom.Article, atom.Span:
		if st := attr(n, "data-section-style"); st != "" {
			k := CanvasBullet
			switch st {
			case "6":
				k = CanvasNumbered
			case "7":
				k = CanvasCheck
			}
			p.numbers = nil
			for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
				if ch.Type == html.ElementNode && (ch.DataAtom == atom.Ul || ch.DataAtom == atom.Ol) {
					p.list(ch, k, 0)
				}
			}
			return
		}
		if strings.Contains(cls, "callout") {
			p.add(CanvasBlock{Kind: CanvasCallout, ID: id, Spans: p.inline(n)})
			return
		}
		if hasBlockChild(n) {
			p.blocks(n)
			return
		}
		if spans := p.inline(n); len(spans) > 0 {
			p.add(CanvasBlock{Kind: CanvasParagraph, ID: id, Spans: spans})
		}
	case atom.A:
		if href := attr(n, "href"); href != "" {
			p.add(CanvasBlock{Kind: CanvasEmbed, ID: id, URL: href, Spans: p.inline(n)})
		}
	default:
		if hasBlockChild(n) {
			p.blocks(n)
		} else if spans := p.inline(n); len(spans) > 0 {
			p.add(CanvasBlock{Kind: CanvasParagraph, ID: id, Spans: spans})
		}
	}
}

func (p *canvasParser) para(n *html.Node, id, cls string) {
	switch {
	case n.DataAtom == atom.Pre || hasWord(cls, "prettyprint"):
		p.add(CanvasBlock{Kind: CanvasCode, ID: id, Spans: plainSpans(p.inline(n))})
		return
	case hasWord(cls, "embedded-link"):
		u := attr(n, "data-url")
		if a := findAtom(n, atom.A); a != nil && u == "" {
			u = attr(a, "href")
		}
		if u == "" {
			u = strings.TrimSpace(textOf(n))
		}
		if richtext.SafeURL(u) != "" {
			p.add(CanvasBlock{Kind: CanvasEmbed, ID: id, URL: u, Spans: p.inline(n)})
			return
		}
	case strings.Contains(cls, "callout"):
		p.add(CanvasBlock{Kind: CanvasCallout, ID: id, Spans: p.inline(n)})
		return
	case hasWord(cls, "embedded-file") || attr(n, "data-file-id") != "":
		if f := p.fileOf(n); f != nil {
			p.add(CanvasBlock{Kind: CanvasFile, ID: id, File: f, URL: f.Permalink})
			return
		}
	}
	// a paragraph that is only an image is an image block
	if img := soleImage(n); img != nil {
		if im := p.image(img); im != nil {
			p.add(CanvasBlock{Kind: CanvasImage, ID: id, Image: im, URL: im.URL})
			return
		}
	}
	p.add(CanvasBlock{Kind: CanvasParagraph, ID: id, Spans: p.inline(n)})
}

// list walks a <ul>/<ol>; nested lists are one level deeper.
func (p *canvasParser) list(n *html.Node, k CanvasBlockKind, indent int) {
	if p.numbers == nil {
		p.numbers = map[int]int{}
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type != html.ElementNode {
			continue
		}
		switch ch.DataAtom {
		case atom.Li:
			b := CanvasBlock{Kind: k, ID: attr(ch, "id"), Indent: indent}
			cls := attr(ch, "class")
			b.Checked = hasWord(cls, "checked") || attr(ch, "data-checked") == "true"
			if k == CanvasNumbered {
				if v, err := strconv.Atoi(attr(ch, "value")); err == nil && v > 0 {
					p.numbers[indent] = v
				}
				if p.numbers[indent] == 0 {
					p.numbers[indent] = 1
				}
				b.Number = p.numbers[indent]
				p.numbers[indent]++
				for d := range p.numbers {
					if d > indent {
						delete(p.numbers, d)
					}
				}
			}
			// the item's own content, without nested lists
			b.Spans = p.inlineSkip(ch, func(c *html.Node) bool {
				return c.Type == html.ElementNode && (c.DataAtom == atom.Ul || c.DataAtom == atom.Ol)
			})
			b.Spans = trimTrailingBreaks(b.Spans)
			p.c.Blocks = append(p.c.Blocks, b)
			for gc := ch.FirstChild; gc != nil; gc = gc.NextSibling {
				if gc.Type == html.ElementNode && (gc.DataAtom == atom.Ul || gc.DataAtom == atom.Ol) {
					p.list(gc, k, indent+1)
				}
			}
		case atom.Ul, atom.Ol:
			p.list(ch, k, indent+1)
		}
	}
}

func (p *canvasParser) table(n *html.Node, id string) {
	var rows [][]CanvasCell
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			if ch.Type != html.ElementNode {
				continue
			}
			switch ch.DataAtom {
			case atom.Tr:
				var row []CanvasCell
				for td := ch.FirstChild; td != nil; td = td.NextSibling {
					if td.Type != html.ElementNode || (td.DataAtom != atom.Td && td.DataAtom != atom.Th) {
						continue
					}
					cell := CanvasCell{ID: attr(td, "id"), Header: td.DataAtom == atom.Th}
					if pp := firstElement(td, atom.P); pp != nil && cell.ID == "" {
						cell.ID = attr(pp, "id")
					}
					cell.Spans = trimTrailingBreaks(p.inline(td))
					row = append(row, cell)
				}
				rows = append(rows, row)
			case atom.Thead, atom.Tbody, atom.Tfoot:
				walk(ch)
			}
		}
	}
	walk(n)
	p.add(CanvasBlock{Kind: CanvasTable, ID: id, Rows: rows})
}

// ---- inline content ----

func (p *canvasParser) inline(n *html.Node) []richtext.Span {
	return p.inlineSkip(n, nil)
}

func (p *canvasParser) inlineSkip(n *html.Node, skip func(*html.Node) bool) []richtext.Span {
	var out []richtext.Span
	var walk func(n *html.Node, st richtext.Style, link string)
	emit := func(s richtext.Span) {
		// merge adjacent plain text with the same style and link
		if l := len(out) - 1; l >= 0 && s.Kind == out[l].Kind && (s.Kind == richtext.Text || s.Kind == richtext.Link) &&
			s.Style == out[l].Style && s.URL == out[l].URL {
			out[l].Text += s.Text
			return
		}
		out = append(out, s)
	}
	text := func(t string, st richtext.Style, link string) {
		if t == "" {
			return
		}
		t = strings.ReplaceAll(t, " ", " ")
		lines := strings.Split(t, "\n")
		for i, ln := range lines {
			if i > 0 {
				emit(richtext.Span{Kind: richtext.Break})
			}
			if ln == "" {
				continue
			}
			if link != "" {
				emit(richtext.Span{Kind: richtext.Link, Text: ln, URL: link, Style: st})
			} else {
				emit(richtext.Span{Kind: richtext.Text, Text: ln, Style: st})
			}
		}
	}
	walk = func(n *html.Node, st richtext.Style, link string) {
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			if skip != nil && skip(ch) {
				continue
			}
			switch ch.Type {
			case html.TextNode:
				text(ch.Data, st, link)
				continue
			case html.ElementNode:
			default:
				continue
			}
			switch ch.DataAtom {
			case atom.Br:
				emit(richtext.Span{Kind: richtext.Break})
			case atom.B, atom.Strong:
				walk(ch, st|richtext.Bold, link)
			case atom.I, atom.Em:
				walk(ch, st|richtext.Italic, link)
			case atom.S, atom.Del, atom.Strike:
				walk(ch, st|richtext.Strike, link)
			case atom.U:
				walk(ch, st|richtext.Underline, link)
			case atom.Code:
				walk(ch, st|richtext.Code, link)
			case atom.Mark:
				walk(ch, st|richtext.Highlight, link)
			case atom.A:
				href := attr(ch, "href")
				if sp, ok := p.mention(href, textOf(ch)); ok {
					emit(sp)
					p.control(ch, richtext.SpansText([]richtext.Span{sp}))
					continue
				}
				if richtext.SafeURL(href) == "" {
					href = link
				}
				walk(ch, st, href)
			case atom.Img:
				if sp, ok := p.emojiSpan(ch); ok {
					emit(sp)
				}
			default:
				if ch.Data == "control" {
					before := len(out)
					walk(ch, st, link)
					var disp string
					for _, s := range out[before:] {
						if s.Kind == richtext.Emoji {
							disp += ":" + s.ID + ":"
						} else {
							disp += s.Text
						}
					}
					if id := attr(ch, "id"); id != "" {
						p.c.Controls[id] = disp
					}
					continue
				}
				walk(ch, st, link)
			}
		}
	}
	walk(n, 0, "")
	return out
}

// control records the display text of a control element (or its parent
// control) for the editor.
func (p *canvasParser) control(n *html.Node, disp string) {
	for c := n; c != nil; c = c.Parent {
		if c.Type == html.ElementNode && c.Data == "control" {
			if id := attr(c, "id"); id != "" {
				p.c.Controls[id] = disp
			}
			return
		}
	}
}

var (
	reTeamUser   = regexp.MustCompile(`/team/([UW][A-Z0-9]{2,})`)
	reArchiveCh  = regexp.MustCompile(`/archives/([CGD][A-Z0-9]{2,})`)
	reMentionTxt = regexp.MustCompile(`^@`)
)

// mention turns a /team/U… or /archives/C… link into a mention span.
func (p *canvasParser) mention(href, txt string) (richtext.Span, bool) {
	if href == "" {
		return richtext.Span{}, false
	}
	if m := reTeamUser.FindStringSubmatch(href); m != nil {
		name := strings.TrimSpace(txt)
		if n := p.r.UserName(m[1]); n != "" && n != m[1] {
			name = n
		}
		name = reMentionTxt.ReplaceAllString(name, "")
		return richtext.Span{Kind: richtext.User, ID: m[1], Text: "@" + name, Self: m[1] == p.r.SelfID()}, true
	}
	if m := reArchiveCh.FindStringSubmatch(href); m != nil && !strings.Contains(href, "/p1") {
		name := strings.TrimPrefix(strings.TrimSpace(txt), "#")
		if n := p.r.ChannelName(m[1]); n != "" && n != m[1] {
			name = n
		}
		return richtext.Span{Kind: richtext.Channel, ID: m[1], Text: "#" + name}, true
	}
	return richtext.Span{}, false
}

// emojiSpan turns an inline <img> into an emoji span (Slack emoji are
// marked data-is-slack and named by alt).
func (p *canvasParser) emojiSpan(n *html.Node) (richtext.Span, bool) {
	src := unproxy(attr(n, "src"))
	name := strings.Trim(attr(n, "alt"), ":")
	if name == "" {
		name = attr(n, "data-stringify-emoji")
	}
	if name == "" && src == "" {
		return richtext.Span{}, false
	}
	if u, txt, ok := p.r.Emoji(name); ok {
		if u == "" && txt != "" {
			return richtext.Span{Kind: richtext.Emoji, ID: name, Text: txt}, true
		}
		if u != "" {
			src = u
		}
	}
	if richtext.SafeImage(src) == "" {
		src = ""
	}
	return richtext.Span{Kind: richtext.Emoji, ID: name, URL: src, Text: ":" + name + ":"}, true
}

func (p *canvasParser) image(n *html.Node) *richtext.Image {
	src := unproxy(attr(n, "src"))
	if richtext.SafeImage(src) == "" {
		return nil
	}
	w, _ := strconv.Atoi(attr(n, "width"))
	h, _ := strconv.Atoi(attr(n, "height"))
	return &richtext.Image{URL: src, Alt: attr(n, "alt"), W: w, H: h, Proxy: richtext.NeedsProxy(src)}
}

func (p *canvasParser) fileOf(n *html.Node) *richtext.File {
	f := &richtext.File{ID: attr(n, "data-file-id")}
	if a := findAtom(n, atom.A); a != nil {
		f.Permalink = attr(a, "href")
		f.Title = strings.TrimSpace(textOf(a))
	}
	if f.Title == "" {
		f.Title = strings.TrimSpace(textOf(n))
	}
	if img := findAtom(n, atom.Img); img != nil {
		f.Thumb = p.image(img)
		f.IsImage = f.Thumb != nil
	}
	f.Name = f.Title
	if f.Title == "" && f.Permalink == "" {
		return nil
	}
	return f
}

// unproxy unwraps slack-imgs.com/?url=… image proxies.
func unproxy(src string) string {
	u, err := url.Parse(src)
	if err != nil {
		return src
	}
	if u.Hostname() == "slack-imgs.com" {
		if inner := u.Query().Get("url"); inner != "" {
			return inner
		}
	}
	return src
}

// ---- html helpers ----

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasWord(s, w string) bool {
	for _, f := range strings.Fields(s) {
		if f == w {
			return true
		}
	}
	return false
}

func findClass(n *html.Node, cls string) *html.Node {
	if n.Type == html.ElementNode && hasWord(attr(n, "class"), cls) {
		return n
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if r := findClass(ch, cls); r != nil {
			return r
		}
	}
	return nil
}

func findAtom(n *html.Node, a atom.Atom) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == a {
		return n
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if r := findAtom(ch, a); r != nil {
			return r
		}
	}
	return nil
}

func firstElement(n *html.Node, a atom.Atom) *html.Node {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && ch.DataAtom == a {
			return ch
		}
	}
	return nil
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(n)
	return b.String()
}

var blockAtoms = map[atom.Atom]bool{atom.P: true, atom.Div: true, atom.H1: true, atom.H2: true, atom.H3: true,
	atom.Ul: true, atom.Ol: true, atom.Table: true, atom.Blockquote: true, atom.Hr: true, atom.Pre: true, atom.Section: true}

func hasBlockChild(n *html.Node) bool {
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if ch.Type == html.ElementNode && blockAtoms[ch.DataAtom] {
			return true
		}
	}
	return false
}

// soleImage returns the only meaningful child of n when it is an <img>
// (possibly wrapped in a link or span) that isn't an emoji.
func soleImage(n *html.Node) *html.Node {
	var img *html.Node
	count := 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			switch ch.Type {
			case html.TextNode:
				if strings.TrimSpace(ch.Data) != "" {
					count += 2
				}
			case html.ElementNode:
				if ch.DataAtom == atom.Img {
					if attr(ch, "data-is-slack") != "" || ch.Parent.Data == "control" {
						count += 2
						continue
					}
					img = ch
					count++
					continue
				}
				if ch.DataAtom != atom.Br {
					walk(ch)
				}
			}
		}
	}
	walk(n)
	if count == 1 {
		return img
	}
	return nil
}

func trimTrailingBreaks(s []richtext.Span) []richtext.Span {
	for len(s) > 0 && s[len(s)-1].Kind == richtext.Break {
		s = s[:len(s)-1]
	}
	return s
}

// plainSpans drops inline styles (code blocks are monospace already).
func plainSpans(s []richtext.Span) []richtext.Span {
	out := s[:0:0]
	for _, sp := range s {
		sp.Style &^= richtext.Code
		out = append(out, sp)
	}
	return trimTrailingBreaks(out)
}
