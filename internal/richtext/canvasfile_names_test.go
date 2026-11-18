package richtext

import "testing"

// A canvas preview's mentions hold only the ids Quip's export gives
// (canvas parse.go: Text "#C1", "@U1"); a message names them through its
// resolver on each conversion, as Slack's FileQuip renders the thumbnail's
// Block Kit, and the stored blocks are left alone.
func TestNamedPreviewMentions(t *testing.T) {
	stored := []Block{
		{Kind: Header, Spans: []Span{{Kind: Channel, ID: "C1", Text: "#C1"}, tx(" Guidelines")}},
		{Kind: Paragraph, Spans: []Span{tx("plain")}},
		{Kind: List, Items: [][]Span{{tx("first")}, {{Kind: User, ID: "U1", Text: "@U1", Style: Bold}, tx(" and "), {Kind: Usergroup, ID: "S1", Text: "@S1"}}}},
	}
	got := namedPreview(R, stored)
	if s := got[0].Spans[0]; s.Text != "#general" || s.ID != "C1" {
		t.Errorf("channel: %+v", s)
	}
	if s := got[2].Items[1][0]; s.Text != "@alice" || s.Style != Bold {
		t.Errorf("user in a list item: %+v", s)
	}
	if s := got[2].Items[1][2]; s.Text != "@design" {
		t.Errorf("group: %+v", s)
	}
	if got[1].Spans[0].Text != "plain" || got[2].Items[0][0].Text != "first" {
		t.Errorf("text blocks: %+v", got)
	}
	if stored[0].Spans[0].Text != "#C1" || stored[2].Items[1][0].Text != "@U1" {
		t.Errorf("the stored preview changed: %+v", stored)
	}
	// no mentions: the same blocks back
	plain := []Block{{Kind: Paragraph, Spans: []Span{tx("x")}}}
	if out := namedPreview(R, plain); &out[0] != &plain[0] {
		t.Error("a preview without mentions was copied")
	}
}
