package richtext

import "testing"

// PreviewLinks: SimpleRichTextPreview's links are plain hyperlinks, a
// truncated one's text (or its URL) as written, in a paragraph, a list
// item and a nested block; the document given is not changed.
func TestPreviewLinksPlain(t *testing.T) {
	link := func(text string) Span {
		return Span{Kind: Link, URL: "https://example.com/doc", Text: text, Truncated: true}
	}
	d := Doc{Blocks: []Block{
		{Kind: Paragraph, Spans: []Span{{Kind: Text, Text: "see "}, link("example.com/doc")}},
		{Kind: List, Items: [][]Span{{{Kind: Text, Text: "one"}}, {link("")}}},
		{Kind: Paragraph, Children: []Block{{Kind: Paragraph, Spans: []Span{link("example.com/x")}}}},
	}}
	got := PreviewLinks(d)
	for _, sp := range [...]Span{got.Blocks[0].Spans[1], got.Blocks[1].Items[1][0], got.Blocks[2].Children[0].Spans[0]} {
		if sp.Truncated {
			t.Errorf("%+v still truncated", sp)
		}
	}
	if s := got.Blocks[1].Items[1][0].Text; s != "https://example.com/doc" {
		t.Errorf("a link without text reads %q, want its URL", s)
	}
	if !d.Blocks[0].Spans[1].Truncated || !d.Blocks[1].Items[1][0].Truncated || !d.Blocks[2].Children[0].Spans[0].Truncated {
		t.Error("PreviewLinks changed the document it was given")
	}
	plain := Doc{Blocks: []Block{{Kind: Paragraph, Spans: []Span{{Kind: Text, Text: "x"}}}}}
	if p := PreviewLinks(plain); &p.Blocks[0] != &plain.Blocks[0] {
		t.Error("a document without truncated links was copied")
	}
}
