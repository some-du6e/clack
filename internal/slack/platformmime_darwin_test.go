//go:build darwin && cgo

package slack

import "testing"

// The real UTType database, on types Apple declares as constants of the
// framework (UTType.png, UTType.quickTimeMovie), whose MIME types don't
// move between macOS versions; none for an extension nothing declares (a
// dynamic type) or that isn't UTF-8. It logs what this Mac types the files
// Karim's reports name (.py, .heic and the rest: this machine's answers,
// not asserted), what Slack desktop here would send. The extension is
// quoted in the log so setup-go's problem matcher doesn't read the .go
// line as a Go error.
func TestUTTypeMimeTypes(t *testing.T) {
	for ext, want := range map[string]string{"png": "image/png", "mov": "video/quicktime"} {
		if m, ok := osMimeType(ext); !ok || m != want {
			t.Errorf("UTType for %q: %q, %v; want %q", "."+ext, m, ok, want)
		}
	}
	if got := FileType("a.mov"); got != "video/quicktime" {
		t.Errorf("FileType(a.mov) = %q, want UTType's video/quicktime", got)
	}
	for _, ext := range []string{"clackzzq", "a\xff", ""} {
		if m, ok := osMimeType(ext); ok {
			t.Errorf("UTType for %q: %q, want none", ext, m)
		}
	}
	for _, ext := range []string{"py", "heic", "MOV", "HEIC", "go", "ts", "rs", "md", "csv", "json", "log", "sh", "ico", "bin"} {
		m, ok := osMimeType(ext)
		t.Logf("UTType %q: %q (%v); FileType %q", "."+ext, m, ok, FileType("a."+ext))
	}
}
