package core

// A task row's description line: Slack's ZgZs (boot-shared) over the
// trigger's schedule, with the time-bucket wording of VVpd and znID and
// the day helpers of client-boot g5+0, as AgentTaskItem calls it
// (n1._(automationInfo, time24, wfb_scheduled_trigger_time_buckets,
// wfb_schedule_on_last_day_of_month, ms3's between-window)). English
// strings, as Slack's en-US build has them; times are toTime's (h:mm A,
// or HH:mm under the time24 pref), dates toCalendarDate's (MMMM Do), the
// zone dayjs's "z" (the zone's short name).

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // a schedule's zone on Windows too (the app has it through jsrt; core's tests need their own)
)

// TaskTextOpts are the row's switches.
type TaskTextOpts struct {
	Time24         bool // the viewer's time24 pref
	TimeBuckets    bool // wfb_scheduled_trigger_time_buckets
	LastDayOfMonth bool // wfb_schedule_on_last_day_of_month
	// Window is ms3 (wfb_agents_can_create_simple_automations_tab_ms3):
	// "between {start} and {end}" over a 15 minute window.
	Window bool
	Now    time.Time // for "Once on …"'s year and the relative days
}

// TaskScheduleText is the row's description: the schedule in words for a
// scheduled task, else the task's description.
func TaskScheduleText(t AgentTask, o TaskTextOpts) string {
	s := t.Schedule
	if t.TriggerType != "scheduled" || s == nil {
		return t.Description
	}
	if s.StartTime == "" || s.Timezone == "" {
		return t.Description
	}
	u, ok := zonedTime(s.StartTime, s.Timezone)
	if !ok {
		return t.Description
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	if s.TimeBucket != "" {
		return timeBucketText(*s, u, o)
	}
	h := taskClock(u, o.Time24)
	tz := zoneAbbrev(u)
	g := ""
	if o.Window {
		g = taskClock(u.Add(15*time.Minute), o.Time24)
	}
	at := func(prefix string) string {
		if o.Window {
			return prefix + " between " + h + " and " + g + " " + tz
		}
		return prefix + " at " + h + " " + tz
	}
	f := s.Frequency
	if f == nil || f.Type == "once" {
		date := calendarDate(u, u.Year() == o.Now.In(u.Location()).Year())
		return at("Once on " + date)
	}
	b := f.RepeatsEvery
	if b == 0 && f.Type != "" { // repeatsEvery ?? 1 (a missing field reads as 1)
		b = 1
	}
	switch f.Type {
	case "daily":
		if b > 1 {
			return at("Every " + strconv.Itoa(b) + " days")
		}
		return at("Daily")
	case "weekly":
		if len(f.OnDays) == 0 {
			return t.Description
		}
		var days []string
		for _, d := range f.OnDays {
			days = append(days, weekdayLabel(d, true))
		}
		list := listify(days)
		if b > 1 {
			return at(list + " every " + strconv.Itoa(b) + " weeks")
		}
		return at(list)
	case "monthly":
		if f.OnLastDayOfMonth {
			if b > 1 {
				return at("Last day of the month, every " + strconv.Itoa(b) + " months")
			}
			return at("Last day of every month")
		}
		if len(f.OnDays) > 0 && f.OnWeekNum != nil {
			ord := ordinalLabel(*f.OnWeekNum, false)
			day := weekdayLabel(f.OnDays[0], false)
			if b > 1 {
				return at(ord + " " + day + " every " + strconv.Itoa(b) + " months")
			}
			return at(ord + " " + day + " of every month")
		}
		if b > 1 {
			return at("Every " + strconv.Itoa(b) + " months")
		}
		return at("Monthly")
	case "yearly":
		date := calendarDate(u, true)
		if b > 1 {
			return at("Every " + strconv.Itoa(b) + " years on " + date)
		}
		return at("Yearly on " + date)
	}
	return t.Description
}

// timeBucketText is VVpd M: "Starts {date} in the morning{repeat}" (or
// the afternoon or evening; any other bucket at the time), the repeat
// znID's.
func timeBucketText(s TaskSchedule, d time.Time, o TaskTextOpts) string {
	date := relativeDay(d, o.Now)
	if date == "" {
		date = calendarDate(d, true)
	}
	rep := repeatText(s, d)
	if rep != "" {
		rep = " " + rep
	}
	switch s.TimeBucket {
	case "morning":
		return "Starts " + date + " in the morning" + rep
	case "afternoon":
		return "Starts " + date + " in the afternoon" + rep
	case "evening":
		return "Starts " + date + " in the evening" + rep
	}
	return "Starts " + date + " at " + taskClock(d, o.Time24) + rep
}

// scheduleOption is TGBB getScheduleFrequencyMenuOption.
func scheduleOption(s TaskSchedule, d time.Time) string {
	f := s.Frequency
	if s.EndTime != "" || s.OccurrenceCount > 0 || f != nil && (f.RepeatsEvery > 1 || f.Type == "hourly") {
		return "custom"
	}
	if f == nil {
		return "once"
	}
	switch f.Type {
	case "weekly":
		day := d.Weekday().String()
		same := true
		for _, x := range f.OnDays {
			if x != day {
				same = false
			}
		}
		if len(f.OnDays) == 0 || same {
			return "weekly_same_day"
		}
		if len(f.OnDays) == 5 {
			week := true
			for _, x := range f.OnDays {
				if x == "Saturday" || x == "Sunday" || weekdayLabel(x, false) == "" {
					week = false
				}
			}
			if week {
				return "weekdays"
			}
		}
		return "custom"
	case "monthly":
		nth := nthDay(d)
		if nth == 0 {
			nth = 1
		}
		// afel {type: "monthly", onDays: [day], onWeekNum: nth}, deep-equal
		if len(f.OnDays) == 1 && f.OnDays[0] == d.Weekday().String() && f.OnWeekNum != nil && *f.OnWeekNum == nth &&
			!f.OnLastDayOfMonth && f.RepeatsEvery <= 1 && f.exactly("type", "on_days", "on_week_num") {
			return "monthly_nth_day"
		}
		if f.OnLastDayOfMonth && d.AddDate(0, 0, 1).Month() != d.Month() {
			return "monthly_last_day"
		}
		return "custom"
	}
	return f.Type
}

// repeatText is znID o: the "and repeats …" clause.
func repeatText(s TaskSchedule, d time.Time) string {
	every := func(n int, one, many string) string {
		if n == 1 {
			return one
		}
		return strconv.Itoa(n) + " " + many
	}
	switch scheduleOption(s, d) {
	case "daily":
		return "and repeats daily"
	case "weekdays":
		return "and repeats every weekday"
	case "weekly_same_day":
		return "and repeats weekly on " + weekdayLabel(d.Weekday().String(), true)
	case "monthly_nth_day":
		return "and repeats monthly on the " + nthOrdinal(d, true) + " " + d.Weekday().String()
	case "monthly", "monthly_last_day", "yearly", "custom":
		f := s.Frequency
		if f == nil {
			return ""
		}
		o := f.RepeatsEvery
		if o == 0 {
			o = 1
		}
		switch f.Type {
		case "hourly":
			if o == 1 {
				return "and repeats hourly"
			}
			return "and repeats every " + every(o, "hour", "hours")
		case "daily":
			if o == 1 {
				return "and repeats daily"
			}
			return "and repeats every " + every(o, "day", "days")
		case "weekly":
			if f.OnDays == nil {
				return "and repeats on a custom schedule"
			}
			var days []string
			for _, x := range f.OnDays {
				days = append(days, weekdayLabel(x, false))
			}
			return "and repeats every " + every(o, "week", "weeks") + " on " + listify(days)
		case "monthly":
			if f.OnLastDayOfMonth {
				return "and repeats every " + every(o, "month", "months") + " on the last day"
			}
			if len(f.OnDays) > 0 || f.OnWeekNum != nil {
				ord := ""
				h := nthDay(d)
				if f.OnWeekNum != nil {
					h = *f.OnWeekNum
				}
				if h != 0 {
					ord = ordinalLabel(h, true)
				} else {
					ord = nthOrdinal(d, true)
				}
				return "and repeats every " + every(o, "month", "months") + " on the " + ord + " " + d.Weekday().String()
			}
			if o == 1 {
				return "and repeats monthly"
			}
			return "and repeats every " + every(o, "month", "months")
		case "yearly":
			if o == 1 {
				return "and repeats annually"
			}
			return "and repeats every " + every(o, "year", "years")
		}
		return "and repeats on a custom schedule"
	}
	return ""
}

// zonedTime is g5+0 toZonedDayjs: an ISO time with a zone (Z, +hh:mm)
// shown in tz, one without read as tz's wall clock.
func zonedTime(s, tz string) (time.Time, bool) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.In(loc), true
		}
	}
	for _, layout := range []string{"2006-01-02T15:04:05.000", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, true
		}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
		return time.Unix(n, 0).In(loc), true
	}
	return time.Time{}, false
}

