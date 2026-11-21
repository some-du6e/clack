package ui

// video_pip_test.go's calls into the mini player (videoplayer/pip.go,
// video_pip.go). files-shots runs that file on ui with
// testdata/video_pip_hooks_ui.go.txt in this file's place.

import (
	"image"
	"time"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// pipView is the mini player as the last frame drew it.
type pipView struct {
	rect     image.Rectangle   // the shell, in the window
	controls []image.Rectangle // Expand, Jump to message, Close
	corner   string
	playing  bool
	elapsed  time.Duration
	trigger  string
}

// pipShown is the mini player, false with none.
func pipShown(e *perfEnv) (pipView, bool) {
	if e.a.vids == nil {
		return pipView{}, false
	}
	if _, _, ok := e.a.vids.Pip(); !ok {
		return pipView{}, false
	}
	at, _, playing := e.a.vids.PipPlayback()
	return pipView{rect: e.a.pip.rect, controls: append([]image.Rectangle(nil), e.a.pip.ctlRects...), corner: e.a.pip.corner,
		playing: playing, elapsed: at, trigger: e.a.vids.PipTrigger()}, true
}

// inlineInPip is whether message ts's inline tile shows that the mini
// player has its video.
func inlineInPip(e *perfEnv, ts string) bool {
	st, _ := e.videoState(ts)
	return st.InPip
}

// viewerDock is the open viewer's Dock to corner, empty with none.
func viewerDock(e *perfEnv) image.Rectangle {
	if lb, ok := topOverlay[*popups.Lightbox](e.a); ok {
		return lb.DockRect()
	}
	return image.Rectangle{}
}
