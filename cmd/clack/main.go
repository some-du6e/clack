// Command clack is a fast, native Slack client (Gio). It was called yoloslack
// until 2026-11-07; config migrates from ~/.config/yoloslack automatically.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/app"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
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
		if os.MkdirAll(filepath.Dir(dst), 0o700) == nil {
			_ = os.WriteFile(dst, b, 0o600)
		}
	}
	log.Printf("config: migrated %s to %s", old, dir)
}

func main() {
	startProfiler()
	if startTrim != nil {
		startTrim()
	}
	credsPath := flag.String("creds", filepath.Join(configDir(), "creds.json"), "credentials file")
	dark := flag.Bool("dark", false, "start in dark mode (overridden by the saved preference)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: clack [flags]\n       clack login [workspace]   (sign in with your token and d cookie)\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("clack", version)
		return
	}

	if flag.Arg(0) == "login" {
		if err := loginCLI(*credsPath, flag.Arg(1)); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg := configDir()
	images := imgcache.New(httpFetch, filepath.Join(cfg, "cache", "img"), nil)
	opts := ui.Options{ConfigDir: cfg, Dark: *dark, Started: started,
		SignOut: func() { _ = os.Remove(*credsPath) }}

	creds, err := readCreds(*credsPath)
	ui.SetVersion(version)
	a := ui.New(opts, images)
	cacheDir := filepath.Join(cfg, "cache")
	connect := func(creds slack.Creds) {
		c := slack.New(creds)
		st := store.New(cacheDir)
		svc := core.New(c, st)
		images.SetFetcher(func(ctx context.Context, raw string) ([]byte, error) {
			// slack.com hosts (files.slack.com, …) need the session cookie and
			// stay in memory; CDNs, Slack's image proxy and unfurl hosts are public.
			if u, err := url.Parse(raw); err == nil {
				h := strings.ToLower(u.Hostname())
				if h == "slack.com" || strings.HasSuffix(h, ".slack.com") {
					b, _, err := svc.Fetch(ctx, raw)
					return b, err
				}
			}
			return httpFetch(ctx, raw)
		})
		a.Attach(svc, cacheDir)
	}
	login := func(in slack.SignInInput) error {
		c, err := slack.SignIn(context.Background(), in)
		if err != nil {
			return err
		}
		if err := writeCreds(*credsPath, c); err != nil {
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
		if err := writeCreds(*credsPath, c); err != nil {
			log.Printf("accounts: %v", err)
			return
		}
		connect(c)
	})
	if os.Getenv("CLACK_TIMING") != "" || os.Getenv("YOLOSLACK_TIMING") != "" {
		log.Printf("timing: main ready: %v", time.Since(started).Round(100*time.Microsecond))
	}
	go func() {
		if err := a.Run(); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

var httpClient = &http.Client{Timeout: 45 * time.Second}

// httpFetch downloads public images (avatars, emoji, unfurl thumbs).
func httpFetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "clack/0.1")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

func readCreds(path string) (slack.Creds, error) {
	var c slack.Creds
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if c.Token == "" {
		return c, fmt.Errorf("no token in %s", path)
	}
	if c.TeamURL == "" {
		c.TeamURL = "https://slack.com/"
	}
	return c, nil
}

func writeCreds(path string, c slack.Creds) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(path, b, 0o600)
}

func loginCLI(path, workspace string) error {
	in := bufio.NewReader(os.Stdin)
	ask := func(prompt string) string {
		fmt.Print(prompt)
		v, _ := in.ReadString('\n')
		return strings.TrimSpace(v)
	}
	if workspace == "" {
		workspace = ask("Workspace (e.g. hackclub): ")
	}
	fmt.Println("These come from your own signed-in Slack session, the same values other third-party Slack clients use.")
	si := slack.SignInInput{Workspace: workspace}
	si.Token = ask("Token (xoxc-…): ")
	si.OrgToken = ask("Org token (xoxc-…, optional, Enterprise Grid only): ")
	si.Cookie = ask("d cookie (xoxd-…): ")
	si.CookieDS = ask("d-s cookie (optional): ")
	creds, err := slack.SignIn(context.Background(), si)
	if err != nil {
		return err
	}
	if err := writeCreds(path, creds); err != nil {
		return err
	}
	fmt.Printf("Signed in as %s on %s. Credentials saved to %s\n", creds.UserID, creds.TeamURL, path)
	return nil
}
