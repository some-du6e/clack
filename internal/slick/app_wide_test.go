package slick

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestAppWideMatchesSlick: Clack's appWide is Slick's APP_WIDE
// (src/shared/pluginWorkspaces.ts at the slick workflow's ref): the same
// plugins with the same reasons, which Preferences shows where workspace
// restrictions would go. The copy had gone stale: SelfCare and SlickMCP
// were missing, so Clack offered workspace restrictions Slick doesn't.
func TestAppWideMatchesSlick(t *testing.T) {
	src := os.Getenv("SLICK_SRC")
	if src == "" {
		t.Skip("SLICK_SRC not set: the slick workflow checks Slick out and runs this")
	}
	b, err := os.ReadFile(filepath.Join(src, "src", "shared", "pluginWorkspaces.ts"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)const APP_WIDE[^{]*\{(.*?)\n\};`).FindSubmatch(b)
	if block == nil {
		t.Fatal("no APP_WIDE table in pluginWorkspaces.ts")
	}
	want := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\s*(\w+): '((?:[^'\\]|\\.)*)',?$`).FindAllSubmatch(block[1], -1) {
		want[string(m[1])] = string(m[2])
	}
	if len(want) < 5 {
		t.Fatalf("read only %d APP_WIDE rows: %q", len(want), block[1])
	}
	for id, reason := range want {
		if got := appWide[id]; got != reason {
			t.Errorf("%s: Clack's reason %q, Slick's %q", id, got, reason)
		}
	}
	for id := range appWide {
		if _, ok := want[id]; !ok {
			t.Errorf("%s is app-wide in Clack, not in Slick", id)
		}
	}
}
