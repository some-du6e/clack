// Package usernotifyprobe asks macOS's UNUserNotificationCenter what it
// says to the process it runs in: the bundle and signature macOS sees,
// whether the center exists at all, the notification settings, and the
// answer to an authorization request. ci.yml's macos job builds its test
// into Clack.app bundles the way release.yml does (the bundle id, an
// ad-hoc signature) and runs it, to settle whether an ad-hoc signed
// Clack can get notification authorization (docs/GAPS-2026-11-14.md gap 1,
// W33). Nothing in Clack imports it.
package usernotifyprobe

// build tells a rebuilt probe from the first (-ldflags -X): a new binary
// has a new ad-hoc signature (cdhash), as each Clack update does.
var build = "1"

// UNAuthorizationOptions bits.
const (
	OptBadge       = 1 << 0
	OptSound       = 1 << 1
	OptAlert       = 1 << 2
	OptProvisional = 1 << 6
)
