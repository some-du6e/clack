//go:build perfharness

package ui

// The memory workflow's scripted session (perf.yml's scale jobs:
// .github/perf/scale.sh on Linux, scale.ps1 on Windows). With
// CLACK_PERF_DRIVE=<dir>, a perfharness build signed in to fakeslackd
// drives itself through a session and, after each stage, writes the
// process's memory to <dir>/<stage>.mem.txt (the Go runtime's
// runtime.MemStats, the OS's counters and the app's caches, as they are),
// <dir>/<stage>.heap.pprof (a heap profile after a GC: inuse_space and
// alloc_space) and <dir>/<stage>.trimmed.txt (the same counters after
// debug.FreeOSMemory). <dir>/done marks the end. Release builds don't
// have this file.
//
// CLACK_PERF_SCRIPT lists the stages, comma-separated (default
// startup,picker,picker-all,scroll,channels,idle):
//
//	users       core.EnsureUsers for every id in CLACK_PERF_USERS (a file,
//	            one per line), as weeks of use learn them; the metadata
//	            cache then holds them all for the next start
//	startup     CLACK_PERF_SETTLE (30 s) after core is ready
//	picker      the emoji picker opened (its first screen drawn)
//	picker-all  … and scrolled through every row of it, CLACK_PERF_PICKER_STEP
//	            (8) rows a frame
//	scroll      each CLACK_PERF_LONG channel (default two of fakeslack's
//	            long ones) paged back to CLACK_PERF_MSGS (3000) messages,
//	            then scrolled down through all of them, CLACK_PERF_SCROLL_STEP
//	            (6) rows a frame
//	channels    CLACK_PERF_CHANNELS (40) more channels opened one after
//	            another, 2 s each
//	idle        left alone for CLACK_PERF_IDLE (10m)
//	gifs        CLACK_PERF_GIF_CHANNEL (fakeslackd -gifs's #gif-wall: large
//	            animated GIFs) paged back to its first message, scrolled
//	            down through every row CLACK_PERF_GIF_STEP (2) rows a
//	            frame, then left showing its newest for CLACK_PERF_GIF_REST
//	            (10 s): only the GIFs on screen play
//	gifs-left   back to the first channel for CLACK_PERF_GIF_LEFT (10 s):
//	            the GIFs' players let go
//	exit        the metadata cache saved, then exit (a priming run)
//
// The driver only calls what a person's clicks and wheel would: Navigate,
// the picker, the lists' scroll positions (messages.PerfScroll*,
// popups.PerfScrollPicker).

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
)

var perfDriveOnce sync.Once

func init() {
	prev := installHooks
	installHooks = func(a *App) {
		if prev != nil {
			prev(a)
		}
		dir := os.Getenv("CLACK_PERF_DRIVE")
		if dir == "" || a.win == nil { // the real window's Run only
			return
		}
		perfDriveOnce.Do(func() { go perfDrive(a, dir) })
	}
}

func perfOpt(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func perfDur(name string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(name)); err == nil {
		return d
	}
	return def
}

func perfInt(name string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(name)); err == nil {
		return n
	}
	return def
}

// perfOnUI runs f on the UI goroutine and waits for it.
func perfOnUI(a *App, f func()) {
	done := make(chan struct{})
	a.Post(func() { f(); close(done) })
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		log.Printf("perfdrive: the UI goroutine didn't run a posted func in 60 s")
	}
}

func perfWait(what string, d time.Duration, cond func() bool) bool {
	for dl := time.Now().Add(d); time.Now().Before(dl); time.Sleep(100 * time.Millisecond) {
		if cond() {
			return true
		}
	}
	log.Printf("perfdrive: gave up waiting for %s after %v", what, d)
	return false
}

