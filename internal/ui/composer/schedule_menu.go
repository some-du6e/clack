package composer

import (
	"sort"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

// The send button's "Schedule for later" menu, as Slack builds it
// (boot-shared BYKP ScheduleSendMenu over BQDv): a 260px menu headed
// "Schedule message" whose rows depend on when you open it
// (getCurrentScheduledSendTimeBlock), then a separator and "Custom time".
// In a DM whose other member keeps another time zone each row says when
// that is for them ("3:00 AM for Alice", Mluw
// getScheduledDraftTimeZoneDetails), and while it is night there (22:00
// to 08:00) a row for 9:00 their time leads (MemberSpecificSchedulingTime).
// With the updated_scheduled_sends_desktop experiment "Tomorrow at 9:00
// AM" is always offered and the last custom time comes back while it is
// still ahead. Rows are sorted by time with duplicates dropped.

// schedOpt is BQDv's scheduling option (its numbering).
type schedOpt int

const (
	schedLaterToday9 schedOpt = iota
	schedLaterToday1
	schedTomorrow9
	schedTomorrow1
	schedMonday9
	schedNextMonday9
	schedMember
	schedPrevious
	schedCustom
)

// schedTimeBlock is getCurrentScheduledSendTimeBlock.
func schedTimeBlock(now time.Time) int {
	d, h := now.Weekday(), now.Hour()
	switch {
	case d >= time.Tuesday && d <= time.Friday && h < 8:
		return 0
	case d >= time.Tuesday && d <= time.Friday && h < 12:
		return 1
	case d >= time.Tuesday && d <= time.Thursday:
		return 2
	case d == time.Friday || d == time.Saturday || d == time.Sunday:
		return 3
	case d == time.Monday && h < 8:
		return 4
	case d == time.Monday && h < 12:
		return 5
	case d == time.Monday:
		return 6
	}
	return 7
}

// schedStandardOptions is getStandardScheduledSendMenuOptions.
func schedStandardOptions(now time.Time, alwaysTomorrow bool) []schedOpt {
	var o []schedOpt
	switch schedTimeBlock(now) {
	case 0:
		o = []schedOpt{schedLaterToday9, schedMonday9, schedCustom}
	case 1:
		o = []schedOpt{schedLaterToday1, schedMonday9, schedCustom}
	case 2:
		o = []schedOpt{schedTomorrow9, schedMonday9, schedCustom}
	case 3:
		o = []schedOpt{schedMonday9, schedCustom}
	case 4:
		o = []schedOpt{schedLaterToday9, schedNextMonday9, schedCustom}
	case 5:
		o = []schedOpt{schedLaterToday1, schedNextMonday9, schedCustom}
	case 6:
		o = []schedOpt{schedTomorrow9, schedNextMonday9, schedCustom}
	default:
		o = []schedOpt{schedCustom}
	}
	if alwaysTomorrow && !schedHas(o, schedTomorrow9) {
		monday := schedHas(o, schedMonday9) || schedHas(o, schedNextMonday9)
		if !(now.AddDate(0, 0, 1).Weekday() == time.Monday && monday) {
			at := len(o) - 1 // before Monday, else before Custom
			for i, x := range o {
				if x == schedMonday9 || x == schedNextMonday9 {
					at = i
					break
				}
			}
			o = append(o[:at], append([]schedOpt{schedTomorrow9}, o[at:]...)...)
		}
	}
	return o
}

func schedHas(o []schedOpt, x schedOpt) bool {
	for _, y := range o {
		if y == x {
			return true
		}
	}
	return false
}

// schedStandardTime is getTimeForStandardSchedulingOption (moment's
// day(): Monday of this week from Sunday, else of next week).
func schedStandardTime(o schedOpt, now time.Time) time.Time {
	at := func(d time.Time, h int) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), h, 0, 0, 0, d.Location())
	}
	switch o {
	case schedLaterToday9:
		return at(now, 9)
	case schedLaterToday1:
		return at(now, 13)
	case schedTomorrow9:
		return at(now.AddDate(0, 0, 1), 9)
	case schedTomorrow1:
		return at(now.AddDate(0, 0, 1), 13)
	case schedMonday9, schedNextMonday9:
		days := 8 - int(now.Weekday()) // Monday of next week
		if now.Weekday() == time.Sunday {
			days = 1
		}
		return at(now.AddDate(0, 0, days), 9)
	}
	return time.Time{}
}

