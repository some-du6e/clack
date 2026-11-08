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
	// Last line of defence: no token or cookie reaches the log, whatever
	// an error string quotes.
	log.SetOutput(slack.RedactWriter(os.Stderr))
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
		fmt.Fprintf(os.Stderr, "usage: clack [flags] [slack://link]\n       clack login [workspace]   (sign in with your token and d cookie)\n\n")
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
		opts.CheckUpdate = checkUpdate
	}

	creds, err := slack.ReadCreds(*credsPath)
	ui.SetVersion(version)
	a := ui.New(opts, images)
	inst, lerr := desktop.Listen(cfg, func(r desktop.Request) {
		a.Raise()
		if r.URL != "" {
			a.OpenURL(r.URL)
		}
	})
	if lerr != nil {
		log.Printf("desktop: single instance: %v", lerr)
	}
	if link != "" {
		a.OpenURL(link)
	}
	cacheDir := filepath.Join(cfg, "cache")
	connect := func(creds slack.Creds) {
		c := slack.New(creds)
		if creds.Desktop() {
			slack.SetPublicUserAgent(slack.DesktopUserAgent())
			c.PersistSession(func(nc slack.Creds) error {
				liveMu.Lock()
				defer liveMu.Unlock()
				if live != c {
					return nil
				}
				return slack.WriteCreds(*credsPath, nc)
			})
		} else {
			slack.SetPublicUserAgent("")
		}
		setLive(c)
		st := store.New(cacheDir)
		svc := core.New(c, st)
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
		a.Attach(svc, cacheDir)
	}
	login := func(in slack.SignInInput) error {
		c, err := slack.SignIn(context.Background(), in)
		if err != nil {
			return err
		}
		if err := slack.WriteCreds(*credsPath, c); err != nil {
			return err
		}
		a.Post(func() { connect(c) })
		return nil
	}
	if err == nil {
		connect(creds)
		a.SetLoginHandler(login) // for "Sign out" → sign back in
	} else {
		a.SetLogin(login)
	}
	// Taut's api.accounts: switch to other saved credentials.
	a.SetSwitchAccount(func(c slack.Creds) {
		if err := slack.CheckCredsHosts(c); err != nil {
			log.Printf("accounts: %v", err)
			return
		}
		if err := slack.WriteCreds(*credsPath, c); err != nil {
			log.Printf("accounts: %v", err)
			return
		}
		connect(c)
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
	for e := range app.Events {
		if u, ok := e.(app.URLEvent); ok && u.URL != nil {
			a.Raise()
			a.OpenURL(u.URL.String())
		}
	}
}

// checkUpdate asks GitHub Releases whether a newer Clack is out. It reads
// release metadata only. CLACK_UPDATE_URL points it at another releases
// API (testing); CLACK_UPDATE_AS pretends to be another version.
func checkUpdate(ctx context.Context) (ui.UpdateInfo, bool, error) {
	cur := version
	if v := os.Getenv("CLACK_UPDATE_AS"); v != "" {
		cur = v
	}
	r, err := desktop.LatestRelease(ctx, cur)
	if err != nil {
		return ui.UpdateInfo{}, false, err
	}
	return ui.UpdateInfo{Tag: r.Tag, URL: r.URL}, desktop.Newer(r.Tag, cur), nil
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
	if workspace == "" {
		workspace = ask("Workspace (e.g. hackclub): ")
	}
	fmt.Println("These come from your own signed-in Slack session, the same values other third-party Slack clients use.")
	si := slack.SignInInput{Workspace: workspace}
	fmt.Println("(What you paste isn't shown.)")
	si.Token = askSecret("Token (xoxc-…): ")
	si.OrgToken = askSecret("Org token (xoxc-…, optional, Enterprise Grid only): ")
	si.Cookie = askSecret("d cookie (xoxd-…): ")
	fmt.Println("Clack shares that browser's session. Slack can sign it out everywhere if it sees it from what looks like another browser,")
	fmt.Println("so give Clack the same browser's user agent (run navigator.userAgent in its DevTools console).")
	si.UserAgent = ask("User agent (Mozilla/5.0 …, recommended): ")
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
