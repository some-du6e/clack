package composer

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/localimg"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// lightboxHost records the image viewer's openings.
type lightboxHost struct {
	slickHost
	opened   []richtext.Image
	overlays []popups.Overlay
}

func (h *lightboxHost) OpenLightbox(img richtext.Image, _ core.Message) {
	h.opened = append(h.opened, img)
}

// ShowOverlay records the pending file viewer (popups.NewPendingViewer)
// with the image it shows.
func (h *lightboxHost) ShowOverlay(o popups.Overlay) {
	h.overlays = append(h.overlays, o)
	if lb, ok := o.(*popups.Lightbox); ok {
		h.opened = append(h.opened, lb.Image())
	}
}

// Karim, 2026-11-10: "you can't click this little image in the composer".
// Slack's attached image is PendingImageFile (boot-shared SYnk): a click
// opens the pending file viewer on it (openPendingFileViewerDispatch with
// its size). Clack's tile opens the image viewer from the file's bytes
// (memory only), which go when the file leaves the composer; the remove
// button on the tile still only removes.
func TestAttachedImageOpensViewer(t *testing.T) {
	h := &lightboxHost{slickHost: slickHost{th: theme.New(false)}}
	a := newAttachments(h)
	a.Add("screenshot.png", imgSynthPNG(t, 480, 300))
	var r input.Router
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Constraints: layout.Constraints{Max: image.Pt(600, 300)}, Now: time.Now(), Source: r.Source()}
		a.Layout(gtx)
		r.Frame(ops)
	}
	click := func(p image.Point) {
		pt := f32.Pt(float32(p.X), float32(p.Y))
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt})
		frame()
		frame()
	}
	frame()
	frame()
	// the 62px tile sits 12px into the gallery
	click(image.Pt(12+attChipH/2, 12+attChipH/2))
	if len(h.opened) != 1 {
		t.Fatalf("a click on the image tile opened %d viewers, want 1", len(h.opened))
	}
	img := h.opened[0]
	if !localimg.Is(img.URL) || img.W != 480 || img.H != 300 || img.Title != "screenshot.png" {
		t.Errorf("viewer image %+v, want the file's own bytes (clack-pending:), 480×300, titled screenshot.png", img)
	}
	if _, ok := localimg.Get(img.URL); !ok {
		t.Errorf("the viewer's image isn't loadable")
	}
	a.mu.Lock()
	c := a.chips[0]
	a.mu.Unlock()
	a.remove(c)
	if _, ok := localimg.Get(img.URL); ok {
		t.Errorf("the file's bytes stayed after it was removed")
	}
}
