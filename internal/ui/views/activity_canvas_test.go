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
	"context"
	"encoding/json"
	"image"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// cmHost records where the Activity sends a click: a jump into a canvas
// (OpenCanvasJump), a conversation (Navigate, JumpPane) or the column
// beside the inbox (OpenThreadAt, OpenMessageSecondary). Its images are
// the fake's own (fs.Fetch), and it records which ones the view asks for.
type cmHost struct {
	*vaHost
	imgs      *imgcache.Cache
	mu        sync.Mutex
	canvas    []string // "ch thread ts side|full|pane"
	routes    []string
	jumps     []string // "ch/thread/ts"
	opened    []string // the column: "ch", "ch/ts", "ch/thread/ts"
	openSec   string   // the canvas the secondary column shows
	paneDrawn string   // the canvas CanvasPane drew last
	asked     []string // the image URLs fetched
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

func (h *cmHost) OpenCanvasJump(ch, thread, ts string, where CanvasWhere) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.canvas = append(h.canvas, ch+" "+thread+" "+ts+" "+map[CanvasWhere]string{CanvasSide: "side", CanvasFull: "full", CanvasPane: "pane"}[where])
	if where == CanvasSide {
		h.openSec = core.FileOfChannel(ch)
	}
}

func (h *cmHost) CanvasShown(fileID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.openSec == fileID
}

func (h *cmHost) CanvasPane(fileID string) layout.Widget {
	return func(gtx C) D {
		h.mu.Lock()
		h.paneDrawn = fileID
		h.mu.Unlock()
		return D{Size: gtx.Constraints.Max}
	}
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
	h.mu.Unlock()
}

func (h *cmHost) OpenMessageSecondary(ch, ts string) {
	h.mu.Lock()
	h.opened = append(h.opened, ch+"/"+ts)
	h.mu.Unlock()
}

func (h *cmHost) OpenThreadAt(ch, thread, ts string) {
	h.mu.Lock()
	h.opened = append(h.opened, ch+"/"+thread+"/"+ts)
	h.mu.Unlock()
}

func (h *cmHost) OpenThread(ch, thread string) {
	h.mu.Lock()
	h.opened = append(h.opened, ch+"/"+thread+"/")
	h.mu.Unlock()
}

func (h *cmHost) SecondaryAt() (string, string) { return "", "" }

// the inbox's column (ActivitySecondary: the draw is a layout.Widget, a
// func(C) D literal would leave cmHost without the interface)
func (h *cmHost) SecondaryShows(ch, ts string) bool         { return false }
func (h *cmHost) OpenDetailSecondary(string, layout.Widget) {}
func (h *cmHost) SecondaryShowsDetail(string) bool          { return false }

var (
	_ ActivitySecondary = (*cmHost)(nil)
	_ CanvasOpener      = (*cmHost)(nil)
	_ SavedSecondary    = (*cmHost)(nil)
	_ PaneJumper        = (*cmHost)(nil)
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
// the Launch plan canvas's comment items (AddCanvasCommentActivity).
type cmOpts struct {
	dark, dense, rev2, inbox, patched, late, comments bool
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
		// drawn); core looks it up as a booted app does
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
			v, h, e, _ := cmActivity(t, o)
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
			vaShot(t, e, "activity-canvas-mention-"+base, o.dark)
		})
	}
}

// A canvas mention described before files.info answered is described
// again once core's "names" brings the file id: the loading bar gives way
// to the canvas and its title.
func TestActivityCanvasMentionRowLateFilesInfo(t *testing.T) {
	v, _, e, fs := cmActivity(t, cmOpts{dense: true, rev2: true, inbox: true, late: true})
	if d := v.rows[fakeslack.CanvasMentionKey].d; !d.ent.Loading() {
		t.Fatalf("precondition: the row knew the canvas before files.info answered (%+v)", d.ent)
	}
	e.until(t, "files.info for the canvas", func() bool { return fs.CallCount("files.info") > 0 })
	e.until(t, "the row described again as the canvas", func() bool {
		r := v.rows[fakeslack.CanvasMentionKey]
		return r != nil && r.built && r.d.entOn && r.d.ent.Icon == "canvas"
	})
	if drawn := cmDrawn(e); !drawn["Pet roster "] {
		t.Errorf("the canvas title isn't drawn once files.info answered (%d labels drawn)", len(drawn))
	}
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

// The inbox's click opens the canvas in the secondary column beside it
// (the jump reads the mention's section); the row stays the opened one
// while the column shows it.
func TestActivityCanvasMentionClickInbox(t *testing.T) {
	v, h, e, _ := cmActivity(t, cmOpts{dense: true, rev2: true, inbox: true})
	v.StepItem(1, false)
	e.frame()
	e.frame()
	canvas, _, opened, _, _ := h.cmCalls()
	want := fakeslack.CanvasMentionChannel + "  " + fakeslack.CanvasMentionTS + " side"
	if len(canvas) != 1 || canvas[0] != want {
		t.Errorf("canvas jumps %q, want %q", canvas, want)
	}
	for _, c := range opened {
		if strings.HasPrefix(c, fakeslack.CanvasMentionChannel) {
			t.Errorf("the column opened the canvas's file channel: %q", opened)
		}
	}
	if !v.selOK || v.selKey != fakeslack.CanvasMentionKey {
		t.Errorf("the mention isn't the opened row (selOK %v, key %q)", v.selOK, v.selKey)
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

// noEmoji is a resolver that names nothing.
type noEmoji struct{}

func (noEmoji) UserName(string) string              { return "" }
func (noEmoji) ChannelName(string) string           { return "" }
func (noEmoji) UsergroupName(string) string         { return "" }
func (noEmoji) Emoji(string) (string, string, bool) { return "", "", false }
func (noEmoji) SelfID() string                      { return "" }
