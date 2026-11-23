package quip

import (
	"errors"
	"strings"
	"testing"
)

// Past maxDiff only the common start and end of an edit are followed: a
// comment anchor in the middle refuses the edit rather than go (the text
// around it was untouched, Slack's withRtml would keep it).
func TestQuipApplyRunsUnfollowedAnchor(t *testing.T) {
	a, b := strings.Repeat("a", 2100), strings.Repeat("b", 2100)
	base := a + `<annotation id="z">mid</annotation>` + b
	edited := "Z" + a[1:] + "mid" + b[:len(b)-1] + "Y" // a typo fixed at each end
	_, err := ApplyRuns(base, []Run{{Text: edited}})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("an anchor in a stretch too long to follow: %v", err)
	}
	// a short one is followed, and the anchor moves with its characters
	out, err := ApplyRuns(`aa<annotation id="z">mid</annotation>bb`, []Run{{Text: "Za" + "mid" + "bY"}})
	if err != nil || out != `Za<annotation id="z">mid</annotation>bY` {
		t.Errorf("a short edit around an anchor: %q, %v", out, err)
	}
}

// SameRTML holds two RTMLs to the same characters and marks, however the
// marks are cut up.
func TestQuipSameRTML(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{`<b>ab</b>`, `<b>a</b><b>b</b>`, true},
		{`<b>Owner: </b><b><control id="x"></control></b>`, `<b>Owner: <control id="x"></control></b>`, true},
		{`ab`, `<b>a</b>b`, false},
		{`<a href="https://a.example">x</a>`, `<a href="https://b.example">x</a>`, false},
		{`ab<annotation id="z"></annotation>cd`, `abcd`, false},
		{`a&nbsp;b`, "a b", true},
		{`Notes<br>`, `Notes`, false},
	} {
		if got := SameRTML(c.a, c.b); got != c.same {
			t.Errorf("SameRTML(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
