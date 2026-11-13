package views

// The Pins tab (channel_pins.go) and a bookmark folder (folder_tab.go)
// against internal/fakeslack's #pins-gallery (synthetic pins and
// bookmarks only), driven through the input router: the rows, the
// keyboard, the menus, the Add link modal's bookmarks.add; plus light and
// dark screenshots of the tab, the folder, their peeks and empty states
// and the modals (VIEWS_SHOTS_DIR, the views-audit workflow).

import (
	"context"
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// pfHost records openPinnedItem's navigation and the bookmarks opened.
type pfHost struct {
	*cfHost
	pinned    []string
	bookmarks []string
	listed    []string // AddMessageToList (folder_tab_more_test.go)
}

func (h *pfHost) OpenPinnedMessage(ch, ts, thread string) { h.pinned = append(h.pinned, ch+"/"+ts) }
func (h *pfHost) OpenBookmark(ch string, b core.Bookmark) { h.bookmarks = append(h.bookmarks, b.Link) }

func pfCore(t *testing.T) (*core.App, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.PinsFolders = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = fakeslack.TeamID, "Synthetic Club"
	s.Unlock()
	s.PutUsers([]slack.User{{ID: fakeslack.SelfID, Name: "self", RealName: "Test Self"},
		{ID: "U0FAKE00001", Name: "ada", RealName: "Ada Park"}, {ID: "U0FAKE00002", Name: "bo", RealName: "Bo Lindqvist"},
		{ID: "U0FAKE00003", Name: "cy", RealName: "Cy Moreau"}, {ID: "U0FAKE00004", Name: "di", RealName: "Di Okafor"},
		{ID: "U0FAKE00005", Name: "ed", RealName: "Ed Tanaka"}})
	s.PutConv(slack.Conversation{ID: fakeslack.PinsChannelID, Name: "pins-gallery", IsChannel: true, IsMember: true, NumMembers: 4})
	app := core.New(slack.New(fs.Creds()), s)
	if u, ok := s.User(fakeslack.SelfID); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	return app, fs
}

func pfHostFor(t *testing.T, app *core.App, fs *fakeslack.Server, dark bool) *pfHost {
	return &pfHost{cfHost: &cfHost{filesHost: &filesHost{vaHost: &vaHost{slickHost: &slickHost{app: app, th: theme.New(dark)}}},
		imgs: cfImages(fs.URL)}}
}

func pfDraw(e *vaEnv, h *pfHost, w layout.Widget) {
	e.draw = func(gtx layout.Context) {
		w(gtx)
		for _, o := range h.open() {
			o.Layout(gtx)
		}
	}
}

// The tab's rows: newest pin first, the file last; ↓ and Enter run
// openPinnedItem; the right-click menu is Copy link and Remove pin; Remove
// pin sends pins.remove and the row goes.
func TestChannelPinsRows(t *testing.T) {
	app, fs := pfCore(t)
	h := pfHostFor(t, app, fs, false)
	v := NewChannelPins(h, fakeslack.PinsChannelID)
	e := &vaEnv{size: image.Pt(1100, 900)}
	pfDraw(e, h, v.Layout)
	e.until(t, "the pins", func() bool { return v.Loaded() && len(v.Items()) == 4 })
	// useCanEditTabs on Grid: the MANAGE_CHANNEL_TABS grant (permissions/info)
	e.until(t, "the tab grant", func() bool { return app.CanEditTabs(fakeslack.PinsChannelID) })
	e.frame()
	var keys []string
	for _, it := range v.Items() {
		keys = append(keys, it.Key())
	}
	if want := strings.Join(append(append([]string{}, fakeslack.PinnedTS...), fakeslack.PinsFileID), ","); strings.Join(keys, ",") != want {
		t.Errorf("rows %v, want %s", keys, want)
	}
	// a click on the first row
	var p image.Point
	for y := 24; y < 400; y += 4 {
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(300, float32(y))})
		e.frame()
		if v.RowButton(keys[0]).Hovered() {
			p = image.Pt(300, y)
			break
		}
	}
	if p == (image.Point{}) {
		t.Fatal("no first row")
	}
	e.frame()
	vaShot(t, e, "pins-tab-hover", false)
	e.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(float32(p.X), float32(p.Y))},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
	e.frame()
	e.frame()
	if len(h.pinned) != 1 || h.pinned[0] != fakeslack.PinsChannelID+"/"+keys[0] {
		t.Errorf("click opened %v", h.pinned)
	}
	// the keyboard: ↓ to the second, Enter
	e.r.Queue(key.Event{Name: key.NameDownArrow, State: key.Press})
	e.frame()
	e.r.Queue(key.Event{Name: key.NameReturn, State: key.Press})
	e.frame()
	e.frame()
	if len(h.pinned) < 2 {
		t.Errorf("Enter opened %v", h.pinned)
	}
	// the menu
	it := v.Items()[0]
	if got := strings.Join(v.MenuLabels(it), "|"); got != "Copy link|Remove pin" {
		t.Errorf("menu %q", got)
	}
	v.OpenMenu(it, image.Pt(400, 120))
	e.frame()
	if len(h.open()) == 0 {
		t.Fatal("no menu")
	}
	dismissAll(h.filesHost)
	if err := app.UnpinFromTab(context.Background(), it); err != nil {
		t.Fatal(err)
	}
	e.frame()
	if len(v.Items()) != 3 {
		t.Errorf("after Remove pin %d rows", len(v.Items()))
	}
	if f := fs.PinsFoldersForms("pins.remove"); len(f) != 1 || f[0].Get("reason") != "unpin-message" {
		t.Errorf("pins.remove %v", f)
	}
}

