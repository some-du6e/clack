package views

import (
	"testing"
	"time"
)

// Slack's rK (0Rg1) for an Activity row, branch by branch, both variants
// (narrow: the Dense row; wide: a Detailed row over 285px).
func TestActivityTimeLikeSlack(t *testing.T) {
	now := time.Date(2026, time.November, 10, 15, 0, 0, 0, time.Local) // a Tuesday
	for _, c := range []struct {
		name         string
		at           time.Time
		wide, narrow string
	}{
		{"8 hours: the time", now.Add(-7 * time.Hour), clock(now.Add(-7 * time.Hour)), clock(now.Add(-7 * time.Hour))},
		{"yesterday", time.Date(2026, time.November, 9, 23, 0, 0, 0, time.Local), "Yesterday", "Yesterday"},
		{"under 8 hours, yesterday: the time", time.Date(2026, time.November, 9, 23, 30, 0, 0, time.Local), "", ""},
		{"the week: the weekday", now.Add(-6*24*time.Hour - 20*time.Hour), "Tuesday", "Tuesday"},
		{"a week ago on its weekday", now.Add(-7*24*time.Hour - 2*time.Hour), "Tuesday", "Tuesday"},
		{"a week and a day", now.Add(-8 * 24 * time.Hour), "November 2nd", "Nov 2nd"},
		{"the 11th", time.Date(2026, time.September, 11, 9, 0, 0, 0, time.Local), "September 11th", "Sep 11th"},
		{"the 23rd last year, within 365 days", time.Date(2025, time.December, 23, 9, 0, 0, 0, time.Local), "December 23rd", "Dec 23rd"},
		{"365 days on: the year", time.Date(2025, time.November, 9, 9, 0, 0, 0, time.Local), "November 9th, 2025", "Nov 9th, 2025"},
	} {
		now := now
		if c.wide == "" { // just past midnight: within 8 hours beats Yesterday
			now = time.Date(2026, time.November, 10, 2, 0, 0, 0, time.Local)
			c.wide, c.narrow = clock(c.at), clock(c.at)
		}
		if got := actWhen(now, c.at, false); got != c.wide {
			t.Errorf("%s: wide %q, want %q", c.name, got, c.wide)
		}
		if got := actWhen(now, c.at, true); got != c.narrow {
			t.Errorf("%s: narrow %q, want %q", c.name, got, c.narrow)
		}
	}
	if actWhen(now, time.Time{}, true) != "" {
		t.Error("no time: want nothing")
	}
	// within the hour: Math.round of the minutes
	for _, c := range []struct {
		age  time.Duration
		want string
	}{
		{30 * time.Second, "Just now"}, {-time.Minute, "Just now"}, {time.Minute, "1 min"},
		{89 * time.Second, "1 min"}, {90 * time.Second, "2 mins"}, {59*time.Minute + 40*time.Second, "60 mins"},
	} {
		got, left := actRel(c.age)
		if got != c.want || left <= 0 {
			t.Errorf("actRel(%v) = %q, %v; want %q", c.age, got, left, c.want)
		}
	}
}
