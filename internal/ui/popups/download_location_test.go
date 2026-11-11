package popups

import (
	"os"
	"path/filepath"
	"testing"
)

// Download location: lbSave writes into DownloadDir when set, and hands
// the bytes to AskDownload when it asks (Choose location for each
// download); a closed dialog is ErrDownloadCanceled.
func TestDownloadLocation(t *testing.T) {
	t.Cleanup(func() { DownloadDir, AskDownload = nil, nil })
	dir := t.TempDir()
	DownloadDir = func() string { return dir }
	p, err := lbSave("report.pdf", "", []byte("%PDF-1.4"))
	if err != nil || filepath.Dir(p) != dir || filepath.Base(p) != "report.pdf" {
		t.Fatalf("into the chosen folder: %q, %v", p, err)
	}
	// asking: the dialog gets the cleaned name and the bytes
	var gotName string
	var gotData []byte
	AskDownload = func(name string, data []byte) (string, bool, error) {
		gotName, gotData = name, data
		return "/picked/" + name, true, nil
	}
	p, err = lbSave("../a/b.png", "", []byte("\x89PNG\r\n\x1a\n"))
	if err != nil || p != "/picked/"+gotName || gotName == "" || filepath.Base(gotName) != gotName || string(gotData) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("asked: %q (%q), %v", p, gotName, err)
	}
	AskDownload = func(string, []byte) (string, bool, error) { return "", true, ErrDownloadCanceled }
	if _, err := lbSave("x.txt", "", []byte("x")); err != ErrDownloadCanceled {
		t.Fatalf("closed dialog: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); err == nil {
		t.Error("a closed dialog still saved into the folder")
	}
	// not asking (off, or no dialog): the folder again
	AskDownload = func(string, []byte) (string, bool, error) { return "", false, nil }
	if p, err := lbSave("x.txt", "", []byte("x")); err != nil || filepath.Dir(p) != dir {
		t.Fatalf("not asking: %q, %v", p, err)
	}
}