func perfDrive(a *App, dir string) {
	t0 := time.Now()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("perfdrive: %v", err)
		return
	}
	logf := func(format string, args ...any) {
		log.Printf("perfdrive: %.0fs "+format, append([]any{time.Since(t0).Seconds()}, args...)...)
	}
	perfWait("core ready", 5*time.Minute, func() bool {
		ready := false
		perfOnUI(a, func() { ready = a.data != nil && !a.data.booted.IsZero() })
		return ready
	})
	logf("core ready")
	script := strings.Split(perfOpt("CLACK_PERF_SCRIPT", "startup,picker,picker-all,scroll,channels,idle"), ",")
	for _, stage := range script {
		stage = strings.TrimSpace(stage)
		logf("stage %s", stage)
		switch stage {
		case "users":
			perfUsers(a, logf)
		case "startup":
			time.Sleep(perfDur("CLACK_PERF_SETTLE", 30*time.Second))
		case "picker":
			perfPicker(a, false, logf)
		case "picker-all":
			perfPicker(a, true, logf)
		case "scroll":
			perfScroll(a, logf)
		case "channels":
			perfChannels(a, logf)
		case "idle":
			perfOnUI(a, func() { a.Navigate("c/" + perfFirstChannel(a)) })
			time.Sleep(perfDur("CLACK_PERF_IDLE", 10*time.Minute))
		case "gifs":
			perfGIFs(a, logf)
		case "gifs-left":
			perfOnUI(a, func() { a.Navigate("c/" + perfFirstChannel(a)) })
			time.Sleep(perfDur("CLACK_PERF_GIF_LEFT", 10*time.Second))
		case "exit":
			logf("saving the metadata cache and exiting")
			var done <-chan struct{}
			perfOnUI(a, func() { done = a.data.stop() })
			if done != nil {
				select {
				case <-done:
				case <-time.After(60 * time.Second):
				}
			}
			_ = os.WriteFile(filepath.Join(dir, "done"), []byte("exit\n"), 0o600)
			os.Exit(0)
		default:
			logf("unknown stage %q", stage)
			continue
		}
		perfSnapshot(a, dir, stage, t0)
		logf("stage %s written", stage)
	}
	_ = os.WriteFile(filepath.Join(dir, "done"), []byte("done\n"), 0o600)
	logf("done")
}

