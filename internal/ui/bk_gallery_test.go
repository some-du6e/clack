package ui

// The blockkit-gallery channel (internal/fakeslack gallery*.go: every
// Block Kit block and variant, synthetic) opened in the real client. With
// BK_SHOTS_DIR set (the bk-gallery-shots workflow) the channel is saved
// top to bottom in light and dark, plus the "Show more" thread.

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
)

func TestBlockKitGallery(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	dir := os.Getenv("BK_SHOTS_DIR")
	if dir != "" && e.gpu == nil {
		w, err := headless.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	ch := fakeslack.GalleryChannelID
	n := len(fs.Gallery())
	e.openChannel(ch)
	e.until("gallery history", 20*time.Second, func() bool { return e.loadedMessages(ch) >= n })
	e.settle(800 * time.Millisecond) // images

	// snap saves the frame and returns its PNG bytes (nil without a GPU).
	snap := func(name string) []byte {
		if dir == "" || e.gpu == nil {
			return nil
		}
		img := image.NewRGBA(image.Rectangle{Max: e.size})
		if err := e.gpu.Screenshot(img); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".png"), buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		e.a.setDark(dark)
		e.settle(300 * time.Millisecond)
		// to the oldest message, then a page at a time down to the newest
		for i := 0; i < 20; i++ {
			e.scroll(e.listPoint(), -4000)
			e.settle(30 * time.Millisecond)
		}
		e.settle(600 * time.Millisecond)
		var prev []byte
		for page := 1; page <= 30; page++ {
			cur := snap("gallery-" + mode + "-" + twoDigits(page))
			if cur == nil || bytes.Equal(cur, prev) {
				if cur != nil {
					_ = os.Remove(filepath.Join(dir, "gallery-"+mode+"-"+twoDigits(page)+".png"))
				}
				break
			}
			prev = cur
			e.scroll(e.listPoint(), float32(e.size.Y-e.a.primaryOrigin.Y)*0.7)
			e.settle(500 * time.Millisecond)
		}

		// the thread: a long section cut at "Show more", the actions under it
		ts := fs.GalleryTS("layout/show more")
		e.a.OpenThread(ch, ts)
		e.until("thread replies", 20*time.Second, func() bool {
			msgs, _ := e.a.data.core.S.Thread(ch, ts)
			return len(msgs) >= 2
		})
		e.settle(800 * time.Millisecond)
		snap("gallery-thread-show-more-" + mode)
	}
}

func twoDigits(i int) string {
	return string([]byte{byte('0' + i/10%10), byte('0' + i%10)})
}
