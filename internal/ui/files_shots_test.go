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

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
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

// fileInView is the #files-gallery message name's first file box,
// scrolled whole into view between the header and the composer (the
// rects are the rows' layout boxes, not clipped by the list); a box taller
// than the pane, with its top in view. Off screen, the list scrolls
// toward it: older messages are above.
func (e *perfEnv) fileInView(t *testing.T, name string) messages.FileRect {
	t.Helper()
	ts := ""
	for _, fc := range e.fs.FilesCases() {
		if fc.Name == name {
			ts = fc.TS
		}
	}
	var fr messages.FileRect
	for i := 0; i < 40; i++ {
		e.frame()
		all := e.channelFiles(fakeslack.FilesChannelID)
		rs := rectsOf(all, ts)
		if len(rs) == 0 {
			dy := float32(-500) // up: the channel opens at its latest
			for _, r := range all {
				if r.TS < ts {
					dy = 500 // something older is showing: it's below
					break
				}
			}
			e.scroll(e.listPoint(), dy)
			e.settle(150 * time.Millisecond)
			continue
		}
		fr = rs[0]
		tall := fr.R.Dy() > e.size.Y-330 // taller than the pane: its top in view
		if fr.R.Min.Y >= 150 && (fr.R.Max.Y <= e.size.Y-180 || tall && fr.R.Min.Y <= 260) {
			return fr
		}
		e.scroll(e.listPoint(), float32(fr.R.Min.Y-220))
		e.settle(200 * time.Millisecond)
	}
	t.Fatalf("%s's file stays at %v", name, fr.R)
	return fr
}

// fileActions is the FileActions shown now on message ts's file: the
// buttons' actions in order and their boxes.
func (e *perfEnv) fileActions(ts string) ([]richview.FileAction, []image.Rectangle) {
	var acts []richview.FileAction
	var rs []image.Rectangle
	if c := e.a.convos[fakeslack.FilesChannelID+"/"]; c != nil {
		if l, ok := c.list.(channelList); ok {
			for _, r := range l.FileActionRects() {
				if r.TS == ts {
					acts = append(acts, r.Action)
					rs = append(rs, r.R)
				}
			}
		}
	}
	return acts, rs
}

// TestFileSnippetExpand: the viewer's long snippet in #files-gallery is
// collapsed with its FileActions (Download, Edit, Share, View file
// details, More actions) on hover; a click on it calls files.info and
// shows the whole highlighted content with Collapse first in the bar and
// the truncation note (the fake server cut it); Show less collapses it.
func TestFileSnippetExpand(t *testing.T) {
	e := filesEnv(t, image.Pt(1440, 900), false)
	long := e.filesMsg("long snippet")
	f := docFiles(long)[0]
	if !f.IsSnippet || f.Highlight == nil || len(f.Highlight.Lines) != 12 || f.LinesMore != 28 || f.Expanded {
		t.Fatalf("long snippet %+v", f)
	}
	fr := e.fileInView(t, "long snippet")
	mid := fr.R.Min.Add(fr.R.Max).Div(2)
	e.move(f32.Pt(float32(mid.X), float32(mid.Y)))
	e.settle(200 * time.Millisecond)
	acts, _ := e.fileActions(long.TS)
	want := []richview.FileAction{richview.FileDownload, richview.FileEdit, richview.FileShare, richview.FileDetails, richview.FileMore}
	if !sameActs(acts, want) {
		t.Errorf("collapsed snippet's FileActions %v, want %v", acts, want)
	}
	n := e.fs.CallCount("files.info")
	e.clickAt(mid, time.Second)
	e.until("the snippet expanded", 10*time.Second, func() bool {
		m := e.filesMsg("long snippet")
		g := docFiles(m)[0]
		return g.Expanded && g.Content != nil && len(g.Content.Lines) == 40
	})
	if e.fs.CallCount("files.info") <= n {
		t.Error("no files.info for the content")
	}
	g := docFiles(e.filesMsg("long snippet"))[0]
	if !g.ContentTruncated {
		t.Error("the server's content_highlight_html_truncated isn't kept")
	}
	e.settle(300 * time.Millisecond)
	fr = e.fileInView(t, "long snippet")
	e.move(f32.Pt(float32(fr.R.Min.X+80), float32(fr.R.Min.Y+60)))
	e.settle(200 * time.Millisecond)
	acts, _ = e.fileActions(long.TS)
	if len(acts) == 0 || acts[0] != richview.FileCollapse {
		t.Errorf("expanded snippet's FileActions %v, want Collapse first", acts)
	}
	e.a.data.core.CollapseFile(fakeslack.FilesChannelID, long.TS, f.ID)
	e.until("the snippet collapsed", 5*time.Second, func() bool { return !docFiles(e.filesMsg("long snippet"))[0].Expanded })
}

