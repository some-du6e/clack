package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The real macOS path: a dmg made with hdiutil (like release.yml's) is
// mounted, its Clack.app copied beside the installed one, the dmg
// detached, and the helper's swap puts it in place.
func TestPrepareMacDMG(t *testing.T) {
	if _, err := exec.LookPath("hdiutil"); err != nil {
		t.Skip("no hdiutil")
	}
	mkApp := func(dir, body string) string {
		app := filepath.Join(dir, "Clack.app")
		os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755)
		os.WriteFile(filepath.Join(app, "Contents", "MacOS", "clack"), []byte("#!/bin/sh\necho "+body+"\n"), 0o755)
		os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte("<plist/>"), 0o644)
		return app
	}
	apps := t.TempDir()
	installed := mkApp(apps, "old")
	src := t.TempDir()
	mkApp(src, "new")
	os.Symlink("/Applications", filepath.Join(src, "Applications"))
	dmg := filepath.Join(t.TempDir(), "Clack-0.2.0-macos-universal.dmg")
	var err error
	for i := 0; i < 5; i++ { // "Resource busy" on hosted runners
		if err = exec.Command("hdiutil", "create", "-volname", "Clack", "-srcfolder", src, "-fs", "HFS+", "-format", "UDZO", "-ov", dmg).Run(); err == nil {
			break
		}
		time.Sleep(3 * time.Second)
	}
	if err != nil {
		t.Fatalf("hdiutil create: %v", err)
	}
	b, _ := os.ReadFile(dmg)
	h := sha256.Sum256(b)
	pkg := Package{Tag: "v0.2.0", Name: filepath.Base(dmg), Path: dmg, SHA256: hex.EncodeToString(h[:])}
	in := Install{Kind: KindMacApp, Target: installed}
	p, err := Prepare(context.Background(), in, pkg)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := os.ReadFile(filepath.Join(installed, "Contents", "MacOS", "clack")); string(out) != "#!/bin/sh\necho old\n" {
		t.Fatal("Prepare touched the installed app")
	}
	if r := p.Relaunch; len(r) != 2 || r[0] != "/usr/bin/open" || r[1] != installed {
		t.Errorf("relaunch %v", p.Relaunch)
	}
	// nothing mounted is left behind
	if out, _ := exec.Command("hdiutil", "info").Output(); len(out) > 0 && strings.Contains(string(out), filepath.Base(dmg)) {
		t.Errorf("dmg still attached:\n%s", out)
	}
	rc := record(t) // don't really `open` the app
	if err := applyPlan(Plan{Swap: p.Swap, Stage: p.Stage, Relaunch: p.Relaunch}, time.Second); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(filepath.Join(installed, "Contents", "MacOS", "clack")).Output()
	if err != nil || string(out) != "new\n" {
		t.Errorf("after the swap: %q %v", out, err)
	}
	ents, _ := os.ReadDir(apps)
	if len(ents) != 1 {
		t.Errorf("left beside the app: %v", ents)
	}
	if len(rc.started) != 1 {
		t.Errorf("relaunch %v", rc.started)
	}
	// the zip flavour (ditto)
	zip := filepath.Join(t.TempDir(), "Clack-0.2.0-macos-universal.zip")
	if err := exec.Command("ditto", "-c", "-k", "--keepParent", filepath.Join(src, "Clack.app"), zip).Run(); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(zip)
	h = sha256.Sum256(b)
	pz, err := Prepare(context.Background(), in, Package{Tag: "v0.2.0", Name: filepath.Base(zip), Path: zip, SHA256: hex.EncodeToString(h[:])})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(pz.Swap.New, "Contents", "MacOS", "clack")); err != nil {
		t.Errorf("zip: %v", err)
	}
	os.RemoveAll(pz.Stage)
}
