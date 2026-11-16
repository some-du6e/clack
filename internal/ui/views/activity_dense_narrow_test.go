package views

// The Dense row in a narrow list (α.89: a 320px Activity list beside an
// open thread drew "[avatar] … [✓] [thread] You:  #clac…" under the hover
// bar). Slack's ActivityRowContentSingleLine (build c07ab895506f, CSS
// activity_single_line_row_content*): under 625px the list is
// --super_narrow_dense (ActivityList eH) and the row takes two lines, the
// message across the first and the location · time under it; the leading
// block is 152px (100px in a container 300px or narrower), flex-shrink 0,
// the sender the part in it that gives way; the status and type icon never
// shrink. At 625px and up the row is one 50px line (200px leading; the
// message gives way, the location and the 54px time keep their widths).
// Read and hovered, light and dark, at 320, 400, 600 and 900px.
// Synthetic content only (internal/fakeslack's feed).

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestActivityDenseNarrowLikeSlack(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(map[bool]string{false: "light", true: "dark"}[dark], func(t *testing.T) { actDenseNarrow(t, dark) })
	}
}

func actDenseNarrow(t *testing.T, dark bool) {
	v, _, e, _ := vaFakeActivity(t, dark)
	v.dense, v.rows, v.dirty = true, map[string]*actRow{}, true
	setW := func(w int) {
		e.size = image.Pt(w, 900)
		for _, r := range v.rows {
			r.dn = actDenseBox{}
		}
		for range 3 {
			e.frame()
		}
	}
	setW(320)
	// a read row with the mark, the type icon and a location: all the
	// parts of the leading block and the meta line
	key := ""
	for _, i := range v.vis {
		if i < 0 {
			continue
		}
		k := actKey(&actPage(v).Items[i])
		if r := v.rows[k]; r != nil && r.dn.h > 0 && r.markOK && r.d.typeIcon != "" && r.d.locOn && r.d.teamIcon == "" {
			key = k
			break
		}
	}
	if key == "" {
		t.Fatal("no drawn read row with a mark, a type icon and a location in the fake feed")
	}
	var fixedStatus, fixedType int
	for _, w := range []int{320, 400, 600, 900} {
		setW(w)
		r := v.rows[key]
		if r == nil || r.dn.h == 0 {
			t.Fatalf("%dpx: %s not drawn", w, key)
		}
		cw := w - 40 // the grouped container's 20px sides
		narrow := w < 625
		if v.superNarrow != narrow {
			t.Errorf("%dpx: superNarrow %v, want %v", w, v.superNarrow, narrow)
		}
		lead := 0
		switch {
		case cw <= 300:
			lead = 100
		case narrow:
			lead = 152
		case cw >= 800:
			lead = 200
		default:
			lead = 300 - v.denseLoc
		}
		dn := r.dn
		if dn.lead != lead {
			t.Errorf("%dpx: leading %d, want %d", w, dn.lead, lead)
		}
		// the status and type icon keep their size at every width
		if fixedStatus == 0 {
			fixedStatus, fixedType = dn.status, dn.typeIcon
			if dn.status != 8+24 || dn.typeIcon != 8+20 {
				t.Errorf("%dpx: status %d, type icon %d; want 32 (8px, the 24px mark slot) and 28 (8px, 20px)", w, dn.status, dn.typeIcon)
			}
		} else if dn.status != fixedStatus || dn.typeIcon != fixedType {
			t.Errorf("%dpx: status %d / type icon %d changed from %d / %d", w, dn.status, dn.typeIcon, fixedStatus, fixedType)
		}
		// the sender has what's left of the leading block
		if want := lead - 20 - 8 - dn.status - dn.typeIcon; dn.name != want {
			t.Errorf("%dpx: sender room %d, want %d", w, dn.name, want)
		}
		if narrow {
			// the message has the whole centre; the meta line has the
			// location (at most 175px, giving way) and the whole time
			if want := cw - lead - 8; dn.msg != want {
				t.Errorf("%dpx: message %d, want the centre's %d", w, dn.msg, want)
			}
			if dn.loc > 175 || dn.loc+dn.time > dn.msg || dn.time <= 8 {
				t.Errorf("%dpx: location %d, time %d in a %dpx line", w, dn.loc, dn.time, dn.msg)
			}
			top, bottom := 0, 0
			if r.snTop {
				top = 1
			}
			if r.snBottom {
				bottom = 1
			}
			// padding 10px, the centre's 1 + 22 + 2 + 18px, the borders
			if want := 20 + 43 + top + bottom; dn.h != want || dn.lines != 2 {
				t.Errorf("%dpx: height %d on %d lines, want %d on 2", w, dn.h, dn.lines, want)
			}
		} else {
			gap := 16
			if cw <= 625 {
				gap = 4
			}
			loc := v.denseLoc
			if cw >= 800 {
				loc = 224
			}
			if want := cw - lead - 8 - gap - loc - gap - 54; dn.msg != want || dn.loc != loc || dn.time != 54 {
				t.Errorf("%dpx: message %d / location %d / time %d, want %d / %d / 54", w, dn.msg, dn.loc, dn.time, want, loc)
			}
			if dn.h != 50 || dn.lines != 1 {
				t.Errorf("%dpx: height %d on %d lines, want 50 on 1", w, dn.h, dn.lines)
			}
		}
		// every drawn Dense row has the same height (borders aside)
		for k, o := range v.rows {
			if o.dn.h == 0 {
				continue
			}
			want := 50
			if narrow {
				want = 63
				if o.snTop {
					want++
				}
				if o.snBottom {
					want++
				}
			}
			if o.dn.h != want {
				t.Errorf("%dpx: row %s %dpx tall, want %d", w, k, o.dn.h, want)
			}
		}
		if err := actDenseShot(t, e, fmt.Sprintf("activity-dense-%d", w), dark, r, -1); err != nil {
			t.Error(err)
		}

		// hovered: the bar sits over the text, centred on the content box;
		// the parts keep their boxes (the super narrow time stays)
		top := actDenseHover(t, e, v, key)
		h := v.rows[key]
		if h.barY != h.denseMid-20 {
			t.Errorf("%dpx: hover bar at %d, want %d (the content box's middle %d - 20)", w, h.barY, h.denseMid-20, h.denseMid)
		}
		if h.dn != dn {
			t.Errorf("%dpx: hovering changed the row's layout\n  read    %+v\n  hovered %+v", w, dn, h.dn)
		}
		if err := actDenseShot(t, e, fmt.Sprintf("activity-dense-%d-hover", w), dark, h, top); err != nil {
			t.Error(err)
		}
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(w)/2, 2)})
		e.frame()
	}
}

