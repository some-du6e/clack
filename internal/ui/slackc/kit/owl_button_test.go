package kit

// A plugin's Block Kit-style button row, laid out and drawn as Slack does.
// OwlMode (Taut plugins/OwlMode.tsx, SaveButton) renders, under Owl's
// confirmation in its DM or modal:
//
//	div.p-actions_block[data-stringify-ignore] > div.p-actions_block_elements
//	  > div.p-actions_block__action > Button(type outline, size small,
//	    className "p-block_kit_button_element <its class> <its theme class>")
//	      > [SvgIcon(glasses, 16), "Save in Taut"]
//
// and styles the button as a flex row (align-items center, gap 6px) with
// an owl-coloured border and label, fainter once saved (disabled, with a
// Forget button after it). The tests use their own stylesheet with those
// declarations (Taut is GPLv3; its source isn't copied here).
//
// In Slack (client-boot-styles cf726c4f) .p-actions_block and
// .p-actions_block_elements are flex rows and .p-actions_block__action has
// margin 8px 8px 0 0, so each button is as wide as its content. Slack's
// Button (client-boot Clgr) is <button class="c-button c-button--outline
// c-button--small …">: .c-button{display:inline-flex;align-items:center},
// .c-button--small{height:28px;padding:0 12px 1px},
// .c-button--outline{border:1px solid}, then
// .p-block_kit_button_element{padding:0 8px 1px;display:block}. The
// button is 28 px tall and 1 + 8 + 16 + 6 + label + 8 + 1 px wide; the
// icon sits 9 px in, centred in the 25 px content box (28 less the
// borders and the 1 px bottom padding), and the label 6 px after it.
// α.113 stretched the button across the message and put the icon on a
// line of its own above the label: the row had no rules, the button's
// box ignored its CSS, and the children of a flex button were laid out
// as blocks.

