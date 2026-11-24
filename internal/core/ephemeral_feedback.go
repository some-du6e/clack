package core

import (
	"context"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// FeedbackNote is what Slack's addEphemeralFeedback thunk is given
// (client-boot w0M8, build 35b42de8b870): its message (channel, thread_ts,
// text, and anything else the caller puts on Slackbot's note, such as
// blocks, attachments, slackbot_feels or a username), its ephemeralType
// and ignoreExistingMessage, and whether the note lasts (temporary:
// false). The thunk's inputText and inputDelta go back into the
// conversation's composer, which is the JS thunk's part (taut).
type FeedbackNote struct {
	Message        slack.Message
	Type           string
	IgnoreExisting bool
	Lasting        bool
}

// AddEphemeralFeedback is Slack's addEphemeralFeedback thunk: Slackbot's
// "Only visible to you" note in the message's channel, or in its thread,
// on this client alone. It returns the note's ts.
//
// A temporary note (the default) is filed under its type: the one it
// names, else Slack's tempSlashCmdFeedback, or threadsTempSlashCmdFeedback
// in a thread (AEXl). It takes the place of that type's last note there,
// keeping its ts (addOrFlashEphemeralBotMsg, 4W6Z: getEphemeralMessagesByType
// with the channel and the thread, lodash's last), unless IgnoreExisting
// or there is none; then it is a new note of the type (addEphemeralMessage,
// gDyO). A lasting note first removes every note of its type in the
// channel, threads included (removeEphemeralMsgsByType, FgcF), and is
// filed under no type (addEphemeralBotMessage).
//
// The note has Slack's ephemeral shape (ajiu): type message,
// is_ephemeral, and Slackbot as its user, or subtype bot_message when it
// carries a username. Slack also cleans its text as an internal message
// (clean_message_text_with_state: the viewer's emoticon conversion, and
// mention placeholders); Clack shows the text as the caller gave it.
func (a *App) AddEphemeralFeedback(f FeedbackNote) string {
	m := f.Message
	ch, thread := m.Channel, m.ThreadTS
	if ch == "" {
		return ""
	}
	typ := f.Type
	if typ == "" {
		typ = feedbackType
		if thread != "" {
			typ = threadFeedbackType
		}
	}
	ix := a.Interactive()
	if f.Lasting {
		ix.removeNotes(typ, ch)
		return a.putEphemeral(m)
	}
	if !f.IgnoreExisting {
		if ts := ix.lastNote(typ, ch, thread); ts != "" {
			a.replaceEphemeral(ts, m)
			return ts
		}
	}
	ts := a.putEphemeral(m)
	ix.noteFeedback(typ, ch, ts)
	return ts
}

// ephemeralShape is Slack's makeEphemeral (ajiu) for m: a message only
// the viewer sees, from Slackbot unless it names a username.
func ephemeralShape(m slack.Message) slack.Message {
	m.Type = "message"
	m.IsEphemeral = true
	if m.Username != "" {
		m.Subtype = "bot_message"
	} else {
		m.User = "USLACKBOT"
	}
	return m
}

// putEphemeral adds m (its channel and thread set) as a new local
// ephemeral and returns its ts: a fresh local ts after everything cached
// there, unless m has one (Slack's addEphemeralMessage: d.ts ||
// generateLocalTs()). Callers are off the UI goroutine: Slackbot's
// profile (name, avatar) is loaded first when the store doesn't have it
// yet, as Slack's member store always does.
func (a *App) putEphemeral(m slack.Message) string {
	m = ephemeralShape(m)
	ch, threadTS := m.Channel, m.ThreadTS
	if m.User == "USLACKBOT" && len(a.S.MissingUsers([]string{"USLACKBOT"})) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		a.EnsureUsers(ctx, []string{"USLACKBOT"})
		cancel()
	}
	if m.TS == "" {
		m.TS = a.pendingTS(ch, threadTS)
	}
	ts := m.TS
	ix := a.Interactive()
	ix.mu.Lock()
	ix.ephem[ch+"/"+ts] = threadTS
	if len(ix.local) > 2000 {
		clear(ix.local)
	}
	ix.local[ch+"/"+ts] = true
	ix.mu.Unlock()
	a.putLocal(ch, m)
	v := a.message(ch, &m)
	a.emit(Event{Type: "message", Channel: ch, TS: ts, ThreadTS: threadTS, UserID: m.User, Msg: &v,
		Reply: !isTopLevel(&m), ConvKind: a.kindOf(ch)})
	ix.emit(ViewEvent{Kind: "ephemeral", Channel: ch, TS: ts})
	return ts
}

// replaceEphemeral gives the local ephemeral at ts m's contents, in place
// (addOrFlashEphemeralBotMsg's messages update: makeEphemeral(m) with the
// note's ts, channel and thread_ts).
func (a *App) replaceEphemeral(ts string, m slack.Message) {
	m = ephemeralShape(m)
	m.TS = ts
	ch := m.Channel
	a.putLocal(ch, m)
	v := a.message(ch, &m)
	a.emit(Event{Type: "update", Channel: ch, TS: ts, ThreadTS: m.ThreadTS, Msg: &v})
}

// noteFeedback files the ephemeral ch/ts under typ (Slack's
// ephemeralMessages[typ][ch][ts]).
func (ix *Interactive) noteFeedback(typ, ch, ts string) {
	ix.mu.Lock()
	if ix.feedback == nil {
		ix.feedback = map[string][]string{}
	}
	ix.feedback[typ] = append(ix.feedback[typ], ch+"/"+ts)
	ix.mu.Unlock()
}

// lastNote is the ts of the last ephemeral of typ still shown in ch, in
// the thread when thread isn't "" (getEphemeralMessagesByType's
// threadTs: only a given thread is matched), or "".
func (ix *Interactive) lastNote(typ, ch, thread string) string {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	keys := ix.feedback[typ]
	for i := len(keys) - 1; i >= 0; i-- {
		c, ts, _ := strings.Cut(keys[i], "/")
		th, shown := ix.ephem[keys[i]]
		if c == ch && shown && (thread == "" || th == thread) {
			return ts
		}
	}
	return ""
}

// removeNotes is removeEphemeralMsgsByType({ephemeralType, channelId}):
// every ephemeral of typ in ch, threads included, goes, and typ files
// none there any more.
func (ix *Interactive) removeNotes(typ, ch string) {
	ix.mu.Lock()
	keys := ix.feedback[typ]
	var keep, drop []string
	for _, k := range keys {
		if c, _, _ := strings.Cut(k, "/"); c == ch {
			drop = append(drop, k)
		} else {
			keep = append(keep, k)
		}
	}
	if len(drop) > 0 {
		ix.feedback[typ] = keep
	}
	ix.mu.Unlock()
	for _, k := range drop {
		c, ts, _ := strings.Cut(k, "/")
		if ix.Ephemeral(c, ts) {
			ix.DismissEphemeral(c, ts)
		}
	}
}
