package core

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// TestNoteFilesNowReadsTheFileStore: a toast reads the first file as
// Slack's file store holds it (Eley's getFileById): a message whose copy
// is a stub (check_file_info) reads a complete copy another held message
// carries, as Slack's store merged it (processFile), while the clips and
// the sender stay the message's own; a message whose copy is complete
// reads its own; one the store doesn't hold keeps its conversion's.
func TestNoteFilesNowReadsTheFileStore(t *testing.T) {
	a, _ := testApp(t)
	msg := func(s string) slack.Message {
		var m slack.Message
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		return m
	}
	full := msg(`{"type":"message","ts":"1762510000.000100","user":"U2","text":"","upload":true,"files":[{"id":"F0SYNTH0001","user":"U2",` +
		`"name":"x.png","title":"Q3 report","mimetype":"image/png","filetype":"png","pretty_type":"PNG","mode":"hosted",` +
		`"url_private":"https://files.example.invalid/x.png","thumb_360":"https://files.example.invalid/t360.png"}]}`)
	stub := msg(`{"type":"message","ts":"1762520000.000100","user":"U1","text":"","files":[{"id":"F0SYNTH0001","file_access":"check_file_info",` +
		`"created":1762520000,"timestamp":1762520000,"user":"U2"},{"id":"F0SYNTH0002","subtype":"slack_audio","url_private":"https://files.example.invalid/a"}]}`)
	a.S.SetLatest("C1", []slack.Message{full, stub}, false)

	m, ok := a.HeldMessage("C1", stub.TS)
	if !ok {
		t.Fatal("the stub's message isn't held")
	}
	if n, ok := noteFilesConv(&m); !ok || n.Title != "" || n.Kind != "generic" {
		t.Errorf("the conversion reads the message's own copy: %+v", n)
	}
	want := noteView{Title: "Q3 report", Kind: "image", PrettyType: "PNG", User: "U2", Sender: "U1", Clips: 1, AudioClip: true}
	if n, ok := noteFilesNow(a, &m); !ok || n != want {
		t.Errorf("a stub reads the store's complete copy: %+v, want %+v", n, want)
	}

	own, _ := a.HeldMessage("C1", full.TS)
	if n, ok := noteFilesNow(a, &own); !ok || n.Title != "Q3 report" || n.Sender != "U2" {
		t.Errorf("a complete copy reads itself: %+v", n)
	}

	gone := Message{Channel: "C1", TS: "1762530000.000100", NoteFiles: own.NoteFiles}
	if n, ok := noteFilesNow(a, &gone); !ok || n.Title != "Q3 report" {
		t.Errorf("a message the store doesn't hold keeps its own: %+v", n)
	}
}
