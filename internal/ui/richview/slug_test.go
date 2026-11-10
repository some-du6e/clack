package richview

import (
	"image"
	"testing"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// A message_mention pill ("karim in #clack", a link to another message)
// is never underlined, as Slack draws .p-rich_text_slug: its .c-link and
// :hover are text-decoration none, also with Accessibility > "Underline
// links to websites" on (which underlines the plain link beside it). On
// hover it is #0b4c8c on #1d9bd133. Karim (alpha.24): "why is it
// underlined". Synthetic text.
func TestSlugPillNotUnderlined(t *testing.T) {
	old := uipref.Get()
	uipref.Set(store.Prefs{UnderlineLinks: true})
	t.Cleanup(func() { uipref.Set(*old) })
	pal := LightPalette()
	st := NewStyle(themeShaper(), noImages{}, pal)
	doc := rt.Doc{Blocks: []rt.Block{{Kind: rt.Paragraph, Spans: []rt.Span{
		{Kind: rt.Link, Text: "a plain link", URL: "https://example.invalid/page"},
		{Kind: rt.Text, Text: " and "},
		{Kind: rt.MessageMention, Text: "alice in #general", ID: "1700000000.000100", URL: "https://synthetic.slack.com/archives/C1/p1700000000000100"},
	}}}}
	const w, h = 520, 40
	v := layoutAt(st, doc, Options{}, w)
	link, pill := findFrag(t, &v.d, "link"), findFrag(t, &v.d, "#general")
	if pill.target < 0 || !v.d.targets[pill.target].slug || v.d.targets[pill.target].underline {
		t.Fatalf("the pill's target %+v: want a slug, not underlined on hover", v.d.targets[pill.target])
	}
	band := func(f *frag) image.Rectangle { // where underline() draws under f
		y := f.y + max(1, f.size/8)
		return image.Rect(int(f.x), y, int(f.x+f.w+0.5), y+max(1, f.size/14))
	}
	draw := func(hover int16) *image.RGBA {
		var ops op.Ops
		paint.FillShape(&ops, pal.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
		v.hover = hover
		v.Layout(testGtx(&ops, w), st, &recHandler{})
		v.hover = hover // Layout's events may reset it: draw with it once more
		ops.Reset()
		paint.FillShape(&ops, pal.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
		v.draw(testGtx(&ops, w), st)
		return pixeltest.Render(t, w, h, &ops)
	}
	img := draw(-1)
	lb := band(link)
	if n := pixeltest.Count(img, lb, pal.Link, 60); n < lb.Dx()/2 {
		t.Fatalf("control: the plain link has %d of %d underline pixels with the preference on", n, lb.Dx())
	}
	pb := band(pill)
	if n := pixeltest.Count(img, pb, pal.MentionText, 60); n > pb.Dx()/4 {
		t.Errorf("the message_mention pill is underlined (%d of %d px in its underline band)", n, pb.Dx())
	}
	// hovered: Slack's hover text color, still no underline
	r := image.Rect(int(pill.x), pill.y-pill.size, int(pill.x+pill.w+0.5), pill.y+2)
	before := pixeltest.Count(img, r, pal.SlugHoverText, 10)
	img = draw(pill.target)
	if n := pixeltest.Count(img, r, pal.SlugHoverText, 10); n < 10 || n < 3*before {
		t.Errorf("hovered pill: %d px of #0b4c8c text (%d before hovering), want its label in Slack's hover color", n, before)
	}
	if n := pixeltest.Count(img, pb, pal.SlugHoverText, 40) + pixeltest.Count(img, pb, pal.MentionText, 40); n > pb.Dx()/4 {
		t.Errorf("the hovered pill is underlined (%d of %d px)", n, pb.Dx())
	}
}