import (
	"image"
	"image/color"
	"math"
	"strconv"
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

	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// owlTestCSS is a plugin stylesheet with the declarations OwlMode gives
// its Save in Taut button.
const owlTestCSS = `
.owl-test--demo { --owl-test-rgb: 214, 48, 49; }
body.sk-client-theme--dark .owl-test--demo { --owl-test-rgb: 255, 107, 107; }
.owl-test__save { display: flex; align-items: center; gap: 6px; }
.owl-test__save.c-button--outline { border-color: rgba(var(--owl-test-rgb), 1); color: rgba(var(--owl-test-rgb), 1); }
.owl-test__save.c-button--disabled { border-color: rgba(var(--owl-test-rgb), 0.4); color: rgba(var(--owl-test-rgb), 0.7); }
`

// owl colours: the demo Owl's red in light and dark mode
var (
	owlLight = color.NRGBA{R: 214, G: 48, B: 49, A: 0xff}
	owlDark  = color.NRGBA{R: 255, G: 107, B: 107, A: 0xff}
)

// owlRowJS is the actions row as Clack's renderer delivers it: the save
// button (k 4, its icon k 5), and once saved a Forget button (k 7).
func owlRowJS(saved bool) string {
	label, forget := "Save in Taut", ""
	if saved {
		label = "Saved in Taut"
		forget = `,{"t":"div","k":6,"p":{"class":"p-actions_block__action"},"c":[` +
			`{"t":"@","n":"Button","k":7,"p":{"type":"outline","size":"small","className":"p-block_kit_button_element","onClick":{"$fn":42}},"c":[{"t":"#","v":"Forget"}]}]}`
	}
	return `{"t":"div","k":1,"p":{"class":"p-actions_block","data-stringify-ignore":"true"},"c":[` +
		`{"t":"div","k":2,"p":{"class":"p-actions_block_elements"},"c":[` +
		`{"t":"div","k":3,"p":{"class":"p-actions_block__action"},"c":[` +
		`{"t":"@","n":"Button","k":4,"p":{"type":"outline","size":"small","className":"p-block_kit_button_element owl-test__save owl-test--demo",` +
		`"disabled":` + strconv.FormatBool(saved) + `,"onClick":{"$fn":41}},"c":[` +
		`{"t":"@","n":"SvgIcon","k":5,"p":{"name":"glasses","size":16}},{"t":"#","v":"` + label + `"}]}]}` + forget + `]}]}`
}

// rectLog is a bridge that keeps the last rect JS was told for each node.
type rectLog struct {
	mu    sync.Mutex
	rects map[int64][4]float32
}

func (b *rectLog) Render(uint64, string, []byte)            {}
func (b *rectLog) RenderFn(uint64, int64, []byte)           {}
func (b *rectLog) Unmount(uint64)                           {}
func (b *rectLog) Dispatch(int64, []byte)                   {}
func (b *rectLog) SetViewport(w, h, dpr float32, dark bool) {}
func (b *rectLog) Call(int64, []byte, func([]byte, error))  {}
func (b *rectLog) SetRects(m map[int64][4]float32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.rects == nil {
		b.rects = map[int64][4]float32{}
	}
	for k, r := range m {
		b.rects[k] = r
	}
}

func (b *rectLog) rect(k int64) ([4]float32, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.rects[k]
	return r, ok
}

// owlEnv lays the row out in a 480×120 window, as a window frame does,
// in light or dark mode with the test stylesheet installed.
type owlEnv struct {
	t     *testing.T
	th    *theme.Theme
	log   *rectLog
	v     *slackc.View
	r     input.Router
	ops   op.Ops
	now   time.Time
	nodes []*slackc.Node
}

const owlW, owlH = 480, 120

func newOwlEnv(t *testing.T, dark, saved bool) *owlEnv {
	t.Helper()
	return newOwlEnvTree(t, dark, owlRowJS(saved))
}

// newOwlEnvTree is newOwlEnv with any tree.
func newOwlEnvTree(t *testing.T, dark bool, js string) *owlEnv {
	t.Helper()
	return newOwlEnvFrames(t, dark, js, 4)
}

// newOwlEnvFrames is newOwlEnvTree after frames frames.
func newOwlEnvFrames(t *testing.T, dark bool, js string, frames int) *owlEnv {
	t.Helper()
	Register()
	e := &owlEnv{t: t, th: theme.New(dark), log: &rectLog{}, v: slackc.NewView(905), now: time.Unix(3000, 0)}
	prev := slackc.CurrentEnv()
	slackc.Init(slackc.Env{Theme: e.th})
	slackc.SetBridge(e.log)
	mode := "sk-client-theme--light"
	if dark {
		mode = "sk-client-theme--dark"
	}
	if err := slackc.SetDocumentProps([]byte(`{}`), []byte(`{"className":"`+mode+`"}`)); err != nil {
		t.Fatal(err)
	}
	slackc.SetStyle("plugin:owl-test", owlTestCSS)
	t.Cleanup(func() {
		e.v.Close()
		slackc.RemoveStyle("plugin:owl-test")
		_ = slackc.SetDocumentProps([]byte(`{}`), []byte(`{}`))
		slackc.SetBridge(nil)
		slackc.Init(prev)
	})
	nodes, err := slackc.ParseTree([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	e.nodes = nodes
	for i := 0; i < frames; i++ {
		e.frame()
	}
	return e
}

// frame paints the message background and lays the row out over it.
func (e *owlEnv) frame() {
	slackc.LockFrame()
	defer slackc.UnlockFrame()
	e.ops.Reset()
	e.now = e.now.Add(20 * time.Millisecond)
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(owlW, owlH)),
		Now: e.now, Source: e.r.Source()}
	slackc.Frame(gtx)
	area := clip.Rect{Max: image.Pt(owlW, owlH)}.Push(gtx.Ops)
	paint.ColorOp{Color: e.th.C.Bg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	area.Pop()
	gtx.Constraints.Min = image.Point{}
	e.v.Layout(gtx, e.nodes, "MessageBlocks", nil, nil)
	e.r.Frame(&e.ops)
}

// rect is node k's window rect (x, y, w, h) as JS reads it.
func (e *owlEnv) rect(k int64) [4]float32 {
	e.t.Helper()
	r, ok := e.log.rect(k)
	if !ok {
		e.t.Fatalf("node %d was never laid out", k)
	}
	return r
}

func owlNear(a, b float32) bool { return math.Abs(float64(a-b)) < 0.6 }

// labelW is the label's width in Lato Bold 13px, as the button draws it.
func (e *owlEnv) labelW(s string) float32 {
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Constraints{Max: image.Pt(owlW, owlH)}}
	return float32(e.th.Measure(gtx, unit.Sp(13), theme.Bold, s))
}

func TestOwlSaveButtonLayout(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) {
			e := newOwlEnv(t, dark, false)
			row, elems, action, btn, icon := e.rect(1), e.rect(2), e.rect(3), e.rect(4), e.rect(5)
			want := 1 + 8 + 16 + 6 + e.labelW("Save in Taut") + 8 + 1
			// the button hugs its content: border, 8px sides, icon, 6px gap, label
			if !owlNear(btn[3], 28) {
				t.Errorf("button %vpx tall, want Slack's c-button--small 28", btn[3])
			}
			if math.Abs(float64(btn[2]-want)) > 1.5 {
				t.Errorf("button %vpx wide, want 1+8+16+6+label+8+1 = %.1f (it stretched across the row: %v)", btn[2], want, btn[2] > owlW/2)
			}
			// the action wrapper is the button, 8px below the row's top
			if !owlNear(action[0], btn[0]) || !owlNear(action[2], btn[2]) || !owlNear(btn[1], row[1]+8) || !owlNear(btn[0], row[0]) {
				t.Errorf("action %v, button %v in row %v: want the button at the row's left, 8px down, as wide as its action", action, btn, row)
			}
			// the elements row ends at the action's 8px right margin
			if !owlNear(elems[2], btn[2]+8) {
				t.Errorf("elements row %vpx wide, want the button's %v and its action's 8px margin", elems[2], btn[2])
			}
			// the icon: 9px in (border, padding), centred in the 25px content box
			ix, iy := icon[0]-btn[0], icon[1]-btn[1]
			if !owlNear(ix, 9) || !owlNear(iy, 1+(25-16)/2.0) || !owlNear(icon[2], 16) || !owlNear(icon[3], 16) {
				t.Errorf("icon at %+.1f,%+.1f (%vx%v) in the button, want +9,+5.5 16x16: left of the label, centred", ix, iy, icon[2], icon[3])
			}
		})
	}
}

