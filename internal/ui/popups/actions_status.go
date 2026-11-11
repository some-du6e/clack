package popups

import (
	"context"
	"image"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// "Set a status" modal (status.png, status_auto2.png, ooo.png): 520 wide,
// at most ~534 tall (header 70, footer 85 with a top border); the WHOLE
// body scrolls (the input too) under the fixed footer. 42px input (radius
// 8, focus ring 1px #1264a3 + 4px) with a leading emoji button, 18px text,
// placeholder "What's your status?", clear button when filled.
//
// Empty: sections (legend 13/700 rgba(29,28,29,.7), padding 24 0 8 36 from
// the modal's left edge), rows 28px: emoji 22 at x=36, **text** 15/700 +
// " — duration" 15/400 #616061 at x=68; hover highlights the full modal
// width #1264a3 with white text. "Schedule for later" (Out of office), "For
// <team>" (team presets, corex cxStatusPresets), "Automatically updates"
// (calendar).
//
// Filled: "Duration" 15/700, a 44px select (radius 8, 18px value,
// chevron), for "Choose a start and end time…" a #f8f8f8 box (radius 0 0 8
// 8, starting 28px below the select's top) with Start/End rows of date +
// time fields, then "Pause notifications until status is cleared". Out of
// office adds "Automatic reply:" with an editor and a preview.

type actDur int

const (
	actDurNone actDur = iota // Don't clear
	actDur30m
	actDur1h
	actDur4h
	actDurToday
	actDurWeek
	actDurKeep   // keep the current status's expiration
	actDurCustom // Choose a start and end time…
)

var actDurLabels = [...]string{"Don’t clear", "30 minutes", "1 hour", "4 hours", "Today", "This week", "", "Choose a start and end time…"}

// actDurNotes are the preset row suffixes (" — Today"), built once.
var actDurNotes = func() (n [len(actDurLabels)]string) {
	for i, l := range actDurLabels {
		n[i] = " — " + l
	}
	return
}()

type actPreset struct {
	emoji, text string
	canonical   string // text_canonical (the default presets: their text)
	dur         actDur
}

// actPresets are Slack's enterprise defaults, used when the workspace has
// no presets or team.prefs.get fails.
var actPresets = []actPreset{
	{"calendar", "In a meeting", "In a meeting", actDur1h},
	{"bus", "Commuting", "Commuting", actDur30m},
	{"face_with_thermometer", "Out sick", "Out sick", actDurToday},
	{"palm_tree", "Vacationing", "Vacationing", actDurNone},
	{"house_with_garden", "Working remotely", "Working remotely", actDurToday},
}

// actPresetDur maps a team preset duration code to a duration.
func actPresetDur(code string) actDur {
	switch code {
	case "30_minutes":
		return actDur30m
	case "1_hour":
		return actDur1h
	case "4_hours":
		return actDur4h
	case "all_day", "today":
		return actDurToday
	case "week", "this_week": // Slack's key is "week" (/Ab5 WEEK)
		return actDurWeek
	}
	return actDurNone // no_expiration and anything unknown
}

// Team presets, fetched once per process (UI goroutine only).
var (
	actPresetCache   []actPreset // nil until loaded
	actPresetFailed  bool        // last fetch failed: use the defaults
	actPresetPending bool
)

// actFetchPresets starts loading the team presets unless cached/in flight.
func actFetchPresets(h Host) {
	if actPresetCache != nil || actPresetPending {
		return
	}
	if h.Core() == nil {
		actPresetFailed = true
		return
	}
	actPresetPending, actPresetFailed = true, false
	run(h, func(ctx context.Context) ([]cxPreset, error) { return cxStatusPresets(ctx, h) }, func(ps []cxPreset, err error) {
		actPresetPending = false
		defer h.Invalidate()
		if err != nil {
			actPresetFailed = true
			return
		}
		if len(ps) == 0 {
			actPresetCache = actPresets
			return
		}
		out := make([]actPreset, 0, len(ps))
		for _, p := range ps {
			out = append(out, actPreset{emoji: p.Emoji, text: p.Text, canonical: p.Canonical, dur: actPresetDur(p.Dur)})
		}
		actPresetCache = out
	})
}

// actCurrentPresets returns the presets to show (ok false while loading).
func actCurrentPresets() ([]actPreset, bool) {
	if actPresetCache != nil {
		return actPresetCache, true
	}
	if actPresetFailed {
		return actPresets, true
	}
	return nil, false
}

// actStatusExpiry is the status_expiration (unix seconds, 0 = never) for
// duration d chosen at now; keep is the existing expiration for actDurKeep.
func actStatusExpiry(now time.Time, d actDur, keep int64) int64 {
	y, m, day := now.Date()
	loc := now.Location()
	switch d {
	case actDur30m:
		return now.Add(30 * time.Minute).Unix()
	case actDur1h:
		return now.Add(time.Hour).Unix()
	case actDur4h:
		return now.Add(4 * time.Hour).Unix()
	case actDurToday:
		return time.Date(y, m, day, 23, 59, 59, 0, loc).Unix()
	case actDurWeek:
		// end of Friday (getDurationPresetTs WEEK: day(getIndexOfNextDayOfWeek(5))):
		// this week's Friday, next week's on a Saturday.
		days := 5 - int(now.Weekday())
		if days < 0 {
			days += 7
		}
		return time.Date(y, m, day+days, 23, 59, 59, 0, loc).Unix()
	case actDurKeep:
		return keep
	}
	return 0
}

// actKeepLabel describes an existing expiration ("Until 5:00 PM").
func actKeepLabel(exp int64, now time.Time) string {
	t := time.Unix(exp, 0).In(now.Location())
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return "Until " + clock(t)
	}
	return "Until " + t.Format("Jan ") + ordinal(d1) + " at " + clock(t)
}

// actRangeDefaults are the Start/End defaults Slack shows
// (CustomStatusScheduledDuration): start now at the start of the minute
// (getDefaultStartTime), end 24 hours later, moved up to the next whole hour
// unless it is on one (2:57 PM → tomorrow 3:00 PM, 3:12 PM → tomorrow
// 4:00 PM).
func actRangeDefaults(now time.Time) (start, end time.Time) {
	start = now.Truncate(time.Minute)
	end = start.Add(24 * time.Hour)
	if end.Minute() != 0 { // by the wall clock (Truncate would use UTC hours)
		y, m, d := end.Date()
		end = time.Date(y, m, d, end.Hour()+1, 0, 0, 0, end.Location())
	}
	return start, end
}

// actOOOReply is the prefilled automatic reply for an out-of-office status
// ending at end: "I’m out of office until {date}." (OutOfOfficeMessageContainer).
func actOOOReply(end, now time.Time) string {
	return "I’m out of office until " + actOOODate(end, now) + "."
}

