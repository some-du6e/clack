package richview

import (
	"strings"
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// A resolved channel-tab slug (richtext tabslug.go) is a RichTextSlug
// pill: the tab's icon, "Pins in ", the private channel's lock where its
// name starts, then the name; one slug target that links to the tab and
// keeps the tab id for TabSlugCard. Synthetic names.
func TestChannelTabSlugPill(t *testing.T) {
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	doc := rt.Doc{Blocks: []rt.Block{{Kind: rt.Paragraph, Spans: []rt.Span{
		{Kind: rt.ChannelTab, Text: "Pins in secret", Pre: len("Pins in "), ID: "G1", URL: "/archives/G1/tabs/pins",
			Icon: "pin", ChIcon: "lock", Chan: "secret"},
	}}}}
	v := layoutAt(st, doc, Options{}, 520)
	var order []string
	for i := range v.d.frags {
		f := &v.d.frags[i]
		switch {
		case f.kind == fText && f.sh != nil && strings.TrimSpace(f.sh.str) != "":
			order = append(order, strings.TrimSpace(f.sh.str))
		case f.kind != fText && strings.HasPrefix(f.url, iconURL):
			order = append(order, f.url)
		}
	}
	if got, want := strings.Join(order, " "), "icon:pin Pins in icon:lock secret"; got != want {
		t.Errorf("the pill's parts %q, want %q", got, want)
	}
	name := findFrag(t, &v.d, "secret")
	tg := v.d.targets[name.target]
	if !tg.slug || tg.underline || tg.kind != tLink || tg.url != "/archives/G1/tabs/pins" || tg.id != "G1" || tg.tab != "pins" {
		t.Errorf("the slug's target %+v", tg)
	}
}
