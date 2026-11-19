package ui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"time"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

// videoBackend, when set (tests: media.NewFake), is the players'
// backend; nil is the OS's (media.Default: Media Foundation on Windows,
// AVFoundation on macOS; none on Linux, where a video tile's click opens
// its page as before).
var videoBackend media.Backend

// videos is the window's inline video players, made when a message list
// first asks.
func (a *App) videos() *videoplayer.Controller {
	if a.vids != nil {
		return a.vids
	}
	a.vids = videoplayer.New(videoplayer.Env{
		Manager:    media.NewManager(videoBackend),
		Open:       a.openVideo,
		FrameReady: a.animReady, // a new video frame: a quiet frame (quiet.go)
		Repaint:    a.Invalidate,
		Pref: func(name string) (json.RawMessage, bool) {
			if c := a.Core(); c != nil {
				return c.Pref(name)
			}
			return nil, false
		},
		SetPref: func(name string, v any) {
			c := a.Core()
			if c == nil {
				return
			}
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := c.SetPref(ctx, name, v); err != nil {
					log.Printf("video: users.prefs.set %s: %v", name, err)
				}
			}()
		},
		Experiment: func(name string) string {
			if c := a.Core(); c != nil {
				return c.Experiment(name)
			}
			return ""
		},
		Theme: func() *theme.Theme { return a.th },
	})
	return a.vids
}

// openVideo is a player's source: core.App.OpenRange (Range from off, the
// session's TLS fingerprint and cookie, the bytes in memory only).
func (a *App) openVideo(ctx context.Context, rawURL string, off int64) (io.ReadCloser, media.Info, error) {
	c := a.Core()
	if c == nil {
		return nil, media.Info{Total: -1}, errors.New("video: signed out")
	}
	rc, info, err := c.OpenRange(ctx, rawURL, off)
	return rc, media.Info{Status: info.Status, Total: info.Total, Seekable: info.Seekable, ETag: info.ETag, LastModified: info.LastModified}, err
}

// MessageMedia is messages.MediaHost's: message ts's video players in
// list pane, nil where no backend plays video.
func (a *App) MessageMedia(pane, ch, ts string, h messages.MediaRowHost) richview.Media {
	v := a.videos()
	if !v.Available() {
		return nil
	}
	return v.Row(pane, ch, ts, h)
}
