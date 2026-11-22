package ui

import (
	"image"
	"runtime"
	"strings"
	"testing"
	"time"
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

// The same for a picker opened from a message's hover toolbar (Add
// reaction…), whose list then isn't laid out again: a notification click
// takes the window to another channel while the picker is up, and the
// picker closes there. The toolbar's popover hold (messages
// toolbar_hold.go) kept the picker until that list laid out again.
func TestClosedEmojiPickerReleasedFromToolbar(t *testing.T) {
	x := newTCEnv(t, "native")
	p := x.hoverBar(func() toolbarLister { return x.cv() }, x.root, barReact)
	x.handClick(p)
	x.until("the emoji picker", 8*time.Second, func() bool {
		return x.openOverlay(func(o Overlay) bool { _, ok := o.(*popups.EmojiPicker); return ok })
	})
	var w weak.Pointer[popups.EmojiPicker]
	var lo, hi uintptr
	for _, o := range x.a.overlays {
		if pk, ok := o.(*popups.EmojiPicker); ok {
			w = weak.Make(pk)
			lo = uintptr(unsafe.Pointer(pk))
			hi = lo + unsafe.Sizeof(*pk)
		}
	}
	if w.Value() == nil {
		t.Fatal("the emoji picker didn't open")
	}
	// another channel: the first one's list isn't laid out again
	other := ""
	for _, id := range x.fs.ChannelIDs() {
		if id != x.ch {
			other = id
			break
		}
	}
	x.a.Navigate("c/" + other)
	x.settle(200 * time.Millisecond)
	if !x.a.dismissTop() {
		t.Fatal("dismissTop closed nothing")
	}
	for i := 0; i < 3; i++ {
		x.frame()
	}
	runtime.GC()
	if w.Value() != nil {
		t.Error("a closed emoji picker opened from the hover toolbar is still reachable three frames after it closed, its channel's list no longer laid out")
		if path := reachPath(x.a, lo, hi, "EmojiPicker"); path != "" {
			t.Errorf("held by App%s", path)
		}
	}
}
