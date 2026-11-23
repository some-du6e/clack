//go:build linux

package slack

import (
	"os"
	"testing"
)

// The process's database is the XDG one over this machine's dirs; it logs
// what this machine's shared-mime-info types the files Karim's reports
// name, what Slack desktop here would send.
func TestSystemXDGMimeTypes(t *testing.T) {
	t.Logf("XDG data dirs: %q", xdgDataDirs(os.Getenv))
	for _, ext := range []string{"mov", "heic", "HEIC", "py", "go", "ts", "rs", "md", "csv", "json", "log", "sh", "ico", "bin"} {
		m, ok := osMimeType(ext)
		if s, sok := systemXDG.platformType(ext); s != m || sok != ok {
			t.Errorf(".%s: osMimeType %q, %v but the system database %q, %v", ext, m, ok, s, sok)
		}
		t.Logf(".%s: %q (%v); FileType: %q", ext, m, ok, FileType("a."+ext))
	}
}
