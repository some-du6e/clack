package richview

import (
	"encoding/json"
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// A link with truncated: true (Slack's composer sends a pasted link that
// way, its text the server's short form of the URL) is drawn as Slack's
// Link draws it (+W5o): RichTextSlug (Pc7Q) with the "link" icon around
// the link. The chip is the message slug's (.p-rich_text_slug,
// surf-hgl-1, radius 3), the label a .c-link in the link colour that is
// never underlined and opens the URL; a plain link beside it is no chip.
// Karim, 2026-11-13 ("like the message pill thingy"). Synthetic text.
func TestTruncatedLinkChip(t *testing.T) {
	const url = "https://example.com/docs/guides/abcdefghijabcdefghijabcdefghij"
	const label = "example.com/docs/…/abcdefghijab…"
	for _, dark := range []bool{false, true} {
		pal := LightPalette()
		if dark {
			pal = DarkPalette()
		}
		st := NewStyle(themeShaper(), noImages{}, pal)
		doc := rt.Doc{Blocks: []rt.Block{{Kind: rt.Paragraph, Spans: []rt.Span{
			{Kind: rt.Text, Text: "read "},
			{Kind: rt.Link, Text: label, URL: url, Truncated: true},
			{Kind: rt.Text, Text: " or "},
			{Kind: rt.Link, Text: "the plain one", URL: "https://example.invalid/page"},
		}}}}
		v := layoutAt(st, doc, Options{}, 640)
		lb := findFrag(t, &v.d, label)
		if lb.target < 0 {
			t.Fatalf("dark=%v: the label has no target", dark)
		}
		tg := v.d.targets[lb.target]
		if tg.kind != tLink || tg.url != url || !tg.slug || !tg.urlCard || tg.underline || tg.website {
			t.Errorf("dark=%v: label target %+v, want the URL, a slug never underlined, with its URL card", dark, tg)
		}
		if lb.col != pal.Link {
			t.Errorf("dark=%v: label colour %v, want the link colour %v (.c-link)", dark, lb.col, pal.Link)
		}
		var icon *frag
		for i := range v.d.frags {
			if f := &v.d.frags[i]; f.kind == fImage && f.url == iconURL+"link" {
				icon = f
			}
		}
		if icon == nil {
			t.Fatalf("dark=%v: no link icon", dark)
		}
		if icon.target != lb.target || icon.col != pal.MentionText || icon.x >= lb.x {
			t.Errorf("dark=%v: icon %+v before the label at %v, want the chip's colour %v and the label's target", dark, *icon, lb.x, pal.MentionText)
		}
		var chip *box
		for i := range v.d.boxes {
			if b := &v.d.boxes[i]; b.fill == pal.MentionBg && b.target == lb.target {
				chip = b
			}
		}
		if chip == nil {
			t.Fatalf("dark=%v: no surf-hgl-1 chip behind the link", dark)
		}
		if chip.radius != 3 || chip.r.Min.X > int(icon.x) || chip.r.Max.X < int(lb.x+lb.w) || chip.hover != pal.MentionHoverBg {
			t.Errorf("dark=%v: chip %+v around icon x %v and label %v..%v", dark, *chip, icon.x, lb.x, lb.x+lb.w)
		}
		pl := findFrag(t, &v.d, "plain")
		if ptg := v.d.targets[pl.target]; ptg.slug || !ptg.underline {
			t.Errorf("dark=%v: the plain link's target %+v", dark, ptg)
		}
	}
}

// richtext keeps the flag: a rich_text link with truncated is a Link span
// marked Truncated, its text as it is (getLinkContents).
func TestTruncatedLinkSpan(t *testing.T) {
	blocks := json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` +
		`{"type":"link","url":"https://example.com/a/b/c/d/e","text":"example.com/a/b/…/e","truncated":true},` +
		`{"type":"text","text":" and "},{"type":"link","url":"https://example.com/x","text":"x"}]}]}]`)
	m := slack.Message{Type: "message", TS: "1762700000.000300", User: "U0", Text: "<https://example.com/a/b/c/d/e|example.com/a/b/…/e> and <https://example.com/x|x>", Blocks: blocks}
	d := rt.FromMessage(codeRes{}, &m)
	if len(d.Blocks) != 1 || len(d.Blocks[0].Spans) != 3 {
		t.Fatalf("doc %+v", d)
	}
	ss := d.Blocks[0].Spans
	if s := ss[0]; s.Kind != rt.Link || !s.Truncated || s.Text != "example.com/a/b/…/e" || s.URL != "https://example.com/a/b/c/d/e" {
		t.Errorf("truncated link %+v", s)
	}
	if s := ss[2]; s.Kind != rt.Link || s.Truncated {
		t.Errorf("plain link %+v", s)
	}
}
