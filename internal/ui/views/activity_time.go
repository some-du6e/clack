package views

// An Activity row's time: Slack's ActivityTimestamp (gantry boot-shared
// /Q+H) and its text (0Rg1 rK, with 2Ie3 toCalendarDate and the en-US
// calendarDateFormat; build 4d0c78085232). From a week old the row holds
// two variants and the CSS picks one by the nearest container's width:
// timestamp_variant_narrow at 285px or narrower (the Dense timestamp is a
// 54px container: always), timestamp_variant_wide otherwise (a Detailed
// row measures the row, activity_multi_line_row_content).

import (
	"math"
	"strconv"
	"time"

	"github.com/some-du6e/clack/internal/i18n"
)

// actNarrowW is the container width at which a row shows the narrow
// variant (@container (width<=285px)), dp.
const actNarrowW = 285

// actRel is rK within the hour: "Just now" under a minute, then the
// minutes, rounded ("{time, plural, =1 {# min} other {# mins}}"), and how
// long until the text changes.
func actRel(age time.Duration) (string, time.Duration) {
	if age < time.Minute {
		return nsMentions.T("Just now"), time.Minute - age
	}
	m := int(math.Round(age.Minutes()))
	left := time.Duration((float64(m)+.5)*float64(time.Minute)) - age
	if left <= 0 {
		left = time.Second
	}
	return nsMentions.T("{time, plural, =1 {# min} other {# mins}}", "time", m), left
}

// actWhen is rK past the hour: the time within 8 hours, "Yesterday", the
// time within a day, the weekday within a week (and on a week-old item
// that fell on the weekday a week ago), else the calendar date, "November
// 9th", with its year from 365 days on ("November 9th, 2025"). narrow is
// the narrow variant's date (useShortenCalendarDate: "Nov 9th").
func actWhen(now, t time.Time, narrow bool) string {
	if t.IsZero() {
		return ""
	}
	const day = 24 * time.Hour
	e := now.Sub(t)
	switch {
	case e < 8*time.Hour:
		return clock(t)
	case sameDay(t, now.AddDate(0, 0, -1)):
		return nsMentions.T("Yesterday")
	case e < day:
		return clock(t)
	case e < 7*day:
		return weekday(t)
	case e < 8*day && t.Format("Monday") == now.AddDate(0, 0, -7).Format("Monday"):
		return weekday(t)
	}
	if l := i18n.Locale(); l != i18n.EnUS {
		return i18n.CalendarDate(l, t, i18n.CalendarOpts{ShortenMonth: narrow, ExcludeYear: e < 365*day})
	}
	month := "January" // MMMM Do
	if narrow {
		month = "Jan" // MMM Do
	}
	s := t.Format(month) + " " + strconv.Itoa(t.Day()) + ordinal(t.Day())
	if e >= 365*day {
		s += ", " + strconv.Itoa(t.Year())
	}
	return s
}
