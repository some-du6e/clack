package richtext

import "github.com/some-du6e/clack/internal/slack"

// The notification file API (red branches stub these).
func fileTypeOf(f *slack.File) string        { return FileType(f) }
func noteFilesOf(fs []slack.File) *NoteFiles { return NoteFilesOf(fs, nil) }
func noteFileTitle(title string) string      { return NoteFileTitle(title) }

// noteFilesFrom is NoteFilesOf with the file store's copy of the first
// file.
func noteFilesFrom(fs []slack.File, first *slack.File) *NoteFiles { return NoteFilesOf(fs, first) }
