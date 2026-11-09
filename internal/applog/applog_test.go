package applog

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDirPerOS(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	none := env(nil)
	home := filepath.Join("/", "home", "k")
	cases := []struct {
		goos   string
		getenv func(string) string
		home   string
		want   string
	}{
		{"darwin", none, "/Users/k", filepath.Join("/Users/k", "Library", "Logs", "Clack")},
		{"linux", env(map[string]string{"XDG_STATE_HOME": "/xdg/state"}), home, filepath.Join("/xdg/state", "clack")},
		{"linux", none, home, filepath.Join(home, ".local", "state", "clack")},
		{"linux", env(map[string]string{"XDG_STATE_HOME": "relative/state"}), home, filepath.Join(home, ".local", "state", "clack")}, // invalid per XDG
		{"freebsd", none, home, filepath.Join(home, ".local", "state", "clack")},
		{"windows", env(map[string]string{"LOCALAPPDATA": `C:\Users\k\AppData\Local`}), `C:\Users\k`, filepath.Join(`C:\Users\k\AppData\Local`, "Clack", "logs")},
		{"windows", none, `C:\Users\k`, filepath.Join(`C:\Users\k`, "AppData", "Local", "Clack", "logs")},
	}
	for _, c := range cases {
		got, err := dirFor(c.goos, c.getenv, c.home)
		if err != nil || got != c.want {
			t.Errorf("%s: dir %q, %v; want %q", c.goos, got, err, c.want)
		}
	}
	for _, goos := range []string{"darwin", "linux", "windows"} {
		if d, err := dirFor(goos, none, ""); err == nil {
			t.Errorf("%s with no home: %q, want an error", goos, d)
		}
	}
	t.Setenv("CLACK_LOG_DIR", "/tmp/clack-logs-override")
	if d, err := Dir(); err != nil || d != "/tmp/clack-logs-override" {
		t.Errorf("CLACK_LOG_DIR: %q, %v", d, err)
	}
}

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	r, err := openRotating(path, 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err := fmt.Fprintf(r, "line %02d %s\n", i, strings.Repeat("x", 30)); err != nil { // 39 bytes
			t.Fatal(err)
		}
	}
	r.Close()
	for _, name := range []string{FileName, FileName + ".1", FileName + ".2"} {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if st.Size() > 100 {
			t.Errorf("%s is %d bytes, over the 100-byte limit", name, st.Size())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, FileName+".3")); err == nil {
		t.Error("a fourth file was kept")
	}
	live, _ := os.ReadFile(path)
	if !strings.Contains(string(live), "line 19") {
		t.Errorf("the newest line isn't in the live file:\n%s", live)
	}
	old, _ := os.ReadFile(path + ".2")
	if strings.Contains(string(old), "line 00") || !strings.Contains(string(old), "line 1") {
		t.Errorf("clack.log.2 should hold the oldest lines kept, not the first ones written:\n%s", old)
	}
	// reopening appends and keeps counting the existing size
	r, err = openRotating(path, 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	if r.size != int64(len(live)) {
		t.Errorf("reopened at size %d, the file has %d", r.size, len(live))
	}
	r.Close()
}

// slowDisk is a sink whose writes wait for gate (a disk that stalls).
type slowDisk struct {
	gate chan struct{}
	mu   sync.Mutex
	buf  bytes.Buffer
}

func (d *slowDisk) Write(p []byte) (int, error) {
	<-d.gate
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.buf.Write(p)
}
func (d *slowDisk) Close() error { return nil }
func (d *slowDisk) String() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.buf.String()
}

