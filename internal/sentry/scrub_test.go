package sentry

import (
	"runtime"
	"strings"
	"testing"
)

// setHome makes Scrub's home directory h for the test.
func setHome(t *testing.T, h string) {
	t.Helper()
	prev := homeDir
	homeDir = func() string { return h }
	t.Cleanup(func() { homeDir = prev })
}

func TestScrub(t *testing.T) {
	setHome(t, "/home/kayla")
	for _, tc := range []struct{ in, want string }{
		// tokens and cookies (applog.Redact's rules)
		{"token xoxc-1234567890-abcdef", "token xoxc-REDACTED"},
		{"dial: Cookie: d=xoxd-abc%2Fdef; d-s=1700000000", "dial: Cookie: REDACTED"},
		{"Authorization: Bearer xoxp-1-2-3", "Authorization: REDACTED"},
		{`{"token":"hunter2"}`, `{"token":"REDACTED"}`},
		{"slack://channel?team=T1&id=C2", "slack://channel?…"},
		// the home directory, either separator, its user's name whole
		{"open /home/kayla/.config/clack/ui.json: denied", "open ~/.config/clack/ui.json: denied"},
		{`C:\x /home/kayla\Downloads\a.png`, `C:\x ~\Downloads\a.png`},
		{"/home/kayla", "~"},
		{"/home/kaylab/x", "/home/<user>/x"},
		// another home: its user's name goes, the rest stays
		{"\t/Users/sam/go/pkg/mod/gioui.org@v0.1/app/os.go:12 +0x1", "\t/Users/<user>/go/pkg/mod/gioui.org@v0.1/app/os.go:12 +0x1"},
		{`at C:\Users\Sam Smith\AppData\Local\Programs\Clack\Clack.exe`, `at C:\Users\<user> Smith\AppData\Local\Programs\Clack\Clack.exe`},
		{`"C:/Users/sam/go/x.go:3"`, `"C:/Users/<user>/go/x.go:3"`},
		// a module path's own /home/ isn't a home
		{"github.com/x/home/pkg.F", "github.com/x/home/pkg.F"},
		{"internal/ui/users/list.go:4", "internal/ui/users/list.go:4"},
		// what has nothing of these stays as it is
		{"component UnstyledButton · rect x 1 y 2 w 3 h 4 dp", "component UnstyledButton · rect x 1 y 2 w 3 h 4 dp"},
		{"", ""},
	} {
		if got := Scrub(tc.in); got != tc.want {
			t.Errorf("Scrub(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
		if got := Scrub(Scrub(tc.in)); got != Scrub(tc.in) {
			t.Errorf("Scrub isn't idempotent on %q: %q", tc.in, got)
		}
	}
}

// On Windows and macOS the home directory matches in any case (their
// file systems ignore it).
func TestScrubHomeCase(t *testing.T) {
	setHome(t, `C:\Users\Kayla`)
	got := Scrub(`c:/users/kayla/AppData/Roaming/clack/crash.log and C:\Users\Kayla\x`)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		if strings.Contains(strings.ToLower(got), "kayla") {
			t.Errorf("Scrub left the home's user: %q", got)
		}
	} else if strings.Contains(got, `C:\Users\Kayla`) {
		t.Errorf("Scrub left the home: %q", got)
	}
}

func TestScrubCrashText(t *testing.T) {
	setHome(t, "/home/kayla")
	for _, tc := range []struct{ in, want string }{
		{`runtime error: index out of range [5] with length 3`, `runtime error: index out of range [5] with length 3`},
		{`bad message "the launch moved to friday, keep it quiet"`, `bad message "…"`},
		{"json: unknown field `secret plans`", "json: unknown field `…`"},
		{`invalid character 'x' looking for beginning of value`, `invalid character 'x' looking for beginning of value`},
		{`no channel C0123ABCD45 for U0ABCDEF12 at 1700000000.123456`, `no channel <id> for <id> at <ts>`},
		{`mail kayla.l+x@example.co.uk bounced`, `mail <email> bounced`},
		{`open /home/kayla/x: xoxc-123-456`, `open ~/x: xoxc-REDACTED`},
	} {
		if got := scrubCrashText(tc.in); got != tc.want {
			t.Errorf("scrubCrashText(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
	long := strings.Repeat("a", 1000)
	if got := []rune(scrubCrashText(long)); len(got) != maxCrashText || got[len(got)-1] != '…' {
		t.Errorf("a long message kept %d characters", len(got))
	}
}
