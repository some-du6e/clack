package composer

import (
	"image"
	"regexp"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The "Custom time" dialog is Slack's ScheduleMessageDialog (boot-shared
// z1vG, CSS p-schedule_message__*):
//
//   - the header: "Schedule message" (18/24, 900) over "Time zone: {zone}"
//     (13/18 at 70%): getTimezoneLocationLabel (df4p) of the detected zone
//     (h05g TimezoneDetector.detect), its select label without the
//     "(UTC…)" part;
//   - ScheduleMessageTimePicker, a DateTimePicker (SzUY): the date field
//     (DatePicker: "Today", "Tomorrow", else toCalendarDate's "November
//     12th, 2026"; days outside getMinimumCustomTime..getMaximumCustomTime
//     disabled) on 60% of the row, the time select (TimePicker,
//     optionsHourIncrement QUARTER_HOUR, only the times between the
//     minimum and maximum on their days) on 40% after 12px. A change
//     keeps the other half and is clamped to the range (handleChange);
//   - in a DM with someone in another time zone, "{memberTime} for
//     {memberName}" under it (Mluw getScheduledDraftTimeZoneDetails);
//   - Cancel and "Schedule Message", off outside 5 minutes to 120 days
//     from now. It opens at getDefaultCustomTime.
type schedulePicker struct {
	h      Host
	m      *popups.Modal
	at     time.Time // customTime (local, whole minutes)
	member *schedMemberInfo
	onPick func(time.Time)

	dateBtn, timeBtn widget.Clickable
	dateOv, timeOv   popups.Overlay
}

// openSchedulePicker shows the dialog at start (getDefaultCustomTime);
// onPick gets the chosen time. member is the DM's other member for the
// time zone line (nil: none).
func openSchedulePicker(h Host, start time.Time, member *schedMemberInfo, onPick func(time.Time)) *schedulePicker {
	p := &schedulePicker{h: h, at: start.Truncate(time.Minute), member: member, onPick: onPick}
	p.m = popups.NewModal(h, popups.ModalOpts{
		Title:           i18n.T("drafts", "Schedule message"),
		Subtitle:        i18n.T("drafts", "Time zone: {timezone}", "timezone", scheduleZoneLabel()),
		SubtitleCaption: true,
		Body:            p.body,
		Buttons: []*popups.ModalButton{
			{Label: i18n.T("drafts", "Cancel"), Kind: popups.BtnOutline},
			{Label: i18n.T("drafts", "Schedule Message"), Kind: popups.BtnPrimary, Enabled: p.valid, Do: func() bool {
				if p.onPick != nil && p.valid() {
					p.onPick(p.at)
				}
				return true
			}},
		},
	})
	h.ShowOverlay(p.m)
	return p
}

// utcPart is getTimezoneLocationLabel's /\(UTC ?[+-]? ?.*?\)/.
var utcPart = regexp.MustCompile(`\(UTC ?[+-]? ?.*?\)`)

// scheduleZoneLabel is getTimezoneLocationLabel(detect(), locale): the
// detected zone's label in Slack's time zone select ("Central Time (US &
// Canada)"); the detector answers DEFAULT_TZ when it gives up.
func scheduleZoneLabel() string {
	z := core.DetectTimezone()
	if z == "" {
		z = core.DefaultTZ
	}
	l := core.TimezoneLabelIn(i18n.Locale(), z)
	if l == "" {
		return ""
	}
	return strings.TrimSpace(utcPart.ReplaceAllString(l, ""))
}

// valid is the dialog's submit rule: getMinimumCustomTime to
// getMaximumCustomTime.
func (p *schedulePicker) valid() bool {
	now := time.Now()
	return !p.at.Before(schedMinTime(now)) && !p.at.After(schedMaxTime(now))
}

// set is DateTimePicker's handleChange: the new time, clamped to the
// minimum and maximum.
func (p *schedulePicker) set(t time.Time) {
	now := time.Now()
	if lo := schedMinTime(now); t.Before(lo) {
		t = lo
	}
	if hi := schedMaxTime(now); t.After(hi) {
		t = hi
	}
	p.at = t
	p.h.Invalidate()
}

func dayOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func minsOf(t time.Time) int { return t.Hour()*60 + t.Minute() }

// dateLabel is DatePickerInput's getDisplayValueFromMoment: "Today",
// "Tomorrow", else toCalendarDate ("November 12th, 2026"); while the
// calendar is open the field shows the full date.
func scheduleDateLabel(day, now time.Time, open bool) string {
	switch {
	case open:
	case sameDate(day, now):
		return i18n.T("date_picker", "Today")
	case sameDate(day, now.AddDate(0, 0, 1)):
		return i18n.T("date_picker", "Tomorrow")
	}
	return i18n.CalendarDate(i18n.Locale(), day, i18n.CalendarOpts{})
}

func sameDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func overlayOpen(o popups.Overlay) bool { return o != nil && !o.Dismissed() }

// timeRange is the time select's options on day: the minimum's time on
// its day, the maximum's on its own (TimePicker's min and max), else the
// whole day.
func scheduleTimeRange(day, now time.Time) (from, to int) {
	from, to = 0, 23*60+59
	if lo := schedMinTime(now); sameDate(lo, day) {
		from = minsOf(lo)
	}
	if hi := schedMaxTime(now); sameDate(hi, day) {
		to = minsOf(hi)
	}
	return from, to
}

func (p *schedulePicker) body(gtx layout.Context) layout.Dimensions {
	th := p.h.Theme()
	now := time.Now()
	w := gtx.Constraints.Max.X
	org := popups.ModalBodyOrigin(gtx, p.m)
	y := gtx.Dp(5) // .p-schedule_message__time_picker padding-top
	fh := popups.SelectFieldHeight(gtx)
	dateW := w * 60 / 100
	timeX := dateW + gtx.Dp(12)
	timeW := w - timeX

	if p.dateBtn.Clicked(gtx) && !overlayOpen(p.dateOv) {
		r := image.Rectangle{Min: org.Add(image.Pt(0, y)), Max: org.Add(image.Pt(dateW, y+fh))}
		at := p.at
		p.dateOv = popups.NewDatePickerRange(p.h, r, dayOf(at), schedMinTime(now), schedMaxTime(now), func(day time.Time) {
			p.set(time.Date(day.Year(), day.Month(), day.Day(), at.Hour(), at.Minute(), 0, 0, at.Location()))
		})
		p.h.ShowOverlay(p.dateOv)
	}
	if p.timeBtn.Clicked(gtx) && !overlayOpen(p.timeOv) {
		r := image.Rectangle{Min: org.Add(image.Pt(timeX, y)), Max: org.Add(image.Pt(timeX+timeW, y+fh))}
		day := dayOf(p.at)
		from, to := scheduleTimeRange(day, now)
		p.timeOv = popups.NewTimePickerRange(p.h, r, minsOf(p.at), from, to, 15, func(m int) {
			p.set(day.Add(time.Duration(m) * time.Minute))
		})
		p.h.ShowOverlay(p.timeOv)
	}

	dateOpen := overlayOpen(p.dateOv)
	off := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
	popups.SelectField(gtx, th, &p.dateBtn, dateW, "calendar", scheduleDateLabel(p.at, now, dateOpen), dateOpen, false)
	off.Pop()
	off = op.Offset(image.Pt(timeX, y)).Push(gtx.Ops)
	popups.SelectField(gtx, th, &p.timeBtn, timeW, "clock", uipref.Clock(p.at), overlayOpen(p.timeOv), false)
	off.Pop()
	y += fh + gtx.Dp(8) // margin-bottom 8px

	if d := scheduleZoneDetails(p.at, p.member, uipref.Get().Time24); d != "" {
		y -= gtx.Dp(2) // .p-schedule_message__time_zone_details margin-top -2px
		off := op.Offset(image.Pt(0, y)).Push(gtx.Ops)
		dd := label(gtx, th.Shaper, textStyle{Size: 13, LineHeight: 18, Color: th.C.FgMuted}, d)
		off.Pop()
		y += dd.Size.Y
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// scheduleZoneDetails is getScheduledDraftTimeZoneDetails: "{memberTime}
// for {memberName}" when the DM's other member keeps another time zone.
func scheduleZoneDetails(at time.Time, m *schedMemberInfo, h24 bool) string {
	if m == nil || m.sameTZ || m.loc == nil {
		return ""
	}
	return i18n.T("drafts", "{memberTime} for {memberName}", "memberTime", schedClock(at.In(m.loc), h24), "memberName", m.name)
}

// center lays w centred in the current constraints.
func center(gtx layout.Context, w layout.Widget) layout.Dimensions {
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min = image.Point{}
		return w(gtx)
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}