// The rects JS reads for a Button's children are where the frame that laid
// them out drew them: the icon 9px in from the first frame on, and again
// in the first frame after a new tree (setTree drops the sub scopes, so
// their placement starts over). The children's scope flushes its rects
// while the button lays them out, before it knows where it centres them
// (Host.PlaceChildren), and they stayed at the content box's corner, 9px
// left, until some later frame flushed them again: nothing asks for one,
// so a quiet DM kept them there. JS gets a frame's rects as the next
// frame starts (slackc.Frame), so each check reads them after one more
// frame, before that frame's own rects are sent.
func TestOwlSaveButtonChildRectsSameFrame(t *testing.T) {
	e := newOwlEnvFrames(t, false, owlRowJS(false), 2)
	check := func(when string) {
		t.Helper()
		btn, icon := e.rect(4), e.rect(5)
		if ix, iy := icon[0]-btn[0], icon[1]-btn[1]; !owlNear(ix, 9) || !owlNear(iy, 5.5) {
			t.Errorf("%s: icon at %+.1f,%+.1f in the button, want +9,+5.5", when, ix, iy)
		}
	}
	check("first frame")
	nodes, err := slackc.ParseTree([]byte(owlRowJS(true)))
	if err != nil {
		t.Fatal(err)
	}
	e.nodes = nodes
	e.frame()
	e.frame()
	check("first frame of a new tree")
}

// Once saved, the disabled Saved in Taut button and Forget sit side by
// side, each as wide as its label, 8px apart (the first action's margin).
func TestOwlSaveButtonSavedRow(t *testing.T) {
	e := newOwlEnv(t, false, true)
	save, forget := e.rect(4), e.rect(7)
	if w := 1 + 8 + 16 + 6 + e.labelW("Saved in Taut") + 8 + 1; math.Abs(float64(save[2]-w)) > 1.5 {
		t.Errorf("Saved in Taut %vpx wide, want %.1f", save[2], w)
	}
	if w := 1 + 8 + e.labelW("Forget") + 8 + 1; math.Abs(float64(forget[2]-w)) > 1.5 {
		t.Errorf("Forget %vpx wide, want 1+8+label+8+1 = %.1f", forget[2], w)
	}
	if !owlNear(forget[0], save[0]+save[2]+8) || !owlNear(forget[1], save[1]) || !owlNear(forget[3], 28) {
		t.Errorf("Forget at %v after Saved in Taut at %v: want it 8px to the right on the same line, 28px tall", forget, save)
	}
}

