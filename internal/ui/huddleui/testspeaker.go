package huddleui

import (
	"image"
	"image/color"
	"math"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Test Speaker, as Slack's useTestSpeakerMenuItem (gantry-v2-async-
// client-boot-shared ifMn) and the playback it calls (client-boot Ofe8's
// ZH with cfKp's pW):
//
//   - the sound is complete_quest_requirement-74d22f6.mp3 (1.5 s, 44.1 kHz
//     mono), at volume 1, on your preferred output (useSlackPreferredSink:
//     getPreferredAudioSinkId, the Speaker you picked, else the default);
//   - each of Slack's sounds is one cached <audio> element: a second click
//     plays it again from the start (currentTime = 0), and it plays beside
//     the lounge music, an element of its own (huddle.Session.PlaySound);
//   - the row keeps its label ("Test Speaker", a 20px icon then 4px then
//     the text: .testSpeakerContainer__t2Dh2). With Accessibility > Allow
//     animations on (a11y_animations) a click turns the icon sound-filled,
//     then every 250 ms it steps through sound-medium-filled, sound-filled,
//     sound-down-filled, … and stops the second time it reaches
//     sound-filled (1.25 s), jiggling meanwhile (.jiggleIcon__KtYd9:
//     jiggle__YcbaG, 0.3 s, 0 → −5° → 5° → 0, each step CSS's ease); then it
//     is sound again. A click while it runs sets sound-filled until the next
//     step and doesn't restart the count. Animations off, nothing changes;
//   - picking a Speaker (useAudioOutSettingsMenuItems) saves the device,
//     plays the test sound (on the new output), then closes the menu;
//   - the menus: the bottom bar's caret popover closes on any pick
//     (HuddleToolbarMuteButton AX6C: onMenuItemSelected stops the click and
//     closes it); the compact huddle window's devices menu
//     (HuddleCompactDeviceMenuOverlay EN94, its menu without
//     onMenuItemSelected) stays open until a row closes it, which Test
//     Speaker doesn't.

// testSpeakerSound is Test Speaker's sound (cfKp pW).
const testSpeakerSound = "complete_quest_requirement-74d22f6.mp3"

const (
	speakerStep   = 250 * time.Millisecond // useTestSpeakerMenuItem's interval
	speakerSteps  = 5                      // until sound-filled comes round twice
	jigglePeriod  = 300 * time.Millisecond // jiggle__YcbaG, .3s infinite
	jiggleDegrees = 5
)

// the icon after each step: f[(a+1)%3] from a = 0, f being
// [sound-down-filled, sound-medium-filled, sound-filled]
var speakerIcons = [speakerSteps]string{"sound-filled", "sound-medium-filled", "sound-filled", "sound-down-filled", "sound-medium-filled"}

// speakerTest is the row's animation (the controller's, so the window's
// and the bar's menus agree).
type speakerTest struct {
	mu    sync.Mutex
	start time.Time // the click that started it (zero: not running)
	click time.Time // the last click while it ran
}

// clicked starts the animation (or, while it runs, shows sound-filled
// again without restarting it). Animations off, it does nothing.
func (t *speakerTest) clicked(now time.Time) {
	if uipref.Get().NoAnimations {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.start.IsZero() || now.Sub(t.start) >= speakerSteps*speakerStep {
		t.start = now
	}
	t.click = now
}

// icon is the row's icon at now, whether it jiggles, and when it next
// changes (zero: it won't).
func (t *speakerTest) icon(now time.Time) (name string, jiggle bool, next time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.start.IsZero() {
		return "sound", false, time.Time{}
	}
	el := now.Sub(t.start)
	if el < 0 || el >= speakerSteps*speakerStep {
		t.start, t.click = time.Time{}, time.Time{}
		return "sound", false, time.Time{}
	}
	step := int(el / speakerStep)
	name = speakerIcons[step]
	if c := t.click.Sub(t.start); c > 0 && int(c/speakerStep) == step {
		name = "sound-filled" // the click's icon, until the next step
	}
	return name, true, t.start.Add(time.Duration(step+1) * speakerStep)
}

// jiggleAngle is jiggle__YcbaG's rotation (degrees) el into the
// animation: 0% 0, 25% −5°, 75% 5°, 100% 0, each keyframe step eased.
func jiggleAngle(el time.Duration) float64 {
	p := math.Mod(float64(el)/float64(jigglePeriod), 1)
	seg := func(from, to, a, b float64) float64 { return a + (b-a)*cssEase((p-from)/(to-from)) }
	switch {
	case p < .25:
		return seg(0, .25, 0, -jiggleDegrees)
	case p < .75:
		return seg(.25, .75, -jiggleDegrees, jiggleDegrees)
	}
	return seg(.75, 1, jiggleDegrees, 0)
}

// cssEase is CSS's default animation-timing-function, ease:
// cubic-bezier(.25, .1, .25, 1).
func cssEase(x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	bez := func(a, b, t float64) float64 { u := 1 - t; return 3*u*u*t*a + 3*u*t*t*b + t*t*t }
	lo, hi, t := 0.0, 1.0, x
	for i := 0; i < 40; i++ {
		d := bez(0.25, 0.25, t) - x
		if math.Abs(d) < 1e-6 {
			break
		}
		if d < 0 {
			lo = t
		} else {
			hi = t
		}
		t = (lo + hi) / 2
	}
	return bez(0.1, 1, t)
}

// testSpeakerLabel draws the row's label: the icon (20px, 1px low;
// rotated about its centre while it jiggles), 4px, "Test Speaker".
func (c *Controller) testSpeakerLabel(th *theme.Theme) func(gtx C, fg color.NRGBA) D {
	return func(gtx C, fg color.NRGBA) D {
		now := gtx.Now
		if now.IsZero() {
			now = time.Now()
		}
		name, jiggle, next := c.spk.icon(now)
		is := gtx.Dp(20)
		h := max(gtx.Constraints.Max.Y, is)
		func() {
			defer op.Offset(image.Pt(0, (h-is)/2+gtx.Dp(1))).Push(gtx.Ops).Pop()
			if jiggle {
				c.spk.mu.Lock()
				el := now.Sub(c.spk.start)
				c.spk.mu.Unlock()
				a := float32(jiggleAngle(el) * math.Pi / 180)
				ctr := f32.Pt(float32(is)/2, float32(is)/2)
				defer op.Affine(f32.Affine2D{}.Rotate(ctr, a)).Push(gtx.Ops).Pop()
				gtx.Execute(op.InvalidateCmd{}) // the next frame of the jiggle
			} else if !next.IsZero() {
				gtx.Execute(op.InvalidateCmd{At: next})
			}
			iconAt(gtx, image.Pt(is, is), name, 20, fg)
		}()
		label := nsHuddles.T("Test Speaker")
		tx := is + gtx.Dp(4)
		ls := labelSize(gtx, th, 15, theme.Regular, label)
		at(gtx, image.Pt(tx, (h-ls.Y)/2), ls, func(gtx C) D { return th.Label(gtx, 15, theme.Regular, fg, label) })
		return D{Size: image.Pt(tx+ls.X, h)}
	}
}

// testSpeaker is the row's click: the animation and the sound.
func (c *Controller) testSpeaker(s *huddle.Session) {
	c.spk.clicked(time.Now())
	c.changed()
	go func() {
		pcm, err := loadMusic(testSpeakerSound)
		if err != nil || s == nil {
			return
		}
		s.PlaySound(testSpeakerSound, pcm)
	}()
}