// actDenseHover moves the pointer down the list until the row is hovered
// and returns the row's top (the first y over it).
func actDenseHover(t *testing.T, e *vaEnv, v *Activity, key string) int {
	t.Helper()
	x := float32(e.size.X) / 3
	move := func(y int) bool {
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(x, float32(y)+0.5)})
		e.frame()
		r := v.rows[key]
		return r != nil && r.click.Hovered()
	}
	for y := 0; y < e.size.Y; y += 8 {
		if !move(y) {
			continue
		}
		top := y
		for top > 0 && move(top-1) {
			top--
		}
		move(top + 8)
		e.frame()
		return top
	}
	t.Fatalf("%dpx: never hovered %s", e.size.X, key)
	return 0
}

// actDenseShot saves the frame (VIEWS_SHOTS_DIR, the activity-shots
// workflow) and, given the row's top, checks its super narrow borders: the
// 1px otl-ter line at its top and bottom, the row's fill just inside.
func actDenseShot(t *testing.T, e *vaEnv, name string, dark bool, r *actRow, top int) error {
	t.Helper()
	dir := os.Getenv("VIEWS_SHOTS_DIR")
	if dir == "" {
		return nil
	}
	win, err := pixeltest.NewWindow(e.size.X, e.size.Y)
	if err != nil {
		return fmt.Errorf("headless window: %v", err)
	}
	defer win.Release()
	for range 3 {
		e.frame()
	}
	if err := win.Frame(&e.ops); err != nil {
		return fmt.Errorf("frame: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := win.Screenshot(img); err != nil {
		return fmt.Errorf("screenshot: %v", err)
	}
	mode := map[bool]string{false: "light", true: "dark"}[dark]
	f, err := os.Create(filepath.Join(dir, name+"-"+mode+".png"))
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	t.Logf("saved %s", f.Name())
	if top < 0 || !r.v.superNarrow {
		return nil
	}
	near := func(a color.RGBA, b color.NRGBA) bool {
		d := func(x, y uint8) bool { return int(x)-int(y) <= 2 && int(y)-int(x) <= 2 }
		return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B)
	}
	border := r.v.p.Border
	px := func(y int) color.RGBA { return img.RGBAAt(8, y) } // the 20px side padding: no text there
	if r.snTop && !near(px(top), border) {
		return fmt.Errorf("%s: no top border at y %d: %v, want %v", name, top, px(top), border)
	}
	if b := top + r.dn.h - 1; r.snBottom && !near(px(b), border) {
		return fmt.Errorf("%s: no bottom border at y %d: %v, want %v", name, b, px(b), border)
	}
	if in := px(top + 5); near(in, border) {
		return fmt.Errorf("%s: the row's fill at y %d is the border colour %v", name, top+5, in)
	}
	return nil
}
