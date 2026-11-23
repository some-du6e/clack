//go:build !windows

package openurl

// shellOpen is Windows' (openurl_windows.go). On macOS and Linux a link
// goes to open / xdg-open (command).
func shellOpen(string) error { return errNoShell }
