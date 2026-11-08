package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cliEnv is `clack --update` against the fake releases server, updating an
// AppImage in a temp dir, applied in this process.
type cliEnv struct {
	f       *fakeReleases
	target  string
	out     bytes.Buffer
	applied int
}

func newCLIEnv(t *testing.T) *cliEnv {
	e := &cliEnv{f: newFakeReleases(t)}
	e.target = filepath.Join(t.TempDir(), "Clack.AppImage")
	os.WriteFile(e.target, []byte("old"), 0o755)
	e.f.files["Clack-0.1.0-alpha.10-x86_64.AppImage"] = payload(200_000)
	e.f.files["Clack-0.1.0-alpha.10-aarch64.AppImage"] = payload(10)
	return e
}

func (e *cliEnv) cli(t *testing.T, version string, latest Release) *CLI {
	return &CLI{Version: version, Out: &e.out,
		Install: Install{Kind: KindAppImage, GOOS: "linux", GOARCH: "amd64", Target: e.target},
		Updater: e.f.updater(t),
		Latest:  func(context.Context, string) (Release, error) { return latest, nil },
		Apply: func(p Plan) (bool, string, error) {
			e.applied++
			if len(p.Relaunch) != 0 {
				t.Errorf("the CLI relaunches Clack: %v", p.Relaunch)
			}
			p.PID = 0
			return false, "", applyPlan(p, time.Second)
		},
	}
}

