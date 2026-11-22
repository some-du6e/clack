package composer

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	vclip "github.com/some-du6e/clack/internal/clip"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// voiceMic is a synthetic microphone that sounds enough like speech for
// the recorder's noise suppression to keep it: internal/huddle's
// synthSpeech (TestMicNoiseSuppression's), a glottal pulse train gliding
// 100 to 150 Hz through a vowel's three formants, 250 ms syllables with
// gaps, at -20 dBFS; looped in 20 ms chunks at real time.
type voiceMic struct {
	stop chan struct{}
	once sync.Once
}

func newVoiceMic() huddle.Device { return &voiceMic{stop: make(chan struct{})} }

var voicePCM = func() []int16 {
	const rate = 48000
	n := 6 * rate
	pcm, on := make([]float64, n), make([]bool, n)
	vowels := [][3]float64{{730, 1090, 2440}, {270, 2290, 3010}, {530, 1840, 2480}, {300, 870, 2240}, {660, 1720, 2410}}
	type res struct{ a1, a2, b, y1, y2 float64 }
	mk := func(f, bw float64) res {
		r := math.Exp(-math.Pi * bw / rate)
		th := 2 * math.Pi * f / rate
		return res{a1: 2 * r * math.Cos(th), a2: -r * r, b: 1 - r}
	}
	var rs [3]res
	phase, t, syl := 0.0, 0, 0
	for t < n {
		v := vowels[syl%len(vowels)]
		for k := range rs {
			rs[k] = mk(v[k], 80+40*float64(k))
		}
		sylLen := rate / 4
		for i := 0; i < sylLen && t < n; i, t = i+1, t+1 {
			env := math.Sin(math.Pi * float64(i) / float64(sylLen))
			phase += (100 + 50*float64(i)/float64(sylLen)) / rate
			x := 0.0
			if phase >= 1 {
				phase--
				x = 1
			}
			for k := range rs {
				r := &rs[k]
				y := r.b*x + r.a1*r.y1 + r.a2*r.y2
				r.y2, r.y1 = r.y1, y
				x = y
			}
			pcm[t], on[t] = x*env, true
		}
		gap := rate * 12 / 100
		if syl%4 == 3 {
			gap = rate * 6 / 10
		}
		t += gap
		syl++
	}
	var s float64
	var c int
	for i, v := range pcm {
		if on[i] {
			s += v * v
			c++
		}
	}
	scale := math.Pow(10, -20.0/20) * 32768 / math.Sqrt(s/float64(c))
	out := make([]int16, n)
	for i, v := range pcm {
		out[i] = int16(math.Max(-32768, math.Min(32767, v*scale)))
	}
	return out
}()

func (d *voiceMic) Start(capture func([]int16), _ func([]int16)) error {
	go func() {
		t := time.NewTicker(20 * time.Millisecond)
		defer t.Stop()
		buf := make([]int16, 960)
		n := 0
		for {
			select {
			case <-d.stop:
				return
			case <-t.C:
			}
			for i := range buf {
				buf[i] = voicePCM[n%len(voicePCM)]
				n++
			}
			if capture != nil {
				capture(buf)
			}
		}
	}()
	return nil
}

func (d *voiceMic) Close() { d.once.Do(func() { close(d.stop) }) }

// TestAudioClipShots draws the audio recorder's states over the channel
// composer, light and dark at 2x: recording (the blue waveform and the
// timer), the last 10 s (red), the speedbump an outside click shows, the
// microphone error dialog, and the attached clip waiting in the composer
// (PendingAudioMessageUpload). composer-audit-clip-<state>-<mode>.png in
// COMPOSER_AUDIT_SHOTS_DIR (composer-audit-shots); skipped without it.
// fakeslack and a synthetic microphone only.
func TestAudioClipShots(t *testing.T) {
	dir := os.Getenv("COMPOSER_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("COMPOSER_AUDIT_SHOTS_DIR not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	setClipRecorder(t, true)
	useToneMic(t)
	stubTyping(t)
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, state := range []string{"recording", "warning", "speedbump", "micerror", "pending"} {
			t.Run(state+"-"+mode, func(t *testing.T) {
				app, _, ch := clipFake(t, map[string]any{"allow_audio_clips": true})
				oldDev, oldLimit := ClipDevice, clipTimeLimit
				t.Cleanup(func() { ClipDevice, clipTimeLimit = oldDev, oldLimit })
				ClipDevice = newVoiceMic
				switch state {
				case "warning":
					clipTimeLimit = 11 * time.Second // the last 10 s from 1 s on
				case "micerror":
					ClipDevice = func() huddle.Device { return deadMic{} }
				}
				clearDraft(ch, "")
				t.Cleanup(func() { clearDraft(ch, "") })
				const scale = 2
				w, h := 760*scale, 360*scale
				th := theme.New(dark)
				host := &clipHost{slickHost: slickHost{app: app, th: th}}
				c := New(host, ch, "")
				t.Cleanup(c.Close)
				c.Origin = image.Pt(0, 200*scale)
				if state == "pending" { // the attachment row: room for the whole box
					c.Origin = image.Pt(0, 100*scale)
				}
				var ops op.Ops
				frame := func() {
					ops.Reset()
					paint.FillShape(&ops, th.C.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
					gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
						Constraints: layout.Constraints{Max: image.Pt(w, h)}, Now: time.Now()}
					slackc.Frame(gtx)
					g := gtx
					g.Constraints.Max = image.Pt(w, h-c.Origin.Y)
					tr := op.Offset(c.Origin).Push(&ops)
					c.Layout(g)
					tr.Pop()
					host.layoutOverlays(gtx)
				}
				until := func(what string, cond func() bool) {
					t.Helper()
					deadline := time.Now().Add(15 * time.Second)
					for !cond() {
						if time.Now().After(deadline) {
							t.Fatalf("timed out waiting for %s", what)
						}
						frame()
						time.Sleep(10 * time.Millisecond)
					}
				}
				frame()
				frame()
				c.ui.btn[crAudio].Click()
				frame()
				rec := c.ClipRecorder()
				if rec == nil {
					t.Fatal("the button opened no recorder")
				}
				if state == "micerror" {
					until("the error dialog", func() bool { return len(host.open()) == 2 })
				} else {
					until("recording", func() bool { return rec.State() == vclip.Recording })
					until("2.4 s of waveform", func() bool { return rec.Elapsed() >= 2400*time.Millisecond })
				}
				switch state {
				case "warning":
					if !rec.Warning() {
						t.Fatal("no warning 8.6 s before the limit")
					}
				case "speedbump":
					c.clipPop.speedbump()
				case "pending":
					c.StopClip()
					until("the clip uploaded", func() bool {
						c.att.mu.Lock()
						defer c.att.mu.Unlock()
						return len(c.att.chips) == 1 && c.att.chips[0].prepared != nil
					})
				}
				frame()
				frame()
				img := pixeltest.Render(t, w, h, &ops)
				path := filepath.Join(dir, fmt.Sprintf("composer-audit-clip-%s-%s.png", state, mode))
				f, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err := png.Encode(f, img); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote %s", path)
			})
		}
	}
}
