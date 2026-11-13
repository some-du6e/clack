package desktop

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// macos-notification-state 2.0.2's MontereyDND (what Slack's shell asks
// before bouncing the Dock icon) on synthetic Focus databases.
func TestMontereyDND(t *testing.T) {
	write := func(t *testing.T, home string, files map[string]string) {
		t.Helper()
		dir := filepath.Join(home, "Library", "DoNotDisturb", "DB")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for n, s := range files {
			if err := os.WriteFile(filepath.Join(dir, n), []byte(s), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	const on = `{"data":[{"storeAssertionRecords":[{"assertionDetails":{"assertionDetailsModeIdentifier":"com.apple.focus.work"}}]}]}`
	const off = `{"data":[{"storeAssertionRecords":[]}]}`
	sched := func(start, startMin, end int) string {
		return `{"data":[{"modeConfigurations":{"NONE":{"triggers":{"triggers":[{"enabledSetting":2,"timePeriodStartTimeHour":` +
			strconv.Itoa(start) + `,"timePeriodStartTimeMinute":` + strconv.Itoa(startMin) + `,"timePeriodEndTimeHour":` + strconv.Itoa(end) + `}]}}}}]}`
	}
	at := func(h, m int) time.Time { return time.Date(2026, 11, 13, h, m, 0, 0, time.Local) }
	for _, c := range []struct {
		name  string
		files map[string]string
		bid   string
		now   time.Time
		want  bool
	}{
		{"no database (unreadable: off)", nil, "", at(12, 0), false},
		{"a Focus on", map[string]string{"Assertions.json": on}, "", at(12, 0), true},
		{"no assertion", map[string]string{"Assertions.json": off}, "", at(12, 0), false},
		{"broken JSON", map[string]string{"Assertions.json": "{"}, "", at(12, 0), false},
		{"scheduled, inside", map[string]string{"Assertions.json": off, "ModeConfigurations.json": sched(9, 30, 17)}, "", at(16, 59), true},
		{"scheduled, start inclusive", map[string]string{"Assertions.json": off, "ModeConfigurations.json": sched(9, 30, 17)}, "", at(9, 30), true},
		{"scheduled, end exclusive", map[string]string{"Assertions.json": off, "ModeConfigurations.json": sched(9, 30, 17)}, "", at(17, 0), false},
		{"scheduled over midnight", map[string]string{"Assertions.json": off, "ModeConfigurations.json": sched(22, 0, 7)}, "", at(3, 0), true},
		{"scheduled over midnight, daytime", map[string]string{"Assertions.json": off, "ModeConfigurations.json": sched(22, 0, 7)}, "", at(12, 0), false},
		{"Focus allows the app", map[string]string{"Assertions.json": on, "ModeConfigurationsSecure.json": `{"data":[{"secureModeConfigurations":{"com.apple.focus.work":{"secureConfiguration":{"allowedApplications":{"com.example.clack":{}}}}}}]}`}, "com.example.clack", at(12, 0), false},
		{"Focus allows a parent id", map[string]string{"Assertions.json": on, "ModeConfigurationsSecure.json": `{"data":[{"secureModeConfigurations":{"com.apple.focus.work":{"secureConfiguration":{"allowedApplications":{"com.example":{}}}}}}]}`}, "com.example.clack", at(12, 0), false},
		{"Focus allows another app", map[string]string{"Assertions.json": on, "ModeConfigurationsSecure.json": `{"data":[{"secureModeConfigurations":{"com.apple.focus.work":{"secureConfiguration":{"allowedApplications":{"com.other.app":{}}}}}}]}`}, "com.example.clack", at(12, 0), true},
	} {
		home := t.TempDir()
		if c.files != nil {
			write(t, home, c.files)
		}
		if got := montereyDND(home, c.bid, c.now); got != c.want {
			t.Errorf("%s: DND %v, want %v", c.name, got, c.want)
		}
	}
}
