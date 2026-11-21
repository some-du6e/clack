package ui

// The media viewer playing videos (popups' video items: Slack's
// MediaViewer, viWS, with MediaViewerContentVideo, Ivom), driven through
// Gio's router over fakeslack's #files-gallery on the fake backend: a
// gallery's video opens the viewer playing (fIJO's tE), Expand opens it
// from the inline player (tR), the footer's keys, the auto-hide (LqYI)
// and the move to the next item at the end (tS).
//
// The player's state comes through video_viewer_hooks_test.go. files-shots
// copies this file onto ui as before-ui with
// testdata/video_viewer_hooks_ui.go.txt in place of the hooks (ui has no
// player): there every check fails, and the shots show what ui does.

import (
	"image"
	"os"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// vvVideoID is #files-gallery's video (fakeslack's FilesVideoID).
const vvVideoID = "F0FILESVID01"

func vvMid(r image.Rectangle) image.Point { return r.Min.Add(r.Max).Div(2) }

func vvHover(e *perfEnv, p image.Point, at time.Duration) {
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y)), Time: at})
	e.frame()
}

// vvTile is message ts's video tile scrolled wholly into the list (the
// list's rects aren't clipped to it).
func vvTile(t *testing.T, e *perfEnv, ts string) image.Rectangle {
	t.Helper()
	for i := 0; ; i++ {
		var tile image.Rectangle
		var l channelList
		if c := e.a.convos["C0FILES0001/"]; c != nil {
			l, _ = c.list.(channelList)
		}
		if l.ChannelView == nil {
			t.Fatal("no channel list")
		}
		list := l.ListRect()
		for _, r := range l.FileRects() {
			if r.TS == ts && r.File.IsVideo {
				tile = r.R
			}
		}
		if !tile.Empty() && tile.In(list) {
			return tile
		}
		if i == 60 {
			t.Fatalf("message %s's video tile %v isn't in the list %v", ts, tile, list)
		}
		switch {
		case tile.Empty():
			rows := l.RowRects()
			if len(rows) > 0 && tsBefore(ts, rows[0].TS) {
				l.ScrollPx(-list.Dy() / 2)
			} else {
				l.ScrollPx(list.Dy() / 2)
			}
		case tile.Min.Y < list.Min.Y:
			l.ScrollPx(tile.Min.Y - list.Min.Y - 16)
		default:
			l.ScrollPx(tile.Max.Y - list.Max.Y + 16)
		}
		e.frame()
		e.frame()
	}
}

// vvLightbox is the viewer on top, nil when none is open.
func vvLightbox(e *perfEnv) *popups.Lightbox {
	for i := len(e.a.overlays) - 1; i >= 0; i-- {
		if lb, ok := e.a.overlays[i].(*popups.Lightbox); ok && !lb.Dismissed() {
			return lb
		}
	}
	return nil
}

// vvOpenGallery clicks the "image and video" gallery's video and waits
// for the viewer to play it.
func vvOpenGallery(t *testing.T, e *perfEnv) {
	t.Helper()
	g := e.filesMsg("image and video")
	tile := vvTile(t, e, g.TS)
	e.clickAt(vvMid(tile), time.Second)
	e.until("the viewer playing the gallery's video", 10*time.Second, func() bool {
		st, ok := viewerState(e)
		return ok && st.video && st.file == vvVideoID && st.playing
	})
}

// A gallery's video opens the media viewer at it, playing (fIJO's tE, in
// a gallery context of two files); Space pauses and plays it (DBSG), m
// mutes it (media_muted); ← to the image closes its player, → back makes
// a new one from where the viewer left the file, playing (tC); Esc closes
// the viewer and its player.
func TestVideoViewerGallery(t *testing.T) {
	viewerVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	vvOpenGallery(t, e)
	st, _ := viewerState(e)
	if st.idx != 1 || st.n != 2 {
		t.Errorf("item %d of %d, want the gallery's video: the 2nd of 2 (the image, then the video)", st.idx+1, st.n)
	}
	e.key(key.NameSpace, 0)
	e.until("paused by Space", 5*time.Second, func() bool { st, _ := viewerState(e); return st.video && !st.playing })
	e.key(key.NameSpace, 0)
	e.until("playing again", 5*time.Second, func() bool { st, _ := viewerState(e); return st.playing })
	st, _ = viewerState(e)
	muted := st.muted
	e.key("M", 0)
	e.until("m toggles the mute", 5*time.Second, func() bool { st, _ := viewerState(e); return st.muted != muted })
	e.key("M", 0)
	e.until("m again", 5*time.Second, func() bool { st, _ := viewerState(e); return st.muted == muted })
	e.settle(500 * time.Millisecond)
	before, _ := viewerState(e)
	e.key(key.NameLeftArrow, 0)
	e.until("← to the image: no player", 5*time.Second, func() bool {
		st, ok := viewerState(e)
		return ok && st.idx == 0 && !st.video && vvPlayers(e) == 0
	})
	e.key(key.NameRightArrow, 0)
	e.until("→ back to the video, playing on from where it was", 10*time.Second, func() bool {
		st, _ := viewerState(e)
		return st.video && st.playing && st.elapsed >= before.elapsed-300*time.Millisecond
	})
	e.key(key.NameEscape, 0)
	e.until("Esc closes the viewer and its player", 5*time.Second, func() bool {
		_, ok := viewerState(e)
		return !ok && vvLightbox(e) == nil && vvPlayers(e) == 0
	})
}