// A folder's rows, the Add link modal (bookmarks.add with the folder as
// parent_id), a row's click and its menus.
func TestFolderTabRows(t *testing.T) {
	app, fs := pfCore(t)
	h := pfHostFor(t, app, fs, false)
	v := NewFolderTab(h, fakeslack.PinsChannelID, FolderTabInfo{ID: fakeslack.PinsFolderTabID, Label: "Design docs", FolderID: fakeslack.PinsFolderID})
	e := &vaEnv{size: image.Pt(1100, 900)}
	pfDraw(e, h, v.Layout)
	e.until(t, "the rows", func() bool { return v.Loaded() && len(v.Rows()) == 4 })
	e.frame()
	if v.Name() != "Design docs" {
		t.Errorf("name %q", v.Name())
	}
	// Add link
	v.openAddLink()
	e.frame()
	var m *popups.BookmarkLinkModal
	for _, o := range h.open() {
		if bm, ok := o.(*popups.BookmarkLinkModal); ok {
			m = bm
		}
	}
	if m == nil {
		t.Fatal("no Add link modal")
	}
	m.SetLink("not a link")
	m.Save()
	if m.Error() == "" {
		t.Error("an invalid link saved")
	}
	m.SetLink("example.com/new")
	m.SetName("New link")
	m.Save()
	e.until(t, "bookmarks.add", func() bool { return len(fs.PinsFoldersForms("bookmarks.add")) == 1 })
	af := fs.PinsFoldersForms("bookmarks.add")[0]
	if af.Get("link") != "http://example.com/new" || af.Get("title") != "New link" || af.Get("parent_id") != fakeslack.PinsFolderID ||
		af.Get("type") != "link" {
		t.Errorf("bookmarks.add %v", af)
	}
	e.until(t, "the new row", func() bool { return len(v.Rows()) == 5 })
	dismissAll(h.filesHost)
	// a row's click
	for y := 60; y < 400; y += 4 {
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(300, float32(y))})
		e.frame()
		if v.RowButton("Bk0PINSL1").Hovered() {
			e.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(300, float32(y))},
				pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(300, float32(y))})
			e.frame()
			e.frame()
			break
		}
	}
	if len(h.bookmarks) != 1 || h.bookmarks[0] != "https://design.example.com/style" {
		t.Errorf("opened %v", h.bookmarks)
	}
}

