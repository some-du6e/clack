//go:build !(darwin && cgo)

package desktop

// UserDefault is Electron's systemPreferences.getUserDefault, which only
// macOS has: nil here.
func UserDefault(key, typ string) any { return nil }
