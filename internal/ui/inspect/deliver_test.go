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
	"strings"
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

// TestDeliveriesInOrderWithText: a capture whose image the clipboard
// refused (it falls back to text) and a newer one copied as an image reach
// the clipboard in the order they were taken (review of a614d3b3: the
// test before this saw the text first through timing alone). The older
// capture is held at the clipboard while the newer one, small, composes
// and waits for it; the clipboard then refuses the older image ("clipboard
// busy"), so its text goes to the window's clipboard, which takes it only
// in a frame. With no frame drawn (1.5 s: the text's handoff waits 3 s at
// most) the newer image waits; once frames come, the text lands, then the
// image.
func TestDeliveriesInOrderWithText(t *testing.T) {
	e := newDragEnv(t)
	e.content = drawPill
	var mu sync.Mutex
	var got []string
	calls := 0
	note := func(s string) {
		mu.Lock()
		got = append(got, s)
		mu.Unlock()
	}
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
	hold := make(chan error)
	e.onClip = func(string) { note("text") }
	WriteImage = func(image.Image) error {
		mu.Lock()
		calls++
		first := calls == 1
		mu.Unlock()
		if first {
			return <-hold // held, then refused
		}
		note("image")
		return nil
	}
	// the older: the primary area, copied at once (Shift)
	big := f32.Pt(700, 300)
	e.startAt(big, "primary")
	e.shiftClick(big, true, true)
	e.frame()
	for dl := time.Now().Add(10 * time.Second); count() < 1; time.Sleep(time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("the older capture never reached the clipboard")
		}
	}
	// the newer: the pill, copied at once, while the older is held
	small := f32.Pt(400, 300)
	e.startAt(small, "Pill")
	e.shiftClick(small, true, true)
	e.frame()
	time.Sleep(300 * time.Millisecond) // the pill's shot (88x68 px at 4x) composes in milliseconds: it waits for the older
	hold <- errors.New("clipboard busy")
	early := 0 // no frame: the older's text can't reach the clipboard, so nor may the newer image
	for dl := time.Now().Add(1500 * time.Millisecond); early == 0 && time.Now().Before(dl); time.Sleep(5 * time.Millisecond) {
		if c := count(); c != 1 {
			early = c
		}
	}
	if early != 0 {
		t.Errorf("the newer capture's image reached the clipboard (%d writes) before a frame took the older one's text", early)
	}
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

// TestNoShotLogsAtOnce: a pick whose shot failed goes out as text at
// once, and its stage-time line says so (review of a614d3b3: it logged an
// Enter nobody pressed, "before the review showed").
func TestNoShotLogsAtOnce(t *testing.T) {
	e := newDragEnv(t)
	e.content = drawPill
	Shoot = func(op.CallOp, image.Rectangle, float32) (*image.RGBA, error) {
		return nil, errors.New("no headless GPU")
	}
	var mu sync.Mutex
	texts := 0
	e.onClip = func(string) { mu.Lock(); texts++; mu.Unlock() }
	got := func() int { mu.Lock(); defer mu.Unlock(); return texts }
	p := f32.Pt(400, 300)
	e.startAt(p, "Pill")
	e.press(p)
	e.frame()
	e.releaseAt(p)
	for dl := time.Now().Add(10 * time.Second); got() == 0; time.Sleep(2 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("the pick with no shot never reached the clipboard as text")
		}
		e.frame()
	}
	e.frame()
	e.tool.Wait()
	if line := LastTimes(); !strings.Contains(line, "; no shot: copied as text at once:") || strings.Contains(line, "Enter") {
		t.Errorf("a pick with no shot logged %q, want it copied as text at once, with no Enter", line)
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