// TestPinsFoldersShots saves the Pins tab, its peek and empty state, a
// folder (with a row's menu), its peek, an empty folder and its peek, the
// Add link and Delete link modals, light and dark (VIEWS_SHOTS_DIR).
func TestPinsFoldersShots(t *testing.T) {
	if os.Getenv("VIEWS_SHOTS_DIR") == "" {
		t.Skip("VIEWS_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		app, fs := pfCore(t)
		h := pfHostFor(t, app, fs, dark)
		settle := func(e *vaEnv) {
			for range 30 { // the avatars, favicons and emoji arrive
				e.frame()
				time.Sleep(15 * time.Millisecond)
			}
		}
		// the tab
		v := NewChannelPins(h, fakeslack.PinsChannelID)
		e := &vaEnv{size: image.Pt(1100, 760)}
		pfDraw(e, h, v.Layout)
		e.until(t, "the pins", func() bool { return v.Loaded() && len(v.Items()) == 4 })
		settle(e)
		vaShot(t, e, "pins-tab", dark)
		// the peek (360 wide, its own height)
		pv := NewChannelPinsPeek(h, fakeslack.PinsChannelID)
		pe := &vaEnv{size: image.Pt(360, 600)}
		pfDraw(pe, h, pv.Layout)
		pe.until(t, "the peek", func() bool { return pv.Loaded() })
		settle(pe)
		vaShot(t, pe, "pins-peek", dark)
		// a folder and its row menu
		fv := NewFolderTab(h, fakeslack.PinsChannelID, FolderTabInfo{ID: fakeslack.PinsFolderTabID, Label: "Design docs", FolderID: fakeslack.PinsFolderID})
		fe := &vaEnv{size: image.Pt(1100, 760)}
		pfDraw(fe, h, fv.Layout)
		fe.until(t, "the folder", func() bool { return fv.Loaded() && len(fv.Rows()) == 4 })
		settle(fe)
		vaShot(t, fe, "folder-tab", dark)
		fv.openRowMenu(fv.Rows()[0], image.Rect(1010, 98, 1038, 126))
		fe.frame()
		vaShot(t, fe, "folder-tab-menu", dark)
		dismissAll(h.filesHost)
		fv.openAddLink()
		fe.frame()
		vaShot(t, fe, "folder-add-link", dark)
		dismissAll(h.filesHost)
		fv.openDelete(fv.Rows()[0])
		fe.frame()
		vaShot(t, fe, "folder-delete-link", dark)
		dismissAll(h.filesHost)
		fv.addMenu(image.Rect(990, 20, 1018, 48), "bottom")
		fe.frame()
		vaShot(t, fe, "folder-add-menu", dark)
		dismissAll(h.filesHost)
		// the folder's peek
		fp := NewFolderTabPeek(h, fakeslack.PinsChannelID, FolderTabInfo{ID: fakeslack.PinsFolderTabID, Label: "Design docs", FolderID: fakeslack.PinsFolderID})
		fpe := &vaEnv{size: image.Pt(360, 4*63)}
		pfDraw(fpe, h, fp.Layout)
		fpe.until(t, "the folder peek", func() bool { return fp.Loaded() })
		settle(fpe)
		vaShot(t, fpe, "folder-peek", dark)
		// the empty folder and its peek
		ev := NewFolderTab(h, fakeslack.PinsChannelID, FolderTabInfo{ID: fakeslack.PinsEmptyTabID, Label: "Later", FolderID: fakeslack.PinsEmptyFolderID})
		ee := &vaEnv{size: image.Pt(1100, 760)}
		pfDraw(ee, h, ev.Layout)
		ee.until(t, "the empty folder", func() bool { return ev.Loaded() })
		settle(ee)
		vaShot(t, ee, "folder-tab-empty", dark)
		ep := NewFolderTabPeek(h, fakeslack.PinsChannelID, FolderTabInfo{ID: fakeslack.PinsEmptyTabID, Label: "Later", FolderID: fakeslack.PinsEmptyFolderID})
		epe := &vaEnv{size: image.Pt(360, 250)}
		pfDraw(epe, h, ep.Layout)
		epe.until(t, "the empty peek", func() bool { return ep.Loaded() })
		settle(epe)
		vaShot(t, epe, "folder-peek-empty", dark)
		// the Pins tab empty: every pin gone
		for _, it := range v.Items() {
			_ = app.UnpinFromTab(context.Background(), it)
		}
		e.until(t, "no pins", func() bool { e.frame(); return len(v.Items()) == 0 })
		settle(e)
		vaShot(t, e, "pins-tab-empty", dark)
		pv2 := NewChannelPinsPeek(h, fakeslack.PinsChannelID)
		pe2 := &vaEnv{size: image.Pt(360, 220)}
		pfDraw(pe2, h, pv2.Layout)
		pe2.until(t, "the empty peek", func() bool { pe2.frame(); return pv2.Loaded() && len(pv2.Items()) == 0 })
		settle(pe2)
		vaShot(t, pe2, "pins-peek-empty", dark)
	}
}
