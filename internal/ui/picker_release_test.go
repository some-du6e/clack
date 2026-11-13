package ui

import (
	"image"
	"runtime"
	"strings"
	"testing"
	"unsafe"
	"weak"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// A closed emoji picker can be collected a few frames after it closes: the
// full index it holds (10 MB at Hack Club scale) goes 5 minutes after its
// last use only if nothing still reaches the picker. The popups stacking
// order (popups.ForgetDismissed) and Gio's router (patch 18: a frame's
// buffers no longer keep an earlier frame's tags) both did.
func TestClosedEmojiPickerReleased(t *testing.T) {
	e := newActShell(t)
	e.frames(2)
	e.a.OpenEmojiPicker(image.Pt(700, 700), func(string) {})
	e.frames(3)
	var w weak.Pointer[popups.EmojiPicker]
	var lo, hi uintptr // where it is, for reachPath (not a reference)
	for _, o := range e.a.overlays {
		if p, ok := o.(*popups.EmojiPicker); ok {
			w = weak.Make(p)
			lo = uintptr(unsafe.Pointer(p))
			hi = lo + unsafe.Sizeof(*p)
		}
	}
	if w.Value() == nil {
		t.Fatal("the emoji picker didn't open")
	}
	if !e.a.dismissTop() {
		t.Fatal("dismissTop closed nothing")
	}
	e.frames(3)
	runtime.GC()
	if w.Value() != nil {
		t.Error("a closed emoji picker is still reachable three frames after it closed")
		for _, r := range []struct {
			name string
			v    any
		}{{"App", e.a}, {"router", &e.rt}, {"ops", &e.ops}, {"core", e.ca}} {
			if path := reachPath(r.v, lo, hi, "EmojiPicker"); path != "" {
				t.Errorf("held by %s%s", r.name, path)
			}
		}
		buf := make([]byte, 1<<20)
		buf = buf[:runtime.Stack(buf, true)]
		for _, g := range strings.Split(string(buf), "\n\n") {
			if strings.Contains(g, "popups.") || strings.Contains(g, "EmojiPicker") {
				t.Logf("goroutine in popups:\n%s", g)
			}
		}
	}
}
