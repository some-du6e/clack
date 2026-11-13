package popups

// The "Set a status" modal's calendar row and calendar step, as Slack's
// web client has them (CDN build 4d0c78085232, gantry-v2-async-client-
// boot-shared):
//
//   - On mount the modal dispatches getCurrentCalendarAndUser (JstU).
//     getCurrentCalendarAndUpsert (20cD) calls calendar.getInstalledCalendars
//     {} and keeps {gcal, ocal} (lnuM setInstalledCalendars), then picks
//     the current calendar (getUserMXCurrentCalendar): with both installed
//     the mx_calendar_type pref's (GOOGLE / OUTLOOK) or else Google, with one
//     installed that one, with none the pref's. When gcal or ocal is
//     installed, getCalendarUserInfoAndUpsert calls calendar.user.info
//     {app_type: gcal|ocal} and keeps cal_user (a reply without one keeps
//     the old). The calendar store isn't persisted.
//   - getAllCalendarPresets (aq7I) has nothing until installedCalendars is
//     known. Without a current calendar it offers Google and Outlook where
//     is_manageable_by_user; with one, that calendar when it's installed or
//     manageable. A row (CustomStatusCalendarIcon) is the app's 22px logo,
//     **In a meeting** and " — Based on your Google Calendar", or, while the
//     calendar user syncs status, " — Google Calendar updates are on". The
//     "Automatically updates" section is there only with a row and while
//     the input is empty.
//   - Pressing a row (handleCalendarInputPressed) replaces the body with
//     BaseCustomStatusCalendarModalContent: one bold line and "Your calendar
//     will automatically update your status…" (renderCalendarModalSyncText),
//     then the status sync picture (the light or dark one for the app). The
//     footer gets "ⓘ Learn more" at the left (the app's help article) and
//     Back and the primary button, by the calendar user: Add (none: opens
//     slack.com/interop/{gcal,ocal}app/install and closes), Connect (not
//     connected: opens provider_auth_url and closes), Turn On / Turn Off
//     (connected: turn_on_status_sync_url when the user has one, else
//     toggleStatusSyncAndUpdate, which sets is_status_sync at once, calls
//     calendar.user.toggleStatusSync {calapp_user_id, enable}, puts it back
//     on failure, and the modal closes).

