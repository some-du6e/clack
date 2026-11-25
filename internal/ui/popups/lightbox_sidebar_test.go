package popups

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// mvdHost is a Host with the app's FileDetails (FileDetailsHost): each
// pane it hands out records its file and draws a marker with a button.
type mvdHost struct {
	*tzHost
	mu     sync.Mutex
	panes  []*mvdPane
	opened []string // OpenFileDetails' file id|channel
}

func (h *mvdHost) FileDetailsPane(f *richtext.File, channel string) FileDetailsPane {
	p := &mvdPane{id: f.ID, channel: channel}
	h.mu.Lock()
	h.panes = append(h.panes, p)
	h.mu.Unlock()
	return p
}

func (h *mvdHost) OpenFileDetails(f *richtext.File, channel string) {
	h.mu.Lock()
	h.opened = append(h.opened, f.ID+"|"+channel)
	h.mu.Unlock()
}

func (h *mvdHost) paneIDs() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var ids []string
	for _, p := range h.panes {
		ids = append(ids, p.id)
	}
	return strings.Join(ids, ",")
}

// mvdPane is a fake FileDetails: a marker filling its box, and a button
// (a focusable widget of the pane's own) at its top left.
type mvdPane struct {
	id, channel string
	size        image.Point // the box it was given, last frame
	frames      int
	btn         widget.Clickable
}

func (p *mvdPane) FileID() string { return p.id }

func (p *mvdPane) Layout(gtx layout.Context) layout.Dimensions {
	p.frames++
	p.size = gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: p.size}, rgb(0x00ff00))
	gtx.Constraints = layout.Exact(image.Pt(40, 20))
	p.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	})
	return layout.Dimensions{Size: p.size}
}

// mvdEnv lays out a Lightbox at a window size on a real router, over an
// image cache that serves every image as a 900x600 PNG, with the Details
// gate in group.
type mvdEnv struct {
	t    *testing.T
	h    *mvdHost
	lb   *Lightbox
	r    input.Router
	ops  op.Ops
	now  time.Time
	size image.Point
	tick time.Duration
}

func newMVDEnv(t *testing.T, group string, open func(h Host) *Lightbox) *mvdEnv {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 900, 600))); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	serve := func(context.Context, string) ([]byte, error) { return data, nil }
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	a.SetExperimentGroup(core.ExpMediaViewerFileDetailsTab, group)
	h := &mvdHost{tzHost: &tzHost{a: a, th: theme.New(false), im: imgcache.New(serve, "", nil)}}
	e := &mvdEnv{t: t, h: h, now: time.Now(), size: image.Pt(1200, 900)}
	e.lb = open(h)
	if e.lb == nil {
		t.Fatal("no viewer")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		e.frame()
		if _, ok, _ := lbShown(h.im, e.lb.cur()); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the viewer's image never loaded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.frame()
	return e
}

func (e *mvdEnv) frame() { e.frameThen(nil) }

// frameThen lays a frame out and runs then on its context after the
// viewer (a FocusCmd for a tag the frame drew).
func (e *mvdEnv) frameThen(then func(gtx layout.Context)) {
	e.ops.Reset()
	e.now = e.now.Add(20 * time.Millisecond)
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(e.size),
		Now: e.now, Source: e.r.Source()}
	e.lb.Layout(gtx)
	if then != nil {
		then(gtx)
	}
	e.r.Frame(&e.ops)
}

func (e *mvdEnv) frames(n int) {
	for i := 0; i < n; i++ {
		e.frame()
	}
}

// click is a primary click at p.
func (e *mvdEnv) click(p image.Point) {
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.tick += time.Second
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Position: pt, Source: pointer.Mouse, Time: e.tick},
		pointer.Event{Kind: pointer.Press, Position: pt, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: e.tick + 10*time.Millisecond},
		pointer.Event{Kind: pointer.Release, Position: pt, Source: pointer.Mouse, Time: e.tick + 60*time.Millisecond},
	)
	e.frames(3)
}

// press is a key pressed and released.
func (e *mvdEnv) press(name key.Name) {
	e.r.Queue(key.Event{Name: name, State: key.Press}, key.Event{Name: name, State: key.Release})
	e.frames(2)
}

// focus gives tag the key focus (as Tab would) and lays out a few frames.
func (e *mvdEnv) focus(tag event.Tag) {
	e.frameThen(func(gtx layout.Context) { gtx.Execute(key.FocusCmd{Tag: tag}) })
	e.frames(3)
}

