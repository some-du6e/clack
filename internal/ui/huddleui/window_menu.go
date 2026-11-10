package huddleui

import (
	"image"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/huddle"
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
// to the conversation, the closed captions, hiding your own tile and the
// preferences. Inviting, topics, huddle themes, layouts, the jukebox, the
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
func (h winHost) CloseSecondary()                           {}
func (h winHost) ShowOverlay(popups.Overlay)                {}
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
		{Key: "copy-huddle-link", Label: "Copy huddle link", Do: func() { c.CopyLink(ch) }},
	}
	if g := c.goToLabel(ch); g != "" { // HuddleGoToMenuItem
		h := c.h
		items = append(items, popups.MenuItem{Key: "huddle-go-to-menu-item", Label: g, Do: func() {
			c.post(func() { h.Navigate("c/" + ch); h.Invalidate() })
		}})
	}
	items = append(items, popups.Sep())
	// CaptionsSubmenu: "Show captions" › Display as… / Closed captions
	on := st.CaptionsOn
	items = append(items, popups.MenuItem{Key: "show-captions", Label: "Show captions", Sub: []popups.MenuItem{
		{Header: "Display as…"},
		{Key: "closed-captions", Label: "Closed captions", Checked: on, Do: func() { s.SetCaptions(!on) }},
	}})
	// useHideSelfMenuItem: only with someone else in the huddle
	if len(st.Participants) > 1 {
		hide := c.hideSelf.Load()
		items = append(items, popups.MenuItem{Key: "hidingSelf", Label: "Hide self-view", Checked: hide, Do: func() {
			c.hideSelf.Store(!hide)
			if !hide { // "user_self_view_is_hidden": Self-view is hidden. Undo
				c.showBannerLink("Self-view is hidden.", "eye-off", "Undo", func() { c.hideSelf.Store(false) })
			} else {
				c.showBanner("", "")
			}
			c.changed()
		}})
	}
	// useAVPreferencesSettingsMenuItem("Huddle preferences", open-in-window)
	items = append(items, popups.Sep(), popups.MenuItem{Key: "huddle-preferences", Label: "Huddle preferences", External: true, Do: func() {
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
		return "Go to direct message"
	}
	if conv.Name == "" {
		return ""
	}
	return "Go to #" + conv.Name
}

// toggleMore opens (or closes) the ⋯ menu above its button.
func (w *window) toggleMore(s *huddle.Session, st huddle.State) {
	if w.more != nil {
		w.more.Dismiss()
		w.more = nil
		return
	}
	w.picker, w.stick = false, false
	anchor := w.moreBtn.Add(image.Pt(0, w.barTop))
	w.more = popups.NewMenuOpts(winHost{w}, w.c.moreMenu(s, st, w), popups.MenuOpts{Anchor: anchor, Side: popups.Above,
		MenuClass: "p-huddle_more_actions_menu"})
}

// layoutMore draws the open ⋯ menu over the window.
func (w *window) layoutMore(gtx C) {
	if w.more == nil {
		return
	}
	w.more.Layout(gtx)
	if w.more.Dismissed() {
		w.more = nil
	}
}