import (
	"context"
	"image"
	"net/url"
	"strconv"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's production calendar app ids (bK56 Oj, qj).
const (
	actGcalAppID = "ADZ494LHY"
	actOcalAppID = "AFV5ECLBZ"
)

// actSlackImg is where the web client's relative /img paths resolve (the
// app origin redirects them to its CDN).
const actSlackImg = "https://app.slack.com/img/calendar/"

// actCalApp is one calendar.getInstalledCalendars entry.
type actCalApp struct {
	IsInstalled  bool   `json:"is_installed"`
	IsManageable bool   `json:"is_manageable_by_user"`
	AppID        string `json:"app_id"`
}

// actCals is calendar.getInstalledCalendars' reply.
type actCals struct {
	Gcal actCalApp `json:"gcal"`
	Ocal actCalApp `json:"ocal"`
}

// actCalendar is Slack's {name, appId} calendar.
type actCalendar struct{ Name, AppID string }

// actCalUser is calendar.user.info's cal_user.
type actCalUser struct {
	ID                  string `json:"id"`
	AppType             string `json:"app_type"`
	IsProviderConnected bool   `json:"is_provider_connected"`
	IsStatusSync        bool   `json:"is_status_sync"`
	IsOOOStatusSync     bool   `json:"is_ooo_status_sync"`
	ProviderAuthURL     string `json:"provider_auth_url"`
	TurnOnStatusSyncURL string `json:"turn_on_status_sync_url"`
}

// The calendar store (Slack's "calendar" slice), UI goroutine only.
var (
	actCalInstalled *actCals
	actCalCurrent   *actCalendar
	actCalUser0     *actCalUser
)

// actGoogle and actOutlook are getCurrentCalendarForMX's calendars.
func actGoogle(appID string) actCalendar  { return actCalendar{Name: "Google", AppID: appID} }
func actOutlook(appID string) actCalendar { return actCalendar{Name: "Outlook", AppID: appID} }

// actMXCalendar is getCurrentCalendarForMX: the mx_calendar_type pref.
func actMXCalendar(pref string) *actCalendar {
	var c actCalendar
	switch pref {
	case "OUTLOOK":
		c = actOutlook(actOcalAppID)
	case "GOOGLE":
		c = actGoogle(actGcalAppID)
	default:
		return nil
	}
	return &c
}

// actCurrentCalendar is getUserMXCurrentCalendar.
func actCurrentCalendar(cals *actCals, pref string) *actCalendar {
	if cals == nil {
		return actMXCalendar(pref)
	}
	g, o := cals.Gcal.IsInstalled, cals.Ocal.IsInstalled
	var c actCalendar
	switch {
	case g && o:
		if mx := actMXCalendar(pref); mx != nil {
			return mx
		}
		c = actGoogle(cals.Gcal.AppID)
	case g:
		c = actGoogle(cals.Gcal.AppID)
	case o:
		c = actOutlook(cals.Ocal.AppID)
	default:
		return actMXCalendar(pref)
	}
	return &c
}

// actCalendarPresets is getAllCalendarPresets' calendars.
func actCalendarPresets(cur *actCalendar, cals *actCals) []actCalendar {
	if cals == nil {
		return nil
	}
	if cur == nil {
		var out []actCalendar
		if cals.Gcal.IsManageable {
			out = append(out, actGoogle(actGcalAppID))
		}
		if cals.Ocal.IsManageable {
			out = append(out, actOutlook(actOcalAppID))
		}
		return out
	}
	manage, inst := cals.Ocal.IsManageable, cals.Ocal.IsInstalled
	if cur.AppID == actGcalAppID {
		manage, inst = cals.Gcal.IsManageable, cals.Gcal.IsInstalled
	}
	if inst || manage {
		return []actCalendar{*cur}
	}
	return nil
}

// actCalendarNote is a calendar row's customDuration.
func actCalendarNote(u *actCalUser, c actCalendar) string {
	// aq7I's "{calType}Calendar …" (current_status), calType "Google "
	if u != nil && u.IsStatusSync {
		return " — " + nsStatus.T("{calType}Calendar updates are on", "calType", c.Name+" ")
	}
	return " — " + nsStatus.T("Based on your {calType}Calendar", "calType", c.Name+" ")
}

// actCalendarLogo is CustomStatusCalendarIcon's image.
func actCalendarLogo(c actCalendar) string {
	if c.AppID == actGcalAppID {
		return actSlackImg + "google-calendar/gCal-logo.png"
	}
	return actSlackImg + "outlook-calendar/oCal-logo.png"
}

// actCalendarPicture is getCalendarStatusSyncPic.
func actCalendarPicture(appID string, dark bool) string {
	switch {
	case appID == actGcalAppID && !dark:
		return actSlackImg + "google-calendar/lt-google-calendar-status-sync.png"
	case appID == actGcalAppID:
		return actSlackImg + "google-calendar/dk-google-calendar-status-sync.png"
	case !dark:
		return actSlackImg + "outlook-calendar/lt-outlook-calendar-status-sync.png"
	}
	return actSlackImg + "outlook-calendar/dk-outlook-calendar-status-sync.png"
}

// actCalendarHelp is the footer's "Learn more" (bK56 QZ, uh).
func actCalendarHelp(c actCalendar) string {
	if c.AppID == actGcalAppID {
		return "https://slack.com/help/articles/206329808-Google-Calendar-for-Slack"
	}
	return "https://slack.com/help/articles/360020134853-Microsoft-Outlook-Calendar-for-Slack"
}

// actCalendarInstall is handleCalendarInstallClick's page (bK56 H2, ny).
func actCalendarInstall(c actCalendar) string {
	if c.AppID == actGcalAppID {
		return "https://slack.com/interop/gcalapp/install"
	}
	return "https://slack.com/interop/ocalapp/install"
}

const actCalendarBlurb = "Your calendar will automatically update your status to “In a meeting” at the start of events, and clear it once they end."

// actCalendarText is renderCalendarModalSyncText: the bold line and the
// sentence under it ("" bold for the last case).
func actCalendarText(c actCalendar, u *actCalUser) (bold, rest string) {
	// D72a's "<strong>…</strong>" lines (current_status), drawn bold
	strong := func(src string) string { return i18n.Plain(nsStatus.RT(src, "calName", c.Name)) }
	blurb := nsStatus.T(actCalendarBlurb)
	switch {
	case u == nil:
		return strong("<strong>Add {calName} Calendar</strong>"), blurb
	case u.IsProviderConnected && !u.IsStatusSync:
		return strong("<strong>Update your status with {calName} Calendar</strong>"), blurb
	case !u.IsProviderConnected:
		return strong("<strong>Connect your {calName} Calendar account</strong>"), blurb
	case u.IsStatusSync:
		return strong("<strong>{calName} Calendar updates are on</strong>"), blurb
	}
	return "", nsStatus.T("Visit the calendar app settings to turn off status sync any time.")
}

// actCalendarAction is the calendar step's primary button label.
func actCalendarAction(u *actCalUser) string {
	switch {
	case u == nil:
		return nsStatus.T("Add")
	case !u.IsProviderConnected:
		return nsStatus.T("Connect")
	case u.IsStatusSync:
		return nsStatus.T("Turn Off")
	}
	return nsStatus.T("Turn On")
}

// actGetInstalledCalendars is calendarGetInstalledCalendars.
func actGetInstalledCalendars(ctx context.Context, a *core.App) (actCals, error) {
	var r actCals
	err := a.C.Call(ctx, "calendar.getInstalledCalendars", url.Values{}, &r)
	return r, err
}

// actCalendarUserInfo is calendarGetUserInfo (app_type gcal or ocal).
func actCalendarUserInfo(ctx context.Context, a *core.App, appType string) (*actCalUser, error) {
	var r struct {
		CalUser *actCalUser `json:"cal_user"`
	}
	err := a.C.Call(ctx, "calendar.user.info", url.Values{"app_type": {appType}}, &r)
	return r.CalUser, err
}

// actToggleStatusSync is calendarToggleUserStatusSync.
func actToggleStatusSync(ctx context.Context, a *core.App, userID string, enable bool) error {
	return a.C.Call(ctx, "calendar.user.toggleStatusSync", url.Values{"calapp_user_id": {userID}, "enable": {strconv.FormatBool(enable)}}, nil)
}

// actFetchCalendars is getCurrentCalendarAndUser, run when the modal opens.
func actFetchCalendars(h Host) {
	a := h.Core()
	if a == nil {
		return
	}
	pref := a.PrefString("mx_calendar_type")
	type got struct {
		cals    *actCals
		cur     *actCalendar
		user    *actCalUser
		userErr error
	}
	run(h, func(ctx context.Context) (got, error) {
		var g got
		cals, err := actGetInstalledCalendars(ctx, a)
		if err != nil {
			return g, err
		}
		g.cals = &cals
		g.cur = actCurrentCalendar(&cals, pref)
		if cals.Gcal.IsInstalled || cals.Ocal.IsInstalled {
			appType := "ocal"
			if g.cur != nil && g.cur.AppID == actGcalAppID {
				appType = "gcal"
			}
			g.user, g.userErr = actCalendarUserInfo(ctx, a, appType)
		}
		return g, nil
	}, func(g got, err error) {
		if err != nil {
			return
		}
		actCalInstalled = g.cals
		if g.cur != nil {
			actCalCurrent = g.cur
		}
		if g.userErr == nil && g.user != nil {
			actCalUser0 = g.user
		}
		h.Invalidate()
	})
}

// actCalState is the modal's calendar step (tx, tq) and its controls.
type actCalState struct {
	on      bool        // the calendar step is showing
	picked  actCalendar // the row that opened it
	btns    []widget.Clickable
	learn   widget.Clickable
	busy    bool // the sync toggle is in flight
	back    *ModalButton
	primary *ModalButton
}

// calPresets are the "Automatically updates" rows.
func (s *actStatus) calPresets() []actCalendar {
	ps := actCalendarPresets(actCalCurrent, actCalInstalled)
	if len(s.cal.btns) < len(ps) {
		s.cal.btns = make([]widget.Clickable, len(ps))
	}
	return ps
}

// calButtons are the calendar step's footer: Back and the action.
func (s *actStatus) calButtons() []*ModalButton {
	if s.cal.back == nil {
		s.cal.back = &ModalButton{Label: nsStatus.T("Back"), Kind: BtnOutline, Do: func() bool {
			s.cal.on = false
			s.focus = true
			return false
		}}
		s.cal.primary = &ModalButton{Kind: BtnPrimary, Enabled: func() bool { return !s.cal.busy }, Do: s.calAction}
	}
	s.cal.primary.Label = actCalendarAction(actCalUser0)
	return []*ModalButton{s.cal.back, s.cal.primary}
}

// calAction is the primary button: handleCalendarInstallClick,
// handleAuthorizeCalendarPressed or handleCalendarStatusSyncToggle.
func (s *actStatus) calAction() bool {
	u := actCalUser0
	switch {
	case u == nil:
		actOpen(s.h, actCalendarInstall(s.cal.picked))
		return true
	case !u.IsProviderConnected:
		actOpen(s.h, u.ProviderAuthURL)
		return true
	case u.TurnOnStatusSyncURL != "":
		actOpen(s.h, u.TurnOnStatusSyncURL)
		return true
	}
	a := s.h.Core()
	if a == nil {
		return false
	}
	enable := !u.IsStatusSync
	// toggleStatusSyncAndUpdate: the store first, back on failure
	prev := *u
	nu := *u
	nu.IsStatusSync = enable
	if !enable {
		nu.IsOOOStatusSync = false
	}
	actCalUser0 = &nu
	s.cal.busy = true
	id := u.ID
	run(s.h, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, actToggleStatusSync(ctx, a, id, enable)
	}, func(_ struct{}, err error) {
		s.cal.busy = false
		if err != nil && actCalUser0 == &nu {
			actCalUser0 = &prev
		}
		s.modal.Dismiss()
		s.h.Invalidate()
	})
	return false
}