// menuItem is the row of m labelled label.
func menuItem(t *testing.T, m *Menu, label string) MenuItem {
	t.Helper()
	if m == nil {
		t.Fatalf("no menu (looking for %q)", label)
	}
	for _, it := range m.Items() {
		if it.Label == label {
			return it
		}
	}
	t.Fatalf("menu %q has no %q", ctxLabels(m), label)
	return MenuItem{}
}

func mvdImageFile(id, name string) *richtext.File {
	return &richtext.File{ID: id, Name: name, Title: name, Mime: "image/png", IsImage: true,
		URL:       "https://files.slack.com/files-pri/T1-" + id + "/" + name,
		Permalink: "https://fake.slack.com/files/U1/" + id + "/" + name,
		Thumb:     &richtext.Image{URL: "https://files.slack.com/files-tmb/T1-" + id + "-abc/" + name, W: 360, H: 240, Proxy: true}}
}

// mvdGallery is a viewer opened outside a message (OpenFileViewer's
// NewFileGallery, so no thread): two image files, then an image that
// isn't a file (no id: no details).
func mvdGallery(h Host) *Lightbox {
	f1, f2 := mvdImageFile("F0MVD00001", "one.png"), mvdImageFile("F0MVD00002", "two.png")
	lb := NewFileGallery(h, []GalleryItem{
		{Img: *f1.Thumb, File: f1},
		{Img: *f2.Thumb, File: f2},
		{Img: richtext.Image{URL: "https://files.slack.com/files-tmb/T1-x/three.png", W: 900, H: 600, Proxy: true}},
	}, 0)
	if lb != nil {
		lb.channel = "C0MVD"
	}
	return lb
}

// openDetails chooses "View file details" in the image's More actions.
func (e *mvdEnv) openDetails() {
	e.t.Helper()
	menuItem(e.t, e.lb.moreMenu(), "View file details").Do()
	e.frames(2)
}

// With the gate on, "View file details" (Y4Ir, between Open in browser
// and Copy image link, data-qa view_file_details) opens the sidebar on
// one Details tab: --single_tab (no underline), "Close details" (no
// thread), the pane for the item's file in the tab panel under the 50px
// bar, and the media column narrower by the sidebar, which is half the
// row at most 400, 360 or 300 by the window's width.
func TestMediaViewerDetailsTab(t *testing.T) {
	e := newMVDEnv(t, "treatment", mvdGallery)
	if ids, _ := e.lb.SidebarTabs(); ids != nil {
		t.Fatalf("the viewer opened with tabs %q", ids)
	}
	_, media := e.lb.SidebarRect()
	if want := image.Rect(28, 28, 1172, 872); media != want {
		t.Fatalf("media column %v with the sidebar closed, want the panel %v", media, want)
	}
	m := e.lb.moreMenu()
	if got := ctxLabels(m); got != "Open in browser|View file details|Copy image link" { // no thread: no separator (separator-view-thread-view-details)
		t.Errorf("image More actions %q", got)
	}
	if it := menuItem(t, m, "View file details"); it.QA != "view_file_details" {
		t.Errorf("View file details' data-qa %q", it.QA)
	}
	e.openDetails()
	ids, active := e.lb.SidebarTabs()
	if strings.Join(ids, ",") != "details" || active != "details" {
		t.Fatalf("tabs %q (active %q), want one Details tab", ids, active)
	}
	if got := e.lb.side.underline; len(got) != 1 || got[0] {
		t.Errorf("a lone Details tab drew the selected underline (%v): --single_tab has none", got)
	}
	if got := e.lb.CloseLabel(); got != "Close details" {
		t.Errorf("close label %q, want Close details", got)
	}
	side, media := e.lb.SidebarRect()
	if want := image.Rect(1172-300, 28, 1172, 872); side != want {
		t.Errorf("sidebar %v in a 1200px window, want %v", side, want)
	}
	if media != image.Rect(28, 28, side.Min.X, 872) {
		t.Errorf("media column %v beside the sidebar %v", media, side)
	}
	tabs, closeR := e.lb.SidebarParts()
	if len(tabs) != 1 || tabs[0].Min != image.Pt(side.Min.X+20, side.Min.Y) || tabs[0].Dy() != 50 {
		t.Errorf("tab %v, want 50 tall at the sidebar's left + 20", tabs)
	}
	if want := image.Rect(1172-8-36, 28+8, 1172-8, 28+8+36); closeR != want {
		t.Errorf("close button %v, want %v (top 8, right 8 in the row)", closeR, want)
	}
	if got := e.h.paneIDs(); got != "F0MVD00001" {
		t.Fatalf("panes asked for %q, want the item's file", got)
	}
	p := e.h.panes[0]
	if p.frames == 0 || p.size != image.Pt(300, 844-50) || p.channel != "C0MVD" {
		t.Errorf("pane drawn %d times in %v (channel %q), want the tab panel 300x794 for C0MVD", p.frames, p.size, p.channel)
	}
	if got := e.lb.DetailsFileID(); got != "F0MVD00001" {
		t.Errorf("Details shows %q", got)
	}
	for _, c := range []struct{ win, want int }{{1500, 400}, {1441, 400}, {1440, 360}, {1281, 360}, {1280, 300}, {760, 300}, {600, 272}} {
		e.size = image.Pt(c.win, 900)
		e.frames(2)
		side, media := e.lb.SidebarRect()
		if side.Dx() != c.want || media.Max.X != side.Min.X || side.Max.X != c.win-28 {
			t.Errorf("%dpx window: sidebar %v, media %v; want %d wide", c.win, side, media, c.want)
		}
		if compact := lbCompact(layout.Context{Metric: unit.Metric{PxPerDp: 1}}, media); compact != (media.Dx() < 480) {
			t.Errorf("%dpx window: compact %v for a %dpx media column", c.win, compact, media.Dx())
		}
	}
}

