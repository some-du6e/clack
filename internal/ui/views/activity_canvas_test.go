package views

// Canvas mentions in Activity (Karim, α.102: the Dense row's location read
// "🔒 :pet-i10: :pet-tyson-final:…", raw shortcodes behind a private
// channel's lock; its click "put me in a limbo esque channel where the
// canvas contents are messages??"). Slack names the canvas's file channel
// as its canvas (InlineChannelEntity: the canvas icon, then the title from
// title_blocks, its emoji as images) in every layout, and a jump to the
// mention opens the canvas at the mention's section. internal/fakeslack's
// AddCanvasMention, made-up content only.

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// cmHost records where the Activity sends a click: a jump into a canvas
// (OpenFileJump), a conversation (Navigate, JumpPane) or the column beside
// the inbox (OpenThreadAt, OpenMessageSecondary). Its column shows what
// the App's would: a canvas's file channel's message as the thread view on
// it (Slack's RedirectFileChannel, ui/canvas_open.go). Its images are the
// fake's own (fs.Fetch), and it records which ones the view asks for.
type cmHost struct {
	*vaHost
	imgs      *imgcache.Cache
	mu        sync.Mutex
	canvas    []string // "ch thread ts side|full|pane"
	routes    []string
	jumps     []string // "ch/thread/ts"
	opened    []string // the column: "ch", "ch/ts", "ch/thread/ts"
	shows     string   // what the column shows: "ch/ts" a thread, "ch/" a conversation, a file id
	paneDrawn string   // the canvas FilePane drew last
	paneFile  string   // the canvas the last pane jump opened (resolved at once, as with the message held)
	paneHeld  bool     // the pane jump is still out (PaneFile not done)
	asked     []string // the image URLs fetched
	splits    []string // OpenSplit's routes
	items     []string // OpenListItem's "list/record"
	hold      bool     // the column's toMessage shows nothing yet (the App waits for files.info)
}

// OpenListItem is a list record's item pane in the column.
func (h *cmHost) OpenListItem(listID, recID string) {
	h.mu.Lock()
	h.items = append(h.items, listID+"/"+recID)
	h.shows = listID
	h.mu.Unlock()
}

// Images draws the fake's pictures (the harness's own host has no cache,
// so its shots leave every image blank).
func (h *cmHost) Images() *imgcache.Cache { return h.imgs }

// askedFor reports whether the view fetched an image whose URL holds sub.
func (h *cmHost) askedFor(sub string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, u := range h.asked {
		if strings.Contains(u, sub) {
			return true
		}
	}
	return false
}

func (h *cmHost) OpenFileJump(ch, thread, ts string, where FileWhere) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.canvas = append(h.canvas, ch+" "+thread+" "+ts+" "+map[FileWhere]string{FileSide: "side", FileFull: "full", FileInPane: "pane"}[where])
	switch where {
	case FileSide:
		if !h.hold {
			h.shows = core.FileOfChannel(ch)
		}
	case FileInPane:
		h.paneFile = core.FileOfChannel(ch)
	}
}

func (h *cmHost) PaneFile() (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.paneHeld {
		return "", false
	}
	return h.paneFile, h.paneFile != ""
}

func (h *cmHost) FileShown(fileID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.shows == fileID
}

func (h *cmHost) FilePane(fileID string) layout.Widget {
	return func(gtx C) D {
		h.mu.Lock()
		h.paneDrawn = fileID
		h.mu.Unlock()
		return D{Size: gtx.Constraints.Max}
	}
}

// closeColumn is the column closing (its close button, Esc).
func (h *cmHost) closeColumn() {
	h.mu.Lock()
	h.shows = ""
	h.mu.Unlock()
}

func (h *cmHost) Navigate(r string) {
	h.mu.Lock()
	h.routes = append(h.routes, r)
	h.mu.Unlock()
}

func (h *cmHost) JumpPane(ch, thread, ts string) {
	h.mu.Lock()
	h.jumps = append(h.jumps, ch+"/"+thread+"/"+ts)
	h.mu.Unlock()
}

func (h *cmHost) OpenChannelSecondary(ch string) {
	h.mu.Lock()
	h.opened = append(h.opened, ch)
	h.shows = ch + "/"
	h.mu.Unlock()
}

// OpenMessageSecondary is toMessage in the column: a canvas's file
// channel's message is the thread view on it (RedirectFileChannel).
func (h *cmHost) OpenMessageSecondary(ch, ts string) {
	_, canvas, _ := h.Core().CanvasOfJump(ch, ts)
	h.mu.Lock()
	h.opened = append(h.opened, ch+"/"+ts)
	switch {
	case h.hold:
	case canvas:
		h.shows = ch + "/" + ts
	default:
		h.shows = ch + "/"
	}
	h.mu.Unlock()
}

func (h *cmHost) OpenThreadAt(ch, thread, ts string) {
	h.mu.Lock()
	h.opened = append(h.opened, ch+"/"+thread+"/"+ts)
	h.shows = ch + "/" + thread
	h.mu.Unlock()
}

