package canvas

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/some-du6e/clack/internal/richtext"
)

// MaxHTML bounds the export Parse accepts (Slack caps a canvas's markdown
// at 1 MiB; the HTML of a large one with tables is a few times that).
const MaxHTML = 16 << 20

// maxDepth bounds element nesting (untrusted input).
const maxDepth = 64

// ErrTooLarge is returned for exports over MaxHTML.
var ErrTooLarge = errors.New("canvas: export too large")

// Quip's section styles (data-section-style on the wrapper div), read from
// Hack Club canvases' exports (2026-11-08, structure only).
const (
	styleBullet   = "5"
	styleNumbered = "6"
	styleCheck    = "7"
	styleImage    = "11"
)

// PanicError is Parse's error for an export that made the parser panic
// (a bug): the panic and its stack, for the caller to log. The document's
// text is in neither.
type PanicError struct {
	Value any
	Stack []byte
}

func (e *PanicError) Error() string { return fmt.Sprintf("canvas: parser panic: %v", e.Value) }

// Parse reads Quip's HTML export of a canvas (url_private of a quip file).
// It never panics: a parser bug comes back as a *PanicError.
func Parse(src string) (d Doc, err error) {
	defer func() {
		if r := recover(); r != nil {
			d, err = Doc{}, &PanicError{Value: r, Stack: debug.Stack()}
		}
	}()
	if len(src) > MaxHTML {
		return Doc{}, ErrTooLarge
	}
	root, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return Doc{}, err
	}
	body := findClass(root, "quip-canvas-content", 0)
	if body == nil {
		body = findAtom(root, atom.Body, 0)
	}
	if body == nil {
		return Doc{}, nil
	}
	p := &parser{}
	return Doc{Blocks: p.blocks(body, 0)}, nil
}

type parser struct{}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, c string) bool {
	for _, f := range strings.Fields(attr(n, "class")) {
		if f == c {
			return true
		}
	}
	return false
}

func findClass(n *html.Node, c string, depth int) *html.Node {
	if depth > maxDepth {
		return nil
	}
	if n.Type == html.ElementNode && hasClass(n, c) {
		return n
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if f := findClass(ch, c, depth+1); f != nil {
			return f
		}
	}
	return nil
}

func findAtom(n *html.Node, a atom.Atom, depth int) *html.Node {
	if depth > maxDepth {
		return nil
	}
	if n.Type == html.ElementNode && n.DataAtom == a {
		return n
	}
	for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
		if f := findAtom(ch, a, depth+1); f != nil {
			return f
		}
	}
	return nil
}

var indentRE = regexp.MustCompile(`--indent\d*:\s*(\d+)`)

// blocks parses the block-level children of n.
func (p *parser) blocks(n *html.Node, depth int) []Block {
	if depth > maxDepth {
		return nil
	}
	var out []Block
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			if t := strings.TrimSpace(c.Data); t != "" { // stray text: its own paragraph
				out = append(out, Block{Kind: Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: collapse(t)}}})
			}
			continue
		}
		if c.Type != html.ElementNode {
			continue
		}
		out = append(out, p.block(c, depth+1)...)
	}
	return mergeCode(out)
}

