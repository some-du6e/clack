package messages

import (
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// fileRemoves is the gallery's removableFileIds for a message the viewer
// may delete: each file in x_files taken out of it (not only when all
// are), a canvas shared in taken out unless it's all the message has, a
// file shared in that the viewer owns deleted (unless file deleting is
// off), and nothing for someone else's file.
func TestFileRemoves(t *testing.T) {
	doc := func(fs ...*richtext.File) richtext.Doc {
		var d richtext.Doc
		for _, f := range fs {
			d.Blocks = append(d.Blocks, richtext.Block{Kind: richtext.FileBlock, File: f})
		}
		return d
	}
	exe := &richtext.File{ID: "F0EXE", Type: "exe", User: "USELF"}
	zip := &richtext.File{ID: "F0ZIP", Type: "zip", User: "USELF"}
	theirs := &richtext.File{ID: "F0THEIRS", Type: "pdf", User: "UOTHER"}
	canvas := &richtext.File{ID: "F0CANVAS", Type: "quip", Mode: "quip", User: "USELF"}

	m := core.Message{Doc: doc(exe, zip), XFiles: []string{"F0EXE", "F0ZIP"}}
	if got := fileRemoves(&m, "USELF", true); len(got) != 2 || got["F0EXE"] || got["F0ZIP"] {
		t.Errorf("uploaded with the message: %v, want both removed (false)", got)
	}
	m = core.Message{Doc: doc(exe, zip), XFiles: []string{"F0EXE"}} // one shared in
	if got := fileRemoves(&m, "USELF", true); len(got) != 2 || got["F0EXE"] || !got["F0ZIP"] {
		t.Errorf("one shared in: %v, want F0EXE removed (false) and F0ZIP deleted (true)", got)
	}
	if got := fileRemoves(&m, "USELF", false); len(got) != 1 || got["F0EXE"] {
		t.Errorf("file deleting off: %v, want F0EXE removed only", got)
	}
	m = core.Message{Doc: doc(theirs, canvas)}
	if got := fileRemoves(&m, "USELF", true); len(got) != 1 || got["F0CANVAS"] {
		t.Errorf("someone else's file and a canvas shared in: %v, want the canvas removed (false) only", got)
	}
	m = core.Message{Doc: doc(canvas)}
	if got := fileRemoves(&m, "USELF", true); got != nil {
		t.Errorf("a canvas that is all the message has: %v, want none", got)
	}
	if got := fileRemoves(&core.Message{}, "USELF", true); got != nil {
		t.Errorf("no files: %v", got)
	}
}
