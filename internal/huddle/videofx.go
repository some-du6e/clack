package huddle

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/huddle/bgblur"
)

// VideoEffect is your camera's background effect: Slack's device pref
// prefs.video.effect (localStorage, not users.prefs). Preferences > Audio
// & video's "Blur your video background…" checkbox (name
// video_background_filter) toggles it between unset and "blur"; the huddle
// window's effects picker also sets "bg" with an effectBgKey (LIVING_ROOM
// and the other images, or a custom upload), which Clack doesn't have.
type VideoEffect string

const (
	VideoEffectNone VideoEffect = ""
	VideoEffectBlur VideoEffect = "blur"
	// VideoEffectImage is Slack's background image ("bg"); Clack sends the
	// camera unchanged for it.
	VideoEffectImage VideoEffect = "bg"
)

// videoFX applies the effect to camera frames on the camera goroutine,
// before the self preview and the encoder (Slack's
// BackgroundWorkerVideoTransformDevice sits between the camera and both).
type videoFX struct {
	mu   sync.Mutex
	want VideoEffect

	runMu   sync.Mutex // the effect belongs to the camera goroutine
	fx      *bgblur.Effect
	failed  bool
	applied atomic.Int64
	took    atomic.Int64 // the last frame's processing time, ns
}

func (v *videoFX) set(e VideoEffect) {
	v.mu.Lock()
	v.want = e
	v.mu.Unlock()
}

func (v *videoFX) get() VideoEffect {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.want
}

// apply blurs f in place when the effect is "blur". Turning it off frees
// the model, as Slack stops its worker for "none".
func (v *videoFX) apply(f *CameraFrame, logf func(string, ...any)) {
	want := v.get()
	v.runMu.Lock()
	defer v.runMu.Unlock()
	if want != VideoEffectBlur {
		if v.fx != nil {
			v.fx.Close()
			v.fx = nil
		}
		return
	}
	if v.fx == nil {
		if v.failed {
			return
		}
		fx, err := bgblur.NewEffect()
		if err != nil {
			v.failed = true // Slack falls back to the plain camera too
			logf("huddle: background blur: %v", err)
			return
		}
		v.fx = fx
	}
	bf := bgblur.Frame{W: f.W, H: f.H, Y: f.Y, UV: f.UV, YStride: f.YStride, UVStride: f.UVStride, FullRange: f.FullRange, At: f.At}
	t0 := time.Now()
	v.fx.Apply(&bf)
	v.took.Store(int64(time.Since(t0)))
	v.applied.Add(1)
}

func (v *videoFX) close() {
	v.runMu.Lock()
	if v.fx != nil {
		v.fx.Close()
		v.fx = nil
	}
	v.runMu.Unlock()
}

// SetVideoEffect changes your camera's background effect from the next
// frame, for others and in your self view.
func (s *Session) SetVideoEffect(e VideoEffect) { s.cam.fx.set(e) }

// VideoEffect is the camera's background effect.
func (s *Session) VideoEffect() VideoEffect { return s.cam.fx.get() }

// SetVideoEffect changes the preview camera's background effect.
func (p *Preview) SetVideoEffect(e VideoEffect) { p.fx.set(e) }

// VideoEffect is the preview camera's background effect.
func (p *Preview) VideoEffect() VideoEffect { return p.fx.get() }
