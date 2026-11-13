package ui

// The channel Pins tab and the bookmark folder tabs on the whole client
// against internal/fakeslack's #pins-gallery, driven through the input
// router: the header tabs open their views in the channel body (Slack's
// useTabView: oG and FolderTabView), the rows, the menus, a pinned
// message's click back to Messages, a link row's click, a drag that
// reorders, the folder title edited in place, live pin events; and the
// tabs' slug peeks (PinsTabPeekV2, FolderTabPeek). PREVIEW_SHOTS_DIR
// (channel-actions-shots) saves light and dark shots of the tab views,
// HOVER_SHOTS_DIR (hover-card) the peeks.

import (
	"encoding/json"
	"image"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func pfEnv(t *testing.T, shotsEnv string) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.PinsFolders = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv(shotsEnv) != "" {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	// live updates: the pin and bookmark events come over the socket
	go e.a.Core().SetRealtime(true)
	e.until("the socket", 15*time.Second, func() bool { return e.a.data.m.Connected })
	return e, fs
}

// pfTab is ch's tab of type typ labelled label.
func pfTab(t *testing.T, e *perfEnv, ch, typ, label string) chTab {
	t.Helper()
	for _, tb := range e.a.channelTabs(ch) {
		if tb.typ == typ && tb.label == label {
			return tb
		}
	}
	t.Fatalf("no %s tab %q in %+v", typ, label, e.a.channelTabs(ch))
	return chTab{}
}

// pfClickTab clicks tab tb in ch's header.
func pfClickTab(t *testing.T, e *perfEnv, ch string, tb chTab) {
	t.Helper()
	o := e.a.primaryOrigin
	msgs := e.a.clicks.get("tab." + ch + ".messages")
	mp, ok := e.scanHover(msgs, image.Pt(o.X+40, o.Y), image.Pt(o.X+40, o.Y+200))
	if !ok {
		t.Fatal("no Messages tab in the header")
	}
	if tb.key == "messages" {
		e.click(mp)
		e.frame()
		return
	}
	p, ok := e.scanHover(e.a.clicks.get("tab."+ch+"."+tb.key), mp, image.Pt(e.size.X-60, mp.Y))
	if !ok {
		t.Fatalf("no %q tab in the header", tb.label)
	}
	e.click(p)
	e.frame()
}

// pfBodyScan scans the channel body's middle column top to bottom for c.
func pfBodyScan(t *testing.T, e *perfEnv, what string, hovered func() bool) image.Point {
	t.Helper()
	o := e.a.primaryOrigin
	top := o.Y + e.a.px(int(theme.HeaderH)) + e.a.px(int(theme.TabsH))
	x := o.X + (e.size.X-o.X)/3
	for y := top + 4; y < e.size.Y-20; y += 4 {
		e.move(f32.Pt(float32(x), float32(y)))
		if hovered() {
			return image.Pt(x, y)
		}
	}
	t.Fatalf("no %s in the body", what)
	return image.Point{}
}

func pfShot(e *perfEnv, name string) {
	for _, dark := range []bool{false, true} {
		e.setDark(dark)
		e.settle(400 * time.Millisecond)
		e.frame()
		e.shot(name + "-" + map[bool]string{false: "light", true: "dark"}[dark])
	}
	e.setDark(false)
	e.settle(200 * time.Millisecond)
}

// The Pins tab: its header tab (pin) opens the view; pins.list {channel,
// reason fetchPinsList}; the rows newest pin first with the file last;
// the right-click menu (Copy link, Remove pin); a pin_added from another
// client lands first; a row's click goes back to Messages at the message.
func TestPinsTabLikeSlack(t *testing.T) {
	e, fs := pfEnv(t, "PREVIEW_SHOTS_DIR")
	ch := fakeslack.PinsChannelID
	e.openChannel(ch)
	e.until("the tabs", 10*time.Second, func() bool { return e.a.data.tabsDone[ch] })
	pins := pfTab(t, e, ch, "bookmarks", "Pins")
	if pins.icon != "pin" || !bodyTab(pins) {
		t.Errorf("Pins tab %+v", pins)
	}
	pfClickTab(t, e, ch, pins)
	e.until("the Pins view", 5*time.Second, func() bool { return e.a.pinsTabView(ch) != nil })
	v := e.a.pinsTab(ch)
	e.until("the pins", 10*time.Second, func() bool { e.frame(); return v.Loaded() && len(v.Items()) == 4 })
	items := v.Items()
	var keys []string
	for _, it := range items {
		keys = append(keys, it.Key())
	}
	want := append(append([]string{}, fakeslack.PinnedTS...), fakeslack.PinsFileID)
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("rows %v, want %v", keys, want)
	}
	if f := fs.PinsFoldersForms("pins.list"); len(f) == 0 || f[0].Get("reason") != "fetchPinsList" {
		t.Errorf("pins.list forms %v", f)
	}
	pfShot(e, "pins-tab")

	// the right-click menu on the first row
	rp := pfBodyScan(t, e, "the first pinned message", v.RowButton(items[0].Key()).Hovered)
	e.rightClickAt(rp, time.Duration(time.Now().UnixNano()))
	m := e.topMenu()
	if m == nil {
		t.Fatalf("no menu on a right-click; overlays %v", e.overlayTypes())
	}
	if got := strings.Join(attMenuLabels(m), "|"); got != "Copy link|Remove pin" {
		t.Errorf("menu %q", got)
	}
	e.frame()
	e.shot("pins-tab-menu-light")
	e.closeMenu()
	if got := strings.Join(v.MenuLabels(items[3]), "|"); got != "Remove pin" {
		t.Errorf("the file's menu %q", got)
	}

	// pin_added from another client: first
	ts := fakeslack.PinsTS(1)
	ev, code := fs.Pin(fakeslack.SelfID, ch, ts, true)
	if code != "" {
		t.Fatal(code)
	}
	fs.Push(ev)
	e.until("pin_added's row", 5*time.Second, func() bool { e.frame(); return len(v.Items()) == 5 && v.Items()[0].Key() == ts })
	ev, _ = fs.Pin(fakeslack.SelfID, ch, ts, false)
	fs.Push(ev)
	e.until("pin_removed", 5*time.Second, func() bool { e.frame(); return len(v.Items()) == 4 })

	// a row's click: openPinnedItem, the channel's Messages
	items = v.Items()
	rp = pfBodyScan(t, e, "the first pinned message", v.RowButton(items[0].Key()).Hovered)
	e.click(rp)
	e.frame()
	e.until("Messages again", 5*time.Second, func() bool { return e.a.chTab[ch] == 0 && e.a.pinsTabView(ch) == nil })
	if e.a.router.Current().Channel() != ch {
		t.Errorf("route %v", e.a.router.Current())
	}

	// the empty state (all three messages unpinned elsewhere, the file
	// through the tab's menu)
	for _, ts := range fakeslack.PinnedTS {
		ev, _ := fs.Pin(fakeslack.SelfID, ch, ts, false)
		fs.Push(ev)
	}
	pfClickTab(t, e, ch, pins)
	e.until("one row left", 5*time.Second, func() bool { e.frame(); return len(v.Items()) == 1 })
	if c := e.a.Core(); c != nil {
		_ = c.UnpinFromTab(t.Context(), v.Items()[0])
	}
	e.until("the empty state", 5*time.Second, func() bool { e.frame(); return len(v.Items()) == 0 })
	pfShot(e, "pins-tab-empty")
}