func TestAsyncNeverBlocksOnSlowDisk(t *testing.T) {
	disk := &slowDisk{gate: make(chan struct{})}
	a := NewAsync(func() (io.WriteCloser, error) { return disk, nil }, nil, 8)
	start := time.Now()
	for i := 0; i < 5000; i++ {
		a.Write([]byte(fmt.Sprintf("line %d\n", i)))
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("5000 writes to a stalled disk took %v; Write must not wait for the disk", d)
	}
	if a.Dropped() == 0 {
		t.Fatal("a stalled disk with an 8-line buffer dropped nothing")
	}
	if a.Flush(50 * time.Millisecond) {
		t.Error("Flush reported done while the disk was stalled")
	}
	close(disk.gate) // the disk comes back
	if !a.Flush(5 * time.Second) {
		t.Fatal("Flush timed out after the disk came back")
	}
	a.Write([]byte("after\n"))
	a.Close(5 * time.Second)
	out := disk.String()
	if !strings.Contains(out, "line 0\n") {
		t.Errorf("the first line queued wasn't written:\n%.300s", out)
	}
	if !strings.Contains(out, "log lines dropped (the disk was too slow)") || !strings.Contains(out, "after\n") {
		t.Errorf("no dropped-lines note before the next line:\n%s", out[max(0, len(out)-300):])
	}
	a.Write([]byte("after close\n")) // dropped, doesn't block or panic
}

func TestRedactedInFile(t *testing.T) {
	var sink syncBuf
	a := NewAsync(func() (io.WriteCloser, error) { return &sink, nil }, nil, 64)
	secrets := []string{
		"xoxc-1234567890-abcdefSECRET",
		"xoxd-COOKIEVALUE%2Fmore%3D",
		"xoxp-99-88-77-PERSONAL",
		"xoxe.xoxp-1-REFRESHSECRET",
		"dcookieSECRET",
		"dsSECRET",
		"lcSECRET",
		"querytokenSECRET",
		"bearerSECRET",
		"jsonSECRET",
		"jsoncookieSECRET",
		"encodedSECRET",
		"magicSECRET",
		"deeplinkquerySECRET",
	}
	lines := []string{
		"slack: auth.test token xoxc-1234567890-abcdefSECRET failed",
		"cookie jar: d=xoxd-COOKIEVALUE%2Fmore%3D; d-s=dsSECRET",
		"Cookie: d=dcookieSECRET; lc=lcSECRET",
		"Set-Cookie: d=dcookieSECRET; Path=/; Secure",
		"rtm: dial wss://wss-primary.slack.com/?token=querytokenSECRET&batch_presence_aware=1: refused",
		`Post "https://hackclub.slack.com/api/chat.postMessage?_x_id=1&token=querytokenSECRET": EOF`,
		"call with xoxp-99-88-77-PERSONAL and xoxe.xoxp-1-REFRESHSECRET",
		"Authorization: Bearer bearerSECRET",
		"header bearer bearerSECRET sent",
		`body {"ok":false,"token":"jsonSECRET","cookie":"jsoncookieSECRET"}`,
		"form t=xoxc%2DencodedSECRET",
		"link slack://T1/magic-login/magicSECRET",
		"ui: deep link not handled: slack://channel?team=T1&id=C1&q=deeplinkquerySECRET",
	}
	for _, l := range lines {
		a.Write([]byte(l + "\n"))
	}
	a.Close(5 * time.Second)
	out := sink.String()
	for _, s := range secrets {
		if strings.Contains(out, s) {
			t.Errorf("%q reached the log file:\n%s", s, out)
		}
	}
	for _, keep := range []string{"slack: auth.test token xox", "rtm: dial wss://wss-primary.slack.com/?token=REDACTED&batch_presence_aware=1: refused", "chat.postMessage", "slack://channel", "REDACTED"} {
		if !strings.Contains(out, keep) {
			t.Errorf("redaction removed too much: no %q in\n%s", keep, out)
		}
	}
	if n := strings.Count(out, "\n"); n != len(lines) {
		t.Errorf("%d lines written, want %d", n, len(lines))
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuf) Close() error { return nil }
func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// An unwritable log directory: Clack starts, logs to stderr as before, and
// says once that there's no log file.
func TestStartUnwritableDir(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		dir  string
		err  error
	}{
		{"dir under a file", filepath.Join(blocker, "logs"), nil},
		{"no log dir", "", errNoDir},
	} {
		t.Run(c.name, func(t *testing.T) {
			var stderr syncBuf
			l := start(c.dir, c.err, &stderr)
			out := log.New(l.Output(&stderr), "", 0)
			done := make(chan struct{})
			go func() {
				defer close(done)
				for i := 0; i < 50; i++ {
					out.Printf("still running %d token=xoxc-SECRET", i)
				}
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("logging blocked with an unwritable log dir")
			}
			l.Close(2 * time.Second)
			got := stderr.String()
			if n := strings.Count(got, "can't write the log file"); n != 1 {
				t.Errorf("%d warnings, want 1:\n%s", n, got)
			}
			if !strings.Contains(got, "still running 49") || strings.Contains(got, "xoxc-SECRET") {
				t.Errorf("stderr lost lines or a token:\n%s", got)
			}
			if l.Writing() {
				t.Error("Writing() with no file")
			}
			if tail := l.Tail(5); len(tail) != 5 || !strings.Contains(tail[4], "still running 49") || strings.Contains(strings.Join(tail, "\n"), "SECRET") {
				t.Errorf("Tail without a file: %q", tail)
			}
		})
	}
}

