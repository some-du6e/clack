package popups

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// isFileTheLastContent: the file is all the message has (no text, no
// blocks, no other file), so its x asks to delete the message instead.
func TestFileIsTheLastContent(t *testing.T) {
	fb := func(id string) richtext.Block {
		return richtext.Block{Kind: richtext.FileBlock, File: &richtext.File{ID: id}}
	}
	if !fileIsTheLastContent(&core.Message{Doc: richtext.Doc{Blocks: []richtext.Block{fb("F1")}}}, "F1") {
		t.Error("a message of one file: not its last content")
	}
	if fileIsTheLastContent(&core.Message{Doc: richtext.Doc{Blocks: []richtext.Block{fb("F1"), fb("F2")}}}, "F1") {
		t.Error("two files: the first is the last content")
	}
	if fileIsTheLastContent(&core.Message{Text: "here it is", Doc: richtext.Doc{Blocks: []richtext.Block{fb("F1")}}}, "F1") {
		t.Error("with text: the file is the last content")
	}
	if fileIsTheLastContent(&core.Message{Blocks: json.RawMessage(`[{"type":"rich_text"}]`), Doc: richtext.Doc{Blocks: []richtext.Block{fb("F1")}}}, "F1") {
		t.Error("with blocks: the file is the last content")
	}
}
