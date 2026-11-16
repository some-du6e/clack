package ui

import (
	"gioui.org/io/pointer"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/messages"
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
//     (file_channel), not a DM with Slackbot, USLACK or an app (app_dm),
//     its history not loaded yet (has_history) or loading. d7's scrollmark
//     (a channel the pane reopens at a saved scroll position) isn't
//     skipped here.
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
	if c == nil || c.Archived || c.Preview || c.File || c.Kind == "app" || c.User == "USLACKBOT" || c.User == "USLACK" {
		return
	}
	messages.PrefetchHistory(a, id)
}