// What the frame draws: the button's border in the owl colour, 28 rows
// tall and as wide as its rect; the icon's ink in its 16px box 9px in; no
// ink in the 6px gap; the label's ink after it; icon and label centred
// on the same line (their ink centres within 2px of the content box's).
func TestOwlSaveButtonPixels(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) {
			e := newOwlEnv(t, dark, false)
			e.frame()
			img := pixeltest.Render(t, owlW, owlH, &e.ops)
			owl, bg := owlLight, e.th.C.Bg
			btnBg := colorsOf(e.th).BtnBg
			if dark {
				owl = owlDark
			}
			r := e.rect(4)
			b := image.Rect(int(r[0]+.5), int(r[1]+.5), int(r[0]+r[2]+.5), int(r[1]+r[3]+.5))
			// the border: owl ink along each edge of the button's rect
			for _, edge := range []image.Rectangle{
				image.Rect(b.Min.X+6, b.Min.Y, b.Max.X-6, b.Min.Y+1), image.Rect(b.Min.X+6, b.Max.Y-1, b.Max.X-6, b.Max.Y),
				image.Rect(b.Min.X, b.Min.Y+6, b.Min.X+1, b.Max.Y-6), image.Rect(b.Max.X-1, b.Min.Y+6, b.Max.X, b.Max.Y-6),
			} {
				if n, all := pixeltest.Ink(img, edge, owl, bg), edge.Dx()*edge.Dy(); n < all*9/10 {
					t.Errorf("border edge %v: %d of %d pixels in the owl colour %v", edge, n, all, owl)
				}
			}
			// nothing in the owl colour outside it
			if n := pixeltest.Ink(img, image.Rect(b.Max.X+2, 0, owlW, owlH), owl, bg); n > 0 {
				t.Errorf("%d owl pixels right of the button: it is wider than its rect", n)
			}
			in := b.Inset(2)
			colInk := func(x int) int { return pixeltest.Ink(img, image.Rect(x, in.Min.Y, x+1, in.Max.Y), owl, btnBg) }
			// icon box: columns 9..25 from the button's left
			icon, gap, text := 0, 0, 0
			for x := b.Min.X + 9; x < b.Min.X+25; x++ {
				icon += colInk(x)
			}
			for x := b.Min.X + 26; x < b.Min.X+30; x++ {
				gap += colInk(x)
			}
			for x := b.Min.X + 31; x < b.Max.X-9; x++ {
				text += colInk(x)
			}
			if icon < 20 || text < 60 || gap > 0 {
				t.Errorf("owl ink: %d in the icon's columns, %d in the 6px gap, %d in the label's: want the icon, a clear gap, then the label", icon, gap, text)
			}
			// vertical: both centred on the content box's middle (1 + 25/2):
			// the middle of each one's ink (the label's caps to its
			// baseline: it has no descenders)
			mid := float64(b.Min.Y) + 1 + 12.5
			ic := inkMidY(img, image.Rect(b.Min.X+9, in.Min.Y, b.Min.X+25, in.Max.Y), owl, btnBg)
			tc := inkMidY(img, image.Rect(b.Min.X+31, in.Min.Y, b.Max.X-9, in.Max.Y), owl, btnBg)
			if math.Abs(ic-mid) > 1.5 || math.Abs(tc-mid) > 1.5 {
				t.Errorf("ink middles: icon %.1f, label %.1f; want both within 1.5px of the content box's middle %.1f", ic, tc, mid)
			}
		})
	}
}

// inkMidY is the middle of the rows of r inked with fg over bg, from the
// top of the first to the bottom of the last (-1 for none).
func inkMidY(img *image.RGBA, r image.Rectangle, fg, bg color.NRGBA) float64 {
	top, bottom := -1, -1
	for y := r.Min.Y; y < r.Max.Y; y++ {
		if pixeltest.Ink(img, image.Rect(r.Min.X, y, r.Max.X, y+1), fg, bg) > 0 {
			if top < 0 {
				top = y
			}
			bottom = y + 1
		}
	}
	if top < 0 {
		return -1
	}
	return float64(top+bottom) / 2
}

