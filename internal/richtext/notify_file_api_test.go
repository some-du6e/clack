package richtext

import "github.com/some-du6e/clack/internal/slack"

// The notification file API (red branches stub these).
func fileTypeOf(f *slack.File) string        { return FileType(f) }
func noteFilesOf(fs []slack.File) *NoteFiles { return NoteFilesOf(fs) }
func noteFileTitle(title string) string      { return NoteFileTitle(title) }
