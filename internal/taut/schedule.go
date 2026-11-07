package taut

import (
	"strconv"
	"strings"
	"time"
)

// ScheduleWindow is Taut's weekly window: days like "mon", 24-hour
// "HH:MM" times; an end at or before the start is the next day.
type ScheduleWindow struct {
	Days  []string `json:"days"`
	Start string   `json:"start"`
	End   string   `json:"end"`
}

var weekdays = map[string]time.Weekday{"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday}

const maxChain = 7 * 24 * time.Hour

func hhmm(s string) (h, m int) {
	a, b, _ := strings.Cut(strings.TrimSpace(s), ":")
	h, _ = strconv.Atoi(a)
	m, _ = strconv.Atoi(b)
	return h, m
}

type run struct{ start, end time.Time }

// runsAround are a window's runs starting the day before, of, and after at.
func runsAround(w ScheduleWindow, at time.Time) []run {
	sh, sm := hhmm(w.Start)
	eh, em := hhmm(w.End)
	var out []run
	y, mo, d := at.Date()
	loc := at.Location()
	for _, off := range []int{-1, 0, 1} {
		day := time.Date(y, mo, d+off, 0, 0, 0, 0, loc)
		on := false
		for _, name := range w.Days {
			if wd, ok := weekdays[strings.ToLower(name)]; ok && wd == day.Weekday() {
				on = true
			}
		}
		if !on {
			continue
		}
		start := time.Date(y, mo, d+off, sh, sm, 0, 0, loc)
		endDay := d + off
		if eh*60+em <= sh*60+sm {
			endDay++
		}
		out = append(out, run{start, time.Date(y, mo, endDay, eh, em, 0, 0, loc)})
	}
	return out
}

// ScheduleEnd is api.scheduleEnd: when the window on at `now` ends, with
// back-to-back windows merged up to a week out; ok is false if none is on.
// tz is an IANA zone ("" = the system's).
func ScheduleEnd(windows []ScheduleWindow, tz string, now time.Time) (time.Time, bool) {
	loc := time.Local
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	now = now.In(loc)
	var end time.Time
	found := false
	for _, w := range windows {
		for _, r := range runsAround(w, now) {
			if !r.start.After(now) && now.Before(r.end) && (!found || r.end.After(end)) {
				end, found = r.end, true
			}
		}
	}
	if !found {
		return time.Time{}, false
	}
	limit := now.Add(maxChain)
	for extended := true; extended && end.Before(limit); {
		extended = false
		for _, w := range windows {
			for _, r := range runsAround(w, end) {
				if !r.start.After(end) && r.end.After(end) {
					end = r.end
					extended = true
				}
			}
		}
	}
	if end.After(limit) {
		end = limit
	}
	return end, true
}
