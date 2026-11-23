//go:build darwin && cgo

package slack

import "testing"

// The real UTType database: the types macOS itself declares for a
// QuickTime movie, a HEIC photo and a Python script (UTType.quickTimeMovie,
// .heic, .pythonScript), none for an extension nothing declares (a dynamic
// type) or that isn't UTF-8. It logs what this Mac types the files Karim's
// reports name, what Slack desktop here would send.
func TestUTTypeMimeTypes(t *testing.T) {
	for ext, want := range map[string]string{"mov": "video/quicktime", "heic": "image/heic", "py": "text/x-python-script"} {
		if m, ok := osMimeType(ext); !ok || m != want {
			t.Errorf("UTType for .%s: %q, %v; want %q", ext, m, ok, want)
		}
		if got := FileType("a." + ext); got != want {
			t.Errorf("FileType(a.%s) = %q, want UTType's %q", ext, got, want)
		}
	}
	for _, ext := range []string{"clackzzq", "a\xff", ""} {
		if m, ok := osMimeType(ext); ok {
			t.Errorf("UTType for %q: %q, want none", ext, m)
		}
	}
	for _, ext := range []string{"MOV", "HEIC", "go", "ts", "rs", "md", "csv", "json", "log", "sh", "ico", "bin"} {
		m, ok := osMimeType(ext)
		t.Logf("UTType for .%s: %q (%v); FileType: %q", ext, m, ok, FileType("a."+ext))
	}
}
