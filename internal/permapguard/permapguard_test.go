package permapguard

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const fixture = `package p

import "sync"

type App struct{ n int }

type perApp struct{ sync.Map }

func newPerApp() *perApp { return &perApp{} }

type key struct {
	a  *App
	ch string
}

var (
	registered = newPerApp()        // made by the maker: released by construction
	bare       sync.Map             // keyed by *App: missed
	byKey      sync.Map             // keyed by key{a, ch} through a local: missed
	values     sync.Map             // holds *App values: released below
	strings    sync.Map             // never sees an App
	plain      = map[*App]int{}     // missed
	dropped    = map[*App]bool{}    // released below
	nested     = map[string][]key{} // missed
	// permapguard: emptied when each request ends
	exempt sync.Map
)

func (a *App) use(ch string) {
	bare.Store(a, 1)
	k := key{a, ch}
	byKey.LoadOrStore(k, struct{}{})
	values.Store(ch, a)
	strings.Store(ch, ch)
	exempt.Store(a, 1)
	registered.Store(a, 1)
}

func (a *App) Release() {
	values.Delete(a)
	delete(dropped, a)
}
`

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}
	// a test file's maps don't count
	if err := os.WriteFile(filepath.Join(dir, "p_test.go"), []byte("package p\n\nvar inTest = map[*App]int{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := Check(Config{Dir: dir, Owner: "App", Release: "Release", Maker: "newPerApp"})
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for _, m := range rep.Missing {
		missing = append(missing, m.Var)
	}
	slices.Sort(missing)
	if want := []string{"bare", "byKey", "nested", "plain"}; !slices.Equal(missing, want) {
		t.Errorf("missing %v, want %v (%v)", missing, want, rep.Missing)
	}
	slices.Sort(rep.Checked)
	if want := []string{"bare", "byKey", "dropped", "exempt", "nested", "plain", "values"}; !slices.Equal(rep.Checked, want) {
		t.Errorf("checked %v, want %v", rep.Checked, want)
	}
	if rep.Registered != 1 {
		t.Errorf("%d registered maps, want 1", rep.Registered)
	}
	if _, err := Check(Config{Dir: dir, Owner: "App", Release: "Close", Maker: "newPerApp"}); err == nil {
		t.Error("no error for a release method the package lacks")
	}
}