// taskClock is toTime: h:mm A in en-US, HH:mm under time24.
func taskClock(t time.Time, time24 bool) string {
	if time24 {
		return t.Format("15:04")
	}
	return t.Format("3:04 PM")
}

// enUSZoneNames are the zone abbreviations CLDR's en-US data has, with
// the offset each names: Intl.DateTimeFormat("en-US", {timeZoneName:
// "short"}) writes these and GMT±H[:MM] for everything else (Paris
// GMT+1, Kolkata GMT+5:30, Manila's "PST" GMT+8, Shanghai's "CST" GMT+8).
var enUSZoneNames = map[string]int{
	"PST": -8 * 3600, "PDT": -7 * 3600, "MST": -7 * 3600, "MDT": -6 * 3600,
	"CST": -6 * 3600, "CDT": -5 * 3600, "EST": -5 * 3600, "EDT": -4 * 3600,
	"AKST": -9 * 3600, "AKDT": -8 * 3600, "HST": -10 * 3600, "HDT": -9 * 3600,
	"AST": -4 * 3600, "ADT": -3 * 3600, "GMT": 0, "UTC": 0,
}

// zoneAbbrev is dayjs's "z" (ZgZs u.format("z")): the timezone plugin's
// offsetName, Intl's en-US short zone name.
func zoneAbbrev(t time.Time) string {
	n, off := t.Zone()
	if o, ok := enUSZoneNames[n]; ok && o == off {
		return n
	}
	if off == 0 {
		return "GMT"
	}
	sign := "+"
	if off < 0 {
		sign, off = "-", -off
	}
	s := "GMT" + sign + strconv.Itoa(off/3600)
	if m := (off % 3600) / 60; m != 0 {
		s += ":" + fmt.Sprintf("%02d", m)
	}
	return s
}

