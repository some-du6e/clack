package acrank

import "testing"

// D72a's semantic match: runs of up to four words against the keyword
// table, the earliest emoji in the table winning, "1:1" special-cased.
func TestStatusEmoji(t *testing.T) {
	cases := []struct{ text, want string }{
		{"In a meeting", "date"},
		{"Out of office until Monday", "no_entry"}, // "out of office" beats "office"
		{"working from home", "house"},
		{"1:1 with Bob", "date"},
		{"Lunch", "green_salad"},
		{"coffee break", "coffee"}, // coffee comes before double_vertical_bar
		{"All-Hands", "date"},      // "all" "hands" → "all hands"
		{"DEEP WORK, do not disturb", "dart"},
		{"Heads down on the release", "dart"},
		{"shipping the release", "rocket"},
		{"Nothing to see", ""},
		{"", ""},
		{"Happy New Year’s!", "clinking_glasses"},
	}
	for _, c := range cases {
		if got := StatusEmoji(c.text, nil); got != c.want {
			t.Errorf("StatusEmoji(%q) = %q, want %q", c.text, got, c.want)
		}
	}
	// keywords go through the I18n (current_status) when the experiment
	// is on: a translated keyword matches, the English one no longer does
	tr := func(s string) string {
		if s == "meeting" {
			return "réunion"
		}
		return s
	}
	if got := StatusEmoji("en réunion", tr); got != "date" {
		t.Errorf("translated keyword: %q", got)
	}
	if got := StatusEmoji("meeting", tr); got != "" {
		t.Errorf("untranslated keyword still matched: %q", got)
	}
	if n := len(statusEmojiTable); n != 77 {
		t.Errorf("%d emoji in the table, want D72a's 77", n)
	}
}
