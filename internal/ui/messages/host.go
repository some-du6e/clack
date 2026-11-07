// Package messages draws Slack conversations: the virtualized channel
// message list (grouping, avatars, day dividers, unread line, reactions,
// thread bars, hover toolbar, files and attachments via richview, infinite
// scroll, jump to recent, mark read) and the thread pane.
package messages

import (
	"image"
	"os"
	"strconv"
	"sync"

	"gioui.org/text"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/blockkit"
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

// SignInHost is optional: the ui root's sign-in screen, shown by the
// list's "Sign in again" link once core reports the session ended.
type SignInHost interface {
	ShowSignIn()
}

// PointerHost is optional: when the Host implements it, popup anchors use
// the last absolute pointer position (window coordinates). Otherwise
// anchors are the click position plus the view's Origin.
type PointerHost interface {
	PointerPos() image.Point
}

// darkTextFade is richview's Style.LightTextFade for dark mode: light
// glyphs get alpha 1 - fade/scale. Ink on the same 15px line vs Chrome's
// dark mode (sk-client-theme--dark): 1x Chrome 116.5k, ours 135.1k
// unfaded, 116.5k at alpha 0.76; 2x Chrome 456.2k, ours 487.7k unfaded,
// 421.4k at 0.76 (needs ~0.886). Both fit fade 0.235.
// CLACK_DARK_TEXT_FADE overrides it for tuning.
func darkTextFade() float32 {
	if v, err := strconv.ParseFloat(os.Getenv("CLACK_DARK_TEXT_FADE"), 32); err == nil {
		return float32(v)
	}
	return 0.235
}

// styles holds one richview.Style per shaper (one per window).
type styleEntry struct {
	st   *richview.Style
	dark bool
	init bool
}

var (
	stylesMu sync.Mutex
	styles   = map[*text.Shaper]*styleEntry{}
)

func styleFor(h Host) *richview.Style { return styleForTheme(h.Theme(), h.Images()) }

func styleForTheme(th *theme.Theme, images *imgcache.Cache) *richview.Style {
	stylesMu.Lock()
	e, ok := styles[th.Shaper]
	if !ok {
		var imgs richview.Images
		if images != nil {
			imgs = images
		}
		e = &styleEntry{st: richview.NewStyle(th.Shaper, imgs, richview.LightPalette())}
		e.st.Face = theme.Typeface
		e.st.EmojiURL = th.EmojiURL // unicode emoji in text → the theme's CDN images
		styles[th.Shaper] = e
	}
	stylesMu.Unlock()
	if th.Dark != e.dark || !e.init {
		e.dark, e.init = th.Dark, true
		// theme.EmboldenFor: dark text needs the stroke, light text doesn't.
		// Message bodies need more than labels to match Chrome. Ink on the
		// same 15px line: at 1x 151.9k (no stroke) / 193.6k (0.3px) vs
		// Chrome 214.0k; at 2x 516.7k (none) / 727.4k (0.9px) vs Chrome
		// 620.5k. Both interpolate to ~0.45px, so richview applies the
		// stroke in pixels: theme.BodyEmbolden is that 0.45px.
		e.st.Embolden = theme.BodyEmboldenFor(theme.LightPalette.Fg)
		e.st.LightTextFade = 0
		if th.Dark {
			e.st.SetPalette(richview.DarkPalette())
			e.st.Embolden = theme.BodyEmboldenFor(theme.DarkPalette.Fg)
			e.st.LightTextFade = darkTextFade()
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
	if blockkit.OpenWorkflowLink(r.h, url, r.m.Channel, r.m.TS) {
		return
	}
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
