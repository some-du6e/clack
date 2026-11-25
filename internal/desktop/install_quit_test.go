package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A copy Slack would call unable to update itself (getIsUpdateSupported:
// a translocated copy, one in the temp directory, one on a dmg) says why.
func TestUpdateUnsupported(t *testing.T) {
	tmp := filepath.Join(os.TempDir(), "unpacked", "Clack")
	cases := []struct {
		in   Install
		want string
	}{
		{Install{Target: "/private/var/folders/ab/xyz/d/AppTranslocation/1234/d/Clack.app", Exe: "/private/var/folders/ab/xyz/d/AppTranslocation/1234/d/Clack.app/Contents/MacOS/clack"}, "running from a translocated copy"},
		{Install{Target: tmp, Exe: tmp}, "running from Temp directory"},
		{Install{Target: "/Applications/Clack.app", Exe: "/Applications/Clack.app/Contents/MacOS/clack"}, ""},
	}
	if runtime.GOOS == "darwin" {
		cases = append(cases, struct {
			in   Install
			want string
		}{Install{Target: "/Volumes/Clack/Clack.app", Exe: "/Volumes/Clack/Clack.app/Contents/MacOS/clack"}, "running from DMG"},
			// Clack's own dmg (release.yml: -volname Clack, Clack.app at
			// its root) only: an app on an external drive updates (α.166
			// and α.167 did)
			struct {
				in   Install
				want string
			}{Install{Target: "/Volumes/External/Applications/Clack.app", Exe: "/Volumes/External/Applications/Clack.app/Contents/MacOS/clack"}, ""})
	}
	for _, c := range cases {
		if got := UpdateUnsupported(c.in); got != c.want {
			t.Errorf("%s: %q, want %q", c.in.Target, got, c.want)
		}
	}
}

// PrepareQuiet never opens the package for the user: where Prepare would
// open the dmg in Finder (a translocated Clack.app), it says so instead.
func TestPrepareQuietNeverOpens(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Clack-mac.dmg")
	body := []byte("not really a dmg")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	pkg := Package{Tag: "v9.9.9", Name: "Clack-mac.dmg", Path: p, SHA256: hex.EncodeToString(sum[:])}
	in := Install{Kind: KindMacApp, Target: "/private/var/folders/ab/xyz/d/AppTranslocation/1234/d/Clack.app"}
	ran := false
	defer func(f func(context.Context, string, ...string) error) { runCmd = f }(runCmd)
	runCmd = func(context.Context, string, ...string) error { ran = true; return nil }
	_, err := PrepareQuiet(context.Background(), in, pkg)
	if !errors.Is(err, ErrNeedsUser) {
		t.Fatalf("PrepareQuiet = %v, want ErrNeedsUser", err)
	}
	if ran {
		t.Error("PrepareQuiet ran a command (it would have opened the dmg)")
	}
}

// Stages older than an hour beside Clack go at start; a newer one (a
// helper may still be swapping it in) and anything else stay.
func TestCleanStages(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Clack.app")
	old, fresh, other := filepath.Join(dir, stagePrefix+"old"), filepath.Join(dir, stagePrefix+"new"), filepath.Join(dir, "Notes")
	for _, d := range []string{target, old, fresh, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	then := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, then, then); err != nil {
		t.Fatal(err)
	}
	CleanStages(target)
	for d, want := range map[string]bool{target: true, old: false, fresh: true, other: true} {
		if _, err := os.Stat(d); (err == nil) != want {
			t.Errorf("%s: exists %v, want %v", filepath.Base(d), err == nil, want)
		}
	}
}

// A helper's update.log is read before its folder goes: the last lines,
// from the end of a long log.
func TestLogTail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "update.log")
	long := strings.Repeat("padding line\n", 600) + "swap: moving the old copy aside: permission denied\nexit 1\n"
	if err := os.WriteFile(p, []byte(long), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := logTail(p, 2); got != "swap: moving the old copy aside: permission denied | exit 1" {
		t.Errorf("logTail = %q", got)
	}
	if got := logTail(filepath.Join(t.TempDir(), "none.log"), 2); got != "" {
		t.Errorf("a missing log: %q", got)
	}
}
