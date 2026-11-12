package workobject

import (
	"math"
	"strconv"
	"time"

	"github.com/some-du6e/clack/internal/i18n"
)

// ns is the I18n namespace of Slack's work object components
// (new I18n("work-objects")).
const ns = i18n.NS("work-objects")

// ToTime is AdSZ's unix parse: a number or numeric string in seconds,
// milliseconds from 1e10 up. ok is false for an empty value.
func ToTime(v float64) (time.Time, bool) {
	if v == 0 || math.IsNaN(v) {
		return time.Time{}, false
	}
	if v >= 1e10 {
		return time.UnixMilli(int64(v)), true
	}
	sec, frac := math.Modf(v)
	return time.Unix(int64(sec), int64(frac*1e9)), true
}

// TSTime is ToTime of a message ts ("1762700000.000100").
func TSTime(ts string) (time.Time, bool) {
	v, err := strconv.ParseFloat(ts, 64)
	if err != nil {
		return time.Time{}, false
	}
	return ToTime(v)
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// dayDiff is dayjs(now).startOf("day").diff(dayjs(t).startOf("day"), "day").
func dayDiff(now, t time.Time) int {
	a, b := startOfDay(now), startOfDay(t)
	// whole calendar days, robust to DST: count from the dates
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	ua := time.Date(ay, am, ad, 0, 0, 0, 0, time.UTC)
	ub := time.Date(by, bm, bd, 0, 0, 0, 0, time.UTC)
	return int(ua.Sub(ub).Hours() / 24)
}

// shortCalendar is toFormattedCalendarDate / 2Ie3 with shortenMonth,
// excludeYear and shortenDay: "Nov 3".
func shortCalendar(loc string, t time.Time) string {
	return i18n.CalendarDate(loc, t, i18n.CalendarOpts{ShortenMonth: true, ExcludeYear: true, ShortenDay: true})
}

// shortFull is tNjd's {date_short_full}: 2Ie3 with shortenMonth and
// shortenDay ("Nov 3, 2025").
func shortFull(loc string, t time.Time) string {
	return i18n.CalendarDate(loc, t, i18n.CalendarOpts{ShortenMonth: true, ShortenDay: true})
}

// UpdatedLabel is generateUpdatedTimestamp (ZV2l L): the footer's text
// for a layout's updated_at, relative to now ("" for no time).
func UpdatedLabel(loc string, updated, now time.Time) string {
	if updated.IsZero() {
		return ""
	}
	c := now.Sub(updated)
	d := int64(math.Floor(c.Seconds()))
	u := int64(math.Floor(float64(d) / 60))
	m := int64(math.Floor(float64(u) / 60))
	p := dayDiff(now, updated)
	switch {
	case d < 60:
		return ns.T("Updated just now")
	case u < 60:
		return ns.T("Updated {minutes} min ago", "minutes", u)
	case m < 24:
		return ns.T("Updated {hours, plural, =1 {# hour} other {# hours}} ago", "hours", m)
	case p == 1:
		return ns.T("Updated yesterday")
	case p < 7:
		return ns.T("Updated on {dayName}", "dayName", i18n.Weekday(loc, updated, false))
	case updated.Year() == now.Year():
		return ns.T("Updated on {date}", "date", shortCalendar(loc, updated))
	}
	return ns.T("Updated on {date}", "date", shortFull(loc, updated))
}

// AsOfLabel is generateAsOfTimestamp (3uan f): the footer's text for the
// message's ts when the layout has no updated_at; from names the
// perspective user ("" none).
func AsOfLabel(loc string, at, now time.Time, do24 bool, from string) string {
	if at.IsZero() {
		return ""
	}
	tm := i18n.Time(loc, at, do24, false)
	days := math.Floor(math.Abs(now.Sub(at).Minutes()) / 1440)
	// bRXt's word in the locale, compared with the English ones as 3uan
	// does: other locales fall through to the weekday
	switch i18n.RelativeDay(loc, at, now, true) {
	case "today":
		if from != "" {
			return ns.T("As of today at {time} from {sourceUserName}", "time", tm, "sourceUserName", from)
		}
		return ns.T("As of today at {time}", "time", tm)
	case "yesterday":
		if from != "" {
			return ns.T("As of yesterday at {time} from {sourceUserName}", "time", tm, "sourceUserName", from)
		}
		return ns.T("As of yesterday at {time}", "time", tm)
	}
	if days <= 7 {
		day := i18n.Weekday(loc, at, false)
		if from != "" {
			return ns.T("As of {dayName} at {time} from {sourceUserName}", "dayName", day, "time", tm, "sourceUserName", from)
		}
		return ns.T("As of {dayName} at {time}", "dayName", day, "time", tm)
	}
	date := shortFull(loc, at)
	if at.Year() == now.Year() {
		date = shortCalendar(loc, at)
	}
	if from != "" {
		return ns.T("As of {date} from {sourceUserName}", "date", date, "sourceUserName", from)
	}
	return ns.T("As of {date}", "date", date)
}

// FooterTip is the footer time's tooltip (jKQ2 Y): "{date} at {time}",
// the date by 2Ie3 with shortenDay, the time with seconds.
func FooterTip(loc string, t time.Time, do24 bool) string {
	if t.IsZero() {
		return ""
	}
	return ns.T("{date} at {time}", "date", i18n.CalendarDate(loc, t, i18n.CalendarOpts{ShortenDay: true}),
		"time", i18n.Time(loc, t, do24, true))
}

// HeaderTime is the header's timestamp label (nuF4 _) when the refresh
// timestamp experiment is off: the time today, else {date_short_pretty}
// (Yesterday, Tomorrow, else "Nov 3", with the year beyond six months).
// nuF4 passes no do24hrTime, so callers give the locale's default
// (i18n.Default24h), not the user's pref.
func HeaderTime(loc string, t, now time.Time, do24 bool) string {
	if t.IsZero() {
		return ""
	}
	if startOfDay(t).Equal(startOfDay(now)) {
		return i18n.Time(loc, t, do24, false)
	}
	if r := i18n.RelativeDay(loc, t, now, false); r != "" {
		return r
	}
	excl := math.Abs(now.Sub(t).Minutes()) < 262080 // excludeYearHelper
	return i18n.CalendarDate(loc, t, i18n.CalendarOpts{ShortenMonth: true, ShortenDay: true, ExcludeYear: excl})
}
