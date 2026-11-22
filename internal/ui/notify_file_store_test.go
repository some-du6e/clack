package ui

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// TestNotifyFileToastReadsTheStore: a file message's toast reads what
// Slack's showNotification reads when it builds the text. The first file
// is the file store's copy (Eley's getFileById: a stub of a file another
// held message carries whole reads that one, so it says the title, or
// the image line); upload is the message the toast was handed (a stored
// copy replaced by one without upload still says "uploaded this file");
// and the uploader's name is getMemberById's, which asks for a member
// the store doesn't hold (memberNameFromFile: the toast says "’s", the
// member goes on the members pass's list). Expected texts are Slack's:
// Eley's extractTextFromMessage with MXE2, hVKV and getFileById running
// in node over the same messages and store (CDN build 35b42de8b870).
// Synthetic messages.
func TestNotifyFileToastReadsTheStore(t *testing.T) {
	_, ca, _ := fakePrefsApp(t)
	b := &backend{core: ca}
	const ada, robin, dm = "U0SYNTH0001", "U0SYNTH0002", "D0SYNTH0001"
	ca.S.PutUsers([]slack.User{{ID: ada, Name: "ada", Profile: slack.Profile{DisplayName: "Ada"}},
		{ID: robin, Name: "robin", Profile: slack.Profile{DisplayName: "Robin"}}})
	conv := &convInfo{Kind: "im", Name: "Ada", User: ada}
	msg := func(s string) slack.Message {
		var m slack.Message
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		return m
	}
	body := func(ts string) string {
		t.Helper()
		m, ok := ca.HeldMessage(dm, ts)
		if !ok {
			t.Fatalf("%s isn't held", ts)
		}
		return b.notifyBody(core.Event{Type: "message", Channel: dm, TS: ts, Msg: &m}, conv)
	}
	const file = `"user":"U0SYNTH0001","name":"x.bin","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted","url_private":"https://files.example.invalid/x"`
	const image = `"user":"U0SYNTH0001","name":"x.png","title":"","mimetype":"image/png","filetype":"png","pretty_type":"PNG","mode":"hosted","url_private":"https://files.example.invalid/x.png","thumb_360":"https://files.example.invalid/t360.png"`
	const stub = `"file_access":"check_file_info","created":1762520000,"timestamp":1762520000,"user":"U0SYNTH0001"`
	ca.S.SetLatest(dm, []slack.Message{
		msg(`{"type":"message","ts":"1762500000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001","title":"Q3 report",` + file + `}]}`),
		msg(`{"type":"message","ts":"1762500000.000200","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0002",` + image + `}]}`),
		// a stub of each
		msg(`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0001",` + stub + `}]}`),
		msg(`{"type":"message","ts":"1762520000.000200","user":"U0SYNTH0001","text":"","upload":true,"files":[{"id":"F0SYNTH0002",` + stub + `}]}`),
		// an upload, and someone the store doesn't hold's file
		msg(`{"type":"message","ts":"1762520000.000300","user":"U0SYNTH0001","text":"","upload":true,"files":[{"id":"F0SYNTH0003","title":"",` + file + `}]}`),
		msg(`{"type":"message","ts":"1762520000.000400","user":"U0SYNTH0001","text":"","files":[{"id":"F0SYNTH0004","title":"",` +
			`"user":"U0SYNTH0099","name":"x.bin","mimetype":"application/octet-stream","filetype":"binary","pretty_type":"Binary","mode":"hosted","url_private":"https://files.example.invalid/y"}]}`),
	}, false)

	if got, want := body("1762520000.000100"), "Q3 report"; got != want {
		t.Errorf("a stub of a file the store holds whole: %q, Slack %q", got, want)
	}
	if got, want := body("1762520000.000200"), "uploaded this image"; got != want {
		t.Errorf("an uploaded stub of an image the store holds whole: %q, Slack %q", got, want)
	}

	// the uploader the store doesn't hold: no name
	if got, want := body("1762520000.000400"), "shared ’s file"; got != want {
		t.Errorf("an unknown uploader's file: %q, Slack %q", got, want)
	}
	// and the toast's read asks for them (getMemberById's
	// fetchIfMissing). The message's conversion asks for its file
	// header's uploader too, and the store notes a miss once, so the
	// toast's own read is seen on a message nothing converted, whose
	// uploader nothing asked for yet.
	const unseen = "U0SYNTH0098"
	ca.S.TakeMissingUsers()
	hand := core.Message{Channel: dm, TS: "1762520000.000500", User: ada,
		NoteFiles: &richtext.NoteFiles{User: unseen, Sender: ada, Kind: "generic", PrettyType: "Binary"}}
	if got, want := b.notifyBody(core.Event{Type: "message", Channel: dm, TS: hand.TS, Msg: &hand}, conv), "shared ’s file"; got != want {
		t.Errorf("an unseen uploader's file: %q, Slack %q", got, want)
	}
	if asked := ca.S.TakeMissingUsers(); !slices.Contains(asked, unseen) {
		t.Errorf("the toast doesn't ask for the uploader (getMemberById fetchIfMissing): %v", asked)
	}

	// the toast's message says upload; the stored copy loses it before
	// the toast is built (noteFresh, after its lookups)
	const up = "1762520000.000300"
	m, _ := ca.HeldMessage(dm, up)
	ev := core.Event{Type: "message", Channel: dm, TS: up, Msg: &m}
	if got := b.notifyBody(ev, conv); got != "uploaded this file" {
		t.Fatalf("the upload as it came: %q, Slack %q", got, "uploaded this file")
	}
	if _, ok := ca.S.MutateNoted(dm, up, func(x *slack.Message) { x.Upload = false }); !ok { // a copy without upload took its place
		t.Fatal("the upload isn't held")
	}
	if got, want := b.notifyBody(b.noteFresh(ev), conv), "uploaded this file"; got != want {
		t.Errorf("the upload read again from a copy without upload: %q, Slack %q", got, want)
	}
}
