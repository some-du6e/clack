package views

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoUnsupportedToast: no control under internal/ui answers a click
// with "Not supported yet" (the old cdUnsupported). Slack opens the real
// thing there, or doesn't draw the control (Karim, 2026-11-22: the User
// Groups tab's Create user group).
func TestNoUnsupportedToast(t *testing.T) {
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if strings.Contains(line, `"Not supported yet"`) || strings.Contains(line, "cdUnsupported") {
				t.Errorf("%s:%d: a stub toast: %s", p, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestNoNotYetAvailable: no page under internal/ui says a feature "isn’t
// available in Clack yet" (Directories' External and Invitations were the
// last such pages; both are Slack's hub now). Emoji packs (popups) is the
// one known page left, listed here so a new one fails.
func TestNoNotYetAvailable(t *testing.T) {
	known := map[string]bool{filepath.Join("..", "popups", "emoji_add.go"): true}
	root := filepath.Join("..")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || known[p] {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if notYetAvailable(line) {
				t.Errorf("%s:%d: a not-yet-available page: %s", p, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// notYetRe is a "… available in <the app> yet" page however the app's
// name goes in: views' and popups' appName, package ui's AppName (or
// ui.AppName), an {appName} placeholder, or "Clack" itself.
var notYetRe = regexp.MustCompile(`available in (Clack|"\s*\+\s*(\w+\.)?[Aa]pp[Nn]ame\s*\+\s*"|\{[Aa]pp[Nn]ame\}) yet`)

func notYetAvailable(line string) bool { return notYetRe.MatchString(line) }

func TestNotYetAvailableMatcher(t *testing.T) {
	for line, want := range map[string]bool{
		`"Managing invitations isn’t available in "+appName+" yet."`: true,
		`"Huddles aren’t available in " + AppName + " yet"`:          true,
		`"x isn’t available in "+ui.AppName+" yet"`:                  true,
		`T("Polls aren’t available in {appName} yet")`:               true,
		`"Recording clips isn't available in Clack yet"`:             true,
		`"This field can't be edited in Clack yet"`:                  false,
		`"Available in Clack"`:                                       false,
	} {
		if got := notYetAvailable(line); got != want {
			t.Errorf("%s: %v, want %v", line, got, want)
		}
	}
}
