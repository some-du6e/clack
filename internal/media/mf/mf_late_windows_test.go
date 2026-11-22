package mf

import (
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media/mediatest"
)

// A first transfer that comes 1.5 s after the data, as the runners' did
// under load (the first frame 1 to 1.65 s after the engine played, its
// clock running on: TestMFRGBA on train-141, TestMFPlaysFixture on
// menu-click-rerender, 2026-11-21): the frames from then on are right and
// every second after it draws them. The seconds before it drew nothing,
// and the checks counted them, so those tests failed on the runner's
// timing; the first frame's budget after Play (TestMFPlaysFixture) is
// what judges a late start.
func TestMFLateFirstTransfer(t *testing.T) {
	delayFirstTransfer.Store(int64(1500 * time.Millisecond))
	defer delayFirstTransfer.Store(0)
	for _, c := range []struct{ name, format string }{{"BGRA", ""}, {"RGBA", "rgba"}} {
		t.Run(c.name, func(t *testing.T) {
			if c.format != "" {
				t.Setenv("CLACK_MF_FORMAT", c.format)
			}
			r := newRig(t, mediatest.GreyGreyRed, false, image.Pt(320, 180))
			samples, first := r.playThrough()
			if len(samples) == 0 {
				t.Fatal("no frame drawn")
			}
			// the hook held the first transfer while the clock ran
			if samples[0].at < time.Second {
				t.Fatalf("the first frame drawn is at %v: the hook didn't hold it past the first second", samples[0].at)
			}
			t.Logf("first frame %v after Play, at %v; %d frames drawn", first, samples[0].at, len(samples))
			if c.format == "" {
				checkColors(t, samples)
			} else {
				checkRGBA(t, samples)
			}
		})
	}
}
