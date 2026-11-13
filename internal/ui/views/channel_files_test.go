package views

// The channel's Files & links tab (channel_files.go) against
// internal/fakeslack's #files-gallery (synthetic files and links only),
// driven through the input router: the rows Slack's
// useChannelAttachmentsExplorer builds, the Attachment type radios, Sort,
// Load More, the cards, opening files and links, the ⋮ menus and the
// keyboard; plus light and dark screenshots (VIEWS_SHOTS_DIR, the
// files-view workflow).

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// cfHost is filesHost with the fake's pictures (thumbs and favicons).
type cfHost struct {
	*filesHost
	imgs *imgcache.Cache
}

func (h *cfHost) Images() *imgcache.Cache { return h.imgs }

// cfFake boots a core on #files-gallery: the users the fixtures name and
// the channel, so the tab's in: token carries its name.
func cfFake(t *testing.T) (*core.App, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.Files = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = fakeslack.TeamID, "Synthetic Club"
	s.Unlock()
	s.PutUsers([]slack.User{{ID: fakeslack.SelfID, Name: "self", RealName: "Test Self"}})
	s.PutConv(slack.Conversation{ID: fakeslack.FilesChannelID, Name: "files-gallery", IsChannel: true, IsMember: true, NumMembers: 6})
	app := core.New(slack.New(fs.Creds()), s)
	if u, ok := s.User(fakeslack.SelfID); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	return app, fs
}

// cfImages serves the fake's /img pictures over plain HTTP (no cookie, a
// local server) and a grey square for anything else.
func cfImages(base string) *imgcache.Cache {
	cl := &http.Client{Timeout: 5 * time.Second}
	return imgcache.New(func(ctx context.Context, u string) ([]byte, error) {
		if !strings.HasPrefix(u, base) {
			img := image.NewRGBA(image.Rect(0, 0, 32, 32))
			for i := range img.Pix {
				img.Pix[i] = 0xaa
			}
			var b bytes.Buffer
			_ = png.Encode(&b, img)
			return b.Bytes(), nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := cl.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	}, "", nil)
}

func cfView(t *testing.T, dark bool) (*ChannelFiles, *cfHost, *vaEnv, *fakeslack.Server) {
	t.Helper()
	app, fs := cfFake(t)
	h := &cfHost{filesHost: &filesHost{vaHost: &vaHost{slickHost: &slickHost{app: app, th: theme.New(dark)}}}, imgs: cfImages(fs.URL)}
	v := NewChannelFiles(h, fakeslack.FilesChannelID)
	e := &vaEnv{size: image.Pt(1100, 900)}
	e.draw = func(gtx layout.Context) {
		v.Layout(gtx)
		for _, o := range h.open() {
			o.Layout(gtx)
		}
	}
	e.until(t, "the three browsers", func() bool {
		return len(v.media.items) > 0 && len(v.files.items) > 0 && len(v.links.links) > 0
	})
	return v, h, e, fs
}

// cfListY is the results list's top: 20 down, the 36px search, 16 under.
const cfListY = 20 + 36 + 16

// hoverFind moves the pointer along y between x0 and x1 until c is
// hovered, and returns that point.
func hoverFind(t *testing.T, e *vaEnv, c *widget.Clickable, y, x0, x1 int) image.Point {
	t.Helper()
	for x := x0; x < x1; x += 4 {
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(x), float32(y))})
		e.frame()
		if c.Hovered() {
			return image.Pt(x, y)
		}
	}
	t.Fatalf("no button along y=%d", y)
	return image.Point{}
}

func cfKinds(rows []cfRow) []int {
	out := make([]int, len(rows))
	for i, r := range rows {
		out[i] = r.kind
	}
	return out
}

