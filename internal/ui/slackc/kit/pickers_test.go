package kit

import (
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

func TestMatchOption(t *testing.T) {
	opts := []any{
		slackc.Props{"label": "One", "value": "1"},
		map[string]any{"label": "Two", "value": 2.0},
		slackc.Props{"label": "Three", "value": "three"},
	}
	cases := []struct {
		sel  any
		want int
	}{
		{slackc.Props{"label": "One", "value": "1"}, 0},
		{slackc.Props{"value": 1.0}, 0},    // number matches its string
		{slackc.Props{"value": "2"}, 1},    // string matches a number
		{map[string]any{"value": 2}, 1},    // Go int
		{"three", 2},                       // bare value
		{slackc.Props{"value": "x"}, -1},   // missing
		{slackc.Props{"label": "One"}, -1}, // no value
		{nil, -1},
	}
	for i, c := range cases {
		if got := pkMatchOption(opts, c.sel); got != c.want {
			t.Errorf("case %d: got %d, want %d", i, got, c.want)
		}
	}
	if pkMatchOption(nil, slackc.Props{"value": "1"}) != -1 {
		t.Error("no options should not match")
	}
}

func TestOptionLabel(t *testing.T) {
	el := slackc.Element{Nodes: []*slackc.Node{{T: "#", V: ":tada: "}, {T: "b", C: []*slackc.Node{{T: "#", V: "Party"}}}}}
	if got := pkOptionLabel(slackc.Props{"label": el, "value": "p"}); got != ":tada: Party" {
		t.Errorf("element label: %q", got)
	}
	if got := pkOptionLabel(slackc.Props{"value": "v"}); got != "v" {
		t.Errorf("value fallback: %q", got)
	}
}

func TestParseTyped(t *testing.T) {
	ok := map[string]int{
		"930":     9*60 + 30,
		"9:30pm":  21*60 + 30,
		"9:30 PM": 21*60 + 30,
		"21:30":   21*60 + 30,
		"9 am":    9 * 60,
		"9":       9 * 60,
		"12am":    0,
		"12 pm":   12 * 60,
		"12:15a":  15,
		"0930":    9*60 + 30,
		"1230":    12*60 + 30,
		"7p.m.":   19 * 60,
		"0:00":    0,
		"23:59":   23*60 + 59,
	}
	for in, want := range ok {
		got, k := pkParseTyped(in)
		if !k || got != want {
			t.Errorf("%q: got %d,%v want %d", in, got, k, want)
		}
	}
	for _, in := range []string{"", "pm", "25:00", "13pm", "0am", "9:7", "9:60", "12345", "abc", "9:3o"} {
		if _, k := pkParseTyped(in); k {
			t.Errorf("%q should not parse", in)
		}
	}
}

func TestHM(t *testing.T) {
	if m, ok := pkParseHM("09:30"); !ok || m != 570 {
		t.Errorf("09:30: %d %v", m, ok)
	}
	if m, ok := pkParseHM("9:05"); !ok || m != 545 {
		t.Errorf("9:05: %d %v", m, ok)
	}
	for _, s := range []string{"", "24:00", "9", "9:", ":30", "x:yy"} {
		if _, ok := pkParseHM(s); ok {
			t.Errorf("%q should not parse", s)
		}
	}
	if s := pkFormatHM(570); s != "09:30" {
		t.Errorf("format: %q", s)
	}
	for m, want := range map[int]string{0: "12:00 AM", 570: "9:30 AM", 720: "12:00 PM", 1290: "9:30 PM", 1439: "11:59 PM"} {
		if got := pkTimeLabel(m); got != want {
			t.Errorf("label %d: %q want %q", m, got, want)
		}
	}
}

func TestSlots(t *testing.T) {
	if n := len(pkSlots(pkIncrement(""), 0, 0)); n != 48 {
		t.Errorf("half-hour day: %d slots", n)
	}
	if n := len(pkSlots(pkIncrement("quarter"), 0, 1439)); n != 96 {
		t.Errorf("quarter day: %d", n)
	}
	s := pkSlots(pkIncrement("full"), 9*60+10, 12*60)
	if len(s) != 3 || s[0] != 600 || s[2] != 720 {
		t.Errorf("bounded hourly: %v", s)
	}
	if pkIncrement("five_minute") != 5 || pkIncrement("ten_minute") != 10 {
		t.Error("increments")
	}
}

func TestDateFormat(t *testing.T) {
	d := time.Date(2026, time.November, 3, 0, 0, 0, 0, time.Local)
	cases := map[string]string{
		"":                  "2026-11-03",
		"YYYY-MM-DD":        "2026-11-03",
		"M/D/YY":            "11/3/26",
		"MMM D, YYYY":       "Nov 3, 2026",
		"dddd, MMMM Do":     "Tuesday, November 3rd",
		"ddd DD.MM.YYYY":    "Tue 03.11.2026",
		"[Day] D [of] MMMM": "Day 3 of November",
	}
	for f, want := range cases {
		got := pkFormatDate(d, f)
		if got != want {
			t.Errorf("format %q: %q want %q", f, got, want)
			continue
		}
		back, ok := pkParseDate(got, f)
		if !ok || !pkSameDay(back, d) {
			t.Errorf("parse %q with %q: %v %v", got, f, back, ok)
		}
	}
	if _, ok := pkParseDate("2026-02-30", ""); ok {
		t.Error("Feb 30 should not parse")
	}
	if _, ok := pkParseDate("2026-13-01", ""); ok {
		t.Error("month 13 should not parse")
	}
	if _, ok := pkParseDate("2026-11-03x", ""); ok {
		t.Error("trailing junk should not parse")
	}
	if got, ok := pkParseDate("november 21st", "MMMM Do"); !ok || got.Day() != 21 || got.Month() != time.November {
		t.Errorf("case-insensitive month: %v %v", got, ok)
	}
	for n, want := range map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 22: "22nd", 23: "23rd", 31: "31st"} {
		if got := pkOrdinal(n); got != want {
			t.Errorf("ordinal %d: %q", n, got)
		}
	}
}

