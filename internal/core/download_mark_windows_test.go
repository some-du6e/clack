package core

import (
	"os"
	"path/filepath"
	"testing"
)

// On Windows a finished download carries Chromium's Zone.Identifier
// stream, byte for byte.
func TestMarkDownloadedWindows(t *testing.T) {
	p := filepath.Join(t.TempDir(), "game.exe")
	if err := os.WriteFile(p, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	const u = "https://files.slack.com/files-pri/T1-F1/download/game.exe"
	MarkDownloaded(p, u)
	b, err := os.ReadFile(p + ":Zone.Identifier")
	if err != nil {
		t.Fatalf("no Zone.Identifier stream: %v", err)
	}
	if got, want := string(b), "[ZoneTransfer]\r\nZoneId=3\r\nHostUrl="+u+"\r\n"; got != want {
		t.Errorf("the stream is %q, want %q", got, want)
	}
}