func (p *parser) block(c *html.Node, depth int) []Block {
	if depth > maxDepth {
		return nil
	}
	id := attr(c, "id")
	switch c.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		k := Heading3
		switch c.DataAtom {
		case atom.H1:
			k = Heading1
		case atom.H2:
			k = Heading2
		}
		return []Block{{ID: id, Kind: k, Spans: p.inline(c, depth)}}
	case atom.P:
		switch {
		case hasClass(c, "prettyprint"):
			return []Block{{ID: id, Kind: Code, Code: rawText(c, depth)}}
		case hasClass(c, "embedded-link"):
			t := strings.TrimSpace(rawText(c, depth))
			u := firstLink(c, depth)
			if u == "" {
				u = t
			}
			return []Block{{ID: id, Kind: Embed, URL: safeURL(u), Title: t}}
		case hasClass(c, "embedded-file"):
			t := strings.TrimSpace(rawText(c, depth))
			return []Block{{ID: id, Kind: File, Title: t, FileID: fileIDIn(c, depth)}}
		}
		def := hasClass(c, "default-content") && !hasClass(c, "customized-default-content")
		return []Block{{ID: id, Kind: Paragraph, Spans: p.inline(c, depth), Default: def}}
	case atom.Hr:
		return []Block{{ID: id, Kind: Divider}}
	case atom.Blockquote:
		return []Block{{ID: id, Kind: Quote, Children: p.blocks(c, depth)}}
	case atom.Table:
		return []Block{p.table(c, depth)}
	case atom.Ul, atom.Ol:
		k := Bullet
		if c.DataAtom == atom.Ol {
			k = Numbered
		}
		return p.list(c, k, 0, depth)
	case atom.Li: // a stray item outside a list
		return p.list(&html.Node{Type: html.ElementNode, DataAtom: atom.Ul, FirstChild: c}, Bullet, 0, depth)
	case atom.Img:
		if im := p.img(c); im != nil {
			return []Block{{ID: id, Kind: Image, Image: im, FileID: im.FileID}}
		}
		return nil
	case atom.Pre:
		return []Block{{ID: id, Kind: Code, Code: rawText(c, depth)}}
	case atom.Br:
		return nil
	case atom.Div:
		switch attr(c, "data-section-style") {
		case styleBullet, styleNumbered, styleCheck:
			k := map[string]Kind{styleBullet: Bullet, styleNumbered: Numbered, styleCheck: Check}[attr(c, "data-section-style")]
			base := 0
			if m := indentRE.FindStringSubmatch(attr(c, "style")); m != nil {
				base, _ = strconv.Atoi(m[1])
				base = min(base, 8)
			}
			var out []Block
			for u := c.FirstChild; u != nil; u = u.NextSibling {
				if u.Type == html.ElementNode && (u.DataAtom == atom.Ul || u.DataAtom == atom.Ol) {
					out = append(out, p.list(u, k, base, depth+1)...)
				}
			}
			return out
		case styleImage:
			im := firstImg(c, depth)
			if im == nil || hasClass(im.Parent, "empty") || strings.HasPrefix(attr(im, "src"), "data:") {
				return nil // Quip's empty image placeholder
			}
			if b := p.img(im); b != nil {
				return []Block{{ID: firstNonEmpty(id, attr(im, "id")), Kind: Image, Image: b, FileID: b.FileID}}
			}
			return nil
		}
		switch {
		case hasClass(c, "flexbox"):
			var cols [][]Block
			for col := c.FirstChild; col != nil; col = col.NextSibling {
				if col.Type == html.ElementNode {
					cols = append(cols, p.blocks(col, depth))
				}
			}
			if len(cols) == 1 {
				return cols[0]
			}
			return []Block{{ID: id, Kind: Columns, Columns: cols}}
		case strings.Contains(attr(c, "class"), "callout"):
			return []Block{{ID: id, Kind: Callout, Emoji: attr(c, "data-emoji"), Children: p.blocks(c, depth)}}
		}
		return p.blocks(c, depth) // layout wrapper
	case atom.Span, atom.B, atom.I, atom.U, atom.Del, atom.S, atom.Strong, atom.Em, atom.Code, atom.A:
		// inline content at block level: a paragraph
		if sp := p.inline(&html.Node{Type: html.ElementNode, DataAtom: atom.P, FirstChild: c}, depth); len(sp) > 0 {
			return []Block{{ID: id, Kind: Paragraph, Spans: sp}}
		}
		return nil
	case atom.Script, atom.Style, atom.Head, atom.Title, atom.Meta, atom.Link:
		return nil
	}
	switch c.Data {
	case "control", "lnk":
		if sp := p.inlineNode(c, 0, "", depth); len(sp) > 0 {
			return []Block{{ID: id, Kind: Paragraph, Spans: trimBreaks(merge(sp))}}
		}
		return nil
	}
	return p.blocks(c, depth)
}

// mergeCode joins consecutive code lines (Quip exports one
// <p class="prettyprint line"> per line) into one Code block.
func mergeCode(bs []Block) []Block {
	out := bs[:0]
	for _, b := range bs {
		if b.Kind == Code && len(out) > 0 && out[len(out)-1].Kind == Code {
			out[len(out)-1].Code += "\n" + b.Code
			continue
		}
		out = append(out, b)
	}
	return out
}

