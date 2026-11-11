package messages

// Find in thread (Slack's ThreadSearchButton and ThreadSearchBar under
// find_in_thread, which Taut's ThreadJump forces on) against
// internal/fakeslack's thread header fixtures (synthetic; nothing talks
// to Slack): "Launch checklist for Friday", "Docs are ready", "Ship it".

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// findEnv opens the public fixture's thread with find_in_thread on.
func findEnv(t *testing.T, dark bool) (*ThreadView, *fakeslack.Server, *findHarness) {
	t.Helper()
	app, fs := headerEnv(t)
	var pub fakeslack.ThreadHeaderCase
	for _, c := range fs.ThreadHeaderCases() {
		if c.Name == "public" {
			pub = c
		}
	}
	h := &headerHost{coreHost: coreHost{app: app, th: theme.New(dark)}, primary: "C0SOMEWHERE"}
	tv := NewThreadView(h, pub.Channel, fakeslack.ThreadRootTS)
	fh := newFindHarness(tv, h.th)
	waitHeader(t, tv, fh.frame, pub)
	fh.until(t, "the thread's three messages", func() bool { return len(tv.s.snap.msgs) == 3 })
	if !tv.find.btnRect.Empty() {
		t.Fatal("Search in thread is drawn with find_in_thread off")
	}
	app.SetExperimentGroup("find_in_thread", "on")
	fh.frame()
	return tv, fs, fh
}

// findHarness draws the pane over a router: clicks and keys.
type findHarness struct {
	tv    *ThreadView
	th    *theme.Theme
	r     input.Router
	ops   *op.Ops
	now   time.Time
	clock time.Duration
	size  image.Point
}

func newFindHarness(tv *ThreadView, th *theme.Theme) *findHarness {
	return &findHarness{tv: tv, th: th, ops: new(op.Ops), now: time.Now(), size: image.Pt(480, 700)}
}

func (fh *findHarness) frame() {
	fh.ops.Reset()
	fh.now = fh.now.Add(20 * time.Millisecond)
	paint.FillShape(fh.ops, fh.th.C.Bg, clip.Rect{Max: fh.size}.Op())
	gtx := layout.Context{Ops: fh.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(fh.size),
		Now: fh.now, Source: fh.r.Source()}
	fh.tv.Layout(gtx)
	fh.r.Frame(fh.ops)
}

func (fh *findHarness) click(p image.Point) {
	fh.clock += 600 * time.Millisecond
	pt := f32.Pt(float32(p.X), float32(p.Y))
	fh.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Time: fh.clock},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt, Time: fh.clock + time.Millisecond},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Time: fh.clock + 2*time.Millisecond},
	)
	for k := 0; k < 4; k++ {
		fh.frame()
	}
}

func (fh *findHarness) press(n key.Name, m key.Modifiers) {
	fh.r.Queue(key.Event{Name: n, Modifiers: m, State: key.Press}, key.Event{Name: n, Modifiers: m, State: key.Release})
	for k := 0; k < 3; k++ {
		fh.frame()
	}
}

func (fh *findHarness) until(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		fh.frame()
		time.Sleep(10 * time.Millisecond)
	}
}

// search opens the bar from the header's button and searches q.
func (fh *findHarness) search(t *testing.T, q string, n int) {
	t.Helper()
	tv := fh.tv
	b := tv.find.btnRect
	if b.Dx() != 36 || b.Dy() != 36 {
		t.Fatalf("Search in thread is %v, want Slack Kit's 36px IconButton", b)
	}
	fh.click(b.Min.Add(b.Size().Div(2)))
	if tv.FindBar().Empty() {
		t.Fatal("clicking Search in thread didn't open the bar")
	}
	tv.SetFindQuery(q)
	fh.frame()
	if got, _, _, loading := tv.FindState(); got != q || !loading {
		t.Fatalf("after typing: query %q loading %v, want %q searching", got, loading, q)
	}
	fh.until(t, "the search's answer", func() bool {
		_, res, _, loading := tv.FindState()
		return !loading && len(res) == n
	})
	fh.frame()
}