// A composer.addButton entry in a pressed state (OwlMode's glasses button
// once a thread replies through Owl: icon glasses-filled, pressed true).
// Taut draws it in Slack's composer as IconButtonBase
// (c-wysiwyg_container__button) in a Tooltip (top, offsetY -7, 500 ms).
// Slack's composer rule makes it 28px with a 4px radius
// (.c-wysiwyg_container .c-wysiwyg_container__button), and no rule styles
// [aria-pressed=true] on it (those are the formatting bar's
// .p-composer__button--sticky and --floating): pressed shows no tile, only
// its icon. Hovered, it takes the composer's own buttons' tile (base-ter,
// #eaeaea in light mode) and, after 500 ms, its label as a tooltip above.
// α.113 drew a pressed tile, a 6px radius and no tooltip.
const owlComposerJS = `{"t":"div","k":20,"p":{"style":{"paddingTop":"80px","paddingLeft":"40px"}},"c":[` +
	`{"t":"@","n":"ComposerButton","k":21,"p":{"id":"owl-test","icon":"glasses-filled","label":"Reply as yourself","pressed":true,` +
	`"tooltip":"Reply as yourself","onClick":{"$fn":44}}}]}`

func TestOwlComposerButtonPixels(t *testing.T) {
	e := newOwlEnvTree(t, false, owlComposerJS)
	r := e.rect(21)
	if !owlNear(r[2], 28) || !owlNear(r[3], 28) {
		t.Errorf("composer button %vx%v, want 28x28 (Slack's composer buttons)", r[2], r[3])
	}
	o := image.Pt(int(r[0]+.5), int(r[1]+.5))
	at := func(x, y int) image.Point { return o.Add(image.Pt(x, y)) }
	bg := e.th.C.Bg
	tile := color.NRGBA{R: 0xea, G: 0xea, B: 0xea, A: 0xff}
	// pressed, not hovered: no tile (its corners and left of its icon)
	img := pixeltest.Render(t, owlW, owlH, &e.ops)
	for _, p := range []image.Point{at(2, 2), at(1, 14), at(26, 25)} {
		if c := img.RGBAAt(p.X, p.Y); !rgbNear(c, bg, 3) {
			t.Errorf("pressed button at %v is %v, want the background %v (Slack has no pressed tile for it)", p, c, bg)
		}
	}
	// hovered: the composer's tile with Slack's 4px radius, then the tip
	ctr := at(14, 14)
	e.r.Queue(pointer.Event{Kind: pointer.Move, Position: f32.Pt(float32(ctr.X), float32(ctr.Y)), Source: pointer.Mouse, Time: time.Second})
	for i := 0; i < 5; i++ {
		e.frame()
	}
	img = pixeltest.Render(t, owlW, owlH, &e.ops)
	for _, p := range []image.Point{at(1, 1), at(1, 14), at(26, 26)} {
		if c := img.RGBAAt(p.X, p.Y); !rgbNear(c, tile, 3) {
			t.Errorf("hovered button at %v is %v, want the composer's hover tile %v (radius 4)", p, c, tile)
		}
	}
	tip := colorsOf(e.th).Tooltip
	above := image.Rect(0, 0, owlW, o.Y)
	if n := pixeltest.Count(img, above, tip, 4); n > 0 {
		t.Errorf("%d tooltip px within 100 ms of hovering: Taut's delay is 500", n)
	}
	for i := 0; i < 30; i++ { // 600 ms
		e.frame()
	}
	img = pixeltest.Render(t, owlW, owlH, &e.ops)
	if n := pixeltest.Count(img, above, tip, 4); n < 300 {
		t.Errorf("%d tooltip px above the button after 600 ms of hovering: want the label's tooltip", n)
	}
	if n := pixeltest.Count(img, image.Rect(0, o.Y+28, owlW, owlH), tip, 4); n > 0 {
		t.Errorf("%d tooltip px below the button: want it above (position top)", n)
	}
}

// rgbNear reports c within tol of want on every channel.
func rgbNear(c color.RGBA, want color.NRGBA, tol int) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(c.R, want.R) <= tol && d(c.G, want.G) <= tol && d(c.B, want.B) <= tol
}
