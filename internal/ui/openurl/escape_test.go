package openurl

import (
	"path/filepath"
	"runtime"
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
