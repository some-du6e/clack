package inspect

// The shots' GPU (shooter.go) with a stand-in for Gio's headless window. A
// session's window is made as the session starts, off the UI goroutine,
// and nothing on the UI goroutine waits for that (review of a8fa243c:
// setWanted took the lock reconcile held while it made or let go of the
// window, so ending a session could freeze the window on a slow driver
// and hang it on a stuck one). A session takes one shot, and its window
// is let go of once the shot is read back, not kept through the review
// (~31 MB of framebuffer and caches for a full-window shot).

import (
	"errors"
	"image"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/op"
)

type fakeShotWin struct {
	mu        sync.Mutex
	size      image.Point
	frames    int
	released  int
	failFrame bool
}

func (w *fakeShotWin) SetSize(x, y int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.size = image.Pt(x, y)
	return nil
}

func (w *fakeShotWin) Frame(*op.Ops) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.frames++
	if w.failFrame {
		return errors.New("device lost")
	}
	return nil
}

func (w *fakeShotWin) Screenshot(img *image.RGBA) error {
	copy(img.Pix, noiseShot(img.Bounds().Size(), 3).Pix)
	return nil
}

func (w *fakeShotWin) Release() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.released++
}

func (w *fakeShotWin) counts() (frames, released int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.frames, w.released
}

// fakeWindows makes the shots' windows fakes; before each one is made
// hook runs with its number (from 0) and can hold it up or fail it.
type fakeWindows struct {
	mu    sync.Mutex
	asked int
	wins  []*fakeShotWin
}

func useFakeWindows(t *testing.T, hook func(n int) error) *fakeWindows {
	t.Helper()
	shots.settle()
	f := &fakeWindows{}
	prev := newShotWindow
	newShotWindow = func(x, y int) (shotWindow, error) {
		f.mu.Lock()
		n := f.asked
		f.asked++
		f.mu.Unlock()
		if hook != nil {
			if err := hook(n); err != nil {
				return nil, err
			}
		}
		w := &fakeShotWin{size: image.Pt(x, y)}
		f.mu.Lock()
		f.wins = append(f.wins, w)
		f.mu.Unlock()
		return w, nil
	}
	t.Cleanup(func() {
		shots.wantMu.Lock()
		shots.wanters = nil
		shots.wantMu.Unlock()
		shots.setWanted(false)
		shots.settle()
		newShotWindow = prev
	})
	return f
}

func (f *fakeWindows) made() []*fakeShotWin {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*fakeShotWin(nil), f.wins...)
}

// live is how many of the windows made weren't let go of.
func (f *fakeWindows) live() int {
	n := 0
	for _, w := range f.made() {
		if _, r := w.counts(); r == 0 {
			n++
		}
	}
	return n
}

// TestShooterSetWantedNeverWaits: a session ending while its window is
// still being made (or let go of) doesn't wait for that: setWanted
// returns at once, and the window, once made, is let go of.
func TestShooterSetWantedNeverWaits(t *testing.T) {
	hold := make(chan struct{})
	making := make(chan struct{}, 1)
	f := useFakeWindows(t, func(n int) error {
		if n == 0 {
			making <- struct{}{}
			<-hold
		}
		return nil
	})
	shots.setWanted(true)
	select {
	case <-making:
	case <-time.After(10 * time.Second):
		t.Fatal("the session's window was never asked for")
	}
	done := make(chan struct{})
	t0 := time.Now()
	go func() {
		shots.setWanted(false) // the session's end, on the UI goroutine
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Errorf("ending the session waited for the window being made (still waiting after %v)", time.Since(t0).Round(time.Millisecond))
	}
	close(hold)
	<-done
	shots.settle()
	if n := f.live(); n != 0 || len(f.made()) != 1 {
		t.Errorf("after the session ended: %d of %d windows made are live, want none", n, len(f.made()))
	}
}

// TestShooterStartStopStart: sessions started and ended in quick
// succession end as the last one asked: one window, live while a session
// runs, none after.
func TestShooterStartStopStart(t *testing.T) {
	f := useFakeWindows(t, func(int) error { time.Sleep(15 * time.Millisecond); return nil })
	for range 3 {
		shots.setWanted(true)
		shots.setWanted(false)
	}
	shots.setWanted(true)
	shots.settle()
	if n := f.live(); n != 1 {
		t.Errorf("a session running after start, stop, start…: %d live windows (%d made), want 1", n, len(f.made()))
	}
	shots.setWanted(false)
	shots.settle()
	if n := f.live(); n != 0 {
		t.Errorf("no session: %d live windows, want none", n)
	}
}

