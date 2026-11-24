package i18n

import (
	"testing"
	"time"
)

// FileDetails' "Created …" line (eg5B renderReferencesMetadata) reads
// toCalendarDateOrNamedDay with uncapitalizedRelativeDay (fUG7: GuNR's
// named day, else toCalendarDate with Nmg4 shouldExcludeYear) and
// toCalendarDateIfYesterdayOrToday (47p5) to pick "Created {date}" or
// "Created on {date}".
func TestCalendarDateOrNamedDay(t *testing.T) {
	now := time.Date(2026, 11, 24, 15, 0, 0, 0, time.UTC)
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 9, 30, 0, 0, time.UTC) }
	cases := []struct {
		name  string
		t     time.Time
		o     NamedDayOpts
		want  string
		named bool // IsTodayOrYesterday
	}{
		{"today", day(2026, 11, 24), NamedDayOpts{UncapitalizedRelativeDay: true}, "today", true},
		{"yesterday", day(2026, 11, 23), NamedDayOpts{UncapitalizedRelativeDay: true}, "yesterday", true},
		{"tomorrow is named but isn't today or yesterday", day(2026, 11, 25), NamedDayOpts{UncapitalizedRelativeDay: true}, "tomorrow", false},
		{"capitalized without the option", day(2026, 11, 24), NamedDayOpts{}, "Today", true},
		{"this year: no year", day(2026, 11, 3), NamedDayOpts{UncapitalizedRelativeDay: true}, "November 3rd", false},
		{"this year's first day", day(2026, 1, 1), NamedDayOpts{}, "January 1st", false},
		{"last year, within 31 days: no year", time.Date(2025, 12, 31, 9, 0, 0, 0, time.UTC), NamedDayOpts{},
			"December 31st", false},
		{"last year, past 31 days: the year", day(2025, 11, 3), NamedDayOpts{}, "November 3rd, 2025", false},
		{"a later year keeps it", day(2027, 2, 2), NamedDayOpts{}, "February 2nd, 2027", false},
		{"excludeYear asked", day(2024, 5, 6), NamedDayOpts{ExcludeYear: true}, "May 6th", false},
		{"shortened month", day(2025, 3, 4), NamedDayOpts{ShortenMonth: true}, "Mar 4th, 2025", false},
	}
	for _, c := range cases {
		if got := CalendarDateOrNamedDay(EnUS, c.t, now, c.o); got != c.want {
			t.Errorf("%s: CalendarDateOrNamedDay = %q, want %q", c.name, got, c.want)
		}
		if got := IsTodayOrYesterday(c.t, now); got != c.named {
			t.Errorf("%s: IsTodayOrYesterday = %v, want %v", c.name, got, c.named)
		}
	}
	// Nmg4 counts seconds: 31 days back across the new year is the edge
	jan := time.Date(2027, 1, 15, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		t    time.Time
		want bool
	}{
		{jan.Add(-31 * 24 * time.Hour), true},
		{jan.Add(-31*24*time.Hour - time.Second), false},
		{time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), true},
		{time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC), false},
	} {
		if got := ShouldExcludeYear(c.t, jan); got != c.want {
			t.Errorf("ShouldExcludeYear(%v, %v) = %v, want %v", c.t, jan, got, c.want)
		}
	}
}
