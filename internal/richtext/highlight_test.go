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
