// Command yoloslack is a fast Slack client: a Go core serving a
// Slack-identical UI to a local browser window.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/server"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/web"
)

func configDir() string {
	if d := os.Getenv("YOLOSLACK_HOME"); d != "" {
		return d
	}
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "yoloslack")
}

func main() {
	addr := flag.String("addr", "127.0.0.1:7777", "listen address")
	credsPath := flag.String("creds", filepath.Join(configDir(), "creds.json"), "credentials file")
	dev := flag.String("dev", "", "serve the UI from this directory instead of the embedded copy (live reload of web files)")
	app := flag.Bool("app", false, "open the UI in a chromeless app window (Chrome/Chromium/Edge)")
	open := flag.Bool("open", false, "open the UI in the default browser")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: yoloslack [flags]\n       yoloslack login [workspace]   (store credentials from a d cookie)\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.Arg(0) == "login" {
		if err := login(*credsPath, flag.Arg(1)); err != nil {
			log.Fatal(err)
		}
		return
	}

	b, err := os.ReadFile(*credsPath)
	if err != nil {
		log.Fatalf("no credentials at %s — run `yoloslack login` first (%v)", *credsPath, err)
	}
	var creds slack.Creds
	if err := json.Unmarshal(b, &creds); err != nil {
		log.Fatalf("bad credentials file: %v", err)
	}
	if creds.TeamURL == "" {
		creds.TeamURL = "https://slack.com/"
	}
	c := slack.New(creds)
	ctx := context.Background()
	if creds.UserID == "" || creds.Team == "" {
		a, err := c.AuthTest(ctx)
		if err != nil {
			log.Fatalf("auth.test: %v", err)
		}
		creds.UserID, creds.Team = a.UserID, a.TeamID
		if creds.EnterpriseID == "" {
			creds.EnterpriseID = a.EnterpriseID
		}
		c.Creds = creds
	}

	st := store.New(filepath.Join(configDir(), "cache"))
	a := server.NewApp(c, st)
	go func() {
		if err := a.Boot(ctx); err != nil {
			log.Printf("boot failed: %v", err)
		}
	}()

	var webFS fs.FS = web.FS
	if *dev != "" {
		webFS = os.DirFS(*dev)
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	url := "http://" + ln.Addr().String() + "/"
	log.Printf("yoloslack listening on %s", url)
	srv := &http.Server{Handler: a.Handler(webFS), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	if *app {
		go openApp(url)
	} else if *open {
		go openBrowser(url)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	_ = st.SaveCache()
	sctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
}

func login(path, workspace string) error {
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
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(creds, "", "  ")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	fmt.Printf("Signed in as %s on %s. Credentials saved to %s\n", creds.UserID, creds.TeamURL, path)
	return nil
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	_ = cmd.Start()
}

func openApp(u string) {
	candidates := []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"}
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium", "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge", "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser"}
	case "windows":
		pf := os.Getenv("ProgramFiles")
		pf86 := os.Getenv("ProgramFiles(x86)")
		candidates = []string{pf + `\Google\Chrome\Application\chrome.exe`, pf86 + `\Google\Chrome\Application\chrome.exe`, pf86 + `\Microsoft\Edge\Application\msedge.exe`, pf + `\Microsoft\Edge\Application\msedge.exe`}
	}
	profile := filepath.Join(configDir(), "window")
	for _, c := range candidates {
		p, err := exec.LookPath(c)
		if err != nil {
			continue
		}
		cmd := exec.Command(p, "--app="+u, "--user-data-dir="+profile, "--window-size=1280,860", "--no-first-run", "--no-default-browser-check")
		if cmd.Start() == nil {
			return
		}
	}
	log.Printf("no Chromium-based browser found for -app; opening default browser")
	openBrowser(u)
}
