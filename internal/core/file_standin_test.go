package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
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