// All: the controls, "Photos and videos" with one grid row, the spacer,
// the files and links newest first, and the Links card (links have a
// second page); the reads are Slack's three browsers.
func TestChannelFilesAll(t *testing.T) {
	v, _, e, fs := cfView(t, false)
	e.frame()
	rows := v.rowsFor()
	k := cfKinds(rows)
	if len(k) < 6 || k[0] != cfrControls || k[1] != cfrSection || k[2] != cfrStrip || k[3] != cfrSpacer {
		t.Fatalf("row kinds %v", k)
	}
	if rows[1].title != "Photos and videos" {
		t.Errorf("section %q", rows[1].title)
	}
	var files, links int
	last := 1e18
	for _, r := range rows[4:] {
		var ts float64
		switch r.kind {
		case cfrFile:
			files++
			ts = fileShareTS(r.file)
			if strings.HasPrefix(r.file.Mime, "image/") || strings.HasPrefix(r.file.Mime, "video/") {
				t.Errorf("media %s in the list without a search", r.file.ID)
			}
		case cfrLink:
			links++
			ts = r.link.Timestamp
		default:
			continue
		}
		if ts > last {
			t.Errorf("row %s out of order", r.key)
		}
		last = ts
	}
	if files == 0 || links != fakeslack.ChannelLinksPerPage {
		t.Errorf("%d files, %d links", files, links)
	}
	if lr := rows[len(rows)-1]; lr.kind != cfrMoreTypes || len(lr.cards) != 1 || lr.cards[0] != 2 {
		t.Errorf("last row %+v, want the Links card", lr)
	}
	forms := fs.SearchForms("search.modules.files")
	if len(forms) < 2 {
		t.Fatalf("%d search.modules.files calls", len(forms))
	}
	var media, rest bool
	for _, f := range forms {
		q := f.Get("query")
		if !strings.Contains(q, "in:<#"+fakeslack.FilesChannelID+"|files-gallery>") || f.Get("search_context") != "desktop_files_channel_tab" {
			t.Errorf("query %q context %q", q, f.Get("search_context"))
		}
		media = media || strings.HasSuffix(q, "type:images type:videos")
		rest = rest || strings.HasSuffix(q, "-type:images -type:videos -type:folder")
	}
	if !media || !rest {
		t.Errorf("forms %v", forms)
	}
	if lf := fs.ChannelLinksForms(); len(lf) == 0 || lf[0].Get("channel_id") != fakeslack.FilesChannelID {
		t.Errorf("searchLinks %v", lf)
	}
}

// The Links radio, then Load More: the second page of links; the Links
// card in All switches too; Oldest goes to every browser.
func TestChannelFilesRadiosAndPaging(t *testing.T) {
	v, _, e, fs := cfView(t, false)
	y := cfListY + 5 + 14
	p := hoverFind(t, e, &v.typeClicks[3], y, 0, e.size.X)
	e.click(p)
	if v.typ != cfLinks {
		t.Fatalf("type %q after clicking Links", v.typ)
	}
	rows := v.rowsFor()
	if rows[len(rows)-1].kind != cfrPagination {
		t.Fatalf("no Load More: %v", cfKinds(rows))
	}
	// Load More at the end of the list
	e.frame()
	top, ok, _ := v.kb.top(rows[len(rows)-2].key)
	if !ok {
		t.Fatal("last link not laid out")
	}
	ly := cfListY + top + v.kb.height(rows[len(rows)-2].key) + 8 + 18
	lp := hoverFind(t, e, &v.loadMore, ly, 0, e.size.X)
	e.click(lp)
	e.until(t, "the second page", func() bool { return len(v.links.links) == fakeslack.ChannelLinksCount })
	if lf := fs.ChannelLinksForms(); lf[len(lf)-1].Get("page") != "2" {
		t.Errorf("Load More asked for page %q", lf[len(lf)-1].Get("page"))
	}
	if rows := v.rowsFor(); rows[len(rows)-1].kind == cfrPagination {
		t.Error("Load More still shown on the last page")
	}
	// Oldest: every browser again, timestamp asc
	n := len(fs.SearchForms("search.modules.files"))
	v.sortOpen = true
	e.frame()
	v.setSort(core.ChannelFilesOldest)
	e.until(t, "the oldest-first reads", func() bool { return len(fs.SearchForms("search.modules.files")) >= n+2 })
	for _, f := range fs.SearchForms("search.modules.files")[n:] {
		if f.Get("sort") != "timestamp" || f.Get("sort_dir") != "asc" {
			t.Errorf("sort %s %s", f.Get("sort"), f.Get("sort_dir"))
		}
	}
	if lf := fs.ChannelLinksForms(); lf[len(lf)-1].Get("sort_dir") != "asc" {
		t.Errorf("links sort_dir %q", lf[len(lf)-1].Get("sort_dir"))
	}
	// the Links card in All, once the re-sorted links' page 1 is back
	v.setType(cfAll)
	e.until(t, "the re-sorted links", func() bool { return !v.links.data.busy() && v.links.page == 1 && v.links.pages == 2 })
	if r := v.rowsFor(); r[len(r)-1].kind != cfrMoreTypes {
		t.Fatalf("no cards: %v", cfKinds(r))
	}
}

