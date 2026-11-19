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
	"strings"
	"sync"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// cmHost records where the Activity sends a click: the canvas viewer
// (OpenCanvasAt) or a conversation (Navigate, JumpPane). Its images are
// the fake's own (fs.Fetch), and it records which ones the view asks for.
type cmHost struct {
	*vaHost
	imgs    *imgcache.Cache
	mu      sync.Mutex
	canvas  []string // "file section full"
	routes  []string
	jumps   []string
	opened  []string
	openSec string   // the canvas the secondary column shows
	asked   []string // the image URLs fetched
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

func (h *cmHost) OpenCanvasAt(fileID, sectionID string, full bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f := "side"
	if full {
		f = "full"
	}
	h.canvas = append(h.canvas, fileID+" "+sectionID+" "+f)
	if !full {
		h.openSec = fileID
	}
}

func (h *cmHost) CanvasShown(fileID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.openSec == fileID
}

func (h *cmHost) Navigate(r string) {
	h.mu.Lock()
	h.routes = append(h.routes, r)
	h.mu.Unlock()
}

func (h *cmHost) JumpPane(ch, thread, ts string) {
	h.mu.Lock()
	h.jumps = append(h.jumps, ch+"/"+ts)
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

// the inbox's column (ActivitySecondary: the draw is a layout.Widget, a
// func(C) D literal would leave cmHost without the interface)
func (h *cmHost) SecondaryShows(ch, ts string) bool         { return false }
func (h *cmHost) OpenDetailSecondary(string, layout.Widget) {}
func (h *cmHost) SecondaryShowsDetail(string) bool          { return false }

var _ ActivitySecondary = (*cmHost)(nil)
var _ CanvasOpener = (*cmHost)(nil)

// cmOpts is one layout: Dense or Detailed, activity_entry_layout_revisions_2
// on or off, the inbox's detail panel beside it (inbox) or the persistent
// one, and whether a plugin patches ActivityItem.
type cmOpts struct {
	dark, dense, rev2, inbox, patched bool
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
	blocks, _ := json.Marshal(fakeslack.CanvasMentionTitleBlocks())
	s.PutCanvasRef(fakeslack.CanvasMentionFile, richtext.CanvasRef{Title: fakeslack.CanvasMentionTitle, TitleBlocks: blocks,
		Status: "normal", Kind: "canvas"})
	s.CustomEmoji.Put(fakeslack.CanvasMentionEmoji, fs.CanvasMentionEmojiURL())
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
	e.until(t, "the canvas mention's row", func() bool { r := v.rows[fakeslack.CanvasMentionKey]; return r != nil && r.built })
	e.frame()
	return v, h, e, fs
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

// With the persistent detail panel (the inbox without
// activity_inbox_remove_persistent_detail_panel), a click on the mention
// opens the canvas in Primary at the mention's section, as Slack's
// jumpToMessage does for a canvas's file channel, never the file channel's
// messages.
func TestActivityCanvasMentionClickPersistent(t *testing.T) {
	v, h, e, _ := cmActivity(t, cmOpts{dense: true, rev2: true})
	v.StepItem(1, false) // the newest row: the canvas mention
	e.frame()
	h.mu.Lock()
	defer h.mu.Unlock()
	want := fakeslack.CanvasMentionFile + " " + fakeslack.CanvasMentionSection + " full"
	if len(h.canvas) != 1 || h.canvas[0] != want {
		t.Errorf("canvas opened %q, want %q", h.canvas, want)
	}
	for _, j := range h.jumps {
		if strings.HasPrefix(j, fakeslack.CanvasMentionChannel) {
			t.Errorf("the panel jumped to the canvas's file channel: %q", h.jumps)
		}
	}
	for _, r := range h.routes {
		if strings.Contains(r, fakeslack.CanvasMentionChannel) {
			t.Errorf("navigated to the canvas's file channel: %q", h.routes)
		}
	}
}

// The inbox's click opens the canvas in the secondary column beside it,
// at the section; the row stays the opened one while the column shows it.
func TestActivityCanvasMentionClickInbox(t *testing.T) {
	v, h, e, _ := cmActivity(t, cmOpts{dense: true, rev2: true, inbox: true})
	v.StepItem(1, false)
	e.frame()
	e.frame()
	h.mu.Lock()
	want := fakeslack.CanvasMentionFile + " " + fakeslack.CanvasMentionSection + " side"
	canvas, opened := append([]string(nil), h.canvas...), append([]string(nil), h.opened...)
	h.mu.Unlock()
	if len(canvas) != 1 || canvas[0] != want {
		t.Errorf("canvas opened %q, want %q", canvas, want)
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