// calendarDate is toCalendarDate: MMMM Do, with the year (", YYYY")
// unless excludeYear.
func calendarDate(t time.Time, excludeYear bool) string {
	s := t.Month().String() + " " + ordinalDay(t.Day())
	if !excludeYear {
		s += ", " + strconv.Itoa(t.Year())
	}
	return s
}

// relativeDay is maybeGetRelativeDay (uncapitalized): today, tomorrow or
// yesterday, else "".
func relativeDay(t, now time.Time) string {
	n := now.In(t.Location())
	ty, tm, td := t.Date()
	day := time.Date(ty, tm, td, 0, 0, 0, 0, t.Location())
	ny, nm, nd := n.Date()
	today := time.Date(ny, nm, nd, 0, 0, 0, 0, t.Location())
	switch day.Sub(today).Round(time.Hour) / (24 * time.Hour) {
	case 0:
		return "today"
	case 1:
		return "tomorrow"
	case -1:
		return "yesterday"
	}
	return ""
}

// weekdayLabel is U6: a day name, or its plural ("Mondays").
func weekdayLabel(day string, plural bool) string {
	switch day {
	case "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday":
		if plural {
			return day + "s"
		}
		return day
	}
	return ""
}

// ordinalLabel is getOrdinalLabelBuilder(n)(lower): First … Fourth, Last.
func ordinalLabel(n int, lower bool) string {
	var s string
	switch n {
	case 1:
		s = "First"
	case 2:
		s = "Second"
	case 3:
		s = "Third"
	case 4:
		s = "Fourth"
	case -1:
		s = "Last"
	default:
		return ""
	}
	if lower {
		return strings.ToLower(s)
	}
	return s
}

// nthDay is getNthDay: which of its weekday in the month the date is (1…4,
// -1 for the last in the month's final week).
func nthDay(t time.Time) int {
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	n, i := int(t.Weekday()), int(first.Weekday())
	r := (t.Day() + i + 6) / 7 // ceil((date + i) / 7)
	end := first.AddDate(0, 1, -1).Day()
	if r >= 4 && end-t.Day() < 7 {
		return -1
	}
	if n >= i {
		return r
	}
	return r - 1
}

// nthOrdinal is nA: the date's nthDay as an ordinal word.
func nthOrdinal(t time.Time, lower bool) string {
	n := nthDay(t)
	if n == 0 {
		return ""
	}
	return ordinalLabel(n, lower)
}

// listify is YlH8 in en-US: "a", "a and b", "a, b, and c".
func listify(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	case 2:
		return xs[0] + " and " + xs[1]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + ", and " + xs[len(xs)-1]
}
