package ui

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"io"
	"log"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/demo"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The introduction's preview renderer: `clack intro-preview`, a process of
// its own that the window starts for the "your Slack, 1:1" beat
// (intro_preview.go). It is `clack demo` without a window: loopback-only
// networking (demo.LockNetwork), the demo workspace on an in-process
// fakeslack, and the whole app (New, Attach, the backend's start) over it
// in a temporary config folder the window made, with the window's theme,
// font and locale in its ui.json. It lays out the app's shell (rail, top
// bar, sidebar, #general with a thread open) headless, and once that has
// settled it renders one frame and writes it to stdout, then exits.
//
// Being its own process is the isolation: whatever the app's code keeps in
// package state (plugin hooks, emoji sets, caches, registries) is the
// renderer's own and goes with it. The App is inert besides (no Dock,
// taskbar or tray badge, no notification or sound), and nothing feeds it
// input.
//
// stdout: the line "CLACKPV1 <w> <h>\n", then w×h RGBA bytes; with -report,
// then "report <json>\n" (tests: introPreviewReport). Logs go to stderr,
// which the window prefixes into its own log.

const introPreviewMagic = "CLACKPV1"

// introPreviewReport is what the renderer saw (tests: -report).
type introPreviewReport struct {
	Calls    map[string]int64 `json:"calls"`    // fakeslack methods called
	Drawn    []string         `json:"drawn"`    // labels drawn in the frame
	Team     string           `json:"team"`     // the sidebar's team
	Sections []string         `json:"sections"` // the sidebar's sections
	Rows     int              `json:"rows"`     // sidebar rows drawn
	Route    string           `json:"route"`    // the route shown
	Thread   bool             `json:"thread"`   // a thread in the secondary pane
	List     string           `json:"list"`     // #general's list's type
	Composer string           `json:"composer"` // #general's composer's type
	Dock     []string         `json:"dock"`     // Dock badges asked for (must be none)
	Dark     bool             `json:"dark"`     // the theme's mode
	Bg       [3]uint8         `json:"bg"`       // the primary panel's colour
	Elapsed  float64          `json:"elapsed"`  // seconds to the frame
	Timeout  bool             `json:"timeout"`  // the frame came at the deadline
	Unread   bool             `json:"unread"`   // #general starts unread (its pill)
}

// RunIntroPreview is `clack intro-preview` (cmd/clack). It returns the
// process's exit code.
func RunIntroPreview(args []string, stdout, stderr io.Writer) int {
	demo.LockNetwork() // first: from here on, loopback only
	log.SetOutput(stderr)
	log.SetFlags(0)
	fs := flag.NewFlagSet("clack intro-preview", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "the config folder the window made (ui.json in it)")
	w := fs.Int("w", 1320, "width in px")
	h := fs.Int("h", 660, "height in px")
	scale := fs.Float64("scale", 1.1, "px per dp")
	spdp := fs.Float64("spdp", 1, "sp per dp")
	report := fs.Bool("report", false, "write what was drawn after the frame (tests)")
	limit := fs.Duration("timeout", 20*time.Second, "render whatever is there after this long")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dir == "" || *w <= 0 || *h <= 0 || *w > 4096 || *h > 4096 || *scale <= 0 {
		fmt.Fprintln(stderr, "intro-preview: -dir and a size up to 4096 px are needed")
		return 2
	}
	t0 := time.Now()
	rep, img, err := renderIntroPreview(*dir, image.Pt(*w, *h), float32(*scale), float32(*spdp), *limit, *report)
	if err != nil {
		fmt.Fprintln(stderr, "intro-preview:", err)
		return 1
	}
	out := bufio.NewWriterSize(stdout, 1<<16)
	fmt.Fprintf(out, "%s %d %d\n", introPreviewMagic, img.Bounds().Dx(), img.Bounds().Dy())
	out.Write(img.Pix)
	if *report {
		rep.Elapsed = time.Since(t0).Seconds()
		b, _ := json.Marshal(rep)
		fmt.Fprintf(out, "report %s\n", b)
	}
	if err := out.Flush(); err != nil {
		return 1
	}
	return 0
}

