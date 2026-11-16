package main

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slick"
)

// Snappy's default-on "Disable crash reporter" writes Chromium's
// disable-crashpad and disable-crash-reporter for the next start. They
// once turned Clack's own crash.log off (no crash evidence from 11-13 to
// 11-15); crash capture must stay on whatever the switches say.
func TestCrashCaptureIgnoresCrashpadSwitches(t *testing.T) {
	for _, tc := range []struct {
		name string
		sw   []string
		note bool
	}{
		{"none", nil, false},
		{"gpu only", []string{slick.SwitchIgnoreGPUBlocklist}, false},
		{"snappy default", []string{slick.SwitchDisableCrashReporter, slick.SwitchDisableCrashpad, slick.SwitchIgnoreGPUBlocklist}, true},
		{"crashpad", []string{slick.SwitchDisableCrashpad}, true},
		{"crash reporter", []string{slick.SwitchDisableCrashReporter}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var headers []string
			note := captureCrashes(func(h string) { headers = append(headers, h) }, tc.sw, "clack test pid 1")
			if len(headers) != 1 || headers[0] != "clack test pid 1" {
				t.Fatalf("crash capture with switches %v: CaptureCrashes calls %q, want one with the header", tc.sw, headers)
			}
			if got := note != ""; got != tc.note {
				t.Fatalf("switches %v: note %q, want a note: %t", tc.sw, note, tc.note)
			}
			if tc.note && !strings.Contains(note, "crash.log stays on") {
				t.Errorf("note %q doesn't say crash.log stays on", note)
			}
		})
	}
}
