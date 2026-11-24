package openurl

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestEscapeExternalHandlerValue: Chromium's EscapeExternalHandlerValue,
// which Electron's shell.openExternal runs a URL through before Windows'
// ShellExecuteEx sees it (openurl_windows.go).
func TestEscapeExternalHandlerValue(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"https://example.test/a/b?q=1&r=2#frag", "https://example.test/a/b?q=1&r=2#frag"},
		{"https://example.test/[x]!'()*-._~;:@=+$,", "https://example.test/[x]!'()*-._~;:@=+$,"},
		{"https://example.test/a|b^c{d}`e", "https://example.test/a%7Cb%5Ec%7Bd%7D%60e"},
		{"https://example.test/%41%zz", "https://example.test/%41%25zz"},
		{"https://example.test/a%41", "https://example.test/a%41"},
		{"https://example.test/a%4", "https://example.test/a%254"}, // a % without two hex digits after it
		{"https://example.test/é", "https://example.test/%C3%A9"},
		{"mailto:a@example.test?subject=hi%20there", "mailto:a@example.test?subject=hi%20there"},
	} {
		if got := escapeExternalHandlerValue(c.in); got != c.want {
			t.Errorf("escapeExternalHandlerValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestCommandLinux: off Windows and macOS a link goes where Electron's
// platform_util_linux.cc OpenExternal sends it: mailto: to xdg-email,
// everything else to xdg-open.
func TestCommandLinux(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("xdg-utils only")
	}
	for u, tool := range map[string]string{"https://example.test/a": "xdg-open", "mailto:a@example.test": "xdg-email"} {
		if c := command(u); filepath.Base(c.Path) != tool && c.Args[0] != tool {
			t.Errorf("%s goes to %q, want %s", u, c.Args, tool)
		}
	}
}

// TestLaunchRemembersTheWayThatWorked: as Slack's openExternal flips
// isOpenExternalShellForced after a retry worked, a link whose first way
// failed and whose retry opened it makes the retry's way go first for
// the next links; off Windows (errNoShell) the tool is the only way.
func TestLaunchRemembersTheWayThatWorked(t *testing.T) {
	t.Cleanup(func() { rundllFirst.Store(false) })
	var calls []string
	shellOK, toolOK := false, true
	shell := func() error {
		calls = append(calls, "shell")
		if shellOK {
			return nil
		}
		return errors.New("ShellExecuteEx failed")
	}
	tool := func() error {
		calls = append(calls, "rundll32")
		if toolOK {
			return nil
		}
		return errors.New("rundll32 failed")
	}
	launchWith(shell, tool)
	launchWith(shell, tool)
	shellOK, toolOK = true, false
	launchWith(shell, tool)
	launchWith(shell, tool)
	if got, want := strings.Join(calls, " "), "shell rundll32 rundll32 rundll32 shell shell"; got != want {
		t.Errorf("the ways tried: %q, want %q", got, want)
	}
	calls = nil
	launchWith(func() error { calls = append(calls, "shell"); return errNoShell }, tool)
	if got := strings.Join(calls, " "); got != "shell rundll32" {
		t.Errorf("off Windows: %q, want the tool after errNoShell", got)
	}
}
