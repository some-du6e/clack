//go:build !windows

package desktop

// FocusAssist is Windows' Focus Assist mode: unsupported here.
func FocusAssist() int { return FocusAssistNotSupported }

// FocusAssistPriority is Windows-only.
func FocusAssistPriority(appUserModelID string) int { return FocusAssistNotSupported }

// FocusAssistActive is Windows' Focus Assist check: never here.
func FocusAssistActive() bool { return false }

// NotificationState is the shell's notification state: unknown here.
func NotificationState() int { return NotifyStateUnknown }

// ToastSwitches is Windows' notifications switches: none here.
func ToastSwitches(aumid string) (all, app int, values map[string]uint32) {
	return ToastSwitchUnset, ToastSwitchUnset, nil
}
