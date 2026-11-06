package popups

import (
	"testing"
	"time"
)

var actTestLoc = time.FixedZone("test", -5*3600)

func actAt(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, actTestLoc)
}

func TestActReminderDue(t *testing.T) {
	// 2026-11-06 is a Friday.
	now := actAt(2026, time.November, 6, 14, 10)
	cases := []struct {
		kind int
		want time.Time
	}{
		{actRem30m, actAt(2026, time.November, 6, 14, 40)},
		{actRem1h, actAt(2026, time.November, 6, 15, 10)},
		{actRem3h, actAt(2026, time.November, 6, 17, 10)},
		{actRemTomorrow, actAt(2026, time.November, 7, 9, 0)},
		{actRemMonday, actAt(2026, time.November, 9, 9, 0)},
	}
	for _, c := range cases {
		if got := actReminderDue(now, c.kind); !got.Equal(c.want) {
			t.Errorf("kind %d: got %v want %v", c.kind, got, c.want)
		}
	}
}

func TestActNextMonday9(t *testing.T) {
	cases := []struct{ now, want time.Time }{
		{actAt(2026, time.November, 9, 8, 0), actAt(2026, time.November, 16, 9, 0)}, // Monday morning -> next week
		{actAt(2026, time.November, 8, 23, 0), actAt(2026, time.November, 9, 9, 0)}, // Sunday
		{actAt(2026, time.November, 7, 12, 0), actAt(2026, time.November, 9, 9, 0)}, // Saturday
		{actAt(2026, time.December, 29, 12, 0), actAt(2027, time.January, 4, 9, 0)}, // across the year
	}
	for _, c := range cases {
		if got := actNextMonday9(c.now); !got.Equal(c.want) {
			t.Errorf("%v: got %v want %v", c.now, got, c.want)
		}
	}
	if got, want := actTomorrow9(actAt(2026, time.November, 30, 22, 0)), actAt(2026, time.December, 1, 9, 0); !got.Equal(want) {
		t.Errorf("tomorrow: got %v want %v", got, want)
	}
}

func TestActMinutesUntil(t *testing.T) {
	now := actAt(2026, time.November, 6, 14, 10)
	if got := actMinutesUntil(now, actTomorrow9(now)); got != 18*60+50 {
		t.Errorf("until tomorrow: got %d", got)
	}
	if got := actMinutesUntil(now, actNextMonday9(now)); got != (2*24+18)*60+50 {
		t.Errorf("until next week: got %d", got)
	}
	if got := actMinutesUntil(now, now.Add(90*time.Second)); got != 2 {
		t.Errorf("rounds up: got %d", got)
	}
	if got := actMinutesUntil(now, now.Add(-time.Hour)); got != 1 {
		t.Errorf("past clamps to 1: got %d", got)
	}
}

func TestActStatusExpiry(t *testing.T) {
	now := actAt(2026, time.November, 6, 14, 10) // Friday
	eod := time.Date(2026, time.November, 6, 23, 59, 59, 0, actTestLoc).Unix()
	sunday := time.Date(2026, time.November, 8, 23, 59, 59, 0, actTestLoc).Unix()
	cases := []struct {
		d    actDur
		want int64
	}{
		{actDurNone, 0},
		{actDur30m, now.Add(30 * time.Minute).Unix()},
		{actDur1h, now.Add(time.Hour).Unix()},
		{actDur4h, now.Add(4 * time.Hour).Unix()},
		{actDurToday, eod},
		{actDurWeek, sunday},
		{actDurKeep, 12345},
	}
	for _, c := range cases {
		if got := actStatusExpiry(now, c.d, 12345); got != c.want {
			t.Errorf("dur %d: got %d want %d", c.d, got, c.want)
		}
	}
	// On Sunday "This week" ends tonight.
	sun := actAt(2026, time.November, 8, 10, 0)
	if got := actStatusExpiry(sun, actDurWeek, 0); got != sunday {
		t.Errorf("sunday week: got %d want %d", got, sunday)
	}
}

func TestActFuzzy(t *testing.T) {
	order := []string{"general", "general-chat", "my general stuff", "the-general", "gen-eral"}
	q := "general"
	prev := 1 << 30
	for _, s := range order {
		sc := actFuzzy(s, q)
		if sc < 0 {
			t.Fatalf("%q should match %q", s, q)
		}
		if sc > prev {
			t.Errorf("%q scored %d, above the previous %d", s, sc, prev)
		}
		prev = sc
	}
	if actFuzzy("random", q) >= 0 {
		t.Error("random should not match general")
	}
	if actFuzzy("anything", "") >= 0 {
		t.Error("empty query matches nothing")
	}
	if a, b := actFuzzy("hack-club", "club"), actFuzzy("hackclub", "club"); a <= b {
		t.Errorf("word prefix (%d) should beat substring (%d)", a, b)
	}
}

func TestActKeepLabel(t *testing.T) {
	now := actAt(2026, time.November, 6, 14, 10)
	if got := actKeepLabel(actAt(2026, time.November, 6, 17, 0).Unix(), now); got != "Until 5:00 PM" {
		t.Errorf("same day: %q", got)
	}
	if got := actKeepLabel(actAt(2026, time.November, 8, 9, 30).Unix(), now); got != "Until Nov 8th at 9:30 AM" {
		t.Errorf("other day: %q", got)
	}
}
