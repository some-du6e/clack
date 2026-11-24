package openurl

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
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
	launchWith(shell, tool, true)
	launchWith(shell, tool, true)
	shellOK, toolOK = true, false
	launchWith(shell, tool, true)
	launchWith(shell, tool, true)
	if got, want := strings.Join(calls, " "), "shell rundll32 rundll32 rundll32 shell shell"; got != want {
		t.Errorf("the ways tried: %q, want %q", got, want)
	}
	calls = nil
	launchWith(func() error { calls = append(calls, "shell"); return errNoShell }, tool, true)
	if got := strings.Join(calls, " "); got != "shell rundll32" {
		t.Errorf("off Windows: %q, want the tool after errNoShell", got)
	}
	// macOS: a failed hand-off is tried once more the same way, never the tool
	calls, shellOK = nil, false
	launchWith(shell, tool, false)
	if got := strings.Join(calls, " "); got != "shell shell" {
		t.Errorf("macOS: %q, want the shell twice", got)
	}
}

// median is the middle of ds (the hand-off timing tests).
func median(ds []time.Duration) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[len(s)/2]
}

// TestCheckRefusesInvalidUTF8: a link whose text isn't UTF-8 (plugin JS
// can make one through window.open or an anchor's href) is refused on
// every OS; on macOS it would have been a nil NSString.
func TestCheckRefusesInvalidUTF8(t *testing.T) {
	if _, err := Check("https://example.com:70000/?q=\xed\xa0\x80"); err != ErrRefused {
		t.Errorf("Check of a surrogate's bytes: %v, want ErrRefused", err)
	}
}

// TestNSURLPrecursor: Chromium's NSURLWithGURL escaping before NSURL
// (macOS 11-13 refuse these raw): the path, query and ref escaped with
// kNSURLCharmap, %XX kept, the authority untouched.
func TestNSURLPrecursor(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"https://grafana.test/d/x?var-x={a,b}", "https://grafana.test/d/x?var-x=%7Ba,b%7D"},
		{"https://maps.test/?q=a|b^c", "https://maps.test/?q=a%7Cb%5Ec"},
		{"https://x.test/?q=café", "https://x.test/?q=caf%C3%A9"},
		{"https://x.test/p%41/[1]?q=%zz#frag[2]#x", "https://x.test/p%41/%5B1%5D?q=%25zz#frag%5B2%5D%23x"},
		{"https://[::1]:8080/a~b", "https://[::1]:8080/a~b"},
		{"https://x.test", "https://x.test"},
		{"mailto:a@example.test?subject=hi%20there", "mailto:a@example.test?subject=hi%20there"},
	} {
		if got := nsURLPrecursor(c.in); got != c.want {
			t.Errorf("nsURLPrecursor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRunToolMailtoFallback: a mailto: link goes to xdg-email, and to
// xdg-open when xdg-email isn't installed (an xdg-open replacement
// without xdg-utils); fake tools on PATH record what they got.
func TestRunToolMailtoFallback(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		t.Skip("xdg-utils only")
	}
	dir := t.TempDir()
	tool := func(name string) string {
		out := filepath.Join(dir, name+".argv")
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho \"$@\" > '"+out+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return out
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return strings.TrimSpace(string(b)) }
	t.Setenv("PATH", dir)
	const u = "mailto:a@example.test"
	openArgv := tool("xdg-open")
	if err := runTool(u); err != nil {
		t.Fatalf("no xdg-email: %v", err)
	}
	if got := read(openArgv); got != u {
		t.Errorf("no xdg-email: xdg-open got %q, want %q", got, u)
	}
	if err := os.Remove(openArgv); err != nil {
		t.Fatal(err)
	}
	emailArgv := tool("xdg-email")
	if err := runTool(u); err != nil {
		t.Fatalf("with xdg-email: %v", err)
	}
	if got := read(emailArgv); got != u {
		t.Errorf("xdg-email got %q, want %q", got, u)
	}
	if _, err := os.Stat(openArgv); err == nil {
		t.Error("xdg-open ran though xdg-email is there")
	}
}