// A sink that fails to open after lines and Close are already queued (a
// slow MkdirAll on Windows): Close returns only after the warning is out.
func TestCloseWaitsForOpenWarning(t *testing.T) {
	release := make(chan struct{})
	var warned atomic.Bool
	a := NewAsync(func() (io.WriteCloser, error) {
		<-release
		return nil, errNoDir
	}, func(error) {
		time.Sleep(50 * time.Millisecond)
		warned.Store(true)
	}, 64)
	for i := 0; i < 10; i++ {
		a.Write([]byte(fmt.Sprintf("line %d\n", i)))
	}
	go func() {
		for len(a.ch) < 11 { // the lines and Close's marker
			time.Sleep(time.Millisecond)
		}
		close(release)
	}()
	a.Close(5 * time.Second)
	if !warned.Load() {
		t.Error("Close returned before the can't-open warning")
	}
	if got := a.Recent(20); len(got) != 10 || got[9] != "line 9" {
		t.Errorf("Recent: %q", got)
	}
}

func TestStartWritesFileAndTail(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLACK_LOG_DIR", dir)
	var stderr syncBuf
	prevOut, prevFlags := log.Writer(), log.Flags()
	l := Start(&stderr)
	defer func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) }()
	for i := 0; i < 300; i++ {
		log.Printf("tail line %d d=cookieSECRET", i)
	}
	tail := l.Tail(200)
	if len(tail) != 200 || !strings.HasSuffix(tail[199], "tail line 299 d=REDACTED") || !strings.HasSuffix(tail[0], "tail line 100 d=REDACTED") {
		t.Fatalf("Tail(200): %d lines, first %q last %q", len(tail), first(tail), last(tail))
	}
	if !l.Writing() || l.Path() != filepath.Join(dir, FileName) || l.Dir() != dir {
		t.Errorf("Writing %v, Path %q, Dir %q", l.Writing(), l.Path(), l.Dir())
	}
	l.Close(2 * time.Second)
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "SECRET") || strings.Count(string(b), "\n") != 300 {
		t.Errorf("the file has %d lines or a secret", strings.Count(string(b), "\n"))
	}
	if strings.Contains(stderr.String(), "SECRET") || !strings.Contains(stderr.String(), "tail line 299") {
		t.Error("stderr lost a line or got a secret")
	}
	if st, _ := os.Stat(filepath.Join(dir, FileName)); st != nil && st.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("log file mode %v, want owner-only", st.Mode().Perm())
	}
}

func TestTailAcrossRotation(t *testing.T) {
	dir := t.TempDir()
	r, err := openRotating(filepath.Join(dir, FileName), 200, 3)
	if err != nil {
		t.Fatal(err)
	}
	a := NewAsync(func() (io.WriteCloser, error) { return r, nil }, nil, 64)
	l := &Logger{dir: dir, w: a}
	for i := 0; i < 12; i++ {
		a.Write([]byte(fmt.Sprintf("rotated %02d %s\n", i, strings.Repeat("y", 20))))
	}
	tail := l.Tail(8)
	if len(tail) != 8 || !strings.HasPrefix(first(tail), "rotated 04") || !strings.HasPrefix(last(tail), "rotated 11") {
		t.Errorf("Tail(8) across clack.log.1 and clack.log: %q", tail)
	}
	l.Close(time.Second)
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func last(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[len(s)-1]
}
