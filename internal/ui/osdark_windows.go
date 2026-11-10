//go:build windows

package ui

import (
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// osPrefersDark reads Windows' app mode (AppsUseLightTheme 0 = dark). A
// registry read is quick, but it's still I/O: watcher goroutine only.
func osPrefersDark() (dark, ok bool) {
	uiguard.OffUI("osPrefersDark")
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

func startOSSchemeWatcher() {
	go func() {
		for {
			if dark, ok := osPrefersDark(); ok {
				setOSScheme(dark)
			}
			time.Sleep(5 * time.Second)
		}
	}()
}
