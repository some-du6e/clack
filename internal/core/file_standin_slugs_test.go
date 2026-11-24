package core

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// An inline canvas or file slug whose file files.info can't find keeps
// fetchFileById's {mode: "not_found"} stand-in, which getFileStatus calls
// hidden: a canvas slug reads "Private canvas" with the lock, a file slug
// "Hidden file" with eye-closed, not deleted's trash. A deleted one reads
// deleted. The tombstones (file_deleted, a delete here) answer every read.
func TestFileStandInSlugs(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	fs.SetFileGone("F0SLGONE01", "file_not_found", nil)
	fs.SetFileGone("F0SLGONE02", "file_deleted", nil)
	a := New(slack.New(fs.Creds()), store.New(""))
	ctx := context.Background()
	for id, want := range map[string]string{"F0SLGONE01": "hidden", "F0SLGONE02": "deleted"} {
		if !a.resolveCanvasRef(ctx, id) {
			t.Fatalf("%s didn't resolve", id)
		}
		if r, _ := a.S.CanvasRef(id); r.Status != want {
			t.Errorf("%s: status %q, want %q", id, r.Status, want)
		}
	}
	m := &slack.Message{TS: "1.0", Text: "x", Blocks: json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[
		{"type":"canvas","file_id":"F0SLGONE01"},{"type":"text","text":" "},
		{"type":"file","file_id":"F0SLGONE01","url":"https://x.slack.com/files/U1/F0SLGONE01/f"}]}]}]`)}
	var slugs []richtext.Span
	for _, b := range richtext.FromMessage(a.S, m).Blocks {
		for _, s := range b.Spans {
			if s.Kind == richtext.Canvas {
				slugs = append(slugs, s)
			}
		}
	}
	if len(slugs) != 2 {
		t.Fatalf("slugs %+v", slugs)
	}
	if s := slugs[0]; s.Text != "Private canvas" || s.Icon != "lock" || s.Off {
		t.Errorf("canvas slug %q icon %q off %v, want Private canvas, lock", s.Text, s.Icon, s.Off)
	}
	if s := slugs[1]; s.Text != "Hidden file" || s.Icon != "eye-closed" {
		t.Errorf("file slug %q icon %q, want Hidden file, eye-closed", s.Text, s.Icon)
	}
	// the tombstones: a canvas read after file_deleted answers deleted
	// without asking, and file_undeleted lets the read through again
	fsTrash(a, fakeslack.CanvasChannel, true)
	if _, err := a.Canvas(ctx, fakeslack.CanvasChannel); !fsDeletedErr(err) {
		t.Errorf("a tombstoned canvas read: %v", err)
	}
	fsTrash(a, fakeslack.CanvasChannel, false)
	if d, err := a.Canvas(ctx, fakeslack.CanvasChannel); err != nil || d == nil {
		t.Errorf("the canvas after file_undeleted: %v", err)
	}
}
