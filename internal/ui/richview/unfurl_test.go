package richview

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// embedDoc is the doc of the #embeds-gallery message name.
func embedDoc(t testing.TB, name string) rt.Doc {
	t.Helper()
	for _, f := range fakeslack.EmbedsFixtures("http://img.test/") {
		if f.Name != name {
			continue
		}
		m := slack.Message{Type: "message", TS: "1762700000.000200", User: "U0", Text: f.Text, Attachments: f.Attachments}
		return rt.FromMessage(codeRes{}, &m)
	}
	t.Fatalf("no embeds fixture %q", name)
	return rt.Doc{}
}

func attBlock(t testing.TB, d rt.Doc) *rt.Block {
	t.Helper()
	for i := range d.Blocks {
		if d.Blocks[i].Kind == rt.AttachmentBlock {
			return &d.Blocks[i]
		}
	}
	t.Fatal("no attachment block")
	return nil
}

// TestUnfurlKinds: MessageAttachmentDefaultV3's routing for each fixture.
func TestUnfurlKinds(t *testing.T) {
	want := map[string]attKind{"x post": akSocial, "youtube": akVideo, "github": akV1, "small thumb": akGeneric,
		"large image": akGeneric, "text only": akGeneric, "image link": akImageCard, "block kit": akV2}
	for name, k := range want {
		if got := kindOf(attBlock(t, embedDoc(t, name))); got != k {
			t.Errorf("%s: kind %d, want %d", name, got, k)
		}
	}
}

func layoutDoc(t testing.TB, d rt.Doc, w int, o Options) *View {
	t.Helper()
	v := New(d, o)
	st := newTestStyle()
	v.relayout(st, w, 1, 1)
	return v
}

// cardBox is the first 12px-radius bordered box (a v3 card).
func cardBox(t testing.TB, v *View) image.Rectangle {
	t.Helper()
	for _, b := range v.d.boxes {
		if b.kind == bBorder && b.radius == 12 {
			return b.r
		}
	}
	t.Fatal("no card box")
	return image.Rectangle{}
}

// TestXCard: the X post is MessageAttachmentTwitterX's social card, as in
// Slack (measured live, light, 2026-11-09): 358 wide; the left box
// padding 12 with the title row, the text 8px under it, the footer
// (Slack's X glyph, "x.com") 8px under that; the image 356x200 under the
// left box. No play button: there is no video_html.
func TestXCard(t *testing.T) {
	v := layoutDoc(t, embedDoc(t, "x post"), 900, Options{})
	r := cardBox(t, v)
	if r.Dx() != 358 {
		t.Errorf("card width %d, want 358", r.Dx())
	}
	var media image.Rectangle
	icon := false
	for _, im := range v.d.imgs {
		if strings.Contains(im.url, "x-post") {
			media = im.r
		}
		if im.url == iconURL+"brand-x-filled" {
			icon = true
		}
	}
	if media.Dx() != 356 || media.Dy() != 200 || media.Max.Y != r.Max.Y-1 {
		t.Errorf("image %v in card %v, want 356x200 at the bottom", media, r)
	}
	if !icon {
		t.Error("footer has no brand-x-filled glyph")
	}
	if len(v.d.overlayIcons) != 0 {
		t.Errorf("play buttons on an image unfurl: %+v", v.d.overlayIcons)
	}
	for _, b := range v.d.boxes {
		if b.kind == bPlay || b.kind == bCircle {
			t.Errorf("play overlay %v on the X card", b.r)
		}
	}
	// title 19 + 4, text 8 + 4 lines (two lines, a paragraph break, a
	// line... clamped at 6), footer 8 + 16, padding 24, borders 2, image 200
	if r.Dy() < 200+2+24+19+4+8+16+8+16 || r.Dy() > 200+2+24+19+4+8+6*16+8+16 {
		t.Errorf("card height %d out of range", r.Dy())
	}
	if !textHas(v, "x.com") || !textHas(v, "Clack Devs") {
		t.Errorf("missing title or footer: %q", string(v.d.plain))
	}
}

