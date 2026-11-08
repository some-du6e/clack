package openurl

import (
	"runtime"
	"testing"
)

func TestCheckSettings(t *testing.T) {
	mac := "x-apple.systempreferences:com.apple.preference.security?Privacy_Camera"
	win := "ms-settings:privacy-webcam"
	bad := []string{"", "https://example.com", "x-apple.systempreferences:com.apple.preference.security?Privacy_",
		mac + "&x=1", mac + " ", "ms-settings:display", win + ";calc", "file:///etc/passwd", "-a Calculator"}
	for _, u := range bad {
		if _, err := CheckSettings(u); err == nil {
			t.Errorf("CheckSettings(%q) accepted", u)
		}
	}
	_, macErr := CheckSettings(mac)
	_, winErr := CheckSettings(win)
	switch runtime.GOOS {
	case "darwin":
		if macErr != nil || winErr == nil {
			t.Errorf("darwin: mac %v, windows %v", macErr, winErr)
		}
	case "windows":
		if winErr != nil || macErr == nil {
			t.Errorf("windows: mac %v, windows %v", macErr, winErr)
		}
	default:
		if macErr == nil || winErr == nil {
			t.Errorf("%s accepts a settings link", runtime.GOOS)
		}
	}
}
