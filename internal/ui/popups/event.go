package popups

// An event block's actions (Slack's Event, boot-shared 0lYF; CDN build
// 9f450b8ad3db):
//
//   - Share event (BaseEventActions): openEventShareDialog, the share
//     dialog for the message the event is in (Clack's forward dialog);
//   - More actions: one item, "View on {calApp} calendar" (open_calendar_
//     link), which opens the event on its calendar;
//   - Yes, No and Maybe (BaseEventRSVP): registerRsvp unless it's the
//     answer this card last sent; a failed call is the toast "Something’s
//     gone wrong. Mind trying again in a moment?", an auth_url in the reply
//     the calendar connect modal (showAuthFlow);
//   - Copy to Your Calendar (BaseEventCopyToCalendar): copyToCalendar,
//     whose auth_url opens the same modal (a failure is only logged).
//
// The calendar connect modal is BaseCalendarAuthLauncher (XiL6):
// "Connect calendar", the sentence naming the calendar, Close and Connect
// a Calendar (a link to auth_url). Left out: the modal's title icon (the
// calendar app's 48px icon from its app record).

import (
	"context"
	"image"
	"strings"
	"sync"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
)

var (
	nsEvent    = i18n.NS("event")
	nsCalendar = i18n.NS("calendar")
)

// eventAnswers is each card's last answer (BaseEventRSVP's state: a click
// on the answer it last sent sends nothing), by its write key; a failed
// call clears it, so the answer can be sent again.
var eventAnswers sync.Map

// EventAction runs an event card's action (richview.EventHandler's act)
// for the event in the message ch/ts; btn anchors More actions' menu.
func EventAction(h Host, act string, ev *richtext.Event, ch, ts string, btn image.Rectangle) {
	if ev == nil {
		return
	}
	// RSVP, Copy and Share event act on the message the card is in: a
	// surface that drew it without one sends nothing
	if (act == "share" || act == "copy" || strings.HasPrefix(act, "rsvp:")) && (ch == "" || ts == "") {
		return
	}
	switch {
	case act == "share":
		shareEventMessage(h, ch, ts)
	case act == "more":
		h.ShowOverlay(NewEventMenu(h, ev, btn))
	case strings.HasPrefix(act, "rsvp:"):
		eventRSVP(h, ev, strings.TrimPrefix(act, "rsvp:"), ch, ts)
	case act == "copy":
		eventCopy(h, ev, ch, ts)
	}
}

// NewEventMenu is BaseEventActions' More actions menu.
func NewEventMenu(h Host, ev *richtext.Event, btn image.Rectangle) *Menu {
	link := ev.WebLink
	items := []MenuItem{{Label: richview.EventCalendarLabel(ev.AppType), Do: func() { topicHandler{h}.OpenURL(link) }}}
	return NewMenuOpts(h, items, MenuOpts{Anchor: btn, Popover: "bottom-left", TryOpposite: true})
}

// shareEventMessage opens the share dialog for the message ch/ts, read
// from Slack when the store doesn't hold it.
func shareEventMessage(h Host, ch, ts string) {
	a := h.Core()
	if a == nil || a.S == nil {
		return
	}
	if sm, ok := a.S.FindHeld(ch, ts); ok {
		h.ShowOverlay(NewForwardModal(h, a.MessageFromSlack(ch, &sm)))
		return
	}
	run(h, func(ctx context.Context) (*slack.Message, error) {
		pg, err := a.C.History(ctx, ch, slack.HistoryOpts{Latest: ts, Limit: 1, Inclusive: true})
		if err != nil {
			return nil, err
		}
		for i := range pg.Messages {
			if pg.Messages[i].TS == ts {
				return &pg.Messages[i], nil
			}
		}
		return nil, nil
	}, func(m *slack.Message, err error) {
		if err == nil && m != nil {
			h.ShowOverlay(NewForwardModal(h, a.MessageFromSlack(ch, m)))
		}
	})
}

func eventRSVP(h Host, ev *richtext.Event, rsvp, ch, ts string) {
	a := h.Core()
	if a == nil {
		return
	}
	key := richview.EventKey(ch, ts, ev.ID)
	if last, ok := eventAnswers.Load(key); ok && last == rsvp {
		return // y's i !== _
	}
	if !beginWrite(key, "rsvp:"+rsvp) {
		return // BaseEventRSVP: every option disabled while a call is out
	}
	eventAnswers.Store(key, rsvp)
	app, id := ev.AppType, ev.ID
	run(h, func(ctx context.Context) (string, error) {
		return a.CalendarEventRSVP(ctx, id, rsvp, ch, ts)
	}, func(auth string, err error) {
		endWrite(key)
		if err != nil {
			eventAnswers.Delete(key)
			h.Toast(nsEvent.T("Something’s gone wrong. Mind trying again in a moment?"))
			return
		}
		if auth != "" {
			h.ShowOverlay(NewCalendarConnectModal(h, app, auth))
		}
	})
}

func eventCopy(h Host, ev *richtext.Event, ch, ts string) {
	a := h.Core()
	if a == nil {
		return
	}
	key := richview.EventKey(ch, ts, ev.ID)
	if !beginWrite(key, "copy") {
		return // its Button loading (disabled) until copyToCalendar settles
	}
	app, id := ev.AppType, ev.ID
	run(h, func(ctx context.Context) (string, error) {
		return a.CalendarEventCopy(ctx, id, ch, ts)
	}, func(auth string, err error) {
		endWrite(key)
		if err == nil && auth != "" {
			h.ShowOverlay(NewCalendarConnectModal(h, app, auth))
		}
	})
}

// calendarConnectText is BaseCalendarAuthLauncher's sentence for app.
func calendarConnectText(app string) string {
	var parts []i18n.Part
	if app == "ocal_gcc" {
		parts = nsCalendar.RT("Looks like <calendar-name>calendar</calendar-name> is installed but you don’t have any connected calendars. Connect a calendar to start creating events!")
	} else {
		parts = nsCalendar.RT("Looks like <calendar-name>calendar</calendar-name> Calendar is installed but you don’t have any connected calendars. Connect a calendar to start creating events!")
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Tag == "calendar-name" {
			b.WriteString(eventCalendarName(app))
			continue
		}
		b.WriteString(p.Text)
	}
	return b.String()
}

// eventCalendarName is sVpJ's name for an appType.
func eventCalendarName(app string) string {
	switch app {
	case "gcal":
		return "Google"
	case "ocal":
		return "Outlook"
	case "ocal_gcc":
		return "Outlook Calendar GCC High"
	}
	return ""
}

// NewCalendarConnectModal is BaseCalendarAuthLauncher: "Connect
// calendar", its sentence, Close and Connect a Calendar, which opens
// authURL and leaves the modal open (a link in a new tab, in Slack).
func NewCalendarConnectModal(h Host, app, authURL string) *Modal {
	v := richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: calendarConnectText(app)}}}}}, richview.Options{NoHoverCards: true})
	return NewModal(h, ModalOpts{Title: nsCalendar.T("Connect calendar"), Body: func(gtx layout.Context) layout.Dimensions {
		if RichStyle == nil {
			return layout.Dimensions{}
		}
		st := RichStyle(h)
		if st == nil {
			return layout.Dimensions{}
		}
		return v.Layout(gtx, st, topicHandler{h})
	}, Buttons: []*ModalButton{
		{Label: nsCalendar.T("Close"), Kind: BtnOutline, Do: func() bool { return true }},
		{Label: nsCalendar.T("Connect a Calendar"), Kind: BtnPrimary, Do: func() bool {
			topicHandler{h}.OpenURL(authURL)
			return false
		}},
	}})
}
