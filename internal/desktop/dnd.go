package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Do Not Disturb as Slack's macOS shell sees it before a Dock bounce
// (SLACK_BOUNCE_DOCK in main.bundle.cjs 4.53.276 asks
// macos-notification-state 2.0.2's getDoNotDisturb). From macOS 12 that
// is MontereyDND (lib/dnd/monterey-macos-dnd.mm), which reads the Focus
// database under ~/Library/DoNotDisturb/DB:
//
//  1. Assertions.json: any data[0].storeAssertionRecords means a Focus
//     (or DND) is on;
//  2. else ModeConfigurations.json: a trigger of the active mode (the
//     first assertion's assertionDetailsModeIdentifier, "NONE" without
//     one) with enabledSetting 2 whose start..end covers the time
//     (start hour*60+minute inclusive, end hour*60 exclusive, wrapping
//     past midnight);
//  3. on, unless ModeConfigurationsSecure.json lists the app's bundle id
//     (or a key the bundle id contains) in the mode's allowedApplications.
//
// A file that can't be read or parsed counts as off, as there.

// montereyDND is MontereyDND isEnabled for the user's home and bundle id.
func montereyDND(home, bundleID string, now time.Time) bool {
	on := dndByAssertion(home)
	if !on {
		on = dndBySchedule(home, now)
	}
	if on {
		return !dndAllowed(home, bundleID)
	}
	return false
}

func dndJSON(home, name string, v any) bool {
	b, err := os.ReadFile(filepath.Join(home, "Library", "DoNotDisturb", "DB", name))
	return err == nil && json.Unmarshal(b, v) == nil
}

type dndAssertions struct {
	Data []struct {
		Records []struct {
			Details struct {
				Mode string `json:"assertionDetailsModeIdentifier"`
			} `json:"assertionDetails"`
		} `json:"storeAssertionRecords"`
	} `json:"data"`
}

func dndByAssertion(home string) bool {
	var a dndAssertions
	return dndJSON(home, "Assertions.json", &a) && len(a.Data) > 0 && len(a.Data[0].Records) > 0
}

// dndActiveMode is getActiveFocusMode.
func dndActiveMode(home string) string {
	var a dndAssertions
	if !dndJSON(home, "Assertions.json", &a) || len(a.Data) == 0 || len(a.Data[0].Records) == 0 {
		return "NONE"
	}
	return a.Data[0].Records[0].Details.Mode
}

func dndBySchedule(home string, now time.Time) bool {
	var c struct {
		Data []struct {
			Modes map[string]struct {
				Triggers struct {
					Triggers []struct {
						Enabled     int  `json:"enabledSetting"`
						StartHour   *int `json:"timePeriodStartTimeHour"`
						EndHour     *int `json:"timePeriodEndTimeHour"`
						StartMinute int  `json:"timePeriodStartTimeMinute"`
					} `json:"triggers"`
				} `json:"triggers"`
			} `json:"modeConfigurations"`
		} `json:"data"`
	}
	if !dndJSON(home, "ModeConfigurations.json", &c) || len(c.Data) == 0 {
		return false
	}
	cur := now.Hour()*60 + now.Minute()
	for _, t := range c.Data[0].Modes[dndActiveMode(home)].Triggers.Triggers {
		if t.Enabled != 2 || t.StartHour == nil || t.EndHour == nil {
			continue
		}
		start, end := *t.StartHour*60+t.StartMinute, *t.EndHour*60
		switch {
		case start < end && cur >= start && cur < end:
			return true
		case end < start && (cur >= start || cur < end):
			return true
		}
	}
	return false
}

// dndAllowed is allowedForBundleId: the active mode lets the app through.
func dndAllowed(home, bundleID string) bool {
	if bundleID == "" {
		return false
	}
	var s struct {
		Data []struct {
			Modes map[string]struct {
				Secure struct {
					Allowed map[string]json.RawMessage `json:"allowedApplications"`
				} `json:"secureConfiguration"`
			} `json:"secureModeConfigurations"`
		} `json:"data"`
	}
	if !dndJSON(home, "ModeConfigurationsSecure.json", &s) || len(s.Data) == 0 {
		return false
	}
	allowed := s.Data[0].Modes[dndActiveMode(home)].Secure.Allowed
	if _, ok := allowed[bundleID]; ok {
		return true
	}
	for k := range allowed { // "%@ CONTAINS SELF": a key inside the bundle id
		if strings.Contains(bundleID, k) {
			return true
		}
	}
	return false
}
