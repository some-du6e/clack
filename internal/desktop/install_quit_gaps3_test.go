package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// swapPlan is a portable exe's (or an AppImage's) on-quit plan, staged as
// stageForQuit stages it: the new copy in a .clack-update-* stage beside
// the target.
func swapPlan(t *testing.T) (p Plan, target, stage string) {
	t.Helper()
	dir := t.TempDir()
	target = filepath.Join(dir, "Clack.exe")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	stage, err := stageNext(target)
	if err != nil {
		t.Fatal(err)
	}
	nw := filepath.Join(stage, "Clack.exe")
	if err := os.WriteFile(nw, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"tag": "v9.9.9", "stage": stage, "on_quit": true,
		"swap": map[string]string{"target": target, "new": nw}})
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p, target, stage
}

// A session that ends after Clack's quit (the helper started, then Windows
// began shutting down: SM_SHUTTINGDOWN) installs nothing: the installer
// doesn't run, the swap doesn't happen, and the stage goes; the update
// waits for the next quit's download.
func TestOnQuitInstallSkipsAsTheSessionEnds(t *testing.T) {
	defer func(f func() bool) { sessionEnding = f }(sessionEnding)
	sessionEnding = func() bool { return true }
	var ran [][]string
	defer func(f func(context.Context, string, ...string) error) { runCmd = f }(runCmd)
	runCmd = func(_ context.Context, name string, args ...string) error {
		ran = append(ran, append([]string{name}, args...))
		return nil
	}
	if err := applyPlan(onQuitPlan(t, ""), 0); err == nil || len(ran) != 0 {
		t.Errorf("the installer, as the session ends: %v, ran %v; want it skipped", err, ran)
	}
	p, target, stage := swapPlan(t)
	if err := applyPlan(p, 0); err == nil {
		t.Error("the swap, as the session ends: no error, want it skipped")
	}
	if b, _ := os.ReadFile(target); string(b) != "old" {
		t.Errorf("as the session ends the target is %q, want the old copy", b)
	}
	if _, err := os.Stat(stage); !os.IsNotExist(err) {
		t.Errorf("the stage stayed (%v)", err)
	}
	// and with the session going on, the same plan installs
	sessionEnding = func() bool { return false }
	p, target, _ = swapPlan(t)
	if err := applyPlan(p, 0); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(target); string(b) != "new" {
		t.Errorf("after the swap the target is %q, want the new copy", b)
	}
}

// The Windows installer is checked against the download's SHA-256 again
// just before the helper runs it: a staged plan runs hours or days after
// the download was verified, from the download cache.
func TestInstallerHashCheckedBeforeItRuns(t *testing.T) {
	setup := filepath.Join(t.TempDir(), "Clack-setup.exe")
	if err := os.WriteFile(setup, []byte("the installer"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("the installer"))
	good := hex.EncodeToString(sum[:])
	p, err := Prepare(context.Background(), Install{Kind: KindWinInstaller, Exe: `C:\Clack\Clack.exe`}, Package{Tag: "v9.9.9", Path: setup, SHA256: good})
	if err != nil {
		t.Fatal(err)
	}
	p.PID, p.Relaunch = 0, nil
	var ran int
	defer func(f func(context.Context, string, ...string) error) { runCmd = f }(runCmd)
	runCmd = func(context.Context, string, ...string) error { ran++; return nil }
	// the cached installer changed after the download's check
	if err := os.WriteFile(setup, []byte("something else"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyPlan(p, 0); err == nil || ran != 0 {
		t.Errorf("a changed installer: %v, ran %d times; want it refused", err, ran)
	}
	if err := os.WriteFile(setup, []byte("the installer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := applyPlan(p, 0); err != nil || ran != 1 {
		t.Errorf("the verified installer: %v, ran %d times; want it run once", err, ran)
	}
}

// A launch while Clack is quitting (Run returned: the update helper's
// start, the logs) waits for it to go and then starts on its own: the
// quitting Clack answered "ok" and took the launch, which did nothing.
func TestLaunchWaitsForAQuittingClack(t *testing.T) {
	dir, err := os.MkdirTemp("", "s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "i.sock")
	got := make(chan Request, 4)
	in, err := listen(sock, func(r Request) { got <- r })
	if err != nil {
		t.Skipf("no unix socket here: %v", err)
	}
	if !forward(sock, Request{URL: "slack://open"}) {
		t.Fatal("a running Clack didn't take the launch")
	}
	<-got
	in.Quitting()
	done := make(chan bool, 1)
	start := time.Now()
	go func() { done <- forward(sock, Request{URL: "slack://open"}) }()
	time.Sleep(400 * time.Millisecond)
	select {
	case took := <-done:
		t.Fatalf("the launch returned %v while Clack was quitting, want it to wait", took)
	default:
	}
	in.Close()
	select {
	case took := <-done:
		if took {
			t.Error("a quitting Clack took the launch")
		}
		if d := time.Since(start); d < 400*time.Millisecond {
			t.Errorf("the launch went on after %v, before the quitting Clack closed", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the launch still waits after the quitting Clack closed")
	}
	select {
	case r := <-got:
		t.Errorf("the quitting Clack handled %+v", r)
	default:
	}
}
