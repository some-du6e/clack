// Command clack is a fast, native Slack client (Gio). It was called yoloslack
// until 2026-11-07; config migrates from ~/.config/yoloslack automatically.
package main

import (
	"bufio"
	"context"
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
	for _, rel := range []string{"creds.json", "ui.json", "cache/sidebar.json", "cache/meta.json"} {
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
	// Last line of defence: no token or cookie reaches the log, whatever
	// an error string quotes.
	log.SetOutput(slack.RedactWriter(os.Stderr))
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
		fmt.Fprintf(os.Stderr, "usage: clack [flags] [slack://link]\n       clack login [slack://…/magic-login/… | workspace]   (sign in with a Slack link, or paste a token and d cookie)\n\n")
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

	cfg := configDir()
	// One Clack per user (and config dir): a second launch, or the OS
	// running `clack slack://…` for a link, hands it to the running one.
	link := desktop.URLArg(flag.Args())
	if desktop.Forward(cfg, desktop.Request{URL: link}) {
		return
	}
	images := imgcache.New(imgcache.PublicFetch, filepath.Join(cfg, "cache", "img"), nil)
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
		SignOut: func() {
			setLive(nil)
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
	// metadata cache and the DPoP key, so after sign-in or an account
	// switch it runs off the UI goroutine and only Attach is posted.
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
		st := store.New(cacheDir)
		svc := core.New(c, st)
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
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
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
	return cmd.Run() == nil
}
