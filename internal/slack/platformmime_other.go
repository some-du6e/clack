//go:build !windows && !linux && !(darwin && cgo)

package slack

// osMimeType: this build asks no OS (Chromium asks one on every platform
// Slack desktop runs on; this is a build without that lookup, such as a
// darwin one without cgo, where FileType has only Chromium's lists).
func osMimeType(string) (string, bool) { return "", false }