// actOOODate is Slack's v4SM date for the reply (en-US): just the time
// today; for an end at 11:59 PM the day after ("tomorrow", "next week" for
// this Friday's, else "Sat, Nov 7"); a week or more away "Sat, Nov 14";
// otherwise "3:00 PM, Sat, Nov 7".
func actOOODate(end, now time.Time) string {
	end = end.In(now.Location())
	day := func(t time.Time) string { return t.Format("Mon, Jan 2") }
	if end.Hour() == 23 && end.Minute() == 59 {
		next := end.AddDate(0, 0, 1)
		if dtSameDay(end, now) {
			return "tomorrow"
		}
		fri := 5 - int(now.Weekday())
		if fri < 0 {
			fri += 7
		}
		if dtSameDay(end, now.AddDate(0, 0, fri)) {
			return "next week"
		}
		return day(next)
	}
	if dtSameDay(end, now) {
		return clock(end)
	}
	if !end.Before(now.AddDate(0, 0, 7)) {
		return day(end)
	}
	return clock(end) + ", " + day(end)
}

// actDurKey is the status_duration Slack saves for d ("" leaves it out).
func actDurKey(d actDur) string {
	switch d {
	case actDurNone:
		return "no_expiration"
	case actDur30m:
		return "30_minutes"
	case actDur1h:
		return "1_hour"
	case actDur4h:
		return "4_hours"
	case actDurToday:
		return "all_day"
	case actDurWeek:
		return "week"
	case actDurCustom:
		return "custom"
	}
	return ""
}

// actBodyMax caps the scrolling body: 534 max modal - 70 header - 85 footer.
const actBodyMax = 379

type actStatus struct {
	h       Host
	modal   *Modal
	ed      widget.Editor
	emoji   string
	dur     actDur
	keepExp int64
	pause   bool
	had     bool
	forTeam string // "For Hack Club"
	focus   bool   // focus the input on the next layout
	showTip bool   // showKeyboardHint: the "Tip:" pill under the modal
	pill    *actTipPill

	presets   []actPreset // nil while loading
	presetBtn []widget.Clickable

	ooo          bool // Out of office (adds the automatic reply)
	rangeInit    bool
	sDay, eDay   dtDate
	sTime, eTime dtTime
	reply        widget.Editor
	replyGen     string // the generated reply (unedited while equal)
	replyText    string // reply's current text
	replyInit    string // a saved reply the editor was filled with ("" none)
	replyFixed   bool   // filled with a saved reply: never regenerated
	selfName     string
	selfAv       string

	canonical string // the chosen preset's text_canonical
	presetDnd bool   // a preset that pauses notifications (Vacationing)
	picked    bool   // a preset was chosen (its presetDnd replaces the snooze state)
	oooSaved  string // the saved automatic reply (profile ooo_message), as text

	sched    []actScheduled // future scheduled statuses (users.customStatus.list)
	schedBtn []widget.Clickable
	schedDel []widget.Clickable
	editID   string // the scheduled status being edited
	editing  *actScheduled
	saveBtn  *ModalButton // "Save", "Update" while editing a scheduled status

	list layout.List // the scrolling body

	emojiBtn, clearBtn, selBtn, pauseBtn, oooBtn widget.Clickable

	btns []*ModalButton // the form's footer
	cal  actCalState    // "Automatically updates" and the calendar step

	// semantic_emoji_matching (status_semantic.go): emojiPicked is the
	// modal's tl ref (an emoji chosen by hand), semAt when the debounced
	// match runs
	emojiPicked bool
	semAt       time.Time
	semSeen     string // the text a5 last saw (SetText's change isn't typing)

	emojiRect, selRect image.Rectangle // content coords, last layout
}

// NewSetStatusModal builds the "Set a status" modal (Slack's
// openCustomStatusDialog), prefilled with the viewer's current status.
// Show it with h.ShowOverlay. While the account pref
// used_custom_status_kb_shortcut is unset it shows Slack's "Tip: Open this
// faster with Ctrl + Shift + Y" pill under the modal (showKeyboardHint).
func NewSetStatusModal(h Host) *Modal { return actNewStatus(h, false) }

// NewSetStatusModalFromShortcut is the modal as Mod+Shift+Y opens it
// (fromKeyboardShortcut): the caller saves used_custom_status_kb_shortcut,
// which Slack sets locally before the modal renders, so it has no tip.
func NewSetStatusModalFromShortcut(h Host) *Modal { return actNewStatus(h, true) }

func actNewStatus(h Host, fromShortcut bool) *Modal { return actNewStatusState(h, fromShortcut).modal }

func actNewStatusState(h Host, fromShortcut bool) *actStatus {
	s := &actStatus{h: h, dur: actDurToday, focus: true, forTeam: "For Hack Club"}
	s.showTip = !fromShortcut && (h.Core() == nil || !h.Core().PrefBool("used_custom_status_kb_shortcut", false))
	s.ed.SingleLine = true
	s.ed.Submit = true
	s.list.Axis = layout.Vertical
	s.rangeHooks()
	if a := h.Core(); a != nil {
		me := a.Self()
		s.selfName, s.selfAv = userName(me), me.Avatar
		if me.StatusText != "" || me.StatusEmoji != "" {
			s.had = true
			s.ed.SetText(me.StatusText)
			s.emoji = strings.Trim(me.StatusEmoji, ":")
			s.emojiPicked = s.emoji != "" // tl = useRef(!!emoji)
			s.semSeen = s.ed.Text()
			if me.StatusExp == 0 {
				s.dur = actDurNone
			} else {
				s.dur, s.keepExp = actDurKeep, me.StatusExp
			}
			if u, ok := a.S.User(me.ID); ok {
				s.canonical = u.Profile.StatusTextCanonical
				s.ooo = s.canonical == actOOOCanonical // hasOooSet
			}
		}
		s.load(a)
	}
	if s.selfName == "" {
		s.selfName = "You"
	}
	if snap, ok := actSnapshot(h); ok && snap.Team.Name != "" {
		s.forTeam = "For " + snap.Team.Name
	}
	actFetchPresets(h)
	s.takePresets()
	var btns []*ModalButton
	if s.had {
		btns = append(btns, &ModalButton{Label: "Clear Status", Kind: BtnOutline, Do: func() bool {
			s.clearStatus()
			return true
		}})
	}
	s.saveBtn = &ModalButton{Label: "Save", Kind: BtnPrimary, Enabled: s.canSave, Do: s.save}
	btns = append(btns, &ModalButton{Label: "Cancel", Kind: BtnOutline}, s.saveBtn)
	title := func() string {
		if s.editID != "" {
			return "Edit status"
		}
		return "Set a status"
	}
	s.btns = btns
	s.modal = NewModal(h, ModalOpts{TitleFunc: title, Body: s.layout, Buttons: btns, Border: true, Over: s.calFooter})
	actFetchCalendars(h) // useMountEffect: getCurrentCalendarAndUser
	if s.ooo {
		s.fillReply()
	}
	return s
}