// Expand in the inline player's corner (FullscreenButton, QC+B; tR)
// pauses it and opens the media viewer at its time, playing because it
// played.
func TestVideoViewerExpand(t *testing.T) {
	viewerVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	m := e.filesMsg("video")
	tile := vvTile(t, e, m.TS)
	e.clickAt(vvMid(tile), time.Second)
	e.until("the inline video playing", 10*time.Second, func() bool { p, _, ok := inlineState(e, m.TS); return ok && p })
	e.settle(700 * time.Millisecond)
	vvHover(e, vvMid(tile), 1500*time.Millisecond)
	e.frame()
	ex, ok := inlineExpand(e, m.TS)
	if !ok || ex.Empty() {
		t.Fatal("no Expand in the player's corner")
	}
	_, at, _ := inlineState(e, m.TS)
	e.clickAt(vvMid(ex), 2*time.Second)
	e.until("the viewer playing the video", 10*time.Second, func() bool {
		st, ok := viewerState(e)
		return ok && st.video && st.file == vvVideoID && st.playing
	})
	if p, _, _ := inlineState(e, m.TS); p {
		t.Error("the inline video plays on behind the viewer: Expand pauses it")
	}
	if st, _ := viewerState(e); st.elapsed < at-300*time.Millisecond {
		t.Errorf("the viewer started at %v, the inline video was at %v", st.elapsed, at)
	}
}

// The viewer's controls hide 1300 ms after the pointer last moved while
// the video plays (LqYI, --controls_hidden), and show while it's paused.
func TestVideoViewerAutoHide(t *testing.T) {
	viewerVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	vvOpenGallery(t, e)
	c := image.Pt(e.size.X/2, e.size.Y/2)
	vvHover(e, c, 3*time.Second)
	e.settle(300 * time.Millisecond)
	if st, _ := viewerState(e); !st.shown {
		t.Error("the controls don't show after the pointer moved")
	}
	e.settle(1700 * time.Millisecond)
	if st, _ := viewerState(e); st.shown || !st.playing {
		t.Errorf("1.7 s after the pointer stopped, playing: shown %v, playing %v; want hidden", st.shown, st.playing)
	}
	e.key(key.NameSpace, 0)
	e.until("paused: the controls show", 5*time.Second, func() bool { st, _ := viewerState(e); return !st.playing && st.shown })
}

// At its end the viewer moves to the next item (tS): the channel Files
// tab's queue, the video then an image.
func TestVideoViewerEndNext(t *testing.T) {
	viewerVideos(t)
	e := filesEnv(t, image.Pt(1440, 900), false)
	files := docFiles(e.filesMsg("image and video"))
	var img, vid *richtext.File
	for _, f := range files {
		switch {
		case f.ID == vvVideoID:
			vid = f
		case f.IsImage:
			img = f
		}
	}
	if img == nil || vid == nil || img.Thumb == nil {
		t.Fatalf("the gallery's files: %d", len(files))
	}
	var poster richtext.Image
	if vid.Thumb != nil {
		poster = *vid.Thumb
	}
	e.a.OpenMediaGallery([]popups.GalleryItem{{File: vid, Img: poster}, {File: img, Img: *img.Thumb}}, 0)
	e.until("the viewer playing the video", 10*time.Second, func() bool {
		st, ok := viewerState(e)
		return ok && st.video && st.playing
	})
	e.until("the next item at the end", 12*time.Second, func() bool {
		st, ok := viewerState(e)
		return ok && st.idx == 1 && !st.video
	})
}

// The viewer with a video, for files-shots (FILES_SHOTS_DIR; on ui
// VIDEO_VIEWER_SHOTS_TAG=before names them for the comparison): playing
// with its controls, its controls hidden, paused, and the inline
// player's corner with Expand.
func TestVideoViewerShots(t *testing.T) {
	if os.Getenv("FILES_SHOTS_DIR") == "" {
		t.Skip("FILES_SHOTS_DIR not set (the files-shots workflow sets it)")
	}
	tag := os.Getenv("VIDEO_VIEWER_SHOTS_TAG")
	if tag == "" {
		tag = "after"
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			viewerVideos(t)
			e := filesEnv(t, image.Pt(1440, 900), dark)
			m := e.filesMsg("video")
			tile := vvTile(t, e, m.TS)
			e.clickAt(vvMid(tile), time.Second)
			e.settle(time.Second)
			vvHover(e, vvMid(tile), 2*time.Second)
			e.filesShot("video-inline-expand-" + mode + "-" + tag)
			g := e.filesMsg("image and video")
			gt := vvTile(t, e, g.TS)
			e.clickAt(vvMid(gt), 3*time.Second)
			e.settle(time.Second)
			vvHover(e, image.Pt(e.size.X/2, e.size.Y/2), 4*time.Second)
			e.filesShot("video-viewer-playing-" + mode + "-" + tag)
			e.settle(1500 * time.Millisecond)
			e.filesShot("video-viewer-hidden-" + mode + "-" + tag)
			e.key(key.NameSpace, 0)
			e.filesShot("video-viewer-paused-" + mode + "-" + tag)
			if st, ok := viewerState(e); !ok || !st.video {
				t.Error("no video in the viewer")
			}
		})
	}
}