// actOpen opens a calendar page in the browser.
func actOpen(h Host, u string) {
	if u == "" {
		return
	}
	if err := openurl.Open(u); err != nil {
		h.Toast("Couldn't open the link: " + errText(err))
	}
}

// calEvents handles the calendar rows and the step's link.
func (s *actStatus) calEvents(gtx layout.Context) {
	for i, c := range s.calPresets() {
		if s.cal.btns[i].Clicked(gtx) {
			s.cal.on, s.cal.picked = true, c
		}
	}
	if s.cal.learn.Clicked(gtx) {
		actOpen(s.h, actCalendarHelp(s.cal.picked))
	}
}

// syncButtons puts the step's footer on the modal (or the form's back).
func (s *actStatus) syncButtons() {
	if s.cal.on {
		s.modal.o.Buttons = s.calButtons()
		return
	}
	s.modal.o.Buttons = s.btns
}

// calContent is BaseCustomStatusCalendarModalContent: the text in a
// content section, then the 20px gap and the picture at the body's width
// (928×284 images, radius 4, an inset 1px rgba(0,0,0,.1) edge).
func (s *actStatus) calContent(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	bold, rest := actCalendarText(s.cal.picked, actCalUser0)
	y := 0
	if bold != "" {
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 15, 22, theme.Bold, cl.Fg, bold, 0)
		})
		y += d.Size.Y
	}
	d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg, rest, 0)
	})
	y += d.Size.Y
	y += dp(gtx, 20)
	ih := w * 284 / 928
	r := image.Rect(0, y, w, y+ih)
	if img, ok := s.h.Images().Get(actCalendarPicture(s.cal.picked.AppID, th.Dark), w); ok {
		at(gtx, r.Min, func(gtx layout.Context) layout.Dimensions {
			return theme.Image(gtx, img, r.Size(), dp(gtx, 4))
		})
	}
	stroke(gtx, r, dp(gtx, 4), border1(gtx), rgba(0x000000, .1))
	return layout.Dimensions{Size: image.Pt(w, r.Max.Y)}
}

