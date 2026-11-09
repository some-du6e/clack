package ui

// Clicking images in messages through Gio's router against fakeslack's
// #files-gallery (synthetic files only): every gallery tile opens Slack's
// image viewer on its own file, the "+ N" tile on the file under it, in
// the channel and in a thread; small and wide-short screenshots (thumbs
// only up to thumb_360) draw as gallery images, not "FILE" cards.

import (
	"image"
	"net/url"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/messages"
)

type openedImage struct {
	img richtext.Image
	ts  string
}

// recordLightbox makes OpenLightbox report what it opened (the viewer
// still opens).
func recordLightbox(t *testing.T) func() []openedImage {
	var mu sync.Mutex
	var got []openedImage
	prev := mkLightbox
	mkLightbox = func(a *App, img richtext.Image, m core.Message) Overlay {
		mu.Lock()
		got = append(got, openedImage{img, m.TS})
		mu.Unlock()
		return prev(a, img, m)
	}
	t.Cleanup(func() { mkLightbox = prev })
	return func() []openedImage {
		mu.Lock()
		defer mu.Unlock()
		return append([]openedImage(nil), got...)
	}
}

// clickAt is a primary click with its own timestamps (two clicks at the
// same time would be a double click).
func (e *perfEnv) clickAt(p image.Point, at time.Duration) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Time: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt, Time: at + time.Millisecond},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Time: at + 2*time.Millisecond},
	)
}

func (e *perfEnv) channelFiles(ch string) []messages.FileRect {
	if c := e.a.convos[ch+"/"]; c != nil {
		if l, ok := c.list.(channelList); ok {
			return l.FileRects()
		}
	}
	return nil
}

func (e *perfEnv) threadFiles(ch, ts string) []messages.FileRect {
	if c := e.a.convos[ch+"/"+ts]; c != nil {
		if l, ok := c.list.(threadList); ok {
			return l.FileRects()
		}
	}
	return nil
}

func rectsOf(rs []messages.FileRect, ts string) []messages.FileRect {
	var out []messages.FileRect
	for _, r := range rs {
		if r.TS == ts {
			out = append(out, r)
		}
	}
	return out
}

// clickTiles clicks the middle of every tile of message ts (rects from
// get) and checks the viewer opened on that tile's file; files are the
// message's files, so the last tile of a "+ N" gallery must be the file
// under it.
func clickTiles(t *testing.T, e *perfEnv, opened func() []openedImage, name, ts string, files []*richtext.File, get func() []messages.FileRect, clock *time.Duration) {
	t.Helper()
	rs := rectsOf(get(), ts)
	if len(rs) == 0 {
		t.Errorf("%s: no file on screen", name)
		return
	}
	if len(rs) > len(files) {
		t.Errorf("%s: %d tiles for %d files", name, len(rs), len(files))
		return
	}
	for i, r := range rs {
		if r.File.ID != files[i].ID {
			t.Errorf("%s: tile %d is %s, want %s", name, i, r.File.ID, files[i].ID)
		}
		if !r.File.IsImage {
			continue // videos open the player, not the image viewer
		}
		if r.R.Dy() < 100 || r.R.Dx() < 100 {
			t.Errorf("%s: tile %d is %v, not a gallery tile", name, i, r.R)
		}
		before := len(opened())
		*clock += 500 * time.Millisecond
		c := r.R.Min.Add(r.R.Max).Div(2)
		e.clickAt(c, *clock)
		for k := 0; k < 4; k++ {
			e.frame()
		}
		got := opened()
		if len(got) != before+1 {
			t.Errorf("%s: clicking tile %d (%s at %v) didn't open the image viewer", name, i, r.File.ID, c)
			continue
		}
		if o := got[len(got)-1]; o.img.URL != files[i].URL || o.ts != ts {
			t.Errorf("%s: tile %d opened %s (message %s), want %s (%s)", name, i, o.img.URL, o.ts, files[i].URL, ts)
		}
		e.a.dismissTop()
		for k := 0; k < 3; k++ {
			e.frame()
		}
	}
}