func textHas(v *View, s string) bool { return strings.Contains(string(v.d.plain), s) }

// TestGenericCard: the v3 row card is 716x108 with the 106px right box;
// thumb_url alone fills it (cover), an image_url too, no media at all
// shows Slack's no-image placeholder (#eaeaea).
func TestGenericCard(t *testing.T) {
	for _, tc := range []struct {
		name, img string
	}{{"small thumb", "news-thumb"}, {"large image", "blog-hero"}, {"text only", ""}} {
		v := layoutDoc(t, embedDoc(t, tc.name), 900, Options{})
		r := cardBox(t, v)
		if r.Dx() != 716 || r.Dy() != 108 {
			t.Errorf("%s: card %v, want 716x108", tc.name, r)
		}
		var side image.Rectangle
		for _, im := range v.d.imgs {
			if tc.img != "" && strings.Contains(im.url, tc.img) {
				side = im.r
			}
		}
		if tc.img == "" {
			for _, b := range v.d.boxes {
				if b.kind == bFill && b.fill == rgb(234, 234, 234) {
					side = b.r
				}
			}
		}
		if side.Dx() != 106 || side.Dy() != 106 || side.Max.X != r.Max.X-1 {
			t.Errorf("%s: right box %v, want 106x106 at the card's right", tc.name, side)
		}
	}
}

// TestVideoServiceCard: YouTube's video_service_card: 358 wide, the 200px thumb
// with the play and external-link buttons (video_html only).
func TestVideoServiceCard(t *testing.T) {
	v := layoutDoc(t, embedDoc(t, "youtube"), 900, Options{})
	r := cardBox(t, v)
	if r.Dx() != 358 {
		t.Errorf("card width %d", r.Dx())
	}
	names := map[string]bool{}
	for _, o := range v.d.overlayIcons {
		names[o.name] = true
	}
	if !names["play"] || !names["external"] {
		t.Errorf("video buttons %v, want play and external", names)
	}
	if !textHas(v, "Clack Channel") || !textHas(v, "YouTube") {
		t.Errorf("author or footer missing: %q", string(v.d.plain))
	}
}

// TestV1Card: a bot's (app) unfurl is the classic card with its color bar.
func TestV1Card(t *testing.T) {
	v := layoutDoc(t, embedDoc(t, "github"), 900, Options{})
	bar := false
	for _, b := range v.d.boxes {
		if b.kind == bFill && b.fill == rgb(0x36, 0xa6, 0x4f) && b.r.Dx() == 4 {
			bar = true
		}
	}
	if !bar {
		t.Error("no 4px color bar")
	}
	for _, s := range []string{"octo-org/clack", "#42 Draw link previews", "Comments", "Open", "GitHub"} {
		if !textHas(v, s) {
			t.Errorf("missing %q in %q", s, string(v.d.plain))
		}
	}
}

// TestNonMediaPref: with expand_non_media_attachments off a text preview
// of a link in the message is gone; the X post (an image) stays only if
// it has no text, so it goes too.
func TestNonMediaPref(t *testing.T) {
	p := *uipref.Get()
	p.NoTextPreviews = true
	uipref.Set(p)
	defer uipref.Reset()
	for _, name := range []string{"small thumb", "x post"} {
		v := layoutDoc(t, embedDoc(t, name), 900, Options{})
		for _, b := range v.d.boxes {
			if b.kind == bBorder && b.radius == 12 {
				t.Errorf("%s: preview still drawn", name)
			}
		}
	}
	v := layoutDoc(t, embedDoc(t, "image link"), 900, Options{})
	cardBox(t, v) // media with no text stays
}

type removeRec struct {
	recHandler
	removed []string
}

func (r *removeRec) RemoveAttachment(id string) { r.removed = append(r.removed, id) }