// A search (Enter) goes to every browser; outside All the sort becomes
// Most relevant, and clearing the text resets it.
func TestChannelFilesSearch(t *testing.T) {
	v, _, e, fs := cfView(t, false)
	v.setType(cfFiles)
	n := len(fs.SearchForms("search.modules.files"))
	v.search.ed.SetText("report")
	v.runSearch("report") // the explorer's onSearch (Enter on the type-query completion)
	e.until(t, "the search", func() bool { return len(fs.SearchForms("search.modules.files")) >= n+2 })
	// the links search goes out on its own goroutine: wait for it too (the
	// release run read the unfiltered page-1 request before it landed)
	e.until(t, "the links search", func() bool {
		lf := fs.ChannelLinksForms()
		return len(lf) > 0 && lf[len(lf)-1].Get("query") == "report"
	})
	for _, f := range fs.SearchForms("search.modules.files")[n:] {
		if !strings.HasPrefix(f.Get("query"), "report in:<#") || f.Get("sort") != "score" {
			t.Errorf("search form query %q sort %q", f.Get("query"), f.Get("sort"))
		}
	}
	if lf := fs.ChannelLinksForms(); lf[len(lf)-1].Get("query") != "report" || lf[len(lf)-1].Get("sort") != "timestamp" {
		t.Errorf("links search %v", lf[len(lf)-1])
	}
	opts := v.sortOptions()
	if len(opts) != 3 || opts[0].value != core.ChannelFilesRelevant {
		t.Errorf("sort options %+v", opts)
	}
	e.until(t, "the results", func() bool { return !v.files.data.busy() && v.files.page == 1 })
	e.frame()
	for _, r := range v.rowsFor() {
		if r.kind == cfrFile && !strings.Contains(strings.ToLower(r.file.title()+r.file.Name), "report") {
			t.Errorf("search row %s %q", r.key, r.file.title())
		}
	}
	v.resetSearch()
	if v.sortValue() != core.ChannelFilesNewest || v.query != "" {
		t.Errorf("after reset: sort %s query %q", v.sortValue(), v.query)
	}
	// All keeps its sort with a search
	v.setType(cfAll)
	v.runSearch("zzz-nothing")
	e.until(t, "no results", func() bool {
		return !v.files.data.busy() && !v.media.data.busy() && !v.links.data.busy() && v.files.page == 1 && v.links.page == 1
	})
	rows := v.rowsFor()
	if r := rows[len(rows)-1]; r.kind != cfrEmpty || r.empty != `No results for "zzz-nothing" in this channel.` {
		t.Errorf("empty row %+v", r)
	}
}

// Opening: a canvas row in the side panel, a link in the browser, a
// media cell in the viewer; Enter on the keyboard's active row opens it.
func TestChannelFilesOpen(t *testing.T) {
	v, h, e, _ := cfView(t, false)
	e.size = image.Pt(1100, 1500) // every row of All in view
	e.frame()
	e.frame()
	rows := v.rowsFor()
	var canvas, link string
	for _, r := range rows {
		if r.kind == cfrFile && r.file.isCanvas() && canvas == "" {
			canvas = r.key
		}
		if r.kind == cfrLink && link == "" {
			link = r.key
		}
	}
	if canvas == "" || link == "" {
		t.Fatalf("rows %v", cfKinds(rows))
	}
	kbClickRow(t, e, v.kb, canvas, cfListY)
	if got := h.last(); got != "split-canvas:"+fakeslack.CanvasShared {
		t.Errorf("canvas click opened %q", got)
	}
	kbClickRow(t, e, v.kb, link, cfListY)
	if got := h.last(); got != "url:"+strings.TrimPrefix(link, "link:") {
		t.Errorf("link click opened %q", got)
	}
	// the first media cell
	if len(v.cells) == 0 {
		t.Fatal("no media cells")
	}
	c := v.cells[0]
	top, ok, _ := v.kb.top(c.key)
	if !ok {
		t.Fatal("cell not laid out")
	}
	cw := min(968, e.size.X-32)
	e.click(image.Pt((e.size.X-cw)/2+40, cfListY+top+40))
	if got := h.last(); !strings.HasPrefix(got, "lightbox:") {
		t.Errorf("media cell opened %q", got)
	}
	// keyboard: the list holds focus after the click; Home, then Enter on
	// the first row (the first media cell)
	kbPress(e, key.NameHome, 0)
	if v.kb.active == "" {
		t.Fatal("no active row after Home")
	}
	before := len(h.opened)
	kbPress(e, key.NameReturn, 0)
	if len(h.opened) != before+1 {
		t.Errorf("Enter on %s opened nothing", v.kb.active)
	}
	kbPress(e, key.NameDownArrow, 0)
	if v.kb.active == "" || v.kb.active == v.kb.items[0].key {
		t.Errorf("Down left the active row at %q", v.kb.active)
	}
}

