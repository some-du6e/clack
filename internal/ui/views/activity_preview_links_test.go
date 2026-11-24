package views

// A mention row's preview in Activity, as Slack's ActivityMessage draws
// it (boot-shared 69nH, build 66e72b57ade1): the Dense layout passes
// withFormat false, so its channel mention is drawn but no link
// (SimpleRichTextPreview without withSlackLinks: BaseMrkdwnChannel's
// noLinking span, no ChannelHoverCard); the Detailed layout keeps it a
// link, with its hover card. Karim (α.161, Windows): "u sure u can click
// from here". Synthetic data on internal/fakeslack.

import (
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// channels sweeps the pointer over size and returns every channel whose
// hover card a trigger reported.
func (s *hvSweep) channels(size image.Point, draw func(gtx layout.Context)) map[string]bool {
	out := map[string]bool{}
	s.frame(size, draw)
	for y := 2; y < size.Y; y += 5 {
		for x := 2; x < size.X; x += 9 {
			s.ptr = image.Pt(x, y)
			s.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(x), float32(y))})
			s.frame(size, draw)
			if req, ok := s.ctl.Pending(); ok && req.ChannelID != "" {
				out[req.ChannelID] = true
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
	fs.PostAtQuiet(ch, ts, "U0FAKE00001", "<@"+fakeslack.SelfID+"> the plan is in <#"+ch+">")
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
	size := image.Pt(1000, 300)
	draw := func(gtx layout.Context) { v.Layout(gtx) }
	for _, tc := range []struct {
		name  string
		dense bool
		card  bool
	}{
		{"detailed", false, true},
		{"dense", true, false},
	} {
		v.dense, v.densitySet, v.dirty = tc.dense, true, true
		v.rows = map[string]*actRow{}
		s := newHvSweep(t)
		got := s.channels(size, draw)
		if got[ch] != tc.card {
			t.Errorf("%s: the mention row's #channel reports its hover card %v, want %v (Slack's ActivityMessage withFormat %v)", tc.name, got[ch], tc.card, !tc.dense)
		}
	}
}
