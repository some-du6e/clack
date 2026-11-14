//go:build darwin

package ui

import "gioui.org/app"

// The macOS main window is frameless like Slack's (gap 4, W35). Slack's
// mac main.bundle.cjs 4.53.276 creates it with frame false and
// titleBarStyle "hidden" (@1145075): the page runs under the title bar,
// which shows no title, and the traffic lights stay, at the
// trafficLightPosition setting's x and half its y ({x: t, y:
// Math.floor(r/2)} of sg(setting), sg clamping each to 0..40). The setting
// is {x: 14, y: 22} until the web client's top nav mounts and
// injectCustomTitlebarStyling writes {x: 12, y: 24} (boot-render, a
// desktop app on macOS), so every start after the first opens with the
// buttons at {12, 12}: the close button 12 points from the left and the
// buttons' tops 12 from the top, centred in the 40 px top nav. Gio patch
// 23 (app.HiddenTitleBar, app.TrafficLightPosition) does the same to
// Clack's window. The nav is the drag area (-webkit-app-region: drag, its
// widgets no-drag, titlebar.go's wcoNavAreas), and a double-click on it
// does what the system's "Double-click a window's title bar to" setting
// says (the patch).
//
// Closing the window hides it (runfromtray.go): Slack's runFromTray
// defaults to true on macOS too, and MAIN_WINDOW_CLOSE hides the window;
// the Dock icon and the app's activation bring it back
// (window_darwin.m).

// macTrafficLightX and macTrafficLightY are Slack's main window's
// traffic-light position: {12, floor(24 / 2)}.
const macTrafficLightX, macTrafficLightY = 12, 12

// mainMinW and mainMinH are Slack's main window minimum outside Linux
// (main.bundle.cjs @1145075: d = linux ? 300 : 400, f = linux ? 300 :
// 668).
const mainMinW, mainMinH = 668, 400

// mainWindowChrome is the main window's options for its frame.
func mainWindowChrome() []app.Option {
	return []app.Option{app.HiddenTitleBar(true), app.TrafficLightPosition(macTrafficLightX, macTrafficLightY)}
}

func init() {
	// MAIN_WINDOW_CLOSE hides the window with no tray icon: the Dock's
	// icon brings it back. hx (@1150977) leaves full screen before it
	// hides; Gio patch 23's Hide does that itself.
	closeHidesWithoutTray = true
	hideLeavesFullscreen = true
	macTitlebarOn = true
}
