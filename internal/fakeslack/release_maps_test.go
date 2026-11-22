package fakeslack

import (
	"testing"

	"github.com/some-du6e/clack/internal/permapguard"
)

// Every process-wide map that holds a *Server is one forget empties (Close
// ends with it): an entry keeps a closed Server, histories and all, to the
// end of the test binary (ci-speed-3's newPerServer maps). It found
// manageStates, a bare sync.Map.
func TestForgetCoversServerMaps(t *testing.T) {
	rep, err := permapguard.Check(permapguard.Config{Dir: ".", Owner: "Server", Release: "forget", Maker: "newPerServer"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range rep.Missing {
		t.Error(m)
	}
	t.Logf("%d newPerServer maps; other maps holding a *Server, forgotten: %v", rep.Registered, rep.Checked)
	if rep.Registered < 20 {
		t.Errorf("found %d newPerServer maps: the guard no longer reads how they're made", rep.Registered)
	}
}
