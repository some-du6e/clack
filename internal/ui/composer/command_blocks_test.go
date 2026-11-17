package composer

import (
	"encoding/json"
	"testing"
)

// A slash command's arguments go out as executeCommand's blocks
// (boot-render 5dbN: convertDeltaToBlocks of deltaWithoutCmd with
// expandTruncatedLinks): one rich_text block with no block_id, mentions as
// their member, channel, group or range, a link slug as a bare link, and no
// blocks for blank arguments. Karim's /remind with his own @karim chip went
// out as the plain text "@karim", and Slack's /remind matched that to a
// different member.
func TestCommandBlocksLikeSlack(t *testing.T) {
	type piece struct {
		text string
		atom *Atom
	}
	doc := func(ps ...piece) *Doc {
		d := NewDoc()
		pos := 0
		for _, p := range ps {
			if p.atom != nil {
				pos = d.InsertAtom(pos, p.atom)
			} else {
				pos = d.Insert(pos, p.text, Attr{})
			}
		}
		return d
	}
	txt := func(s string) piece { return piece{text: s} }
	atom := func(a *Atom) piece { return piece{atom: a} }
	karim := &Atom{Kind: AtomUser, ID: "U082DRXPMF1", Text: "@karim"}

	cases := []struct {
		name string
		d    *Doc
		want string // "" = no blocks
	}{
		{"mention", doc(txt("/remind me "), atom(karim), txt(" do that tomorrow at 3:05pm")),
			`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` +
				`{"type":"text","text":"me "},{"type":"user","user_id":"U082DRXPMF1"},{"type":"text","text":" do that tomorrow at 3:05pm"}]}]}]`},
		{"mention first", doc(txt("/remind "), atom(karim), txt(" do that")),
			`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` +
				`{"type":"user","user_id":"U082DRXPMF1"},{"type":"text","text":" do that"}]}]}]`},
		{"channel, group, broadcast", doc(txt("/ticket "),
			atom(&Atom{Kind: AtomChannel, ID: "C0CFT52NEGM", Text: "#clack"}), txt(" "),
			atom(&Atom{Kind: AtomUsergroup, ID: "S0123", Text: "@devs"}), txt(" "),
			atom(&Atom{Kind: AtomBroadcast, ID: "here", Text: "@here"})),
			`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` +
				`{"type":"channel","channel_id":"C0CFT52NEGM"},{"type":"text","text":" "},` +
				`{"type":"usergroup","usergroup_id":"S0123"},{"type":"text","text":" "},` +
				`{"type":"broadcast","range":"here"}]}]}]`},
		// expandTruncatedLinks: url only, no text, no truncated
		{"link slug expands", doc(txt("/ticket "),
			atom(&Atom{Kind: AtomLink, ID: "https://example.com/docs/a/guide", URL: "https://example.com/docs/a/guide", Text: "example.com/docs/…/guide"})),
			`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` +
				`{"type":"link","url":"https://example.com/docs/a/guide"}]}]}]`},
		{"leading space", doc(txt("  /ticket   anchor")),
			`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"anchor"}]}]}]`},
		{"no arguments", doc(txt("/giphy")), ""},
		{"blank arguments", doc(txt("/giphy   ")), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := commandArgs(c.d).CommandBlocks()
			if c.want == "" {
				if got != nil {
					t.Fatalf("blank arguments sent blocks %s; Slack sends none", got)
				}
				return
			}
			var g, w any
			if err := json.Unmarshal(got, &g); err != nil {
				t.Fatalf("blocks %q: %v", got, err)
			}
			_ = json.Unmarshal([]byte(c.want), &w)
			gb, _ := json.Marshal(g)
			wb, _ := json.Marshal(w)
			if string(gb) != string(wb) {
				t.Errorf("blocks\n got %s\nwant %s", gb, wb)
			}
		})
	}

	// the link slug's atom is shared with the composer: expanding it for
	// the command must not touch the document
	d := doc(txt("/ticket "), atom(&Atom{Kind: AtomLink, ID: "https://example.com/x", URL: "https://example.com/x", Text: "example.com/x…"}))
	_ = commandArgs(d).CommandBlocks()
	for _, a := range d.Attrs {
		if a.Atom != nil && a.Atom.Text != "example.com/x…" {
			t.Errorf("CommandBlocks rewrote the composer's link slug to %q", a.Atom.Text)
		}
	}
}