// load fetches what the modal shows beyond core's state: the scheduled
// statuses, the saved automatic reply and whether the current status's
// expiration matches a notification pause (Slack's
// doesExpirationMatchDndEnd pre-checks "Pause notifications").
func (s *actStatus) load(a *core.App) {
	h := s.h
	exp := s.keepExp
	type loaded struct {
		sched     []actScheduled
		schedErr  error
		reply     string
		canonical string
		profErr   error
		paused    bool
	}
	run(h, func(ctx context.Context) (loaded, error) {
		var l loaded
		l.sched, l.schedErr = actListScheduled(ctx, a, time.Now().Unix())
		l.reply, l.canonical, l.profErr = actSelfOOO(ctx, a)
		if exp > 0 {
			if st, err := actDNDInfo(ctx, a); err == nil {
				l.paused = actExpMatchesDND(exp, st.SnoozeEnd)
			}
		}
		return l, nil
	}, func(l loaded, _ error) {
		defer h.Invalidate()
		if s.modal.done {
			return
		}
		if l.schedErr == nil {
			s.setSched(l.sched)
		}
		if l.profErr == nil {
			s.oooSaved = l.reply
			if s.had && l.canonical != "" {
				s.canonical = l.canonical
				s.ooo = l.canonical == actOOOCanonical
			}
			if s.ooo && s.editID == "" && !s.replyFixed && s.replyText == s.replyGen { // not typed in yet
				s.fillReply()
			}
		}
		if l.paused {
			s.pause = true
		}
	})
}

// reloadSched refetches the scheduled statuses.
func (s *actStatus) reloadSched(a *core.App) {
	run(s.h, func(ctx context.Context) ([]actScheduled, error) { return actListScheduled(ctx, a, time.Now().Unix()) }, func(l []actScheduled, err error) {
		if err == nil {
			s.setSched(l)
			s.h.Invalidate()
		}
	})
}

func (s *actStatus) setSched(list []actScheduled) {
	s.sched = list
	if len(s.schedBtn) != len(list) {
		s.schedBtn = make([]widget.Clickable, len(list))
		s.schedDel = make([]widget.Clickable, len(list))
	}
}

// fillReply fills the automatic reply as OutOfOfficeMessageContainer does:
// the scheduled status's own when editing one, else the saved reply, else
// the generated "I’m out of office until …" that follows the end time until
// edited.
func (s *actStatus) fillReply() {
	txt := s.oooSaved
	if s.editing != nil {
		txt = actOOOPlain(s.editing.OOOMessage)
	}
	if txt != "" {
		s.reply.SetText(txt)
		s.replyInit, s.replyText, s.replyFixed = txt, txt, true
		return
	}
	s.replyInit, s.replyFixed = "", false
	s.replyGen, s.replyText = "", ""
	s.reply.SetText("")
	s.rangeChanged()
}

func (s *actStatus) takePresets() {
	if s.presets != nil {
		return
	}
	if ps, ok := actCurrentPresets(); ok {
		s.presets = ps
		s.presetBtn = make([]widget.Clickable, len(ps))
	}
}

// rangeHooks wires the Start/End fields (once).
func (s *actStatus) rangeHooks() {
	s.sDay.OnChange = func() {
		s.eDay.Min = s.sDay.Day
		if s.eDay.Day.Before(s.sDay.Day) {
			s.eDay.Day = s.sDay.Day
		}
		s.rangeChanged()
	}
	s.sTime.OnChange = s.rangeChanged
	s.eDay.OnChange = s.rangeChanged
	s.eTime.OnChange = s.rangeChanged
	s.sTime.From = func() int {
		if now := time.Now(); dtSameDay(s.sDay.Day, now) {
			return dtMins(now)
		}
		return 0
	}
	s.eTime.From = func() int {
		from := 0
		if now := time.Now(); dtSameDay(s.eDay.Day, now) {
			from = dtMins(now)
		}
		if dtSameDay(s.eDay.Day, s.sDay.Day) && s.sTime.Mins+1 > from {
			from = s.sTime.Mins + 1
		}
		return from
	}
}

// setRange resets Start/End to Slack's defaults.
func (s *actStatus) setRange(now time.Time) {
	start, end := actRangeDefaults(now)
	s.sDay.Day, s.sTime.Mins = dtDay(start), dtMins(start)
	s.eDay.Day, s.eTime.Mins = dtDay(end), dtMins(end)
	s.sDay.Min, s.eDay.Min = dtDay(now), s.sDay.Day
	s.rangeInit = true
	s.rangeChanged()
}

// rangeChanged keeps an unedited automatic reply in sync with the end.
func (s *actStatus) rangeChanged() {
	if s.replyFixed || s.replyText != s.replyGen {
		return
	}
	gen := ""
	if end := s.replyEnd(); !end.IsZero() {
		gen = actOOOReply(end, time.Now())
	}
	s.reply.SetText(gen)
	s.replyGen, s.replyText = gen, gen
}

// replyEnd is the end the generated reply names (zero: the status doesn't
// clear, and Slack generates none).
func (s *actStatus) replyEnd() time.Time {
	if s.dur == actDurCustom {
		if !s.rangeInit {
			return time.Time{}
		}
		return s.endTime()
	}
	if exp := actStatusExpiry(time.Now(), s.dur, s.keepExp); exp > 0 {
		return time.Unix(exp, 0)
	}
	return time.Time{}
}

func (s *actStatus) startTime() time.Time { return dtAt(s.sDay.Day, s.sTime.Mins) }
func (s *actStatus) endTime() time.Time   { return dtAt(s.eDay.Day, s.eTime.Mins) }

func (s *actStatus) text() string { return strings.TrimSpace(s.ed.Text()) }

func (s *actStatus) canSave() bool {
	filled := s.text() != "" || s.emoji != ""
	if filled && s.dur == actDurCustom {
		end, now := s.endTime(), time.Now()
		if s.editID == "" && len(s.sched) >= actMaxScheduled && s.startTime().Unix() > now.Unix()+30 {
			return false // the limit alert
		}
		return end.After(s.startTime()) && end.After(now)
	}
	return filled || s.had
}

// clearStatus is Slack's clearUserStatus: no toast, and a pause that
// belonged to the status ends with it.
func (s *actStatus) clearStatus() {
	a := s.h.Core()
	if a == nil {
		s.h.Toast("Not connected yet")
		return
	}
	canonical, exp := s.canonical, s.keepExp
	run(s.h, func(ctx context.Context) (bool, error) { return actClearStatus(ctx, a, canonical, exp) }, func(ended bool, err error) {
		if err == nil && ended {
			actDNDUntil = time.Time{}
		}
	})
}

