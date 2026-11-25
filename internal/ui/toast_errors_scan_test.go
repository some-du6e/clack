package ui

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// No toast shows a Go error: Karim's "Couldn't download: core: Fetch:
// file over 64 MB: slack: response too large" was one of about 100. A
// failure toasts Slack's own text for it (or Slack's generic one,
// popups.FailedToast) and the error goes to the log; where Slack only
// logs, Clack only logs. This scans internal/ui for a Toast call whose
// line reads an error's text (err.Error(), errText, friendlyErr and the
// details helpers that fall back to it).
func TestNoErrorTextInToasts(t *testing.T) {
	re := regexp.MustCompile(`\bToast[A-Za-z]*\(.*(\berr[A-Za-z]*\.Error\(\)|\berrText\(|\bfriendlyErr\(|\bdirErr\(|\bcdErr\(|\bcdErrCode\()`)
	var bad []string
	err := filepath.WalkDir(".", func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for n := 1; sc.Scan(); n++ {
			if line := sc.Text(); re.MatchString(line) {
				bad = append(bad, p+":"+strconv.Itoa(n)+": "+strings.TrimSpace(line))
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bad {
		t.Errorf("a toast shows an error's text (log it; toast Slack's text or popups.FailedToast): %s", b)
	}
}
