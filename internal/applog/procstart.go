package applog

import "time"

// ProcessStart is when the OS started this process: the exec, before the
// loader, the Go runtime and every package's init ran. ok is false where
// the OS doesn't say (then a start counts from main, as before).
//
// The startup timeline counts from it (cmd/clack), so its milestones are
// what the user waits for after a click: macOS's Dock bounces from the
// launch until the app finishes launching, whatever ran before main.
func ProcessStart() (start time.Time, ok bool) {
	t, ok := processStart()
	if !ok || t.IsZero() || t.After(time.Now()) {
		return time.Time{}, false
	}
	return t, true
}
