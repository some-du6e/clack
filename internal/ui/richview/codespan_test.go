package richview

import (
	"encoding/json"
	"image"
	"reflect"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	rt "github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// A synthetic app message with the element sequence of the reported
// blank code chip: emoji, text, code "@Abcd" (a text element, not a
// mention), text, bold, text, bold, text, italic.
const codeAfterEmoji = `{"ts":"1.0","bot_id":"B1","text":"x","blocks":[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[
{"type":"emoji","name":"robot_face"},
{"type":"text","text":" Ask "},
{"type":"text","text":"@Abcd","style":{"code":true}},
{"type":"text","text":" about "},
{"type":"text","text":"this","style":{"bold":true}},
{"type":"text","text":" or "},
{"type":"text","text":"that","style":{"bold":true}},
{"type":"text","text":", "},
{"type":"text","text":"soon","style":{"italic":true}}]}]}]}`

type codeRes struct{}

func (codeRes) UserName(string) string      { return "" }
func (codeRes) ChannelName(string) string   { return "" }
func (codeRes) UsergroupName(string) string { return "" }
func (codeRes) SelfID() string              { return "U0" }
func (codeRes) Emoji(name string) (string, string, bool) {
	return "https://e.test/" + name + ".png", "", true
}

var themeShaper = sync.OnceValue(theme.NewShaper) // Lato + system fonts, as in the app

func codeDoc(t *testing.T) rt.Doc {
	t.Helper()
	var m slack.Message
	if err := json.Unmarshal([]byte(codeAfterEmoji), &m); err != nil {
		t.Fatal(err)
	}
	d := rt.FromMessage(codeRes{}, &m)
	var code []rt.Span
	for _, b := range d.Blocks {
		for _, s := range b.Spans {
			if s.Style&rt.Code != 0 {
				code = append(code, s)
			}
		}
	}
	if len(code) != 1 || code[0].Kind != rt.Text || code[0].Text != "@Abcd" {
		t.Fatalf("code spans %+v, want one plain Text \"@Abcd\" (no mention)", code)
	}
	return d
}

func hasSegments(p clip.PathSpec) bool { return reflect.ValueOf(p).FieldByName("hasSegments").Bool() }

// TestCodeSpanAfterEmoji: the code chip's glyphs are real outlines in the
// code font and colour, inside the CodeBg box, drawn after it; and (with a
// GPU) its pixels show CodeText over CodeBg. Light and dark.
func TestCodeSpanAfterEmoji(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "light"
		pal := LightPalette()
		if dark {
			name, pal = "dark", DarkPalette()
		}
		t.Run(name, func(t *testing.T) {
			st := NewStyle(themeShaper(), noImages{}, pal)
			const w, h = 420, 40
			v := layoutAt(st, codeDoc(t), Options{}, w)
			f := findFrag(t, &v.d, "@Abcd")
			if f.col != pal.CodeText || f.size != 12 {
				t.Fatalf("code frag colour %v size %d, want %v 12", f.col, f.size, pal.CodeText)
			}
			if f.target >= 0 || f.comp != 0 {
				t.Fatalf("code frag is clickable/a component (target %d comp %d): a mention?", f.target, f.comp)
			}
			if !hasSegments(st.Shaper.Shape(f.sh.glyphs)) {
				t.Fatal("code glyphs have no outline (drawn as nothing)")
			}
			if p, ok := st.emboldenedOf(f.sh, 0.3); ok && !hasSegments(p) {
				t.Fatal("emboldened code glyphs have no outline")
			}
			bs := boxesOf(&v.d, bBorder)
			if len(bs) != 1 || !boxContainsFrag(bs[0], f) || bs[0].fill != pal.CodeBg || bs[0].stroke != pal.CodeBorder {
				t.Fatalf("code boxes %+v around %v", bs, f)
			}
			box := bs[0]
			for _, b := range v.d.boxes {
				if b.layer != 0 && b.r.Overlaps(box.r) {
					t.Fatalf("a box drawn above the text covers the code chip: %+v", b)
				}
			}

			var ops op.Ops
			paint.FillShape(&ops, pal.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
			gtx := testGtx(&ops, w)
			v.Layout(gtx, st, &recHandler{})
			img := pixeltest.Render(t, w, h, &ops)
			in := box.r.Inset(2)
			if n := pixeltest.Ink(img, in, pal.CodeText, pal.CodeBg); n < 12 {
				t.Errorf("%d CodeText pixels in the code chip %v, want ≥12 (blank chip)", n, in)
			}
			if n := pixeltest.Count(img, in, pal.CodeBg, 3); n < in.Dx()*in.Dy()/3 {
				t.Errorf("only %d CodeBg pixels in the chip %v", n, in)
			}
		})
	}
}

// TestEmojiTooltip: resting on an emoji shows its :name: (with the skin
// tone) in Slack's dark tooltip after EmojiTipDelay, above the emoji.
func TestEmojiTooltip(t *testing.T) {
	em := func(id string) rt.Span { return rt.Span{Kind: rt.Emoji, ID: id, URL: "https://e.test/" + id + ".png"} }
	d := doc(para(txt("one")), para(txt("two")), para(txt("three")), para(txt("wave "), em("wave::skin-tone-3"), txt(" and "), em("robot_face")))
	st := NewStyle(themeShaper(), noImages{}, LightPalette())
	v := New(d, Options{})
	var r input.Router
	var ops op.Ops
	const w, h = 400, 200
	t0 := time.Unix(1000, 0)
	frame := func(now time.Time) {
		ops.Reset()
		paint.FillShape(&ops, st.Palette.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Exact(image.Pt(w, h))}
		v.Layout(gtx, st, &recHandler{})
		r.Frame(&ops)
	}
	frame(t0)
	var ems []*frag
	for i := range v.d.frags {
		if f := &v.d.frags[i]; f.kind == fImage {
			ems = append(ems, f)
		}
	}
	if len(ems) != 2 {
		t.Fatalf("%d emoji frags", len(ems))
	}
	for i, want := range []string{":wave::skin-tone-3:", ":robot_face:"} {
		f := ems[i]
		c := f32.Pt(f.x+float32(f.size)/2, float32(f.y+f.size/2))
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: c})
		frame(t0)
		if got, ok := v.HoveredEmoji(); !ok || got != want {
			t.Fatalf("hovered %q %v, want %q", got, ok, want)
		}
	}
	f := ems[1]
	above := image.Rect(int(f.x)-10, f.y-30, int(f.x)+f.size+10, f.y-8)
	frame(t0.Add(100 * time.Millisecond))
	early := pixeltest.Render(t, w, h, &ops)
	frame(t0.Add(EmojiTipDelay + 10*time.Millisecond))
	late := pixeltest.Render(t, w, h, &ops)
	if n := pixeltest.Count(early, above, st.Palette.TooltipBg, 8); n > 0 {
		t.Errorf("tooltip before the delay (%d px)", n)
	}
	if n := pixeltest.Count(late, above, st.Palette.TooltipBg, 8); n < 100 {
		t.Errorf("no tooltip above the emoji after the delay (%d px)", n)
	}
	if n := pixeltest.Ink(late, above, st.Palette.TooltipText, st.Palette.TooltipBg); n < 10 {
		t.Errorf("tooltip has no text (%d px)", n)
	}

	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(2, 2)})
	frame(t0.Add(time.Second))
	if _, ok := v.HoveredEmoji(); ok {
		t.Fatal("still hovering an emoji over plain text")
	}
}
