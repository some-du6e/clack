package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// fetchFileById's stand-ins for files.info's refusals, and what a tab, a
// canvas or a list reads off them (getFileStatus, maybeGetFileStatusString,
// SmallFileEntity's icon, the eye-closed swap).
func TestFileStandIn(t *testing.T) {
	refuse := func(code string, data map[string]any) error {
		body := map[string]any{"ok": false, "error": code}
		for k, v := range data {
			body[k] = v
		}
		b, _ := json.Marshal(body)
		return fmt.Errorf("files.info: %w", &slack.Error{Method: "files.info", Code: code, Body: b})
	}
	secret := map[string]any{"filetype": "quip", "title": "Secret plan"}
	for _, c := range []struct {
		err                    error
		mode, access           string
		status, name, icon     string
		gated, deleted, noFind bool
	}{
		{refuse("file_deleted", nil), "tombstone", "", "deleted", "Deleted file", "trash", false, true, false},
		{refuse("file_not_found", nil), "not_found", "", "hidden", "Hidden file", "question", false, false, true},
		{refuse("not_visible", secret), "quip", "not_visible", "hidden", "Private canvas", "question", true, false, false},
		{refuse("not_visible", map[string]any{"filetype": "list"}), "list", "not_visible", "hidden", "Private list", "question", true, false, false},
		{refuse("access_denied", map[string]any{"filetype": "quip"}), "quip", "access_denied", "hidden", "Private canvas", "eye-closed", true, false, false},
		{refuse("canvas_disabled_user_team", nil), "canvas_slack_connect_blocked", "canvas_disabled_user_team", "hidden", "Hidden file", "eye-closed", true, false, false},
		{refuse("slack_connect_lists_team_blocked", nil), "lists_slack_connect_blocked", "slack_connect_lists_team_blocked", "hidden", "Hidden file", "eye-closed", true, false, false},
		{refuse("template_not_visible", nil), "template_not_visible", "", "hidden", "Private canvas", "question", true, false, false},
		{refuse("hidden_by_limit", nil), "hidden_by_limit", "", "hidden", "Hidden file", "eye-closed", false, false, false},
	} {
		f, ok := fileStandIn(c.err)
		if !ok {
			t.Errorf("%v: no stand-in", c.err)
			continue
		}
		if f.Mode != c.mode || f.FileAccess != c.access {
			t.Errorf("%v: mode %q file_access %q, want %q %q", c.err, f.Mode, f.FileAccess, c.mode, c.access)
		}
		g, gone := fileGoneOf(&f)
		if !gone || g.Status != c.status || g.Name != c.name || g.Icon != c.icon || g.Gated != c.gated ||
			g.Deleted() != c.deleted || g.NotFound() != c.noFind {
			t.Errorf("%v: %+v (gone %v), want status %q name %q icon %q gated %v deleted %v not found %v",
				c.err, g, gone, c.status, c.name, c.icon, c.gated, c.deleted, c.noFind)
		}
		var ge *FileGoneError
		if !errors.As(fileGoneErr("F1", c.err), &ge) || ge.Gone.Status != c.status {
			t.Errorf("%v: fileGoneErr %v", c.err, fileGoneErr("F1", c.err))
		}
	}
	// a failed read stays an error
	if _, ok := fileStandIn(refuse("ratelimited", nil)); ok {
		t.Error("ratelimited made a stand-in")
	}
	if _, ok := fileStandIn(errors.New("timeout")); ok {
		t.Error("a network error made a stand-in")
	}
	// a files.info file in tombstone mode, or is_deleted, is deleted
	for _, f := range []filesInfoFile{{Mode: "tombstone"}, {Mode: "quip", Filetype: "quip", IsDeleted: true}} {
		if g, gone := fileGoneOf(&f); !gone || g.Status != "deleted" || g.Icon != "trash" {
			t.Errorf("%+v: %+v", f, g)
		}
	}
	if _, gone := fileGoneOf(&filesInfoFile{Mode: "quip", Filetype: "quip", Title: "Notes"}); gone {
		t.Error("a canvas Slack shows is gone")
	}
	// a tab: its own label stays unless the file is gated
	tb := TombstoneTab(Tab{Type: "canvas", Label: "Roadmap", FileID: "F1", Unread: true})
	if tb.Title != "Roadmap" || !tb.Deleted || tb.Unread || tb.Gone == nil {
		t.Errorf("labeled tombstoned tab %+v", tb)
	}
	if tb := TombstoneTab(Tab{Type: "canvas", FileID: "F1", Title: "Notes"}); tb.Title != "Deleted file" {
		t.Errorf("unlabeled tombstoned tab %+v", tb)
	}
}

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
	a.trashFiles([]string{fakeslack.CanvasChannel}, true)
	var ge *FileGoneError
	if _, err := a.Canvas(ctx, fakeslack.CanvasChannel); !errors.As(err, &ge) || !ge.Gone.Deleted() {
		t.Errorf("a tombstoned canvas read: %v", err)
	}
	a.trashFiles([]string{fakeslack.CanvasChannel}, false)
	if d, err := a.Canvas(ctx, fakeslack.CanvasChannel); err != nil || d == nil {
		t.Errorf("the canvas after file_undeleted: %v", err)
	}
}
