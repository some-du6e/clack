package core

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// notesIn is every local ephemeral in ch's timeline and the thread
// threadTS ("" for none), ts → message.
func notesIn(a *App, ch, threadTS string) map[string]slack.Message {
	out := map[string]slack.Message{}
	if tl := a.S.Timeline(ch); tl != nil {
		for _, m := range tl.Msgs {
			if m.IsEphemeral {
				out[m.TS] = m
			}
		}
	}
	if threadTS != "" {
		th, _ := a.S.Thread(ch, threadTS)
		for _, m := range th {
			if m.IsEphemeral {
				out[m.TS] = m
			}
		}
	}
	return out
}

// Slack's addEphemeralFeedback (client-boot w0M8): a temporary note takes
// the place of its type's last note in the conversation, keeping its ts
// (addOrFlashEphemeralBotMsg), unless ignoreExistingMessage; a thread's
// note is of its own type; a lasting one removes the channel's notes of
// its type and is filed under none. Each is Slackbot's, with what the
// caller put on it (slackbot_feels), or a bot_message with a username.
func TestAddEphemeralFeedbackLikeSlack(t *testing.T) {
	a, _ := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "USLACKBOT", Name: "slackbot", IsBot: true}})
	a.S.SetLatest("C1", []slack.Message{{Type: "message", TS: "1.000100", User: "U1", Text: "hi"}}, false)
	a.S.SetThread("C1", "1.000100", []slack.Message{{Type: "message", TS: "1.000100", User: "U1", Text: "hi", ThreadTS: "1.000100"}})
	ix := a.Interactive()

	first := a.AddEphemeralFeedback(FeedbackNote{Message: slack.Message{Channel: "C1", Text: "Put Block Kit JSON after bk:", SlackbotFeels: "sad_surprise"}})
	notes := notesIn(a, "C1", "")
	m, ok := notes[first]
	if !ok || len(notes) != 1 {
		t.Fatalf("after the first note the channel shows %d notes (%v), want it alone", len(notes), notes)
	}
	if m.User != "USLACKBOT" || m.Type != "message" || m.Text != "Put Block Kit JSON after bk:" || m.SlackbotFeels != "sad_surprise" || m.Channel != "C1" {
		t.Errorf("the note is %+v, want Slackbot's ephemeral with its text and slackbot_feels", m)
	}
	if !ix.Ephemeral("C1", first) || !ix.LocalNotice("C1", first) {
		t.Error("the note isn't a local ephemeral")
	}

	again := a.AddEphemeralFeedback(FeedbackNote{Message: slack.Message{Channel: "C1", Text: "That isn't valid JSON"}})
	notes = notesIn(a, "C1", "")
	if again != first || len(notes) != 1 || notes[first].Text != "That isn't valid JSON" {
		t.Fatalf("a second note of the type: ts %s (first %s), notes %v; want the first in place with the new text", again, first, notes)
	}
	if notes[first].SlackbotFeels != "" {
		t.Errorf("the replaced note kept the old note's slackbot_feels %q: it takes the new message whole", notes[first].SlackbotFeels)
	}

	beside := a.AddEphemeralFeedback(FeedbackNote{Message: slack.Message{Channel: "C1", Text: "beside it"}, IgnoreExisting: true})
	if beside == first || len(notesIn(a, "C1", "")) != 2 {
		t.Fatalf("ignoreExistingMessage: ts %s, notes %v; want a second note", beside, notesIn(a, "C1", ""))
	}
	// the type's last note is the one the next note replaces
	if ts := a.AddEphemeralFeedback(FeedbackNote{Message: slack.Message{Channel: "C1", Text: "the last one"}}); ts != beside {
		t.Errorf("the next note replaced %s, want the type's last (%s)", ts, beside)
	}

	inThread := a.AddEphemeralFeedback(FeedbackNote{Message: slack.Message{Channel: "C1", ThreadTS: "1.000100", Text: "in the thread"}})
	if inThread == first || inThread == beside {
		t.Fatal("the thread's note replaced the channel's")
	}
	if th := notesIn(a, "C1", "1.000100")[inThread]; th.ThreadTS != "1.000100" || th.Text != "in the thread" {
		t.Errorf("the thread's note: %+v", th)
	}
	other := a.AddEphemeralFeedback(FeedbackNote{Message: slack.Message{Channel: "C1", Text: "its own type"}, Type: "schedule_send_error"})
	if other == first || other == beside {
		t.Fatal("a note of another type replaced the command feedback")
	}

	lasting := a.AddEphemeralFeedback(FeedbackNote{Message: slack.Message{Channel: "C1", Text: "stays"}, Lasting: true})
	if ix.Ephemeral("C1", first) || ix.Ephemeral("C1", beside) {
		t.Error("a lasting note left the channel's notes of its type")
	}
	if !ix.Ephemeral("C1", inThread) {
		t.Error("a lasting note removed the thread's note, which is of the thread type")
	}
	if !ix.Ephemeral("C1", other) {
		t.Error("a lasting note removed a note of another type")
	}
	if !ix.Ephemeral("C1", lasting) {
		t.Fatal("the lasting note isn't shown")
	}
	// filed under no type: a later temporary note doesn't replace it, a later lasting one doesn't remove it
	if ts := a.AddEphemeralFeedback(FeedbackNote{Message: slack.Message{Channel: "C1", Text: "temporary"}}); ts == lasting {
		t.Error("a temporary note replaced the lasting one")
	}
	a.AddEphemeralFeedback(FeedbackNote{Message: slack.Message{Channel: "C1", Text: "another"}, Lasting: true})
	if !ix.Ephemeral("C1", lasting) {
		t.Error("a later lasting note removed the earlier one")
	}

	bot := a.AddEphemeralFeedback(FeedbackNote{Message: slack.Message{Channel: "C1", Text: "from a bot", Username: "Helper"}, Type: "bot_note"})
	if m := notesIn(a, "C1", "")[bot]; m.Subtype != "bot_message" || m.User != "" || m.Username != "Helper" {
		t.Errorf("a note with a username: %+v, want a bot_message with no user", m)
	}
	if ts := a.AddEphemeralFeedback(FeedbackNote{Message: slack.Message{Text: "nowhere"}}); ts != "" {
		t.Errorf("a note without a channel went to %q", ts)
	}
}