// A folder tab: "Design docs" opens FolderTabView with its four rows by
// rank; the row menus; a link row's click opens the link; dragging the
// last row to the top reorders (bookmarks.reorder center/right); the
// title edited in place renames the tab (conversations.updateTab reason
// folder_tab_view_header); the loose "Bookmarks" folder and the empty
// "Later" one.
func TestFolderTabLikeSlack(t *testing.T) {
	e, fs := pfEnv(t, "PREVIEW_SHOTS_DIR")
	ch := fakeslack.PinsChannelID
	e.openChannel(ch)
	e.until("the tabs", 10*time.Second, func() bool { return e.a.data.tabsDone[ch] })
	design := pfTab(t, e, ch, "folder", "Design docs")
	if design.folder != fakeslack.PinsFolderID || !bodyTab(design) {
		t.Errorf("Design docs tab %+v", design)
	}
	pfClickTab(t, e, ch, design)
	e.until("the folder view", 5*time.Second, func() bool { return e.a.folderTabView(ch) != nil })
	v := e.a.folderTab(ch, design)
	e.until("the folder's rows", 10*time.Second, func() bool { e.frame(); return v.Loaded() && len(v.Rows()) == 4 })
	var ids []string
	for _, b := range v.Rows() {
		ids = append(ids, b.ID)
	}
	if strings.Join(ids, ",") != "Bk0PINSL1,Bk0PINSL2,Bk0PINSL3,Bk0PINSF1" {
		t.Errorf("rows %v", ids)
	}
	if lf := fs.PinsFoldersForms("bookmarks.list"); len(lf) == 0 || lf[0].Get("include_legacy_workflows") != "true" {
		t.Errorf("bookmarks.list %v", lf)
	}
	e.settle(500 * time.Millisecond) // the file's files.info, the favicon
	pfShot(e, "folder-tab")
	rows := v.Rows()
	if got := strings.Join(v.RowMenuLabels(rows[0]), "|"); got != "Copy link|Edit link|─|Delete link" {
		t.Errorf("link menu %q", got)
	}
	if got := strings.Join(v.RowMenuLabels(rows[3]), "|"); got != "Copy link|─|Remove file" {
		t.Errorf("file menu %q", got)
	}

	// the link row's click
	var mu sync.Mutex
	var opened []string
	prev := openBookmarkURL
	openBookmarkURL = func(u string) { mu.Lock(); opened = append(opened, u); mu.Unlock() }
	t.Cleanup(func() { openBookmarkURL = prev })
	p0 := pfBodyScan(t, e, "the first row", v.RowButton("Bk0PINSL1").Hovered)
	e.click(p0)
	e.frame()
	e.until("the link opened", 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(opened) == 1 && opened[0] == "https://design.example.com/style"
	})

	// drag the file row (last) onto the first: it moves to the top
	p3 := pfBodyScan(t, e, "the last row", v.RowButton("Bk0PINSF1").Hovered)
	e.rzPress(p3)
	for y := p3.Y; y >= p0.Y; y -= 8 {
		e.rzMove(image.Pt(p3.X, y))
	}
	e.rzMove(p0)
	e.frame()
	e.shot("folder-tab-drag-light")
	e.rzRelease(p0)
	e.until("bookmarks.reorder", 5*time.Second, func() bool { return len(fs.PinsFoldersForms("bookmarks.reorder")) == 1 })
	of := fs.PinsFoldersForms("bookmarks.reorder")[0]
	if of.Get("center") != "Bk0PINSF1" || of.Get("right") != "Bk0PINSL1" || of.Get("left") != "" ||
		of.Get("parent_id") != fakeslack.PinsFolderID || of.Get("bookmark_type") != "file" || of.Get("entity_id") != fakeslack.PinsDocFileID {
		t.Errorf("reorder %v", of)
	}
	e.until("the new order", 5*time.Second, func() bool {
		e.frame()
		r := v.Rows()
		return len(r) == 4 && r[0].ID == "Bk0PINSF1" && r[1].ID == "Bk0PINSL1"
	})
	mu.Lock()
	if len(opened) != 1 {
		t.Errorf("the drag also opened a row: %v", opened)
	}
	mu.Unlock()

	// the title, edited in place
	v.StartTitleEdit()
	e.frame()
	v.SetTitleText("Design system")
	v.SubmitTitle()
	e.until("conversations.updateTab", 5*time.Second, func() bool { return len(fs.PinsFoldersForms("conversations.updateTab")) == 1 })
	uf := fs.PinsFoldersForms("conversations.updateTab")[0]
	if uf.Get("id") != fakeslack.PinsFolderTabID || uf.Get("label") != "Design system" || uf.Get("reason") != "folder_tab_view_header" {
		t.Errorf("updateTab %v", uf)
	}
	e.until("the renamed tab", 10*time.Second, func() bool {
		e.frame()
		for _, tb := range e.a.channelTabs(ch) {
			if tb.key == design.key && tb.label == "Design system" {
				return true
			}
		}
		return false
	})

	// the loose Bookmarks folder and the empty one
	loose := pfTab(t, e, ch, "folder", "Bookmarks")
	pfClickTab(t, e, ch, loose)
	lv := e.a.folderTab(ch, loose)
	e.until("the loose rows", 10*time.Second, func() bool { e.frame(); return lv.Loaded() && len(lv.Rows()) == 2 })
	if lv.Name() != "Bookmarks" {
		t.Errorf("loose folder name %q", lv.Name())
	}
	later := pfTab(t, e, ch, "folder", "Later")
	pfClickTab(t, e, ch, later)
	ev := e.a.folderTab(ch, later)
	e.until("the empty folder", 10*time.Second, func() bool { e.frame(); return ev.Loaded() })
	if len(ev.Rows()) != 0 {
		t.Errorf("Later rows %v", ev.Rows())
	}
	pfShot(e, "folder-tab-empty")
}

