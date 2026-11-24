package popups

import (
	"os"
	"path/filepath"
	"testing"
)

// Download location: lbSave writes into DownloadDir when set, and into the
// folder ChooseDownloadDir picks when it asks (Choose location for each
// download: Slack's showOpenDialog with openDirectory, not a save
// dialog); a closed chooser is ErrDownloadCanceled and saves nothing.
func TestDownloadLocation(t *testing.T) {
	t.Cleanup(func() { DownloadDir, ChooseDownloadDir = nil, nil })
	dir := t.TempDir()
	DownloadDir = func() string { return dir }
	p, err := lbSave("report.pdf", "", []byte("%PDF-1.4"))
	if err != nil || filepath.Dir(p) != dir || filepath.Base(p) != "report.pdf" {
		t.Fatalf("into the chosen folder: %q, %v", p, err)
	}
	// asking: the picked folder, under the cleaned name
	picked := t.TempDir()
	ChooseDownloadDir = func() (string, bool, error) { return picked, true, nil }
	p, err = lbSave("../a/b.png", "", []byte("\x89PNG\r\n\x1a\n"))
	if err != nil || filepath.Dir(p) != picked || filepath.Base(p) != "_a_b.png" {
		t.Fatalf("asked: %q, %v", p, err)
	}
	ChooseDownloadDir = func() (string, bool, error) { return "", true, ErrDownloadCanceled }
	if _, err := lbSave("x.txt", "", []byte("x")); err != ErrDownloadCanceled {
		t.Fatalf("closed chooser: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); err == nil {
		t.Error("a closed chooser still saved into the folder")
	}
	// not asking (off, or no chooser): the folder again
	ChooseDownloadDir = func() (string, bool, error) { return "", false, nil }
	if p, err := lbSave("x.txt", "", []byte("x")); err != nil || filepath.Dir(p) != dir {
		t.Fatalf("not asking: %q, %v", p, err)
	}
}
