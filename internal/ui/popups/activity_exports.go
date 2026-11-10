package popups

import (
	"context"
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
)

// Helpers the Activity view's item menu (views/activity_slack.go) shares
// with the channel menus here.

// CopyText puts s on the clipboard.
func CopyText(gtx layout.Context, s string) { copyText(gtx, s) }

// ChannelLink is a conversation's link, as the channel menu's Copy link
// builds it.
func ChannelLink(h Host, ch string) string {
	return "https://" + teamDomain(h) + ".slack.com/archives/" + ch
}

// IsMuted reports whether the viewer muted ch, as the channel menus see it
// (a toggle made here counts before the server echoes it).
func IsMuted(h Host, ch string) bool { return actIsMuted(h, ch) }

// NewReminderModal is the message menu's "Custom…" Reminder dialog (save
// for later with a due time: core Save), which Slack's activity item menu
// opens too (onAddReminder "Custom" → the custom reminder dialog).
func NewReminderModal(h Host, ch, ts string) *Modal { return actNewReminderModal(h, ch, ts) }

// NewReminderDueModal is the same dialog for an item that's already
// saved (RemindMenu's "Custom…" on an Activity reminder): save gets the
// new due time.
func NewReminderDueModal(h Host, save func(dueAt int64)) *Modal { return actReminderModal(h, save) }

// MoveChannelItems, when set (internal/ui), are the Move to… menu's rows
// for a channel and whether there is a custom section (Slack's item menu
// shows "Move channel" ▸ those rows with one, else "Move to new
// section"); CreateSectionWith opens "Create a section" for the channel.
var (
	MoveChannelItems  func(ch string) (items []MenuItem, custom bool)
	CreateSectionWith func(ch string)
)

// MessageLink is a message's link, as the message menu's Copy link builds
// it (the message's permalink when Slack sent one).
func MessageLink(h Host, ch, ts, threadTS, perma string) string {
	if perma != "" {
		return perma
	}
	return permalink(h, ch, ts, threadTS)
}

// ActDo runs a write off the UI goroutine and toasts ok (or the error),
// as the menus' actions do.
func ActDo(h Host, ok string, f func(ctx context.Context, a *core.App) error) { actDo(h, ok, f) }

// ModalBodyPoint turns a point in m's body into window coordinates: the
// emoji picker's anchor under a body's emoji button (the Activity view's
// "Create a new view" modal, as the section modals anchor theirs).
func ModalBodyPoint(gtx layout.Context, m *Modal, p image.Point) image.Point {
	return dtModalOrigin(gtx, m).Add(p)
}
