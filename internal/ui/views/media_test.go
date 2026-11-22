package views

// Round 3's review, MAJOR 4: the Threads, Unreads and Later views play a
// lone video in place, as Slack's do (seRe's gallery contexts take
// THREADS_VIEW and UNREAD_VIEW; vzTO gives a saved message's video its
// own gallery). On the round-3 head (0cbe7e63) their message bodies had
// no richview.Media: the tile was the gallery's, its click the file's
// page. The test's host plays nothing; it records what the views ask.

import (
	"image"
	"sync"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// recMedia is a richview.Media that takes every video tile and counts
// the ones it was asked to draw.
type recMedia struct {
	mu    sync.Mutex
	panes map[string]int
	pane  string
}

func (m *recMedia) MediaTile(f *richtext.File, files int, overflow bool) bool {
	return f != nil && f.IsVideo && files == 1 && !overflow
}

func (m *recMedia) LayoutMedia(gtx layout.Context, s richview.MediaSlot) layout.Dimensions {
	m.mu.Lock()
	m.panes[m.pane]++
	m.mu.Unlock()
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// mediaVAHost is a views host whose videos play (the App's ViewMedia).
type mediaVAHost struct {
	*vaHost
	mu    sync.Mutex
	asked map[string]int // pane: the messages asked for
	rec   *recMedia
}

func (h *mediaVAHost) ViewMedia(pane, ch, ts string, rh interface {
	OpenFile(f *richtext.File)
	FileAction(a richview.FileAction, f *richtext.File, btn image.Rectangle)
}) richview.Media {
	h.mu.Lock()
	h.asked[pane]++
	h.mu.Unlock()
	return &recMedia{panes: h.rec.panes, pane: pane}
}

func TestViewsPlayVideosInPlace(t *testing.T) {
	app, _ := cfFake(t)
	rec := &recMedia{panes: map[string]int{}}
	h := &mediaVAHost{vaHost: &vaHost{slickHost: &slickHost{app: app, th: theme.New(false)}}, asked: map[string]int{}, rec: rec}
	video := &richtext.File{ID: "F0VID", Name: "clip.mp4", Title: "clip.mp4", Type: "mp4", Mime: "video/mp4", IsVideo: true,
		URL: "https://files.slack.com/files-pri/T1-F0VID/clip.mp4", Permalink: "https://x.slack.com/files/U1/F0VID/clip.mp4",
		Thumb: &richtext.Image{URL: "https://files.slack.com/files-tmb/T1-F0VID/clip_thumb_video.png", W: 640, H: 360},
		OrigW: 640, OrigH: 360, DurationMS: 3000}
	m := core.Message{Channel: "C1", TS: "1700000000.000100", User: "U1",
		Doc: richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.FileBlock, File: video}}}}
	for _, tc := range []struct {
		pane string
		body func(gtx layout.Context) layout.Dimensions
	}{
		{"threads", func(gtx layout.Context) layout.Dimensions {
			v := NewThreads(h)
			v.frame()
			return v.msgBody(gtx, &m, false, 0)
		}},
		{"unreads", func(gtx layout.Context) layout.Dimensions {
			v := NewUnreads(h)
			v.frame()
			return v.msgBody(gtx, &m, false, 0)
		}},
		{"later", func(gtx layout.Context) layout.Dimensions {
			v := NewLater(h)
			v.frame()
			return v.msgBody(gtx, &m, false, 0)
		}},
	} {
		e := &vaEnv{size: image.Pt(900, 700)}
		e.draw = func(gtx layout.Context) { tc.body(gtx) }
		e.frame()
		e.frame()
		h.mu.Lock()
		asked := h.asked[tc.pane]
		h.mu.Unlock()
		rec.mu.Lock()
		drawn := rec.panes[tc.pane]
		rec.mu.Unlock()
		if asked == 0 || drawn == 0 {
			t.Errorf("%s: the lone video isn't the player's (asked for players %d times, drew %d tiles): Slack plays it in place there", tc.pane, asked, drawn)
		}
	}
}
