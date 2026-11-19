package inspect

// Two captures on their way to the clipboard at once (review of a8fa243c):
// each one's delivery ran on a goroutine of its own, so a big capture's
// (0.4-0.7 s: its colours, image and PNG) could finish after a small one
// taken after it and put the older image on the clipboard last, and two
// captures in the same second wrote the same files.

import (
	"bytes"
	"errors"
	"image"
	"os"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/op"
)

// TestDeliveriesInOrder: a capture taken while an earlier one is still
// being copied reaches the clipboard after it, so the newest one is what
// the clipboard holds.
func TestDeliveriesInOrder(t *testing.T) {
	e := newDragEnv(t)
	var mu sync.Mutex
	var order []image.Point
	calls := 0
	hold := make(chan struct{})
	WriteImage = func(img image.Image) error {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if first {
			<-hold
		}
		mu.Lock()
		order = append(order, img.Bounds().Size())
		mu.Unlock()
		return nil
	}
	count := func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return calls, len(order)
	}
	// the primary area, copied at once (Shift), held at the clipboard
	big := f32.Pt(400, 300)
	e.startAt(big, "primary")
	e.shiftClick(big, true, true)
	e.frame()
	for dl := time.Now().Add(10 * time.Second); ; time.Sleep(time.Millisecond) {
		if c, _ := count(); c == 1 {
			break
		}
		if time.Now().After(dl) {
			close(hold)
			t.Fatal("the first capture never reached the clipboard")
		}
	}
	// the sidebar, smaller, while the first is still being copied
	small := f32.Pt(100, 300)
	e.startAt(small, "sidebar")
	e.shiftClick(small, true, true)
	e.frame()
	time.Sleep(300 * time.Millisecond)
	if c, _ := count(); c != 1 {
		t.Errorf("the second capture reached the clipboard (%d writes) while the first was still being copied", c)
	}
	close(hold)
	for dl := time.Now().Add(10 * time.Second); ; time.Sleep(time.Millisecond) {
		if _, n := count(); n == 2 {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("both captures never reached the clipboard")
		}
	}
	e.tool.Wait()
	mu.Lock()
	defer mu.Unlock()
	a, b := order[0], order[1]
	if a.X*a.Y <= b.X*b.Y {
		t.Errorf("clipboard writes %v then %v: the smaller (newer) capture's image went first, the older one's last", a, b)
	}
}

// TestDeliveriesInOrderWithText: a capture that falls back to text (its
// shot failed) and one copied as an image right after it reach the
// clipboard in the order they were taken (review of 3a75558d: the text
// went through the host's CopyText, which took it a frame or two later,
// so the newer image could land first and the older text last). The text
// goes to the window's clipboard first thing in a frame, and the image
// waits until a frame has passed since.
func TestDeliveriesInOrderWithText(t *testing.T) {
	e := newDragEnv(t)
	var mu sync.Mutex
	var got []string
	note := func(s string) {
		mu.Lock()
		got = append(got, s)
		mu.Unlock()
	}
	e.onClip = func(string) { note("text") }
	fail := true
	Shoot = func(_ op.CallOp, region image.Rectangle, k float32) (*image.RGBA, error) {
		if fail {
			fail = false
			return nil, errors.New("no headless GPU")
		}
		return noiseShot(ShotSize(region, k), 1), nil
	}
	WriteImage = func(image.Image) error {
		note("image")
		return nil
	}
	p := f32.Pt(400, 300)
	e.startAt(p, "primary")
	e.shiftClick(p, true, true) // copied at once; no shot: as text
	e.frame()
	e.startAt(p, "primary")
	e.shiftClick(p, true, true) // copied at once, as an image
	e.frame()
	for dl := time.Now().Add(10 * time.Second); time.Now().Before(dl); {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 2 {
			break
		}
		e.frame()
		time.Sleep(2 * time.Millisecond)
	}
	e.tool.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != "text" || got[1] != "image" {
		t.Errorf("the clipboard took %q, want the older capture's text, then the newer one's image", got)
	}
}

// TestSaveUniqueStems: two reports saved in the same second keep a stem
// each, every image beside its own text, and their names sort in the
// order they were saved (what lists the folder by name, as the ui tests'
// readReport does, finds the newest last).
func TestSaveUniqueStems(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 11, 19, 5, 14, 34, 120e6, time.UTC)
	a, err := save(dir, when, []byte("image a"), "text a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := save(dir, when.Add(300*time.Millisecond), []byte("image b"), "text b")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("two reports in one second saved as one stem %q", a)
	}
	if !(a+".txt" < b+".txt") || !(a+".png" < b+".png") {
		t.Errorf("the second report's files (%s) sort before the first's (%s)", b, a)
	}
	for _, c := range []struct{ stem, img, txt string }{{a, "image a", "text a"}, {b, "image b", "text b"}} {
		img, err1 := os.ReadFile(c.stem + ".png")
		txt, err2 := os.ReadFile(c.stem + ".txt")
		if err1 != nil || err2 != nil || !bytes.Equal(img, []byte(c.img)) || string(txt) != c.txt {
			t.Errorf("%s: image %q (%v), text %q (%v); want %q, %q", c.stem, img, err1, txt, err2, c.img, c.txt)
		}
	}
}
