package composer

import "testing"

// NotificationBar's renderFormattingTips (SyrM): Enter on Windows, Return
// on macOS and Linux (+oeN uF isWindows), Shift only while Enter sends.
func TestHintSourceLikeSlack(t *testing.T) {
	cases := []struct {
		goos                      string
		newlinePref, preNL, inPre bool
		want                      string
	}{
		{"windows", false, false, false, "<b>Shift + Enter</b> to add a new line"},
		{"darwin", false, false, false, "<b>Shift + Return</b> to add a new line"},
		{"linux", false, false, false, "<b>Shift + Return</b> to add a new line"},
		{"windows", true, false, false, "<b>Enter</b> to add a new line"},
		{"darwin", true, false, false, "<b>Return</b> to add a new line"},
		{"linux", true, false, false, "<b>Return</b> to add a new line"},
		{"windows", false, true, true, "<b>Enter</b> to add a new line"},
		{"linux", false, true, true, "<b>Return</b> to add a new line"},
		{"windows", false, true, false, "<b>Shift + Enter</b> to add a new line"}, // not in ``` code
		{"windows", false, false, true, "<b>Shift + Enter</b> to add a new line"}, // the pref is off
	}
	for _, c := range cases {
		if got := hintSource(c.goos, c.newlinePref, c.preNL, c.inPre); got != c.want {
			t.Errorf("hintSource(%s, newline %v, pre %v/%v) = %q, want %q", c.goos, c.newlinePref, c.preNL, c.inPre, got, c.want)
		}
	}
}
