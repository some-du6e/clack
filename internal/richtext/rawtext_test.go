package richtext

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unsafe"

	"github.com/some-du6e/clack/internal/slack"
)

// refAppendRichText is appendRichText as it was before raw spans
// (2026-11-19, 0a17a3f6), the reference eachRichText must cut texts as.
func refAppendRichText(out []Span, s string, lead, first bool) []Span {
	text := func(t string) {
		if t != "" {
			out = append(out, Span{Kind: Text, Text: t})
		}
	}
	brk := func(k BrKind) { out = append(out, Span{Kind: Break, Br: k}) }
	if first {
		ws := len(s) - len(strings.TrimLeftFunc(s, unicode.IsSpace))
		for _, ln := range strings.SplitAfter(s[:ws], "\n") {
			if t, ok := strings.CutSuffix(ln, "\n"); ok {
				text(t)
				brk(BrNone)
			} else {
				text(ln)
			}
		}
		s, lead = s[ws:], false
	}
	virt := lead && strings.HasPrefix(s, "\n")
	if virt {
		s = "\n" + s
	}
	for s != "" {
		loc := richRunRe.FindStringIndex(s)
		if loc == nil {
			loc = []int{len(s), len(s)}
		}
		for k, ln := range strings.Split(s[:loc[0]], "\n") {
			if k > 0 {
				brk(BrLine)
			}
			text(ln)
		}
		if loc[0] == loc[1] {
			break
		}
		run := s[loc[0]:loc[1]]
		k := strings.Count(run, "\n")
		for j, ln := range strings.Split(run, "\n") {
			switch {
			case j == 0:
			case virt && j == 1:
			case !virt && j == k:
				brk(BrNone)
			default:
				brk(BrPara)
			}
			text(ln)
		}
		s, virt = s[loc[1]:], false
	}
	return out
}

// rawTexts are texts made of what BaseText's rules turn on: words,
// spaces, tabs, no-break spaces and newlines, in every order.
func rawTexts(n int) []string {
	parts := []string{"ab", "c", " ", "\t", " ", "\n", "\n", "\n\n", " \n", "\n ", "é"}
	x := uint64(7)
	next := func() uint64 { x ^= x << 13; x ^= x >> 7; x ^= x << 17; return x }
	out := []string{"", "\n", "\n\n", " \n \n", "a\n", "\na", "\n\na", "a\n\n", " a", "\n  \n \nb"}
	for len(out) < n {
		var b strings.Builder
		for k := int(next() % 14); k >= 0; k-- {
			b.WriteString(parts[next()%uint64(len(parts))])
		}
		out = append(out, b.String())
	}
	return out
}

// richLeadRunRe is the run a leading newline makes with the "\n" in front
// of it: what richRunRe matches at the start of "\n"+s, less that "\n".
var richLeadRunRe = regexp.MustCompile(`^(?:\n[ \t\x{00A0}]*)*\n`)

// TestRichTextPieces: eachRichText cuts every text as the old
// appendRichText did, and a raw span stands for exactly those spans;
// nextRun and runEnd find what richRunRe does.
func TestRichTextPieces(t *testing.T) {
	for _, s := range rawTexts(4000) {
		want := richRunRe.FindStringIndex(s)
		if got := nextRun(s); !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: nextRun %v, richRunRe %v", s, got, want)
		}
		if strings.HasPrefix(s, "\n") {
			if got, want := runEnd(s, 0), richLeadRunRe.FindStringIndex(s)[1]; got != want {
				t.Fatalf("%q: runEnd %d, want %d", s, got, want)
			}
		}
		for _, f := range [][2]bool{{false, false}, {true, false}, {true, true}} {
			want := refAppendRichText(nil, s, f[0], f[1])
			var got []Span
			eachRichText(s, f[0], f[1], func(x string) bool {
				if x != "" {
					got = append(got, Span{Kind: Text, Text: x})
				}
				return true
			}, func(k BrKind) bool { got = append(got, Span{Kind: Break, Br: k}); return true })
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%q lead %v first %v: %+v, want %+v", s, f[0], f[1], got, want)
			}
			raw := Span{Kind: Text, Text: s, Br: rawKind(f[0], f[1]), Style: Bold}
			for i := range want {
				if want[i].Kind != Break {
					want[i].Style = Bold
				}
			}
			if got := Expand([]Span{raw}); !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
				t.Fatalf("%q raw %v: %+v, want %+v", s, raw.Br, got, want)
			}
		}
	}
}