// save applies the status (handleSubmit / handleSave); false keeps the
// modal open.
func (s *actStatus) save() bool {
	h := s.h
	txt, emoji := s.text(), s.emoji
	if txt == "" && emoji == "" && s.editID == "" {
		s.clearStatus()
		return true
	}
	a := h.Core()
	if a == nil {
		h.Toast("Not connected yet")
		return true
	}
	if s.editID != "" && s.dur != actDurCustom {
		return false // editing a scheduled status saves only with a start (handleSubmit)
	}
	now := time.Now()
	sv := actStatusSave{Emoji: emoji, Text: txt, Canonical: s.canonical, Duration: actDurKey(s.dur), IsDnd: s.pause, EditID: s.editID}
	if s.dur == actDurCustom {
		start, end := s.startTime(), s.endTime()
		if !end.After(start) || !end.After(now) {
			return false // "End time must be in the future." is showing
		}
		sv.Exp = end.Unix()
		if start.Unix() > now.Unix()+30 || s.editID != "" { // isScheduledImmediately: within 30 s
			sv.Start = start.Unix()
		}
	} else {
		sv.Exp = actStatusExpiry(now, s.dur, s.keepExp)
	}
	if sv.Start > 0 && s.editID == "" && len(s.sched) >= actMaxScheduled {
		return false // "You can only have 5 scheduled statuses at a time."
	}
	// ooo_message: the reply for an Out of office status, else "" (which
	// clears one); an unedited saved reply is left out and kept.
	sv.OOOSet = true
	if s.ooo {
		reply := strings.TrimSpace(s.replyText)
		if s.replyInit != "" && reply == strings.TrimSpace(s.replyInit) {
			sv.OOOSet = false
		} else {
			sv.OOO = actOOOBlocks(reply)
		}
	}
	if sv.Start > 0 {
		toast := "Status scheduled."
		if s.editID != "" {
			toast = "Scheduled status updated."
		}
		run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, actScheduleStatus(ctx, a, sv) }, func(_ struct{}, err error) {
			if err != nil {
				h.Toast("Something went wrong. Please try again later.") // ScheduledStatusToast error
				return
			}
			h.Toast(toast)
		})
		return true
	}
	pause := s.pause
	dnd := actDNDSave{Pause: pause, PresetDnd: s.presetDnd, Picked: s.picked, Exp: sv.Exp, InitialExp: s.keepExp}
	run(h, func(ctx context.Context) (bool, error) {
		if err := actProfileSet(ctx, a, sv); err != nil {
			return false, err
		}
		st, err := actDNDInfo(ctx, a)
		if err != nil {
			st = core.DNDStatus{}
		}
		return actApplyDND(ctx, a, st, dnd, time.Now())
	}, func(snoozed bool, err error) {
		if err != nil { // updateCustomStatus swallows a failed save (.catch(noop))
			return
		}
		if snoozed {
			if sv.Exp > 0 {
				actDNDUntil = time.Unix(sv.Exp, 0)
			}
		} else if !pause {
			actDNDUntil = time.Time{}
		}
	})
	return true
}

// win converts a content point (x from the modal's left edge, y from the
// top of the scrolled content) to window coordinates.
func (s *actStatus) win(gtx layout.Context, p image.Point) image.Point {
	return dtModalOrigin(gtx, s.modal).Add(image.Pt(p.X-dp(gtx, 28), p.Y-s.list.Position.Offset))
}

func (s *actStatus) durLabel(d actDur) string {
	if d == actDurKeep {
		return actKeepLabel(s.keepExp, time.Now())
	}
	return actDurLabels[d]
}

func (s *actStatus) events(gtx layout.Context) {
	h := s.h
	for {
		e, ok := s.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok && s.canSave() {
			if s.save() {
				s.modal.Dismiss()
			}
			return
		}
		if _, ok := e.(widget.ChangeEvent); ok {
			s.semanticTyped(gtx.Now)
		}
	}
	s.semanticTick(gtx)
	for {
		e, ok := s.reply.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.ChangeEvent); ok {
			s.replyText = s.reply.Text()
		}
	}
	if s.emojiBtn.Clicked(gtx) {
		r := s.emojiRect
		h.OpenEmojiPicker(s.win(gtx, image.Pt(r.Min.X, r.Max.Y+dp(gtx, 4))), func(name string) {
			s.emoji = strings.Trim(name, ":")
			s.emojiPicked, s.semAt = true, time.Time{}
			s.focus = true
			h.Invalidate()
		})
	}
	if s.clearBtn.Clicked(gtx) { // handleReset
		s.ed.SetText("")
		s.emoji = ""
		s.emojiPicked, s.semAt = false, time.Time{}
		if s.dur == actDurKeep || s.dur == actDurCustom {
			s.dur = actDurToday
		}
		s.ooo, s.canonical, s.presetDnd, s.picked = false, "", false, false
		s.setEditing(nil)
		s.focus = true
	}
	for i := range s.presetBtn {
		if s.presetBtn[i].Clicked(gtx) {
			s.pickPreset(s.presets[i].emoji, s.presets[i].text, s.presets[i].canonical, s.presets[i].dur)
		}
	}
	if s.oooBtn.Clicked(gtx) {
		s.pickPreset("no_entry", "Out of office", actOOOCanonical, actDurCustom)
	}
	for i := range s.schedBtn {
		if i < len(s.sched) && s.schedDel[i].Clicked(gtx) {
			s.confirmDeleteScheduled(s.sched[i].ID)
		}
		if i < len(s.sched) && s.schedBtn[i].Clicked(gtx) {
			s.editScheduled(s.sched[i])
		}
	}
	s.calEvents(gtx)
	if s.pauseBtn.Clicked(gtx) {
		s.pause = !s.pause
	}
	if s.selBtn.Clicked(gtx) {
		s.openDurations(gtx)
	}
}

// pickPreset fills the form from a preset (handlePresetSelection): Out of
// office and a Vacationing that doesn't clear become a start and end range;
// Vacationing pauses notifications by itself.
func (s *actStatus) pickPreset(emoji, text, canonical string, d actDur) {
	s.ed.SetText(text)
	n := s.ed.Len()
	s.ed.SetCaret(n, n)
	s.emoji, s.dur, s.canonical = emoji, d, canonical
	s.emojiPicked, s.semAt = true, time.Time{}
	s.setEditing(nil)
	s.picked, s.presetDnd = true, canonical == "Vacationing"
	s.ooo = canonical == actOOOCanonical
	if s.ooo || canonical == "Vacationing" && d == actDurNone {
		s.dur = actDurCustom
		s.setRange(time.Now())
	}
	if s.ooo {
		s.fillReply()
	}
	if s.presetDnd {
		s.pause = false
	}
	s.focus = true
}

// editScheduled opens a scheduled status for editing (Slack's "Editing
// scheduled status"): its emoji, text, start, end and pause.
func (s *actStatus) editScheduled(st actScheduled) {
	s.ed.SetText(st.Text)
	n := s.ed.Len()
	s.ed.SetCaret(n, n)
	s.emoji, s.canonical = strings.Trim(st.Emoji, ":"), st.TextCanonical
	if s.emoji != "" {
		s.emojiPicked = true
	}
	s.semAt = time.Time{}
	s.dur, s.pause = actDurCustom, st.IsDnd
	s.picked, s.presetDnd = false, false
	cp := st
	s.setEditing(&cp)
	start, end := time.Unix(st.Scheduled, 0), time.Unix(st.Expire, 0)
	if st.Expire == 0 {
		_, end = actRangeDefaults(start)
	}
	now := time.Now()
	s.sDay.Day, s.sTime.Mins = dtDay(start), dtMins(start)
	s.eDay.Day, s.eTime.Mins = dtDay(end), dtMins(end)
	s.sDay.Min, s.eDay.Min = dtDay(now), s.sDay.Day
	s.rangeInit = true
	s.ooo = st.TextCanonical == actOOOCanonical
	if s.ooo {
		s.fillReply()
	}
	s.focus = true
}

// setEditing starts (st) or stops (nil) editing a scheduled status.
func (s *actStatus) setEditing(st *actScheduled) {
	s.editing, s.editID = st, ""
	if st != nil {
		s.editID = st.ID
	}
	if s.saveBtn != nil {
		s.saveBtn.Label = "Save"
		if st != nil {
			s.saveBtn.Label = "Update"
		}
	}
}