// calFooter draws "ⓘ Learn more" at the footer's left, in card
// coordinates (the modal's Over): .p-custom_status_modal_calendar__footer,
// 15px at foreground_max .7 with an 8px gap.
func (s *actStatus) calFooter(gtx layout.Context) {
	if !s.cal.on {
		return
	}
	th := s.h.Theme()
	cl := colorsOf(th)
	card := gtx.Constraints.Max
	bh := dp(gtx, 36)
	y := card.Y - dp(gtx, 24) - bh
	fg := theme.WithAlpha(cl.Fg, 0xb3)
	at(gtx, image.Pt(dp(gtx, 28), y), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &s.cal.learn, nil, func(gtx layout.Context) layout.Dimensions {
			is := dp(gtx, 15)
			at(gtx, image.Pt(0, (bh-is)/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "info", 15, fg)
			})
			x := is + dp(gtx, 8)
			learn := nsStatus.T("Learn more")
			lw := textW(gtx, th, 15, theme.Regular, learn)
			at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, fg, learn)
				})
			})
			if s.cal.learn.Hovered() {
				fill(gtx, image.Rect(x, bh/2+dp(gtx, 9), x+lw, bh/2+dp(gtx, 10)), fg)
			}
			return layout.Dimensions{Size: image.Pt(x+lw, bh)}
		})
	})
}

// calRow draws an "Automatically updates" row: the logo, **In a meeting**
// and its note (CustomStatusPresetItem).
func (s *actStatus) calRow(gtx layout.Context, th *theme.Theme, cl *Colors, fw, padX, y int, btn *widget.Clickable, c actCalendar) int {
	return s.presetRow(gtx, th, cl, fw, padX, y, btn, func(gtx layout.Context) layout.Dimensions {
		px := dp(gtx, 22)
		if img, ok := s.h.Images().Get(actCalendarLogo(c), px); ok {
			return containImage(gtx, img, image.Pt(px, px))
		}
		return layout.Dimensions{Size: image.Pt(px, px)}
	}, nsStatus.T("In a meeting"), actCalendarNote(actCalUser0, c))
}
