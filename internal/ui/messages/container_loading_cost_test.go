package messages

// The uv-loader GIF (MessageContainerLoading's, 400x400, about 100 frames
// of mostly 20 ms) frames at its own rate while a view's first load is
// out. Each frame is composed and scaled once at the drawn size and kept:
// a loop drawn again allocates next to nothing, where it made a new
// 640 KB image (and a texture upload) every GIF frame.

import (
	"image"
	"runtime"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestContainerLoadingFrameCost(t *testing.T) {
	prev := gifKeep
	gifKeep = time.Hour // the test's own pace can't drop the kept frames
	t.Cleanup(func() { gifKeep = prev })
	th := theme.New(false)
	start := time.Now()
	var ops op.Ops
	frames := 0
	frame := func(now time.Time) {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(800, 600))}
		MessageContainerLoading(gtx, th, image.Pt(800, 600), "", start)
		frames++
	}
	p := uvLoader(false)
	if p.g == nil {
		t.Fatal("control: the uv-loader GIF didn't decode")
	}
	for d := time.Duration(0); d < p.total; d += 10 * time.Millisecond { // one loop: every frame composed
		frame(start.Add(d))
	}
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	n := frames
	for d := p.total; d < 2*p.total; d += 10 * time.Millisecond {
		frame(start.Add(d))
	}
	runtime.ReadMemStats(&m1)
	n = frames - n
	if got := m1.TotalAlloc - m0.TotalAlloc; got > 4<<20 {
		t.Errorf("a second loop of %d frames allocated %d KB, want the kept frames re-used (a 640 KB image each frame before)", n, got>>10)
	}
}
