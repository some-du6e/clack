package quip

import "testing"

// TestParseUnfurlID: a slack_unfurl_id is "suo:<team>/<channel>/<ts>"
// (the webapp's parseSlackUnfurlId); anything else names no container.
func TestParseUnfurlID(t *testing.T) {
	const id = "suo:T0FAKE0001/C0FAKE00000/1762500000.000100"
	team, ch, ts, ok := ParseUnfurlID(id)
	if !ok || team != "T0FAKE0001" || ch != "C0FAKE00000" || ts != "1762500000.000100" {
		t.Errorf("ParseUnfurlID(%q) = %q %q %q %v", id, team, ch, ts, ok)
	}
	for _, bad := range []string{"", "suo:", "suo:T/C", "suo:T/C/1/2", "sc:T/C/1"} {
		if _, _, _, ok := ParseUnfurlID(bad); ok {
			t.Errorf("ParseUnfurlID(%q) ok", bad)
		}
	}
	if got := UnfurlID("T0FAKE0001", "C0FAKE00000", "1762500000.000100"); got != id {
		t.Errorf("UnfurlID = %q, want %q", got, id)
	}
}

// TestPairLinkCardsByURL: the export's link cards pair with the model's
// card sections by url, each the next unused one, not by count: a file
// card (type 56 too) or a card the export doesn't show is skipped.
func TestPairLinkCardsByURL(t *testing.T) {
	mk := func(id, u string) *Section {
		return &Section{ID: Ptr(id), Type: Ptr(TypeLinkUnfurlCard), Content: &Content{LinkUnfurl: &LinkUnfurlCard{URL: Ptr(u)}}}
	}
	cards := []*Section{mk("f", "https://example.com/file.pdf"), mk("a1", "https://example.com/a"), mk("b", "https://example.com/b"), mk("a2", "https://example.com/a")}
	got := PairLinkCards(cards, []string{"https://example.com/a", "https://example.com/a", "https://example.com/c", "https://example.com/b"})
	want := []string{"a1", "a2", "", "b"}
	for i, s := range got {
		id := ""
		if s != nil {
			id = *s.ID
		}
		if id != want[i] {
			t.Errorf("card %d paired with %q, want %q", i, id, want[i])
		}
	}
}
