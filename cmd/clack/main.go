// Command clack is a fast, native Slack client (Gio). It was called yoloslack
// until 2026-11-07; config migrates from ~/.config/yoloslack automatically.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"

	"github.com/some-du6e/yoloslackgoclient/assets/icon"
	"github.com/some-du6e/yoloslackgoclient/internal/applog"
	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/desktop"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui"
)

var started = time.Now()

// version is set at release builds (-ldflags "-X main.version=v0.1.0").
var version = "dev"

func configDir() string {
	if d := os.Getenv("CLACK_HOME"); d != "" {
		return d
	}
	if d := os.Getenv("YOLOSLACK_HOME"); d != "" { // pre-rename name
		return d
	}
	d, _ := os.UserConfigDir()
	dir := filepath.Join(d, "clack")
	migrateConfig(filepath.Join(d, "yoloslack"), dir)
	return dir
}

// migrateConfig copies the pre-rename config (creds, UI prefs, sidebar and
// metadata caches) into the new directory once. It copies rather than moves
// so older builds and tools reading the old path keep working.
func migrateConfig(old, dir string) {
	if _, err := os.Stat(dir); err == nil {
		return
	}
	if _, err := os.Stat(old); err != nil {
		return
	}
	for _, rel := range []string{"creds.json", "ui.json"} { // caches moved to cacheRoot (v1, per account)
		b, err := os.ReadFile(filepath.Join(old, rel))
		if err != nil {
			continue
		}
		dst := filepath.Join(dir, rel)
		if rel == "creds.json" {
			_ = slack.WriteSecretFile(dst, b) // owner-only (an ACL on Windows)
			_ = slack.RestrictFile(filepath.Join(old, rel))
			continue
		}
		if os.MkdirAll(filepath.Dir(dst), 0o700) == nil {
			_ = os.WriteFile(dst, b, 0o600)
		}
	}
	log.Printf("config: migrated %s to %s", old, dir)
}

// cacheRoot is where the per-account caches live (store.AccountDir):
// CLACK_CACHE_DIR, else <CLACK_HOME>/cache when CLACK_HOME is set (tests
// and the perf harness keep everything in one directory), else the OS's
// user cache directory (~/Library/Caches/Clack, ~/.cache/clack,
// %LocalAppData%\Clack).
func cacheRoot(cfg string) string {
	if d := os.Getenv("CLACK_CACHE_DIR"); d != "" {
		return d
	}
	if os.Getenv("CLACK_HOME") != "" || os.Getenv("YOLOSLACK_HOME") != "" {
		return filepath.Join(cfg, "cache")
	}
	d, err := os.UserCacheDir()
	if err != nil || d == "" {
		return filepath.Join(cfg, "cache")
	}
	name := "Clack"
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		name = "clack"
	}
	return filepath.Join(d, name)
}

// migrateLegacyCache moves the caches older builds kept for whoever was
// signed in last (<config>/cache/{meta.json,img}) into acct, when they
// were that user's (sidebar.json names its self id), and deletes the rest
// of them. Off the UI goroutine.
func migrateLegacyCache(cfg, acct, userID string) {
	old := filepath.Join(cfg, "cache")
	side := filepath.Join(old, "sidebar.json")
	b, err := os.ReadFile(side)
	if err != nil {
		return // nothing from before (or already done)
	}
	var sc struct{ SelfID string }
	mine := json.Unmarshal(b, &sc) == nil && sc.SelfID != "" && sc.SelfID == userID
	if mine && os.MkdirAll(acct, 0o700) == nil {
		if _, err := os.Stat(filepath.Join(acct, "meta.json")); os.IsNotExist(err) {
			_ = os.Rename(filepath.Join(old, "meta.json"), filepath.Join(acct, "meta.json"))
		}
		if _, err := os.Stat(filepath.Join(acct, "img")); os.IsNotExist(err) {
			_ = os.Rename(filepath.Join(old, "img"), filepath.Join(acct, "img"))
		}
	}
	_ = os.Remove(side) // its absence means the migration ran
	log.Printf("cache: moved the old shared cache to the account's (kept: %t)", mine)
	go func() { // what wasn't this user's, or didn't move (a big image tree: not before the window)
		for _, n := range []string{"meta.json", "meta.json.tmp", "sidebar.json.tmp"} {
			_ = os.Remove(filepath.Join(old, n))
		}
		_ = os.RemoveAll(filepath.Join(old, "img"))
	}()
}

