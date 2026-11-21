package views

import (
	"image"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// Videos in the views' messages: Slack plays a lone video in place in the
// Threads and Unreads views (seRe's isMediaGalleryViewContext takes
// THREADS_VIEW and UNREAD_VIEW, and fIJO keeps a lone video in place in
// a gallery context) and in Later (vzTO gives each playable video its own
// gallery there). The App's players draw them (videoplayer, through
// MediaSource) under a key of the view's own (base.mediaPane), and the
// tile pauses under 10% visible in the view's list on its own (Gio patch
// 53); a gallery's video still opens the media viewer.

// MediaSource is optional on the Host: the players for message ts of ch,
// as a view named pane draws it (nil where videos don't play).
type MediaSource interface {
	ViewMedia(pane, ch, ts string, h interface {
		OpenFile(f *richtext.File)
		FileAction(a richview.FileAction, f *richtext.File, btn image.Rectangle)
	}) richview.Media
}

// viewMedia is a message's players in a view, and the host they act
// through.
type viewMedia struct {
	md richview.Media
	h  *viewMediaHost
}

// mediaFor is m's players in the view, nil where the view has none.
func (b *base) mediaFor(m *core.Message) richview.Media {
	ms, ok := b.h.(MediaSource)
	if !ok || b.mediaPane == "" || m == nil || m.Channel == "" || m.TS == "" {
		return nil
	}
	if b.media == nil {
		b.media = map[string]*viewMedia{}
	}
	if len(b.media) > 2000 {
		clear(b.media)
	}
	key := m.Channel + "/" + m.TS
	e := b.media[key]
	if e == nil {
		h := &viewMediaHost{b: b}
		e = &viewMedia{md: ms.ViewMedia(b.mediaPane, m.Channel, m.TS, h), h: h}
		b.media[key] = e
	}
	e.h.m = *m
	return e.md
}

// viewMediaHost is a view message's host for its players: its file opens
// as the view's do, More actions opens the video's menu, Expand opens
// the media viewer.
type viewMediaHost struct {
	b *base
	m core.Message
}

func (h *viewMediaHost) OpenFile(f *richtext.File) {
	if f != nil && f.IsVideo { // a gallery's video: the media viewer
		if vo, ok := h.b.h.(interface {
			OpenVideoViewer(f *richtext.File, m core.Message, at time.Duration, autoplay bool) bool
		}); ok && vo.OpenVideoViewer(f, h.m, 0, true) {
			return
		}
	}
	hostHandler{h.b.h}.OpenFile(f)
}

// FileAction takes btn in the View's coordinates, which a view doesn't
// know: the players open their menu with OpenFileMenu.
func (h *viewMediaHost) FileAction(a richview.FileAction, f *richtext.File, btn image.Rectangle) {}

// OpenFileMenu is the corner's More actions (videoplayer.MenuOpener),
// under win, the button in window coordinates.
func (h *viewMediaHost) OpenFileMenu(f *richtext.File, win, view image.Rectangle, onClose func()) bool {
	ph, ok := h.b.h.(popups.Host)
	if !ok || f == nil || win.Empty() {
		return false
	}
	ph.ShowOverlay(popups.NewVideoActionsMenuClosing(ph, win, h.m, f, false, onClose))
	return true
}

// ExpandVideo is the corner's Expand (videoplayer.Expander).
func (h *viewMediaHost) ExpandVideo(f *richtext.File, at time.Duration, autoplay bool) bool {
	vo, ok := h.b.h.(interface {
		OpenVideoViewer(f *richtext.File, m core.Message, at time.Duration, autoplay bool) bool
	})
	return ok && vo.OpenVideoViewer(f, h.m, at, autoplay)
}