// The header's button opens the bar; a query is searched once, 300ms
// after the last change (search.inline with find_in_thread), the results
// newest first; every message marks the query, the newest is the current
// result and flashes. Enter and Shift+Enter step and wrap round; Escape
// closes the bar and the highlights go.
func TestThreadHeaderFindInThread(t *testing.T) {
	tv, fs, fh := findEnv(t, false)
	fh.search(t, "s", 3)
	_, res, idx, _ := tv.FindState()
	want := []string{fakeslack.ThreadReplyTS(1), fakeslack.ThreadReplyTS(0), fakeslack.ThreadRootTS}
	if !reflect.DeepEqual(res, want) || idx != 0 {
		t.Fatalf("results %v index %d, want %v from 0", res, idx, want)
	}
	forms := fs.SearchForms("search.inline")
	if len(forms) != 1 {
		t.Fatalf("%d search.inline calls for one query, want 1 (debounced)", len(forms))
	}
	if f := forms[0]; f.Get("find_in_thread") != "true" || f.Get("thread_ts") != fakeslack.ThreadRootTS || f.Get("query") != "s" {
		t.Errorf("search.inline form %v", f)
	}
	marks := func(cur string) {
		t.Helper()
		if len(tv.s.rows) != 3 {
			t.Fatalf("%d rows, want 3", len(tv.s.rows))
		}
		for ts, r := range tv.s.rows {
			if r.find != "s" || r.findFocused != (ts == cur) {
				t.Errorf("row %s: marks %q focused %v (current %s)", ts, r.find, r.findFocused, cur)
			}
		}
	}
	marks(want[0])
	if tv.s.findFlash != want[0] {
		t.Errorf("flashing %q, want the first result %s", tv.s.findFlash, want[0])
	}
	steps := []struct {
		mods key.Modifiers
		idx  int
	}{{0, 1}, {0, 2}, {0, 0}, {key.ModShift, 2}}
	for _, s := range steps {
		fh.press(key.NameReturn, s.mods)
		if _, _, idx, _ := tv.FindState(); idx != s.idx {
			t.Fatalf("Enter (mods %v): index %d, want %d", s.mods, idx, s.idx)
		}
		marks(want[s.idx])
		if tv.s.findFlash != want[s.idx] {
			t.Errorf("flashing %q, want %s", tv.s.findFlash, want[s.idx])
		}
	}
	fh.press(key.NameEscape, 0)
	if !tv.FindBar().Empty() || tv.find.active {
		t.Fatal("Escape left the bar open")
	}
	fh.frame()
	for ts, r := range tv.s.rows {
		if r.find != "" || r.findFocused {
			t.Errorf("row %s still marks %q after closing", ts, r.find)
		}
	}
}

// No match: "No results", nothing marked, the arrows disabled.
func TestThreadHeaderFindInThreadNoMatch(t *testing.T) {
	tv, _, fh := findEnv(t, false)
	fh.search(t, "zebra", 0)
	if _, res, _, _ := tv.FindState(); len(res) != 0 {
		t.Fatalf("results %v", res)
	}
	for ts, r := range tv.s.rows {
		if r.find != "" {
			t.Errorf("row %s marks %q with no results", ts, r.find)
		}
	}
	fh.press(key.NameReturn, 0) // nothing to step to
	if _, _, idx, _ := tv.FindState(); idx != 0 {
		t.Errorf("Enter with no results moved to %d", idx)
	}
}

// The bar's pixels, light and dark (THREAD_HEADER_SHOTS_DIR): the input
// with the query, "1 of 3", the matches marked in Slack's find colours
// (the current result's darker). Needs a GPU (the thread-header
// workflow's xvfb step).
func TestThreadFindShots(t *testing.T) {
	for _, dark := range []bool{false, true} {
		name := "thread-find-light"
		if dark {
			name = "thread-find-dark"
		}
		t.Run(name, func(t *testing.T) {
			tv, _, fh := findEnv(t, dark)
			fh.search(t, "s", 3)
			for i := 0; i < 160; i++ { // past the 3s flash
				fh.frame()
			}
			img := pixeltest.Render(t, fh.size.X, fh.size.Y, fh.ops)
			if dir := os.Getenv("THREAD_HEADER_SHOTS_DIR"); dir != "" {
				saveFindShot(t, filepath.Join(dir, name+".png"), img)
				r := tv.FindBar()
				saveFindShot(t, filepath.Join(dir, name+"-bar.png"), img.SubImage(image.Rect(r.Min.X-8, r.Min.Y-8, r.Max.X+8, r.Max.Y+8)))
			}
			bar := tv.FindBar()
			if bar.Dx() != 300 {
				t.Errorf("the bar is %v, want 300px (min-width over 33%% of 480)", bar)
			}
			// the current result's marks (#1264a38c over white, or
			// #2ba5ceb3 in dark) and the others' (…59 / …73)
			bg := fh.th.C.Bg
			focused, other := findColor(dark, true), findColor(dark, false)
			nf := pixeltest.Ink(img, img.Bounds(), focused, bg)
			no := pixeltest.Ink(img, img.Bounds(), other, bg)
			t.Logf("focused-match pixels %d, other-match pixels %d", nf, no)
			if nf < 20 || no < 20 {
				t.Errorf("the marks aren't drawn: %d focused-match and %d other-match pixels", nf, no)
			}
		})
	}
}

// findColor is a find in thread mark's colour as the palette flattens it
// over the pane: the current result's or the others'.
func findColor(dark, focused bool) color.NRGBA {
	p := richview.LightPalette()
	if dark {
		p = richview.DarkPalette()
	}
	if focused {
		return p.FindFocused
	}
	return p.FindHighlight
}

func saveFindShot(t *testing.T, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Error(err)
	}
}
