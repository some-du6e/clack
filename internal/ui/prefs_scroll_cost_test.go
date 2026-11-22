package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// scrollLibraryPlugin is one entry of the synthetic 71-plugin library.
type scrollLibraryPlugin struct {
	system, id, name, desc, cat string
	installed                   bool
}

// scrollLibrary is a library the size of the published one (71 plugins
// in the 2026-11-19 release: Taut's and Slick's), across its categories,
// with Slack members credited by mention on every third, a few plugins in
// both systems (one row, two tags), Hack Club-only and off-upstream ones,
// and 4 of them installed (2 Taut, 2 Slick), as Karim has. Synthetic
// names and text, never a real plugin's.
func scrollLibrary() []scrollLibraryPlugin {
	cats := map[string][]string{
		pluginlib.Taut:  {"messages", "messageBox", "people", "privacy", "app", "fun", "other"},
		pluginlib.Slick: {"messages", "composer", "people", "channels", "sounds", "privacy", "appearance", "app", "hackclub", "other"},
	}
	words := []string{"Quiet", "Bright", "Swift", "Tidy", "Lucky", "Calm", "Bold", "Gentle", "Clever", "Rapid", "Cozy", "Sharp"}
	things := []string{"Threads", "Reactions", "Mentions", "Huddles", "Sidebar", "Emoji", "Links", "Drafts", "Status", "Search", "Pins", "Files"}
	var out []scrollLibraryPlugin
	for i := 0; i < 71; i++ {
		sys := pluginlib.Taut
		if i%2 == 1 {
			sys = pluginlib.Slick
		}
		name := words[i%len(words)] + " " + things[(i/len(words)+i)%len(things)] + fmt.Sprintf(" %02d", i)
		if i == 1 || i == 3 || i == 5 { // the same plugin in both systems: one row, both tags
			name = out[i-1].name
		}
		cs := cats[sys]
		desc := "Makes " + strings.ToLower(things[i%len(things)]) + " behave a little differently: a synthetic plugin for the scrolling test, long enough to wrap onto a second line in the library's row."
		out = append(out, scrollLibraryPlugin{system: sys, id: fmt.Sprintf("ScrollLib%02d", i), name: name, desc: desc, cat: cs[i%len(cs)],
			installed: i == 8 || i == 21 || i == 34 || i == 47})
	}
	return out
}

// scrollPluginCode is an installed plugin's bundle: the synthetic plugin
// padded to 64 KB, about a built plugin's size, so reading and hashing
// it costs what a real one does.
func scrollPluginCode(p scrollLibraryPlugin) []byte {
	var code []byte
	if p.system == pluginlib.Taut {
		code = synthTaut(p.id, p.name, p.desc, p.cat, false)
	} else {
		code = synthSlick(p.id, p.name, p.desc, p.cat)
	}
	pad := "\n// " + strings.Repeat("synthetic padding ", 200)
	for len(code) < 64<<10 {
		code = append(code, pad...)
	}
	return code
}

// publishScrollLibrary writes the synthetic library's index.
func publishScrollLibrary(l *libServer, ps []scrollLibraryPlugin) {
	src := map[string]pluginlib.Source{
		pluginlib.Taut:  {Name: "Taut", Repo: "https://github.com/jeremy46231/taut", Commit: "6fe5123e3b4cffaa", License: "GPL-3.0"},
		pluginlib.Slick: {Name: "Slick", Repo: "https://github.com/3kh0/slick", Commit: "f52d55dbd458feb0", License: "GPL-3.0"},
	}
	ix := pluginlib.Index{Schema: 1, Sources: src}
	for i, p := range ps {
		code := []byte(p.id)
		if p.installed {
			code = scrollPluginCode(p)
		}
		e := pluginlib.Entry{System: p.system, ID: p.id, Name: p.name, Description: p.desc, Category: p.cat,
			File: pluginlib.File{File: p.system + "-" + p.id + ".js", SHA256: pluginlib.Hash(code)}}
		switch {
		case i%3 == 0:
			e.Authors = shotAuthors // two members by mention and a plain name
		case i%3 == 1:
			e.Authors = []pluginlib.Author{{Name: "Synthetic Author", URL: "https://example.com"}}
		}
		e.HackClubOnly = p.cat == "hackclub"
		e.UpstreamDisabled = i%17 == 16
		ix.Plugins = append(ix.Plugins, e)
	}
	b, _ := json.Marshal(ix)
	l.mu.Lock()
	l.files["index.json"] = b
	l.mu.Unlock()
}

// notchStats is what one wheel notch's frames cost.
type notchStats struct {
	frames int
	costs  []time.Duration // each frame, layout to the end of the router's frame
	rows   []int           // library rows laid out in each frame
	reads  int64           // install-state lookups during the notch
}

