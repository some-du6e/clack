package ui

import (
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestFocusExpiration(t *testing.T) {
	now := time.Unix(1900000000, 0)
	for _, c := range []struct {
		opt  string
		want int64
	}{{"half_hour", 1900001800}, {"one_hour", 1900003600}, {"two_hours", 1900007200}, {"no_expiration", focusForever}, {"", focusForever}} {
		if got := focusExpiration(c.opt, now); got != c.want {
			t.Errorf("%q: %d want %d", c.opt, got, c.want)
		}
	}
	loc := time.FixedZone("t", 0)
	n := time.Date(2026, 11, 10, 14, 0, 0, 0, loc)
	if got := focusSubLabel(focusForever, n); got != "Notifications paused" {
		t.Errorf("forever: %q", got)
	}
	if got := focusSubLabel(time.Date(2026, 11, 10, 14, 30, 0, 0, loc).Unix(), n); got != "Notifications paused until 2:30 PM" {
		t.Errorf("today: %q", got)
	}
	if got := focusSubLabel(time.Date(2026, 11, 11, 0, 30, 0, 0, loc).Unix(), n); got != "Notifications paused until tomorrow at 12:30 AM" {
		t.Errorf("tomorrow: %q", got)
	}
}

// Turning focus mode on writes focus_mode_expiration_ts (Don’t turn off by
// default); it then quiets the sidebar and swaps in the silent sidebar
// theme, with sidebarInverted from focus_mode_force_dark_mode_enabled;
// turning it off writes 0.
func TestFocusModeToggle(t *testing.T) {
	a, ca, fs := fakeApp(t, map[string]any{"focus_mode_force_dark_mode_enabled": false})
	ca.SetExperimentGroup("silent_sidebar", "on")
	var mu sync.Mutex
	var sets []url.Values
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "users.prefs.set" {
			mu.Lock()
			sets = append(sets, f)
			mu.Unlock()
		}
		return nil, false
	})
	if !a.focusModeOn() || a.focusModeActive() {
		t.Fatalf("on %v active %v", a.focusModeOn(), a.focusModeActive())
	}
	a.toggleFocusMode()
	if !a.focusModeActive() || !a.focusQuiet() {
		t.Fatal("not active right after the toggle (optimistic)")
	}
	waitTheme(t, a, "the write", func() bool { mu.Lock(); defer mu.Unlock(); return len(sets) == 1 && !a.fm().pending })
	mu.Lock()
	if sets[0].Get("name") != "focus_mode_expiration_ts" || sets[0].Get("value") != "9999999999" {
		t.Errorf("write %v", sets[0])
	}
	mu.Unlock()
	if ca.PrefInt("focus_mode_expiration_ts", 0) != focusForever || !a.focusModeActive() {
		t.Error("the pref didn't land")
	}
	ft, ok := a.focusTheme()
	if !ok || ft.Primary.Palette != "gray" || ft.Highlight2.Palette != "mojito" || ft.SidebarInverted || ft.Brightness != 6 {
		t.Errorf("silent sidebar theme %+v %v", ft, ok)
	}
	// a later duration pick writes now + 1 hour
	a.fm().option = "one_hour"
	before := time.Now().Unix()
	a.setFocusExp(focusExpiration("one_hour", time.Now()))
	waitTheme(t, a, "the duration write", func() bool { mu.Lock(); defer mu.Unlock(); return len(sets) == 2 && !a.fm().pending })
	if exp := a.focusExp(); exp < before+3600 || exp > time.Now().Unix()+3600 {
		t.Errorf("one hour: %d", exp)
	}
	a.toggleFocusMode()
	waitTheme(t, a, "the off write", func() bool { mu.Lock(); defer mu.Unlock(); return len(sets) == 3 && !a.fm().pending })
	mu.Lock()
	if sets[2].Get("value") != "0" {
		t.Errorf("off write %v", sets[2])
	}
	mu.Unlock()
	if a.focusModeActive() || a.focusQuiet() {
		t.Error("still active")
	}
	if _, ok := a.focusTheme(); ok {
		t.Error("theme still swapped")
	}
	// without the experiment there is no focus mode at all
	ca.SetExperimentGroup("silent_sidebar", "off")
	a.toggleFocusMode()
	if a.focusModeActive() || a.fm().pending {
		t.Error("toggled without silent_sidebar")
	}
}

