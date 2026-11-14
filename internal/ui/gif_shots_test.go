package ui

// GIFs in a channel, end to end against internal/fakeslack's #gif-wall
// (synthetic animated GIFs: fakeslack/gifs.go), light and dark:
//
//   - Animations on: the GIF file (its thumb_480_gif) and the /giphy
//     attachment play at their drawn size, past the old 160px cap: players
//     run while they show and their frames advance (with a GPU the tile's
//     pixels change between two frames).
//   - Accessibility > "Allow animations" off (a11y_animations false):
//     nothing plays; the file draws its deanimate still with Slack's Play
//     button; hovering shows FileActions' Expand; a click on the still
//     plays it; Expand opens the viewer.
//
// IMAGE_SEND_SHOTS_DIR (image-send.yml) saves each stage: gif-*.png.

import (
	"bytes"
	"image"
	"os"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

func gifEnv(t *testing.T, dark, anim bool) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.GIFs = 6
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	if !anim {
		fs.SetPrefs(map[string]any{"a11y_animations": false})
		t.Cleanup(uipref.Reset)
	}
	e := startEnv(t, fs, t.TempDir())
	e.size = image.Pt(1280, 800)
	if os.Getenv("IMAGE_SEND_SHOTS_DIR") != "" {
		if e.gpu != nil {
			e.gpu.Release()
		}
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	if dark {
		e.a.setDark(true)
	}
	e.openChannel(fakeslack.GIFWallID)
	e.until("#gif-wall's history", 20*time.Second, func() bool { return e.loadedMessages(fakeslack.GIFWallID) > 0 })
	e.settle(800 * time.Millisecond)
	e.toLatest()
	return e
}

// gifTile is the newest GIF file's tile, scrolled whole into view: under
// the channel header, over the composer (FileRects lists the rows' boxes
// as laid out, not as clipped by the list).
func (e *perfEnv) gifTile(t *testing.T) messages.FileRect {
	t.Helper()
	var tile messages.FileRect
	for i := 0; i < 10; i++ {
		tile = messages.FileRect{}
		e.until("a GIF file's tile", 10*time.Second, func() bool {
			for _, r := range e.channelFiles(fakeslack.GIFWallID) {
				if r.File.Type == "gif" && r.R.Max.Y <= e.size.Y {
					tile = r
				}
			}
			return tile.File != nil
		})
		if tile.R.Min.Y >= 150 && tile.R.Max.Y <= e.size.Y-180 {
			return tile
		}
		e.scroll(e.listPoint(), float32(tile.R.Min.Y-200)) // its top to y=200
		e.settle(250 * time.Millisecond)
	}
	t.Fatalf("the GIF file's tile stays at %v", tile.R)
	return tile
}

// framesFor runs frames for d and reports how many ran, and the fewest
// frame advances that proves GIFs at 40 ms a frame play: a player moves
// on at most once a drawn frame (llvmpipe draws a frame in ~100 ms).
func (e *perfEnv) framesFor(d time.Duration) (frames int, want uint64) {
	for dl := time.Now().Add(d); time.Now().Before(dl); frames++ {
		e.frame()
		time.Sleep(2 * time.Millisecond)
	}
	return frames, uint64(min(frames, int(d/(40*time.Millisecond))) / 2)
}

// grab is the window's pixels in r (nil without a GPU).
func (e *perfEnv) grab(r image.Rectangle) []byte {
	if e.gpu == nil {
		return nil
	}
	m := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(m); err != nil {
		e.tb.Fatalf("screenshot: %v", err)
	}
	return append([]byte(nil), m.SubImage(r.Intersect(m.Rect)).(*image.RGBA).Pix...)
}

func TestGIFShots(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode+"/playing", func(t *testing.T) {
			e := gifEnv(t, dark, true)
			defer e.shutdown()
			images := e.a.images
			tile := e.gifTile(t)
			if !images.Animating() {
				t.Fatal("animations are off")
			}
			e.until("GIFs playing", 10*time.Second, func() bool { return images.Stats().Players >= 2 })
			s0 := images.Stats()
			a := e.grab(tile.R)
			n, want := e.framesFor(600 * time.Millisecond) // 15 GIF frames at 40 ms
			s1 := images.Stats()
			if d := s1.Advances - s0.Advances; d < want {
				t.Errorf("600 ms on screen (%d frames drawn): %d GIF frames advanced, want %d or more", n, d, want)
			}
			if sz := s1.LargestFrame; max(sz.X, sz.Y) <= 160 {
				t.Errorf("the largest frame playing is %v: past 160px GIFs should play at their drawn size", sz)
			}
			if !images.Loaded(tile.File.Thumb.URL) || tile.File.Still == nil || images.Loaded(tile.File.Still.URL) {
				t.Errorf("animations on: the tile should draw %s, not the still", tile.File.Thumb.URL)
			}
			if b := e.grab(tile.R); a != nil && bytes.Equal(a, b) {
				t.Error("the GIF tile's pixels are the same 600 ms later")
			}
			e.sendShot("gif-1-playing-" + mode)
			e.settle(250 * time.Millisecond)
			e.sendShot("gif-2-playing-later-" + mode)
		})
		t.Run(mode+"/still", func(t *testing.T) {
			e := gifEnv(t, dark, false)
			defer e.shutdown()
			images := e.a.images
			if images.Animating() {
				t.Fatal("a11y_animations off, but animations are on")
			}
			tile := e.gifTile(t)
			e.settle(500 * time.Millisecond)
			if s := images.Stats(); s.Players != 0 {
				t.Errorf("animations off: %d players", s.Players)
			}
			if tile.File.Still == nil || !images.Loaded(tile.File.Still.URL) || images.Loaded(tile.File.Thumb.URL) {
				t.Errorf("animations off: the tile should draw the deanimate still, not %s", tile.File.Thumb.URL)
			}
			e.sendShot("gif-3-still-" + mode)
			mid := tile.R.Min.Add(tile.R.Max).Div(2)
			e.move(f32.Pt(float32(mid.X), float32(mid.Y)))
			e.settle(300 * time.Millisecond)
			e.sendShot("gif-4-still-hover-expand-" + mode)

			// a click on the still plays it
			e.clickAt(mid, time.Second)
			e.until("the still the viewer played", 5*time.Second, func() bool { return images.Stats().Players >= 1 })
			s0 := images.Stats()
			n, want := e.framesFor(500 * time.Millisecond)
			if d := images.Stats().Advances - s0.Advances; d < want {
				t.Errorf("played: %d GIF frames advanced in 500 ms (%d frames drawn), want %d or more", d, n, want)
			}
			e.sendShot("gif-5-played-" + mode)

			// Expand (top 8, right 8, 32px) opens the viewer
			opened := recordLightbox(t)
			btn := image.Pt(tile.R.Max.X-8-16, tile.R.Min.Y+8+16)
			e.move(f32.Pt(float32(btn.X), float32(btn.Y)))
			e.settle(800 * time.Millisecond) // the tooltip
			e.sendShot("gif-6-expand-tooltip-" + mode)
			e.clickAt(btn, 3*time.Second)
			e.frame()
			e.frame()
			if got := opened(); len(got) != 1 {
				t.Errorf("Expand opened %d viewers", len(got))
			}
			e.key(key.NameEscape, 0)
			e.frame()
		})
	}
}
