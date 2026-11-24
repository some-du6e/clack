package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// On macOS a finished download carries the quarantine xattr Slack's f1
// writes: "0002;<hex seconds>;Clack;<UUID>".
func TestMarkDownloadedDarwin(t *testing.T) {
	p := filepath.Join(t.TempDir(), "game.zip")
	if err := os.WriteFile(p, []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}
	MarkDownloaded(p, "https://files.slack.com/files-pri/T1-F1/download/game.zip")
	out, err := exec.Command("/usr/bin/xattr", "-p", "com.apple.quarantine", p).Output()
	if err != nil {
		t.Fatalf("no quarantine xattr: %v", err)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ";")
	if len(parts) != 4 || parts[0] != "0002" || parts[2] != "Clack" || len(parts[3]) != 36 {
		t.Errorf("quarantine %q, want 0002;<seconds>;Clack;<UUID>", out)
	}
}