// TestFocusPopoverShots renders "You’re in focus mode" and the control
// strip button (off, on) to STATUS_SHOTS_DIR (status-shots.yml).
func TestFocusPopoverShots(t *testing.T) {
	dir := os.Getenv("STATUS_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			a, ca, _ := fakeApp(t, map[string]any{"focus_mode_expiration_ts": time.Now().Add(90 * time.Minute).Unix(), "priority_dnd_override": true})
			ca.SetExperimentGroup("silent_sidebar", "on")
			ca.SetExperimentGroup("availability_prefs_section", "on")
			a.th.Dark = dark
			a.th.Refresh()
			a.fm().subtitle = focusSubtitles[0]
			const scale = 2
			win := image.Pt(420*scale, 360*scale)
			var ops op.Ops
			gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Constraints: layout.Exact(win)}
			paint.FillShape(&ops, a.th.C.SidebarBg, clip.Rect{Max: win}.Op())
			a.focusButton(gtx, image.Pt(8*scale, 8*scale), 36*scale)
			func() {
				defer op.Offset(image.Pt(60*scale, 8*scale)).Push(gtx.Ops).Pop()
				cg := gtx
				cg.Constraints = layout.Constraints{Min: image.Pt(320*scale, 0), Max: image.Pt(320*scale, 340*scale)}
				m := op.Record(gtx.Ops)
				d := a.focusCard(cg)
				call := m.Stop()
				theme.RRect(gtx, image.Rectangle{Max: d.Size}, 12*scale, a.th.C.BgRaised)
				call.Add(gtx.Ops)
			}()
			img := pixeltest.Render(t, win.X, win.Y, &ops)
			saveFocusShot(t, dir, "focus-popover-"+mode, img)
		})
	}
}

func saveFocusShot(t *testing.T, dir, name string, img image.Image) {
	t.Helper()
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

// focus_mode_custom_duration: "Custom" is offered only in the treatment;
// picking it writes today's next half hour an hour on, the date and time
// controls write the pref as they change (a date that makes the time past
// takes the default), and turning focus mode off puts the duration back
// to "Don’t turn off".
func TestFocusCustomDuration(t *testing.T) {
	loc := time.FixedZone("t", 0)
	now := time.Date(2026, 11, 10, 14, 10, 20, 0, loc)
	if d := focusCustomDefault(now); !d.Equal(time.Date(2026, 11, 10, 15, 30, 0, 0, loc)) {
		t.Errorf("default %v", d)
	}
	if d := focusCustomDefault(time.Date(2026, 11, 10, 23, 50, 0, 0, loc)); !d.Equal(time.Date(2026, 11, 11, 0, 30, 0, 0, loc)) {
		t.Errorf("default before midnight %v", d)
	}
	if got := focusDateLabel(time.Date(2026, 11, 1, 0, 0, 0, 0, loc)); got != "Nov 1st, 2026" {
		t.Errorf("date label %q", got)
	}
	lbl := func(t time.Time) string { return t.Format("15:04") }
	opts := focusTimeOptions("2026-11-10", "15:30", now, lbl)
	if len(opts) != 19 || opts[0].value != "14:30" || opts[len(opts)-1].value != "23:30" {
		t.Errorf("today's options %d: %v … %v", len(opts), opts[0], opts[len(opts)-1])
	}
	if opts := focusTimeOptions("2026-11-11", "09:15", now, lbl); len(opts) != 49 || opts[19].value != "09:15" {
		t.Errorf("another day with an off-step time: %d, [19] %v", len(opts), opts[19])
	}

	a, ca, fs := fakeApp(t, nil)
	ca.SetExperimentGroup("silent_sidebar", "on")
	var mu sync.Mutex
	var sets []url.Values
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "users.prefs.set" {
			mu.Lock()
			sets = append(sets, f)
			mu.Unlock()
		}
		return nil, false
	})
	keys := func() []string {
		var out []string
		for _, o := range a.focusOptionList() {
			out = append(out, o.key)
		}
		return out
	}
	ca.SetExperimentGroup("focus_mode_custom_duration", "") // unassigned (core's defaults hold the account's treatment)
	if k := keys(); len(k) != 4 {
		t.Errorf("unassigned: %v", k)
	}
	ca.SetExperimentGroup("focus_mode_custom_duration", "control")
	if k := keys(); len(k) != 4 {
		t.Errorf("control: %v", k)
	}
	ca.SetExperimentGroup("focus_mode_custom_duration", "treatment")
	if k := keys(); len(k) != 5 || k[4] != focusCustom {
		t.Fatalf("treatment: %v", k)
	}
	lastSet := func(n int) url.Values {
		waitTheme(t, a, "write", func() bool { mu.Lock(); defer mu.Unlock(); return len(sets) == n && !a.fm().pending })
		mu.Lock()
		defer mu.Unlock()
		return sets[n-1]
	}
	a.toggleFocusMode()
	lastSet(1)
	wall := time.Now()
	a.pickFocusOption(focusCustom, wall)
	def := focusCustomDefault(wall)
	if f := a.fm(); f.option != focusCustom || f.customDate != def.Format("2006-01-02") || f.customTime != def.Format("15:04") {
		t.Fatalf("custom state %+v", *f)
	}
	if v := lastSet(2).Get("value"); v != strconv.FormatInt(def.Unix(), 10) {
		t.Errorf("custom write %s, want %d", v, def.Unix())
	}
	tomorrow := wall.AddDate(0, 0, 1)
	a.setFocusCustomTime("09:00", wall)
	if v := lastSet(3).Get("value"); v != strconv.FormatInt(focusAt(def.Format("2006-01-02"), "09:00", wall.Location()).Unix(), 10) {
		t.Errorf("time write %s", v)
	}
	a.setFocusCustomDate(tomorrow, wall)
	want := focusAt(tomorrow.Format("2006-01-02"), "09:00", wall.Location())
	if v := lastSet(4).Get("value"); v != strconv.FormatInt(want.Unix(), 10) {
		t.Errorf("date write %s, want %d", v, want.Unix())
	}
	// back to the default date: 09:00 is past (or not: then it stays)
	a.setFocusCustomDate(def, wall)
	got := lastSet(5).Get("value")
	exp := focusAt(def.Format("2006-01-02"), "09:00", wall.Location())
	if exp.Before(wall) {
		exp = def
	}
	if got != strconv.FormatInt(exp.Unix(), 10) {
		t.Errorf("default-date write %s, want %d", got, exp.Unix())
	}
	a.toggleFocusMode() // off
	lastSet(6)
	if f := a.fm(); f.option != "no_expiration" || f.customDate != "" || f.customTime != "" {
		t.Errorf("after off %+v", *f)
	}
}

