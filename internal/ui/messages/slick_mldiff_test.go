package messages

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// MessageLogger's edit diff (src/plugins/MessageLogger/index.ts css() and
// editHistory): under an edited message, .slick-ml-edited-original (block,
// opacity .62) holds jsdiff's diffWordsWithSpace(old, new) parts, the
// words you took out in .slick-ml-edit-removed (#f27b9f on rgba(224, 30,
// 90, .25), struck through) and the ones you added in .slick-ml-edit-added
// (#75d9a5 on rgba(46, 160, 100, .25)). Karim 2026-11-12: "this is
// missing a slight tilt & extra" edited to "… tilt" drew "& extra" green
// and not struck. Checked against what Chromium computes and paints for
// the same CSS: the cascade's values, and the pixels the edit adds (pink
// ink, a struck line, the red tint at the group's .62 opacity, composited
// in sRGB as Chromium does), light and dark.

const (
	mlOld = "this is missing a slight tilt & extra"
	mlNew = "this is missing a slight tilt"
)

var (
	mlRemovedFg = color.NRGBA{R: 0xf2, G: 0x7b, B: 0x9f, A: 0xff}
	mlRemovedBg = color.NRGBA{R: 224, G: 30, B: 90, A: 64} // rgba(…, .25): Chromium keeps .25, 63.75 in 8 bits
	mlAddedFg   = color.NRGBA{R: 0x75, G: 0xd9, B: 0xa5, A: 0xff}
)

func TestSlickViewMessageLoggerEditDiff(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) { mlEditDiff(t, dark, mode) })
	}
}

func mlEditDiff(t *testing.T, dark bool, mode string) {
	const ch, ts = "C0MLDIFF", "1700000801.000100"
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: ch, Name: "diffs", IsChannel: true, IsMember: true, NumMembers: 3})
		s.SetLatest(ch, []slack.Message{{Type: "message", TS: ts, User: "UBOB", Text: mlOld}}, false)
	})
	e.h.th = theme.New(dark)
	e.install(t, "MessageLogger", nil)
	patched(t, "MessageWrapper", "MessageText", "Blocks")
	v := NewChannelView(e.h, ch)
	v.Preview = true
	size := image.Pt(900, 400)
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(size), Now: time.Now()}
		v.Layout(gtx)
	}
	e.waitFor(t, "the row drawn through the plugin", frame, func() bool { return v.s.rows[ts] != nil && e.ui.anyTree(ts) })
	for i := 0; i < 5; i++ {
		frame()
	}
	gpu := mlPixels()
	var before *image.RGBA
	if gpu {
		before = pixeltest.Render(t, size.X, size.Y, ops)
	}

	// the edit, as Slack's socket sends it (message_changed with the old copy)
	raw, _ := json.Marshal(map[string]any{"type": "message", "subtype": "message_changed", "channel": ch, "hidden": true, "ts": "1700000811.000100",
		"message":          map[string]any{"type": "message", "user": "UBOB", "text": mlNew, "ts": ts, "edited": map[string]any{"user": "UBOB", "ts": "1700000811.000000"}},
		"previous_message": map[string]any{"type": "message", "user": "UBOB", "text": mlOld, "ts": ts}})
	e.app.Inject(slack.Event{Type: "message", Subtype: "message_changed", Channel: json.RawMessage(`"` + ch + `"`), Raw: raw})
	e.waitFor(t, "the edit history with its removed words", frame, func() bool {
		return e.ui.anyTree("slick-ml-edit-removed") && len(slackc.ClassStyles("slick-ml-edit-removed")) > 0
	})
	for i := 0; i < 5; i++ {
		frame()
	}
	if e.ui.anyTree("slick-ml-edit-added") {
		t.Errorf("the diff has an added part; jsdiff gives only the removed %q", " & extra")
		e.ui.dump(t)
	}

	// the cascade, as Chromium computes it
	rm := slackc.ClassStyles("slick-ml-edit-removed")
	orig := slackc.ClassStyles("slick-ml-edited-original")
	t.Logf("%s: removed %+v; original %+v; added %+v", mode, rm, orig, slackc.ClassStyles("slick-ml-edit-added"))
	if len(rm) != 1 || len(orig) != 1 {
		t.Fatalf("%d removed spans and %d originals laid out, want 1 and 1", len(rm), len(orig))
	}
	r := rm[0]
	if r.Color != mlRemovedFg {
		t.Errorf("removed words' color %v, want #f27b9f (Chromium: rgb(242, 123, 159))", r.Color)
	}
	if r.Background.R != mlRemovedBg.R || r.Background.G != mlRemovedBg.G || r.Background.B != mlRemovedBg.B || r.Background.A < 63 || r.Background.A > 64 {
		t.Errorf("removed words' background %v, want rgba(224, 30, 90, .25)", r.Background)
	}
	if r.Decoration&css.DecoLineThrough == 0 {
		t.Errorf("removed words aren't struck through (text-decoration %b)", r.Decoration)
	}
	if o := orig[0].Opacity; o < 0.615 || o > 0.625 {
		t.Errorf("the original's opacity %v, want .62", o)
	}
	if r.Opacity < 0.615 || r.Opacity > 0.625 {
		t.Errorf("the removed words' group opacity %v, want .62 (the original's)", r.Opacity)
	}

	if !gpu {
		t.Log("no GPU: the cascade checked, the pixels not")
		return
	}
	after := pixeltest.Render(t, size.X, size.Y, ops)
	saveMLShot(t, "message-logger-edit-diff-"+mode, after)

	// the pixels the edit added: Chromium paints the group (text, line and
	// background) at .62 over the pane, blended in sRGB
	bg := e.h.th.C.Bg
	tint := mlOver(bg, mlRemovedBg, 0.62)
	ink := mlOver(bg, mlRemovedFg, 0.62) // the group's text covers its own background: text at .62 over the pane
	green := mlOver(bg, mlAddedFg, 0.62)
	newPx := func(match func(c color.NRGBA) bool) (n int) {
		b := after.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				c, o := after.RGBAAt(x, y), before.RGBAAt(x, y)
				if c != o && match(color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff}) {
					n++
				}
			}
		}
		return
	}
	near := func(want color.NRGBA, tol int) func(color.NRGBA) bool {
		return func(c color.NRGBA) bool {
			return mlAbs(int(c.R)-int(want.R)) <= tol && mlAbs(int(c.G)-int(want.G)) <= tol && mlAbs(int(c.B)-int(want.B)) <= tol
		}
	}
	nInk, nTint, nGreen := newPx(near(ink, 10)), newPx(near(tint, 6)), newPx(near(green, 24))
	struck := mlLongestRun(after, before, near(ink, 16))
	t.Logf("%s: new pixels: pink ink %v ×%d, red tint %v ×%d, green %v ×%d; longest pink run %dpx", mode, ink, nInk, tint, nTint, green, nGreen, struck)
	if nInk < 20 {
		t.Errorf("%s: the removed words aren't pink (%d pixels near %v); most common new colours: %s", mode, nInk, ink, mlTopColors(after, before, 8))
	}
	if nTint < 60 {
		t.Errorf("%s: no red tint behind them (%d pixels near %v, sRGB blend); most common new colours: %s", mode, nTint, tint, mlTopColors(after, before, 8))
	}
	if nGreen > 10 {
		t.Errorf("%s: %d new green pixels (near %v): something is drawn as added", mode, nGreen, green)
	}
	if struck < 30 { // " & extra" is ~50px at 15px; glyph strokes are far shorter
		t.Errorf("%s: no line through the removed words (longest pink run %dpx)", mode, struck)
	}
}

