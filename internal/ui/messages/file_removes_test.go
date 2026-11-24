package messages

import (
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// fileRemoves is the gallery's removableFileIds for the viewer's own
// message: every file removed from it when all were uploaded with it
// (x_files), else the files shared in that the viewer owns, deleted;
// none for canvases shared in, nor for someone else's files.
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
	if got := fileRemoves(&m, "USELF"); len(got) != 2 || got["F0EXE"] || got["F0ZIP"] {
		t.Errorf("uploaded with the message: %v, want both removed (false)", got)
	}
	m = core.Message{Doc: doc(exe, zip), XFiles: []string{"F0EXE"}} // one shared in
	if got := fileRemoves(&m, "USELF"); len(got) != 1 || !got["F0ZIP"] {
		t.Errorf("one shared in: %v, want F0ZIP deleted (true) only", got)
	}
	m = core.Message{Doc: doc(theirs, canvas)}
	if got := fileRemoves(&m, "USELF"); got != nil {
		t.Errorf("someone else's file and a canvas shared in: %v, want none", got)
	}
	if got := fileRemoves(&core.Message{}, "USELF"); got != nil {
		t.Errorf("no files: %v", got)
	}
}