// perfFirstChannel is the sidebar's first channel (fakeslack's
// #announcements).
func perfFirstChannel(a *App) string {
	ids := perfChannelIDs(a)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// perfChannelIDs are the viewer's channels (not DMs), by id.
func perfChannelIDs(a *App) []string {
	c := a.Core()
	if c == nil {
		return nil
	}
	c.S.RLock()
	var ids []string
	for id, cv := range c.S.Convs {
		if cv.IsChannel && !cv.IsArchived {
			ids = append(ids, id)
		}
	}
	c.S.RUnlock()
	sort.Strings(ids)
	return ids
}

func perfUsers(a *App, logf func(string, ...any)) {
	b, err := os.ReadFile(os.Getenv("CLACK_PERF_USERS"))
	if err != nil {
		logf("users: %v", err)
		return
	}
	ids := strings.Fields(string(b))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	for i := 0; i < len(ids); i += 2000 {
		a.Core().EnsureUsers(ctx, ids[i:min(len(ids), i+2000)])
	}
	c := a.Core()
	c.S.RLock()
	n := len(c.S.Users)
	c.S.RUnlock()
	logf("users: %d asked, %d known", len(ids), n)
}

func perfPicker(a *App, all bool, logf func(string, ...any)) {
	if !all {
		perfOnUI(a, func() {
			a.Navigate("c/" + perfFirstChannel(a))
		})
		time.Sleep(2 * time.Second)
		perfOnUI(a, func() {
			if mkEmojiPicker != nil {
				a.ShowOverlay(mkEmojiPicker(a, image.Pt(a.winSize.X/2, a.winSize.Y*3/4), func(string) {}))
			}
		})
		time.Sleep(10 * time.Second) // the index builds; the first screen's images arrive
		return
	}
	var top Overlay
	perfOnUI(a, func() {
		if n := len(a.overlays); n > 0 {
			top = a.overlays[n-1]
		}
	})
	step := perfInt("CLACK_PERF_PICKER_STEP", 8) // under a screen of rows: every row is drawn
	steps := 0
	for ; steps < 20000; steps++ {
		var end, ok bool
		perfOnUI(a, func() { end, ok = popups.PerfScrollPicker(top, step) })
		if !ok || end {
			break
		}
		time.Sleep(50 * time.Millisecond) // a frame or two: each row's emoji are asked for
	}
	logf("picker: %d scroll steps", steps)
	time.Sleep(10 * time.Second)
	perfOnUI(a, func() { a.dismissTop() })
	time.Sleep(2 * time.Second)
}

func perfScroll(a *App, logf func(string, ...any)) {
	var chans []string
	if v := os.Getenv("CLACK_PERF_LONG"); v != "" {
		chans = strings.Split(v, ",")
	} else {
		chans = []string{"C0FAKE00001", "C0FAKE00002"}
	}
	target := perfInt("CLACK_PERF_MSGS", 3000)
	for _, ch := range chans {
		perfOnUI(a, func() { a.Navigate("c/" + ch) })
		perfWait("history "+ch, 60*time.Second, func() bool {
			n := 0
			perfOnUI(a, func() { n, _, _, _ = messages.PerfList(ch) })
			return n > 0
		})
		// page back: infinite scroll asks for one older page a second
		// while the list is at its top
		var n int
		for dl := time.Now().Add(10 * time.Minute); time.Now().Before(dl); time.Sleep(150 * time.Millisecond) {
			var more, loading bool
			perfOnUI(a, func() {
				n, more, loading, _ = messages.PerfList(ch)
				messages.PerfScrollTop(ch)
			})
			if n >= target || (!more && !loading) {
				break
			}
		}
		// … and down through every row
		step := perfInt("CLACK_PERF_SCROLL_STEP", 6)
		steps := 0
		for ; steps < 100000; steps++ {
			end := false
			perfOnUI(a, func() { end = messages.PerfScrollDown(ch, step) })
			if end {
				break
			}
			time.Sleep(40 * time.Millisecond)
		}
		logf("scroll: %s %d messages, %d steps down", ch, n, steps)
	}
	time.Sleep(10 * time.Second)
}

// perfGIFs is the gifs stage: #gif-wall paged back and scrolled through,
// then shown.
func perfGIFs(a *App, logf func(string, ...any)) {
	ch := perfOpt("CLACK_PERF_GIF_CHANNEL", "C0GIFWALL01")
	perfOnUI(a, func() { a.Navigate("c/" + ch) })
	if !perfWait("history "+ch, 60*time.Second, func() bool {
		n := 0
		perfOnUI(a, func() { n, _, _, _ = messages.PerfList(ch) })
		return n > 0
	}) {
		return // a fakeslackd without -gifs
	}
	var n int
	for dl := time.Now().Add(5 * time.Minute); time.Now().Before(dl); time.Sleep(150 * time.Millisecond) {
		var more, loading bool
		perfOnUI(a, func() {
			n, more, loading, _ = messages.PerfList(ch)
			messages.PerfScrollTop(ch)
		})
		if !more && !loading {
			break
		}
	}
	step := perfInt("CLACK_PERF_GIF_STEP", 2) // a GIF's row is tall
	steps := 0
	for ; steps < 20000; steps++ {
		end := false
		perfOnUI(a, func() { end = messages.PerfScrollDown(ch, step) })
		if end {
			break
		}
		time.Sleep(60 * time.Millisecond)
	}
	logf("gifs: %s %d messages, %d steps down", ch, n, steps)
	time.Sleep(perfDur("CLACK_PERF_GIF_REST", 10*time.Second))
}

func perfChannels(a *App, logf func(string, ...any)) {
	long := map[string]bool{}
	for _, ch := range strings.Split(perfOpt("CLACK_PERF_LONG", "C0FAKE00001,C0FAKE00002,C0FAKE00003"), ",") {
		long[ch] = true
	}
	n := perfInt("CLACK_PERF_CHANNELS", 40)
	opened := 0
	for _, ch := range perfChannelIDs(a) {
		if opened >= n {
			break
		}
		if long[ch] {
			continue
		}
		perfOnUI(a, func() { a.Navigate("c/" + ch) })
		perfWait("history "+ch, 20*time.Second, func() bool {
			m := 0
			perfOnUI(a, func() { m, _, _, _ = messages.PerfList(ch) })
			return m > 0
		})
		time.Sleep(2 * time.Second) // its images arrive
		opened++
	}
	logf("channels: opened %d", opened)
	time.Sleep(5 * time.Second)
}

func perfSnapshot(a *App, dir, stage string, t0 time.Time) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "stage %s\nelapsed_s %.0f\n", stage, time.Since(t0).Seconds())
	perfAppCounts(a, &b)
	perfGoMem(&b)
	perfOSMem(&b)
	_ = os.WriteFile(filepath.Join(dir, stage+".mem.txt"), b.Bytes(), 0o600)
	runtime.GC()
	if f, err := os.Create(filepath.Join(dir, stage+".heap.pprof")); err == nil {
		_ = pprof.Lookup("heap").WriteTo(f, 0)
		f.Close()
	}
	if f, err := os.Create(filepath.Join(dir, stage+".goroutine.txt")); err == nil {
		_ = pprof.Lookup("goroutine").WriteTo(f, 1)
		f.Close()
	}
	debug.FreeOSMemory()
	b.Reset()
	fmt.Fprintf(&b, "stage %s (after GC and FreeOSMemory)\n", stage)
	perfGoMem(&b)
	perfOSMem(&b)
	_ = os.WriteFile(filepath.Join(dir, stage+".trimmed.txt"), b.Bytes(), 0o600)
}