// The sidebar's close, "Close details", closes only the sidebar (aO) and
// gives the focus to the media column's close; the viewer stays.
func TestMediaViewerCloseDetails(t *testing.T) {
	e := newMVDEnv(t, "treatment", mvdGallery)
	e.openDetails()
	_, closeR := e.lb.SidebarParts()
	e.click(closeR.Min.Add(image.Pt(18, 18)))
	if e.lb.Dismissed() {
		t.Fatal("Close details closed the viewer")
	}
	if ids, _ := e.lb.SidebarTabs(); ids != nil {
		t.Fatalf("tabs %q after Close details", ids)
	}
	if e.lb.side.detailsOpened || e.lb.side.tab != "" {
		t.Errorf("Details still chosen (%v, tab %q) after aM", e.lb.side.detailsOpened, e.lb.side.tab)
	}
	if !e.r.Source().Focused(&e.lb.closeBtn) {
		t.Error("the media column's close doesn't have the focus after Close details")
	}
	if got := e.lb.CloseLabel(); got != "Close" {
		t.Errorf("close label %q with the sidebar closed", got)
	}
	_, media := e.lb.SidebarRect()
	if media != image.Rect(28, 28, 1172, 872) {
		t.Errorf("media column %v after the sidebar closed", media)
	}
	// that close is the viewer's again
	e.click(image.Pt(1172-8-18, 28+8+18))
	if !e.lb.Dismissed() {
		t.Error("the media column's Close didn't close the viewer")
	}
}

// With a thread (a message's file: channel and ts) the close reads
// "Close" with Details open and closes the viewer (renderCloseButton's a
// needs !(e && i)).
func TestMediaViewerDetailsWithThread(t *testing.T) {
	f := mvdImageFile("F0MVD00003", "msg.png")
	msg := core.Message{Channel: "C0MVD", TS: "1762700000.000100", Doc: richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.FileBlock, File: f}}}}
	e := newMVDEnv(t, "treatment", func(h Host) *Lightbox { return NewLightbox(h, *f.Thumb, msg) })
	e.openDetails()
	if ids, _ := e.lb.SidebarTabs(); strings.Join(ids, ",") != "details" {
		t.Fatalf("tabs %q", ids)
	}
	if got := e.lb.CloseLabel(); got != "Close" {
		t.Errorf("close label %q with a thread, want Close", got)
	}
	if p := e.h.panes; len(p) != 1 || p[0].channel != "C0MVD" {
		t.Errorf("panes %v, want one for the message's channel", e.h.paneIDs())
	}
	_, closeR := e.lb.SidebarParts()
	e.click(closeR.Min.Add(image.Pt(18, 18)))
	if !e.lb.Dismissed() {
		t.Error("Close with a thread didn't close the viewer")
	}
}

