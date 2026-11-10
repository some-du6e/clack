package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/some-du6e/clack/internal/desktop"
)

// wantsUpdateCLI reports whether args ask for `clack --update`.
func wantsUpdateCLI(args []string) bool {
	for _, a := range args {
		if a == "--update" || a == "-update" {
			return true
		}
	}
	return false
}

// updateCLI is `clack --update [--check] [--yes]`, the way out for a Clack
// that freezes at launch. It runs before anything else in main: no window,
// no config migration, no ui.json, no credentials, no account theme, no
// Slack. It returns the exit code.
func updateCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("clack --update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Bool("update", true, "download and install the newest Clack release, without opening a window")
	check := fs.Bool("check", false, "only report whether a newer release exists")
	yes := fs.Bool("yes", false, "don't ask before installing")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: clack --update [--check] [--yes]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	u := &desktop.Updater{Dir: updateCacheDir(), UserAgent: "clack/" + updateVersion()}
	if raw := os.Getenv("CLACK_UPDATE_URL"); raw != "" {
		if p, err := url.Parse(raw); err == nil && p.Hostname() != "" {
			u.Hosts = append(append([]string(nil), desktop.GitHubHosts...), p.Hostname())
			u.LoopbackHTTP = true
		}
	}
	c := &desktop.CLI{Version: updateVersion(), Check: *check, Yes: *yes, In: stdin, Out: stdout,
		Install: desktop.DetectInstall(), Updater: u}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := c.Run(ctx); err != nil {
		fmt.Fprintf(stderr, "clack: update failed: %v\n", err)
		return 1
	}
	return 0
}

// updateCacheDir is <config>/cache/updates, worked out without
// configDir(): that migrates the old config (creds included), and the CLI
// must not touch it.
func updateCacheDir() string {
	dir := os.Getenv("CLACK_HOME")
	if dir == "" {
		dir = os.Getenv("YOLOSLACK_HOME")
	}
	if dir == "" {
		d, err := os.UserConfigDir()
		if err != nil {
			d = os.TempDir()
		}
		dir = filepath.Join(d, "clack")
	}
	return filepath.Join(dir, "cache", "updates")
}
