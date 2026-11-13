package ui

import (
	"image"
	"runtime"
	"testing"
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
	for _, o := range e.a.overlays {
		if p, ok := o.(*popups.EmojiPicker); ok {
			w = weak.Make(p)
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
	}
}