// renderIntroPreview boots the demo workspace in an inert App and renders
// its settled shell at size px.
func renderIntroPreview(dir string, size image.Point, scale, spdp float32, limit time.Duration, report bool) (*introPreviewReport, *image.RGBA, error) {
	rep := &introPreviewReport{}
	var dockMu sync.Mutex
	dockBadge = func(l string) { dockMu.Lock(); rep.Dock = append(rep.Dock, l); dockMu.Unlock() } // never the OS's
	s, err := demo.Start(demo.Options{Quiet: true, Seed: 1, Read: []string{demo.GeneralID}, Logf: func(string, ...any) {}})
	if err != nil {
		return nil, nil, err
	}
	defer s.Close()
	c, err := s.NewClient()
	if err != nil {
		return nil, nil, err
	}
	images := imgcache.New(s.Fetch, "", nil)
	a := New(Options{ConfigDir: dir, NoRealtime: true}, images)
	a.inert, a.pinAcct = true, true
	a.motionOverride = 1 // a still: no list or hover animations
	cache := filepath.Join(dir, "cache")
	a.Attach(core.New(c, store.New(cache)), cache)
	images.SetInvalidate(func() {})
	a.th.Images = images
	sh := <-a.shaperCh
	a.th.SetShapers(sh[0], sh[1])
	a.data.start(a)
	defer func() {
		select {
		case <-a.data.stop():
		case <-time.After(3 * time.Second):
		}
	}()
	win, err := headless.NewWindow(size.X, size.Y)
	if err != nil {
		return nil, nil, fmt.Errorf("headless window: %w", err)
	}
	defer win.Release()

	var (
		r        input.Router // nothing is ever queued: no input reaches the app
		ops      op.Ops
		threadAt time.Time
		deadline = time.Now().Add(limit)
		drawn    = map[string]bool{}
	)
	layoutOnce := func() {
		a.drain()
		slackc.Pump()
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale * spdp},
			Constraints: layout.Exact(size), Source: r.Source()}
		a.pxPerDp, a.winSize = scale, size
		a.frameSeq.Add(1)
		a.syncThemeTokens()
		client := clsClient.Enter(gtx, 0, 0)
		ia := clsIA.Enter(gtx, 0, 0)
		a.layoutShell(gtx)
		ia.Leave()
		client.Leave()
		r.Frame(&ops)
	}
	for {
		layoutOnce()
		now := time.Now()
		if b := a.data; !b.booted.IsZero() {
			if ch := a.router.Current().Channel(); ch != demo.GeneralID {
				a.Navigate("c/" + demo.GeneralID)
			} else if threadAt.IsZero() {
				if ts := newestThread(a.data.core, demo.GeneralID); ts != "" {
					a.OpenThread(demo.GeneralID, ts)
					threadAt = now
				}
			}
		}
		// settled: the thread's replies, the header's reads and the avatars
		// have had 1.5 s
		if !threadAt.IsZero() && now.Sub(threadAt) > 1500*time.Millisecond {
			break
		}
		if now.After(deadline) {
			rep.Timeout = true
			log.Printf("intro-preview: not settled after %v (booted %v, thread %v)", limit, !a.data.booted.IsZero(), !threadAt.IsZero())
			break
		}
		time.Sleep(16 * time.Millisecond)
	}
	if report {
		prev := theme.DrawHook
		theme.DrawHook = func(s string) { drawn[s] = true }
		defer func() { theme.DrawHook = prev }()
	}
	layoutOnce()
	if err := win.Frame(&ops); err != nil {
		return nil, nil, fmt.Errorf("frame: %w", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: size})
	if err := win.Screenshot(img); err != nil {
		return nil, nil, fmt.Errorf("screenshot: %w", err)
	}
	if report {
		m := a.model()
		rep.Calls = s.FS.CallCounts()
		for k := range drawn {
			rep.Drawn = append(rep.Drawn, k)
		}
		sort.Strings(rep.Drawn)
		rep.Team, rep.Rows, rep.Route = m.TeamName, a.sidebar.rowCount, string(a.router.Current())
		for _, sec := range m.Sections {
			rep.Sections = append(rep.Sections, sec.Name)
		}
		rep.Thread = a.secondary.kind == secThread
		if cv := a.convos[demo.GeneralID+"/"]; cv != nil {
			rep.List, rep.Composer = fmt.Sprintf("%T", cv.list), fmt.Sprintf("%T", cv.comp)
		}
		if ci := m.conv(demo.GeneralID); ci != nil {
			rep.Unread = ci.Unread
		}
		rep.Dark = a.th.Dark
		bg := a.th.C.Bg
		rep.Bg = [3]uint8{bg.R, bg.G, bg.B}
	}
	return rep, img, nil
}

// newestThread is the newest thread root in ch's loaded history ("" none).
func newestThread(c *core.App, ch string) string {
	tl := c.S.Timeline(ch)
	if tl == nil {
		return ""
	}
	for i := len(tl.Msgs) - 1; i >= 0; i-- {
		if m := tl.Msgs[i]; m.ReplyCount > 0 && m.ThreadTS == m.TS {
			return m.TS
		}
	}
	return ""
}