func (h *cmHost) OpenThread(ch, thread string) {
	h.mu.Lock()
	h.opened = append(h.opened, ch+"/"+thread+"/")
	h.shows = ch + "/" + thread
	h.mu.Unlock()
}

func (h *cmHost) SecondaryAt() (string, string) { return "", "" }

// the menu's Open in split view (popups.SplitViewer)
func (h *cmHost) OpenSplit(route string) {
	h.mu.Lock()
	h.splits = append(h.splits, route)
	h.mu.Unlock()
}

func (h *cmHost) SplitViewOK() bool { return true }
func (h *cmHost) InSplitView() bool { return false }

// the inbox's column (ActivitySecondary: the draw is a layout.Widget, a
// func(C) D literal would leave cmHost without the interface)
func (h *cmHost) SecondaryShows(ch, ts string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.shows == ch+"/"+ts
}
func (h *cmHost) OpenDetailSecondary(string, layout.Widget) {}
func (h *cmHost) SecondaryShowsDetail(string) bool          { return false }

var (
	_ ActivitySecondary  = (*cmHost)(nil)
	_ FileOpener         = (*cmHost)(nil)
	_ SavedSecondary     = (*cmHost)(nil)
	_ PaneJumper         = (*cmHost)(nil)
	_ popups.SplitViewer = (*cmHost)(nil)
)

// cmCalls is a copy of what h recorded.
func (h *cmHost) cmCalls() (canvas, jumps, opened, routes []string, pane string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.canvas), slices.Clone(h.jumps), slices.Clone(h.opened), slices.Clone(h.routes), h.paneDrawn
}

// cmOpts is one layout: Dense or Detailed, activity_entry_layout_revisions_2
// on or off, the inbox's detail panel beside it (inbox) or the persistent
// one, and whether a plugin patches ActivityItem. With late, files.info
// answers only after the row is drawn; with comments the feed also holds
// the Launch plan canvas's comment items (AddCanvasCommentActivity), with
// lists the kitchen list's record comment (AddListCommentActivity), its
// thread's root held.
type cmOpts struct {
	dark, dense, rev2, inbox, patched, late, comments, lists bool
}

// cmActivity is the Activity page over the fixture with the canvas
// mention, and the store as Karim's had it: the file channel a private
// conversation it knows, and the canvas's files.info (core asks for it
// when the mention's message arrives).
func cmActivity(t *testing.T, o cmOpts) (*Activity, *cmHost, *vaEnv, *fakeslack.Server) {
	t.Helper()
	app, fs := vaFake(t)
	fs.AddCanvasMention()
	s := app.S
	s.PutUsers([]slack.User{{ID: fakeslack.CanvasMentionUser, Name: "ada", RealName: "Ada Example"}})
	s.PutConv(slack.Conversation{ID: fakeslack.CanvasMentionChannel, Name: fakeslack.CanvasMentionTitle, IsChannel: true, IsGroup: true,
		IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 2})
	if o.late {
		// canvas refs live in memory only, so every start meets the
		// mention before files.info answers (held here until the row is
		// drawn); core looks it up as a booted app does. The feed's DMs
		// are known: their lookups' "names" would describe every waiting
		// row again on their own, and in a quiet session files.info's
		// comes alone
		s.PutConv(slack.Conversation{ID: "D0FAKE00000", IsIM: true, User: "U0FAKE00001", IsMember: true})
		s.PutConv(slack.Conversation{ID: fakeslack.ActivitySlackbotDM, IsIM: true, User: "USLACKBOT", IsMember: true})
		fs.SetFault("files.info", fakeslack.Fault{Delay: 1500 * time.Millisecond})
		app.ServeLookups()
		t.Cleanup(app.StopTraffic)
	} else {
		blocks, _ := json.Marshal(fakeslack.CanvasMentionTitleBlocks())
		s.PutCanvasRef(fakeslack.CanvasMentionFile, richtext.CanvasRef{Title: fakeslack.CanvasMentionTitle, TitleBlocks: blocks,
			Status: "normal", Kind: "canvas"})
	}
	s.CustomEmoji.Put(fakeslack.CanvasMentionEmoji, fs.CanvasMentionEmojiURL())
	if o.comments {
		fs.AddCanvasCommentActivity()
		s.PutConv(slack.Conversation{ID: fakeslack.FileChanLaunch, Name: "Launch plan", IsChannel: true, IsGroup: true,
			IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 4})
		s.PutCanvasRef(fakeslack.CanvasChannel, richtext.CanvasRef{Title: "Launch plan", Status: "normal", Kind: "canvas"})
	}
	if o.lists {
		fs.AddListCommentActivity()
		s.PutConv(slack.Conversation{ID: fakeslack.FileChanKitchen, Name: "Launch tracker", IsChannel: true, IsGroup: true,
			IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 3})
		s.PutCanvasRef(fakeslack.ListKitchen, richtext.CanvasRef{Title: "Launch tracker", Status: "normal", Kind: "list"})
		s.PutFetched(fakeslack.FileChanKitchen, []slack.Message{{Type: "message", Subtype: "list_record_comment", User: "U0FAKE00001",
			TS: fakeslack.ListKitchenThread, ThreadTS: fakeslack.ListKitchenThread,
			SlackList: &slack.MessageSlackList{ListID: fakeslack.ListKitchen, ListRecordID: fakeslack.ListKitchenRecord}}}, s.LiveSeq())
	}
	on := map[bool]string{true: "on", false: "off"}
	app.SetExperimentGroup("activity_inbox", "on")
	app.SetExperimentGroup("nested_secondary_view", on[o.inbox])
	app.SetExperimentGroup("activity_inbox_remove_persistent_detail_panel", on[o.inbox])
	app.SetExperimentGroup("activity_entry_layout_revisions_2", on[o.rev2])
	if o.dense {
		if err := app.SetActivityPrefs(context.Background(), "", "dense"); err != nil {
			t.Fatal(err)
		}
	}
	if o.patched {
		lmPatch(t, app, []string{"ActivityItem"}, false)
	}
	h := &cmHost{vaHost: &vaHost{slickHost: &slickHost{app: app, th: theme.New(o.dark)}}}
	h.imgs = imgcache.New(func(ctx context.Context, u string) ([]byte, error) {
		h.mu.Lock()
		h.asked = append(h.asked, u)
		h.mu.Unlock()
		if strings.Contains(u, "/"+fakeslack.CanvasMentionEmoji+"/") {
			return cmMagenta, nil // a colour no row draws otherwise: cmMagentaIn finds the emoji's pixels
		}
		return fs.Fetch(ctx, u)
	}, "", nil)
	v := NewActivity(h)
	e := &vaEnv{size: image.Pt(1200, 900)}
	e.draw = func(gtx C) { v.Layout(gtx) }
	e.until(t, "the activity feed with the canvas mention", func() bool {
		for _, it := range actPage(v).Items {
			if actKey(&it) == fakeslack.CanvasMentionKey {
				return true
			}
		}
		return false
	})
	keys := []string{fakeslack.CanvasMentionKey}
	if o.comments {
		keys = append(keys, fakeslack.CanvasCommentKey, fakeslack.CanvasCommentThreadKey)
	}
	if o.lists {
		keys = append(keys, fakeslack.ListCommentKey)
	}
	for _, k := range keys {
		e.until(t, "the row "+k, func() bool { r := v.rows[k]; return r != nil && r.built })
	}
	e.frame()
	return v, h, e, fs
}

