package desktop

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// onQuitPlan is a Windows installer's on-quit plan as installOnQuit hands
// it to the helper (JSON: what the helper reads).
func onQuitPlan(t *testing.T, socket string) Plan {
	t.Helper()
	raw := `{"tag":"v9.9.9","run":["C:\\cache\\Clack-setup.exe","/VERYSILENT","/CURRENTUSER","/SUPPRESSMSGBOXES","/NORESTART"],"on_quit":true`
	if socket != "" {
		b, _ := json.Marshal(socket)
		raw += `,"socket":` + string(b)
	}
	var p Plan
	if err := json.Unmarshal([]byte(raw+"}"), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// A Clack the user reopened after quitting keeps running: the on-quit
// install sees it answer on the instance socket and waits for the next
// quit. The installer (clack.iss: CloseApplications=force,
// RestartApplications=no) closed it and nothing started it again.
func TestOnQuitInstallSkipsAReopenedClack(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "i.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("no unix socket here: %v", err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	var ran [][]string
	defer func(f func(context.Context, string, ...string) error) { runCmd = f }(runCmd)
	runCmd = func(_ context.Context, name string, args ...string) error {
		ran = append(ran, append([]string{name}, args...))
		return nil
	}
	_ = applyPlan(onQuitPlan(t, sock), 0)
	if len(ran) != 0 {
		t.Errorf("with a Clack running again the installer ran: %v", ran)
	}
}

// With no Clack running the on-quit installer runs, with
// /NOCLOSEAPPLICATIONS: one started while it installs fails it instead of
// being closed.
func TestOnQuitInstallNeverClosesClack(t *testing.T) {
	var ran [][]string
	defer func(f func(context.Context, string, ...string) error) { runCmd = f }(runCmd)
	runCmd = func(_ context.Context, name string, args ...string) error {
		ran = append(ran, append([]string{name}, args...))
		return nil
	}
	_ = applyPlan(onQuitPlan(t, filepath.Join(t.TempDir(), "none.sock")), 0)
	if len(ran) != 1 || !strings.Contains(strings.Join(ran[0], " "), "/NOCLOSEAPPLICATIONS") {
		t.Errorf("the on-quit installer ran %v, want it once with /NOCLOSEAPPLICATIONS", ran)
	}
}

// An AppImage updates the file it was started from ($APPIMAGE, the
// target), not its mount under $TMPDIR, which every AppImage runs from:
// UpdateUnsupported called every AppImage "running from Temp directory",
// so none ever staged.
func TestUpdateUnsupportedAppImage(t *testing.T) {
	in := Install{Kind: KindAppImage, Target: "/home/someone/Applications/Clack.AppImage",
		Exe: filepath.Join(os.TempDir(), ".mount_ClackAbc123", "clack")}
	if got := UpdateUnsupported(in); got != "" {
		t.Errorf("an AppImage in ~/Applications: %q, want it to update", got)
	}
	in.Target = filepath.Join(os.TempDir(), "Clack.AppImage")
	if got := UpdateUnsupported(in); got != "running from Temp directory" {
		t.Errorf("an AppImage that is itself in the temp directory: %q", got)
	}
}
