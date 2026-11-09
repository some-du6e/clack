package popups

import (
	"context"
	"image"
	"image/color"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Slack's "Schedule Huddle" modal (HuddleScheduleInternalModal in
// client-boot-shared, opened by openHuddleScheduleModalUsingSideEffect from
// the header caret's Schedule Huddle). 560 wide:
//
//   - "Connect a calendar to Slack to schedule a huddle." (boxed info)
//     until calendar.user.status / calendar.getConnectedCalendars find a
//     writable calendar; Schedule stays disabled without one.
//   - Title ("Add a title"; channels prefill "#name huddle").
//   - When: a date select, then start "to" end and a 112px time zone
//     select. Start defaults to the next half hour, 30 minutes long; the
//     start list steps by 15 minutes, the end list 48 slots of 15 after it.
//   - Participants: in a channel "Invite everyone in #name" and a people
//     picker ("Start typing a name…"), hint "Anyone in #name will see this
//     huddle, and people you add will get an invite."; DMs invite everyone
//     ("Everyone in this conversation will get an invite.").
//   - Description (optional): "Any details for the invite?".
//   - Schedule → huddles.scheduled.new {channel_id, scheduled_time,
//     duration (s), invite_all_channel_members | invitees, topic,
//     description, reason "schedule-new"}; ok → "<title> scheduled" (or
//     "Huddle scheduled") and close; errors map to Slack's messages.
//
// Differences: the date is the calendar popover (Slack's is a select of
// days), the end list stops at midnight, and the members-not-in-channel
// warning and user groups aren't there.

const (
	hsTitle        = "Schedule Huddle"
	hsNoCalendar   = "Connect a calendar to Slack to schedule a huddle."
	hsEveryoneDM   = "Everyone in this conversation will get an invite."
	hsPlaceholder  = "Start typing a name…"
	hsDescPrompt   = "Any details for the invite?"
	hsTitlePrompt  = "Add a title"
	hsScheduleBtn  = "Schedule"
	hsScheduledAny = "Huddle scheduled"
)

// hsErrors are Slack's messages for huddles.scheduled.new errors.
var hsErrors = map[string]string{
	"scheduling_huddle_in_past":      "The scheduled time must be in the future.",
	"schedule_hudding_in_past":       "The scheduled time must be in the future.",
	"invalid_duration":               "The selected duration is invalid.",
	"too_many_invitees":              "Too many people to invite to one huddle. Remove some and try again.",
	"channel_not_found":              "You can only schedule a huddle in a channel you’re a member of.",
	"restricted_action":              "Only people who can post in this channel can schedule a huddle in it.",
	"cannot_huddle_here":             "You can’t schedule a huddle in this conversation.",
	"slack_connect_not_supported":    "You can’t schedule huddles in channels shared with other organizations yet.",
	"permission_denied":              "Huddles are turned off for this channel.",
	"invite_restricted":              "Your organization doesn’t allow everyone in this channel to join the same huddle.",
	"calendar_not_connected":         hsNoCalendar,
	"calendar_event_creation_failed": "There was a problem adding this huddle to your calendar. Give it another try in a few minutes.",
}

const hsUnknownErr = "Something went wrong. If the problem persists, please drop us a line."

// hsZones are the time zone select's choices besides yours.
var hsZones = []string{"UTC", "America/Los_Angeles", "America/Denver", "America/Chicago", "America/New_York",
	"America/Sao_Paulo", "Europe/London", "Europe/Berlin", "Africa/Cairo", "Asia/Kolkata", "Asia/Singapore",
	"Asia/Tokyo", "Australia/Sydney"}

// HuddleScheduleModal is the Schedule Huddle dialog for one conversation.
type HuddleScheduleModal struct {
	h     Host
	ch    string
	name  string // "#general", or the DM's title
	dm    bool   // a DM or group DM: everyone is invited
	modal *Modal
	btn   *ModalButton

	titleEd, descEd, pickEd widget.Editor
	descBox                 widget.Clickable
	focused                 bool

	day                       dtDate
	start, dur                int // minutes after midnight; minutes long
	loc                       *time.Location
	startBtn, endBtn, zoneBtn widget.Clickable
	startR, endR, zoneR       image.Rectangle
	list                      Overlay

	inviteAll bool
	allBtn    widget.Clickable
	query     string
	gen       int
	results   []core.User
	rowBtn    []widget.Clickable
	picked    []core.User
	chipBtn   []widget.Clickable

	calLoaded, calOK bool
	busy             bool
	errMsg           string
}

// NewHuddleScheduleModal opens Schedule Huddle for channel ch.
func NewHuddleScheduleModal(h Host, ch string) *HuddleScheduleModal {
	s := &HuddleScheduleModal{h: h, ch: ch, dur: 30, loc: time.Local}
	s.titleEd.SingleLine, s.pickEd.SingleLine, s.pickEd.Submit = true, true, true
	if a := h.Core(); a != nil {
		if cv, ok := a.Conv(ch); ok {
			s.dm = cv.Kind == "im" || cv.Kind == "mpim"
			s.name = cv.Name
			if !s.dm {
				s.name = "#" + cv.Name
			} else {
				s.name = convTitle(h, cv)
			}
		}
		if tz := a.Self().TZ; tz != "" {
			if l, err := time.LoadLocation(tz); err == nil {
				s.loc = l
			}
		}
	}
	if !s.dm && s.name != "" {
		s.titleEd.SetText(s.name + " huddle")
	}
	now := time.Now().In(s.loc)
	st := dtCeil(now, 30)
	s.day = dtDate{Day: dtDay(st), Min: dtDay(now), OnChange: s.clampStart}
	s.start = dtMins(st)
	s.btn = &ModalButton{Label: hsScheduleBtn, Kind: BtnPrimary,
		Enabled: func() bool { return s.calLoaded && s.calOK && !s.busy && s.when().After(time.Now()) },
		Do:      func() bool { s.submit(); return false }}
	s.modal = NewModal(h, ModalOpts{Title: hsTitle, Width: 560, Body: s.layout, Buttons: []*ModalButton{s.btn}, Class: "p-huddle_schedule_modal"})
	s.loadCalendar()
	return s
}

// Layout, Dismissed and Dismiss make it an Overlay.
func (s *HuddleScheduleModal) Layout(gtx layout.Context) layout.Dimensions {
	return s.modal.Layout(gtx)
}
func (s *HuddleScheduleModal) Dismissed() bool { return s.modal.Dismissed() }
func (s *HuddleScheduleModal) Dismiss()        { s.modal.Dismiss() }

// CalendarReady reports whether a writable calendar was found (Schedule
// is enabled).
func (s *HuddleScheduleModal) CalendarReady() bool { return s.calLoaded && s.calOK }

// Error is the banner's error ("" none).
func (s *HuddleScheduleModal) Error() string { return s.errMsg }

// SetTitle replaces the title.
func (s *HuddleScheduleModal) SetTitle(t string) { s.titleEd.SetText(t) }

// Pick adds a person to invite.
func (s *HuddleScheduleModal) Pick(u core.User) {
	for _, q := range s.picked {
		if q.ID == u.ID {
			return
		}
	}
	s.picked = append(s.picked, u)
	s.pickEd.SetText("")
	s.query, s.results = "", nil
	s.errMsg = ""
}

// SetInviteAll ticks "Invite everyone in #channel".
func (s *HuddleScheduleModal) SetInviteAll(on bool) { s.inviteAll = on }

// Submit presses Schedule.
func (s *HuddleScheduleModal) Submit() { s.modal.press(s.btn) }

func (s *HuddleScheduleModal) when() time.Time {
	d := s.day.Day
	return time.Date(d.Year(), d.Month(), d.Day(), 0, s.start, 0, 0, s.loc)
}

// clampStart keeps the start in the future after the day changes.
func (s *HuddleScheduleModal) clampStart() {
	if now := time.Now().In(s.loc); dtSameDay(s.day.Day, now) && s.start < dtMins(now) {
		s.start = dtMins(dtCeil(now, 15))
	}
}

// loadCalendar is Slack's useWritableCalendar: calendar.user.status, then
// calendar.getConnectedCalendars for each connected provider.
func (s *HuddleScheduleModal) loadCalendar() {
	run(s.h, func(ctx context.Context) (bool, error) {
		a := s.h.Core()
		if a == nil {
			return false, context.Canceled
		}
		var st struct {
			Status []struct {
				App    string `json:"cal_app_type"`
				Status string `json:"user_status"`
			} `json:"status"`
		}
		if err := a.C.Call(ctx, "calendar.user.status", nil, &st); err != nil {
			return false, err
		}
		for _, c := range st.Status {
			if c.Status != "connected" {
				continue
			}
			var cc struct {
				Calendars []any `json:"connected_calendars"`
			}
			if a.C.Call(ctx, "calendar.getConnectedCalendars", url.Values{"cal_app_type": {c.App}}, &cc) == nil && len(cc.Calendars) > 0 {
				return true, nil
			}
		}
		return false, nil
	}, func(ok bool, _ error) {
		s.calLoaded, s.calOK = true, ok
	})
}

func (s *HuddleScheduleModal) submit() {
	if s.busy || !s.btn.enabled() {
		return
	}
	var ids []string
	all := s.dm || s.inviteAll
	self := ""
	if a := s.h.Core(); a != nil {
		self = a.Self().ID
	}
	if !all {
		for _, u := range s.picked {
			if u.ID != self {
				ids = append(ids, u.ID)
			}
		}
	}
	p := url.Values{"channel_id": {s.ch}, "scheduled_time": {strconv.FormatInt(s.when().Unix(), 10)},
		"duration": {strconv.Itoa(60 * s.dur)}, "reason": {"schedule-new"}}
	if all {
		p.Set("invite_all_channel_members", "true")
	} else if len(ids) > 0 {
		p.Set("invitees", strings.Join(ids, ","))
	}
	title := strings.TrimSpace(s.titleEd.Text())
	if title != "" {
		p.Set("topic", title)
	}
	if d := s.descEd.Text(); d != "" {
		p.Set("description", d)
	}
	s.busy, s.errMsg = true, ""
	run(s.h, func(ctx context.Context) (struct{}, error) {
		a := s.h.Core()
		if a == nil {
			return struct{}{}, context.Canceled
		}
		var res struct {
			ICalUID string `json:"icaluid"`
		}
		return struct{}{}, a.C.Call(ctx, "huddles.scheduled.new", p, &res)
	}, func(_ struct{}, err error) {
		s.busy = false
		if err != nil {
			s.errMsg = hsErrText(err)
			return
		}
		if title != "" {
			s.h.Toast(title + " scheduled")
		} else {
			s.h.Toast(hsScheduledAny)
		}
		s.modal.Dismiss()
	})
}

func hsErrText(err error) string {
	if se, ok := err.(*slack.Error); ok {
		if m, ok := hsErrors[se.Code]; ok {
			return m
		}
	}
	return hsUnknownErr
}

func (s *HuddleScheduleModal) search(q string) {
	s.query = q
	s.gen++
	gen := s.gen
	if strings.TrimSpace(q) == "" {
		s.results = nil
		return
	}
	run(s.h, func(ctx context.Context) ([]core.User, error) {
		a := s.h.Core()
		if a == nil {
			return nil, context.Canceled
		}
		return a.SearchUsers(ctx, q, 20)
	}, func(us []core.User, err error) {
		if gen != s.gen || err != nil {
			return
		}
		out := us[:0:0]
		for _, u := range us {
			if u.IsBot || u.Deleted {
				continue
			}
			if out = append(out, u); len(out) == 6 {
				break
			}
		}
		s.results = out
	})
}

func (s *HuddleScheduleModal) listOpen() bool { return s.list != nil && !s.list.Dismissed() }

func (s *HuddleScheduleModal) events(gtx layout.Context) {
	if !s.focused {
		gtx.Execute(key.FocusCmd{Tag: &s.titleEd})
		s.focused = true
	}
	for {
		e, ok := s.pickEd.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			if t := s.pickEd.Text(); t != s.query {
				s.search(t)
			}
		case widget.SubmitEvent:
			if len(s.results) > 0 {
				s.Pick(s.results[0])
			}
		}
	}
	for {
		if _, ok := s.titleEd.Update(gtx); !ok {
			break
		}
	}
	for {
		if _, ok := s.descEd.Update(gtx); !ok {
			break
		}
	}
	if s.descBox.Clicked(gtx) {
		gtx.Execute(key.FocusCmd{Tag: &s.descEd})
	}
	for i := range s.rowBtn {
		if i < len(s.results) && s.rowBtn[i].Clicked(gtx) {
			s.Pick(s.results[i])
			break
		}
	}
	for i := range s.chipBtn {
		if i < len(s.picked) && s.chipBtn[i].Clicked(gtx) {
			s.picked = append(s.picked[:i:i], s.picked[i+1:]...)
			break
		}
	}
	if s.allBtn.Clicked(gtx) {
		s.inviteAll = !s.inviteAll
		s.errMsg = ""
	}
	if s.startBtn.Clicked(gtx) && !s.listOpen() {
		from := 0
		if now := time.Now().In(s.loc); dtSameDay(s.day.Day, now) {
			from = dtMins(now)
		}
		s.list = dtNewTimeListStep(s.h, s.startR, s.start, from, 15, func(m int) { s.start = m; s.h.Invalidate() })
		s.h.ShowOverlay(s.list)
	}
	if s.endBtn.Clicked(gtx) && !s.listOpen() {
		s.list = dtNewTimeListStep(s.h, s.endR, s.start+s.dur, s.start+15, 15, func(m int) {
			if m > s.start {
				s.dur = m - s.start
			}
			s.h.Invalidate()
		})
		s.h.ShowOverlay(s.list)
	}
	if s.zoneBtn.Clicked(gtx) && !s.listOpen() {
		var items []MenuItem
		names := append([]string{s.loc.String()}, hsZones...)
		seen := map[string]bool{}
		for _, n := range names {
			if seen[n] {
				continue
			}
			seen[n] = true
			l, err := time.LoadLocation(n)
			if err != nil {
				continue
			}
			abbr, _ := time.Now().In(l).Zone()
			items = append(items, MenuItem{Label: abbr + "  " + strings.ReplaceAll(n, "_", " "), Checked: n == s.loc.String(), Do: func() {
				s.loc = l
				s.clampStart()
			}})
		}
		m := NewMenuOpts(s.h, items, MenuOpts{Width: 280, Anchor: s.zoneR, Side: Below})
		s.list = m
		s.h.ShowOverlay(m)
	}
}