// list flattens a <ul> into one block per item; nested lists go one level
// deeper. Numbered items count per level, restarting after a shallower item.
func (p *parser) list(u *html.Node, k Kind, level, depth int) []Block {
	if depth > maxDepth {
		return nil
	}
	var out []Block
	n := 0
	for c := u.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		switch c.DataAtom {
		case atom.Ul, atom.Ol:
			out = append(out, p.list(c, k, level+1, depth+1)...)
		case atom.Li:
			if v, err := strconv.Atoi(attr(c, "value")); err == nil && v > 0 {
				n = v - 1
			}
			n++
			b := Block{ID: attr(c, "id"), Kind: k, Level: level, Number: n, Checked: hasClass(c, "checked")}
			// the item's text is in a <span id>; nested lists inside the li follow it
			var nested []Block
			var kids []*html.Node
			for x := c.FirstChild; x != nil; x = x.NextSibling {
				kids = append(kids, x)
			}
			var inl []richtext.Span
			for _, x := range kids {
				if x.Type == html.ElementNode && (x.DataAtom == atom.Ul || x.DataAtom == atom.Ol) {
					nested = append(nested, p.list(x, k, level+1, depth+1)...)
					continue
				}
				if b.ID == "" && x.Type == html.ElementNode && x.DataAtom == atom.Span {
					b.ID = attr(x, "id")
				}
				inl = append(inl, p.inlineNode(x, 0, "", depth+1)...)
			}
			b.Spans = trimBreaks(merge(inl))
			out = append(out, b)
			out = append(out, nested...)
		default:
			out = append(out, p.block(c, depth+1)...)
		}
	}
	return out
}

func (p *parser) table(t *html.Node, depth int) Block {
	b := Block{ID: attr(t, "id"), Kind: Table, Header: attr(t, "data-header") == "true"}
	var rows func(n *html.Node, d int)
	rows = func(n *html.Node, d int) {
		if d > maxDepth {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode {
				continue
			}
			switch c.DataAtom {
			case atom.Tr:
				var row []Cell
				for td := c.FirstChild; td != nil; td = td.NextSibling {
					if td.Type == html.ElementNode && (td.DataAtom == atom.Td || td.DataAtom == atom.Th) {
						if td.DataAtom == atom.Th && len(b.Rows) == 0 {
							b.Header = true
						}
						row = append(row, Cell{Blocks: p.blocks(td, d+1)})
					}
				}
				b.Rows = append(b.Rows, row)
			case atom.Thead, atom.Tbody, atom.Tfoot:
				rows(c, d+1)
			}
		}
	}
	rows(t, depth)
	return b
}

var blobRE = regexp.MustCompile(`^/collab-slack-blob/[^/]+/(F[A-Z0-9]{4,24})`)

func (p *parser) img(n *html.Node) *Img {
	src := attr(n, "src")
	im := &Img{Alt: attr(n, "alt")}
	im.W, _ = strconv.Atoi(attr(n, "width"))
	im.H, _ = strconv.Atoi(attr(n, "height"))
	if m := blobRE.FindStringSubmatch(src); m != nil {
		im.FileID = m[1]
		im.URL = src
		return im
	}
	if u := safeURL(src); strings.HasPrefix(u, "https://") {
		im.URL = u
		return im
	}
	return nil
}

func firstImg(n *html.Node, depth int) *html.Node {
	return findAtom(n, atom.Img, depth)
}

