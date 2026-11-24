package views

// An Activity inbox row's preview, as Slack draws it (n17J ActivityList,
// build 66e72b57ade1): every dense row is ActivityRowContentSingleLine and
// every multi-line one ActivityRowContentMultiLine, whose ActivityMessage
// draws the mentions and links as such (withFormat true) under CSS
// pointer-events:none. So in both layouts the preview's @mention and
// #channel open no card and its link stays a link. Karim (α.161,
// Windows): "u sure u can click from here". Synthetic data on
// internal/fakeslack.

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// cards sweeps the pointer over size and returns every member and
// channel whose hover card a trigger reported.
func (s *hvSweep) cards(size image.Point, draw func(gtx layout.Context)) map[string]bool {
	out := map[string]bool{}
	s.frame(size, draw)
	for y := 2; y < size.Y; y += 5 {
		for x := 2; x < size.X; x += 9 {
			s.ptr = image.Pt(x, y)
			s.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(x), float32(y))})
			s.frame(size, draw)
			if req, ok := s.ctl.Pending(); ok {
				out[req.UserID+req.ChannelID] = true
			}
		}
	}
	return out
}

func TestActivityDenseMentionPreviewIsInert(t *testing.T) {
	sz := fakeslack.Small
	sz.Views = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	ch := fakeslack.ViewsChannel
	ts := fmt.Sprintf("%d.000100", time.Now().Add(-5*time.Minute).Unix())
	fs.PostAtQuiet(ch, ts, "U0FAKE00001", "<@"+fakeslack.SelfID+"> <https://example.invalid/doc|doc> the plan is in <#"+ch+">")
	key := "at_user-" + ch + "-" + ts
	fs.UpsertActivity(map[string]any{"key": key, "feed_ts": ts, "is_unread": true, "is_bot": false,
		"item": map[string]any{"type": "at_user", "message": map[string]any{"ts": ts, "channel": ch, "author_user_id": "U0FAKE00001"}}})
	app := actOrderBoot(t, fs, t.TempDir())
	v, e := actOrderView(app)
	e.until(t, "the mention's row", func() bool {
		for _, it := range actPage(v).Items {
			if actKey(&it) == key {
				return !v.data.busy()
			}
		}
		return false
	})
	// links reports whether a preview view of the message holds the link
	// as a link (not its text alone)
	links := func() bool {
		for k, en := range v.rich {
			if !strings.HasSuffix(k, ch+"/"+ts) {
				continue
			}
			for _, b := range en.v.Doc().Blocks {
				for _, sp := range b.Spans {
					if sp.Kind == richtext.Link && sp.URL == "https://example.invalid/doc" {
						return true
					}
				}
			}
		}
		return false
	}
	size := image.Pt(1000, 300)
	draw := func(gtx layout.Context) { v.Layout(gtx) }
	for _, dense := range []bool{false, true} {
		name := map[bool]string{false: "detailed", true: "dense"}[dense]
		v.dense, v.densitySet, v.dirty = dense, true, true
		v.rows = map[string]*actRow{}
		clear(v.rich)
		got := newHvSweep(t).cards(size, draw)
		if got[fakeslack.SelfID] || got[ch] {
			t.Errorf("%s: the mention row's preview opens a hover card (@mention %v, #channel %v): Slack's row is pointer-events:none", name, got[fakeslack.SelfID], got[ch])
		}
		if !links() {
			t.Errorf("%s: the preview's link isn't a link any more (Slack's ActivityMessage keeps withFormat true: c-link)", name)
		}
	}
}

// Without the activity inbox Slack's older rows keep the preview live;
// with it, every row type's preview is inert, DMs and threads included.
func TestActivityPreviewInertUnderTheInbox(t *testing.T) {
	app := actFeedApp(t)
	h := &slickHost{app: app, th: theme.New(false)}
	v := NewActivity(h)
	for _, typ := range []string{"at_user", "dm", "mpdm", "channel", "thread_v2", "message_reaction"} {
		for _, dense := range []bool{false, true} {
			v.dense = dense
			r := &actRow{v: v, it: &core.ActivityItem{Type: typ}}
			app.SetExperimentGroup("activity_inbox", "on")
			if !r.inertPreview() {
				t.Errorf("inbox on, %s (dense %v): the preview isn't inert", typ, dense)
			}
			app.SetExperimentGroup("activity_inbox", "off")
			if r.inertPreview() {
				t.Errorf("inbox off, %s (dense %v): the preview is inert; Slack's older rows keep it live", typ, dense)
			}
		}
	}
}

// An inert and a live preview of one message are two views: sharing one,
// each frame set it back and forth (a frame loop with MentionProfiles).
func TestActivityPreviewInertKeyedApart(t *testing.T) {
	app := actFeedApp(t)
	h := &slickHost{app: app, th: theme.New(false)}
	v := NewActivity(h)
	m := &core.Message{TS: "1700000000.000100", Channel: "C1", User: "U2", Text: "see the plan"}
	m.Doc = richtext.Doc{Blocks: richtext.FromMrkdwn(nil, m.Text)}
	var ops op.Ops
	for range 2 {
		gtx := actFrame(&ops, image.Pt(600, 200))
		v.msgBodySub(gtx, m, false, 1, "", true)
		v.msgBodySub(gtx, m, false, 1, "", false)
	}
	n := 0
	for k := range v.rich {
		if strings.HasSuffix(k, "C1/1700000000.000100") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d cached views for the message, want an inert and a live one", n)
	}
}
