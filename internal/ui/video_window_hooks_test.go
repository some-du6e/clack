//go:build windows || (darwin && cgo)

package ui

import (
	"github.com/some-du6e/clack/internal/media"
	_ "github.com/some-du6e/clack/internal/media/avf" // the OS's decoders, as cmd/clack imports them
	_ "github.com/some-du6e/clack/internal/media/mf"
)

// Stand-ins for TestRealWindowVideoFrames' player calls on code with no
// inline player (video-playback-red: round 2's backends, no player): no
// video tile is a player's, and there's nothing to click.

type videoTileInfo struct {
	mounted, playing, loading bool
	frames                    int64
}

func videoBackendAvailable() error                               { return media.Available() }
func videoTileState(a *App, ch, ts string) (videoTileInfo, bool) { return videoTileInfo{}, false }
func videoClick(a *App, ch, ts string) bool                      { return false }
func videoLoop(a *App, ch, ts string)                            {}