// With the gate off there is never a Details tab: "View file details"
// still shows, and navigates to FileDetails (OpenFileDetails) with the
// viewer left open; the close reads "Close".
func TestMediaViewerDetailsGateOff(t *testing.T) {
	e := newMVDEnv(t, "control", mvdGallery)
	e.openDetails()
	if ids, _ := e.lb.SidebarTabs(); ids != nil {
		t.Fatalf("tabs %q with the gate off", ids)
	}
	if e.lb.Dismissed() {
		t.Error("View file details closed the viewer (the image's fallback leaves it open)")
	}
	if got := strings.Join(e.h.opened, ","); got != "F0MVD00001|C0MVD" {
		t.Errorf("OpenFileDetails %q, want the item's file in the viewer's channel", got)
	}
	if got := e.lb.CloseLabel(); got != "Close" {
		t.Errorf("close label %q", got)
	}
	if got := e.h.paneIDs(); got != "" {
		t.Errorf("panes asked for %q with the gate off", got)
	}
	// an item that isn't a file has no details item at all (tr)
	e.press(key.NameRightArrow)
	e.press(key.NameRightArrow)
	if idx, _ := e.lb.Position(); idx != 2 {
		t.Fatalf("at item %d", idx)
	}
	if got := ctxLabels(e.lb.moreMenu()); got != "Open in browser|Copy image link" {
		t.Errorf("More actions for an image that isn't a file %q", got)
	}
}

// A video's More actions (aRSD) always has View details (stories_av): it
// opens the Details tab with the gate on; with it off it navigates to
// FileDetails and closes the viewer (previewFile).
func TestMediaViewerVideoViewDetails(t *testing.T) {
	vf := &richtext.File{ID: "F0MVDVID01", Name: "clip.mp4", Title: "clip.mp4", Mime: "video/mp4", IsVideo: true,
		URL: "https://files.slack.com/files-pri/T1-F0MVDVID01/clip.mp4", Permalink: "https://fake.slack.com/files/U1/F0MVDVID01/clip.mp4"}
	var last *mvdHost
	for _, group := range []string{"treatment", "control"} {
		e := newMVDEnv(t, group, mvdGallery)
		last = e.h
		lb := &Lightbox{h: e.h, zoom: 1, items: []lbItem{lbVideoItem(vf)}, channel: "C0MVD"}
		menuItem(t, lb.moreMenu(), "View details").Do()
		on := group == "treatment"
		if on {
			if lb.Dismissed() || !lb.side.open || lb.side.tab != lbTabDetails || !lb.side.detailsOpened {
				t.Errorf("gate on: View details left open=%v tab=%q dismissed=%v", lb.side.open, lb.side.tab, lb.Dismissed())
			}
			if len(e.h.opened) != 0 {
				t.Errorf("gate on: OpenFileDetails %q", e.h.opened)
			}
			continue
		}
		if !lb.Dismissed() {
			t.Error("gate off: View details left the viewer open (previewFile closes it)")
		}
		if got := strings.Join(e.h.opened, ","); got != "F0MVDVID01|C0MVD" {
			t.Errorf("gate off: OpenFileDetails %q", got)
		}
	}
	// the message player's menu keeps its View details (the file's page)
	m := NewVideoActionsMenu(last.tzHost, image.Rect(0, 0, 24, 24), core.Message{}, vf, false)
	if it := menuItem(t, m, "View details"); it.Do == nil || it.Label != nsFile.T("View details") {
		t.Errorf("the message player's View details %+v", it)
	}
}

// Escape closes the whole viewer with the sidebar open.
func TestMediaViewerEscapeWithDetails(t *testing.T) {
	e := newMVDEnv(t, "treatment", mvdGallery)
	e.openDetails()
	e.press(key.NameEscape)
	if !e.lb.Dismissed() {
		t.Error("Escape didn't close the viewer with Details open")
	}
}

// → moves to the next item with Details still selected, and the tab
// shows FileDetails for the new file; an item without a file id resets
// Details and closes the sidebar (no tab is left), which stays closed
// back on a file.
func TestMediaViewerDetailsFollowsItem(t *testing.T) {
	e := newMVDEnv(t, "treatment", mvdGallery)
	e.openDetails()
	e.press(key.NameRightArrow)
	if idx, _ := e.lb.Position(); idx != 1 {
		t.Fatalf("→ moved to item %d", idx)
	}
	if ids, active := e.lb.SidebarTabs(); strings.Join(ids, ",") != "details" || active != "details" {
		t.Fatalf("tabs %q (active %q) on the next file", ids, active)
	}
	if got := e.h.paneIDs(); got != "F0MVD00001,F0MVD00002" {
		t.Errorf("panes %q, want one for each file", got)
	}
	if got := e.lb.DetailsFileID(); got != "F0MVD00002" {
		t.Errorf("Details shows %q on the second file", got)
	}
	e.press(key.NameRightArrow)
	if idx, _ := e.lb.Position(); idx != 2 {
		t.Fatalf("→ moved to item %d", idx)
	}
	if ids, _ := e.lb.SidebarTabs(); ids != nil {
		t.Errorf("tabs %q on an image that isn't a file", ids)
	}
	if e.lb.side.open || e.lb.side.detailsOpened || e.lb.side.tab != "" {
		t.Errorf("sidebar open=%v detailsOpened=%v tab=%q, want reset and closed", e.lb.side.open, e.lb.side.detailsOpened, e.lb.side.tab)
	}
	e.press(key.NameLeftArrow)
	if ids, _ := e.lb.SidebarTabs(); ids != nil {
		t.Errorf("tabs %q back on a file: the sidebar closed for good", ids)
	}
}