func (s *HuddleScheduleModal) layout(gtx layout.Context) layout.Dimensions {
	s.events(gtx)
	th := s.h.Theme()
	cl := colorsOf(th)
	org := dtModalOrigin(gtx, s.modal)
	w := gtx.Constraints.Max.X
	y := 0
	gap := dp(gtx, 16)
	label := func(t string) {
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, t)
		})
		y += dp(gtx, 26)
	}
	banner := func(t string, c color.NRGBA) {
		_, d := record(loose(gtx, image.Pt(w-dp(gtx, 32), 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, t, 0)
		})
		bh := d.Size.Y + dp(gtx, 24)
		rrect(gtx, image.Rect(0, y, w, y+bh), dp(gtx, 8), theme.WithAlpha(c, 0x1a))
		fill(gtx, image.Rect(0, y+dp(gtx, 4), dp(gtx, 4), y+bh-dp(gtx, 4)), c)
		at(gtx, image.Pt(dp(gtx, 16), y+dp(gtx, 12)), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 32), 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, t, 0)
		})
		y += bh + gap
	}
	if s.calLoaded && !s.calOK {
		banner(hsNoCalendar, cl.Link)
	}
	if s.errMsg != "" {
		banner(s.errMsg, cl.Danger)
	}

	// Title
	label("Title")
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, dp(gtx, 40)))
		return field(gtx, th, &s.titleEd, hsTitlePrompt, "", 40, 15)
	})
	y += dp(gtx, 40) + gap

	// When: date, then start "to" end + time zone
	label("When")
	fh := dtHeight(gtx, dtLarge)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return s.day.Layout(gtx, s.h, org.Add(image.Pt(0, y)), w, dtLarge)
	})
	y += fh + dp(gtx, 8)
	toW := textW(gtx, th, 15, theme.Bold, "to")
	zw := dp(gtx, 112)
	tw := (w - zw - toW - 3*dp(gtx, 8)) / 2
	x := 0
	s.startR = image.Rectangle{Min: org.Add(image.Pt(x, y)), Max: org.Add(image.Pt(x+tw, y+fh))}
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		return dtField(gtx, th, &s.startBtn, tw, dtLarge, "", dtTimeLabel(s.start), true, false, false)
	})
	x += tw + dp(gtx, 8)
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, fh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, "to")
		})
	})
	x += toW + dp(gtx, 8)
	s.endR = image.Rectangle{Min: org.Add(image.Pt(x, y)), Max: org.Add(image.Pt(x+tw, y+fh))}
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		return dtField(gtx, th, &s.endBtn, tw, dtLarge, "", dtTimeLabel((s.start+s.dur)%(24*60)), true, false, false)
	})
	x += tw + dp(gtx, 8)
	s.zoneR = image.Rectangle{Min: org.Add(image.Pt(x, y)), Max: org.Add(image.Pt(x+zw, y+fh))}
	abbr, _ := s.when().Zone()
	at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
		return dtField(gtx, th, &s.zoneBtn, zw, dtLarge, "", abbr, true, false, false)
	})
	y += fh + gap

	// Participants
	if !s.dm {
		label("Participants")
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &s.allBtn, nil, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(0, dp(gtx, 3)), func(gtx layout.Context) layout.Dimensions {
					return Checkbox(gtx, th, s.inviteAll, false, false)
				})
				t := "Invite everyone in " + s.name
				d := at(gtx, image.Pt(dp(gtx, 24), 0), func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, cl.Fg, t)
				})
				return layout.Dimensions{Size: image.Pt(dp(gtx, 24)+d.Size.X, dp(gtx, 22))}
			})
		})
		y += dp(gtx, 22) + dp(gtx, 8)
		if !s.inviteAll {
			y = s.picker(gtx, th, cl, w, y)
			y += dp(gtx, 8)
		}
	}
	hint := hsEveryoneDM
	if !s.dm && !s.inviteAll {
		hint = "Anyone in " + s.name + " will see this huddle, and people you add will get an invite."
	}
	if !s.dm && s.inviteAll {
		hint = hsEveryoneDM
	}
	d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 13, 18, theme.Regular, theme.WithAlpha(cl.Fg, 0xb3), hint, 0)
	})
	y += d.Size.Y + gap

	// Description (optional)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		dl := text(gtx, th, 15, theme.Bold, cl.Fg, "Description")
		at(gtx, image.Pt(dl.Size.X+dp(gtx, 4), 0), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Regular, cl.Fg3, "(optional)")
		})
		return dl
	})
	y += dp(gtx, 26)
	y += s.descField(gtx, th, cl, w, y)
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 8))}
}