func main() {
	// The update helper (a temp copy of Clack started by "Install and
	// restart"): it waits for Clack to exit, installs, relaunches.
	if len(os.Args) == 3 && os.Args[1] == "-apply-update" {
		if err := desktop.ApplyUpdate(os.Args[2]); err != nil {
			os.Exit(1)
		}
		return
	}
	// `clack --update`: download and install the newest release from the
	// terminal, for a Clack that won't open. Before everything else: no
	// window, config, credentials or Slack.
	if wantsUpdateCLI(os.Args[1:]) {
		attachConsole()
		os.Exit(updateCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
	// `clack demo`: the built-in fake workspace (demo.go). Also before
	// everything else: its own config dir, loopback-only network.
	if wantsDemo(os.Args[1:]) {
		attachConsole()
		os.Exit(demoMain(demoArgs(os.Args[1:]), os.Stdout, os.Stderr))
	}
	// The log goes to stderr (as before) and, redacted, to a rotating file
	// in the OS's log folder (applog.Dir: ~/Library/Logs/Clack on macOS),
	// so a Clack started from the Dock keeps its evidence. The file opens
	// on the log writer's goroutine; no line ever waits for the disk. No
	// token or cookie reaches either, whatever an error string quotes.
	logs := applog.Start(os.Stderr)
	// The startup timeline (applog/startup.go) counts from the process
	// start; its debug lines are on for the first minute.
	applog.StartTimeline(started)
	// Go panics nothing recovers and the runtime's fatal errors go to fd 2
	// only; the runtime also writes them to crash.log next to clack.log,
	// and the next start copies the trace, redacted, into clack.log.
	logs.CaptureCrashes(fmt.Sprintf("clack %s %s/%s pid %d, started %s", version, runtime.GOOS, runtime.GOARCH, os.Getpid(), time.Now().Format(time.RFC3339)))
	// Every request to Slack names this build: "Clack/<version> (+…)".
	slack.SetVersion(version)
	// Finder on old macOS versions passes -psn_<n>; the flag package
	// would reject it.
	args := os.Args[:1]
	for _, a := range os.Args[1:] {
		if !strings.HasPrefix(a, "-psn_") {
			args = append(args, a)
		}
	}
	os.Args = args
	// The platform app id: X11 WM_CLASS and the Wayland app_id, matching
	// clack.desktop (its icon and slack:// handler).
	app.ID = "clack"

	startProfiler()
	if startTrim != nil {
		startTrim()
	}
	credsPath := flag.String("creds", filepath.Join(configDir(), "creds.json"), "credentials file")
	dark := flag.Bool("dark", false, "start in dark mode (overridden by the saved preference)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	regURL := flag.Bool("register-url-handler", false, "make Clack the slack:// link handler for this user and exit")
	unregURL := flag.Bool("unregister-url-handler", false, "remove Clack's slack:// handler registration (Windows) and exit")
	noRealtime := flag.Bool("no-realtime", os.Getenv("CLACK_NO_REALTIME") != "", "never open Slack's realtime connection (poll instead), whatever Preferences says; also CLACK_NO_REALTIME=1")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: clack [flags] [slack://link]\n       clack login [slack://…/magic-login/… | workspace]   (sign in with a Slack link, or paste a token and d cookie)\n       clack demo [--speed N] [--reset]   (a built-in fake workspace, offline; clack demo --help)\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("clack", version)
		return
	}

	if *regURL || *unregURL {
		f, what := desktop.RegisterURLHandler, "registered"
		if *unregURL {
			f, what = desktop.UnregisterURLHandler, "unregistered"
		}
		if err := f(); err != nil {
			log.Fatal(err)
		}
		fmt.Println("clack: slack:// handler", what)
		return
	}

	if flag.Arg(0) == "login" {
		if err := loginCLI(*credsPath, flag.Arg(1)); err != nil {
			log.Fatal(err)
		}
		return
	}

	log.Printf("clack %s starting (%s/%s, %s)", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
	cfg := configDir()
	// One Clack per user (and config dir): a second launch, or the OS
	// running `clack slack://…` for a link, hands it to the running one.
	link := desktop.URLArg(flag.Args())
	if desktop.Forward(cfg, desktop.Request{URL: link}) {
		return
	}
	// The image cache's disk tier is the signed-in account's (build sets it).
	images := imgcache.New(imgcache.PublicFetch, "", nil)
	croot := cacheRoot(cfg)
	go store.PurgeOldVersions(croot) // Slack's store_out_of_date: older layouts go
	// The live session. A desktop session's client saves its rotated
	// cookies to creds.json, but only while it is the live one: after a
	// sign-out or an account switch, a late write from the old client must
	// not bring its credentials back.
	var (
		liveMu sync.Mutex
		live   *slack.Client
	)
	setLive := func(c *slack.Client) {
		liveMu.Lock()
		old := live
		live = c
		liveMu.Unlock()
		if old != nil && old != c {
			old.PersistSession(nil)
		}
	}
	opts := ui.Options{ConfigDir: cfg, Dark: *dark, Started: started, NoRealtime: *noRealtime,
		LogDir: logs.Dir(), LogTail: logs.Tail,
		SignOut: func() {
			setLive(nil)
			// the account's images go with its cache (the UI drops the
			// rest: core.App.DropCache)
			if d := images.Dir(); d != "" {
				images.SetDir("")
				go func() { _ = os.RemoveAll(d) }() // <account dir>/img, set by build
			}
			liveMu.Lock()
			_ = os.Remove(*credsPath)
			liveMu.Unlock()
		},
		OnWindowEvent: func(e event.Event) {
			desktop.SetWindowIcon(e, icon.PNG32, icon.PNG64, icon.PNG128, icon.PNG512)
		},
	}
	if desktop.Released(version) || os.Getenv("CLACK_UPDATE_URL") != "" {
		u := newUpdates(filepath.Join(cfg, "cache", "updates"))
		opts.CheckUpdate, opts.DownloadUpdate, opts.InstallUpdate = u.check, u.download, u.install
	}

	creds, err := slack.ReadCreds(*credsPath)
	ui.SetVersion(version)
	a := ui.New(opts, images)
	// A slack:// link is a navigation, except a magic-login link, which
	// is Slack's browser handing "the Slack app" a sign-in: that signs in
	// (on the sign-in screen only; see ui.App.SignInWithLink).
	openLink := func(u string) {
		if slack.IsMagicLink(u) {
			a.SignInWithLink(u)
			return
		}
		a.OpenURL(u)
	}
	inst, lerr := desktop.Listen(cfg, func(r desktop.Request) {
		a.Raise()
		if r.URL != "" {
			openLink(r.URL)
		}
	})
	if lerr != nil {
		log.Printf("desktop: single instance: %v", lerr)
	}
	cacheDir := filepath.Join(cfg, "cache")
	// build makes the session's client, store and core: it reads the
	// boot snapshot and the DPoP key, so after sign-in or an account
	// switch it runs off the UI goroutine and only Attach is posted. At
	// launch it runs before the window opens, which is what lets the first
	// frame paint the account's theme and sidebar.
	build := func(creds slack.Creds) *core.App {
		c := slack.New(creds)
		if creds.Desktop() {
			c.PersistSession(func(nc slack.Creds) error {
				liveMu.Lock()
				defer liveMu.Unlock()
				if live != c {
					return nil
				}
				return slack.WriteCreds(*credsPath, nc)
			})
		}
		setLive(c)
		acct := store.AccountDir(croot, firstNonEmpty(creds.Team, creds.EnterpriseID), creds.UserID)
		migrateLegacyCache(cfg, acct, creds.UserID)
		st := store.New(acct)
		svc := core.New(c, st)
		images.SetDir(filepath.Join(acct, "img"))
		// After core.New (which listens for the session ending): a bound
		// session whose DPoP key won't load sends nothing and goes back to
		// the sign-in screen.
		if err := c.EnableDPoP(filepath.Dir(*credsPath)); err != nil {
			log.Printf("slack: %v", err)
		}
		images.SetFetcher(func(ctx context.Context, raw string) ([]byte, error) {
			// https slack.com hosts (files.slack.com, …) need the session
			// cookie and stay in memory (core.Fetch). Everything else is
			// fetched with no credentials (imgcache.PublicFetch): https only,
			// Slack's CDNs directly, any other host (unfurls, attachment and
			// block images, bot icons, plugin CSS) through Slack's image
			// proxy so a sender never sees your IP address, and never from a
			// private address.
			if c.CredentialsOK(raw) {
				b, _, err := svc.Fetch(ctx, raw)
				return b, err
			}
			return imgcache.PublicFetch(ctx, raw)
		})
		svc.LoadBootCache() // the warm first frame (core/bootcache.go)
		return svc
	}
	connect := func(creds slack.Creds) { a.Attach(build(creds), cacheDir) }
	login := func(in slack.SignInInput) error {
		var (
			c   slack.Creds
			err error
		)
		if in.Link != "" {
			c, err = signInWithLink(in.Link, filepath.Dir(*credsPath))
		} else {
			c, err = slack.SignIn(context.Background(), in)
		}
		if err != nil {
			return err
		}
		if err := slack.WriteCreds(*credsPath, c); err != nil {
			return err
		}
		svc := build(c) // the sign-in goroutine, not the UI's
		a.Post(func() { a.Attach(svc, cacheDir) })
		return nil
	}
	if err == nil {
		connect(creds)
		a.SetLoginHandler(login) // for "Sign out" → sign back in
	} else {
		a.SetLogin(login)
	}
	if link != "" {
		openLink(link)
	}
	// Taut's api.accounts: switch to other saved credentials.
	// Called on the UI goroutine: the file writes and build run off it.
	a.SetSwitchAccount(func(c slack.Creds) {
		go func() {
			if err := slack.CheckCredsHosts(c); err != nil {
				log.Printf("accounts: %v", err)
				return
			}
			if err := slack.WriteCreds(*credsPath, c); err != nil {
				log.Printf("accounts: %v", err)
				return
			}
			svc := build(c)
			a.Post(func() { a.Attach(svc, cacheDir) })
		}()
	})
	if os.Getenv("CLACK_TIMING") != "" || os.Getenv("YOLOSLACK_TIMING") != "" {
		log.Printf("timing: main ready: %v", time.Since(started).Round(100*time.Microsecond))
	}
	var tray *desktop.Tray
	if runtime.GOOS == "windows" { // Slack's notification-area icon
		var terr error
		if tray, terr = desktop.StartTray("Clack", a.Raise, a.Quit); terr != nil {
			log.Printf("desktop: tray: %v", terr)
		}
	}
	go func() {
		err := a.Run()
		liveMu.Lock()
		c := live
		liveMu.Unlock()
		if c != nil {
			c.FlushSession() // a desktop session's last rotated cookies
		}
		tray.Close()
		inst.Close()
		code := 0
		if err != nil {
			log.Print(err)
			code = 1
		}
		logs.Close(time.Second) // what's still queued for the log file
		os.Exit(code)
	}()
	// app.Events replaces app.Main: macOS delivers slack:// links opened
	// while Clack runs (or that launched it) as URLEvents.
	//
	// The loop body runs synchronously inside the OS's open-URL callback,
	// on the main thread (macOS: application:openURLs:, gio_onOpenURI;
	// Windows: the window procedure). a.Raise is Window.Perform, which
	// queues work for the main thread and waits for it, so calling it here
	// deadlocked: Clack beachballed when a browser handed it a sign-in link
	// while it was running (2026-11-08). Do nothing here but hand off.
	for e := range app.Events {
		if u, ok := e.(app.URLEvent); ok && u.URL != nil {
			link := u.URL.String()
			go func() {
				a.Raise()
				openLink(link)
			}()
		}
	}
}

// signInWithLink is desktop-style sign-in: trade a slack:// magic-login
// link for Clack's own session. DPoP keys, when Slack asks for a binding,
// live next to creds.json (dir), owner-only.
func signInWithLink(link, dir string) (slack.Creds, error) {
	sets, err := slack.ParseMagicLink(link)
	if err != nil {
		return slack.Creds{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return slack.LoginMagic(ctx, sets, slack.MagicConfig{DPoPDir: dir})
}

func loginCLI(path, workspace string) error {
	in := bufio.NewReader(os.Stdin)
	ask := func(prompt string) string {
		fmt.Print(prompt)
		v, _ := in.ReadString('\n')
		return strings.TrimSpace(v)
	}
	// askSecret doesn't echo what's typed (tokens and cookies would otherwise
	// sit in the terminal's scrollback).
	askSecret := func(prompt string) string {
		if !setEcho(false) {
			return ask(prompt)
		}
		defer func() { setEcho(true); fmt.Println() }()
		return ask(prompt)
	}
	// `clack login slack://…/magic-login/…`, or the link pasted at the
	// first prompt: desktop-style sign-in, Clack's own session.
	link := workspace
	if link == "" {
		fmt.Println("Sign in with a Slack link: in your browser, open " + "https://slack.com/ssb/add?s=1&v=" + slack.SlackDesktopVersion)
		fmt.Println("and sign in. When Slack offers to open the Slack app, copy that slack:// link (right-click, Copy Link).")
		fmt.Println("(Or press Enter to paste a token and cookie from the browser instead.)")
		link = askSecret("Sign-in link (slack://…): ")
	}
	if slack.IsMagicLink(link) {
		creds, err := signInWithLink(link, filepath.Dir(path))
		if err != nil {
			return err
		}
		if err := slack.WriteCreds(path, creds); err != nil {
			return err
		}
		fmt.Printf("Signed in as %s on %s with Clack's own session. Credentials saved to %s\n", creds.UserID, creds.TeamURL, path)
		return nil
	}
	if link != "" && strings.HasPrefix(strings.ToLower(link), "slack://") {
		return slack.ErrNotMagicLink
	}
	if workspace == "" {
		workspace = ask("Workspace (e.g. hackclub): ")
	}
	fmt.Println("These come from your own signed-in Slack session, the same values other third-party Slack clients use.")
	si := slack.SignInInput{Workspace: workspace}
	fmt.Println("(What you paste isn't shown.)")
	si.Token = askSecret("Token (xoxc-…): ")
	si.OrgToken = askSecret("Org token (xoxc-…, optional, Enterprise Grid only): ")
	si.Cookie = askSecret("d cookie (xoxd-…): ")
	creds, err := slack.SignIn(context.Background(), si)
	if err != nil {
		return err
	}
	if err := slack.WriteCreds(path, creds); err != nil {
		return err
	}
	fmt.Printf("Signed in as %s on %s. Credentials saved to %s\n", creds.UserID, creds.TeamURL, path)
	return nil
}

// setEcho turns terminal echo on or off with stty (Unix terminals only);
// false if it couldn't.
func setEcho(on bool) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	if fi, err := os.Stdin.Stat(); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	arg := "-echo"
	if on {
		arg = "echo"
	}
	cmd := exec.Command("stty", arg)
	cmd.Stdin = os.Stdin
	cmd.WaitDelay = 2 * time.Second // never wait on a child's inherited pipes
	return cmd.Run() == nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
