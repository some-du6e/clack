package desktop

import (
	"slices"
	"testing"
)

// A URL handler's launch is the program and the link: whatever a quote in
// the link split off after it never reaches the flags, --update or demo.
// Other launches pass untouched.
func TestURLLaunchArgs(t *testing.T) {
	for _, c := range []struct{ in, want []string }{
		{[]string{"clack"}, []string{"clack"}},
		{[]string{"clack", "-dark"}, []string{"clack", "-dark"}},
		{[]string{"clack", "--update", "--yes"}, []string{"clack", "--update", "--yes"}},
		{[]string{"clack", "demo", "slack://x"}, []string{"clack", "demo", "slack://x"}},
		{[]string{"Clack.exe", "slack://T08PSQ7BQ/magic-login/abc"}, []string{"Clack.exe", "slack://T08PSQ7BQ/magic-login/abc"}},
		{[]string{"Clack.exe", "slack://x/", "--update", "--yes"}, []string{"Clack.exe", "slack://x/"}},
		{[]string{"Clack.exe", "SLACK:open", "-creds", `\\host\share\creds.json`}, []string{"Clack.exe", "SLACK:open"}},
		{[]string{"clack", "slack://x", "demo"}, []string{"clack", "slack://x"}},
	} {
		if got := URLLaunchArgs(c.in); !slices.Equal(got, c.want) {
			t.Errorf("URLLaunchArgs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// the result can't grow into what the caller's slice still holds
	in := []string{"Clack.exe", "slack://x", "--update"}
	_ = append(URLLaunchArgs(in), "-dark")
	if in[2] != "--update" {
		t.Errorf("appending to the result overwrote the input: %q", in)
	}
	if IsURLLaunch([]string{"clack"}) || !IsURLLaunch([]string{"clack", "Slack://open"}) {
		t.Error("IsURLLaunch")
	}
}
