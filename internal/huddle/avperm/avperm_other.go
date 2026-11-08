//go:build !(darwin && cgo)

package avperm

import "runtime"

// No OS prompt to ask for: capture reports its own failures.
func check(Media) Status { return Authorized }

func request(m Media, ch chan Status) { ch <- Authorized }

func settingsURL(m Media) string {
	switch runtime.GOOS {
	case "windows":
		if m == Camera {
			return "ms-settings:privacy-webcam"
		}
		return "ms-settings:privacy-microphone"
	case "darwin": // a cgo-less build can't ask, but the page still exists
		if m == Camera {
			return "x-apple.systempreferences:com.apple.preference.security?Privacy_Camera"
		}
		return "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone"
	}
	return ""
}
