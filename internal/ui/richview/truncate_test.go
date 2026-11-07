package richview

import (
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

func para(s string) richtext.Block {
	return richtext.Block{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: s}}}
}

func TestTruncateBlocksLines(t *testing.T) {
	// 7 lines as separate blocks: keeps 5 (cut at the 5th line break)
	var bs []richtext.Block
	for i := 0; i < 7; i++ {
		bs = append(bs, para("line"))
	}
	out, cut := truncateBlocks(bs, attMaxChars, attMaxNewlines)
	if !cut || len(out) != 5 {
		t.Fatalf("lines: cut=%v kept=%d", cut, len(out))
	}
	// a list's items count as lines too
	list := richtext.Block{Kind: richtext.List, Items: [][]richtext.Span{{{Kind: richtext.Text, Text: "a"}}, {{Kind: richtext.Text, Text: "b"}}}}
	out, cut = truncateBlocks([]richtext.Block{para("1"), para("2"), para("3"), list, para("6")}, attMaxChars, attMaxNewlines)
	if !cut || len(out) != 4 || len(out[3].Items) != 2 {
		t.Fatalf("list: cut=%v %+v", cut, out)
	}
	// short text is left alone
	if _, cut := truncateBlocks([]richtext.Block{para("a"), para("b")}, attMaxChars, attMaxNewlines); cut {
		t.Error("short cut")
	}
}

func TestTruncateBlocksChars(t *testing.T) {
	long := strings.Repeat("x", 400)
	out, cut := truncateBlocks([]richtext.Block{para(long)}, attMaxChars, attMaxNewlines)
	if !cut || len(out) != 1 || out[0].Spans[0].Text != strings.Repeat("x", 300)+"…" {
		t.Fatalf("chars: cut=%v %q", cut, out[0].Spans[0].Text)
	}
	// shared messages allow 700
	if _, cut := truncateBlocks([]richtext.Block{para(long)}, shareMaxChars, shareMaxNewlines); cut {
		t.Error("share limit")
	}
}

func TestWithExpander(t *testing.T) {
	bs := withExpander([]richtext.Block{para("text")}, true)
	sp := bs[0].Spans
	if len(bs) != 1 || len(sp) != 3 || sp[2].Text != "Show more" || sp[2].URL != expandURL {
		t.Fatalf("more: %+v", bs)
	}
	bs = withExpander([]richtext.Block{para("text")}, false)
	if len(bs) != 2 || bs[1].Spans[0].Text != "Show less" {
		t.Fatalf("less: %+v", bs)
	}
}
