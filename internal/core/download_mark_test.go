package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// The Mark-of-the-Web Chromium writes on every download on Windows
// (the review of 93ab723be: Clack's downloads carried none): exactly
// [ZoneTransfer], ZoneId=3, the referrer when there is one and HostUrl,
// each line ending CRLF; about:internet when the source is unknown.
func TestZoneIdentifier(t *testing.T) {
	const u = "https://files.slack.com/files-pri/T1-F1/download/game.exe?origin_team=T1"
	if got, want := zoneIdentifier(u, ""), "[ZoneTransfer]\r\nZoneId=3\r\nHostUrl="+u+"\r\n"; got != want {
		t.Errorf("zoneIdentifier = %q, want %q", got, want)
	}
	if got, want := zoneIdentifier(u, "https://app.slack.com/"), "[ZoneTransfer]\r\nZoneId=3\r\nReferrerUrl=https://app.slack.com/\r\nHostUrl="+u+"\r\n"; got != want {
		t.Errorf("with a referrer = %q, want %q", got, want)
	}
	if got := zoneIdentifier("", ""); !strings.HasSuffix(got, "HostUrl=about:internet\r\n") {
		t.Errorf("no source = %q", got)
	}
}

// A long name keeps its extension (Chromium's TruncateFilename cuts the
// stem), and the superscript COM¹-³ / LPT¹-³ are device names too.
func TestCleanFileNameLimits(t *testing.T) {
	long := strings.Repeat("a", 300) + ".exe"
	got := CleanFileName(long)
	if len(got) > maxFileName || !strings.HasSuffix(got, ".exe") || !strings.HasPrefix(got, "aaa") {
		t.Errorf("a %d-byte name cleaned to %d bytes %q…", len(long), len(got), got[:min(len(got), 20)])
	}
	for _, n := range []string{"COM¹.txt", "lpt³", "COM1.exe", "nul"} {
		if got := CleanFileName(n); !strings.HasPrefix(got, "_") {
			t.Errorf("CleanFileName(%q) = %q, want the device name defused", n, got)
		}
	}
	if got := CleanFileName("COM10.txt"); got != "COM10.txt" {
		t.Errorf("COM10 isn't a device: %q", got)
	}
}

// A Pause while the download waits to auto-resume (interrupted, its
// runner still going) stops that wait: the download stays paused.
func TestDownloadPauseDuringAutoResume(t *testing.T) {
	defer func(d time.Duration) { downloadResumeDelay = d }(downloadResumeDelay)
	downloadResumeDelay = 2 * time.Second
	fs, a := dlFake(t)
	dir := t.TempDir()
	sf := fs.AddBigFile("F0BIGEXE08", fakeslack.BigFile{Name: "wait.exe", Size: 3 << 20, Cut: 100, CutAt: 1 << 20})
	dlStart(a, sf, dir)
	dlWait(t, a, sf.ID, 30*time.Second, inState(DownloadInterrupted))
	a.Downloads().Pause(sf.ID)
	if d, _ := a.Downloads().Get(sf.ID); d.State != DownloadPaused {
		t.Fatalf("paused while waiting: %s", d.State)
	}
	time.Sleep(3 * time.Second) // past the resume the wait was for
	if d, _ := a.Downloads().Get(sf.ID); d.State != DownloadPaused {
		t.Errorf("the auto-resume undid the Pause: %s", d.State)
	}
	if n, _ := fs.BigFileRequests(sf.ID); n != 1 {
		t.Errorf("%d requests, want the first only", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "wait.exe")); !os.IsNotExist(err) {
		t.Errorf("wait.exe exists (%v)", err)
	}
}
