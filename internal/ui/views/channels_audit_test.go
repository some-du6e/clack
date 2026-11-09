package views

// Channel details and browser rules from the channels audit
// (docs/CHANNELS-AUDIT.md), checked against Slack's web client side by
// side on 2026-11-08.

import (
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// MediumMemberEntity: bold display name (real name when none is set),
// then the real name after a display name. #clack's members: "clod (you)
// clod", "dodge1 dodge1", "karim" (real name a zero-width space, which
// core drops), "kyto Kyto Agent".
func TestMemberNames(t *testing.T) {
	cases := []struct {
		u          core.User
		prim, secd string
	}{
		{core.User{ID: "USELF", Name: "clod", RealName: "clod", DisplaySet: true}, "clod (you)", "clod"},
		{core.User{ID: "U1", Name: "dodge1", RealName: "dodge1", DisplaySet: true}, "dodge1", "dodge1"},
		{core.User{ID: "U2", Name: "karim", RealName: "", DisplaySet: true}, "karim", ""},
		{core.User{ID: "U3", Name: "kyto", RealName: "Kyto Agent", DisplaySet: true}, "kyto", "Kyto Agent"},
		{core.User{ID: "U4", Name: "Robin Example", RealName: "Robin Example"}, "Robin Example", ""},
	}
	for _, c := range cases {
		p, s := cdMemberNames(c.u, "USELF")
		if p != c.prim || s != c.secd {
			t.Errorf("%s: got %q / %q, want %q / %q", c.u.ID, p, s, c.prim, c.secd)
		}
	}
}

func TestSelfFirst(t *testing.T) {
	us := []core.User{{ID: "UK"}, {ID: "USELF"}, {ID: "UD"}}
	got := cdSelfFirst(us, "USELF")
	var ids []string
	for _, u := range got {
		ids = append(ids, u.ID)
	}
	if strings.Join(ids, ",") != "USELF,UK,UD" {
		t.Errorf("order %v", ids)
	}
	if got := cdSelfFirst(us[:1], "USELF"); len(got) != 1 || got[0].ID != "UK" {
		t.Errorf("without the viewer: %+v", got)
	}
}

// A purpose's unresolved channel mention parses again once core had a
// second to name it; resolved ones stay cached.
func TestRichLineCache(t *testing.T) {
	name := "C0EA9S0A0"
	calls := 0
	parse := func(raw string) []richtext.Block {
		calls++
		return []richtext.Block{{Spans: []richtext.Span{{Kind: richtext.Text, Text: "Also see: "}, {Kind: richtext.Channel, ID: "C0EA9S0A0", Text: "#" + name}}}}
	}
	c := richLineCache{}
	t0 := time.Unix(1000, 0)
	if s := c.get("x", parse, t0); len(s) != 2 || s[1].Text != "#C0EA9S0A0" {
		t.Fatalf("spans %+v", s)
	}
	c.get("x", parse, t0.Add(500*time.Millisecond))
	if calls != 1 {
		t.Errorf("reparsed within a second: %d", calls)
	}
	name = "hq"
	if s := c.get("x", parse, t0.Add(1500*time.Millisecond)); calls != 2 || s[1].Text != "#hq" {
		t.Errorf("after a second: calls %d spans %+v", calls, s)
	}
	c.get("x", parse, t0.Add(10*time.Second))
	if calls != 2 {
		t.Errorf("a resolved purpose parsed again: %d", calls)
	}
}
