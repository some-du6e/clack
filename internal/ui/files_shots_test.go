package ui

// Files in messages against internal/fakeslack's #files-gallery
// (synthetic files only; nothing talks to Slack): one message per way
// Slack draws a shared file (gallery v3 with one to six images, a
// portrait, a video, an image with a video, audio, a voice clip, a PDF,
// a snippet, a zip, two documents, and the viewer's own image). The
// render test opens the channel and checks the documents and the file
// menu (Save for later everywhere, Delete file only on the viewer's own
// image); with FILES_SHOTS_DIR set (the files-shots workflow) it saves
// the whole channel in one tall shot plus the file menu, the delete
// dialog and the viewer at 1440x900, light and dark.

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func filesEnv(t *testing.T, size image.Point, dark bool) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.Files = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.size = size
	if os.Getenv("FILES_SHOTS_DIR") != "" {
		if e.gpu != nil {
			e.gpu.Release()
		}
		w, err := pixeltest.NewWindow(size.X, size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	if dark {
		e.a.setDark(true)
	}
	e.openChannel(fakeslack.FilesChannelID)
	n := len(fs.FilesCases())
	e.until("the #files-gallery history", 20*time.Second, func() bool { return e.loadedMessages(fakeslack.FilesChannelID) >= n })
	e.settle(1500 * time.Millisecond) // thumbnails from the fake's /img
	return e
}

// filesShot saves the last frame (FILES_SHOTS_DIR only).
func (e *perfEnv) filesShot(name string) {
	dir := os.Getenv("FILES_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	e.settle(800 * time.Millisecond)
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

// filesMsg is the #files-gallery message for a FilesCase name.
func (e *perfEnv) filesMsg(name string) core.Message {
	e.tb.Helper()
	c := e.a.data.core
	for _, fc := range e.fs.FilesCases() {
		if fc.Name != name {
			continue
		}
		m, ok := c.S.Find(fakeslack.FilesChannelID, fc.TS)
		if !ok {
			e.tb.Fatalf("%s (%s) isn't loaded", name, fc.TS)
		}
		return c.MessageFromSlack(fakeslack.FilesChannelID, &m)
	}
	e.tb.Fatalf("no files case %q", name)
	return core.Message{}
}

func docFiles(m core.Message) []*richtext.File {
	var out []*richtext.File
	for _, b := range m.Doc.Blocks {
		if b.Kind == richtext.FileBlock && b.File != nil {
			out = append(out, b.File)
		}
	}
	return out
}

func fileMenuLabels(m *popups.Menu) string {
	var out []string
	for _, it := range m.Items() {
		if it.Separator {
			out = append(out, "-")
		} else {
			out = append(out, it.Label)
		}
	}
	return strings.Join(out, "|")
}

func TestFilesChannel(t *testing.T) {
	e := filesEnv(t, image.Pt(1440, 900), false)
	want := map[string]int{"one image": 1, "two images": 2, "three images": 3, "four images": 4, "six images": 6,
		"image and video": 2, "two documents": 2, "voice clip": 1}
	for name, n := range want {
		if got := len(docFiles(e.filesMsg(name))); got != n {
			t.Errorf("%s: %d file blocks, want %d", name, got, n)
		}
	}
	six := docFiles(e.filesMsg("six images"))
	for _, f := range six {
		if !f.IsImage || f.Thumb == nil || f.OrigW == 0 || f.Thumb.W == 0 {
			t.Errorf("gallery image %+v lacks the sizes gallery v3 needs", f)
		}
	}
	if v := docFiles(e.filesMsg("video"))[0]; !v.IsVideo || v.DurationMS != 83000 || v.Thumb == nil || v.Thumb.W != 1280 {
		t.Errorf("video %+v", v)
	}
	if c := docFiles(e.filesMsg("voice clip"))[0]; !c.IsAudio || c.Subtype != "slack_audio" || len(c.Waveform) == 0 {
		t.Errorf("voice clip %+v", c)
	}
	if s := docFiles(e.filesMsg("snippet"))[0]; !s.IsSnippet || s.Lines != 7 {
		t.Errorf("snippet %+v", s)
	}
	at := image.Pt(700, 400)
	own := e.filesMsg("own image")
	if got := fileMenuLabels(popups.NewFileMenu(e.a, at, own, docFiles(own)[0])); !strings.HasSuffix(got, "|-|Delete file") || !strings.Contains(got, "Save for later") {
		t.Errorf("own image menu %q, want Save for later and Delete file", got)
	}
	other := e.filesMsg("one image")
	if got := fileMenuLabels(popups.NewFileMenu(e.a, at, other, docFiles(other)[0])); strings.Contains(got, "Delete file") || !strings.Contains(got, "Save for later") {
		t.Errorf("another person's image menu %q", got)
	}
	zip := e.filesMsg("zip")
	if got := fileMenuLabels(popups.NewFileMenu(e.a, at, zip, docFiles(zip)[0])); got != "Share file…|Copy link|Download|Save for later" {
		t.Errorf("zip menu %q", got)
	}
	for i := 0; i < 3; i++ {
		e.frame()
	}
}

func TestFilesShots(t *testing.T) {
	if os.Getenv("FILES_SHOTS_DIR") == "" {
		t.Skip("FILES_SHOTS_DIR not set (the files-shots workflow sets it)")
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			// the whole channel in one tall window
			tall := filesEnv(t, image.Pt(1440, 3600), dark)
			tall.filesShot("files-channel-" + mode)
			tall.shutdown()

			e := filesEnv(t, image.Pt(1440, 900), dark)
			e.filesShot("files-bottom-" + mode)
			own := e.filesMsg("own image")
			e.a.ShowOverlay(popups.NewFileMenu(e.a, image.Pt(760, 420), own, docFiles(own)[0]))
			e.filesShot("files-menu-own-image-" + mode)
			e.a.dismissTop()
			popups.ConfirmDeleteFile(e.a, docFiles(own)[0])
			e.filesShot("files-delete-dialog-" + mode)
			e.a.dismissTop()
			six := e.filesMsg("six images")
			e.a.ShowOverlay(popups.NewLightbox(e.a, *docFiles(six)[1].Thumb, six))
			e.filesShot("files-viewer-" + mode)
			e.a.dismissTop()
		})
	}
}
