package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Time zones, as Slack's client handles them (boot-shared sbu9, CDN build
// a84662d2d135):
//
//   - Language & region's select lists SlackTimezones by label; picking one
//     writes the tz pref (users.prefs.set). Slack's servers use it for
//     summary and notification emails, activity times and reminders.
//   - With set_tz_automatically on, maybeUpdateTimezone (boot-shared C_)
//     writes the detected zone to tz whenever they differ; the detector
//     (TimezoneDetector.detect) takes the system zone if Slack lists it,
//     else maps it through intl_api_manual, intl_api_equivalent and
//     intl_api_fallback. Turning the box on writes the zone at once.

// TimezoneLabel is the select's label for an IANA id ("" if Slack doesn't
// list it).
func TimezoneLabel(id string) string {
	for _, z := range SlackTimezones {
		if z.ID == id {
			return z.Label
		}
	}
	return ""
}

// DefaultTZ is Slack's DEFAULT_TZ (the select's fallback and the
// detector's "gave_up" answer).
const DefaultTZ = "America/Los_Angeles"

// canonicalZone is detect's lookup for a system zone ("" when none).
func canonicalZone(z string) string {
	if z == "" {
		return ""
	}
	if TimezoneLabel(z) != "" {
		return z
	}
	for _, m := range []map[string]string{tzManual, tzEquivalent, tzFallback} {
		if v, ok := m[z]; ok {
			return v
		}
	}
	return ""
}

// systemZone is the OS's IANA zone: $TZ, else /etc/localtime's target
// (Linux, macOS). Windows has no IANA name to read: "". Tests get
// TestSystemZone (never the machine's).
var systemZone = func() string {
	if testing.Testing() {
		return TestSystemZone
	}
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" {
		return tz
	}
	p, err := filepath.EvalSymlinks("/etc/localtime")
	if err != nil {
		return ""
	}
	if i := strings.Index(p, "zoneinfo/"); i >= 0 {
		return p[i+len("zoneinfo/"):]
	}
	return ""
}

// TestSystemZone is the zone systemZone reports under go test.
var TestSystemZone string

// DetectTimezone is the detected zone in Slack's list ("" when unknown).
func DetectTimezone() string { return canonicalZone(systemZone()) }

// MaybeUpdateTimezone is maybeUpdateTimezone: with set_tz_automatically
// on, write the detected zone to tz when it differs. It returns the zone
// written ("" for none).
func (a *App) MaybeUpdateTimezone(ctx context.Context) (string, error) {
	if !a.PrefBool("set_tz_automatically", false) {
		return "", nil
	}
	z := DetectTimezone()
	if z == "" || z == a.PrefString("tz") {
		return "", nil
	}
	return z, a.SetPref(ctx, "tz", z)
}
