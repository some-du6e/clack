package ui

import (
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// noteTestMessage is m as core converts it for a notification's text (red
// branches stub it without the files' note).
func noteTestMessage(m *slack.Message) *core.Message {
	return &core.Message{TS: m.TS, User: m.User, Subtype: m.Subtype, Text: m.Text, ThreadTS: m.ThreadTS,
		Doc: richtext.FromMessage(noResolver{}, m), Blocks: m.Blocks, Attachments: m.RawAttachments, BotID: m.BotID,
		NoteFiles: core.NoteFilesOf(m), Upload: m.Upload}
}

// notePossessiveOf is notePossessive (red branches stub it).
func notePossessiveOf(name, loc string) string { return notePossessive(name, loc) }
