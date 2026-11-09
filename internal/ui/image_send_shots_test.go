package ui

// Sending an image from the composer, end to end against internal/fakeslack
// (synthetic images only; nothing talks to Slack): the tile while the
// eager upload is processing, the placeholder row right after Enter
// ("Uploading…" over the File's own bytes at .5 with a spinner), and the
// final row (the text, "image.png ▾", then the image) once the share's
// echo replaced it; then the same with an image and no text. The test
// checks the row sequence: one row per send, never a FILE card, the text
// and the image together from the first frame. With IMAGE_SEND_SHOTS_DIR
// set (the image-send workflow) it saves each stage, light and dark.

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

func sendSynthPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(30 + x*170/w), uint8(60 + y*150/h), 190, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func (e *perfEnv) sendShot(name string) {
	dir := os.Getenv("IMAGE_SEND_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	e.settle(300 * time.Millisecond)
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		e.tb.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		e.tb.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		e.tb.Fatal(err)
	}
}

func imageSendEnv(t *testing.T, dark bool) (*perfEnv, string) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.ProcessDelay = 2500 * time.Millisecond // long enough to see the tile and the placeholder
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
	ch := fs.ChannelIDs()[2]
	e.openChannel(ch)
	e.until("the channel's history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.settle(800 * time.Millisecond)
	return e, ch
}

// composer is the open channel's composer.
func (e *perfEnv) channelComposer(ch string) composerBox {
	e.tb.Helper()
	cv := e.a.convos[ch+"/"]
	if cv == nil || cv.comp == nil || cv.compRect.Empty() {
		e.tb.Fatal("no composer laid out")
	}
	cb, ok := cv.comp.(composerBox)
	if !ok {
		e.tb.Fatalf("the composer is a %T", cv.comp)
	}
	r := cv.compRect
	e.click(r.Min.Add(image.Pt(60, r.Dy()/2)))
	e.frame()
	return cb
}

func (e *perfEnv) rowsFor(ch, cid string) []core.Message {
	c := e.a.data.core
	var out []core.Message
	if tl := c.S.Timeline(ch); tl != nil {
		for i := range tl.Msgs {
			if m := tl.Msgs[i]; m.ClientMsgID == cid {
				out = append(out, c.MessageFromSlack(ch, &m))
			}
		}
	}
	return out
}

func TestImageSendShots(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e, ch := imageSendEnv(t, dark)
			defer e.shutdown()
			c := e.a.data.core
			evs, stop := c.Subscribe()
			defer stop()
			var cids []string
			watch := func() {
				for {
					select {
					case ev := <-evs:
						if ev.Type == "message" && ev.Msg != nil && ev.Msg.Pending && ev.Channel == ch {
							cids = append(cids, ev.ClientMsgID)
						}
					default:
						return
					}
				}
			}

			// 1. text and an image: the tile, the placeholder, the final row
			cb := e.channelComposer(ch)
			e.typeText("look at the new activity page")
			e.frame()
			cb.AttachFile("image.png", sendSynthPNG(t, 640, 400))
			e.settle(600 * time.Millisecond)
			e.sendShot("image-send-1-tile-" + mode)
			e.key(key.NameReturn, 0)
			e.frame()
			if cb.Attached() != 0 || cb.DocText() != "" {
				t.Fatalf("after Enter the composer holds %d files and %q", cb.Attached(), cb.DocText())
			}
			e.until("the placeholder", 5*time.Second, func() bool { watch(); return len(cids) == 1 })
			e.sendShot("image-send-2-placeholder-" + mode)
			checkRows := func(cid string, wantText bool) {
				t.Helper()
				for _, m := range e.rowsFor(ch, cid) {
					var text, img, card bool
					for _, b := range m.Doc.Blocks {
						switch {
						case b.Kind == richtext.FileBlock && b.File != nil && b.File.IsImage:
							img = true
						case b.Kind == richtext.FileBlock:
							card = true
						case b.Kind == richtext.Paragraph:
							text = true
						}
					}
					if !img || card || text != wantText {
						t.Errorf("row %s (pending %v): image %v, FILE card %v, text %v", m.TS, m.Pending, img, card, text)
					}
				}
			}
			e.until("the shared message", 15*time.Second, func() bool {
				watch()
				checkRows(cids[0], true)
				rs := e.rowsFor(ch, cids[0])
				return len(rs) == 1 && !rs[0].Pending
			})
			e.settle(1500 * time.Millisecond) // the thumbnail from the fake's /img
			if rs := e.rowsFor(ch, cids[0]); len(rs) != 1 {
				t.Errorf("%d rows for the send, want one", len(rs))
			}
			e.sendShot("image-send-3-final-" + mode)

			// 2. an image with no text (Karim's send)
			cb = e.channelComposer(ch)
			cb.AttachFile("image.png", sendSynthPNG(t, 480, 300))
			e.settle(300 * time.Millisecond)
			e.key(key.NameReturn, 0)
			e.frame()
			e.until("the second placeholder", 5*time.Second, func() bool { watch(); return len(cids) == 2 })
			e.sendShot("image-send-4-placeholder-image-only-" + mode)
			e.until("the second shared message", 15*time.Second, func() bool {
				watch()
				checkRows(cids[1], false)
				rs := e.rowsFor(ch, cids[1])
				return len(rs) == 1 && !rs[0].Pending
			})
			e.settle(1500 * time.Millisecond)
			e.sendShot("image-send-5-final-image-only-" + mode)
			if n := len(e.fs.Shared()); n != 2 {
				t.Errorf("%d shares, want 2", n)
			}
		})
	}
}