// confirmDeleteScheduled asks first, as Slack's speed bump does.
func (s *actStatus) confirmDeleteScheduled(id string) {
	h := s.h
	a := h.Core()
	if a == nil {
		return
	}
	del := &ModalButton{Label: "Delete Status", Kind: BtnDanger, Do: func() bool {
		for i := range s.sched { // removeScheduledStatus: drop it right away
			if s.sched[i].ID == id {
				s.setSched(append(append([]actScheduled(nil), s.sched[:i]...), s.sched[i+1:]...))
				break
			}
		}
		run(h, func(ctx context.Context) (struct{}, error) { return struct{}{}, actDeleteScheduled(ctx, a, id) }, func(_ struct{}, err error) {
			if err != nil {
				h.Toast("Something went wrong. Please try again later.")
				s.reloadSched(a) // fetchCustomStatuses
				return
			}
			h.Toast("Scheduled status removed.")
		})
		return true
	}}
	th := h.Theme()
	m := NewModal(h, ModalOpts{
		Title:     "Delete this scheduled status?",
		AutoFocus: del,
		Body: func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Regular, colorsOf(th).Fg, "Plans change, and that’s okay.", 0)
		},
		Buttons: []*ModalButton{{Label: "Keep Status", Kind: BtnOutline}, del},
	})
	h.ShowOverlay(m)
}

func (s *actStatus) openDurations(gtx layout.Context) {
	h := s.h
	var items []MenuItem
	pick := func(d actDur) func() {
		return func() {
			s.dur = d
			if d == actDurCustom && !s.rangeInit {
				s.setRange(time.Now())
			}
			s.focus = true
			h.Invalidate()
		}
	}
	if s.keepExp > 0 {
		items = append(items, MenuItem{Label: s.durLabel(actDurKeep), Checked: s.dur == actDurKeep, Do: pick(actDurKeep)})
	}
	for d := actDurNone; d <= actDurWeek; d++ {
		if s.ooo && d != actDurNone && d != actDurToday && d != actDurWeek {
			continue // CustomStatusDurationDefault: isOutOfOffice keeps these
		}
		items = append(items, MenuItem{Label: actDurLabels[d], Checked: s.dur == d, Do: pick(d)})
	}
	items = append(items, MenuItem{Label: actDurLabels[actDurCustom], Checked: s.dur == actDurCustom, Do: pick(actDurCustom)})
	r := s.selRect
	wr := image.Rectangle{Min: s.win(gtx, r.Min), Max: s.win(gtx, r.Max)}
	ppd := gtx.Metric.PxPerDp
	if ppd <= 0 {
		ppd = 1
	}
	wdp := float32(r.Dx()) / ppd
	h.ShowOverlay(NewMenuOpts(h, items, MenuOpts{Anchor: wr, Width: wdp, OnClose: func() { s.focus = true }}))
}

func (s *actStatus) layout(gtx layout.Context) layout.Dimensions {
	if s.modal.done {
		return layout.Dimensions{}
	}
	if s.showTip && s.pill == nil {
		s.pill = &actTipPill{h: s.h, modal: s.modal}
		s.h.ShowOverlay(s.pill)
	}
	if s.focus && !s.cal.on {
		gtx.Execute(key.FocusCmd{Tag: &s.ed})
		s.focus = false
	}
	s.events(gtx)
	if s.modal.done {
		return layout.Dimensions{}
	}
	s.syncButtons()
	s.takePresets()
	th := s.h.Theme()
	cl := colorsOf(th)
	if s.cal.on { // the calendar step replaces the form
		return s.calContent(gtx, th, cl)
	}
	padX := dp(gtx, 28)
	bw := gtx.Constraints.Max.X
	fw := bw + 2*padX // the list spans the full modal width (row hover)
	maxH := minInt(dp(gtx, actBodyMax), gtx.Constraints.Max.Y)
	var d layout.Dimensions
	at(gtx, image.Pt(-padX, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(fw, 0), Max: image.Pt(fw, maxH)}
		d = s.list.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(fw, 1<<20)}
			return s.content(gtx, th, cl, fw, padX)
		})
		return d
	})
	off := s.list.Position.Offset
	if off > 0 { // header edge while scrolled
		fill(gtx, image.Rect(-padX, 0, bw+padX, 1), cl.Ring)
	}
	if total, vis := s.list.Position.Length, d.Size.Y; total > vis && vis > 0 {
		thumb := maxInt(dp(gtx, 24), vis*vis/total)
		ty := (vis - thumb) * minInt(off, total-vis) / maxInt(1, total-vis)
		sw := dp(gtx, 7)
		x := bw + padX - dp(gtx, 4) - sw
		rrect(gtx, image.Rect(x, ty, x+sw, ty+thumb), sw/2, theme.WithAlpha(cl.Fg, 0x66))
	}
	return layout.Dimensions{Size: image.Pt(bw, d.Size.Y)}
}

