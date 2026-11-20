package ui

// The pick-to-review path on the whole client (Karim, 2026-11-18, α.108 on
// Windows, a 1918x1080 window at 1x: "this shit takes to long between a
// selection and the blur page"; the banner "Blur: preparing the shot…" up
// long after the pick). At 1918x1080 dp, at 1x and 2x, a plain click on
// the reaction pill, a click on a component widened with ↑ to most of the
// message pane, a region dragged over most of the window, and a click on a
// frozen frame each draw their review's shot in the frame after the pick's
// (frames as App.Run runs them: the frame, then AfterFrame). Each
// capture's stage times are logged (inspect: capture …), Enter's delivery
// included. With CLACK_PIXEL_TESTS=1 the shots are the headless GPU's
// (Mesa's llvmpipe on Linux, WARP on Windows); without, noise as big.

import (
	"fmt"
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/inspect"
)

// frameShot is a frame as App.Run's: laid out (or drawn frozen), then the
// pick's shot (inspectAfterFrame).
func (e *perfEnv) frameShot() {
	e.frame()
	ops := &e.ops
	if e.frozen != nil {
		ops = e.frozen
	}
	e.a.inspectAfterFrame(ops, e.size)
}

// noiseShots replaces the headless capture with noise as big as its shot
// (no GPU): every pixel differs from its neighbours, as a shot of text does
// at that density.
func noiseShots(t *testing.T) {
	prev := inspect.Shoot
	inspect.Shoot = func(_ op.CallOp, region image.Rectangle, k float32) (*image.RGBA, error) {
		img := image.NewRGBA(image.Rectangle{Max: inspect.ShotSize(region, k)})
		for i := range img.Pix {
			if i%4 == 3 {
				img.Pix[i] = 0xff
			} else {
				img.Pix[i] = uint8(i*37 + i/4099*11)
			}
		}
		return img, nil
	}
	t.Cleanup(func() { inspect.Shoot = prev })
}

func TestInspectReviewTiming(t *testing.T) {
	gpu := os.Getenv("CLACK_PIXEL_TESTS") != ""
	for _, sc := range []float32{1, 2} {
		t.Run(fmt.Sprintf("%gx", sc), func(t *testing.T) {
			if !gpu {
				noiseShots(t)
			}
			prevWrite := inspect.WriteImage
			inspect.WriteImage = func(image.Image) error { return nil } // no clipboard on CI
			t.Cleanup(func() { inspect.WriteImage = prevWrite })
			e := newInspectEnv(t)
			e.scale = sc
			e.size = image.Pt(int(1918*sc), int(1080*sc))
			e.settle(500 * time.Millisecond)

			capture := func(name string) {
				t.Helper()
				t0 := time.Now()
				e.frameShot() // the pick's frame, then its shot
				if !e.a.insp.Reviewing() {
					t.Errorf("%s: no review after the pick", name)
					return
				}
				n := 0
				for i := 1; i <= 2 && n == 0; i++ {
					e.frameShot()
					if e.a.insp.ShotDrawn() {
						n = i
					}
				}
				if n == 0 {
					// how long the review took after all
					for dl := time.Now().Add(60 * time.Second); !e.a.insp.ShotDrawn() && time.Now().Before(dl); {
						time.Sleep(2 * time.Millisecond)
						e.frameShot()
					}
					t.Errorf("%s at %gx: the review's shot isn't drawn within 2 frames of the pick (drawn %v after it)", name, sc, time.Since(t0).Round(time.Millisecond))
				}
				wall := time.Since(t0)
				e.settle(450 * time.Millisecond) // a region's review takes Enter once on screen a while
				e.key(key.NameReturn, 0)
				e.frameShot()
				e.a.insp.Wait()
				e.frame()
				line := inspect.LastTimes()
				t.Logf("%s at %gx: review drawn in frame %d after the pick's, %v after it; %s", name, sc, n, wall.Round(100*time.Microsecond), line)
				if n > 1 {
					t.Errorf("%s at %gx: the review's shot drawn in frame %d after the pick's, want the first", name, sc, n)
				}
				// the session's GPU, made as it started, did the shot: a
				// context made for the shot (seconds, the first on llvmpipe)
				// says "gpu context"
				if gpu && !strings.Contains(line, "gpu ready") {
					t.Errorf("%s at %gx: the shot didn't use the session's GPU: %s", name, sc, line)
				}
			}

			// the reaction pill
			e.startSession(t)
			pc := e.pillCenter()
			pill := image.Pt(int(pc.X), int(pc.Y))
			e.click(pill)
			capture("the reaction pill")

			// a component widened with ↑ from the pill as far as it goes, or
			// to a quarter of the window (an ancestor as big as the one
			// inside it is a step too)
			e.startSession(t)
			label, r, _ := e.a.insp.Hovered()
			for range 24 {
				if r.Dx()*r.Dy()*4 >= e.size.X*e.size.Y {
					break
				}
				e.key(key.NameUpArrow, 0)
				e.frame()
				nl, nr, ok := e.a.insp.Hovered()
				if !ok || nl == label && nr == r {
					break // the outermost
				}
				label, r = nl, nr
			}
			t.Logf("widened to %q %v", label, r)
			e.click(pill)
			capture("a component (" + label + ")")

			// a region dragged over most of the window
			e.startSession(t)
			d := func(v int) int { return int(float32(v) * sc) }
			from, to := image.Pt(d(40), d(60)), image.Pt(e.size.X-d(40), e.size.Y-d(40))
			e.drag(from, image.Pt((from.X+to.X)/2, (from.Y+to.Y)/2), to)
			e.release(to)
			capture("a region")

			// a click on a frozen frame
			e.startSession(t)
			e.key(inspect.FreezeKey, 0)
			e.frame()
			if !e.a.insp.Frozen() {
				t.Fatal("F didn't freeze")
			}
			e.click(pill)
			capture("the pill, frozen")
		})
	}
}
