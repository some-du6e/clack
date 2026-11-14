// Package icon embeds Clack's app icon (see clack.svg; the PNG, .ico and
// .icns files next to it are rendered from it) for window and Dock icons,
// and the Windows notification-area icon's states (build.py draws them).
package icon

import _ "embed"

// PNGs of the app icon, by pixel size.
var (
	//go:embed clack-32.png
	PNG32 []byte
	//go:embed clack-64.png
	PNG64 []byte
	//go:embed clack-128.png
	PNG128 []byte
	//go:embed clack-512.png
	PNG512 []byte
)

// The notification-area icon's .ico files, one per state of Slack's tray
// (slack-taskbar-rest, -unread and -highlight.ico): no unreads, unread
// messages, mentions.
var (
	//go:embed clack-tray-rest.ico
	TrayRest []byte
	//go:embed clack-tray-unread.ico
	TrayUnread []byte
	//go:embed clack-tray-highlight.ico
	TrayHighlight []byte
)