func firstLink(n *html.Node, depth int) string {
	if depth > maxDepth {
		return ""
	}
	if n.Type == html.ElementNode && (n.Data == "lnk" || n.DataAtom == atom.A) {
		if h := attr(n, "href"); h != "" {
			return h
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if u := firstLink(c, depth+1); u != "" {
			return u
		}
	}
	return ""
}

var fileIDRE = regexp.MustCompile(`F[A-Z0-9]{6,24}`)

// fileIDIn finds a Slack file id on an embedded-file paragraph: a
// data-slack-file-id ("sf:F…") or a /files/… link.
func fileIDIn(n *html.Node, depth int) string {
	if depth > maxDepth {
		return ""
	}
	if n.Type == html.ElementNode {
		if v := attr(n, "data-slack-file-id"); v != "" {
			if m := fileIDRE.FindString(v); m != "" {
				return m
			}
		}
		if h := attr(n, "href"); strings.Contains(h, "/files/") {
			if m := fileIDRE.FindString(h); m != "" {
				return m
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := fileIDIn(c, depth+1); f != "" {
			return f
		}
	}
	return ""
}

// rawText is the text under n (code blocks: whitespace kept, <br> = "\n").
func rawText(n *html.Node, depth int) string {
	var b strings.Builder
	var rec func(n *html.Node, d int)
	rec = func(n *html.Node, d int) {
		if d > maxDepth {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch {
			case c.Type == html.TextNode:
				b.WriteString(c.Data)
			case c.Type == html.ElementNode && c.DataAtom == atom.Br:
				b.WriteByte('\n')
			case c.Type == html.ElementNode:
				rec(c, d+1)
			}
		}
	}
	rec(n, depth)
	return strings.TrimRight(b.String(), "\n")
}

// inline parses n's children as inline content.
func (p *parser) inline(n *html.Node, depth int) []richtext.Span {
	var out []richtext.Span
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		out = append(out, p.inlineNode(c, 0, "", depth+1)...)
	}
	return trimBreaks(merge(out))
}

func (p *parser) inlineNode(n *html.Node, st richtext.Style, link string, depth int) []richtext.Span {
	if depth > maxDepth {
		return nil
	}
	switch n.Type {
	case html.TextNode:
		t := collapse(n.Data)
		if t == "" {
			return nil
		}
		if link != "" {
			return []richtext.Span{{Kind: richtext.Link, Text: t, URL: link, Style: st}}
		}
		return []richtext.Span{{Kind: richtext.Text, Text: t, Style: st}}
	case html.ElementNode:
	default:
		return nil
	}
	switch n.DataAtom {
	case atom.Br:
		return []richtext.Span{{Kind: richtext.Break}}
	case atom.B, atom.Strong:
		st |= richtext.Bold
	case atom.I, atom.Em:
		st |= richtext.Italic
	case atom.Del, atom.S, atom.Strike:
		st |= richtext.Strike
	case atom.U:
		st |= richtext.Underline
	case atom.Code:
		st |= richtext.Code
	case atom.Img:
		if im := p.img(n); im != nil && im.FileID == "" {
			return []richtext.Span{{Kind: richtext.InlineImage, URL: im.URL, Text: im.Alt}}
		}
		return nil
	case atom.Span:
		if strings.Contains(attr(n, "style"), "background-color") || attr(n, "bgcolor") != "" {
			st |= richtext.Highlight
		}
	case atom.A:
		if h := attr(n, "href"); h != "" {
			if u := safeURL(h); u != "" {
				link = u
			}
		}
	case atom.Script, atom.Style:
		return nil
	}
	switch n.Data {
	case "lnk":
		if u := safeURL(attr(n, "href")); u != "" {
			link = u
		}
	case "control":
		return p.control(n, st, depth)
	}
	var out []richtext.Span
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		out = append(out, p.inlineNode(c, st, link, depth+1)...)
	}
	return out
}

// control is Quip's inline object: a mention (<a>@name</a>, <a>#channel</a>),
// an emoji (<img alt=name src=…>) or a date (plain text).
func (p *parser) control(n *html.Node, st richtext.Style, depth int) []richtext.Span {
	if im := findAtom(n, atom.Img, depth); im != nil {
		name := strings.Trim(attr(im, "alt"), ":")
		u := safeURL(attr(im, "src"))
		if !strings.HasPrefix(u, "https://") {
			u = ""
		}
		return []richtext.Span{{Kind: richtext.Emoji, ID: name, URL: u, Style: st}}
	}
	t := strings.TrimSpace(collapse(rawText(n, depth)))
	if t == "" {
		return nil
	}
	switch {
	case t == "@here" || t == "@channel" || t == "@everyone":
		return []richtext.Span{{Kind: richtext.Broadcast, Text: t, ID: t[1:], Self: true, Style: st}}
	case strings.HasPrefix(t, "@"):
		return []richtext.Span{{Kind: richtext.User, Text: t, Style: st}}
	case strings.HasPrefix(t, "#"):
		return []richtext.Span{{Kind: richtext.Channel, Text: t, Style: st}}
	}
	return []richtext.Span{{Kind: richtext.Date, Text: t, Style: st}}
}

// collapse folds HTML whitespace runs to one space (Quip's text has none
// it means; line breaks are <br>).
func collapse(s string) string {
	if !strings.ContainsAny(s, "\n\t\r") && !strings.Contains(s, "  ") {
		return s
	}
	var b strings.Builder
	sp := false
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			if !sp {
				b.WriteByte(' ')
			}
			sp = true
			continue
		}
		sp = false
		b.WriteRune(r)
	}
	return b.String()
}

// merge joins neighbouring spans of the same kind, style and target.
func merge(sp []richtext.Span) []richtext.Span {
	out := sp[:0]
	for _, s := range sp {
		if n := len(out); n > 0 && (s.Kind == richtext.Text || s.Kind == richtext.Link) {
			l := &out[n-1]
			if l.Kind == s.Kind && l.Style == s.Style && l.URL == s.URL {
				l.Text += s.Text
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

// trimBreaks drops Quip's trailing <br/> at the end of an item.
func trimBreaks(sp []richtext.Span) []richtext.Span {
	for len(sp) > 0 && sp[len(sp)-1].Kind == richtext.Break {
		sp = sp[:len(sp)-1]
	}
	return sp
}

// safeURL keeps http(s), mailto and slack links (at most 4 KiB, no
// userinfo); anything else is dropped (untrusted document).
func safeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 4096 {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "http":
		if u.Host == "" {
			return ""
		}
		return raw
	case "mailto", "slack":
		return raw
	}
	return ""
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}