// The ⋮ menus: a file's is the Files view's items, a link's Copy link.
func TestChannelFilesMenus(t *testing.T) {
	v, h, e, _ := cfView(t, false)
	e.frame()
	var it *fileItem
	for i := range v.files.items {
		if v.files.items[i].isCanvas() {
			it = &v.files.items[i]
		}
	}
	if it == nil {
		t.Fatal("no canvas row")
	}
	if got := menuLabels(v.fv.fileMenuItems(*it)); !strings.Contains(got, "Copy link to canvas") {
		t.Errorf("canvas menu %s", got)
	}
	v.openLinkMenu(v.links.links[0], image.Rect(900, 300, 928, 328))
	e.frame()
	if len(h.open()) == 0 {
		t.Fatal("no link menu")
	}
	dismissAll(h.filesHost)
	if primary, secondary := cfLinkLabels(&core.ChannelLink{URL: "https://status.example.net/"}); primary != "status.example.net" || secondary != "" {
		t.Errorf("bare host labels %q %q", primary, secondary)
	}
	if primary, secondary := cfLinkLabels(&core.ChannelLink{URL: "https://docs.example.org/guides/onboarding?step=2", Title: "Onboarding guide"}); primary != "Onboarding guide" || secondary != "docs.example.org" {
		t.Errorf("titled labels %q %q", primary, secondary)
	}
	if primary, secondary := cfLinkLabels(&core.ChannelLink{URL: "https://example.com/blog/launch-week"}); primary != "example.com" || secondary != "/blog/launch-week" {
		t.Errorf("path labels %q %q", primary, secondary)
	}
}

// Row heights stay put when a row is hovered (the action text and the
// buttons draw over the same box).
func TestChannelFilesStableRows(t *testing.T) {
	v, _, e, _ := cfView(t, false)
	e.frame()
	var key string
	for _, r := range v.rowsFor() {
		if r.kind == cfrFile {
			key = r.key
			break
		}
	}
	h0 := v.kb.height(key)
	top, _, _ := v.kb.top(key)
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(e.size.X/2), float32(cfListY+top+h0/2))})
	e.frame()
	e.frame()
	if h1 := v.kb.height(key); h1 != h0 || h0 == 0 {
		t.Errorf("row height %d, hovered %d", h0, h1)
	}
}

// TestChannelFilesShots saves the tab: All, its hover, Media, Links, the
// file and link menus, a search with no results and the loading rows,
// light and dark (VIEWS_SHOTS_DIR, the files-view workflow).
func TestChannelFilesShots(t *testing.T) {
	if os.Getenv("VIEWS_SHOTS_DIR") == "" {
		t.Skip("VIEWS_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		v, h, e, _ := cfView(t, dark)
		settle := func() {
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) && (v.media.data.busy() || v.files.data.busy() || v.links.data.busy()) {
				e.frame()
				time.Sleep(10 * time.Millisecond)
			}
			for range 20 { // the thumbs and favicons arrive
				e.frame()
				time.Sleep(15 * time.Millisecond)
			}
		}
		settle()
		vaShot(t, e, "chfiles-all", dark)
		// a file row's hover (Files: the action text and FileActions)
		v.setType(cfFiles)
		settle()
		for _, r := range v.rowsFor() {
			if r.kind == cfrFile && r.file.Mode == "hosted" {
				top, _, _ := v.kb.top(r.key)
				e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(e.size.X/2), float32(cfListY+top+20))})
				break
			}
		}
		e.frame()
		vaShot(t, e, "chfiles-hover", dark)
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(2, 2)})
		v.setType(cfMedia)
		settle()
		vaShot(t, e, "chfiles-media", dark)
		v.setType(cfLinks)
		settle()
		vaShot(t, e, "chfiles-links", dark)
		v.openLinkMenu(v.links.links[0], image.Rect(950, 120, 978, 148))
		e.frame()
		vaShot(t, e, "chfiles-link-menu", dark)
		dismissAll(h.filesHost)
		v.setType(cfFiles)
		settle()
		for i := range v.files.items {
			if v.files.items[i].isCanvas() {
				v.openFileMenu(v.files.items[i], image.Rect(950, 120, 978, 148))
				break
			}
		}
		e.frame()
		vaShot(t, e, "chfiles-file-menu", dark)
		dismissAll(h.filesHost)
		v.sortOpen = true
		e.frame()
		vaShot(t, e, "chfiles-sort", dark)
		v.sortOpen = false
		v.setType(cfAll)
		v.search.ed.SetText("zzz-nothing")
		v.runSearch("zzz-nothing")
		settle()
		vaShot(t, e, "chfiles-empty", dark)
	}
}
