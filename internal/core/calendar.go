package core

// An event block's calls (Slack's Event, boot-shared 0lYF; CDN build
// 9f450b8ad3db): registerRsvp (calendar.event.rsvp) and copyToCalendar
// (calendar.event.copy), each with the event and the message it's in.
// When the viewer's calendar isn't connected yet the reply carries an
// auth_url, which the card's calendar connect modal opens. Both are a
// click on the card, which Slack sends every time: an answer or a copy
// sent again after a failure skips the client's backoff (slack.NoBackoff).

import (
	"context"
	"net/url"

	"github.com/some-du6e/clack/internal/slack"
)

// CalendarEventRSVP is registerRsvp: the viewer's answer to the event
// (rsvp: accepted, declined or tentative). authURL is the reply's
// auth_url.
func (a *App) CalendarEventRSVP(ctx context.Context, eventID, rsvp, ch, ts string) (authURL string, err error) {
	var r struct {
		AuthURL string `json:"auth_url"`
	}
	err = a.C.Call(slack.NoBackoff(ctx), "calendar.event.rsvp", url.Values{"event_id": {eventID}, "rsvp": {rsvp}, "message_ts": {ts}, "channel": {ch}}, &r)
	return r.AuthURL, err
}

// CalendarEventCopy is copyToCalendar: the event copied to the viewer's
// primary calendar. authURL is the reply's auth_url.
func (a *App) CalendarEventCopy(ctx context.Context, eventID, ch, ts string) (authURL string, err error) {
	var r struct {
		AuthURL string `json:"auth_url"`
	}
	err = a.C.Call(slack.NoBackoff(ctx), "calendar.event.copy", url.Values{"event_id": {eventID}, "message_ts": {ts}, "channel": {ch}}, &r)
	return r.AuthURL, err
}