// cmChoose opens the item keyed key, as its row's click does.
func cmChoose(t *testing.T, v *Activity, e *vaEnv, key string) {
	t.Helper()
	page := actPage(v)
	for i := range page.Items {
		if it := &page.Items[i]; actKey(it) == key {
			v.choose(it, key, v.rowFor(key))
			e.frame()
			e.frame()
			return
		}
	}
	t.Fatalf("no item %s in the feed", key)
}

// cmMagenta is the title emoji's image in these tests: solid magenta.
var cmMagenta = func() []byte {
	m := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for i := 0; i < len(m.Pix); i += 4 {
		m.Pix[i], m.Pix[i+1], m.Pix[i+2], m.Pix[i+3] = 0xff, 0, 0xff, 0xff
	}
	var b bytes.Buffer
	_ = png.Encode(&b, m)
	return b.Bytes()
}()

// cmMagentaIn is img's largest patch of magenta (4-connected) and its
// size in pixels. The fake's other pictures are gradients, a few of whose
// pixels come out magenta too, never a patch.
func cmMagentaIn(img *image.RGBA) (image.Rectangle, int) {
	b := img.Rect
	is := func(p image.Point) bool { c := img.RGBAAt(p.X, p.Y); return c.R >= 235 && c.G <= 40 && c.B >= 235 }
	seen := make([]bool, b.Dx()*b.Dy())
	at := func(p image.Point) int { return (p.Y-b.Min.Y)*b.Dx() + p.X - b.Min.X }
	var best image.Rectangle
	bestN := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := image.Pt(x, y)
			if seen[at(p)] || !is(p) {
				continue
			}
			var r image.Rectangle
			n := 0
			seen[at(p)] = true
			for stack := []image.Point{p}; len(stack) > 0; {
				q := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				r, n = r.Union(image.Rect(q.X, q.Y, q.X+1, q.Y+1)), n+1
				for _, d := range [...]image.Point{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					if o := q.Add(d); o.In(b) && !seen[at(o)] && is(o) {
						seen[at(o)] = true
						stack = append(stack, o)
					}
				}
			}
			if n > bestN {
				best, bestN = r, n
			}
		}
	}
	return best, bestN
}

