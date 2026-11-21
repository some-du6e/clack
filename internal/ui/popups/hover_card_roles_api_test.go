package popups

import "github.com/some-du6e/clack/internal/core"

// The role tests' reach into the fix's API (red-evidence commits replace
// this file with the base's equivalents).

// hrDND is whether core marked u's notifications paused.
func hrDND(u core.User) bool { return u.DND }

// hrPresenceIcon is the SvgIcon the card's presence draws for u.
func hrPresenceIcon(u core.User, active bool) string { return pfPresenceIconName(u, active) }