// giantText is n one-word lines (the giant-wall fixture's shape), with a
// paragraph run every para lines when para > 0.
func giantText(n, para int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte('\n')
			if para > 0 && i%para == 0 {
				b.WriteByte('\n')
			}
		}
		fmt.Fprintf(&b, "w%dx%d", i, i%7)
	}
	return b.String()
}

func giantMessage(t *testing.T, text, field string, pre bool) *slack.Message {
	t.Helper()
	el, _ := json.Marshal(text)
	sec := "rich_text_section"
	if pre {
		sec = "rich_text_preformatted"
	}
	blocks := `[{"type":"rich_text","block_id":"g1","elements":[{"type":"` + sec + `","elements":[{"type":"text","text":` + string(el) + `}]}]}]`
	return &slack.Message{TS: "1.0", User: "U1", Text: field, Blocks: json.RawMessage(blocks)}
}

// expandedDoc is FromMessage with every element cut into spans (RawLines
// out of reach): the Doc a raw span stands for.
func expandedDoc(m *slack.Message) Doc {
	old := RawLines
	RawLines = 1 << 30
	defer func() { RawLines = old }()
	return FromMessage(R, m)
}

func expandDoc(d Doc) Doc {
	bs := append([]Block(nil), d.Blocks...)
	for i := range bs {
		bs[i].Spans = Expand(bs[i].Spans)
	}
	d.Blocks = bs
	return d
}

// TestRawSpanMessage: a text element of RawLines newlines or more is one
// span holding the message's own text (one copy of it), and it stands for
// the spans the element is cut into: the same plain text, the same
// keyword and find marks.
func TestRawSpanMessage(t *testing.T) {
	for _, c := range []struct {
		name string
		text string
		pre  bool
	}{
		{"lines", giantText(3000, 0), false},
		{"paragraphs", giantText(3000, 9), false},
		{"leading newlines", "\n\n " + giantText(1000, 0), false},
		{"pre", giantText(2000, 50), true},
	} {
		text := strings.Clone(c.text)
		m := giantMessage(t, c.text, text, c.pre)
		d := FromMessage(R, m)
		if len(d.Blocks) != 1 || len(d.Blocks[0].Spans) != 1 || !IsRaw(&d.Blocks[0].Spans[0]) {
			t.Fatalf("%s: %d blocks, spans %d: want one raw span", c.name, len(d.Blocks), len(d.Blocks[0].Spans))
		}
		if sp := &d.Blocks[0].Spans[0]; unsafe.StringData(sp.Text) != unsafe.StringData(m.Text) {
			t.Errorf("%s: the raw span's text is a second copy of the message's", c.name)
		}
		want := expandedDoc(m)
		if got := expandDoc(d); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: the raw span stands for %d spans, the element is %d", c.name, len(got.Blocks[0].Spans), len(want.Blocks[0].Spans))
		}
		if got, w := PlainText(d), PlainText(want); got != w {
			t.Errorf("%s: plain text %d bytes, want %d", c.name, len(got), len(w))
		}
		for _, words := range [][]string{{"w12x5"}, {"x3"}, {" "}, {"w2999x3"}} {
			hd := FromMessage(R, m)
			HighlightDoc(hd.Blocks, words)
			hw := expandedDoc(m)
			HighlightDoc(hw.Blocks, words)
			if got := expandDoc(hd); !reflect.DeepEqual(got, hw) {
				t.Errorf("%s: keywords %q mark other runs than the element's spans get", c.name, words)
			}
			if got, w := expandDoc(FindHighlighted(d, words)), FindHighlighted(want, words); !reflect.DeepEqual(got, w) {
				t.Errorf("%s: find %q marks other runs than the element's spans get", c.name, words)
			}
		}
	}
	// under the threshold nothing changes: RawLines lines are a span a
	// line and a break between; one more is one raw span
	if d := FromMessage(R, giantMessage(t, giantText(RawLines, 0), "", false)); len(d.Blocks[0].Spans) != 2*RawLines-1 {
		t.Errorf("%d lines: %d spans, want a span a line and a break between", RawLines, len(d.Blocks[0].Spans))
	}
	if d := FromMessage(R, giantMessage(t, giantText(RawLines+1, 0), "", false)); len(d.Blocks[0].Spans) != 1 || !IsRaw(&d.Blocks[0].Spans[0]) {
		t.Errorf("%d lines: %d spans, want one raw span", RawLines+1, len(d.Blocks[0].Spans))
	}
}
