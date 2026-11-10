package ui

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gioui.org/layout"
	"github.com/some-du6e/clack/internal/applog"
	"github.com/some-du6e/clack/internal/slick"
	"github.com/some-du6e/clack/internal/taut"

	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Getting the log out: Help > Open Logs Folder and Help > Copy Diagnostics
// (macOS menu bar), and the same two buttons in Preferences > Advanced >
// Troubleshooting, next to Show freeze reports.

// diagLogLines is how many log lines Copy Diagnostics includes.
const diagLogLines = 200

// showLogs opens the log folder in Finder (Explorer, the file manager), or
// says why it can't. Call it off the UI goroutine (it stats the folder).
func (a *App) showLogs() {
	dir := a.opts.LogDir
	if dir == "" {
		a.Post(func() { a.Toast("Clack isn't keeping a log file on this computer") })
		return
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		a.Post(func() { a.Toast("Clack couldn't create its logs folder: " + homeTilde(dir)) })
		return
	}
	_ = openurl.OpenFolder(dir)
}

// troubleshootingSection is Preferences > Advanced > Troubleshooting: the
// log, the freeze reports, Copy diagnostics (every OS; macOS also has
// them in the Help menu).
func (p *prefsModal) troubleshootingSection(f *prefsForm) {
	a, gtx := p.a, f.gtx
	f.subheading("Troubleshooting")
	if a.opts.LogDir != "" {
		f.note("Clack keeps a log in "+homeTilde(a.opts.LogDir)+", without message text, tokens or cookies.", true, 2)
	}
	x, bh := gtx.Dp(28), gtx.Dp(28)
	for _, it := range []struct {
		key, label string
		do         func()
	}{
		{"diag_logs", "Open logs folder", func() { go a.showLogs() }},
		{"diag_freeze", "Show freeze reports", func() { go a.showFreezeReports() }},
		{"diag_copy", "Copy diagnostics", a.copyDiagnostics},
	} {
		b := p.btn(it.key)
		hit := b.Clicked(gtx) // popups.Button lays the clickable out: read it first
		d := at(gtx, image.Pt(x, f.y), image.Pt(max(0, f.w-x), bh), func(gtx layout.Context) layout.Dimensions {
			return popups.Button(gtx, a.th, b, popups.BtnOutline, popups.BtnSmall, it.label, "", 0)
		})
		if hit {
			it.do()
		}
		x += d.Size.X + gtx.Dp(8)
	}
	f.y += bh + gtx.Dp(8)
}

// copyDiagnostics puts a short summary for a bug report on the clipboard:
// version, OS, plugin ids and the newest log lines, redacted. UI goroutine;
// the log is read and the summary built off it.
func (a *App) copyDiagnostics() {
	in := diagInfo{
		version:  buildVersion,
		started:  a.opts.Started,
		logDir:   a.opts.LogDir,
		realtime: a.realtimeOn(),
		patched:  slackc.PatchedNames(),
		route:    a.router.Current().logString(),
	}
	t, s, tail := a.Taut(), a.Slick(), a.opts.LogTail
	go func() {
		in.plugins = pluginStates(t, s)
		if tail != nil {
			in.log = tail(diagLogLines)
		}
		text := in.text(time.Now())
		a.Post(func() {
			a.copyText(text)
			a.Toast(fmt.Sprintf("Copied diagnostics: version, OS, plugins and the last %d log lines", len(in.log)))
		})
	}()
}

type diagInfo struct {
	version  string
	started  time.Time
	logDir   string
	realtime bool
	patched  []string
	route    string
	plugins  string
	log      []string
}

// text is the summary. Every line goes through applog.Redact (the log
// lines were already; the rest has nothing to redact, but nothing here
// should ever carry a token).
func (in diagInfo) text(now time.Time) string {
	var b strings.Builder
	v := in.version
	if v == "" {
		v = "dev"
	}
	fmt.Fprintf(&b, "Clack diagnostics\n")
	fmt.Fprintf(&b, "version: %s\n", v)
	fmt.Fprintf(&b, "os: %s/%s, %s, %d CPUs\n", runtime.GOOS, runtime.GOARCH, runtime.Version(), runtime.NumCPU())
	if !in.started.IsZero() {
		fmt.Fprintf(&b, "running for: %v\n", now.Sub(in.started).Round(time.Second))
	}
	fmt.Fprintf(&b, "copied: %s\n", now.Format(time.RFC3339))
	if in.logDir != "" {
		fmt.Fprintf(&b, "log file: %s\n", homeTilde(filepath.Join(in.logDir, applog.FileName)))
	}
	fmt.Fprintf(&b, "live updates: %v\n", onOff(in.realtime))
	fmt.Fprintf(&b, "page: %s\n", in.route)
	plugins := in.plugins
	if plugins == "" {
		plugins = "none"
	}
	fmt.Fprintf(&b, "plugins: %s\n", plugins)
	patched := "none"
	if len(in.patched) > 0 {
		patched = strings.Join(in.patched, ", ")
	}
	fmt.Fprintf(&b, "patched components: %s\n", patched)
	fmt.Fprintf(&b, "\nlast %d log lines:\n", len(in.log))
	for _, l := range in.log {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return applog.Redact(b.String())
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// pluginStates is "slick: chat-tabs (running), foo (off); taut: none":
// installed plugin ids and whether each is running, nothing else. Off the
// UI goroutine (Taut's Settings reads its data store).
func pluginStates(t *taut.Taut, s *slick.Slick) string {
	state := func(enabled, running bool) string {
		switch {
		case running:
			return "running"
		case enabled:
			return "on, not running"
		default:
			return "off"
		}
	}
	join := func(items []string) string {
		if len(items) == 0 {
			return "none"
		}
		return strings.Join(items, ", ")
	}
	var sl, tl []string
	if s != nil {
		for _, p := range s.Settings().Plugins {
			sl = append(sl, fmt.Sprintf("%s (%s)", p.ID, state(p.Active, p.Running)))
		}
	}
	if t != nil {
		for _, c := range t.Settings().Categories {
			for _, p := range c.Plugins {
				tl = append(tl, fmt.Sprintf("%s (%s)", p.ID, state(p.Enabled, p.Running)))
			}
		}
	}
	return "slick: " + join(sl) + "; taut: " + join(tl)
}

// homeTilde shows path with the home directory as ~ (no user name in a
// pasted report).
func homeTilde(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(filepath.Separator)) {
		return "~" + path[len(home):]
	}
	return path
}