// content draws the body; x=0 is the modal's left edge, fw its width.
func (s *actStatus) content(gtx layout.Context, th *theme.Theme, cl *Colors, fw, padX int) layout.Dimensions {
	cw := fw - 2*padX
	y := dp(gtx, 4)
	filled := s.ed.Len() > 0 || s.emoji != ""
	if filled && s.dur == actDurCustom {
		// the scheduled-flow alerts above the input, re-checked every second
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Truncate(time.Second).Add(time.Second)})
		if s.editID == "" && len(s.sched) >= actMaxScheduled {
			y += actAlert(gtx, th, cl, padX, y, cw, "You can only have 5 scheduled statuses at a time.")
		}
		if s.rangeInit && !s.endTime().After(time.Now()) {
			y += actAlert(gtx, th, cl, padX, y, cw, "End time must be in the future.")
		}
	}
	iy := y
	at(gtx, image.Pt(padX, iy), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(cw, dp(gtx, 42))}
		return s.input(gtx, th, cl)
	})
	s.emojiRect = s.emojiRect.Add(image.Pt(padX, iy))
	y += dp(gtx, 42)
	if !filled {
		if len(s.sched) > 0 {
			y += s.legend(gtx, th, cl, padX, y, "Scheduled")
			now := time.Now()
			for i, st := range s.sched {
				if i >= len(s.schedBtn) {
					break
				}
				y += s.schedRow(gtx, th, cl, fw, padX, y, i, st, now)
			}
		}
		y += s.legend(gtx, th, cl, padX, y, "Schedule for later")
		y += s.preset(gtx, th, cl, fw, padX, y, &s.oooBtn, "no_entry", "Out of office", " — Set a start and end date")
		if s.presets != nil {
			y += s.legend(gtx, th, cl, padX, y, s.forTeam)
			for i, p := range s.presets {
				y += s.preset(gtx, th, cl, fw, padX, y, &s.presetBtn[i], p.emoji, p.text, actDurNotes[p.dur])
			}
		}
		if cals := s.calPresets(); len(cals) > 0 {
			y += s.legend(gtx, th, cl, padX, y, "Automatically updates")
			for i, c := range cals {
				y += s.calRow(gtx, th, cl, fw, padX, y, &s.cal.btns[i], c)
			}
		}
		return layout.Dimensions{Size: image.Pt(fw, y+dp(gtx, 4))}
	}
	// duration
	y += dp(gtx, 20)
	at(gtx, image.Pt(padX, y), func(gtx layout.Context) layout.Dimensions {
		return text(gtx, th, 15, theme.Bold, cl.Fg, "Duration")
	})
	y += dp(gtx, 30)
	sh := dp(gtx, 44)
	selY := y
	s.selRect = image.Rect(padX, selY, padX+cw, selY+sh)
	custom := s.dur == actDurCustom
	boxTop := selY + dp(gtx, 28)
	if custom {
		box := image.Rect(padX, boxTop, padX+cw, boxTop+dp(gtx, 28+28+12+28+12))
		rrect(gtx, box, dp(gtx, 8), cl.BgAlt)
		fill(gtx, image.Rect(box.Min.X, box.Min.Y, box.Max.X, box.Min.Y+dp(gtx, 8)), cl.BgAlt)
	}
	at(gtx, image.Pt(padX, selY), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &s.selBtn, nil, func(gtx layout.Context) layout.Dimensions {
			r := image.Rect(0, 0, cw, sh)
			border := dtBorder(th)
			if s.selBtn.Hovered() {
				border = cl.Fg2
			}
			rrect(gtx, r, dp(gtx, 8), border)
			rrect(gtx, r.Inset(1), dp(gtx, 8)-1, cl.Bg)
			at(gtx, image.Pt(dp(gtx, 12), 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, cw-dp(gtx, 52)), sh)}
				return vcenter(gtx, sh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 18, theme.Regular, cl.Fg, s.durLabel(s.dur))
				})
			})
			at(gtx, image.Pt(cw-dp(gtx, 12+16), (sh-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "chevron-down", 16, cl.Fg)
			})
			return layout.Dimensions{Size: r.Max}
		})
	})
	y = selY + sh
	if custom {
		y = s.rangeRows(gtx, th, cl, padX, cw, boxTop)
	}
	if s.dur != actDurNone && (!s.presetDnd || s.ooo) { // shouldShowPauseNotificationsOption
		y += dp(gtx, 12)
		at(gtx, image.Pt(padX, y), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &s.pauseBtn, nil, func(gtx layout.Context) layout.Dimensions {
				rh := dp(gtx, 22)
				actCheckbox(gtx, cl, image.Pt(0, (rh-dp(gtx, 16))/2), s.pause)
				lbl := "Pause notifications until status is cleared"
				at(gtx, image.Pt(dp(gtx, 25), 0), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, cl.Fg, lbl)
					})
				})
				return layout.Dimensions{Size: image.Pt(dp(gtx, 25)+textW(gtx, th, 15, theme.Regular, lbl), rh)}
			})
		})
		y += dp(gtx, 22)
	}
	if s.ooo {
		y += dp(gtx, 16)
		at(gtx, image.Pt(padX, y), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, "Automatic reply:")
		})
		y += dp(gtx, 30)
		y += s.replyBox(gtx, th, cl, padX, y, cw)
		if p := s.replyPreview(gtx, th, cl, padX, y+dp(gtx, 12), cw); p > 0 {
			y += dp(gtx, 12) + p
		}
	}
	return layout.Dimensions{Size: image.Pt(fw, y+dp(gtx, 4))}
}

// rangeRows draws the Start/End rows inside the box at top; returns the
// box's bottom.
func (s *actStatus) rangeRows(gtx layout.Context, th *theme.Theme, cl *Colors, x0, cw, top int) int {
	rh, gap := dp(gtx, 28), dp(gtx, 12)
	tw, dw := dp(gtx, 116), dp(gtx, 192)
	tx := x0 + cw - dp(gtx, 12) - tw
	dx := tx - dp(gtx, 12) - dw
	if lo := x0 + dp(gtx, 72); dx < lo { // narrow window: shrink the date
		dx = lo
		dw = maxInt(dp(gtx, 80), tx-dp(gtx, 12)-dx)
	}
	y := top + dp(gtx, 28)
	for i, lbl := range [2]string{"Start", "End"} {
		day, tm := &s.sDay, &s.sTime
		if i == 1 {
			day, tm = &s.eDay, &s.eTime
		}
		at(gtx, image.Pt(x0+dp(gtx, 12), y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Bold, cl.Fg, lbl)
			})
		})
		at(gtx, image.Pt(dx, y), func(gtx layout.Context) layout.Dimensions {
			return day.Layout(gtx, s.h, s.win(gtx, image.Pt(dx, y)), dw, dtSmall)
		})
		at(gtx, image.Pt(tx, y), func(gtx layout.Context) layout.Dimensions {
			return tm.Layout(gtx, s.h, s.win(gtx, image.Pt(tx, y)), tw, dtSmall)
		})
		y += rh + gap
	}
	return y
}

// actReplyTools is the reply editor's (inert) formatting toolbar; "" is a
// separator.
var actReplyTools = [...]string{"bold", "italic", "strike", "", "link", "list-ordered", "list-bullet", "", "quote", "code", "code-block"}

// replyBox draws the automatic-reply editor (bordered, 36px #f8f8f8
// toolbar) at (x, y), w wide; returns its height.
func (s *actStatus) replyBox(gtx layout.Context, th *theme.Theme, cl *Colors, x, y, w int) int {
	tbH := dp(gtx, 36)
	px, py := dp(gtx, 12), dp(gtx, 9)
	iw := maxInt(1, w-2*px)
	call, d := record(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(iw, 0), Max: image.Pt(iw, 1<<20)}
		return th.Editor(gtx, &s.reply, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
	})
	taH := maxInt(d.Size.Y, dp(gtx, 22)) + 2*py
	r := image.Rect(x, y, x+w, y+tbH+taH)
	rad := dp(gtx, 8)
	if gtx.Focused(&s.reply) {
		ring := dp(gtx, 4)
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		rrect(gtx, r, rad, cl.Focus)
	} else {
		rrect(gtx, r, rad, cl.InputBorder)
	}
	rrect(gtx, r.Inset(1), rad-1, cl.Bg)
	tb := image.Rect(r.Min.X+1, r.Min.Y+1, r.Max.X-1, r.Min.Y+tbH)
	rrect(gtx, tb, rad-1, cl.BgAlt)
	fill(gtx, image.Rect(tb.Min.X, tb.Min.Y+rad, tb.Max.X, tb.Max.Y), cl.BgAlt)
	ix := r.Min.X + dp(gtx, 3)
	for _, name := range actReplyTools {
		if name == "" {
			fill(gtx, image.Rect(ix+dp(gtx, 4), tb.Min.Y+dp(gtx, 9), ix+dp(gtx, 4)+maxInt(1, dp(gtx, 1)), tb.Max.Y-dp(gtx, 9)), cl.Divider)
			ix += dp(gtx, 8)
			continue
		}
		at(gtx, image.Pt(ix+dp(gtx, 8), r.Min.Y+(tbH-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, name, 16, cl.Subtle)
		})
		ix += dp(gtx, 32)
	}
	ta := image.Rect(r.Min.X, r.Min.Y+tbH, r.Max.X, r.Max.Y)
	func() {
		defer clip.Rect(ta).Push(gtx.Ops).Pop()
		pointer.CursorText.Add(gtx.Ops)
	}()
	macroAt(gtx, call, image.Pt(x+px, ta.Min.Y+py))
	return r.Dy()
}

