package ui

// OS file drops (Gio patch 11's app.FileDropEvent) through the real
// client against internal/fakeslack (synthetic; nothing talks to Slack):
// which pane's overlay shows as the drag moves, what it says, that it
// goes on leave, and where a drop's files end up (Slack's FileDragDrop,
// drop_overlay.go). The events go to App.fileDrop as Run hands them over;
// frames go through a real input.Router.

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// dropDrawn records the strings frames draw (theme.DrawHook).
type dropDrawn struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (d *dropDrawn) hook(t *testing.T) {
	d.seen = map[string]bool{}
	theme.DrawHook = func(s string) {
		d.mu.Lock()
		d.seen[s] = true
		d.mu.Unlock()
	}
	t.Cleanup(func() { theme.DrawHook = nil })
}

func (d *dropDrawn) reset() {
	d.mu.Lock()
	d.seen = map[string]bool{}
	d.mu.Unlock()
}

func (d *dropDrawn) has(s string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.seen[s]
}

func dropAt(kind app.FileDropKind, p image.Point, mods key.Modifiers, paths ...string) app.FileDropEvent {
	return app.FileDropEvent{Kind: kind, Position: f32.Pt(float32(p.X), float32(p.Y)), Paths: paths, Modifiers: mods}
}

func centre(r image.Rectangle) image.Point { return r.Min.Add(r.Size().Div(2)) }

// dropEnv opens the public fixture channel with a thread beside it.
func dropEnv(t *testing.T) (e *perfEnv, ch string, chZone, thZone image.Rectangle) {
	t.Helper()
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e = startEnv(t, fs, t.TempDir())
	e.a.motionOverride = 1 // reduced motion: the overlay draws its last frame at once
	e.waitReady()
	ch = fakeslack.ThreadPublicID
	e.openChannel(ch)
	e.a.OpenThread(ch, fakeslack.ThreadRootTS)
	chc, thc := e.a.convoFor(ch, ""), e.a.convoFor(ch, fakeslack.ThreadRootTS)
	e.until("both panes take drops", 20*time.Second, func() bool {
		chZone, thZone = image.Rectangle{}, image.Rectangle{}
		for _, z := range e.a.drop.zones {
			switch z.c {
			case chc:
				chZone = z.r
			case thc:
				thZone = z.r
			}
		}
		return !chZone.Empty() && !thZone.Empty() && chc.comp != nil && thc.comp != nil
	})
	return e, ch, chZone, thZone
}

func TestFileDropOverlayFollowsThePane(t *testing.T) {
	e, ch, chZone, thZone := dropEnv(t)
	chc, thc := e.a.convoFor(ch, ""), e.a.convoFor(ch, fakeslack.ThreadRootTS)
	if chZone.Overlaps(thZone) {
		t.Fatalf("the panes' zones overlap: channel %v, thread %v", chZone, thZone)
	}
	var d dropDrawn
	d.hook(t)
	team := "Upload to " + e.a.teamName()

	// nothing over the window yet
	e.frame()
	if d.has(team) || d.has("Upload to thread") {
		t.Fatal("an overlay drew before any drag")
	}

	// into the message pane: its overlay, not the thread's
	e.a.fileDrop(dropAt(app.FileDragEnter, centre(chZone), 0, "/tmp/a.png"))
	if e.a.drop.hover != chc {
		t.Fatalf("enter over the message pane: hover %v", e.a.drop.hover)
	}
	d.reset()
	e.frame()
	if !d.has(team) {
		t.Fatalf("the message pane's overlay didn't draw %q", team)
	}
	// the subtitle, word by word around the Shift chip
	for _, w := range []string{"Hold", "Shift", "immediately", "#th-launch-notes"} {
		if !d.has(w) {
			t.Errorf("the subtitle didn't draw %q", w)
		}
	}
	if d.has("Upload to thread") {
		t.Error("the thread's overlay drew over the message pane's drag")
	}

	// across to the thread pane
	e.a.fileDrop(dropAt(app.FileDragOver, centre(thZone), 0))
	if e.a.drop.hover != thc {
		t.Fatalf("over the thread pane: hover %v", e.a.drop.hover)
	}
	d.reset()
	e.frame()
	if !d.has("Upload to thread") {
		t.Fatal("the thread pane's overlay didn't draw")
	}
	if d.has(team) {
		t.Error("the message pane's overlay still drew with the drag over the thread")
	}

	// over the sidebar: no pane, no overlay
	e.a.fileDrop(dropAt(app.FileDragOver, image.Pt(chZone.Min.X-40, chZone.Min.Y+100), 0))
	if e.a.drop.hover != nil {
		t.Fatalf("over the sidebar: hover %v", e.a.drop.hover)
	}
	d.reset()
	e.frame()
	if d.has(team) || d.has("Upload to thread") {
		t.Fatal("an overlay drew with the drag over the sidebar")
	}

	// back over the message pane, then the drag leaves (or Esc)
	e.a.fileDrop(dropAt(app.FileDragOver, centre(chZone), 0))
	e.frame()
	e.a.fileDrop(app.FileDropEvent{Kind: app.FileDragLeave})
	if e.a.drop.on || e.a.drop.hover != nil {
		t.Fatalf("after leave: on %v hover %v", e.a.drop.on, e.a.drop.hover)
	}
	d.reset()
	e.frame()
	if d.has(team) {
		t.Fatal("the overlay stayed after the drag left")
	}
}

