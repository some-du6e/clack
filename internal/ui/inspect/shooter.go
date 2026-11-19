package inspect

import (
	"errors"
	"image"
	"log"
	"reflect"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/op"
)

// The shots' GPU. Each shot used to make a headless context, device and
// GPU and let them go again: 4-35 ms on WARP and llvmpipe, 1.2 s for the
// first in a process on llvmpipe (its shaders compiled), and its glyph
// paths and images uploaded afresh every time. A session now keeps one
// headless window (shooter): made when the session starts, off the UI
// goroutine, so the first pick finds it ready; each shot gives its
// framebuffer the shot's size (Gio patch 52, Window.SetSize), keeping the
// GPU's caches from shot to shot; let go when the session ends. Outside a
// session (a test calling Shoot) a shot makes and lets go of its own
// window, as before.

type shooter struct {
	mu   sync.Mutex
	want bool       // a session runs
	w    shotWindow // the session's, nil until made
	// pending counts the reconciles running (settle: tests)
	pending sync.WaitGroup
}

// shotWindow is what a shot renders in: Gio's headless.Window (tests: a
// fake).
type shotWindow interface {
	SetSize(width, height int) error
	Frame(*op.Ops) error
	Screenshot(*image.RGBA) error
	Release()
}

// newShotWindow makes a shot's window (tests replace it).
var newShotWindow = func(width, height int) (shotWindow, error) {
	w, err := headless.NewWindow(width, height)
	if err != nil {
		return nil, err
	}
	return w, nil
}

var shots shooter

// shootHeadless is Shoot's own: the session's window renders it.
func shootHeadless(frame op.CallOp, region image.Rectangle, k float32) (*image.RGBA, error) {
	return shots.shoot(frame, region, k)
}

// ownShoot reports that Shoot is the headless one (tests replace it: no
// GPU to make then).
func ownShoot() bool {
	return reflect.ValueOf(Shoot).Pointer() == reflect.ValueOf(shootHeadless).Pointer()
}

// setWanted says a session started (on) or ended: its window is made or
// let go of off the UI goroutine. Several calls in a row end as the last
// asked: each one acts on want as it is when it runs.
func (s *shooter) setWanted(on bool) {
	s.mu.Lock()
	s.want = on
	s.mu.Unlock()
	s.pending.Add(1)
	go func() {
		defer s.pending.Done()
		s.reconcile()
	}()
}

// settle waits for the reconciles running (tests).
func (s *shooter) settle() { s.pending.Wait() }

func (s *shooter) reconcile() {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.want && s.w == nil:
		w, err := newShotWindow(64, 64)
		if err != nil {
			log.Printf("inspect: no headless GPU for the shots yet: %v", err)
			return
		}
		s.w = w
	case !s.want && s.w != nil:
		s.w.Release()
		s.w = nil
	}
}

// shoot renders region of frame at k through the session's window (made
// now if it isn't yet: a shot waits for one being made), or a window of
// its own outside a session.
func (s *shooter) shoot(frame op.CallOp, region image.Rectangle, k float32) (*image.RGBA, error) {
	size := ShotSize(region, k)
	if size.X <= 0 || size.Y <= 0 {
		return nil, errors.New("empty region")
	}
	t0 := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	st := shotTimes{reused: s.w != nil}
	var o op.Ops
	tr := op.Affine(f32.AffineId().Offset(f32.Pt(-float32(region.Min.X), -float32(region.Min.Y))).Scale(f32.Point{}, f32.Pt(k, k))).Push(&o)
	frame.Add(&o)
	tr.Pop()
	for try := 0; ; try++ {
		w, own, err := s.window(size)
		if err != nil {
			return nil, err
		}
		t1 := time.Now()
		img, err := render(w, &o, size, &st, t1)
		if own {
			w.Release()
		}
		if err == nil {
			st.gpu = t1.Sub(t0)
			lastShot = st
			return img, nil
		}
		if own || try > 0 {
			return nil, err
		}
		// the session's window failed (on EGL another headless window's
		// Release can terminate the display under it): a new one, once
		log.Printf("inspect: the shots' GPU failed (%v): a new one", err)
		s.w.Release()
		s.w = nil
		st.reused = false
	}
}

// window is the window for a shot of size px (s.mu held): the session's,
// given that size, or a new one, kept as the session's while one runs;
// own when it's the shot's alone, to let go of after.
func (s *shooter) window(size image.Point) (w shotWindow, own bool, err error) {
	if s.w != nil {
		if err := s.w.SetSize(size.X, size.Y); err == nil {
			return s.w, false, nil
		}
		s.w.Release()
		s.w = nil
	}
	if w, err = newShotWindow(size.X, size.Y); err != nil {
		return nil, false, err
	}
	if s.want {
		s.w = w
		return w, false, nil
	}
	return w, true, nil
}

// render draws o in w (size px) and reads it back, into st's render and
// readback times from t1.
func render(w shotWindow, o *op.Ops, size image.Point, st *shotTimes, t1 time.Time) (*image.RGBA, error) {
	if err := w.Frame(o); err != nil {
		return nil, err
	}
	t2 := time.Now()
	img := image.NewRGBA(image.Rectangle{Max: size})
	if err := w.Screenshot(img); err != nil {
		return nil, err
	}
	st.render, st.read = t2.Sub(t1), time.Since(t2)
	return img, nil
}