// replyPreview draws how the automatic reply looks to someone who DMs you;
// returns its height (0 when the reply is empty).
func (s *actStatus) replyPreview(gtx layout.Context, th *theme.Theme, cl *Colors, x, y, w int) int {
	reply := strings.TrimSpace(s.replyText)
	if reply == "" {
		return 0
	}
	pad := dp(gtx, 12)
	iw := maxInt(1, w-2*pad)
	call, d := record(loose(gtx, image.Pt(iw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		yy := 0
		at(gtx, image.Pt(0, (dp(gtx, 18)-dp(gtx, 14))/2), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "eye", 14, cl.Muted)
		})
		at(gtx, image.Pt(dp(gtx, 20), 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, iw-dp(gtx, 20)), dp(gtx, 18))}
			return text(gtx, th, 13, theme.Regular, cl.Muted, "Automatic reply ∙ Only visible to you")
		})
		yy += dp(gtx, 26)
		at(gtx, image.Pt(0, yy+dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions { return avatar(gtx, s.h, s.selfAv, 20) })
		tx := dp(gtx, 28)
		at(gtx, image.Pt(tx, yy), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, iw-tx), dp(gtx, 22))}
			return text(gtx, th, 15, theme.Black, cl.Fg, s.selfName)
		})
		yy += dp(gtx, 24)
		bc, bd := record(loose(gtx, image.Pt(maxInt(1, iw-tx), 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, reply, 4)
		})
		macroAt(gtx, bc, image.Pt(tx, yy))
		return layout.Dimensions{Size: image.Pt(iw, yy+bd.Size.Y)}
	})
	ht := d.Size.Y + 2*pad
	stroke(gtx, image.Rect(x, y, x+w, y+ht), dp(gtx, 8), 1, cl.Ring)
	macroAt(gtx, call, image.Pt(x+pad, y+pad))
	return ht
}

// input draws the 42px status field.
func (s *actStatus) input(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	var state css.State
	if gtx.Focused(&s.ed) {
		state = css.StateFocusWithin
	}
	st := statusInputClass.Enter(gtx, state, 0)
	defer st.Leave()
	if st.Hidden() {
		return layout.Dimensions{}
	}
	return st.Wrap(gtx, func(gtx layout.Context) layout.Dimensions {
		return s.inputNative(gtx, th, cl)
	})
}

func (s *actStatus) inputNative(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	hh := dp(gtx, 42)
	r := image.Rect(0, 0, w, hh)
	rad := dp(gtx, 8)
	if gtx.Focused(&s.ed) {
		ring := dp(gtx, 4)
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		rrect(gtx, r, rad, cl.Focus)
	} else {
		rrect(gtx, r, rad, cl.InputBorder)
	}
	rrect(gtx, r.Inset(1), rad-1, cl.Bg)

	bs := dp(gtx, 28)
	bp := image.Pt(dp(gtx, 7), (hh-bs)/2)
	s.emojiRect = image.Rectangle{Min: bp, Max: bp.Add(image.Pt(bs, bs))}
	at(gtx, bp, func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &s.emojiBtn, func(gtx layout.Context, sz image.Point, hovered bool) {
			if hovered {
				rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 4), cl.BgHover)
			}
		}, func(gtx layout.Context) layout.Dimensions {
			is := dp(gtx, 20)
			at(gtx, image.Pt((bs-is)/2, (bs-is)/2), func(gtx layout.Context) layout.Dimensions {
				if s.emoji != "" {
					return emojiImage(gtx, s.h, s.emoji, 20)
				}
				return icon(gtx, "emoji", 20, cl.Fg2)
			})
			return layout.Dimensions{Size: image.Pt(bs, bs)}
		})
	})
	filled := s.ed.Len() > 0 || s.emoji != ""
	right := w - dp(gtx, 12)
	if filled {
		cp := image.Pt(w-dp(gtx, 7)-bs, (hh-bs)/2)
		at(gtx, cp, func(gtx layout.Context) layout.Dimensions {
			// Slack: a filled dark circle with a white ×.
			return clickBox(gtx, &s.clearBtn, nil, func(gtx layout.Context) layout.Dimensions {
				d := dp(gtx, 18)
				o := (bs - d) / 2
				rrect(gtx, image.Rect(o, o, o+d, o+d), d/2, cl.Fg)
				is := dp(gtx, 12)
				at(gtx, image.Pt((bs-is)/2, (bs-is)/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, "close-small", 12, cl.Bg)
				})
				return layout.Dimensions{Size: image.Pt(bs, bs)}
			})
		})
		right = cp.X - dp(gtx, 4)
	}
	x := dp(gtx, 42)
	tw := maxInt(1, right-x)
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, hh)}
			if s.ed.Len() == 0 {
				call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 18, theme.Regular, cl.Placeholder, "What's your status?")
				})
				call.Add(gtx.Ops)
			}
			return th.Editor(gtx, &s.ed, unit.Sp(18), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
		})
	})
	return layout.Dimensions{Size: r.Max}
}

// legend draws a section heading at y (padding 24 0 8, text at x=36 from
// the modal edge) and returns its height.
func (s *actStatus) legend(gtx layout.Context, th *theme.Theme, cl *Colors, padX, y int, label string) int {
	at(gtx, image.Pt(padX+dp(gtx, 8), y+dp(gtx, 24)), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, dp(gtx, 18), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Bold, theme.WithAlpha(cl.Fg, 0xb3), label)
		})
	})
	return dp(gtx, 24+18+8)
}

// preset draws one full-width "emoji **text** — note" row at y; returns
// its height. note includes the leading " — ".
func (s *actStatus) preset(gtx layout.Context, th *theme.Theme, cl *Colors, fw, padX, y int, btn *widget.Clickable, emoji, label, note string) int {
	return s.presetRow(gtx, th, cl, fw, padX, y, btn, func(gtx layout.Context) layout.Dimensions {
		return emojiImage(gtx, s.h, emoji, 22)
	}, label, note)
}

// presetRow is a preset row with img (22px) at the left.
func (s *actStatus) presetRow(gtx layout.Context, th *theme.Theme, cl *Colors, fw, padX, y int, btn *widget.Clickable, img layout.Widget, label, note string) int {
	rh := dp(gtx, 28)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(fw, rh))
		return clickBox(gtx, btn, nil, func(gtx layout.Context) layout.Dimensions {
			fg, muted := cl.Fg, cl.Muted
			if btn.Hovered() {
				fill(gtx, image.Rect(0, 0, fw, rh), cl.Highlight)
				fg, muted = cl.HighText, cl.HighText
			}
			at(gtx, image.Pt(padX+dp(gtx, 8), (rh-dp(gtx, 22))/2), img)
			x := padX + dp(gtx, 40)
			right := fw - padX
			at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, right-x), rh)}
				return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Bold, fg, label)
				})
			})
			x += textW(gtx, th, 15, theme.Bold, label)
			if x < right {
				at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, right-x), rh)}
					return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, muted, note)
					})
				})
			}
			return layout.Dimensions{Size: image.Pt(fw, rh)}
		})
	})
	return rh
}