// cmShot is vaShot returning the frame it saved (nil without
// VIEWS_SHOTS_DIR, whose workflow has the GPU context pixels need).
func cmShot(t *testing.T, e *vaEnv, name string, dark bool) *image.RGBA {
	t.Helper()
	dir := os.Getenv("VIEWS_SHOTS_DIR")
	if dir == "" {
		return nil
	}
	win, err := pixeltest.NewWindow(e.size.X, e.size.Y)
	if err != nil {
		t.Fatalf("headless window: %v", err)
	}
	defer win.Release()
	for range 3 { // images and rows settle
		e.frame()
	}
	if err := win.Frame(&e.ops); err != nil {
		t.Fatalf("frame: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := win.Screenshot(img); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+"-"+map[bool]string{false: "light", true: "dark"}[dark]+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return img
}

// cmRowDraws draws the row keyed key alone and returns the icons it drew
// and the emoji images it drew (their pixel size by URL).
func cmRowDraws(t *testing.T, v *Activity, key string) (icons []string, emoji map[string]int) {
	t.Helper()
	emoji = map[string]int{}
	iconDrawn = func(n string) { icons = append(icons, n) }
	emojiDrawn = func(u string, px int) { emoji[u] = px }
	defer func() { iconDrawn, emojiDrawn = nil, nil }()
	page := actPage(v)
	for i := range page.Items {
		if actKey(&page.Items[i]) != key {
			continue
		}
		var ops op.Ops
		gtx := actFrame(&ops, image.Pt(1200, 400))
		gtx.Constraints = layout.Constraints{Min: image.Pt(1200, 0), Max: image.Pt(1200, 400)}
		v.row(gtx, &page.Items[i], i)
		return icons, emoji
	}
	t.Fatalf("no item %s in the feed", key)
	return nil, nil
}

// cmCanvasIconDrawn checks that the row keyed key drew the canvas icon, not
// a private channel's lock.
func cmCanvasIconDrawn(t *testing.T, v *Activity, key string) {
	t.Helper()
	drew, _ := cmRowDraws(t, v, key)
	if !slices.Contains(drew, icons.Slack("canvas")) || slices.Contains(drew, icons.Slack("lock-filled")) {
		t.Errorf("%s drew the icons %q, want the canvas's (%s) and no lock", key, drew, icons.Slack("canvas"))
	}
}

// drawn is every label the next frame draws.
func cmDrawn(e *vaEnv) map[string]bool {
	var mu sync.Mutex
	set := map[string]bool{}
	theme.DrawHook = func(s string) { mu.Lock(); set[s] = true; mu.Unlock() }
	e.frame()
	theme.DrawHook = nil
	mu.Lock()
	defer mu.Unlock()
	return set
}

// The row names the canvas's file channel as its canvas in every layout,
// natively and under a plugin that patches ActivityItem: the canvas icon
// (not a private channel's lock) and the title's text, its emoji drawn as
// an image, never the channel's ":shortcode:" name.
func TestActivityCanvasMentionRow(t *testing.T) {
	for _, o := range []cmOpts{
		{dense: true, rev2: true, inbox: true}, {dense: false, rev2: true, inbox: true},
		{dense: true, rev2: false, inbox: true}, {dense: false, rev2: false, inbox: true},
		{dense: true, rev2: true, inbox: true, patched: true}, {dense: false, rev2: true, inbox: true, patched: true},
		{dense: true, rev2: true, inbox: true, dark: true}, {dense: false, rev2: true, inbox: true, dark: true},
	} {
		base := map[bool]string{true: "dense", false: "detailed"}[o.dense] + map[bool]string{true: "-rev2", false: ""}[o.rev2] +
			map[bool]string{true: "-patched", false: ""}[o.patched]
		name := base + map[bool]string{true: "-dark", false: "-light"}[o.dark]
		t.Run(name, func(t *testing.T) {
			v, h, e, fs := cmActivity(t, o)
			d := v.rows[fakeslack.CanvasMentionKey].d
			if !d.entOn || d.ent.Icon != "canvas" {
				t.Errorf("the destination's icon %q, want canvas (InlineChannelIcon of a canvas's file channel)", d.ent.Icon)
			}
			drawn := cmDrawn(e)
			var got []string
			for s := range drawn {
				if strings.Contains(s, ":"+fakeslack.CanvasMentionEmoji+":") || strings.Contains(s, "Pet roster") || strings.Contains(s, "tribute") {
					got = append(got, s)
				}
			}
			for _, s := range got {
				if strings.Contains(s, ":"+fakeslack.CanvasMentionEmoji+":") {
					t.Errorf("drew the title's shortcode as text: %q (the title's labels %q)", s, got)
				}
			}
			if !drawn["Pet roster "] && !drawn["Pet roster"] {
				t.Errorf("the canvas title isn't drawn (title labels %q)", got)
			}
			// the emoji is the emoji's image (the store's URL for it)
			e.until(t, "the title's emoji image fetched", func() bool { return h.askedFor("/" + fakeslack.CanvasMentionEmoji + "/") })
			e.until(t, "the pictures loaded", func() bool { return h.imgs.Pending() == 0 })
			// what the row drew: the canvas icon, no lock, the emoji's image at
			// caption size (16px)
			cmCanvasIconDrawn(t, v, fakeslack.CanvasMentionKey)
			_, emoji := cmRowDraws(t, v, fakeslack.CanvasMentionKey)
			if px := emoji[fs.CanvasMentionEmojiURL()]; px != 16 {
				t.Errorf("the title's emoji drew at %dpx (drew %v), want 16", px, emoji)
			}
			// and on screen, in the shots run: about 16x16 of the emoji's
			// magenta, in Dense's location column at the row's right
			if img := cmShot(t, e, "activity-canvas-mention-"+base, o.dark); img != nil {
				r, n := cmMagentaIn(img)
				if n < 150 || r.Dx() < 12 || r.Dx() > 20 || r.Dy() < 12 || r.Dy() > 20 {
					t.Errorf("the emoji's pixels: %d in %v, want about 16x16", n, r)
				}
				if o.dense && r.Min.X < e.size.X*6/10 {
					t.Errorf("the emoji drew at %v, want the location column at the row's right", r)
				}
			}
		})
	}
}

// A canvas mention described before files.info answered is described
// again once core's "names" brings the file id: the loading bar gives way
// to the canvas and its title.
func TestActivityCanvasMentionRowLateFilesInfo(t *testing.T) {
	v, _, e, fs := cmActivity(t, cmOpts{dense: true, rev2: true, inbox: true, late: true})
	// the "names" core sends: a conversation's describes a loading row
	// again on its own (namesIn), so the one that brings the file id, and
	// any after it, must carry no conversation
	evs, unsub := v.h.Core().Subscribe()
	t.Cleanup(unsub)
	var mu sync.Mutex
	var named [][]string
	go func() {
		for ev := range evs {
			if ev.Type == "names" {
				mu.Lock()
				named = append(named, ev.Users)
				mu.Unlock()
			}
		}
	}()
	fileNames := func() (int, [][]string) {
		mu.Lock()
		defer mu.Unlock()
		for k, ids := range named {
			if slices.Contains(ids, fakeslack.CanvasMentionFile) {
				return k, slices.Clone(named)
			}
		}
		return -1, nil
	}
	if d := v.rows[fakeslack.CanvasMentionKey].d; !d.ent.Loading() {
		t.Fatalf("precondition: the row knew the canvas before files.info answered (%+v)", d.ent)
	}
	e.until(t, "files.info for the canvas", func() bool { return fs.CallCount("files.info") > 0 })
	e.until(t, "the names that bring the file id", func() bool { k, _ := fileNames(); return k >= 0 })
	described := func() bool {
		r := v.rows[fakeslack.CanvasMentionKey]
		return r != nil && r.built && r.d.entOn && r.d.ent.Icon == "canvas"
	}
	for deadline := time.Now().Add(2 * time.Second); !described(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the row is still %+v 2 s after files.info named the canvas", v.rows[fakeslack.CanvasMentionKey].d.ent)
		}
		e.frame()
	}
	k, all := fileNames()
	for _, ids := range all[k:] {
		if actConvIDs(ids) {
			t.Fatalf("precondition: a conversation's names came with or after the file id's (%q): they describe the row again on their own", all[k:])
		}
	}
	if drawn := cmDrawn(e); !drawn["Pet roster "] {
		t.Errorf("the canvas title isn't drawn once files.info answered (%d labels drawn)", len(drawn))
	}
	cmCanvasIconDrawn(t, v, fakeslack.CanvasMentionKey)
}

// With the persistent detail panel (the inbox without
// activity_inbox_remove_persistent_detail_panel), a click on the mention
// is Slack's jumpToMessage, which opens the canvas in Primary beside the
// Sidebar's inbox: the jump goes to the view's own pane, and the pane
// draws the canvas. Never the file channel's messages, never another page.
func TestActivityCanvasMentionClickPersistent(t *testing.T) {
	v, h, e, _ := cmActivity(t, cmOpts{dense: true, rev2: true})
	v.StepItem(1, false) // the newest row: the canvas mention
	e.frame()
	e.frame()
	canvas, jumps, _, routes, pane := h.cmCalls()
	want := fakeslack.CanvasMentionChannel + "  " + fakeslack.CanvasMentionTS + " pane"
	if len(canvas) != 1 || canvas[0] != want {
		t.Errorf("canvas jumps %q, want %q", canvas, want)
	}
	if pane != fakeslack.CanvasMentionFile {
		t.Errorf("the pane drew canvas %q, want %s", pane, fakeslack.CanvasMentionFile)
	}
	if len(jumps) > 0 || len(routes) > 0 {
		t.Errorf("the panel jumped to %q, navigated to %q", jumps, routes)
	}
}

// The inbox's click is GMkG's toMessage in the column, as for any
// message: Slack's RedirectFileChannel (the App's, ui/canvas_open.go)
// makes it the thread view on the mention, so the view opens no canvas
// itself. The row stays the opened one while the column shows that
// thread, and lets go once the column closes.
func TestActivityCanvasMentionClickInbox(t *testing.T) {
	v, h, e, _ := cmActivity(t, cmOpts{dense: true, rev2: true, inbox: true})
	v.StepItem(1, false)
	e.frame()
	e.frame()
	canvas, _, opened, _, _ := h.cmCalls()
	want := fakeslack.CanvasMentionChannel + "/" + fakeslack.CanvasMentionTS
	if len(canvas) > 0 || !slices.Equal(opened, []string{want}) {
		t.Errorf("the click: canvas jumps %q, the column %q; want toMessage %s", canvas, opened, want)
	}
	if !v.selOK || !v.selSec || v.selKey != fakeslack.CanvasMentionKey {
		t.Errorf("the mention isn't the opened row beside the column (selOK %v, selSec %v, key %q)", v.selOK, v.selSec, v.selKey)
	}
	h.closeColumn()
	e.frame()
	if v.selOK {
		t.Errorf("the row is still opened after the column closed")
	}
}

// Slack's item menu calls a file channel's items canvas-related (ITY6
// renderMenuPlaceholderItem's isCanvasRelated, gxWy isFileChannelById)
// and leaves out Open in home and Remind me about this. Open in split view
// stays: toChannel, or toThread for a comment's thread, in the split view.
// An ordinary channel's item keeps both.
func TestActivityCanvasItemMenu(t *testing.T) {
	v, h, _, _ := cmActivity(t, cmOpts{dense: true, rev2: true, inbox: true, comments: true, lists: true})
	keysOf := func(key string, split bool) []string {
		var keys []string
		for _, mi := range v.itemMenu(h, *actItem(t, v, key), key, false) {
			keys = append(keys, mi.Key)
			if split && mi.Key == popups.SplitKey {
				mi.Do()
			}
		}
		return keys
	}
	for _, c := range []struct{ key, split string }{
		{fakeslack.CanvasMentionKey, "c/" + fakeslack.CanvasMentionChannel},
		{fakeslack.CanvasCommentKey, "c/" + fakeslack.FileChanLaunch + "/t/" + fakeslack.CommentOpen},
		{fakeslack.CanvasCommentThreadKey, "c/" + fakeslack.FileChanLaunch + "/t/" + fakeslack.CommentUnread},
		{fakeslack.ListCommentKey, "c/" + fakeslack.FileChanKitchen + "/t/" + fakeslack.ListKitchenThread}, // a list's file channel
	} {
		keys := keysOf(c.key, true)
		if slices.Contains(keys, "open_in_home") || slices.Contains(keys, "remind_submenu") {
			t.Errorf("%s's menu %q has Open in home or Remind me about this, which Slack leaves out of a file channel's items", c.key, keys)
		}
		h.mu.Lock()
		splits := slices.Clone(h.splits)
		h.mu.Unlock()
		if !slices.Contains(keys, popups.SplitKey) || len(splits) == 0 || splits[len(splits)-1] != c.split {
			t.Errorf("%s: Open in split view opened %q, want %q (menu %q)", c.key, splits, c.split, keys)
		}
	}
	if keys := keysOf("fa-at-user", false); !slices.Contains(keys, "open_in_home") || !slices.Contains(keys, "remind_submenu") {
		t.Errorf("an ordinary channel's mention lost Open in home or Remind me about this: %q", keys)
	}
}

// A canvas comment's items sit in the canvas's file channel too, without a
// document_mention: their rows name the canvas, and their clicks keep the
// comment's thread. In the inbox a reply that mentions the viewer opens
// its thread in the column (GMkG: toThread), as a comment's thread_v2
// does (toQuipCommentThread). With the persistent panel the reply jumps to
// its message, the canvas with the thread (jumpToMessage, isSpaceship),
// while the thread_v2 item opens its comment thread in the pane.
func TestActivityCanvasCommentItems(t *testing.T) {
	t.Run("rows", func(t *testing.T) {
		v, _, e, _ := cmActivity(t, cmOpts{dense: true, rev2: true, inbox: true, comments: true})
		for _, k := range []string{fakeslack.CanvasCommentKey, fakeslack.CanvasCommentThreadKey} {
			if d := v.rows[k].d; !d.entOn || d.ent.Icon != "canvas" || d.ent.Text != "Launch plan" {
				t.Errorf("%s: the destination %+v, want the canvas Launch plan", k, d.ent)
			}
		}
		v.scrollToKey(fakeslack.CanvasCommentKey) // the comments are older than the fixture's other items
		e.frame()
		if drawn := cmDrawn(e); !drawn["Launch plan"] {
			t.Errorf("the canvas's title isn't drawn")
		}
		cmCanvasIconDrawn(t, v, fakeslack.CanvasCommentKey)
		cmCanvasIconDrawn(t, v, fakeslack.CanvasCommentThreadKey)
	})
	t.Run("inbox", func(t *testing.T) {
		v, h, e, _ := cmActivity(t, cmOpts{dense: true, rev2: true, inbox: true, comments: true})
		cmChoose(t, v, e, fakeslack.CanvasCommentKey)
		canvas, _, opened, _, _ := h.cmCalls()
		want := fakeslack.FileChanLaunch + "/" + fakeslack.CommentOpen + "/" + fakeslack.CanvasCommentTS
		if len(canvas) > 0 || !slices.Contains(opened, want) {
			t.Errorf("the reply's click: canvas jumps %q, the column %q; want the thread %s", canvas, opened, want)
		}
		cmChoose(t, v, e, fakeslack.CanvasCommentThreadKey)
		canvas, _, opened, _, _ = h.cmCalls()
		thread := fakeslack.FileChanLaunch + "/" + fakeslack.CommentUnread + "/"
		if len(canvas) > 0 || !slices.ContainsFunc(opened, func(o string) bool { return strings.HasPrefix(o, thread) }) {
			t.Errorf("the thread_v2 click: canvas jumps %q, the column %q; want the comment thread %s…", canvas, opened, thread)
		}
	})
	t.Run("persistent", func(t *testing.T) {
		v, h, e, _ := cmActivity(t, cmOpts{dense: true, rev2: true, comments: true})
		cmChoose(t, v, e, fakeslack.CanvasCommentKey)
		canvas, jumps, _, _, pane := h.cmCalls()
		want := fakeslack.FileChanLaunch + " " + fakeslack.CommentOpen + " " + fakeslack.CanvasCommentTS + " pane"
		if len(canvas) != 1 || canvas[0] != want || len(jumps) > 0 {
			t.Errorf("the reply's click: canvas jumps %q (want %q), pane jumps %q", canvas, want, jumps)
		}
		if pane != fakeslack.CanvasChannel {
			t.Errorf("the pane drew canvas %q, want %s", pane, fakeslack.CanvasChannel)
		}
		cmChoose(t, v, e, fakeslack.CanvasCommentThreadKey)
		canvas, jumps, _, _, _ = h.cmCalls()
		thread := fakeslack.FileChanLaunch + "/" + fakeslack.CommentUnread + "/"
		if len(canvas) != 1 || !slices.ContainsFunc(jumps, func(j string) bool { return strings.HasPrefix(j, thread) }) {
			t.Errorf("the thread_v2 click: canvas jumps %q, pane jumps %q; want the comment thread %s… in the pane", canvas, jumps, thread)
		}
	})
}

// A file channel's item whose view opens after the click (the App waited
// for files.info, or for the message: ui/canvas_open.go) is the opened
// row once the column shows it, the thread view on the mention or the
// conversation a wait gave up on, and lets go when the column closes.
func TestActivityFileItemFollowsLateView(t *testing.T) {
	for _, late := range []string{"thread", "conversation"} {
		t.Run(late, func(t *testing.T) {
			v, h, e, _ := cmActivity(t, cmOpts{dense: true, rev2: true, inbox: true})
			h.mu.Lock()
			h.hold = true
			h.mu.Unlock()
			v.StepItem(1, false)
			e.frame()
			if !v.selOK || v.selSec {
				t.Fatalf("precondition: the row opens beside nothing yet (selOK %v, selSec %v)", v.selOK, v.selSec)
			}
			h.mu.Lock()
			h.shows = fakeslack.CanvasMentionChannel + "/"
			if late == "thread" {
				h.shows += fakeslack.CanvasMentionTS
			}
			h.mu.Unlock()
			e.frame()
			if !v.selOK || !v.selSec {
				t.Errorf("the row doesn't follow the late %s (selOK %v, selSec %v)", late, v.selOK, v.selSec)
			}
			h.closeColumn()
			e.frame()
			if v.selOK {
				t.Errorf("the row is still opened after the column closed")
			}
		})
	}
}

// A reply in a list record's comment thread, its root held, opens the
// record with its thread (GMkG reads the root's slack_list before any
// thread rule: el.C, defaultContentType "thread"), not the thread on the
// list's file channel. In the inbox that's the record's item pane in the
// column, and the row follows it; with the persistent panel it's the jump
// to the list at the record, in the pane.
func TestActivityListCommentOpensRecord(t *testing.T) {
	t.Run("inbox", func(t *testing.T) {
		v, h, e, _ := cmActivity(t, cmOpts{dense: true, rev2: true, inbox: true, lists: true})
		cmChoose(t, v, e, fakeslack.ListCommentKey)
		h.mu.Lock()
		items, opened := slices.Clone(h.items), slices.Clone(h.opened)
		h.mu.Unlock()
		want := fakeslack.ListKitchen + "/" + fakeslack.ListKitchenRecord
		if !slices.Equal(items, []string{want}) || len(opened) > 0 {
			t.Errorf("the click opened items %q, the column %q; want the record %s alone", items, opened, want)
		}
		if !v.selOK || !v.selSec {
			t.Errorf("the row doesn't follow the record's pane (selOK %v, selSec %v)", v.selOK, v.selSec)
		}
		// the row's destination is the list's file channel as
		// InlineChannelEntity draws it: getChannelIconType's lists icon,
		// never a private channel's lock
		if d := v.rows[fakeslack.ListCommentKey].d; !d.entOn || d.ent.Icon != "lists" {
			t.Errorf("the list comment's destination %+v, want the lists icon", d.ent)
		}
	})
	t.Run("persistent", func(t *testing.T) {
		v, h, e, _ := cmActivity(t, cmOpts{dense: true, rev2: true, lists: true})
		cmChoose(t, v, e, fakeslack.ListCommentKey)
		canvas, jumps, _, _, _ := h.cmCalls()
		want := fakeslack.FileChanKitchen + " " + fakeslack.ListKitchenThread + " " + fakeslack.ListCommentTS + " pane"
		if !slices.Equal(canvas, []string{want}) || len(jumps) > 0 {
			t.Errorf("the click: file jumps %q, pane jumps %q; want %q", canvas, jumps, want)
		}
	})
}

// The location's title spans hold the title's emoji as an emoji (its
// image drawn from the store), and a title converted before the emoji map
// had the emoji keeps it as one, without an image, for the surface to
// resolve when it draws (richtext.TitleBlocks).
func TestActivityCanvasMentionTitleSpans(t *testing.T) {
	v, _, _, _ := cmActivity(t, cmOpts{dense: true, rev2: true, inbox: true})
	d := v.rows[fakeslack.CanvasMentionKey].d
	var emoji *richtext.Span
	for i := range d.ent.Spans {
		if d.ent.Spans[i].Kind == richtext.Emoji {
			emoji = &d.ent.Spans[i]
		}
	}
	if emoji == nil || emoji.ID != fakeslack.CanvasMentionEmoji {
		t.Fatalf("the title's spans %+v have no %s emoji", d.ent.Spans, fakeslack.CanvasMentionEmoji)
	}
	// a title converted before the map had the emoji keeps it as an emoji
	sp := richtext.TitleBlocks(&noEmoji{}, "x", json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"emoji","name":"`+fakeslack.CanvasMentionEmoji+`"}]}]}]`))
	if len(sp) != 1 || sp[0].Kind != richtext.Emoji || sp[0].ID != fakeslack.CanvasMentionEmoji || sp[0].URL != "" {
		t.Errorf("an unknown emoji element converted to %+v, want an Emoji span without an image", sp)
	}
}

// A hidden emoji (Unicode 17 with emoji_v17 off, which richEmoji leaves
// as its characters) stays its characters in the canvas entity, even when
// a custom emoji has its name; an emoji TitleBlocks kept is drawn as its
// image.
func TestActivityCanvasTitleHiddenEmoji(t *testing.T) {
	v, h, _, fs := cmActivity(t, cmOpts{dense: true, rev2: true, inbox: true})
	const hidden = "late-hidden-fixture"
	hiddenURL := fs.URL + "img/64x64/" + fakeslack.TeamID + "/" + hidden + "/00000000000000f4.png"
	v.h.Core().S.CustomEmoji.Put(hidden, hiddenURL)
	char := string(rune(0x1FAE8))
	spans := []richtext.Span{{Kind: richtext.Text, Text: "Plan "}, {Kind: richtext.Emoji, ID: hidden, Text: char},
		{Kind: richtext.Emoji, ID: fakeslack.CanvasMentionEmoji, Text: ":" + fakeslack.CanvasMentionEmoji + ":"}}
	drew := map[string]int{}
	labels := map[string]bool{}
	emojiDrawn = func(u string, px int) { drew[u] = px }
	theme.DrawHook = func(s string) { labels[s] = true }
	defer func() { emojiDrawn, theme.DrawHook = nil, nil }()
	var ops op.Ops
	for deadline := time.Now().Add(10 * time.Second); drew[fs.CanvasMentionEmojiURL()] == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the kept emoji's image never drew (drew %v)", drew)
		}
		v.canvasTitle(actFrame(&ops, image.Pt(600, 40)), spans, 13, wBold, v.p.Sec)
	}
	if _, ok := drew[hiddenURL]; ok || h.askedFor("/"+hidden+"/") {
		t.Errorf("the hidden emoji was drawn as the custom emoji of its name")
	}
	if !labels[char] {
		t.Errorf("the hidden emoji's characters aren't drawn (labels %v)", labels)
	}
}

// noEmoji is a resolver that names nothing.
type noEmoji struct{}

func (noEmoji) UserName(string) string              { return "" }
func (noEmoji) ChannelName(string) string           { return "" }
func (noEmoji) UsergroupName(string) string         { return "" }
func (noEmoji) Emoji(string) (string, string, bool) { return "", "", false }
func (noEmoji) SelfID() string                      { return "" }