// schedStandardLabel is getLabelForStandardSchedulingOptionThunk, with
// Slack's 24-hour strings (time24) word for word.
func schedStandardLabel(o schedOpt, h24 bool) string {
	if h24 {
		return [...]string{"Later today at 09:00", "Later today at 13:00", "Tomorrow  at 09:00", "Tomorrow at 13:00",
			"Monday at 09:00", "Next Monday at 09:00", "", "", "Custom time"}[o]
	}
	return [...]string{"Later today at 9:00 AM", "Later today at 1:00 PM", "Tomorrow at 9:00 AM", "Tomorrow at 1:00 PM",
		"Monday at 9:00 AM", "Next Monday at 9:00 AM", "", "", "Custom time"}[o]
}

// schedRow is one row of the menu.
type schedRow struct {
	opt         schedOpt
	at          time.Time // zero for Custom time
	label, desc string
}

// schedMemberInfo is the one other member of a DM, for the member rows.
type schedMemberInfo struct {
	name   string
	loc    *time.Location // their time zone
	sameTZ bool           // the viewer's time zone name is theirs (or theirs is unknown)
	// offset: their tz_offset isn't 0 (getMemberSpecificSchedulingTime
	// returns nothing without one)
	offset bool
}

// schedMemberLoc is the member's time zone: its name, else the offset.
func schedMemberLoc(u core.User) *time.Location {
	if u.TZ != "" {
		if l, err := time.LoadLocation(u.TZ); err == nil {
			return l
		}
	}
	return time.FixedZone(u.TZLabel, u.TZOffset)
}

// schedMemberTime is getMemberSpecificSchedulingTime without the DND
// half (Clack has no other members' dnd state): at night for them
// (isDuringMemberNighttime, 22:00 to 08:00) it is 9:00 their time, today
// or tomorrow, or on Monday from Friday 9:00 to Sunday
// (shouldScheduleForMemberAfterWeekend).
func schedMemberTime(m *schedMemberInfo, now time.Time) (time.Time, bool) {
	if m == nil || m.loc == nil || !m.offset {
		return time.Time{}, false
	}
	t := now.In(m.loc)
	if h := t.Hour(); h < 22 && h >= 8 {
		return time.Time{}, false
	}
	nine := time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, m.loc)
	if schedAfterWeekend(t) {
		days := 8 - int(t.Weekday())
		if t.Weekday() == time.Sunday {
			days = 1
		}
		return nine.AddDate(0, 0, days), true
	}
	if t.Hour() >= 9 {
		nine = nine.AddDate(0, 0, 1)
	}
	return nine, true
}

// schedAfterWeekend is shouldScheduleForMemberAfterWeekend.
func schedAfterWeekend(t time.Time) bool {
	d := t.Weekday()
	return d == time.Friday && t.Hour() >= 9 || d == time.Saturday || d == time.Sunday
}

// schedMemberLabel is getLabelForSchedulingOptionThunk's member row.
func schedMemberLabel(at, now time.Time, m *schedMemberInfo, h24 bool) string {
	at = at.In(now.Location()) // the viewer's clock and day (moment's local time)
	ts := schedClock(at, h24)
	if schedAfterWeekend(now.In(m.loc)) {
		day := "Sunday"
		if at.Weekday() == time.Monday {
			day = "Monday"
		}
		return day + " at " + ts
	}
	if at.YearDay() == now.YearDay() && at.Year() == now.Year() {
		return "Later today at " + ts
	}
	return "Tomorrow at " + ts
}

