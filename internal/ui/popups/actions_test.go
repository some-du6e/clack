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
	friday := eod // "This week" ends on Friday (getDurationPresetTs WEEK)
	cases := []struct {
		d    actDur
		want int64
	}{
		{actDurNone, 0},
		{actDur30m, now.Add(30 * time.Minute).Unix()},
		{actDur1h, now.Add(time.Hour).Unix()},
		{actDur4h, now.Add(4 * time.Hour).Unix()},
		{actDurToday, eod},
		{actDurWeek, friday},
		{actDurKeep, 12345},
	}
	for _, c := range cases {
		if got := actStatusExpiry(now, c.d, 12345); got != c.want {
			t.Errorf("dur %d: got %d want %d", c.d, got, c.want)
		}
	}
	// On Saturday and Sunday it is the coming Friday.
	nextFri := time.Date(2026, time.November, 13, 23, 59, 59, 0, actTestLoc).Unix()
	for _, d := range []int{7, 8} {
		if got := actStatusExpiry(actAt(2026, time.November, d, 10, 0), actDurWeek, 0); got != nextFri {
			t.Errorf("Nov %d week: got %v want %v", d, time.Unix(got, 0), time.Unix(nextFri, 0))
		}
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

func TestDtCeil(t *testing.T) {
	sec := func(hh, mm, ss int) time.Time { return time.Date(2026, time.November, 6, hh, mm, ss, 0, actTestLoc) }
	cases := []struct {
		in   time.Time
		step int
		want time.Time
	}{
		{sec(14, 57, 30), 1, actAt(2026, time.November, 6, 14, 58)},
		{sec(14, 58, 0), 1, actAt(2026, time.November, 6, 14, 58)},
		{sec(15, 12, 0), 30, actAt(2026, time.November, 6, 15, 30)},
		{sec(15, 30, 0), 30, actAt(2026, time.November, 6, 15, 30)},
		{sec(15, 30, 1), 30, actAt(2026, time.November, 6, 16, 0)},
		{sec(14, 58, 0), 60, actAt(2026, time.November, 6, 15, 0)},
		{sec(23, 50, 0), 30, actAt(2026, time.November, 7, 0, 0)},
	}
	for _, c := range cases {
		if got := dtCeil(c.in, c.step); !got.Equal(c.want) {
			t.Errorf("dtCeil(%v, %d) = %v, want %v", c.in, c.step, got, c.want)
		}
	}
	if got := dtAt(actAt(2026, time.November, 6, 0, 0), 25*60); !got.Equal(actAt(2026, time.November, 7, 1, 0)) {
		t.Errorf("dtAt overflow: %v", got)
	}
}

func TestActRangeDefaults(t *testing.T) {
	// getDefaultStartTime is startOf("minute"); the end is 24 h later,
	// moved up to the next whole hour unless on one.
	now := time.Date(2026, time.November, 6, 14, 57, 30, 0, actTestLoc)
	start, end := actRangeDefaults(now)
	if !start.Equal(actAt(2026, time.November, 6, 14, 57)) || !end.Equal(actAt(2026, time.November, 7, 15, 0)) {
		t.Errorf("2:57:30 PM: start %v end %v", start, end)
	}
	_, end = actRangeDefaults(actAt(2026, time.November, 6, 15, 12))
	if !end.Equal(actAt(2026, time.November, 7, 16, 0)) {
		t.Errorf("3:12 PM: end %v", end)
	}
	_, end = actRangeDefaults(actAt(2026, time.November, 6, 15, 0))
	if !end.Equal(actAt(2026, time.November, 7, 15, 0)) {
		t.Errorf("3:00 PM: end %v", end)
	}
	// a half-hour zone still lands on its own whole hour
	ist := time.FixedZone("IST", 5*3600+1800)
	_, end = actRangeDefaults(time.Date(2026, time.November, 6, 15, 12, 0, 0, ist))
	if want := time.Date(2026, time.November, 7, 16, 0, 0, 0, ist); !end.Equal(want) {
		t.Errorf("IST 3:12 PM: end %v want %v", end, want)
	}
}

func TestDtMonthGrid(t *testing.T) {
	nov := dtMonthGrid(actAt(2026, time.November, 17, 10, 0)) // Nov 1 2026 is a Sunday
	if nov[0] != 1 || nov[5] != 6 || nov[29] != 30 || nov[30] != 0 {
		t.Errorf("November: %v", nov)
	}
	dec := dtMonthGrid(actAt(2026, time.December, 1, 0, 0)) // Tuesday
	if dec[0] != 0 || dec[1] != 0 || dec[2] != 1 || dec[32] != 31 || dec[33] != 0 {
		t.Errorf("December: %v", dec)
	}
	may := dtMonthGrid(actAt(2026, time.May, 1, 0, 0)) // Friday, 31 days: needs the 6th row
	if may[5] != 1 || may[35] != 31 {
		t.Errorf("May: %v", may)
	}
	feb := dtMonthGrid(actAt(2028, time.February, 1, 0, 0)) // leap year
	n := 0
	for _, d := range feb {
		if d > n {
			n = d
		}
	}
	if n != 29 {
		t.Errorf("Feb 2028 has %d days", n)
	}
}

func TestDtTimeSlots(t *testing.T) {
	all := dtTimeSlots(0, 30)
	if len(all) != 48 || all[0] != 0 || all[47] != 23*60+30 {
		t.Errorf("whole day: %d slots %v…", len(all), all[:2])
	}
	s := dtTimeSlots(15*60+12, 30) // 3:12 PM → 3:30 PM first
	if len(s) != 17 || s[0] != 15*60+30 || s[1] != 16*60 {
		t.Errorf("from 3:12 PM: %v", s)
	}
	if s := dtTimeSlots(15*60+30, 30); s[0] != 15*60+30 {
		t.Errorf("on a slot: %v", s[0])
	}
	if s := dtTimeSlots(23*60+45, 30); len(s) != 0 {
		t.Errorf("after the last slot: %v", s)
	}
	if got := dtTimeLabel(15*60 + 30); got != "3:30 PM" {
		t.Errorf("label: %q", got)
	}
	if got := dtTimeLabel(0); got != "12:00 AM" {
		t.Errorf("midnight: %q", got)
	}
}

func TestDtDateLabel(t *testing.T) {
	now := actAt(2026, time.November, 6, 14, 10)
	cases := []struct {
		day   time.Time
		style dtStyle
		want  string
	}{
		{actAt(2026, time.November, 6, 0, 0), dtRelative, "Today"},
		{actAt(2026, time.November, 7, 0, 0), dtRelative, "Tomorrow"},
		{actAt(2026, time.November, 9, 0, 0), dtRelative, "Mon, Nov 9"},
		{actAt(2027, time.January, 1, 0, 0), dtRelative, "Fri, Jan 1, 2027"},
		{actAt(2026, time.November, 6, 0, 0), dtLong, "November 6th, 2026"},
		{actAt(2026, time.November, 22, 0, 0), dtLong, "November 22nd, 2026"},
	}
	for _, c := range cases {
		if got := dtDateLabel(c.day, now, c.style); got != c.want {
			t.Errorf("%v: got %q want %q", c.day, got, c.want)
		}
	}
}

func TestActPresetDur(t *testing.T) {
	now := actAt(2026, time.November, 6, 14, 10) // Friday
	eod := time.Date(2026, time.November, 6, 23, 59, 59, 0, actTestLoc).Unix()
	cases := []struct {
		code string
		dur  actDur
		exp  int64
	}{
		{"no_expiration", actDurNone, 0},
		{"all_day", actDurToday, eod},
		{"30_minutes", actDur30m, now.Add(30 * time.Minute).Unix()},
		{"1_hour", actDur1h, now.Add(time.Hour).Unix()},
		{"4_hours", actDur4h, now.Add(4 * time.Hour).Unix()},
		{"this_week", actDurWeek, eod},
		{"week", actDurWeek, eod},
		{"something_new", actDurNone, 0},
	}
	for _, c := range cases {
		d := actPresetDur(c.code)
		if d != c.dur {
			t.Errorf("%s: dur %d want %d", c.code, d, c.dur)
		}
		if got := actStatusExpiry(now, d, 0); got != c.exp {
			t.Errorf("%s: expiry %d want %d", c.code, got, c.exp)
		}
	}
	if actDurNotes[actDurToday] != " — Today" || actDurLabels[actDurNone] != "Don’t clear" {
		t.Errorf("labels: %q %q", actDurNotes[actDurToday], actDurLabels[actDurNone])
	}
}

func TestActOOOReply(t *testing.T) {
	now := actAt(2026, time.November, 6, 14, 10) // Friday
	if got, want := actOOOReply(actAt(2026, time.November, 7, 15, 0), now), "I’m out of office until 3:00 PM, Sat, Nov 7."; got != want {
		t.Errorf("got %q want %q", got, want)
	}
	// v4SM: the time alone today; 11:59 PM ends name the day after; a week
	// or more away drops the time.
	cases := []struct {
		end  time.Time
		want string
	}{
		{actAt(2026, time.November, 6, 17, 30), "5:30 PM"},
		{actAt(2026, time.November, 6, 23, 59), "tomorrow"},
		{actAt(2026, time.November, 10, 23, 59), "Wed, Nov 11"},
		{actAt(2026, time.November, 13, 15, 0), "Fri, Nov 13"},
		{actAt(2026, time.November, 12, 9, 0), "9:00 AM, Thu, Nov 12"},
	}
	for _, c := range cases {
		if got := actOOODate(c.end, now); got != c.want {
			t.Errorf("%v: got %q want %q", c.end, got, c.want)
		}
	}
	// on a Wednesday, this Friday's 11:59 PM is "next week"
	if got := actOOODate(actAt(2026, time.November, 13, 23, 59), actAt(2026, time.November, 11, 9, 0)); got != "next week" {
		t.Errorf("Friday from Wednesday: %q", got)
	}
}

func TestAvWorkingHours(t *testing.T) {
	// workhours.png: DND 0:00 → 0:30 shows "12:30 AM to 12:00 AM".
	from, to := avFromDND("0:00", "0:30")
	if from != 30 || to != 0 {
		t.Errorf("from %d to %d", from, to)
	}
	if s, e := avToDND(30, 0); s != "0:00" || e != "0:30" {
		t.Errorf("inverse: %q %q", s, e)
	}
	// Notifications 8:00 AM – 10:00 PM ⇔ DND 22:00 → 8:00.
	if s, e := avToDND(8*60, 22*60); s != "22:00" || e != "8:00" {
		t.Errorf("8-22: %q %q", s, e)
	}
	from, to = avFromDND("22:00", "8:00")
	if from != 8*60 || to != 22*60 {
		t.Errorf("22→8: %d %d", from, to)
	}
	for _, m := range []int{0, 5, 9 * 60, 17*60 + 30, 23*60 + 30} {
		s, e := avToDND(m, (m+90)%1440)
		if f, t2 := avFromDND(s, e); f != m || t2 != (m+90)%1440 {
			t.Errorf("roundtrip %d: %d %d", m, f, t2)
		}
	}
	if got := avFormatHM(5); got != "0:05" {
		t.Errorf("format: %q", got)
	}
	if _, ok := avParseHM("bad"); ok {
		t.Error("bad parsed")
	}
	if m, ok := avParseHM("24:00"); !ok || m != 0 {
		t.Errorf("24:00 → %d %v", m, ok)
	}
	if from, to := avFromDND("", ""); from != 9*60 || to != 17*60 {
		t.Errorf("fallback: %d %d", from, to)
	}
}
