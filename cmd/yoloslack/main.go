// Command yoloslack is a fast, native Slack client (Gio).
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
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/app"

	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui"
)

var started = time.Now()

func configDir() string {
	if d := os.Getenv("YOLOSLACK_HOME"); d != "" {
		return d
	}
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "yoloslack")
}

func main() {
	credsPath := flag.String("creds", filepath.Join(configDir(), "creds.json"), "credentials file")
	dark := flag.Bool("dark", false, "start in dark mode (overridden by the saved preference)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: yoloslack [flags]\n       yoloslack login [workspace]   (store credentials from a d cookie)\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.Arg(0) == "login" {
		if err := loginCLI(*credsPath, flag.Arg(1)); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg := configDir()
	images := imgcache.New(httpFetch, filepath.Join(cfg, "cache", "img"), nil)
	opts := ui.Options{ConfigDir: cfg, Dark: *dark, Started: started}

	creds, err := readCreds(*credsPath)
	if err != nil {
		opts.Login = func(workspace, cookie string) error {
			c, err := slack.Login(context.Background(), workspace, cookie)
			if err != nil {
				return err
			}
			return writeCreds(*credsPath, c)
		}
	}
	a := ui.New(opts, images)
	_ = creds
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
	req.Header.Set("User-Agent", "yoloslack/0.1")
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
	if workspace == "" {
		fmt.Print("Workspace (e.g. hackclub): ")
		workspace, _ = in.ReadString('\n')
		workspace = strings.TrimSpace(workspace)
	}
	fmt.Println("Paste the value of your `d` cookie for slack.com (DevTools → Application → Cookies):")
	cookie, _ := in.ReadString('\n')
	cookie = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(cookie), "d="))
	creds, err := slack.Login(context.Background(), workspace, cookie)
	if err != nil {
		return err
	}
	if err := writeCreds(path, creds); err != nil {
		return err
	}
	fmt.Printf("Signed in as %s on %s. Credentials saved to %s\n", creds.UserID, creds.TeamURL, path)
	return nil
}
