package richtext

import (
	"reflect"
	"testing"
)

// lxEU's rules: case-insensitive, whole words, not inside @mentions or
// <…>, CJK words match anywhere, longer words first, overlaps merge.
func TestLocateHighlightWords(t *testing.T) {
	cases := []struct {
		text  string
		words []string
		rich  bool
		want  []Match
	}{
		{"Deploy is done", []string{"deploy"}, false, []Match{{0, 6}}},
		{"redeploy it", []string{"deploy"}, false, nil},
		{"deploy-bot", []string{"deploy"}, false, []Match{{0, 6}}},
		{"ping @deploy now", []string{"deploy"}, false, nil},
		{"see <https://x/deploy|link>", []string{"deploy"}, false, nil},
		{"日本語のテスト", []string{"テスト"}, false, []Match{{4, 3}}},
		{"it’s ok", []string{"it's"}, false, []Match{{0, 4}}},
		{"go golang", []string{"go", "golang"}, false, []Match{{0, 2}, {3, 6}}},
		{"x", []string{" ", ""}, false, nil},
	}
	for _, c := range cases {
		got := LocateHighlightWords(c.text, c.words, c.rich)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q %q: %v, want %v", c.text, c.words, got, c.want)
		}
	}
}

func TestHighlightDocSplitsSpans(t *testing.T) {
	bs := []Block{{Kind: Paragraph, Spans: []Span{
		{Kind: Text, Text: "the "}, {Kind: Text, Text: "Release train", Style: Bold}, {Kind: User, Text: "@release", ID: "U1"}, {Kind: Text, Text: " release"},
	}}}
	HighlightDoc(bs, []string{"release"})
	var got []string
	for _, sp := range bs[0].Spans {
		if sp.Style&Highlight != 0 {
			got = append(got, sp.Text)
		}
	}
	if !reflect.DeepEqual(got, []string{"Release", "release"}) {
		t.Fatalf("highlighted %q in %+v", got, bs[0].Spans)
	}
	if bs[0].Spans[1].Style&Bold == 0 || bs[0].Spans[2].Text != " train" || bs[0].Spans[2].Style&Bold == 0 {
		t.Fatalf("split lost styles: %+v", bs[0].Spans)
	}
}

// Find in thread marks every occurrence of the query in a message's rich
// text (substringMatch): inside words, inside @mention text and overlapping
// ones merged, case-insensitively, over a keyword highlight; the blocks
// handed in stay as they were.
func TestFindHighlighted(t *testing.T) {
	doc := Doc{Blocks: []Block{
		{Kind: Paragraph, Spans: []Span{{Kind: Text, Text: "Docs are ready, "}, {Kind: User, ID: "U1", Text: "@ada"}, {Kind: Text, Text: " READ it", Style: Highlight}}},
		{Kind: List, Items: [][]Span{{{Kind: Text, Text: "aaa"}}}},
		{Kind: Header, Spans: []Span{{Kind: Text, Text: "read me"}}},
	}}
	before := append([]Span(nil), doc.Blocks[0].Spans...)
	got := FindHighlighted(doc, []string{"rea"})
	var marked []string
	for _, sp := range got.Blocks[0].Spans {
		if sp.Style&FindHighlight != 0 {
			marked = append(marked, sp.Text)
		}
	}
	if want := []string{"rea", "REA"}; !reflect.DeepEqual(marked, want) {
		t.Errorf("marked runs %q, want %q (spans %+v)", marked, want, got.Blocks[0].Spans)
	}
	for _, sp := range got.Blocks[0].Spans {
		if sp.Text == "REA" && sp.Style&Highlight == 0 {
			t.Error("the keyword highlight under the match was dropped")
		}
	}
	if !reflect.DeepEqual(doc.Blocks[0].Spans, before) {
		t.Errorf("the original spans changed: %+v", doc.Blocks[0].Spans)
	}
	// overlapping occurrences ("aa" twice in "aaa") are one run
	ls := FindHighlighted(doc, []string{"aa"}).Blocks[1].Items[0]
	if len(ls) != 1 || ls[0].Text != "aaa" || ls[0].Style&FindHighlight == 0 {
		t.Errorf("list item %+v, want one marked run \"aaa\"", ls)
	}
	// a header isn't BaseRichText: no marks
	for _, sp := range got.Blocks[2].Spans {
		if sp.Style&FindHighlight != 0 {
			t.Errorf("header span %q marked", sp.Text)
		}
	}
	if d := FindHighlighted(doc, []string{" "}); &d.Blocks[0] != &doc.Blocks[0] {
		t.Error("a blank query copied the blocks")
	}
}
