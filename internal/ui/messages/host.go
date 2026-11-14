// Package messages draws Slack conversations: the virtualized channel
// message list (grouping, avatars, day dividers, unread line, reactions,
// thread bars, hover toolbar, files and attachments via richview, infinite
// scroll, jump to recent, mark read) and the thread pane.
package messages

import (
	"image"
	"os"
	"strconv"
	"strings"
	"sync"

	"gioui.org/text"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Host is what the messages package needs from the ui root: the shared
// Host of docs/NATIVE.md (popups.Host, which composer.Host also is), so the
// list can open popups' modals and menus and embed the inline edit composer.
type Host interface {
	popups.Host
}

// CanvasOpener is optional: the ui root shows canvases natively (canvas
// files in messages, links to canvases).
type CanvasOpener interface {
	OpenCanvas(id string)
	OpenCanvasLink(url string) bool
}

// ChannelTabOpener is optional: OpenChannelTabLink handles a channel-tab
// link (/archives/<C>/tabs/<id>), opening a code channel's view in its
// details pane (navigateToChannelTabSlug), and reports whether url was one.
type ChannelTabOpener interface {
	OpenChannelTabLink(url string) bool
}

// ListOpener is optional: the ui root shows lists natively (list files in
// messages, links to lists and their items).
type ListOpener interface {
	OpenList(id string)
	OpenListLink(url string) bool
}

// PaneBack is optional: the back stack of the view container being laid
// out (Slack's view.prev, which the split view keeps when it navigates in
// place). While PaneBack reports true the thread header draws Slack's
// FlexpaneBackButton, and a click calls GoPaneBack.
type PaneBack interface {
	PaneBack() bool
	GoPaneBack()
}

// ListAdder is optional: the ui root's "Add to list…" dialog for a
// message (Slack's createActionAddToList, AddToListDialog).
type ListAdder interface {
	AddMessageToList(m core.Message)
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
// glyphs get alpha 1 - fade/scale. The old 0.235 cancelled a too-bright
// body colour (#d1d2d3); with Slack's --dt_color-content-sec (#b9babd) and
// the embedded Lato, unfaded bodies match Chrome's ink within ±1% at 1x
// (2026-11-07), so the default is 0.
// CLACK_DARK_TEXT_FADE overrides it for tuning.
func darkTextFade() float32 {
	if v, err := strconv.ParseFloat(os.Getenv("CLACK_DARK_TEXT_FADE"), 32); err == nil {
		return float32(v)
	}
	return 0
}

// darkTextEmbolden is richview's Style.LightEmbolden for dark mode: light
// bodies match Chrome unemboldened at 1x but were 7% light at 2x; ~0.17px
// at 2x closes it (round-1 slope ~4.5% per 0.1px), i.e. 0.34·(1-1/scale).
// CLACK_DARK_TEXT_EMB overrides it for tuning.
func darkTextEmbolden() float32 {
	if v, err := strconv.ParseFloat(os.Getenv("CLACK_DARK_TEXT_EMB"), 32); err == nil {
		return float32(v)
	}
	return 0.34
}

// styles holds one richview.Style per shaper (one per window).
type styleEntry struct {
	st   *richview.Style
	dark bool
	init bool
	gen  uint64 // th.Gen the palette was set at (stylesheet tokens)
}

var (
	stylesMu sync.Mutex
	styles   = map[*text.Shaper]*styleEntry{}
)

func styleFor(h Host) *richview.Style {
	st := styleForTheme(h.Theme(), h.Images())
	if app := h.Core(); app != nil {
		st.SetFlatHeadings(flatHeadings(app))
		st.SetWorkObjects(woStyleFlags(h, app)) // work object cards (workobjects.go)
		st.WorkObjects = woSource{app}
		st.PrivateForwarding = func() bool { v, _ := app.TeamPref("private_message_forwarding"); return string(v) == "true" }
	}
	return st
}

// RichStyle is the message rows' richview style for h's window: their
// palette, text model and experiments (the work object flexpane draws
// its Details and comments with it).
func RichStyle(h Host) *richview.Style { return styleFor(h) }

// flatHeadings is Slack's cQrd WZ, which Header (/WQN) reads for
// p-header_block--flat_scale: flat_message_heading_scale on, or
// portable_content_composer's treatment.
func flatHeadings(app *core.App) bool {
	return app.Experiment("flat_message_heading_scale") == "on" || app.Experiment("portable_content_composer") == "treatment"
}

func styleForTheme(th *theme.Theme, images *imgcache.Cache) *richview.Style {
	uiExt = theme.UIExtents(th.Shaper)
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
	if th.Dark != e.dark || !e.init || th.Gen != e.gen {
		e.dark, e.init, e.gen = th.Dark, true, th.Gen
		// theme.EmboldenFor: dark text needs the stroke, light text doesn't.
		// Message bodies need more than labels to match Chrome. Ink on the
		// same 15px line: at 1x 151.9k (no stroke) / 193.6k (0.3px) vs
		// Chrome 214.0k; at 2x 516.7k (none) / 727.4k (0.9px) vs Chrome
		// 620.5k. Both interpolate to ~0.45px, so richview applies the
		// stroke in pixels: theme.BodyEmbolden is that 0.45px.
		e.st.Embolden = theme.BodyEmboldenFor(theme.LightPalette.Fg)
		e.st.LightTextFade, e.st.LightEmbolden = 0, 0
		if th.Dark {
			e.st.SetPalette(richview.DarkPalette())
			e.st.Embolden = theme.BodyEmboldenFor(theme.DarkPalette.Fg)
			e.st.LightTextFade = darkTextFade()
			e.st.LightEmbolden = darkTextEmbolden()
		} else {
			e.st.SetPalette(richview.LightPalette())
		}
		if p, ok := richview.Themed(th); ok {
			e.st.SetPalette(p)
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
	if co, ok := r.h.(CanvasOpener); ok && co.OpenCanvasLink(url) {
		return
	}
	if to, ok := r.h.(ChannelTabOpener); ok && to.OpenChannelTabLink(url) {
		return
	}
	if lo, ok := r.h.(ListOpener); ok && lo.OpenListLink(url) {
		return
	}
	if strings.HasPrefix(url, "/services/") {
		// a footer's "Added by <app>" (AttachmentFooterBotAttribution:
		// href /services/<bot_id>, target _blank): the workspace's page
		if c := r.h.Core(); c != nil && c.C != nil {
			if base := strings.TrimSuffix(c.C.Creds.TeamURL, "/"); base != "" {
				openURL(base + url)
			}
		}
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

// OpenLink is a mrkdwn link's click with its text: Slack's
// suspicious-link warning first (popups.MaybeWarnLink, unless
// suppress_link_warning), then OpenURL.
func (r *richHandler) OpenLink(url, text string) {
	if ph, ok := r.h.(popups.Host); ok && popups.MaybeWarnLink(ph, text, url) {
		return
	}
	r.OpenURL(url)
}

// OpenBot is "Added by <app>" under a Block Kit attachment:
// renderAttributionMaybe's ProfileCardTrigger (serviceId: the bot)
// opens the bot's profile card, Slack's app card.
func (r *richHandler) OpenBot(botID string, anchor image.Point) {
	if ph, ok := r.h.(popups.Host); ok {
		ph.ShowOverlay(popups.NewBotProfileCard(ph, r.anchor(anchor), botID))
	}
}

func (r *richHandler) OpenUser(id string, anchor image.Point) {
	r.h.OpenProfile(r.anchor(anchor), id)
}
func (r *richHandler) OpenChannel(id string) { r.h.Navigate("c/" + id) }
func (r *richHandler) OpenUsergroup(id string, anchor image.Point) {
	// Slack's UserGroupElement opens the user group profile (the ui
	// root's App.OpenUserGroup, the same pane views/adapt.go opens)
	if o, ok := r.h.(interface{ OpenUserGroup(id string) }); ok {
		o.OpenUserGroup(id)
	}
}
func (r *richHandler) OpenImage(img richtext.Image) {
	r.h.OpenLightbox(img, *r.m)
}
func (r *richHandler) OpenFile(f *richtext.File) {
	if co, ok := r.h.(CanvasOpener); ok && f.ID != "" && core.IsCanvasFile(f.Type, "", f.PrettyType) {
		co.OpenCanvas(f.ID)
		return
	}
	if lo, ok := r.h.(ListOpener); ok && f.ID != "" && core.IsListFile(f.Type, "", f.PrettyType) {
		lo.OpenList(f.ID)
		return
	}
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

// FileSectionMenuOpener is optional on the Host: a message file card's
// FileSectionDropdownButton (custom_file_sections, source "file_pillow")
// opens FileSectionMoveToMenu under btn (window coordinates).
type FileSectionMenuOpener interface {
	OpenFileSectionMenu(id string, list bool, title string, btn image.Rectangle, source string)
}

// OpenFileSection is richview's FileSectionOpener: the canvas pillow's
// or the list entity's section button.
func (r *richHandler) OpenFileSection(f *richtext.File, btn image.Rectangle) {
	o, ok := r.h.(FileSectionMenuOpener)
	if !ok || f == nil {
		return
	}
	min := btn.Min
	if r.anchor != nil {
		min = r.anchor(min)
	}
	title := f.Title
	if title == "" {
		title = f.Name
	}
	o.OpenFileSectionMenu(f.ID, core.IsListFile(f.Type, "", f.PrettyType), title, image.Rectangle{Min: min, Max: min.Add(btn.Size())}, "file_pillow")
}

// JoinHuddle is a huddle message's Join Huddle (richview.HuddleJoiner):
// Slack's HuddleMsgJoinButton runs startHuddle with entryPoint
// huddle_message_button, which the ui root's huddle controller does.
func (r *richHandler) JoinHuddle(channel string) {
	if j, ok := r.h.(interface{ JoinHuddleFrom(channel, entry string) }); ok {
		j.JoinHuddleFrom(channel, "huddle_message_button")
	}
}

// RemoveAttachment is the unfurl's remove button: Slack's
// BaseDeleteMessageAttachmentDialog ("Remove preview?", the danger
// "Remove"; Clack has no admin blocklist form), then
// removeAttachmentsIncludingChildren: the preview goes from the message
// at once and chat.deleteAttachment tells Slack. A failure shows Slack's
// "Attachment removing failed!".
func (r *richHandler) RemoveAttachment(id string) {
	m := r.m
	if m == nil || id == "" {
		return
	}
	popups.ConfirmRemovePreview(r.h, m.Channel, m.TS, id)
}

// Button is a click on a Block Kit element richview drew itself: only a
// link button does anything. Rows and the Blocks host component draw a
// message's elements through blockkit's element host (blockkit.go),
// which sends blocks.actions; richview draws them itself only for a
// message still being sent, which Slack doesn't act on either.
func (r *richHandler) Button(b richtext.Button) {
	if b.URL != "" {
		openURL(b.URL)
	}
}
