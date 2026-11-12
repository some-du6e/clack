package huddleui

import "image/color"

// captionLook is what a huddle window's native Windows caption shows
// (caption_windows.go): the window's own backdrop and text, in dark mode
// when the window is dark. Slack's huddle and invite windows draw their
// own title bars on the window's colour (.p-huddle_window_titlebar--windows,
// HuddleInviteWindow's), so Clack's native caption takes those colours
// instead of the user's accent colour (Karim, α.37: a bright blue bar
// over the huddle window). Like the main window's caption (ui
// caption_windows.go, branch signin-flat) it is cosmetic and best effort.
type captionLook struct {
	dark     bool
	bg, text color.NRGBA
}

// hudCaption is the huddle window's (and the screen share popout's): the
// window's frame colour with the title's light text.
var hudCaption = captionLook{dark: true, bg: hudFrame, text: color.NRGBA{R: 0xe8, G: 0xe8, B: 0xe8, A: 0xff}}

// inviteCaption is the invite window's: its backdrop and text per mode.
func inviteCaption(k invInk, dark bool) captionLook {
	return captionLook{dark: dark, bg: k.bg, text: k.text}
}
