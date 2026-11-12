package huddleui

import (
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The huddle window's ⋯ (HuddleToolbarMoreActionsButton: "More actions",
// ellipsis-vertical-filled, a menu above it, position "top"). Slack builds
// the expanded window's menu (gantry-v2-async-client-boot-shared a+g1) as:
//
//	Invite people · Copy huddle link · Go to #channel | Go to direct message
//	────
//	Add topic · Choose a huddle theme · Layouts › · Play music › · Huddle language ›
//	────
//	Show captions › (Display as… · Closed captions · Side-by-side) · Hide self-view
//	────
//	Huddle preferences
//
// Clack keeps the rows it can do: copying the link (rooms.getLink), going
// to the conversation, the topic (screenhero.rooms.rename, topic.go), the
// closed captions (rooms.startTranscription, captions_log.go), hiding your
// own tile and the preferences. Huddle themes, layouts, the jukebox, the
// huddle language and side-by-side captions need Slack features Clack
// doesn't have (docs/HUDDLES-AUDIT.md lists them).

// winHost is popups.Host for menus drawn in the huddle window: its own
// theme (shapers aren't shared across windows) and invalidation; the rest
// goes to the main window on its goroutine.
type winHost struct{ w *window }

func (h winHost) Core() *core.App         { return h.w.c.h.Core() }
func (h winHost) Theme() *theme.Theme     { return h.w.th }
func (h winHost) Images() *imgcache.Cache { return h.w.c.h.Images() }
func (h winHost) Invalidate()             { h.w.invalidate() }
func (h winHost) Navigate(route string) {
	m := h.w.c.h
	h.w.c.post(func() { m.Navigate(route); m.Invalidate() })
}
func (h winHost) OpenThread(channel, ts string) {
	m := h.w.c.h
	h.w.c.post(func() { m.OpenThread(channel, ts); m.Invalidate() })
}
func (h winHost) CloseSecondary() {}

// ShowOverlay stacks o over the huddle window (its modals: Slack opens
// HuddleStillHereModal, the topic dialog and DeviceWarningModal in the
// huddle window when it's open). Any goroutine.
func (h winHost) ShowOverlay(o popups.Overlay) {
	h.w.mu.Lock()
	h.w.overlays = append(h.w.overlays, o)
	h.w.mu.Unlock()
	h.w.invalidate()
}

// post runs f on the huddle window's goroutine at its next frame.
func (h winHost) post(f func()) {
	h.w.mu.Lock()
	h.w.posts = append(h.w.posts, f)
	h.w.mu.Unlock()
	h.w.invalidate()
}

func (h winHost) OpenEmojiPicker(image.Point, func(string)) {}
func (h winHost) OpenProfile(image.Point, string)           {}
func (h winHost) OpenMessageMenu(image.Point, core.Message) {}
func (h winHost) OpenLightbox(richtext.Image, core.Message) {}
func (h winHost) Toast(text string)                         { h.w.c.h.Toast(text) }

// moreMenu is the ⋯ menu for the huddle you're in; w is the huddle
// window when the menu opens there (nil: the docked bar).
func (c *Controller) moreMenu(s *huddle.Session, st huddle.State, w *window) []popups.MenuItem {
	ch := st.Channel
	items := []popups.MenuItem{
		{Key: "copy-huddle-link", Label: nsHuddles.T("Copy huddle link"), Do: func() { c.CopyLink(ch) }},
	}
	if g := c.goToLabel(ch); g != "" { // HuddleGoToMenuItem
		h := c.h
		items = append(items, popups.MenuItem{Key: "huddle-go-to-menu-item", Label: g, Do: func() {
			c.post(func() { h.Navigate("c/" + ch); h.Invalidate() })
		}})
	}
	items = append(items, popups.Sep())
	var ph popups.Host // where the menu's dialogs open: the window it's in
	if w != nil {
		ph = winHost{w}
	} else if h, ok := c.h.(popups.Host); ok {
		ph = h
	}
	// HuddleEditTopicMenuItem (topic.go), then a divider before captions
	items = append(items, c.topicItem(s, st, ph), popups.Sep())
	// CaptionsSubmenu: "Show captions" › Display as… / Closed captions
	on := st.CaptionsOn
	items = append(items, popups.MenuItem{Key: "show-captions", Label: nsHuddles.T("Show captions"), Sub: []popups.MenuItem{
		{Header: nsHuddles.T("Display as…")},
		{Key: "closed-captions", Label: nsHuddles.T("Closed captions"), Checked: on, Do: func() { c.ToggleClosedCaptions(s) }},
	}})
	// useHideSelfMenuItem: only with someone else in the huddle
	if len(st.Participants) > 1 {
		hide := c.hideSelf.Load()
		items = append(items, popups.MenuItem{Key: "hidingSelf", Label: nsHuddles.T("Hide self-view"), Checked: hide, Do: func() {
			c.hideSelf.Store(!hide)
			if !hide { // "user_self_view_is_hidden": Self-view is hidden. Undo
				text, undo, _ := linkParts("<strong>Self-view is hidden.</strong> <a>Undo</a>") // HuddlesInformationalBanner
				c.showBannerLink(text, "eye-off", undo, func() { c.hideSelf.Store(false) })
			} else {
				c.showBanner("", "")
			}
			c.changed()
		}})
	}
	// useAVPreferencesSettingsMenuItem("Huddle preferences", open-in-window)
	items = append(items, popups.Sep(), popups.MenuItem{Key: "huddle-preferences", Label: nsHuddles.T("Huddle preferences"), External: true, Do: func() {
		if p, ok := c.h.(interface{ OpenPreferences(section string) }); ok {
			c.post(func() { p.OpenPreferences("video_audio") })
		}
	}})
	return items
}

// goToLabel is HuddleGoToTipContent: "Go to direct message" for a DM or
// group DM, else "Go to #channel"; "" when there's nowhere to go.
func (c *Controller) goToLabel(ch string) string {
	app := c.h.Core()
	if app == nil {
		return ""
	}
	conv, ok := app.Conv(ch)
	if !ok {
		return ""
	}
	switch conv.Kind {
	case "im", "mpim":
		if i18n.Locale() != i18n.EnUS {
			return nsHuddles.T("Go to conversation") // GoToTooltip
		}
		return "Go to direct message"
	}
	if conv.Name == "" {
		return ""
	}
	return nsHuddles.T("Go to {copyChannelName}", "copyChannelName", "#"+conv.Name) // HuddleGoToTipContent
}

// toggleMore opens (or closes) the ⋯ menu above its button.
func (w *window) toggleMore(s *huddle.Session, st huddle.State) {
	if w.more != nil {
		w.more.Dismiss()
		w.more = nil
		return
	}
	w.closeMenus()
	anchor := w.moreBtn.Add(image.Pt(0, w.barTop))
	w.more = popups.NewMenuOpts(winHost{w}, w.c.moreMenu(s, st, w), popups.MenuOpts{Anchor: anchor, Side: popups.Above,
		MenuClass: "p-huddle_more_actions_menu"})
}

// toggleAudio opens (or closes) the mic's Audio settings menu above its
// caret (position "top", closeOnSelect false: a pick keeps it open until
// the menu item closes it).
func (w *window) toggleAudio(s *huddle.Session) {
	if w.audio != nil {
		w.audio.Dismiss()
		w.audio = nil
		return
	}
	w.closeMenus()
	anchor := w.audioBtn.Add(image.Pt(0, w.barTop))
	w.audio = popups.NewMenuOpts(winHost{w}, w.c.audioMenu(s), popups.MenuOpts{Anchor: anchor, Side: popups.Above,
		MenuClass: "p-huddle_more_actions_menu"})
}

// closeMenus closes the toolbar's menus and pickers (one is open at a
// time, as Slack's popovers close on the next click outside them).
func (w *window) closeMenus() {
	for _, m := range []**popups.Menu{&w.more, &w.audio, &w.video, &w.leaveMenu} {
		if *m != nil {
			(*m).Dismiss()
			*m = nil
		}
	}
	w.closeQuick()
	w.stick = false
}

// toggleQuick opens (or closes) the emoji button's quick picker above it.
func (w *window) toggleQuick(s *huddle.Session) {
	if w.quick != nil {
		w.closeQuick()
		return
	}
	w.closeMenus()
	anchor := w.reactBtn.Add(image.Pt(0, w.barTop))
	w.quick = newQuickPicker(winHost{w}, anchor, w.popout == "", func(name string) { _ = s.React(name, "") })
}

func (w *window) closeQuick() {
	if w.quick != nil {
		w.quick.Dismiss()
		w.quick = nil
	}
}

// layoutMore draws the open toolbar menus and the quick picker over the
// window.
func (w *window) layoutMore(gtx C) {
	if w.quick != nil {
		w.quick.Layout(gtx)
		if w.quick != nil && w.quick.Dismissed() {
			w.quick = nil
		}
	}
	for _, m := range []**popups.Menu{&w.video, &w.leaveMenu} {
		if *m != nil {
			(*m).Layout(gtx)
			if (*m).Dismissed() {
				*m = nil
			}
		}
	}
	if w.more != nil {
		w.more.Layout(gtx)
		if w.more.Dismissed() {
			w.more = nil
		}
	}
	if w.audio != nil {
		w.audio.Layout(gtx)
		if w.audio.Dismissed() {
			w.audio = nil
		}
	}
}

// runPosts runs what winHost.post queued (start of a frame).
func (w *window) runPosts() {
	w.mu.Lock()
	ps := w.posts
	w.posts = nil
	w.mu.Unlock()
	for _, f := range ps {
		f()
	}
}

// layoutOverlays draws the stacked overlays (modals) over everything,
// dropping the dismissed ones.
func (w *window) layoutOverlays(gtx C) {
	w.mu.Lock()
	ovs := append([]popups.Overlay(nil), w.overlays...)
	w.mu.Unlock()
	if len(ovs) == 0 {
		return
	}
	var keep []popups.Overlay
	for _, o := range ovs {
		g := gtx
		g.Constraints = layout.Exact(gtx.Constraints.Max)
		o.Layout(g)
		if !o.Dismissed() {
			keep = append(keep, o)
		}
	}
	w.mu.Lock()
	// overlays shown during this frame stay
	for _, o := range w.overlays[len(ovs):] {
		keep = append(keep, o)
	}
	w.overlays = keep
	w.mu.Unlock()
}
