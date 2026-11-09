package popups

import "gioui.org/layout"

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