func TestUpdateCLIInstalls(t *testing.T) {
	noInstall(t) // nothing exec'd: the swap is a rename, no relaunch
	e := newCLIEnv(t)
	r := e.f.release("v0.1.0-alpha.10", "Clack-0.1.0-alpha.10-x86_64.AppImage", "Clack-0.1.0-alpha.10-aarch64.AppImage")
	c := e.cli(t, "v0.1.0-alpha.9", r)
	c.Yes = true
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, e.out.String())
	}
	if b, _ := os.ReadFile(e.target); !bytes.Equal(b, e.f.files["Clack-0.1.0-alpha.10-x86_64.AppImage"]) || e.applied != 1 {
		t.Errorf("AppImage not replaced (applied %d)", e.applied)
	}
	out := e.out.String()
	for _, want := range []string{"Clack v0.1.0-alpha.9 (install type: appimage", "Newer release: v0.1.0-alpha.10",
		"Package for this install: Clack-0.1.0-alpha.10-x86_64.AppImage", "downloading", "(100%)",
		"Verified against SHA256SUMS: ", "Installing…", "Updated to Clack v0.1.0-alpha.10"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestUpdateCLITampered(t *testing.T) {
	noInstall(t)
	e := newCLIEnv(t)
	name := "Clack-0.1.0-alpha.10-x86_64.AppImage"
	e.f.sums[name] = payload(5) // SHA256SUMS lists other bytes
	c := e.cli(t, "v0.1.0-alpha.9", e.f.release("v0.1.0-alpha.10", name))
	c.Yes = true
	if err := c.Run(context.Background()); !errors.Is(err, ErrChecksum) {
		t.Fatalf("tampered: %v", err)
	}
	if b, _ := os.ReadFile(e.target); string(b) != "old" || e.applied != 0 {
		t.Error("a tampered package was installed")
	}
	if strings.Contains(e.out.String(), "Verified") || strings.Contains(e.out.String(), "Installing") {
		t.Errorf("claims verification:\n%s", e.out.String())
	}
	// no SHA256SUMS entry: same
	e.f.sums[name] = nil
	if err := c.Run(context.Background()); !errors.Is(err, ErrNoChecksum) || e.applied != 0 {
		t.Errorf("missing entry: %v", err)
	}
}

func TestUpdateCLIUpToDateAndCheck(t *testing.T) {
	noInstall(t)
	e := newCLIEnv(t)
	r := e.f.release("v0.1.0-alpha.10", "Clack-0.1.0-alpha.10-x86_64.AppImage")
	// up to date (same, and newer than the newest release)
	for _, v := range []string{"v0.1.0-alpha.10", "v0.1.0"} {
		e.out.Reset()
		if err := e.cli(t, v, r).Run(context.Background()); err != nil || !strings.Contains(e.out.String(), "Clack is up to date (newest release: v0.1.0-alpha.10)") {
			t.Errorf("%s: %v\n%s", v, err, e.out.String())
		}
	}
	// --check: reports, downloads nothing
	e.out.Reset()
	c := e.cli(t, "v0.1.0-alpha.9", r)
	c.Check = true
	if err := c.Run(context.Background()); err != nil || !strings.Contains(e.out.String(), "Package for this install: Clack-0.1.0-alpha.10-x86_64.AppImage") {
		t.Errorf("check: %v\n%s", err, e.out.String())
	}
	if n := e.f.gets["Clack-0.1.0-alpha.10-x86_64.AppImage"] + e.f.gets["SHA256SUMS"]; n != 0 || e.applied != 0 {
		t.Errorf("--check fetched %d files, applied %d", n, e.applied)
	}
	// a dev build can't compare
	if err := e.cli(t, "dev", r).Run(context.Background()); !errors.Is(err, ErrDevBuild) {
		t.Errorf("dev: %v", err)
	}
	// no package for this install: the release page
	c = e.cli(t, "v0.1.0-alpha.9", r)
	c.Install.Kind = KindUnknown
	if err := c.Run(context.Background()); !errors.Is(err, ErrNoAsset) {
		t.Errorf("unknown install: %v", err)
	}
}

func TestUpdateCLIPrompt(t *testing.T) {
	noInstall(t)
	e := newCLIEnv(t)
	r := e.f.release("v0.1.0-alpha.10", "Clack-0.1.0-alpha.10-x86_64.AppImage")
	for _, answer := range []string{"n\n", "\n", ""} {
		c := e.cli(t, "v0.1.0-alpha.9", r)
		c.In = strings.NewReader(answer)
		if err := c.Run(context.Background()); !errors.Is(err, ErrNotConfirmed) {
			t.Errorf("answer %q: %v", answer, err)
		}
	}
	if e.f.gets["Clack-0.1.0-alpha.10-x86_64.AppImage"] != 0 || e.applied != 0 {
		t.Error("downloaded or installed without a yes")
	}
	if !strings.Contains(e.out.String(), "Download and install Clack v0.1.0-alpha.10? [y/N]") {
		t.Errorf("no prompt:\n%s", e.out.String())
	}
	c := e.cli(t, "v0.1.0-alpha.9", r)
	c.In = strings.NewReader("y\n")
	if err := c.Run(context.Background()); err != nil || e.applied != 1 {
		t.Errorf("y: %v, applied %d", err, e.applied)
	}
}

// The real release lookup (LatestRelease, pre-release aware) through
// CLACK_UPDATE_URL, as `clack --update` uses it.
func TestUpdateCLIRealLookup(t *testing.T) {
	noInstall(t)
	e := newCLIEnv(t)
	r := e.f.release("v0.1.0-alpha.10", "Clack-0.1.0-alpha.10-x86_64.AppImage")
	r.Prerelease = true
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode([]Release{{Tag: "v0.1.0-alpha.9", Prerelease: true}, r, {Tag: "v0.2.0", Draft: true}})
	}))
	defer api.Close()
	t.Setenv("CLACK_UPDATE_URL", api.URL)
	c := e.cli(t, "v0.1.0-alpha.9", Release{})
	c.Latest = nil // LatestRelease
	c.Yes = true
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("%v\n%s", err, e.out.String())
	}
	if !strings.Contains(e.out.String(), "Updated to Clack v0.1.0-alpha.10") {
		t.Errorf("output:\n%s", e.out.String())
	}
}