// The tabs' slug peeks: a Pins slug shows PinsTabPeekV2 (the rows), a
// folder slug FolderTabPeek (63px rows), the empty folder its empty
// state (contentHeight 250). Light and dark shots (HOVER_SHOTS_DIR).
func TestPinsFolderSlugPeeks(t *testing.T) {
	sz := fakeslack.Small
	sz.PinsFolders, sz.ThreadHeader = true, true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	conv := fakeslack.ThreadPublicID
	ch := fakeslack.PinsChannelID
	tabs := []string{"bookmarks", fakeslack.PinsFolderTabID, fakeslack.PinsEmptyTabID}
	var els []string
	for i, tab := range tabs {
		if i > 0 {
			els = append(els, `{"type":"text","text":"   "}`)
		}
		els = append(els, `{"type":"channel","channel_id":"`+ch+`","tab_id":"`+tab+`"}`)
	}
	blocks := `[{"type":"rich_text","block_id":"pf","elements":[{"type":"rich_text_section","elements":[` + strings.Join(els, ",") + `]}]}]`
	fs.PostBlocksQuiet(conv, fakeslack.ProfileID, "Pins slugs", json.RawMessage(blocks))
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("HOVER_SHOTS_DIR") != "" {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	e.openChannel(conv)
	e.settle(800 * time.Millisecond)
	ctl := &e.a.hover.ctl
	away := f32.Pt(float32(e.a.primaryOrigin.X)-60, float32(e.size.Y)/2)
	for i, tab := range tabs {
		var r image.Rectangle
		e.until("the slug to "+tab, 10*time.Second, func() bool {
			r = e.chcRectOf(conv, func(l richview.LinkRect) bool {
				return l.Kind == richview.HitLink && l.ID == ch && strings.HasSuffix(l.URL, "/tabs/"+tab)
			})
			return !r.Empty()
		})
		e.move(f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2)))
		time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
		e.frame()
		if req, _, op := ctl.Open(); !op || req.Tab != tab {
			t.Fatalf("%s: open %v %+v", tab, op, req)
		}
		ready := func() bool {
			p := e.a.hover.tpk
			if p == nil || p.Rect().Empty() {
				return false
			}
			switch b := p.body.(type) {
			case *pinsTabPeek:
				return b.v.Loaded() && len(b.v.Items()) == 4
			case *folderTabPeek:
				return b.v.Loaded()
			}
			return false
		}
		e.until(tab+"'s peek", 10*time.Second, func() bool { e.frame(); return ready() })
		e.settle(300 * time.Millisecond)
		card := e.a.hover.tpk.Rect()
		if card.Dx() != 360 {
			t.Errorf("%s: width %d", tab, card.Dx())
		}
		switch i {
		case 1:
			if h := card.Dy(); h != 4*63 {
				t.Errorf("folder peek height %d, want 4 rows of 63", h)
			}
		case 2:
			if h := card.Dy(); h != 250 {
				t.Errorf("empty folder peek height %d, want 250", h)
			}
		}
		name := map[int]string{0: "pins", 1: "folder", 2: "folder-empty"}[i]
		for _, dark := range []bool{false, true} {
			e.setDark(dark)
			time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
			e.frame()
			e.until(tab+"'s peek again", 10*time.Second, func() bool { e.frame(); return ready() })
			e.settle(300 * time.Millisecond)
			hcShot(t, e, "tab-peek-"+name+"-"+map[bool]string{false: "light", true: "dark"}[dark])
		}
		e.setDark(false)
		e.move(away)
		time.Sleep(hovercard.HideDelay + 50*time.Millisecond)
		e.frame()
	}
}