// schedPreviousLabel is the "previously scheduled" row's label.
func schedPreviousLabel(at, now time.Time, h24 bool) string {
	at = at.In(now.Location())
	ts := schedClock(at, h24)
	same := func(a, b time.Time) bool { return a.Year() == b.Year() && a.YearDay() == b.YearDay() }
	switch {
	case same(at, now):
		return "Later today at " + ts
	case same(at, now.AddDate(0, 0, 1)):
		return "Tomorrow at " + ts
	}
	end := time.Date(now.Year(), now.Month(), now.Day()+7, 23, 59, 59, 0, now.Location())
	if at.After(end) {
		// toCalendarDate excludeYear: MMMMDo ("November 18th")
		return at.Format("January 2") + schedOrdinal(at.Day()) + " at " + ts
	}
	if at.Weekday() == now.Weekday() {
		return "Next " + at.Format("Monday") + " at " + ts
	}
	return at.Format("Monday") + " at " + ts
}

// schedOrdinal is moment's English ordinal suffix ("st", "nd", "th").
func schedOrdinal(d int) string {
	if d%100 >= 11 && d%100 <= 13 {
		return "th"
	}
	switch d % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	}
	return "th"
}

func schedClock(t time.Time, h24 bool) string {
	if h24 {
		return t.Format("15:04")
	}
	return t.Format("3:04 PM")
}

// lastCustomSchedule is the drafts state's lastCustomScheduledTime: the
// last time picked with "Custom time" (the experiment's "previously
// scheduled" row).
var lastCustomSchedule time.Time

// scheduleRows is getScheduledSendMenuOptions with each row's label and
// description.
func scheduleRows(now time.Time, h24, experiment bool, m *schedMemberInfo) []schedRow {
	std := schedStandardOptions(now, experiment)
	var extra []schedRow
	if at, ok := schedMemberTime(m, now); ok {
		extra = append(extra, schedRow{opt: schedMember, at: at, label: schedMemberLabel(at, now, m, h24)})
	}
	if experiment && lastCustomSchedule.After(now) {
		extra = append(extra, schedRow{opt: schedPrevious, at: lastCustomSchedule, label: schedPreviousLabel(lastCustomSchedule, now, h24)})
	}
	var rows []schedRow
	for _, o := range std {
		if o != schedCustom {
			rows = append(rows, schedRow{opt: o, at: schedStandardTime(o, now), label: schedStandardLabel(o, h24)})
		}
	}
	if len(extra) > 0 {
		rows = append(rows, extra...)
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].at.Before(rows[j].at) })
		uniq := rows[:0]
		for _, r := range rows {
			if len(uniq) > 0 && uniq[len(uniq)-1].at.Equal(r.at) {
				continue
			}
			uniq = append(uniq, r)
		}
		rows = uniq
	}
	if m != nil && !m.sameTZ {
		for i := range rows {
			// "{memberTime} for {memberName}"
			rows[i].desc = schedClock(rows[i].at.In(m.loc), h24) + " for " + m.name
		}
	}
	return append(rows, schedRow{opt: schedCustom, label: schedStandardLabel(schedCustom, h24)})
}

// schedCustomDefault is getDefaultCustomTime: the next hour, or the one
// after when that is under getMinimumCustomTime (5 minutes) away; in a
// DM at night for the other member, their 9:00.
func schedCustomDefault(now time.Time, m *schedMemberInfo) time.Time {
	if at, ok := schedMemberTime(m, now); ok {
		return at
	}
	t := now.Truncate(time.Hour).Add(time.Hour)
	if t.Before(schedMinTime(now)) {
		t = t.Add(time.Hour)
	}
	return t
}

// schedMinTime and schedMaxTime are getMinimumCustomTime and
// getMaximumCustomTime: 5 minutes to 120 days ahead (seconds dropped).
func schedMinTime(now time.Time) time.Time { return now.Add(5 * time.Minute).Truncate(time.Minute) }
func schedMaxTime(now time.Time) time.Time { return now.AddDate(0, 0, 120).Truncate(time.Minute) }
