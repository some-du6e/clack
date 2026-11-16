package ui

import (
	"gioui.org/io/pointer"
	"gioui.org/widget"
)

// The sidebar row's press prefetch, as Slack's web client has it (build
// 2c0d02ca130f): a conversation row's onPointerDown (boot-render PZNh
// handlePointerDown → onPressStart) starts the channel's history request
// when the row is pressed, before the release (the click) navigates there
// (prefetchPointerdownHistory, "Prefetch channel history when a sidebar
// row is pressed"). Only:
//
//   - a mouse press with the primary button and no modifier (Alt, Ctrl,
//     Meta, Shift), not on one of the row's own controls (the target's
//     closest a, button, input or [role=button|checkbox|menuitem]);
//   - outside organize mode (F: isEditMode, where a press picks the row);
//   - for a conversation that isn't the open one (getPrimaryView's id);
//   - under pointerdown_history_prefetch "treatment";
//   - for a conversation the prefetch can open (TWX9 d7's skips): not
//     archived, a member's (not_member), not a canvas's or list's
//     (file_channel); a DM only with a person the member store holds
//     (app_dm: Slackbot's and USLACK's DMs, and any whose partner isn't
//     an existent member that isn't a bot, vrX/ isExistentMember and nMbN
//     isBotUser, a partner not loaded yet included); its history not
//     loaded yet (has_history) or loading. d7's scrollmark (a channel the
//     pane reopens at a saved scroll position) isn't skipped here.
//
// The read only fills core's store (core.App.PrefetchHistory): the view
// that mounts on the click finds the page there, or joins the read while
// it's out, and the conversation on screen stays the open one until then.
//
// Slack doesn't prefetch on hover: a row's onPointerEnter only begins a
// sampled "channel_history_intent" study (client-boot 8BF7, one session in
// 100,000) that measures the dwell before a click. Nor do the quick
// switcher's rows: their selection navigates, and the navigation starts
// the request at once (requestNavigationHistoryPrefetchOnNavigate, 1zp8),
// as the channel view's mount does here.
//
// Slack also cancels a prefetch no navigation took after 3 s, or when the
// press turns into a drag (onPointerCancel, onDragStart): the request is
// aborted if it hasn't answered. Clack lets it land; the page warms the
// channel either way.

// sbPressPrefetch is a conversation row's press (id, the row's button
// btn after it took this frame's pointer events).
func (a *App) sbPressPrefetch(id string, e pointer.Event, btn *widget.Clickable) {
	if e.Kind != pointer.Press || e.Source != pointer.Mouse || e.Buttons != pointer.ButtonPrimary || e.Modifiers != 0 {
		return
	}
	if !btn.Pressed() || a.sidebar.edit.on || a.router.Current().Channel() == id {
		return // a press on the row's own controls; organize mode; the open conversation
	}
	if a.exp("pointerdown_history_prefetch") != "treatment" {
		return
	}
	c := a.model().conv(id)
	if c == nil || c.Archived || c.Preview || c.File || !a.sbPrefetchDM(c) {
		return
	}
	if svc := a.Core(); svc != nil {
		svc.PrefetchHistory(id)
	}
}

// sbPrefetchDM is d7's app_dm test: false for a DM whose partner isn't a
// person the member store holds (true for anything that isn't a DM).
func (a *App) sbPrefetchDM(c *convInfo) bool {
	if c.Kind != "im" && c.Kind != "app" {
		return true
	}
	if c.Kind == "app" || c.User == "" || c.User == "USLACKBOT" || c.User == "USLACK" {
		return false
	}
	svc := a.Core()
	if svc == nil {
		return false
	}
	u, ok := svc.User(c.User)
	return ok && !u.IsBot
}