func TestFilesClickOpensViewer(t *testing.T) {
	opened := recordLightbox(t)
	e := filesEnv(t, image.Pt(1440, 6000), false)
	ch := fakeslack.FilesChannelID
	var clock time.Duration
	e.until("file tiles on screen", 10*time.Second, func() bool { return len(e.channelFiles(ch)) > 20 })
	cases := []string{"one image", "portrait image", "two images", "three images", "four images", "six images",
		"image and video", "own image", "wide short image", "tiny image"}
	for _, name := range cases {
		m := e.filesMsg(name)
		files := docFiles(m)
		for _, f := range files {
			if !f.IsImage && !f.IsVideo {
				continue
			}
			if f.IsImage && f.Thumb == nil {
				t.Errorf("%s: %s has no inline thumb (draws as a file card)", name, f.ID)
			}
		}
		clickTiles(t, e, opened, name, m.TS, files, func() []messages.FileRect { return e.channelFiles(ch) }, &clock)
	}
	if rs := rectsOf(e.channelFiles(ch), e.filesMsg("six images").TS); len(rs) >= 6 {
		t.Errorf("six images: %d tiles, want a \"+ N\" tile", len(rs))
	}

	// the same in a thread (narrower: the "+ N" tile comes sooner)
	root := ""
	for _, c := range e.fs.FilesCases() {
		if c.Name == fakeslack.FilesThread {
			root = c.TS
		}
	}
	e.a.OpenThread(ch, root)
	e.until("the thread's files", 15*time.Second, func() bool { return len(e.threadFiles(ch, root)) >= 2 })
	e.settle(300 * time.Millisecond)
	replies := e.fs.FilesReplies(root)
	if len(replies) != 3 {
		t.Fatalf("thread fixture: %d messages", len(replies))
	}
	c := e.a.data.core
	for _, r := range replies[1:] {
		m := c.MessageFromSlack(ch, &r)
		clickTiles(t, e, opened, "thread "+r.Text, r.TS, docFiles(m), func() []messages.FileRect { return e.threadFiles(ch, root) }, &clock)
	}
}

// A pasted screenshot whose message event beats Slack's processing (the
// file has a name and url_private but no mimetype, filetype or thumbs)
// starts as a file card, becomes a gallery image once files.info has the
// processed file, and then opens the viewer.
func TestFilesUnprocessedPasteBecomesImage(t *testing.T) {
	opened := recordLightbox(t)
	e := filesEnv(t, image.Pt(1440, 6000), false)
	ch := fakeslack.FilesChannelID
	const id, ts = "F0FILESPST01", "1762609000.000100"
	stub := map[string]any{"id": id, "created": 1762609000, "timestamp": 1762609000, "name": "image.png", "title": "image.png",
		"mimetype": "", "filetype": "", "pretty_type": "", "user": "U0FAKE00001", "size": 0, "mode": "hosted",
		"is_external": false, "url_private": e.fs.URL + "img/1022x125/" + id + ".png", "media_display_type": "unknown",
		"permalink": e.fs.URL + "files/" + id + "/image.png", "has_rich_preview": false, "file_access": "visible"}
	processed := map[string]any{}
	for k, v := range stub {
		processed[k] = v
	}
	for k, v := range map[string]any{"mimetype": "image/png", "filetype": "png", "pretty_type": "PNG", "size": 9000,
		"original_w": 1022, "original_h": 125, "thumb_64": e.fs.URL + "img/64x8/" + id + ".png", "thumb_160": e.fs.URL + "img/160x20/" + id + ".png",
		"thumb_360": e.fs.URL + "img/360x44/" + id + ".png", "thumb_360_w": 360, "thumb_360_h": 44} {
		processed[k] = v
	}
	var mu sync.Mutex
	infos := 0
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "files.info" || f.Get("file") != id {
			return nil, false
		}
		mu.Lock()
		defer mu.Unlock()
		infos++
		if infos == 1 { // still processing
			return map[string]any{"ok": true, "file": stub}, true
		}
		return map[string]any{"ok": true, "file": processed}, true
	})
	t.Cleanup(func() { e.fs.SetHook(nil) })
	c := e.a.data.core
	evs, stop := c.Subscribe()
	defer stop()
	c.SetRealtime(true)
	dl := time.After(10 * time.Second)
	for up := false; !up; {
		select {
		case ev := <-evs:
			up = ev.Type == "connection" && ev.Connected
		case <-dl:
			t.Fatal("no realtime connection")
		}
	}
	e.fs.Push(map[string]any{"type": "message", "subtype": "file_share", "channel": ch, "ts": ts, "event_ts": ts, "user": "U0FAKE00001",
		"text": "this too", "upload": true, "files": []any{stub}})
	e.until("the paste in the channel", 10*time.Second, func() bool { return len(rectsOf(e.channelFiles(ch), ts)) > 0 })
	e.until("the paste as a gallery image", 20*time.Second, func() bool {
		rs := rectsOf(e.channelFiles(ch), ts)
		return len(rs) == 1 && rs[0].File.IsImage && rs[0].File.Thumb != nil
	})
	m, ok := c.S.Find(ch, ts)
	if !ok {
		t.Fatal("the paste isn't stored")
	}
	var clock time.Duration
	clickTiles(t, e, opened, "processed paste", ts, docFiles(c.MessageFromSlack(ch, &m)), func() []messages.FileRect { return e.channelFiles(ch) }, &clock)
}
