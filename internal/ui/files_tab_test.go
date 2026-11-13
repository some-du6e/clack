package ui

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// scanHover moves the pointer from a to b in 4px steps until c is
// hovered; ok is false when it never is.
func (e *perfEnv) scanHover(c *widget.Clickable, a, b image.Point) (image.Point, bool) {
	d := b.Sub(a)
	n := max(absPx(d.X), absPx(d.Y)) / 4
	for i := 0; i <= n; i++ {
		p := a.Add(image.Pt(d.X*i/max(n, 1), d.Y*i/max(n, 1)))
		e.move(f32.Pt(float32(p.X), float32(p.Y)))
		if c.Hovered() {
			return p, true
		}
	}
	return image.Point{}, false
}

func absPx(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// The Files & links tab in #files-gallery's header (Slack's files tab,
// canvas-browser): a click opens the tab's view in the channel body,
// which reads Slack's three browsers; Messages goes back.
func TestFilesTabOpens(t *testing.T) {
	sz := fakeslack.Small
	sz.Files = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	ch := fakeslack.FilesChannelID
	e.openChannel(ch)
	e.until("the gallery's tabs", 10*time.Second, func() bool { return e.a.data.tabsDone[ch] })
	tabs := e.a.channelTabs(ch)
	var files chTab
	for _, tb := range tabs {
		if isFilesTab(tb) {
			files = tb
		}
	}
	if files.key == "" || files.label != "Files & links" || files.icon != "canvas-browser" {
		t.Fatalf("tabs %+v", tabs)
	}
	msgs := e.a.clicks.get("tab." + ch + ".messages")
	o := e.a.primaryOrigin
	mp, ok := e.scanHover(msgs, image.Pt(o.X+40, o.Y), image.Pt(o.X+40, o.Y+200))
	if !ok {
		t.Fatal("no Messages tab in the header")
	}
	btn := e.a.clicks.get("tab." + ch + "." + files.key)
	fp, ok := e.scanHover(btn, mp, image.Pt(e.size.X-60, mp.Y))
	if !ok {
		t.Fatal("no Files & links tab in the header")
	}
	n := len(fs.SearchForms("search.modules.files"))
	e.click(fp)
	e.frame()
	e.until("the Files & links view", 5*time.Second, func() bool { return e.a.filesTabView(ch) != nil })
	e.until("the tab's reads", 10*time.Second, func() bool {
		return len(fs.SearchForms("search.modules.files")) >= n+2 && len(fs.ChannelLinksForms()) >= 1
	})
	for _, f := range fs.SearchForms("search.modules.files")[n:] {
		if f.Get("search_context") != "desktop_files_channel_tab" {
			t.Errorf("search_context %q", f.Get("search_context"))
		}
	}
	// back to Messages
	e.click(mp)
	e.frame()
	if e.a.filesTabView(ch) != nil {
		t.Error("the Files & links view stayed after Messages")
	}
}