// TestShooterReleasesAfterShot: a session's pick is shot through the
// window made as it started, and that window is let go of once the shot
// is read back, while the review is still up (a session never shoots
// twice).
func TestShooterReleasesAfterShot(t *testing.T) {
	f := useFakeWindows(t, nil)
	e := newDragEnv(t)
	Shoot = shootHeadless // newDragEnv's cleanup puts its own back
	p := f32.Pt(400, 300)
	e.startAt(p, "primary")
	shots.settle()
	if n := len(f.made()); n != 1 {
		t.Fatalf("a session start made %d windows, want 1", n)
	}
	e.shiftClick(p, false, false)
	e.frame()
	if !e.tool.Reviewing() {
		t.Fatal("no review after the click")
	}
	w := f.made()[0]
	shots.settle()
	frames, released := w.counts()
	if frames != 1 || released != 1 || !e.tool.Reviewing() {
		t.Errorf("the review up after the shot: the session's window drew %d frames, let go of %d times (reviewing %v); want one frame, let go of once", frames, released, e.tool.Reviewing())
	}
	e.key(key.NameEscape, 0)
	e.frame()
	shots.settle()
	if _, released := w.counts(); released != 1 || len(f.made()) != 1 {
		t.Errorf("after the review: let go of %d times, %d windows made; want once, one", released, len(f.made()))
	}
}

// TestShooterTwoSessions: two windows' sessions at once (each of Clack's
// windows runs a tool of its own: the main window's, the huddle
// window's): the first one's shot keeps the window the other's session
// wants, the other's shot goes through it, and after both none is live.
func TestShooterTwoSessions(t *testing.T) {
	f := useFakeWindows(t, nil)
	a, b := newDragEnv(t), newDragEnv(t)
	Shoot = shootHeadless // the envs' cleanups put their own back
	p := f32.Pt(400, 300)
	a.startAt(p, "primary")
	b.startAt(p, "primary")
	shots.settle()
	if n := len(f.made()); n != 1 {
		t.Fatalf("two sessions made %d windows, want one", n)
	}
	a.shiftClick(p, false, false)
	a.frame()
	shots.settle()
	if !a.tool.Reviewing() || f.live() != 1 {
		t.Fatalf("after one session's shot: reviewing %v, %d live windows; want the review, the other session's window kept", a.tool.Reviewing(), f.live())
	}
	b.shiftClick(p, false, false)
	b.frame()
	shots.settle()
	if !b.tool.Reviewing() || len(f.made()) != 1 || f.live() != 0 {
		t.Errorf("after the other's shot: reviewing %v, %d windows made, %d live; want one made, through which it shot, none live", b.tool.Reviewing(), len(f.made()), f.live())
	}
	for _, e := range []*dragEnv{a, b} {
		e.key(key.NameEscape, 0)
		e.frame()
	}
}

// TestShooterFailedWindows: a session whose window couldn't be made shoots
// through one made for the shot; a session window that fails its frame is
// replaced once, and the shot comes from the new one.
func TestShooterFailedWindows(t *testing.T) {
	f := useFakeWindows(t, func(n int) error {
		if n == 0 {
			return errors.New("no GPU yet")
		}
		return nil
	})
	shots.setWanted(true)
	shots.settle()
	var o op.Ops
	frame := op.Record(&o).Stop()
	img, err := shots.shoot(frame, image.Rect(0, 0, 40, 30), 2)
	if err != nil || img.Bounds().Size() != image.Pt(80, 60) {
		t.Fatalf("a shot with no session window: %v, %v", img, err)
	}
	shots.setWanted(false)
	shots.settle()
	if n := f.live(); n != 0 {
		t.Errorf("%d windows live after the session, want none", n)
	}

	// a session window that fails its frame
	shots.setWanted(true)
	shots.settle()
	ws := f.made()
	bad := ws[len(ws)-1]
	bad.mu.Lock()
	bad.failFrame = true
	bad.mu.Unlock()
	img, err = shots.shoot(frame, image.Rect(0, 0, 40, 30), 2)
	if err != nil || img == nil {
		t.Fatalf("a shot whose session window failed: %v", err)
	}
	if _, r := bad.counts(); r != 1 {
		t.Errorf("the failed window let go of %d times, want once", r)
	}
	shots.setWanted(false)
	shots.settle()
	if n := f.live(); n != 0 {
		t.Errorf("%d windows live after the session, want none", n)
	}
}
