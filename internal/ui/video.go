package ui

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"io"
	"log"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
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
// first asks. A controller is one session's (closeVideos ends it), so its
// calls use that session's core, taken here: the players' own goroutines
// call them, and a.data changes on the UI goroutine.
func (a *App) videos() *videoplayer.Controller {
	if a.vids != nil {
		return a.vids
	}
	c := a.Core()
	var writes serialQueue // users.prefs.set and media.progress.update, in the order the players made them
	a.vids = videoplayer.New(videoplayer.Env{
		Manager: media.NewManager(videoBackend),
		Open: func(ctx context.Context, rawURL string, off int64) (io.ReadCloser, media.Info, error) {
			return openVideo(ctx, c, rawURL, off)
		},
		FrameReady:  a.videoFrameReady, // a new video frame: a quiet frame of this window (quiet.go)
		FrameOrigin: a.FrameOrigin,
		Repaint:     a.videoRepaint,
		Pref: func(name string) (json.RawMessage, bool) {
			if c != nil {
				return c.Pref(name)
			}
			return nil, false
		},
		SetPref: func(name string, v any) {
			if c == nil {
				return
			}
			writes.do(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := c.SetPref(ctx, name, v); err != nil {
					log.Printf("video: users.prefs.set %s: %v", name, err)
				}
			})
		},
		Experiment: func(name string) string {
			if c != nil {
				return c.Experiment(name)
			}
			return ""
		},
		Theme:     func() *theme.Theme { return a.th },
		FocusRing: func(gtx layout.Context, r image.Rectangle, rad int) { popups.FocusRing(gtx, a.th, r, rad) },
		Tooltip: func(gtx layout.Context, id any, hovered bool, r image.Rectangle, below bool, delay time.Duration, label string) {
			st := a.vidTips[id]
			if st == nil {
				if !hovered {
					return
				}
				if a.vidTips == nil {
					a.vidTips = map[any]*popups.TipState{}
				}
				st = &popups.TipState{}
				a.vidTips[id] = st
			}
			st.Delay = delay
			side := popups.Above
			if below {
				side = popups.Below
			}
			off := op.Offset(r.Min).Push(gtx.Ops)
			popups.Tooltip(gtx, a.th, st, hovered, r.Size(), side, label, "")
			off.Pop()
		},
		ProgressUpdate: func(file string, offsetMS, durationMS int64) {
			if c == nil {
				return
			}
			writes.do(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := c.MediaProgressUpdate(ctx, file, offsetMS, durationMS); err != nil {
					log.Printf("video: media.progress.update %s: %v", file, err)
				}
			})
		},
	})
	return a.vids
}

// serialQueue runs its calls one at a time, in the order they came, off
// the caller's goroutine: two writes of one pref (a slider's press, then
// its release) reach the server in that order.
type serialQueue struct {
	mu      sync.Mutex
	q       []func()
	running bool
}

func (s *serialQueue) do(f func()) {
	s.mu.Lock()
	s.q = append(s.q, f)
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()
	go func() {
		for {
			s.mu.Lock()
			if len(s.q) == 0 {
				s.running = false
				s.mu.Unlock()
				return
			}
			f := s.q[0]
			s.q = s.q[1:]
			s.mu.Unlock()
			f()
		}
	}()
}

// videoRepaint is a player's state change its overlay shows (a play, a
// pause, the time while hovered): a layout, counted.
func (a *App) videoRepaint() {
	a.videoRepaints.Add(1)
	a.Invalidate()
}

// closeVideos closes every player and forgets the controller: the
// session or the window going (sign-out, the session ending or the
// api.accounts sign-in screen, an account switch, quit), as Slack's
// client view unmounting every <video> does. UI goroutine.
func (a *App) closeVideos() {
	if a.vids != nil {
		a.vids.CloseAll()
		a.vids = nil
	}
	a.vidTips = nil
}

// Videos is the window's players (popups' media viewer plays its video
// items with them: one plays at a time with the inline ones).
func (a *App) Videos() *videoplayer.Controller { return a.videos() }

// VideosPlay reports whether videos play here (a backend), for the
// views that open them in the media viewer.
func (a *App) VideosPlay() bool { return a.videos().Available() }

// OpenVideoViewer is messages.VideoViewerOpener's: the media viewer at f,
// one of m's files, from at, playing unless !autoplay. false where
// videos don't play (the caller opens the file's page).
func (a *App) OpenVideoViewer(f *richtext.File, m core.Message, at time.Duration, autoplay bool) bool {
	lb := popups.NewVideoViewer(a, m, f, at, autoplay)
	if lb == nil {
		return false
	}
	a.ShowOverlay(lb)
	return true
}

// openVideo is a player's source: core.App.OpenRange (Range from off, the
// session's TLS fingerprint and cookie, the bytes in memory only).
func openVideo(ctx context.Context, c *core.App, rawURL string, off int64) (io.ReadCloser, media.Info, error) {
	if c == nil {
		return nil, media.Info{Total: -1}, errors.New("video: signed out")
	}
	rc, info, err := c.OpenRange(ctx, rawURL, off)
	return rc, media.Info{Status: info.Status, Total: info.Total, Seekable: info.Seekable, ETag: info.ETag, LastModified: info.LastModified}, err
}

// ViewMedia is views.MediaSource's: message ts's players as the view
// named pane draws it (the Threads, Unreads and Later views), under the
// view's own keys.
func (a *App) ViewMedia(pane, ch, ts string, h interface {
	OpenFile(f *richtext.File)
	FileAction(a richview.FileAction, f *richtext.File, btn image.Rectangle)
}) richview.Media {
	return a.MessageMedia(pane, ch, ts, h)
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