func TestFileDropAttachesToThePane(t *testing.T) {
	e, ch, chZone, thZone := dropEnv(t)
	chc, thc := e.a.convoFor(ch, ""), e.a.convoFor(ch, fakeslack.ThreadRootTS)
	dir := t.TempDir()
	file := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("dropped "+name+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	attached := func(c *convo) int {
		if b, ok := c.comp.(composerBox); ok {
			return b.Attached()
		}
		return -1
	}

	// a drop on the message pane: the channel composer's draft
	one := file("drop-one.txt")
	e.a.fileDrop(dropAt(app.FileDragEnter, centre(chZone), 0, one))
	e.frame()
	e.a.fileDrop(dropAt(app.FileDrop, centre(chZone), 0, one))
	e.until("the channel composer attaches the file", 10*time.Second, func() bool { return attached(chc) == 1 })
	if n := attached(thc); n != 0 {
		t.Fatalf("the thread composer has %d attachments after a drop on the message pane", n)
	}
	if e.a.drop.on || e.a.drop.hover != nil {
		t.Fatal("the overlay is still up after the drop")
	}

	// a drop on the thread pane: the reply composer's (no enter first,
	// as WM_DROPFILES sends it)
	two, three := file("drop-two.txt"), file("drop-three.txt")
	e.a.fileDrop(dropAt(app.FileDrop, centre(thZone), 0, two, three))
	e.until("the thread composer attaches both files", 10*time.Second, func() bool { return attached(thc) == 2 })
	if n := attached(chc); n != 1 {
		t.Fatalf("the channel composer has %d attachments, want still 1", n)
	}

	// a folder is refused, the files beside it attach
	sub := filepath.Join(dir, "a-folder")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	four := file("drop-four.txt")
	e.a.fileDrop(dropAt(app.FileDrop, centre(chZone), 0, sub, four))
	e.until("the file beside the folder attaches", 10*time.Second, func() bool { return attached(chc) == 2 })

	// Shift: shared to the conversation at once, the draft untouched
	share := file("drop-share.txt")
	e.a.fileDrop(dropAt(app.FileDrop, centre(chZone), key.ModShift, share))
	e.until("the Shift drop is shared to the channel", 20*time.Second, func() bool {
		if tl := e.a.data.core.S.Timeline(ch); tl != nil {
			for _, m := range tl.Msgs {
				for _, f := range m.Files {
					if f.Name == "drop-share.txt" {
						return true
					}
				}
			}
		}
		return false
	})
	e.settle(200 * time.Millisecond)
	if n := attached(chc); n != 2 {
		t.Fatalf("the channel composer has %d attachments after the Shift drop, want 2", n)
	}

	// a drop outside the panes does nothing
	e.a.fileDrop(dropAt(app.FileDrop, image.Pt(chZone.Min.X-40, chZone.Min.Y+100), 0, file("drop-nowhere.txt")))
	e.settle(300 * time.Millisecond)
	if attached(chc) != 2 || attached(thc) != 2 {
		t.Fatalf("a drop on the sidebar attached something: channel %d thread %d", attached(chc), attached(thc))
	}
}

func TestFileDropArchivedChannelTakesNothing(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	ch := fakeslack.ThreadArchivedID
	e.openChannel(ch)
	c := e.a.convoFor(ch, "")
	e.until("the archived channel's footer", 20*time.Second, func() bool { return e.a.footerFor(c) == footArchived })
	e.settle(200 * time.Millisecond)
	for _, z := range e.a.drop.zones {
		if z.c == c {
			t.Fatal("the archived channel registered a drop zone (FileDragDrop is disabled there)")
		}
	}
	e.a.fileDrop(dropAt(app.FileDragEnter, image.Pt(e.size.X-200, e.size.Y/2), 0, "/tmp/x.png"))
	if e.a.drop.hover == c {
		t.Fatal("the archived channel shows the drop overlay")
	}
}

func TestDropSubtitleWraps(t *testing.T) {
	e, _, _, _ := dropEnv(t)
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	sub := "Hold \x00 to share immediately to #a-rather-long-channel-name"
	one := e.a.dropSubtitleLines(gtx, sub, 4000)
	if len(one) != 1 {
		t.Fatalf("wide: %d lines, want 1", len(one))
	}
	full := one[0].w
	lines := e.a.dropSubtitleLines(gtx, sub, full*2/3)
	if len(lines) < 2 {
		t.Fatalf("narrow: %d lines, want 2 or more", len(lines))
	}
	chip := false
	for _, ln := range lines {
		if ln.w > full*2/3 {
			t.Errorf("a line is %d wide, over %d", ln.w, full*2/3)
		}
		if n := len(ln.parts); n > 0 && (ln.parts[0] == " " || ln.parts[n-1] == " ") {
			t.Errorf("a line starts or ends with a space: %q", ln.parts)
		}
		for _, p := range ln.parts {
			chip = chip || p == "\x00"
		}
	}
	if !chip {
		t.Error("the Shift chip got lost in the wrap")
	}
}

func TestDropTimingAndNight(t *testing.T) {
	for _, c := range []struct{ x, want float64 }{{0, 0}, {1, 1}, {0.5, 0.5}} {
		if got := cubicBezierY(0.42, 0, 0.58, 1, c.x); got < c.want-0.01 || got > c.want+0.01 {
			t.Errorf("ease-in-out(%v) = %v, want %v", c.x, got, c.want)
		}
	}
	if got := cssEase(0.5); got < 0.79 || got > 0.81 { // ease at 50% ≈ 0.8024
		t.Errorf("ease(.5) = %v, want ≈ .80", got)
	}
	day := time.Date(2026, 11, 11, 12, 0, 0, 0, time.Local)
	for h, want := range map[int]bool{7: true, 8: false, 19: false, 20: true, 23: true, 0: true} {
		if got := dropNight(day.Add(time.Duration(h-12) * time.Hour)); got != want {
			t.Errorf("dropNight at %02d:00 = %v, want %v", h, got, want)
		}
	}
}

// TestDropOverlayShots saves the overlays over the message pane and the
// thread pane, light and dark, day and night cards (FILES_SHOTS_DIR: the
// files-shots workflow; drop-*.png).
func TestDropOverlayShots(t *testing.T) {
	if os.Getenv("FILES_SHOTS_DIR") == "" {
		t.Skip("FILES_SHOTS_DIR not set")
	}
	defer func(f func(time.Time) bool) { dropNightAt = f }(dropNightAt)
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e, _, chZone, thZone := dropEnv(t)
			w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
			if err != nil {
				t.Fatalf("headless window: %v", err)
			}
			if e.gpu != nil {
				e.gpu.Release()
			}
			e.gpu = w
			if dark {
				e.a.setDark(true)
			}
			e.settle(600 * time.Millisecond)
			for _, night := range []bool{false, true} {
				dropNightAt = func(time.Time) bool { return night }
				when := map[bool]string{false: "day", true: "night"}[night]
				e.a.fileDrop(dropAt(app.FileDragEnter, centre(chZone), 0, "/tmp/a.png"))
				e.filesShot("drop-message-pane-" + when + "-" + mode)
				e.a.fileDrop(dropAt(app.FileDragOver, centre(thZone), 0))
				e.filesShot("drop-thread-" + when + "-" + mode)
				e.a.fileDrop(app.FileDropEvent{Kind: app.FileDragLeave})
				e.settle(100 * time.Millisecond)
			}
			// the entrance, mid-way (motion on): the cards scaling in
			e.a.motionOverride = -1
			e.a.fileDrop(dropAt(app.FileDragEnter, centre(chZone), 0, "/tmp/a.png"))
			e.frame()
			e.a.drop.since = e.now.Add(-dropOverlayAnim / 2)
			gtx := e.context()
			e.a.frame(gtx)
			e.r.Frame(&e.ops)
			if err := e.gpu.Frame(&e.ops); err != nil {
				t.Fatal(err)
			}
			img := image.NewRGBA(image.Rectangle{Max: e.size})
			if err := e.gpu.Screenshot(img); err != nil {
				t.Fatal(err)
			}
			saveDropShot(t, "drop-message-pane-entrance-"+mode, img)
			e.a.fileDrop(app.FileDropEvent{Kind: app.FileDragLeave})
		})
	}
}

func saveDropShot(t *testing.T, name string, img image.Image) {
	f, err := os.Create(filepath.Join(os.Getenv("FILES_SHOTS_DIR"), name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
