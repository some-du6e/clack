package ui

import (
	"encoding/json"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

// BetterTypingIndicator patches TypingNames and hands <Original> member
// links as firstTyper/secondTyper. Karim's screenshot: the name came out
// at the 15px body size with "is typing…" as raised small text. In
// Slack's DOM those nodes sit in TypingNames' <b> inside
// .p-notification_bar__section (10.5px, 1rem line, foreground_max), so the
// whole line is one 10.5px run on one baseline in the 24px bar. Ink
// taller than ~12px means a name drew at the body size again.
func TestSlickViewBetterTypingIndicator(t *testing.T) {
	e := newSV(t)
	e.install("BetterTypingIndicator", map[string]any{"showAvatars": false})
	e.until(5*time.Second, "TypingNames patched", func() bool { return slackc.Patched("TypingNames") })
	sh := newSVShooter(t, e)
	var cv *convo
	e.until(8*time.Second, "#general's composer laid out", func() bool {
		cv = e.a.convos[ttGen+"/"]
		return cv != nil && !cv.compRect.Empty()
	})
	typing := func(users ...string) {
		for _, u := range users {
			raw, _ := json.Marshal(map[string]any{"type": "user_typing", "channel": ttGen, "user": u})
			e.ca.Inject(slack.Event{Type: "user_typing", Channel: json.RawMessage(`"` + ttGen + `"`), Raw: raw})
		}
	}
	for _, c := range []struct {
		name  string
		users []string
	}{
		{"better-typing-one", []string{svAlice}},
		{"better-typing-two", []string{svAlice, svBob}},
	} {
		typing(c.users...)
		e.until(6*time.Second, c.name+" through the plugin", func() bool { return e.tree("slick-typing-indicator") != "" })
		e.wait(600*time.Millisecond, func() bool { return false })
		if tr := e.tree("slick-typing-indicator"); tr != "" {
			i := strings.Index(tr, "slick-typing-indicator")
			t.Logf("%s tree: %s", c.name, tr[max(i-600, 0):min(i+2400, len(tr))])
		}
		for _, dark := range []bool{false, true} {
			e.a.th.SetDark(dark)
			e.wait(300*time.Millisecond, func() bool { return false })
			img := sh.shot()
			r := cv.compRect
			bar := image.Rect(r.Min.X, r.Max.Y-24, r.Max.X, r.Max.Y)
			top, bottom, ok := inkRows(img, bar)
			mode := map[bool]string{false: "-light", true: "-dark"}[dark]
			sh.save(c.name+mode, img.SubImage(image.Rect(r.Min.X, max(r.Min.Y-20, 0), r.Max.X, min(r.Max.Y+10, e.sz.Y))).(*image.RGBA))
			if !ok {
				t.Errorf("%s%s: no typing line drawn in the bar %v", c.name, mode, bar)
				continue
			}
			t.Logf("%s%s: ink rows %d..%d of the bar's %d..%d", c.name, mode, top, bottom, bar.Min.Y, bar.Max.Y)
			if h := bottom - top + 1; h > 12 {
				t.Errorf("%s%s: the typing line's ink is %dpx tall; 10.5px text is ~10px (a name at the 15px body size again?)", c.name, mode, h)
			}
			if top <= bar.Min.Y || bottom >= bar.Max.Y-1 {
				t.Errorf("%s%s: the typing line's ink (%d..%d) leaves the 24px bar %d..%d", c.name, mode, top, bottom, bar.Min.Y, bar.Max.Y)
			}
		}
		e.a.th.SetDark(false)
		typing(c.users...)
	}
}

// inkRows finds the first and last rows of r holding text ink: pixels
// far from the row's left-edge background.
func inkRows(img *image.RGBA, r image.Rectangle) (top, bottom int, ok bool) {
	r = r.Intersect(img.Bounds())
	bg := img.RGBAAt(r.Max.X-4, r.Min.Y+r.Dy()/2)
	top, bottom = -1, -1
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X + 4; x < r.Min.X+r.Dx()/2; x++ {
			if colorDist(img.RGBAAt(x, y), bg) > 90 {
				if top < 0 {
					top = y
				}
				bottom = y
				break
			}
		}
	}
	return top, bottom, top >= 0
}

func colorDist(a, b color.RGBA) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)
}