// actScheduledRange is formatScheduledDateRange: "Today, 3:00 PM – 5:00 PM",
// "Tomorrow 9:00 AM – Fri, Nov 14 5:00 PM".
func actScheduledRange(start, end int64, now time.Time) string {
	st, en := time.Unix(start, 0).In(now.Location()), time.Unix(end, 0).In(now.Location())
	sd, ed := dtDateLabel(dtDay(st), now, dtRelative), dtDateLabel(dtDay(en), now, dtRelative)
	if sd == ed {
		return sd + ", " + clock(st) + " – " + clock(en)
	}
	return sd + " " + clock(st) + " – " + ed + " " + clock(en)
}

// schedRow draws a "Scheduled" row: emoji, **text** — range, and on hover
// the Delete (×) control at the right (CustomStatusPresetItem with
// enableControls).
func (s *actStatus) schedRow(gtx layout.Context, th *theme.Theme, cl *Colors, fw, padX, y, i int, st actScheduled, now time.Time) int {
	rh := dp(gtx, 28)
	note := ""
	if st.Expire > 0 {
		note = " — " + actScheduledRange(st.Scheduled, st.Expire, now)
	}
	emoji := strings.Trim(st.Emoji, ":")
	if emoji == "" && st.Text != "" {
		emoji = "speech_balloon"
	}
	h := s.preset(gtx, th, cl, fw, padX, y, &s.schedBtn[i], emoji, st.Text, note)
	if s.schedBtn[i].Hovered() || s.schedDel[i].Hovered() {
		bs := dp(gtx, 24)
		at(gtx, image.Pt(fw-padX-bs, y+(rh-bs)/2), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &s.schedDel[i], func(gtx layout.Context, sz image.Point, hovered bool) {
				if hovered {
					rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 4), theme.WithAlpha(cl.HighText, 0x33))
				}
			}, func(gtx layout.Context) layout.Dimensions {
				is := dp(gtx, 16)
				at(gtx, image.Pt((bs-is)/2, (bs-is)/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, "close", 16, cl.HighText)
				})
				return layout.Dimensions{Size: image.Pt(bs, bs)}
			})
		})
	}
	return h
}

// actAlert draws Slack's error Alert (c-alert--level_error, boxed: padding
// 12 16, radius 8, --dt_color-surf-imp, a warning icon in content-imp, 15px
// text) w wide at (x, y); returns its height plus the 16px margin below.
func actAlert(gtx layout.Context, th *theme.Theme, cl *Colors, x, y, w int, msg string) int {
	bg := rgba(0xff81aa, float32(0x21)/255)
	if th.Dark {
		bg = rgba(0xb41541, float32(0x40)/255)
	}
	if th.Tokens != nil {
		if c, own, ok := th.Tokens.Color("var(--dt_color-surf-imp)"); own && ok {
			bg = c
		}
	}
	px, py := dp(gtx, 16), dp(gtx, 12)
	tx := px + dp(gtx, 20+12)
	call, d := record(loose(gtx, image.Pt(maxInt(1, w-tx-px), 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg, msg, 0)
	})
	hh := maxInt(d.Size.Y, dp(gtx, 22)) + 2*py
	rrect(gtx, image.Rect(x, y, x+w, y+hh), dp(gtx, 8), bg)
	at(gtx, image.Pt(x+px, y+py+dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "warning", 20, cl.Danger)
	})
	macroAt(gtx, call, image.Pt(x+tx, y+py))
	return hh + dp(gtx, 16)
}

// actCheckbox draws a 16px Slack checkbox at p.
func actCheckbox(gtx layout.Context, cl *Colors, p image.Point, on bool) {
	sz := dp(gtx, 16)
	r := image.Rectangle{Min: p, Max: p.Add(image.Pt(sz, sz))}
	rad := dp(gtx, 4)
	if on {
		rrect(gtx, r, rad, cl.Highlight)
		is := dp(gtx, 12)
		at(gtx, p.Add(image.Pt((sz-is)/2, (sz-is)/2)), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "check", 12, cl.HighText)
		})
		return
	}
	rrect(gtx, r, rad, theme.WithAlpha(cl.Fg, 0x80))
	rrect(gtx, r.Inset(1), rad-1, cl.Bg)
}

// ---- "Tip:" hint pill ----
//
// Slack's keyboard hint (D72a): while showKeyboardHint, the modal ends
// with .p-custom_status_modal__keyboard_hint_container (absolute, bottom
// -50px, full modal width, centered; the modal is overflow: visible)
// holding a Pill (.p-pill: 15px/1.46668 white on rgba(0,0,0,.7), radius
// 1000, padding 6 16, centered): "<strong>Tip:</strong> Open this faster
// with <strong>{commandKey} + Shift + Y</strong>", commandKey ⌘ on macOS
// and Ctrl elsewhere. It stays while the modal is open. It is drawn above
// the modal without taking input and never calls stacked(), so the modal
// under it keeps Escape; if the root dismisses the pill as its topmost
// overlay (Escape routing), the modal closes with it.

type actTipSeg struct {
	s string
	w font.Weight
}

// actTipSegs is the tip's text: bold, regular, bold. The keys are the
// shortcut's chips joined by " + " (commandKey: ⌘ on macOS, Ctrl
// elsewhere).
func actTipSegs() [3]actTipSeg {
	keys := strings.Join(shortcut.Chips("Mod+Shift+Y"), " + ")
	return [3]actTipSeg{{"Tip:", theme.Bold}, {"Open this faster with", theme.Regular}, {keys, theme.Bold}}
}

type actTipPill struct {
	h     Host
	modal *Modal
	done  bool
}

func (p *actTipPill) Dismissed() bool { return p.done || p.modal == nil || p.modal.Dismissed() }

// Dismiss closes the pill and the modal it belongs to.
func (p *actTipPill) Dismiss() {
	if p.done {
		return
	}
	p.done = true
	if p.modal != nil {
		p.modal.Dismiss()
	}
}

// actTipRect is the pill's box for a modal at mr (window coordinates)
// and a text tw wide: its bottom 50px below the modal's, centered on it.
func actTipRect(gtx layout.Context, mr image.Rectangle, tw int) image.Rectangle {
	px, py := dp(gtx, 16), dp(gtx, 6)
	hh := dp(gtx, 22) + 2*py
	w := minInt(tw+2*px, mr.Dx())
	bottom := mr.Max.Y + dp(gtx, 50)
	x := mr.Min.X + (mr.Dx()-w)/2
	return image.Rect(x, bottom-hh, x+w, bottom)
}

func (p *actTipPill) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	if p.Dismissed() || p.modal.rect.Empty() {
		return layout.Dimensions{}
	}
	th := p.h.Theme()
	segs := actTipSegs()
	gap := dp(gtx, 4) // a 15px space
	tw := 0
	for i, sg := range segs {
		if i > 0 {
			tw += gap
		}
		tw += textW(gtx, th, 15, sg.w, sg.s)
	}
	r := actTipRect(gtx, p.modal.rect, tw)
	rrect(gtx, r, r.Dy()/2, rgba(0x000000, .7))
	x := r.Min.X + (r.Dx()-tw)/2
	for _, sg := range segs {
		at(gtx, image.Pt(x, r.Min.Y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, r.Dy(), func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, sg.w, rgb(0xffffff), sg.s)
			})
		})
		x += textW(gtx, th, 15, sg.w, sg.s) + gap
	}
	return layout.Dimensions{}
}
