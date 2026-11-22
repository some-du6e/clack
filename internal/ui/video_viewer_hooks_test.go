package ui

// video_viewer_test.go's calls into the players (internal/ui/videoplayer
// and popups' video items). files-shots runs that file on ui with
// testdata/video_viewer_hooks_ui.go.txt in this file's place.

import (
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// vvState is the viewer on top as the tests read it.
type vvState struct {
	idx, n                int  // the item shown, of how many
	video                 bool // it's a video item with its player
	file                  string
	playing, muted, shown bool
	elapsed               time.Duration
	play, mute, bar       image.Rectangle
}

// viewerVideos plays videos on the fake backend.
func viewerVideos(t *testing.T) { fakeVideos(t) }

// viewerState is the open viewer's item, false with no viewer open.
func viewerState(e *perfEnv) (vvState, bool) {
	lb := vvLightbox(e)
	if lb == nil {
		return vvState{}, false
	}
	var s vvState
	s.idx, s.n = lb.Position()
	st, f, shown, ok := lb.VideoState()
	if ok {
		s.video, s.playing, s.muted, s.shown, s.elapsed = true, st.Playing, st.Muted, shown, st.Elapsed
		if f != nil {
			s.file = f.ID
		}
		s.play, s.mute, s.bar = lb.VideoParts()
	}
	return s, true
}

// vvPlayers are the players open.
func vvPlayers(e *perfEnv) int {
	if e.a.vids == nil {
		return 0
	}
	return e.a.vids.Mounted()
}

// inlineState is message ts's inline player in #files-gallery.
func inlineState(e *perfEnv, ts string) (playing bool, elapsed time.Duration, ok bool) {
	if e.a.vids == nil {
		return false, 0, false
	}
	st, ok := e.a.vids.TileState("channel", fakeslack.FilesChannelID, ts, fakeslack.FilesVideoID)
	return st.Playing, st.Elapsed, ok && st.Mounted
}

// inlineExpand is the inline player's Expand on screen, as its corner
// last drew it (shown while hovered).
func inlineExpand(e *perfEnv, ts string) (image.Rectangle, bool) {
	if e.a.vids == nil {
		return image.Rectangle{}, false
	}
	st, ok := e.a.vids.TileState("channel", fakeslack.FilesChannelID, ts, fakeslack.FilesVideoID)
	if !ok || st.Expand.Empty() {
		return image.Rectangle{}, false
	}
	for _, r := range rectsOf(e.channelFiles(fakeslack.FilesChannelID), ts) {
		if r.File.IsVideo {
			return st.Expand.Add(r.R.Min), true
		}
	}
	return image.Rectangle{}, false
}
