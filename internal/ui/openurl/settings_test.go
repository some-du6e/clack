package openurl

import "testing"

func TestOpenSettingsRefusesOthers(t *testing.T) {
	for _, u := range []string{"", "x-apple.systempreferences:", "ms-settings:", "x-apple.systempreferences:com.apple.preference.security?Privacy_Microphone&x",
		"file:///etc/passwd", "https://example.com", "ms-settings:privacy-microphone "} {
		if err := OpenSettings(u); err != ErrRefused {
			t.Errorf("%q: %v", u, err)
		}
	}
}
