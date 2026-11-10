// Package canvas is Clack's model of a Slack canvas: Quip's HTML export of
// the document (the canvas file's url_private, <div class=quip-canvas-content>)
// parsed into blocks, and the markdown the public canvases.edit method takes
// for each block. docs/CANVASES.md describes the protocol.
//
// Everything here is pure: no I/O, safe for concurrent use.
package canvas

import "github.com/some-du6e/clack/internal/richtext"

// Kind says what a block is.
type Kind uint8

const (
	Paragraph Kind = iota
	Heading1
	Heading2
	Heading3
	Bullet   // one list item; Level is the nesting depth
	Numbered // one numbered item; Number is its ordinal at its level
	Check    // one checklist item; Checked
	Quote    // blockquote: Children (paragraphs, lists)
	Code     // code block: Code holds the text, lines joined by "\n"
	Divider
	Table   // Rows of Cells; Header: the first row is a header row
	Image   // Image
	Embed   // an embedded link (unfurl card): URL, Title
	File    // an embedded Slack file: FileID, Title
	Columns // Quip's flexbox layout: Columns, each a list of blocks
	Callout // Slack's callout (a tinted panel with an emoji): Emoji, Children
)

// Block is one vertical unit of a canvas. ID is Quip's section id
// ("temp:C:…"), the section_id canvases.edit takes; blocks that aren't a
// section of their own (table cells, column wrappers) may have none.
type Block struct {
	ID       string
	Kind     Kind
	Spans    []richtext.Span // inline content (paragraphs, headings, list items)
	Level    int             // list nesting depth (0 = top)
	Number   int             // Numbered: 1-based ordinal at its level
	Checked  bool            // Check
	Code     string          // Code
	Rows     [][]Cell        // Table
	Header   bool            // Table: the first row is a header row
	Image    *Img            // Image
	URL      string          // Embed: the link
	FileID   string          // File, Image: the Slack file (F…)
	Title    string          // Embed / File: the text Quip shows
	Columns  [][]Block       // Columns
	Children []Block         // Quote, Callout
	Emoji    string          // Callout
	// Default: a Paragraph that is Quip's default content (class
	// default-content, e.g. a new canvas's "Time to document your
	// genius!") the user never edited. Slack draws such a section
	// transparent (.default-content:not(.customized-default-content)
	// >.content{opacity:0}) and shows the canvas's placeholder instead.
	Default bool
}

// Cell is one table cell; it holds blocks (usually one paragraph).
type Cell struct {
	Blocks []Block
}

// Img is an image in a canvas. URL is as exported: absolute, data:, or
// Quip's relative "/collab-slack-blob/<thread>/<F…>?size_name=…", which
// Clack never fetches (it resolves FileID through files.info instead).
type Img struct {
	URL, Alt string
	W, H     int
	FileID   string
}

// Doc is a whole canvas.
type Doc struct {
	Blocks []Block
}

// Text returns a block's plain text (spans concatenated; Break = "\n").
func (b *Block) Text() string {
	return SpansText(b.Spans)
}

// SpansText is the plain text of spans.
func SpansText(spans []richtext.Span) string {
	n := 0
	for _, s := range spans {
		n += len(s.Text) + 1
	}
	out := make([]byte, 0, n)
	for _, s := range spans {
		if s.Kind == richtext.Break {
			out = append(out, '\n')
			continue
		}
		if s.Kind == richtext.Emoji && s.Text == "" {
			out = append(out, ':')
			out = append(out, s.ID...)
			out = append(out, ':')
			continue
		}
		out = append(out, s.Text...)
	}
	return string(out)
}

// IsList reports whether k is a list item kind.
func (k Kind) IsList() bool { return k == Bullet || k == Numbered || k == Check }

// IsText reports whether a block of kind k is a run of inline text a user
// edits in place (paragraphs, headings, list items).
func (k Kind) IsText() bool {
	return k == Paragraph || k == Heading1 || k == Heading2 || k == Heading3 || k.IsList()
}

// Walk calls f for every block, depth first (quote/callout children,
// columns and table cells included). f returns false to skip children.
func (d *Doc) Walk(f func(b *Block) bool) {
	walk(d.Blocks, f)
}

func walk(bs []Block, f func(b *Block) bool) {
	for i := range bs {
		b := &bs[i]
		if !f(b) {
			continue
		}
		walk(b.Children, f)
		for _, c := range b.Columns {
			walk(c, f)
		}
		for _, r := range b.Rows {
			for _, c := range r {
				walk(c.Blocks, f)
			}
		}
	}
}

// Find returns the block with section id id (nil if none).
func (d *Doc) Find(id string) *Block {
	var out *Block
	d.Walk(func(b *Block) bool {
		if out == nil && b.ID == id {
			out = b
		}
		return out == nil
	})
	return out
}

// FileIDs returns the Slack files the canvas embeds (images, file cards),
// in document order without duplicates.
func (d *Doc) FileIDs() []string {
	seen := map[string]bool{}
	var out []string
	d.Walk(func(b *Block) bool {
		if b.FileID != "" && !seen[b.FileID] {
			seen[b.FileID] = true
			out = append(out, b.FileID)
		}
		return true
	})
	return out
}