// TestFocusCustomShots renders the popover with Custom picked
// (STATUS_SHOTS_DIR, status-shots.yml).
func TestFocusCustomShots(t *testing.T) {
	dir := os.Getenv("STATUS_SHOTS_DIR")
	a, ca, _ := fakeApp(t, map[string]any{"focus_mode_expiration_ts": time.Now().Add(90 * time.Minute).Unix()})
	ca.SetExperimentGroup("silent_sidebar", "on")
	ca.SetExperimentGroup("focus_mode_custom_duration", "treatment")
	f := a.fm()
	f.subtitle = focusSubtitles[0]
	f.option = focusCustom
	f.customDate, f.customTime = time.Now().AddDate(0, 0, 1).Format("2006-01-02"), "16:30"
	const scale = 2
	win := image.Pt(360*scale, 420*scale)
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Constraints: layout.Exact(win)}
	paint.FillShape(&ops, a.th.C.SidebarBg, clip.Rect{Max: win}.Op())
	var d layout.Dimensions
	func() {
		defer op.Offset(image.Pt(20*scale, 8*scale)).Push(gtx.Ops).Pop()
		cg := gtx
		cg.Constraints = layout.Constraints{Min: image.Pt(320*scale, 0), Max: image.Pt(320*scale, 400*scale)}
		m := op.Record(gtx.Ops)
		d = a.focusCard(cg)
		call := m.Stop()
		theme.RRect(gtx, image.Rectangle{Max: d.Size}, 12*scale, a.th.C.BgRaised)
		call.Add(gtx.Ops)
	}()
	if f.dateRect.Empty() || f.timeRect.Empty() || f.timeRect.Min.X <= f.dateRect.Max.X {
		t.Errorf("custom row: date %v time %v", f.dateRect, f.timeRect)
	}
	if f.dateRect.Min.Y != f.durRect.Max.Y+8*scale {
		t.Errorf("custom row at %d, the select ends at %d", f.dateRect.Min.Y, f.durRect.Max.Y)
	}
	img := pixeltest.Render(t, win.X, win.Y, &ops)
	saveFocusShot(t, dir, "focus-popover-custom", img)
}
