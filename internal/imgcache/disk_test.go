package imgcache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestThumbOK(t *testing.T) {
	for u, want := range map[string]bool{
		"https://files.slack.com/files-tmb/T0266FRGM-F1-abc/image_360.png": true,
		"https://files-edge.slack.com/files-tmb/T-F/a_360.png":             true,
		"https://files.slack.com/files-pri/T-F/image.png":                  false, // the full file
		"https://files.slack.com/files-tmb/T-F/a.png?t=xoxe-1":             false, // a signed URL
		"http://files.slack.com/files-tmb/T-F/a.png":                       false,
		"https://files.slack.com.evil.com/files-tmb/T-F/a.png":             false,
		"https://example.com/files-tmb/a.png":                              false,
	} {
		if got := ThumbOK(u); got != want {
			t.Errorf("ThumbOK(%q) = %v, want %v", u, got, want)
		}
	}
}

// The disk tier stays under its limit, oldest files out first.
func TestDiskTrimKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	c := New(nil, "", nil)
	c.SetDiskLimit(1000)
	now := time.Now()
	for i := 0; i < 10; i++ {
		p := filepath.Join(dir, "ab", "f"+string(rune('0'+i)))
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, make([]byte, 200), 0o600)
		at := now.Add(time.Duration(i-10) * time.Hour)
		_ = os.Chtimes(p, at, at)
	}
	c.trim(dir)
	var left []string
	var total int64
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			left = append(left, filepath.Base(p))
			total += info.Size()
		}
		return nil
	})
	if total > 750 {
		t.Fatalf("%d bytes left, want at most 3/4 of the 1000 limit", total)
	}
	for _, n := range left {
		if n < "f6" {
			t.Errorf("kept the old %s over newer files (left %v)", n, left)
		}
	}
}

// SetDir drops what the previous account's session decoded.
func TestSetDirDropsMemory(t *testing.T) {
	c := New(nil, "", nil)
	c.mu.Lock()
	c.entries[key{"https://ca.slack-edge.com/T-U-1-72", 24}] = c.lru.PushFront(&entry{n: 10})
	c.bytes = 10
	c.mu.Unlock()
	c.SetDir(t.TempDir())
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) != 0 || c.bytes != 0 {
		t.Fatalf("SetDir kept %d entries (%d bytes)", len(c.entries), c.bytes)
	}
}
