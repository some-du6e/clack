// Package messages draws Slack conversations: the virtualized channel
// message list (grouping, avatars, day dividers, unread line, reactions,
// thread bars, hover toolbar, files and attachments via richview, infinite
// scroll, jump to recent, mark read) and the thread pane.
package messages

import (
	"image"
	"sync"

	"gioui.org/text"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Host is what the messages package needs from the ui root: the shared
// Host of docs/NATIVE.md (popups.Host, which composer.Host also is), so the
// list can open popups' modals and menus and embed the inline edit composer.
type Host interface {
	popups.Host
}

// PointerHost is optional: when the Host implements it, popup anchors use
// the last absolute pointer position (window coordinates). Otherwise
// anchors are the click position plus the view's Origin.
type PointerHost interface {
	PointerPos() image.Point
}

// styles holds one richview.Style per shaper (one per window).
type styleEntry struct {
	st   *richview.Style
	dark bool
}

var (
	stylesMu sync.Mutex
	styles   = map[*text.Shaper]*styleEntry{}
)

func styleFor(h Host) *richview.Style {
	th := h.Theme()
	stylesMu.Lock()
	e, ok := styles[th.Shaper]
	if !ok {
		var imgs richview.Images
		if c := h.Images(); c != nil {
			imgs = c
		}
		e = &styleEntry{st: richview.NewStyle(th.Shaper, imgs, richview.LightPalette())}
		e.st.Face = theme.Typeface
		styles[th.Shaper] = e
	}
	stylesMu.Unlock()
	if th.Dark != e.dark {
		e.dark = th.Dark
		if th.Dark {
			e.st.SetPalette(richview.DarkPalette())
		} else {
			e.st.SetPalette(richview.LightPalette())
		}
	}
	return e.st
}

// richHandler routes richview clicks to the Host.
type richHandler struct {
	h      Host
	m      *core.Message
	anchor func(p image.Point) image.Point
}

func (r *richHandler) OpenURL(url string) {
	if ch, ts, ok := slackLink(url); ok {
		if ts != "" {
			r.h.Navigate("c/" + ch + "/t/" + ts)
		} else {
			r.h.Navigate("c/" + ch)
		}
		return
	}
	openURL(url)
}
func (r *richHandler) OpenUser(id string, anchor image.Point) {
	r.h.OpenProfile(r.anchor(anchor), id)
}
func (r *richHandler) OpenChannel(id string) { r.h.Navigate("c/" + id) }
func (r *richHandler) OpenUsergroup(id string, anchor image.Point) {
	r.h.Toast("User group")
}
func (r *richHandler) OpenImage(img richtext.Image) {
	r.h.OpenLightbox(img, *r.m)
}
func (r *richHandler) OpenFile(f *richtext.File) {
	if f.IsImage && f.Thumb != nil {
		img := *f.Thumb
		if f.URL != "" {
			img.URL = f.URL
			img.Proxy = true
		}
		if img.Title == "" {
			img.Title = f.Title
		}
		r.h.OpenLightbox(img, *r.m)
		return
	}
	if f.Permalink != "" {
		openURL(f.Permalink)
	} else if f.URL != "" {
		openURL(f.URL)
	}
}
func (r *richHandler) Button(b richtext.Button) {
	if b.URL != "" {
		openURL(b.URL)
		return
	}
	r.h.Toast("Interactive buttons aren't supported yet")
}
