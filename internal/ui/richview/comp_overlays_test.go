package richview

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// treeBridge is a JS side that answers each Render of a component it was
// given a tree for with that tree (a plugin's replacement).
type treeBridge struct {
	trees map[string]string
	roots map[string][]uint64
}

func (b *treeBridge) Render(root uint64, name string, _ []byte) {
	if b.roots == nil {
		b.roots = map[string][]uint64{}
	}
	b.roots[name] = append(b.roots[name], root)
}
func (b *treeBridge) RenderFn(uint64, int64, []byte)              {}
func (b *treeBridge) Unmount(uint64)                              {}
func (b *treeBridge) Dispatch(int64, []byte)                      {}
func (b *treeBridge) Call(int64, []byte, func([]byte, error))     {}
func (b *treeBridge) SetRects(map[int64][4]float32)               {}
func (b *treeBridge) SetViewport(float32, float32, float32, bool) {}
func (b *treeBridge) WantsEvent(string) bool                      { return false }

// deliver hands every root rendered so far its component's tree.
func (b *treeBridge) deliver(t *testing.T) {
	t.Helper()
	for name, roots := range b.roots {
		tree, ok := b.trees[name]
		if !ok {
			continue
		}
		for _, r := range roots {
			if err := slackc.Deliver(r, []byte(tree)); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		}
	}
}

// In component mode a play button (an attachment GIF's
// PlayPauseAnimations, animations off) and a spinner (a pending pillow's
// PendingFilesSpinner) draw with their component, MessageAttachment or
// MessageFile: over its fill when the native draws it (they hid under
// it), and not at all when a plugin's tree replaces it (they drew over
// the replacement, from the static pass). A gallery's images aren't a
// component, and their plays stay in the static pass.
func TestPlaysAndSpinnersDrawWithTheirComponent(t *testing.T) {
	uipref.Set(store.Prefs{NoAnimations: true})
	t.Cleanup(uipref.Reset)
	pending := &rt.File{ID: "F0PENDSPIN", Name: "game_scarlett_master.exe", Title: "game_scarlett_master.exe", Type: "exe", Mode: "hosted", Pending: true}
	const gifURL = "https://media.example.test/cat.gif"
	cases := []struct {
		name string
		comp string // the component that draws it
		d    rt.Doc
		// mark is where the play or spinner draws (laid out), and how many
		// of its pixels are its own ink (not the native card's)
		mark func(v *View) image.Rectangle
		ink  func(c, bg color.RGBA) bool
	}{
		{"pending pillow's spinner", "MessageFile", doc(fileBlock(pending)), func(v *View) image.Rectangle {
			if len(v.d.spinners) == 0 {
				return image.Rectangle{}
			}
			return v.d.spinners[0].r
		}, func(c, _ color.RGBA) bool { return int(c.B) > int(c.R)+60 }}, // the arc's blue
		{"attachment GIF's play button", "MessageAttachment", doc(rt.Block{Kind: rt.AttachmentBlock, Att: &rt.Attachment{Image: &rt.Image{URL: gifURL, W: 480, H: 270}}}), func(v *View) image.Rectangle {
			if len(v.d.plays) == 0 {
				return image.Rectangle{}
			}
			mid := v.d.imgs[v.d.plays[0].img].r.Min.Add(v.d.imgs[v.d.plays[0].img].r.Max).Div(2)
			return image.Rect(mid.X-8, mid.Y-8, mid.X+8, mid.Y+8) // the triangle
		}, func(c, _ color.RGBA) bool { return int(c.R)+int(c.G)+int(c.B) < 200 }}, // its dark triangle
	}
	for _, tc := range cases {
		for _, replaced := range []bool{false, true} {
			name := tc.name + "/native"
			if replaced {
				name = tc.name + "/replaced by a plugin"
			}
			t.Run(name, func(t *testing.T) {
				slackc.Styles.Set("test-comp-overlays", ".zz-test-comp-overlays{color:red}") // a theme sheet
				slackc.SetPatched([]string{tc.comp})
				b := &treeBridge{}
				if replaced {
					b.trees = map[string]string{tc.comp: `[{"t":"div","k":1}]`}
				}
				slackc.SetBridge(b)
				t.Cleanup(func() {
					slackc.Styles.Remove("test-comp-overlays")
					slackc.SetPatched(nil)
					slackc.SetBridge(nil)
					slackc.Reset()
				})
				st := newTestStyle()
				st.Images = newGIFImages(gifURL) // the GIF says it animates; drawn clear
				bg := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
				const w, h = 600, 500
				v := New(tc.d, Options{Own: true, Channel: "C0COMPOVL", TS: "1795302540.000200"})
				var ops *op.Ops
				for i := 0; i < 3; i++ {
					ops = new(op.Ops)
					paint.FillShape(ops, bg, clip.Rect{Max: image.Pt(w, h)}.Op())
					gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(w, h))}
					v.Layout(gtx, st, nil)
					b.deliver(t)
				}
				if len(v.d.comps) == 0 {
					t.Fatalf("not in component mode: no %s component", tc.comp)
				}
				r := tc.mark(v)
				if r.Empty() {
					t.Fatal("no play button or spinner laid out")
				}
				img := pixeltest.Render(t, w, h, ops)
				n := 0
				bgc := color.RGBA{R: bg.R, G: bg.G, B: bg.B, A: 0xff}
				for y := r.Min.Y; y < r.Max.Y; y++ {
					for x := r.Min.X; x < r.Max.X; x++ {
						if tc.ink(img.RGBAAt(x, y), bgc) {
							n++
						}
					}
				}
				switch {
				case !replaced && n == 0:
					t.Errorf("no ink of it in %v: hidden under the native MessageFile", r)
				case replaced && n > 0:
					t.Errorf("%d pixels of it in %v over the plugin's replacement of the file", n, r)
				}
			})
		}
	}
}