// TestRemoveButton: the viewer's previews get the overlay "x" while the
// card is hovered; clicking it asks the handler to remove attachment 1.
func TestRemoveButton(t *testing.T) {
	if v := layoutDoc(t, embedDoc(t, "x post"), 900, Options{}); len(v.d.removes) != 0 {
		t.Fatal("remove button on someone else's message")
	}
	h := &removeRec{}
	hs := &harness{st: newTestStyle(), v: New(embedDoc(t, "x post"), Options{Own: true}), h: h, w: 900}
	hs.frame()
	if len(hs.v.d.removes) != 1 {
		t.Fatalf("%d remove buttons", len(hs.v.d.removes))
	}
	card := cardBox(t, hs.v)
	rm := hs.v.d.removes[0].r
	if rm.Dx() != 20 || rm.Min.Y != card.Min.Y-9 || rm.Max.X != card.Max.X+9 {
		t.Errorf("button %v for card %v, want 20px at top -9 right -9", rm, card)
	}
	c := f32.Pt(float32(rm.Min.X+rm.Max.X)/2, float32(rm.Min.Y+rm.Max.Y)/2)
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: c}, press(c, 0), release(c, 20))
	if len(h.removed) != 1 || h.removed[0] != "1" {
		t.Errorf("removed %v, want [1]", h.removed)
	}
}

// gradImages serves a gradient for every URL (screenshots).
type gradImages struct{ cache map[string]paint.ImageOp }

func (g *gradImages) Get(url string, px int) (paint.ImageOp, bool) {
	if op, ok := g.cache[url]; ok {
		return op, true
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	h := 0
	for _, c := range url {
		h = h*31 + int(c)
	}
	base := color.NRGBA{R: uint8(h), G: uint8(h >> 8), B: uint8(h >> 16), A: 255}
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.NRGBA{R: base.R/2 + uint8(x*2), G: base.G/2 + uint8(y*2), B: base.B / 2, A: 255})
		}
	}
	op := paint.NewImageOp(img)
	g.cache[url] = op
	return op, true
}

// TestEmbedsCardShots saves each fixture's message body, light and dark,
// and the X post hovered (its remove button), when EMBEDS_SHOTS_DIR is set
// (the embeds-shots workflow).
func TestEmbedsCardShots(t *testing.T) {
	dir := os.Getenv("EMBEDS_SHOTS_DIR")
	if dir == "" {
		t.Skip("EMBEDS_SHOTS_DIR not set (the embeds-shots workflow sets it)")
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		pal := LightPalette()
		if dark {
			pal = DarkPalette()
		}
		for _, f := range fakeslack.EmbedsFixtures("http://img.test/") {
			for _, hover := range []bool{false, true} {
				if hover && f.Name != "x post" {
					continue
				}
				st := NewStyle(themeShaper(), &gradImages{cache: map[string]paint.ImageOp{}}, pal)
				v := New(embedDoc(t, f.Name), Options{Own: f.User < 0})
				var r input.Router
				var ops op.Ops
				const w, h, pad = 820, 460, 20
				frame := func() {
					ops.Reset()
					paint.FillShape(&ops, pal.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
					tr := op.Offset(image.Pt(pad, pad)).Push(&ops)
					gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
						Constraints: layout.Exact(image.Pt(w-2*pad, h-2*pad))}
					v.Layout(gtx, st, &recHandler{})
					tr.Pop()
					r.Frame(&ops)
				}
				frame()
				name := "embeds-" + strings.ReplaceAll(f.Name, " ", "-") + "-" + mode
				if hover {
					c := cardBox(t, v)
					r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(pad+c.Min.X+40), float32(pad+c.Min.Y+40))})
					frame()
					frame()
					name += "-hover"
				}
				frame()
				img := pixeltest.Render(t, w, h, &ops)
				fh, err := os.Create(filepath.Join(dir, name+".png"))
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(fh, img); err != nil {
					t.Fatal(err)
				}
				fh.Close()
			}
		}
	}
}
