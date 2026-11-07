package popups

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// File names come from the sender (file.name, image titles, URL paths):
// saving must stay inside Downloads and never make an executable.
func TestLightboxSaveNames(t *testing.T) {
	for in, want := range map[string]string{
		"../../.bashrc":          "_.._.bashrc",
		`..\..\AppData\evil.bat`: `_.._AppData_evil.bat`,
		"/etc/passwd":            "_etc_passwd",
		"C:\\Windows\\x.dll":     "C__Windows_x.dll",
		"..":                     "",
		" . ":                    "",
		"a\x00b\nc":              "a_b_c",
		"CON":                    "_CON",
		"nul.txt":                "_nul.txt",
		"com1.tar.gz":            "_com1.tar.gz",
		"console.txt":            "console.txt",
		"photo.png":              "photo.png",
		"trailing dots...":       "trailing dots",
		"\u202egnp.exe":          "_gnp.exe", // bidi override would show it as "exe.png"
		strings.Repeat("é", 150): strings.Repeat("é", 100),
	} {
		got := lbCleanName(in)
		if got != want {
			t.Errorf("lbCleanName(%q) = %q, want %q", in, got, want)
		}
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("lbCleanName(%q) = %q keeps a separator", in, got)
		}
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dl := filepath.Join(home, "Downloads")
	for _, name := range []string{"../../evil.sh", "/tmp/x", "..", "", "CON"} {
		p, err := lbSave(name, "https://files.slack.com/files-pri/T1-F1/../../x.sh", []byte("#!/bin/sh\necho hi\n"))
		if err != nil {
			t.Fatalf("lbSave(%q): %v", name, err)
		}
		if filepath.Dir(p) != dl {
			t.Errorf("lbSave(%q) wrote %s, outside %s", name, p, dl)
		}
		if runtime.GOOS != "windows" {
			fi, err := os.Stat(p)
			if err != nil {
				t.Fatalf("stat %s: %v", p, err)
			}
			if fi.Mode().Perm()&0o111 != 0 {
				t.Errorf("lbSave(%q): mode %v (must not be executable)", name, fi.Mode())
			}
		}
	}
}

// Animated GIFs load directly only from Giphy (what gif.search returns);
// look-alike hosts don't count.
func TestGifAnimURLHost(t *testing.T) {
	for _, u := range []string{
		"https://evil.example/giphy.com/x.gif",
		"https://giphy.com.evil.io/media/x/200w.webp",
		"https://evilgiphy.com/x.gif",
		"http://media.giphy.com/media/x/200w.gif",
	} {
		if got := gifAnimURL(Gif{Preview: u}); got != "" {
			t.Errorf("gifAnimURL(%q) = %q, want refused", u, got)
		}
	}
	if got := gifAnimURL(Gif{Preview: "https://media2.giphy.com/media/abc/200w.webp?cid=1"}); got != "https://media2.giphy.com/media/abc/200w.gif?cid=1" {
		t.Errorf("gifAnimURL(giphy) = %q", got)
	}
}
