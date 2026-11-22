package core

import "github.com/some-du6e/clack/internal/richtext"

// noteView is what TestNoteFilesNowReadsTheFileStore reads of a toast's
// NoteFiles (red branches stub noteFilesNow).
type noteView struct {
	Title, Kind, PrettyType, User, Sender string
	Clips                                 int
	AudioClip                             bool
}

// noteFilesNow is a.NoteFilesNow(m) as a noteView, ok false for none.
func noteFilesNow(a *App, m *Message) (noteView, bool) { return viewOf(a.NoteFilesNow(m)) }

// noteFilesConv is m's conversion's NoteFiles as a noteView.
func noteFilesConv(m *Message) (noteView, bool) { return viewOf(m.NoteFiles) }

func viewOf(n *richtext.NoteFiles) (noteView, bool) {
	if n == nil {
		return noteView{}, false
	}
	return noteView{Title: n.Title, Kind: n.Kind, PrettyType: n.PrettyType, User: n.User, Sender: n.Sender, Clips: n.Clips, AudioClip: n.AudioClip}, true
}
