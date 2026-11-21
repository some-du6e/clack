//go:build windows || (darwin && cgo)

package ui

import (
	"fmt"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/media"
	_ "github.com/some-du6e/clack/internal/media/avf" // the OS's decoders, as cmd/clack imports them
	_ "github.com/some-du6e/clack/internal/media/mf"
)

// The parts of TestRealWindowVideoFrames that reach the inline player,
// apart so a run on code without it (video-playback-red) can stand them
// in.

// videoTileInfo is a video tile's player as the test reads it.
type videoTileInfo struct {
	mounted, playing, loading bool
	frames                    int64 // the player's frames published
	asks                      int64 // the GPU's asks for its frame
	stats                     string
}

func videoBackendAvailable() error { return media.Available() }

// playerRepaints are the layouts the players asked for so far.
func playerRepaints(a *App) int64 { return a.videoRepaints.Load() }

// videoTileState is #files-gallery's video in message ts of ch, false
// until the list laid its tile out.
func videoTileState(a *App, ch, ts string) (videoTileInfo, bool) {
	if a.vids == nil {
		return videoTileInfo{}, false
	}
	st, ok := a.vids.TileState("channel", ch, ts, fakeslack.FilesVideoID)
	if !ok {
		return videoTileInfo{}, false
	}
	in := videoTileInfo{mounted: st.Mounted, playing: st.Playing, loading: st.Loading}
	if st.Player != nil {
		s := st.Player.Stats()
		in.frames, in.asks = s.Frames, s.Asks
		in.stats = fmt.Sprintf("%+v; state %+v", s, st.Player.State())
	}
	return in, true
}

// videoClick is a click on that tile (fIJO's tE).
func videoClick(a *App, ch, ts string) bool {
	return a.vids != nil && a.vids.Click("channel", ch, ts, fakeslack.FilesVideoID)
}

// videoLoop has its player loop, so it plays through the whole measure.
func videoLoop(a *App, ch, ts string) {
	if a.vids == nil {
		return
	}
	if st, ok := a.vids.TileState("channel", ch, ts, fakeslack.FilesVideoID); ok && st.Player != nil {
		st.Player.SetLoop(true)
	}
}