func durMS(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// TestPrefsLibraryScrollCost (Karim, α.112 on Windows, 2026-11-19:
// Preferences > Plugins > Plugin library "scrolling here feels wierd",
// then "laggy?" and "it feels capped"): the library's 71 plugins (4
// installed) at 1918x1080, 1x, light, scrolled by mouse-wheel notches the
// way Windows sends them (100 dp, Notched: Gio patch 9 animates each over
// Chromium's 200 ms) at a 60 Hz display's pace. It logs every notch's
// frames, their cost (layout and the router's frame; with CLACK_GPU_COST
// the GPU's too, whole and partial, and what the partial frame repaints),
// the library rows each frame lays out and the install-state lookups
// (each reads and hashes the plugin's files), and with
// CLACK_SCROLL_PROFILE a CPU profile of the notches. Slack's prefs pane
// scrolls on the compositor: no layout, the display's rate. It requires a
// scroll frame to lay out only the rows on screen (and a few past it), to
// look up no install state, to cost at most scrollFrameBudget at the
// median, a notch to get the frames a 60 Hz display shows (10 of its 12),
// and the partial frame to repaint nothing outside the dialog.
func TestPrefsLibraryScrollCost(t *testing.T) {
	ls := newLibServer(t)
	ps := scrollLibrary()
	publishScrollLibrary(ls, ps)
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	t.Setenv("CLACK_HOME", home) // Taut's and Slick's dirs
	e := startEnv(t, fs, home)
	if e.gpu != nil { // CLACK_BENCH_GPU's window is 1440x900: the layout is measured alone
		e.gpu.Release()
		e.gpu = nil
	}
	e.size = image.Pt(1918, 1080)
	e.waitReady()
	e.until("Taut and Slick", 60*time.Second, func() bool { return e.a.Taut() != nil && e.a.Slick() != nil })
	tt, sl := e.a.Taut(), e.a.Slick()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for _, p := range ps {
		if !p.installed {
			continue
		}
		code := scrollPluginCode(p)
		if p.system == pluginlib.Taut {
			if _, err := tt.Import(ctx, code, ""); err != nil {
				t.Fatalf("install %s: %v", p.id, err)
			}
			_ = tt.SetEnabled(ctx, p.id, true)
		} else {
			if _, err := sl.Import(ctx, code); err != nil {
				t.Fatalf("install %s: %v", p.id, err)
			}
			_ = sl.SetEnabled(ctx, p.id, true)
		}
	}
	e.a.setDark(false)
	e.settle(300 * time.Millisecond)
	var pm *prefsModal
	prefsTap = func(p *prefsModal) { pm = p }
	t.Cleanup(func() { prefsTap = nil })
	e.a.showPrefs(pluginsTabID)
	e.until("Preferences > Plugins", 10*time.Second, func() bool { return pm != nil && pm.plugins != nil })
	pp := pm.plugins
	pp.setMode("browse")
	e.until("the plugin library", 30*time.Second, func() bool { return pp.lib.ix != nil && !pp.lib.loading })
	e.settle(1500 * time.Millisecond)

	// what a frame lays out: the rows (each draws its plugin's name) and
	// the install-state lookups through the systems' hosts
	names := map[string]bool{}
	for _, p := range ps {
		names[p.name] = true
	}
	hosts := map[string]libraryHost{}
	for _, sys := range []string{pluginlib.Taut, pluginlib.Slick} {
		if h, ok := pp.lib.host(sys); ok {
			hosts[sys] = h
		}
	}
	var reads atomic.Int64
	pp.lib.hosts = func(sys string) (libraryHost, bool) {
		h, ok := hosts[sys]
		if ok && h.installed != nil {
			in := h.installed
			h.installed = func(id string) (string, string) { reads.Add(1); return in(id) }
		}
		return h, ok
	}
	installed := 0
	for _, g := range libraryGroups(libraryEntries(pp.lib.ix)) {
		in, _ := pp.lib.groupStates(g)
		installed += len(in)
	}
	if installed != 4 {
		t.Fatalf("%d library plugins installed, want 4", installed)
	}
	var drawnMu sync.Mutex
	drawn := map[string]bool{}
	prevHook := theme.DrawHook
	theme.DrawHook = func(s string) {
		if names[s] {
			drawnMu.Lock()
			drawn[s] = true
			drawnMu.Unlock()
		}
	}
	t.Cleanup(func() { theme.DrawHook = prevHook })
	rowsDrawn := func() int {
		drawnMu.Lock()
		defer drawnMu.Unlock()
		n := len(drawn)
		clear(drawn)
		return n
	}

	// the pointer over the page (the 800x700 dialog is centred, its page
	// 523 wide from 249 in, under the 76px header)
	pt := f32.Pt(float32(e.size.X/2+110), float32(e.size.Y/2+60))
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt})
	e.frame()
	e.frame()
	rowsDrawn()
	if prof := os.Getenv("CLACK_SCROLL_PROFILE"); prof != "" {
		f, err := os.Create(filepath.Join(prof, "prefs-library-scroll.cpu.pprof"))
		if err != nil {
			t.Fatal(err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			t.Fatal(err)
		}
		defer func() { pprof.StopCPUProfile(); f.Close() }()
	}
	const vsync = time.Second / 60
	var notches []notchStats
	for n := 0; n < 10; n++ {
		e.r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: pt, Scroll: f32.Pt(0, 100), Notched: true})
		var ns notchStats
		r0 := reads.Load()
		vblank := time.Now().Add(vsync)
		for i := 0; i < 120; i++ {
			t0 := time.Now()
			e.frame()
			c := time.Since(t0)
			ns.frames++
			ns.costs = append(ns.costs, c)
			ns.rows = append(ns.rows, rowsDrawn())
			if at, ok := e.r.WakeupTime(); !ok || at.After(time.Now().Add(vsync)) {
				break // the notch's animation ended: no frame asked for
			}
			// a 60 Hz display shows the frame at the next vblank after it's
			// done: one that misses a vblank waits for the one after (a
			// frame a little over 16.7 ms halves the rate)
			for done := time.Now(); vblank.Before(done); {
				vblank = vblank.Add(vsync)
			}
			time.Sleep(time.Until(vblank))
			vblank = vblank.Add(vsync)
		}
		ns.reads = reads.Load() - r0
		notches = append(notches, ns)
	}
	var all []time.Duration
	maxRows, frames := 0, 0
	var lookups int64
	for i, ns := range notches {
		var ms []string
		for _, c := range ns.costs {
			ms = append(ms, fmt.Sprintf("%.1f", durMS(c)))
		}
		t.Logf("notch %d: %d frames, %s ms; rows laid out %v; %d install-state lookups", i+1, ns.frames, strings.Join(ms, " "), ns.rows, ns.reads)
		all = append(all, ns.costs...)
		frames += ns.frames
		lookups += ns.reads
		for _, r := range ns.rows {
			maxRows = max(maxRows, r)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	med, p90 := all[len(all)/2], all[len(all)*9/10]
	t.Logf("%d notches: %.1f frames a notch (a 60 Hz display shows 12 in Chromium's 200 ms, 6 when a frame misses its vblank), a frame %.1f ms at the median, %.1f at p90, %.1f at most; up to %d of %d plugins' rows laid out in a frame; %d install-state lookups",
		len(notches), float64(frames)/float64(len(notches)), durMS(med), durMS(p90), durMS(all[len(all)-1]), maxRows, len(ps), lookups)
	if maxRows > 24 {
		t.Errorf("a scroll frame laid out %d of the library's rows; want the ones on screen (about 7) and a few past it", maxRows)
	}
	if lookups > 0 {
		t.Errorf("%d install-state lookups while scrolling (each reads and hashes a plugin's files); want none", lookups)
	}
	if med > scrollFrameBudget {
		t.Errorf("a scroll frame costs %.1f ms at the median; want at most %v", durMS(med), scrollFrameBudget)
	}
	if fpn := float64(frames) / float64(len(notches)); fpn < 10 {
		t.Errorf("%.1f frames a notch; a 60 Hz display shows 12 in its 200 ms", fpn)
	}
	if os.Getenv("CLACK_GPU_COST") == "" {
		return
	}
	// the GPU's part: two scroll frames 8 px apart, drawn whole and as a
	// partial frame (Gio patch 46), the backend's time and what the
	// partial one repaints
	w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
	if err != nil {
		t.Fatalf("headless GPU: %v", err)
	}
	defer w.Release()
	defer damageModes(false, false)
	var a, b op.Ops
	now := time.Now()
	pp.s.list.Position.Offset += 8
	e.layInto(&a, now, nil)
	pp.s.list.Position.Offset += 8
	e.layInto(&b, now, nil)
	damageModes(false, false)
	var whole []float64
	for i := 0; i < 5; i++ {
		damageDraw(t, w, &a)
		st, ms := damageDraw(t, w, &b)
		whole = append(whole, ms)
		if i == 0 {
			t.Logf("a scroll frame drawn whole: %s", costLine(st))
		}
	}
	damageModes(true, false)
	var part []float64
	var dpx int64
	for i := 0; i < 5; i++ {
		damageDraw(t, w, &a)
		st, ms := damageDraw(t, w, &b)
		part = append(part, ms)
		_, _, px := damageOf(st)
		dpx = max(dpx, px)
		if i == 0 {
			t.Logf("a scroll frame drawn partial: %s; %s", damagePart(st), costLine(st))
		}
	}
	sort.Float64s(whole)
	sort.Float64s(part)
	dialog := int64(800 * 700)
	t.Logf("the GPU: a scroll frame %.1f ms whole, %.1f ms partial (median of 5), repainting %d px (the dialog is %d, the window %d)",
		whole[2], part[2], dpx, dialog, int64(e.size.X*e.size.Y))
	if dpx > dialog {
		t.Errorf("a scroll frame repaints %d px, more than the dialog's %d: something behind the modal redraws", dpx, dialog)
	}
	if dir := os.Getenv("CLACK_GPU_COST_SHOTS"); dir != "" {
		damageModes(false, false)
		damageDraw(t, w, &b)
		img := damageShot(t, w)
		f, err := os.Create(filepath.Join(dir, "prefs-library-scroll.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
}

// scrollFrameBudget is what one scroll frame may cost at the median on
// CI's runners: half a 60 Hz frame, room for the GPU's part.
const scrollFrameBudget = 8 * time.Millisecond
