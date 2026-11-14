package desktop

// The Focus Assist modes: windows-focus-assist's getFocusAssist values
// (focusassist_windows.go). Off Windows it's always unsupported.
const (
	FocusAssistNotSupported = -2
	FocusAssistFailed       = -1
	FocusAssistOff          = 0
	FocusAssistPriorityOnly = 1
	FocusAssistAlarmsOnly   = 2
)

// focusAssistOn is Slack's a9 (main.bundle.cjs 4.53.276 win32 @924665):
// priority-only mutes unless the app has priority, alarms-only always,
// anything else (off, failed, unsupported) never.
func focusAssistOn(mode int, priority bool) bool {
	switch mode {
	case FocusAssistPriorityOnly:
		return !priority
	case FocusAssistAlarmsOnly:
		return true
	}
	return false
}