func sameActs(a, b []richview.FileAction) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestFileActionsShots saves the file cards' hover states, light and
// dark (FILES_SHOTS_DIR, the files-shots workflow): a highlighted snippet
// with its FileActions and a button's tooltip, the viewer's long snippet
// collapsed (Show more) and expanded (Show less, the truncation note), a
// PDF pillow and a gallery image with theirs.
func TestFileActionsShots(t *testing.T) {
	if os.Getenv("FILES_SHOTS_DIR") == "" {
		t.Skip("FILES_SHOTS_DIR not set (the files-shots workflow sets it)")
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := filesEnv(t, image.Pt(1440, 900), dark)
			defer e.shutdown()
			hover := func(name string, dx, dy int) messages.FileRect {
				fr := e.fileInView(t, name)
				x, y := fr.R.Min.X+dx, fr.R.Min.Y+dy
				if dx < 0 {
					x = fr.R.Max.X + dx
				}
				e.move(f32.Pt(float32(x), float32(y)))
				e.settle(300 * time.Millisecond)
				return fr
			}
			// fib.py: the bar, and More actions' tooltip (top-right)
			hover("snippet", 120, 40)
			e.filesShot("files-snippet-hover-" + mode)
			m := e.filesMsg("snippet")
			if _, rs := e.fileActions(m.TS); len(rs) > 0 {
				c := rs[len(rs)-1].Min.Add(rs[len(rs)-1].Max).Div(2)
				e.move(f32.Pt(float32(c.X), float32(c.Y)))
				e.settle(800 * time.Millisecond)
				e.filesShot("files-snippet-tooltip-" + mode)
			} else {
				t.Error("fib.py shows no FileActions")
			}
			// the long one: collapsed under the fade with Show more
			fr := hover("long snippet", 120, 80)
			e.filesShot("files-snippet-collapsed-" + mode)
			mid := fr.R.Min.Add(fr.R.Max).Div(2)
			e.clickAt(mid, time.Second)
			e.until("the snippet expanded", 10*time.Second, func() bool {
				g := docFiles(e.filesMsg("long snippet"))[0]
				return g.Expanded && g.Content != nil
			})
			e.settle(500 * time.Millisecond)
			// expanded: its bottom (Show less, the truncation note)
			fr = e.fileInView(t, "long snippet")
			if fr.R.Max.Y > e.size.Y-180 {
				e.scroll(e.listPoint(), float32(fr.R.Max.Y-(e.size.Y-240)))
				e.settle(300 * time.Millisecond)
			}
			for _, r := range rectsOf(e.channelFiles(fakeslack.FilesChannelID), e.filesMsg("long snippet").TS) {
				e.move(f32.Pt(float32(r.R.Min.X+120), float32(r.R.Max.Y-60)))
			}
			e.settle(300 * time.Millisecond)
			e.filesShot("files-snippet-expanded-" + mode)
			e.a.data.core.CollapseFile(fakeslack.FilesChannelID, e.filesMsg("long snippet").TS, docFiles(e.filesMsg("long snippet"))[0].ID)
			e.settle(300 * time.Millisecond)
			// a PDF pillow and a gallery image
			hover("pdf", 60, 30)
			e.filesShot("files-pdf-hover-" + mode)
			hover("one image", 60, 60)
			e.filesShot("files-image-hover-" + mode)
		})
	}
}
