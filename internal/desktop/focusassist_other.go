//go:build !windows

package desktop

// FocusAssist is Windows' Focus Assist mode: unsupported here.
func FocusAssist() int { return FocusAssistNotSupported }

// FocusAssistPriority is Windows-only.
func FocusAssistPriority(appUserModelID string) int { return FocusAssistNotSupported }

// FocusAssistActive is Windows' Focus Assist check: never here.
func FocusAssistActive() bool { return false }
