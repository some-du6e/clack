package core

import (
	"context"
	"net/url"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// RemoveFileFromMessage is chatRemoveFile: chat.removeFile {channel, ts,
// file}, and the stored message drops the file (files and x_files) once
// Slack answers ok; an error leaves it.
func TestRemoveFileFromMessage(t *testing.T) {
	a, f := testApp(t)
	const ch, ts = "D0REMOVE01", "1700000500.000100"
	m := slack.Message{TS: ts, User: "USELF", Files: []slack.File{{ID: "F0A"}, {ID: "F0B"}}, XFiles: []string{"F0A", "F0B"}}
	a.S.SetLatest(ch, []slack.Message{m}, false)
	var got url.Values
	f.hooks["chat.removeFile"] = func(v url.Values) { got = v }
	f.reply["chat.removeFile"] = `{"ok":false,"error":"cant_update_message"}`
	if err := a.RemoveFileFromMessage(context.Background(), ch, ts, "F0B"); err == nil {
		t.Fatal("an error answer reported ok")
	}
	if s, _ := a.S.Find(ch, ts); len(s.Files) != 2 {
		t.Fatalf("a refused removal took the file out: %+v", s.Files)
	}
	f.reply["chat.removeFile"] = `{"ok":true}`
	if err := a.RemoveFileFromMessage(context.Background(), ch, ts, "F0A"); err != nil {
		t.Fatal(err)
	}
	if got.Get("channel") != ch || got.Get("ts") != ts || got.Get("file") != "F0A" {
		t.Errorf("chat.removeFile sent %v", got)
	}
	s, _ := a.S.Find(ch, ts)
	if len(s.Files) != 1 || s.Files[0].ID != "F0B" || len(s.XFiles) != 1 || s.XFiles[0] != "F0B" {
		t.Errorf("after the removal: files %+v, x_files %v", s.Files, s.XFiles)
	}
}
