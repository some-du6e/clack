package richview

import (
	"strings"
	"testing"
	"time"
)

// listBlocks is one ordered rich_text_list at indent and offset (JSON
// numbers as a sender wrote them), its items "x" and "y".
func listBlocks(indent, offset string) string {
	item := func(s string) string {
		return `{"type":"rich_text_section","elements":[{"type":"text","text":"` + s + `"}]}`
	}
	return `[{"type":"rich_text","elements":[{"type":"rich_text_list","style":"ordered","indent":` + indent +
		`,"offset":` + offset + `,"elements":[` + item("x") + `,` + item("y") + `]}]}]`
}

// listMarkers lays doc out at 600px and returns its list markers, the
// "N." texts drawn before the items (nil if the layout outlasts d).
func listMarkers(t *testing.T, blocks string, d time.Duration) []string {
	t.Helper()
	doc := kitDoc(t, blocks)
	done := make(chan []string, 1)
	go func() {
		v := layoutAt(newTestStyle(), doc, Options{}, 600)
		var out []string
		for i := range v.d.frags {
			if f := &v.d.frags[i]; f.kind == fText && f.sh != nil && f.p0 < 0 && strings.HasSuffix(f.sh.str, ".") {
				out = append(out, f.sh.str)
			}
		}
		done <- out
	}()
	select {
	case m := <-done:
		return m
	case <-time.After(d):
		t.Errorf("laying out the list took more than %v", d)
		return nil
	}
}

// TestListHugeOffsetDraws (the review of bkc-128): an ordered list at
// indent 2 draws lower-roman markers, and toRoman wrote one "m" per
// thousand, so a sender's offset of 1e9 drew a megabyte-long marker and
// one of 4.6e18 looped until the conversation hung. The browser keeps the
// counter in 32 bits (Slack's counter-reset "list-2 {offset}") and
// lower-roman falls back to decimal above 3999.
func TestListHugeOffsetDraws(t *testing.T) {
	for _, c := range []struct {
		offset string
		want   []string
	}{
		{"3997", []string{"mmmcmxcviii.", "mmmcmxcix."}},
		{"3998", []string{"mmmcmxcix.", "4000."}},
		{"999999999", []string{"1000000000.", "1000000001."}},
		{"4611686018427387904", []string{"2147483647.", "2147483647."}},
	} {
		got := listMarkers(t, listBlocks("2", c.offset), 5*time.Second)
		if len(got) != len(c.want) || (len(got) == 2 && (got[0] != c.want[0] || got[1] != c.want[1])) {
			short := make([]string, len(got))
			for i, s := range got {
				short[i] = s[:min(len(s), 24)]
			}
			t.Errorf("offset %s: markers %q (%d), want %q", c.offset, short, len(got), c.want)
		}
		if t.Failed() {
			return // the next offset is bigger: the old toRoman never finishes it
		}
	}
}
