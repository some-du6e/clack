//go:build !(darwin && cgo) && !windows && !(linux && cgo)

package huddle

import "image"

// No screen capture backend in this build (no cgo on macOS or Linux):
// shareOS stays nil, so ShareSupported is false and the share button is
// greyed (CLACK_HUDDLE_TESTSCREEN still works).

func screenAccess() bool   { return true }
func requestScreenAccess() {}

const (
	screenSettingsURL = ""
	borderSupported   = false
)

func showBorder(ShareSource)                  {}
func hideBorder()                             {}
func setupPanel(PanelHandle, image.Rectangle) {}
