package inspect

// Two captures on their way to the clipboard at once (review of a8fa243c):
// each one's delivery ran on a goroutine of its own, so a big capture's
// (0.4-0.7 s: its colours, image and PNG) could finish after a small one
// taken after it and put the older image on the clipboard last, and two
// captures in the same second wrote the same files.

import (
	"bytes"
	"image"
	"os"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
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