// descField is the multi-line description input.
func (s *HuddleScheduleModal) descField(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int) int {
	padX, padY := dp(gtx, 12), dp(gtx, 10)
	tw := maxInt(1, w-2*padX)
	content, cd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, dp(gtx, 160))}
		if s.descEd.Len() == 0 {
			call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Placeholder, hsDescPrompt)
			})
			call.Add(gtx.Ops)
		}
		return th.Editor(gtx, &s.descEd, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
	})
	hh := maxInt(cd.Size.Y+2*padY, dp(gtx, 88))
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		r := image.Rect(0, 0, w, hh)
		actFocusBox(gtx, cl, r, gtx.Focused(&s.descEd))
		s.descBox.Layout(exact(gtx, r.Max), func(gtx layout.Context) layout.Dimensions {
			defer clip.Rect(r).Push(gtx.Ops).Pop()
			pointer.CursorText.Add(gtx.Ops)
			return layout.Dimensions{Size: r.Max}
		})
		macroAt(gtx, content, image.Pt(padX, padY))
		return layout.Dimensions{Size: r.Max}
	})
	return hh
}

// picker is the invitee picker: chips for the people picked, the input and
// its results. It returns the y after it.
func (s *HuddleScheduleModal) picker(gtx layout.Context, th *theme.Theme, cl *Colors, w, y int) int {
	if len(s.picked) > 0 {
		for len(s.chipBtn) < len(s.picked) {
			s.chipBtn = append(s.chipBtn, widget.Clickable{})
		}
		x, rowH := 0, dp(gtx, 28)
		for i, u := range s.picked {
			name := userName(u)
			cw := dp(gtx, 30) + textW(gtx, th, 15, theme.Bold, name) + dp(gtx, 28)
			if x > 0 && x+cw > w {
				x, y = 0, y+rowH+dp(gtx, 6)
			}
			at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
				return clickBox(gtx, &s.chipBtn[i], func(gtx layout.Context, sz image.Point, hovered bool) {
					a := uint8(0x1a)
					if hovered {
						a = 0x2a
					}
					rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 4), theme.WithAlpha(cl.Link, a))
				}, func(gtx layout.Context) layout.Dimensions {
					at(gtx, image.Pt(dp(gtx, 4), dp(gtx, 4)), func(gtx layout.Context) layout.Dimensions {
						return avatar(gtx, s.h, u.Avatar, 20)
					})
					at(gtx, image.Pt(dp(gtx, 30), 0), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
							return text(gtx, th, 15, theme.Bold, cl.Link, name)
						})
					})
					at(gtx, image.Pt(cw-dp(gtx, 22), (rowH-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
						return icon(gtx, "close", 16, cl.Link)
					})
					return layout.Dimensions{Size: image.Pt(cw, rowH)}
				})
			})
			x += cw + dp(gtx, 6)
		}
		y += rowH + dp(gtx, 8)
	}
	ih := dp(gtx, 40)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, ih))
		return field(gtx, th, &s.pickEd, hsPlaceholder, "", 40, 15)
	})
	y += ih
	if len(s.results) == 0 {
		return y
	}
	for len(s.rowBtn) < len(s.results) {
		s.rowBtn = append(s.rowBtn, widget.Clickable{})
	}
	y += dp(gtx, 4)
	rh := dp(gtx, 36)
	box := image.Rect(0, y, w, y+rh*len(s.results)+dp(gtx, 12))
	card(gtx, cl, box, dp(gtx, 8))
	ry := y + dp(gtx, 6)
	for i, u := range s.results {
		at(gtx, image.Pt(0, ry), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(w, rh))
			return clickBox(gtx, &s.rowBtn[i], func(gtx layout.Context, sz image.Point, hovered bool) {
				if hovered || i == 0 {
					fill(gtx, image.Rectangle{Max: sz}, cl.Highlight)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				fg := cl.Fg
				if s.rowBtn[i].Hovered() || i == 0 {
					fg = cl.HighText
				}
				at(gtx, image.Pt(dp(gtx, 16), (rh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
					return avatar(gtx, s.h, u.Avatar, 20)
				})
				at(gtx, image.Pt(dp(gtx, 44), 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 60), rh)}
					return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Bold, fg, userName(u))
					})
				})
				return layout.Dimensions{Size: image.Pt(w, rh)}
			})
		})
		ry += rh
	}
	return box.Max.Y
}
