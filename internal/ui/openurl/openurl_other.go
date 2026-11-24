//go:build !windows && !(darwin && cgo)

package openurl

// shellOpen is Windows' (openurl_windows.go) and macOS's
// (openurl_darwin.go). Elsewhere a link goes to xdg-open / xdg-email
// (command).
func shellOpen(string) error { return errNoShell }

// retryWithTool: there is only the tool here.
const retryWithTool = true