// A press inside the sidebar (on the pane, on the tab bar) doesn't reach
// the background's close-on-press; a press on the band outside the panel
// still closes the viewer.
func TestMediaViewerSidebarPress(t *testing.T) {
	e := newMVDEnv(t, "treatment", mvdGallery)
	e.openDetails()
	side, _ := e.lb.SidebarRect()
	e.click(image.Pt(side.Min.X+150, side.Min.Y+400))
	e.click(image.Pt(side.Min.X+150, side.Min.Y+25))
	if e.lb.Dismissed() {
		t.Fatal("a press inside the sidebar closed the viewer")
	}
	if ids, _ := e.lb.SidebarTabs(); strings.Join(ids, ",") != "details" {
		t.Errorf("tabs %q after presses in the sidebar", ids)
	}
	e.click(image.Pt(5, 450))
	if !e.lb.Dismissed() {
		t.Error("a press outside the panel didn't close the viewer")
	}
}

// The viewer takes the key focus back every frame it's on top, but not
// from a widget it drew: the Details pane's, the tab, the sidebar close.
// The tab takes Slack's tablist keys (and the viewer's → still moves the
// item), and keyboard focus draws the ring and, on a lone tab, the
// underline.
func TestMediaViewerSidebarFocus(t *testing.T) {
	e := newMVDEnv(t, "treatment", mvdGallery)
	e.openDetails()
	if !e.r.Source().Focused(e.lb) {
		t.Fatal("the viewer doesn't have the focus")
	}
	p := e.h.panes[0]
	e.focus(&p.btn)
	if !e.r.Source().Focused(&p.btn) {
		t.Error("the viewer took the focus from the Details pane's button")
	}
	e.press(key.NameRightArrow) // the overlay's →, from a widget inside
	if idx, _ := e.lb.Position(); idx != 1 {
		t.Errorf("→ with the pane's button focused left item %d", idx)
	}
	e.focus(&e.lb.closeBtn)
	if !e.r.Source().Focused(&e.lb.closeBtn) {
		t.Error("the viewer took the focus from the sidebar's close")
	}
	e.focus(&e.lb.side.tabFocus)
	if !e.r.Source().Focused(&e.lb.side.tabFocus) {
		t.Fatal("the viewer took the focus from the tab")
	}
	if got := e.lb.side.underline; len(got) != 1 || !got[0] {
		t.Errorf("a keyboard-focused lone tab drew underline %v, want the ring and the underline", got)
	}
	e.press(key.NameLeftArrow)
	if idx, _ := e.lb.Position(); idx != 0 {
		t.Errorf("← on the tab left item %d (the overlay's binding sees it too)", idx)
	}
	if ids, active := e.lb.SidebarTabs(); strings.Join(ids, ",") != "details" || active != "details" {
		t.Errorf("tabs %q (active %q) after ← on the tab", ids, active)
	}
	if !e.r.Source().Focused(&e.lb.side.tabFocus) {
		t.Error("the tab lost the focus to ←")
	}
	// a widget the viewer didn't draw doesn't keep it
	var under int
	e.frameThen(func(gtx layout.Context) {
		keyTarget(gtx, &under, image.Rect(0, 0, 10, 10))
		gtx.Execute(key.FocusCmd{Tag: &under})
	})
	e.frames(2)
	if !e.r.Source().Focused(e.lb) {
		t.Error("the viewer didn't take the focus back from a widget it didn't draw")
	}
}

// The pending file viewer (NewPendingViewer) never has the sidebar: the
// file isn't sent yet.
func TestPendingViewerNoDetails(t *testing.T) {
	e := newMVDEnv(t, "treatment", func(h Host) *Lightbox {
		return NewPendingViewer(h, richtext.Image{URL: "https://example.com/pending.png", W: 900, H: 600}, "pending.png", nil)
	})
	e.lb.openDetails()
	e.frames(2)
	if ids, _ := e.lb.SidebarTabs(); ids != nil {
		t.Errorf("the pending viewer drew tabs %q", ids)
	}
	if e.lb.detailsAvailable() {
		t.Error("a pending file has details")
	}
}
