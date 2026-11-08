//go:build windows

package ui

import "golang.org/x/sys/windows/registry"

// osPrefersDark reads Windows' app mode (AppsUseLightTheme 0 = dark).
func osPrefersDark() (dark, ok bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false, false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return false, false
	}
	return v == 0, true
}
