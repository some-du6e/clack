package huddle

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// Transcription, as Slack's huddle event log runs it (client-boot B3Qp
// startEventLog, YmpG stopEventLog, the 4xZR huddleEvents store and
// /KZR's rooms.startTranscription fetcher, build 4d0c78085232):
//
//   - the event log is off until you turn captions on. startEventLog
//     returns at once while it's on or loading; else it turns it on
//     (enableEventLog, setEventLogLoading), calls
//     rooms.startTranscription with the call id (camelCase callId sent as
//     call_id), once more after 5 s when the server answers
//     permission_denied, and on failure turns the log off again
//     (disableEventLog). Finally it pushes START_TRANSCRIPTION (member
//     USLACKBOT, timestamp 0), which HuddleClosedCaptions draws as
//     "Captions have been turned on.".
//   - only an enabled log keeps transcripts (updateTranscriptions and
//     pushNonTranscriptionEvent are no-ops otherwise), so captions show
//     only after you turned them on, whoever started the transcription.
//   - stopEventLog (turning captions off, and the first thing stopHuddle
//     does) returns at once unless the log is on and not loading; else it
//     turns it off, which drops its events, and calls
//     rooms.stopTranscription.

// StartEventLog is startEventLog's native. It blocks on the Web API:
// call it off the UI goroutine.
func (s *Session) StartEventLog(ctx context.Context) error {
	s.mu.Lock()
	if s.events || s.eventsLoading {
		s.mu.Unlock()
		return nil
	}
	s.events, s.eventsLoading, s.lines, s.meta = true, true, nil, nil
	s.mu.Unlock()
	s.changed()
	err := s.transcription(ctx, "rooms.startTranscription")
	var se *slack.Error
	if errors.As(err, &se) && se.Code == "permission_denied" {
		select {
		case <-time.After(transcriptionRetry):
			err = s.transcription(ctx, "rooms.startTranscription")
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	s.mu.Lock()
	if err != nil {
		s.events, s.lines, s.meta = false, nil, nil // disableEventLog
	}
	s.eventsLoading = false
	if s.events {
		s.lines = append(s.lines, Caption{Start: true, UserID: "USLACKBOT"})
	}
	s.mu.Unlock()
	s.changed()
	if err != nil {
		s.logf("huddle: rooms.startTranscription: %v", err)
	}
	return err
}

// transcriptionRetry is how long startEventLog waits before asking again
// after permission_denied (tests shorten it).
var transcriptionRetry = 5 * time.Second

// StopEventLog is stopEventLog's native (blocks on the Web API).
func (s *Session) StopEventLog(ctx context.Context) error {
	s.mu.Lock()
	if !s.events || s.eventsLoading {
		s.mu.Unlock()
		return nil
	}
	s.events, s.eventsLoading, s.lines, s.meta = false, true, nil, nil
	s.mu.Unlock()
	s.changed()
	err := s.transcription(ctx, "rooms.stopTranscription")
	s.mu.Lock()
	s.eventsLoading = false
	s.mu.Unlock()
	s.changed()
	return err
}

// EventLog reports whether the event log is on (huddleEvents.enabled)
// and whether a start or stop is in flight (isLoading).
func (s *Session) EventLog() (on, loading bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.events, s.eventsLoading
}

// SetEventLog is Slack's enableEventLog / disableEventLog actions on
// their own, without the transcription the thunks start or stop (a
// plugin keeping the log open, HuddleActivity): the log turns on or off
// and its events are dropped either way (the 4xZR reducer returns the
// initial state with enabled set).
func (s *Session) SetEventLog(on bool) {
	s.mu.Lock()
	if s.events == on && !s.eventsLoading && len(s.lines) == 0 && len(s.meta) == 0 {
		s.mu.Unlock()
		return
	}
	s.events, s.eventsLoading, s.lines, s.meta = on, false, nil, nil
	s.mu.Unlock()
	s.changed()
}

// Transcription is roomsStartTranscriptionFetcher (on) or
// roomsStopTranscriptionFetcher: rooms.startTranscription or
// rooms.stopTranscription for this call, leaving the event log as it is.
// Blocks on the Web API.
func (s *Session) Transcription(ctx context.Context, on bool) error {
	if on {
		return s.transcription(ctx, "rooms.startTranscription")
	}
	return s.transcription(ctx, "rooms.stopTranscription")
}

// LogEvent is one of the event log's non-transcription events, as
// Slack's call handlers push them (pushNonTranscriptionEvent, client-boot
// 4xZR U$) while the log is on: JOIN and LEAVE (onPeerJoined,
// onPeerLeft), SCREENSHARE_START and SCREENSHARE_STOP (the video tile
// handlers, a content tile), SEND_REACTION (sendReaction, onPeerReacji;
// Emoji is the reaction).
type LogEvent struct {
	Type     string
	MemberID string
	At       time.Time
	Emoji    string
}

// maxLogEvents caps the log (Slack keeps every event while it's on).
const maxLogEvents = 2000

// pushEventLocked is pushNonTranscriptionEvent: a no-op while the log is
// off. Callers hold mu.
func (s *Session) pushEventLocked(typ, user, emoji string) {
	if !s.events || user == "" {
		return
	}
	if len(s.meta) >= maxLogEvents {
		s.meta = append(s.meta[:0], s.meta[len(s.meta)-maxLogEvents/2:]...)
	}
	s.meta = append(s.meta, LogEvent{Type: typ, MemberID: user, At: time.Now(), Emoji: emoji})
}

func (s *Session) transcription(ctx context.Context, method string) error {
	if s.o.Client == nil {
		return errors.New("huddle: no Slack client")
	}
	var out struct {
		OK bool `json:"ok"`
	}
	return s.o.Client.Call(ctx, method, url.Values{"call_id": {s.join.RoomID}}, &out)
}

// Rename sets the huddle's topic: Slack's setHuddleTopic → renameCall
// (client-boot rky3 xB), screenhero.rooms.rename {room, room_name}, then
// the call's name locally (upsert {id, name}); the room's sh_room_update
// brings it to everyone. Blocks on the Web API.
func (s *Session) Rename(ctx context.Context, name string) error {
	if s.o.Client == nil {
		return errors.New("huddle: no Slack client")
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := s.o.Client.Call(ctx, "screenhero.rooms.rename", url.Values{"room": {s.join.RoomID}, "room_name": {name}}, &out); err != nil {
		return err
	}
	s.mu.Lock()
	s.join.Name = name
	s.mu.Unlock()
	s.changed()
	return nil
}
