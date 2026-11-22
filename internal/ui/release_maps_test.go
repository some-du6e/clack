package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every package map keyed by *App is in release's list: an entry keeps
// its App, and with it its session, reachable for good once the App is
// done (ci-speed-3: six of fourteen sync.Maps were missing, and a heap
// dump of the ui tests showed them holding finished tests' Apps).
func TestReleaseCoversAppMaps(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	rel, err := os.ReadFile("release.go")
	if err != nil {
		t.Fatal(err)
	}
	keyed := regexp.MustCompile(`(?m)^(?:var\s+|\t)(\w+)\s+(?:sync\.Map\s*// \*App\b|= map\[\*App\])`)
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range keyed.FindAllStringSubmatch(string(b), -1) {
			n++
			if !regexp.MustCompile(`[&(]` + m[1] + `\b`).Match(rel) {
				t.Errorf("%s: %s is keyed by *App, but release (release.go) doesn't drop the App from it", f, m[1])
			}
		}
	}
	if n < 10 {
		t.Errorf("found %d maps keyed by *App: the pattern no longer matches how they're declared", n)
	}
}
