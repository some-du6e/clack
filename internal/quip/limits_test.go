package quip

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// A fragment deeper than Clack's HTML parser goes (512 open elements) is
// a parse Slack's doesn't make: ParseRTML says so and keeps the text.
// It read such a section as empty (the review of B1), and a save made
// from that would have blanked it.
func TestQuipParseRTMLTooDeep(t *testing.T) {
	deep := strings.Repeat("<span>", 600) + "kept" + strings.Repeat("</span>", 600)
	if text, marks, ok := ParseRTML(deep); ok || text != deep || marks != nil {
		t.Errorf("600 nested tags: %d bytes of text, marks %v, ok %v; want the RTML itself, no marks, not ok", len(text), marks, ok)
	}
	// the control: 400 deep is in the parser's reach
	shallow := strings.Repeat("<span>", 400) + "kept" + strings.Repeat("</span>", 400)
	if text, _, ok := ParseRTML(shallow); !ok || text != "kept" {
		t.Errorf("400 nested tags: %q, ok %v", text, ok)
	}
}

// walkRTML is linear in the tree: a crafted section (240 marks deep, 240
// looked-through tags inside them, then many lines) cost each node a
// search of the open marks for every ancestor, and each mark its
// subtree's text again (the review of B1: minutes at a few MB). The deep
// walk takes about what a flat one over the same lines does.
func TestQuipWalkRTMLLinear(t *testing.T) {
	lines := strings.Repeat("x<br>", 20000)
	walk := func(rtml string) time.Duration {
		t.Helper()
		root := rtmlRoot(rtml)
		if root == nil {
			t.Fatal("the fragment didn't parse")
		}
		best := time.Duration(math.MaxInt64)
		for i := 0; i < 3; i++ {
			c := &idCollector{}
			start := time.Now()
			if !walkRTML(root, c) {
				t.Fatal("the walk stopped")
			}
			best = min(best, time.Since(start))
		}
		return best
	}
	flat := walk("<b><span>" + lines)
	deep := walk(strings.Repeat("<b>", 240) + strings.Repeat("<span>", 240) + lines)
	if deep > 10*flat+50*time.Millisecond {
		t.Errorf("240 marks and 240 tags deep: %v, flat: %v", deep, flat)
	}
}

// An answer of empty records can't decode to gigabytes: each is a whole
// struct (an empty Section, 416 bytes, from two), so repeated fields draw
// on a budget of 24 times the answer's size (the review of B1: 16 MiB of
// empty sections came to about 3.5 GB).
func TestQuipUnmarshalBudget(t *testing.T) {
	empty := bytes.Repeat([]byte{0x3a, 0x00}, 1<<19) // Payload.Sections: 512Ki empty sections in 1 MiB
	var p Payload
	if err := Unmarshal(empty, &p); !errors.Is(err, ErrMalformed) {
		t.Errorf("1 MiB of empty sections: %v, %d sections", err, len(p.Sections))
	}
	// the control: as many sections as real ones are big (an id and a
	// sequence, smaller than any section Slack sends) fit
	sec := Marshal(&Section{ID: Ptr("temp:C:ABC1234567890abcdef0123456"), Sequence: Ptr(int64(12345))})
	if len(sec) >= 128 {
		t.Fatalf("a %d-byte section needs a longer length", len(sec))
	}
	var many []byte
	for len(many) < 1<<20 {
		many = append(many, 0x3a, byte(len(sec)))
		many = append(many, sec...)
	}
	var q Payload
	if err := Unmarshal(many, &q); err != nil || len(q.Sections) < 20000 {
		t.Errorf("1 MiB of small sections: %v, %d sections", err, len(q.Sections))
	}
}