func mlPixels() bool {
	return os.Getenv("CLACK_PIXEL_TESTS") != "" || os.Getenv("SLICK_SHOTS_DIR") != ""
}

// mlOver is col at alpha a·(col.A/255) over bg, blended in sRGB (Chromium).
func mlOver(bg, col color.NRGBA, a float64) color.NRGBA {
	al := a * float64(col.A) / 255
	mix := func(b, c uint8) uint8 { return uint8(float64(b)*(1-al) + float64(c)*al + 0.5) }
	return color.NRGBA{R: mix(bg.R, col.R), G: mix(bg.G, col.G), B: mix(bg.B, col.B), A: 0xff}
}

func mlAbs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// mlLongestRun is the longest horizontal run of new pixels matching m.
func mlLongestRun(after, before *image.RGBA, m func(color.NRGBA) bool) int {
	best := 0
	b := after.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		run := 0
		for x := b.Min.X; x < b.Max.X; x++ {
			c := after.RGBAAt(x, y)
			if c != before.RGBAAt(x, y) && m(color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0xff}) {
				run++
				best = max(best, run)
			} else {
				run = 0
			}
		}
	}
	return best
}

// mlTopColors lists the n commonest colours the edit added.
func mlTopColors(after, before *image.RGBA, n int) string {
	count := map[color.RGBA]int{}
	b := after.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if c := after.RGBAAt(x, y); c != before.RGBAAt(x, y) {
				count[c]++
			}
		}
	}
	type cc struct {
		c color.RGBA
		n int
	}
	var l []cc
	for c, k := range count {
		l = append(l, cc{c, k})
	}
	sort.Slice(l, func(i, j int) bool { return l[i].n > l[j].n })
	s := ""
	for i := 0; i < len(l) && i < n; i++ {
		s += fmt.Sprintf("#%02x%02x%02x×%d ", l[i].c.R, l[i].c.G, l[i].c.B, l[i].n)
	}
	return s
}

func saveMLShot(t *testing.T, name string, img *image.RGBA) {
	t.Helper()
	dir := os.Getenv("SLICK_SHOTS_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
