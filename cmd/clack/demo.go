package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"gioui.org/app"
	"gioui.org/io/event"

	"github.com/some-du6e/clack/assets/icon"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/demo"
	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui"
)

// `clack demo` (or `clack --demo`): Clack signed in to Quillfeather Paper
// Co., a built-in fake workspace with scripted bots (internal/demo). It
// runs before anything that reads the real config: it has its own config
// directory (never ~/.config/clack: no creds.json, cache or ui.json of
// yours is read or written), no log file, no update checks, no slack://
// handler, and the process is loopback-only from the first line
// (demo.LockNetwork), so nothing can reach Slack.

// wantsDemo reports whether args ask for the demo: a first argument of
// "demo", or --demo anywhere.
func wantsDemo(args []string) bool {
	if len(args) > 0 && args[0] == "demo" {
		return true
	}
	for _, a := range args {
		if a == "--demo" || a == "-demo" {
			return true
		}
	}
	return false
}

// demoArgs drops the "demo" subcommand and --demo from args.
func demoArgs(args []string) []string {
	var out []string
	for i, a := range args {
		if (i == 0 && a == "demo") || a == "--demo" || a == "-demo" {
			continue
		}
		out = append(out, a)
	}
	return out
}

// demoMarker marks a directory as the demo's, so --reset never deletes
// anything else.
const demoMarker = ".clack-demo"

// demoDir is the demo's config directory: CLACK_DEMO_HOME, else
// <user config dir>/clack-demo.
func demoDir() (string, error) {
	if d := os.Getenv("CLACK_DEMO_HOME"); d != "" {
		return filepath.Abs(d)
	}
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "clack-demo"), nil
}

// realConfigDirs are the directories the real Clack keeps credentials
// in; the demo refuses to use (or reset) any of them.
func realConfigDirs() []string {
	var out []string
	for _, k := range []string{"CLACK_HOME", "YOLOSLACK_HOME"} {
		if d := os.Getenv(k); d != "" {
			if a, err := filepath.Abs(d); err == nil {
				out = append(out, a)
			}
		}
	}
	if d, err := os.UserConfigDir(); err == nil {
		out = append(out, filepath.Join(d, "clack"), filepath.Join(d, "yoloslack"), d)
	}
	if h, err := os.UserHomeDir(); err == nil {
		out = append(out, h)
	}
	return out
}

// checkDemoDir refuses a demo directory that is (or holds) the real
// config, or that has a creds.json in it.
func checkDemoDir(dir string) error {
	clean := filepath.Clean(dir)
	for _, r := range realConfigDirs() {
		r = filepath.Clean(r)
		if clean == r || strings.HasPrefix(r+string(filepath.Separator), clean+string(filepath.Separator)) {
			return fmt.Errorf("refusing demo directory %s: it is (or contains) your real Clack config %s", dir, r)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "creds.json")); err == nil {
		return fmt.Errorf("refusing demo directory %s: it has a creds.json", dir)
	}
	return nil
}

// resetDemoDir deletes the demo directory, only if it carries the marker.
func resetDemoDir(dir string) error {
	if err := checkDemoDir(dir); err != nil {
		return err
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, demoMarker)); err != nil {
		return fmt.Errorf("not resetting %s: it has no %s marker, so it may not be the demo's", dir, demoMarker)
	}
	return os.RemoveAll(dir)
}

// prepareDemoDir creates the directory with its marker and a first
// ui.json (live updates on, #general open) when there is none.
func prepareDemoDir(dir string) error {
	if err := checkDemoDir(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, demoMarker), []byte("Clack demo workspace data (clack demo --reset deletes this folder)\n"), 0o600); err != nil {
		return err
	}
	// the cache is the last run's messages, which were relative to its
	// start: always start from the workspace as it is now
	_ = os.RemoveAll(filepath.Join(dir, "cache"))
	p := filepath.Join(dir, "ui.json")
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(p, []byte(`{"realtime":true,"last_channel":"`+demo.GeneralID+`"}`+"\n"), 0o600)
	}
	return nil
}

