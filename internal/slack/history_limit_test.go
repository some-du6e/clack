package slack

import "testing"

// DefaultHistoryLimit is Slack's determineDefaultHistoryLimit: 3 for two of
// Slack's own conversations, 28 for the rest.
func TestDefaultHistoryLimit(t *testing.T) {
	for ch, want := range map[string]int{"C105M7SCQ": 3, "G01A79C5B6E": 3, "C0FAKE00001": 28, "D0FAKE00001": 28, "": 28} {
		if got := DefaultHistoryLimit(ch); got != want {
			t.Errorf("DefaultHistoryLimit(%q) = %d, want %d", ch, got, want)
		}
	}
}
