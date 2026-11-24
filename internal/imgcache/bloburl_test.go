package imgcache

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// A blob: URL (a plugin's object URL: HMojis' emoji images) loads from
// the blob source the UI sets, the plugins' runtime: drawn at its size,
// nothing asked of the fetcher, nothing written to the disk tier. A URL
// the source doesn't hold (revoked), or any with no source, fails.
func TestBlobURLImages(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			src.SetNRGBA(x, y, color.NRGBA{G: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	var asked atomic.Int32
	fetch := func(ctx context.Context, raw string) ([]byte, error) {
		asked.Add(1)
		return nil, errors.New("no network in this test")
	}
	dir := t.TempDir()
	c := New(fetch, dir, nil)
	defer c.Close()
	t.Cleanup(func() { SetBlobSource(nil) })
	SetBlobSource(func(u string) ([]byte, bool) {
		if u == "blob:clack/held" {
			return buf.Bytes(), true
		}
		return nil, false
	})
	wait := func(u string, want bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, ok := c.Get(u, 0); ok {
				if !want {
					t.Fatalf("%s was drawn", u)
				}
				return
			}
			if c.Failed(u, 0) {
				if want {
					t.Fatalf("%s failed to load", u)
				}
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s neither drew nor failed in 10s", u)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	wait("blob:clack/held", true)
	if sz, _ := c.Size("blob:clack/held", 0); sz != image.Pt(3, 2) {
		t.Errorf("drawn at %v, want its 3×2", sz)
	}
	wait("blob:clack/revoked", false)
	SetBlobSource(nil)
	wait("blob:clack/no-source", false)
	if n := asked.Load(); n != 0 {
		t.Errorf("the fetcher was asked %d times; a blob: URL never reaches the network", n)
	}
	files := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files++
		}
		return nil
	})
	if files != 0 {
		t.Errorf("%d files in the disk tier, want none", files)
	}
}
