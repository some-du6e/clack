package canvas

import (
	"strings"
	"testing"
)

// TestParseEmbeddedLinkLabel: Quip's export of a link card is its
// placeholder alone, "Link URL: <url>", with no id and no anchor (the
// card is drawn from Quip's model). The card's URL is the text after the
// label, and the label is never its title; an anchor's href still goes
// first, and a bare URL is the URL.
func TestParseEmbeddedLinkLabel(t *testing.T) {
	for _, c := range []struct{ name, html, url string }{
		{"label", `<p class='embedded-link'>Link URL: https://example.com/repo</p>`, "https://example.com/repo"},
		{"anchor", `<p class='embedded-link'>Link URL: <a href="https://example.com/a">https://example.com/b</a></p>`, "https://example.com/a"},
		{"bare", `<p class='embedded-link'>https://example.com/bare</p>`, "https://example.com/bare"},
	} {
		d, err := Parse(`<div class="quip-canvas-content">` + c.html + `</div>`)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(d.Blocks) != 1 || d.Blocks[0].Kind != Embed {
			t.Fatalf("%s: blocks %+v, want one Embed", c.name, d.Blocks)
		}
		b := d.Blocks[0]
		if b.URL != c.url || b.Title != "" || b.ID != "" {
			t.Errorf("%s: URL %q title %q id %q, want URL %q and no title or id", c.name, b.URL, b.Title, b.ID, c.url)
		}
	}
	// what Clack writes is Quip's shape, and reads back the same
	d := Doc{Blocks: []Block{{Kind: Embed, URL: "https://example.com/repo"}}}
	if got, want := HTML(d, func() string { return "temp:C:LC1x1" }), `<p class='embedded-link'>Link URL: https://example.com/repo</p>`; !strings.Contains(got, want) {
		t.Errorf("HTML %s, want it to hold %s", got, want)
	}
}