const demoUsage = `usage: clack demo [--speed N] [--dark] [--reset] [--quiet]
       clack --demo [flags]

Opens Clack signed in to Quillfeather Paper Co., a built-in fake workspace:
a paper company's branch office with a dozen made-up coworkers, a few days
of backstory and scripted bots that chat, reply in threads, react, type,
change statuses, run polls, ring you for a huddle and stream an AI agent's
work into a code channel. Everything is local and offline: the demo runs
its own fake Slack server on 127.0.0.1, the process refuses every other
host, and it keeps its own settings folder, so your real Slack account,
credentials, cache and preferences are never touched.

Flags:
`

// demoMain is `clack demo`. It returns an exit code for --help and errors;
// otherwise the window's lifetime is the process's.
func demoMain(args []string, stdout, stderr io.Writer) int {
	demo.LockNetwork() // first: from here on, loopback only
	fs := flag.NewFlagSet("clack demo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	speed := fs.Float64("speed", envFloat("CLACK_DEMO_SPEED", 1), "how chatty the bots are: 2 is twice as often, 0.5 half (also CLACK_DEMO_SPEED)")
	dark := fs.Bool("dark", false, "start in dark mode (overridden by a theme chosen in the demo)")
	reset := fs.Bool("reset", false, "forget the demo's saved settings (theme, sidebar, last channel) and start fresh")
	quiet := fs.Bool("quiet", false, "no ambient chatter: the bots only answer you")
	verbose := fs.Bool("v", false, "log what the bots do")
	fs.Usage = func() {
		fmt.Fprint(stderr, demoUsage)
		fs.PrintDefaults()
		dir, _ := demoDir()
		fmt.Fprintf(stderr, "\nSettings folder: %s (CLACK_DEMO_HOME overrides it)\n", dir)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *speed <= 0 || *speed > 100 {
		fmt.Fprintln(stderr, "clack demo: --speed must be more than 0 and at most 100")
		return 2
	}
	log.SetOutput(slack.RedactWriter(stderr)) // no log file: the real one is Clack's
	dir, err := demoDir()
	if err != nil {
		fmt.Fprintln(stderr, "clack demo:", err)
		return 1
	}
	if *reset {
		if err := resetDemoDir(dir); err != nil {
			fmt.Fprintln(stderr, "clack demo:", err)
			return 1
		}
		fmt.Fprintln(stdout, "clack demo: reset", dir)
	}
	if err := prepareDemoDir(dir); err != nil {
		fmt.Fprintln(stderr, "clack demo:", err)
		return 1
	}
	app.ID = "clack-demo"
	opts := demo.Options{Speed: *speed, Quiet: *quiet}
	if *verbose {
		opts.Logf = log.Printf
	}
	sess, err := demo.Start(opts)
	if err != nil {
		fmt.Fprintln(stderr, "clack demo:", err)
		return 1
	}
	log.Printf("clack %s demo (%s/%s): Quillfeather Paper Co. on %s, speed %gx", version, runtime.GOOS, runtime.GOARCH, sess.FS.URL, *speed)
	cacheDir := filepath.Join(dir, "cache")
	images := imgcache.New(sess.Fetch, filepath.Join(cacheDir, "img"), nil)
	banner := "Quillfeather Paper Co. and everyone in it are made up. Nothing here connects to Slack. (clack demo --reset starts over)"
	uopts := ui.Options{ConfigDir: dir, Dark: *dark, Started: started, Demo: banner,
		SignOut: func() {},
		OnWindowEvent: func(e event.Event) {
			desktop.SetWindowIcon(e, icon.PNG32, icon.PNG64, icon.PNG128, icon.PNG512)
		},
	}
	ui.SetVersion(version)
	a := ui.New(uopts, images)
	c, err := sess.NewClient()
	if err != nil { // the session's own fake is always loopback; never reached
		fmt.Fprintln(stderr, "clack demo:", err)
		return 1
	}
	svc := core.New(c, store.New(cacheDir))
	a.Attach(svc, cacheDir)
	// "Sign out" in the demo leads to a sign-in screen that only says so
	a.SetLoginHandler(func(slack.SignInInput) error {
		return errors.New("this is the demo workspace: quit and run clack (without demo) to sign in to Slack")
	})
	go func() {
		err := a.Run()
		sess.Close()
		code := 0
		if err != nil {
			log.Print(err)
			code = 1
		}
		os.Exit(code)
	}()
	for range app.Events { // macOS: keep the main thread's event loop; links are ignored in the demo
	}
	return 0
}

func envFloat(k string, def float64) float64 {
	if v := os.Getenv(k); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
