package core

import (
	"testing"

	"github.com/some-du6e/clack/internal/permapguard"
)

// Every process-wide map that holds an *App is one Release empties: an
// entry keeps its App, and with it its session, reachable for good
// (ci-speed-3's newPerApp maps; karim-1118 found core's listPrefsStates, a
// bare sync.Map no release knew of).
func TestReleaseCoversAppMaps(t *testing.T) {
	rep, err := permapguard.Check(permapguard.Config{Dir: ".", Owner: "App", Release: "Release", Maker: "newPerApp"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range rep.Missing {
		t.Error(m)
	}
	t.Logf("%d newPerApp maps; other maps holding an *App, released: %v", rep.Registered, rep.Checked)
	if rep.Registered < 20 {
		t.Errorf("found %d newPerApp maps: the guard no longer reads how they're made", rep.Registered)
	}
}