func TestMonthGrid(t *testing.T) {
	g := pkMonthGrid(time.Date(2026, time.November, 15, 0, 0, 0, 0, time.UTC))
	// Nov 1 2026 is a Sunday
	if g[0] != 1 || g[29] != 30 || g[30] != 0 {
		t.Errorf("Nov 2026 grid: %v", g)
	}
	g = pkMonthGrid(time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC))
	// Feb 1 2026 is a Sunday, 28 days
	if g[0] != 1 || g[27] != 28 || g[28] != 0 {
		t.Errorf("Feb 2026 grid: %v", g)
	}
}

func TestRules(t *testing.T) {
	r := pkMakeRules(slackc.Props{
		"disabledDates":      []any{"2026-11-10", "bad"},
		"disableDatesBefore": "2026-11-05",
		"disableDatesAfter":  "2026-11-20",
	}, "")
	day := func(d int) time.Time { return time.Date(2026, time.November, d, 0, 0, 0, 0, time.Local) }
	for d, want := range map[int]bool{4: true, 5: false, 10: true, 11: false, 20: false, 21: true} {
		if got := r.disabled(day(d)); got != want {
			t.Errorf("Nov %d disabled=%v want %v", d, got, want)
		}
	}
}

func TestRangeStep(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, time.November, d, 0, 0, 0, 0, time.Local) }
	var zero time.Time
	s, e, f, done := pkRangeStep(zero, zero, day(5), 0)
	if !pkSameDay(s, day(5)) || !e.IsZero() || f != 1 || done {
		t.Fatalf("start pick: %v %v %d %v", s, e, f, done)
	}
	s, e, f, done = pkRangeStep(s, e, day(9), f)
	if !pkSameDay(s, day(5)) || !pkSameDay(e, day(9)) || f != 0 || !done {
		t.Fatalf("end pick: %v %v %d %v", s, e, f, done)
	}
	// picking the end before the start restarts the range
	s, e, f, done = pkRangeStep(day(5), zero, day(2), 1)
	if !pkSameDay(s, day(2)) || !e.IsZero() || f != 1 || done {
		t.Fatalf("restart: %v %v %d %v", s, e, f, done)
	}
	// a new start after the end clears the end
	s, e, _, _ = pkRangeStep(day(5), day(9), day(12), 0)
	if !pkSameDay(s, day(12)) || !e.IsZero() {
		t.Fatalf("start past end: %v %v", s, e)
	}
}