func perfGoMem(w io.Writer) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	mb := func(v uint64) string { return fmt.Sprintf("%.1f MB", float64(v)/(1<<20)) }
	fmt.Fprintf(w, "go_heap_alloc %s\ngo_heap_inuse %s\ngo_heap_idle %s\ngo_heap_released %s\ngo_heap_unreturned %s\ngo_heap_sys %s\n",
		mb(m.HeapAlloc), mb(m.HeapInuse), mb(m.HeapIdle), mb(m.HeapReleased), mb(m.HeapIdle-m.HeapReleased), mb(m.HeapSys))
	fmt.Fprintf(w, "go_stack_inuse %s\ngo_stack_sys %s\ngo_mspan_sys %s\ngo_mcache_sys %s\ngo_buckhash_sys %s\ngo_gc_sys %s\ngo_other_sys %s\ngo_sys %s\n",
		mb(m.StackInuse), mb(m.StackSys), mb(m.MSpanSys), mb(m.MCacheSys), mb(m.BuckHashSys), mb(m.GCSys), mb(m.OtherSys), mb(m.Sys))
	pct := debug.SetGCPercent(-1) // reads GOGC (and turns the collector off) …
	debug.SetGCPercent(pct)       // … and puts it back at once
	limit := debug.SetMemoryLimit(-1)
	lim := "none"
	if limit < 1<<62 {
		lim = mb(uint64(limit))
	}
	fmt.Fprintf(w, "go_next_gc %s\ngo_num_gc %d\ngo_heap_objects %d\ngo_goroutines %d\ngo_gc_percent %d\ngo_mem_limit %s\n",
		mb(m.NextGC), m.NumGC, m.HeapObjects, runtime.NumGoroutine(), pct, lim)
}

func perfAppCounts(a *App, w io.Writer) {
	if c := a.Core(); c != nil {
		c.S.RLock()
		users, convs, groups := len(c.S.Users), len(c.S.Convs), len(c.S.Groups)
		c.S.RUnlock()
		tls := c.S.TimelineIDs()
		msgs := 0
		for _, id := range tls {
			if t := c.S.Timeline(id); t != nil {
				msgs += len(t.Msgs)
			}
		}
		fmt.Fprintf(w, "app_users %d\napp_convs %d\napp_usergroups %d\napp_custom_emoji %d\napp_timelines %d\napp_timeline_msgs %d\n",
			users, convs, groups, c.S.CustomEmoji.Len(), len(tls), msgs)
		// the ids sent to users/info so far, where the client counts them
		// (an interface, so the driver still builds on an older side)
		if ua, ok := any(c).(interface{ UsersAsked() int64 }); ok {
			fmt.Fprintf(w, "app_users_asked %d\n", ua.UsersAsked())
		}
	}
	if a.images != nil {
		e, b, l := a.images.PerfStats()
		fmt.Fprintf(w, "app_img_entries %d\napp_img_bytes %.1f MB\napp_img_limit %.1f MB\n", e, float64(b)/(1<<20), float64(l)/(1<<20))
		// animated GIFs (imgcache/anim.go), where the side has them (an
		// interface: the driver still builds on an older side)
		if s, ok := any(a.images).(interface {
			PerfAnimStats() (players, playerBytes, animated, encodedBytes int)
		}); ok {
			p, pb, an, enc := s.PerfAnimStats()
			fmt.Fprintf(w, "app_img_players %d\napp_img_player_bytes %.1f MB\napp_img_animated %d\napp_img_gif_bytes %.1f MB\n",
				p, float64(pb)/(1<<20), an, float64(enc)/(1<<20))
		}
	}
	var lists, rows, laid, models, msgs int
	perfOnUI(a, func() { lists, rows, laid, models, msgs = messages.PerfCounts() })
	fmt.Fprintf(w, "app_list_states %d\napp_list_rows %d\napp_list_rows_laid_out %d\napp_models %d\napp_model_msgs %d\n", lists, rows, laid, models, msgs)
}
