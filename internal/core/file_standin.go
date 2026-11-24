package core

import (
	"encoding/json"
	"errors"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// A file files.info refuses, as Slack's files store keeps it. fetchFileById
// (client-boot, "Fetches file info") doesn't fail on these answers: it
// stores a stand-in object, so every surface reads the file's state from
// it (getFileStatus, shouldShowFile) and nothing asks again:
//
//   - file_deleted: {mode: "tombstone", is_deleted: true} (the store's
//     tombstone, as tombstoneFiles makes it on the file_deleted event).
//   - file_not_found: {mode: "not_found"}.
//   - a Slack Connect or admin refusal of a canvas (IqFK's list) or a list:
//     mode canvas_slack_connect_blocked or lists_slack_connect_blocked,
//     file_access the code.
//   - access_denied (make_external_access_denied): mode the answer's
//     filetype, file_access "access_denied".
//   - not_visible (make_not_visible): the answer's title, mode its
//     filetype, file_access "not_visible".
//   - template_not_visible: filetype "quip", mode the code.
//   - hidden_by_limit: mode "hidden_by_limit".
//
// Any other error is a failed read, kept as an error.

// Slack's refusals of a canvas or a list (IqFK, the wefN codes).
var (
	canvasBlockedErrors = map[string]bool{"canvas_globally_disabled": true, "slack_connect_team_blocked": true,
		"slack_connect_canvas_sharing_blocked": true, "canvas_disabled_user_team": true, "canvas_disabled_file_team": true}
	listBlockedErrors = map[string]bool{"slack_connect_lists_team_blocked": true, "slack_connect_lists_sharing_blocked": true}
)

// fileStandIn is fetchFileById's stand-in for a files.info error, ok false
// for an error that isn't one of its answers.
func fileStandIn(err error) (filesInfoFile, bool) {
	var se *slack.Error
	if !errors.As(err, &se) {
		return filesInfoFile{}, false
	}
	var body struct {
		Filetype string `json:"filetype"`
		Title    string `json:"title"`
	}
	_ = json.Unmarshal(se.Body, &body)
	switch code := se.Code; {
	case code == "file_deleted":
		return filesInfoFile{Mode: "tombstone", IsDeleted: true}, true
	case code == "file_not_found":
		return filesInfoFile{Mode: "not_found"}, true
	case canvasBlockedErrors[code]:
		return filesInfoFile{Mode: "canvas_slack_connect_blocked", FileAccess: code}, true
	case listBlockedErrors[code]:
		return filesInfoFile{Mode: "lists_slack_connect_blocked", FileAccess: code}, true
	case code == "access_denied":
		return filesInfoFile{Mode: body.Filetype, FileAccess: "access_denied"}, true
	case code == "not_visible":
		return filesInfoFile{Title: body.Title, Mode: body.Filetype, FileAccess: "not_visible"}, true
	case code == "template_not_visible":
		return filesInfoFile{Filetype: "quip", Mode: code}, true
	case code == "hidden_by_limit":
		return filesInfoFile{Mode: "hidden_by_limit"}, true
	}
	return filesInfoFile{}, false
}

// FileGone is what a surface needs of a file Slack doesn't show
// (shouldShowFile false): its status (getFileStatus: "deleted" or
// "hidden"), the status string Slack names it by (maybeGetFileStatusString),
// the icon SmallFileEntity draws for it (trash for a deleted file,
// eye-closed for one hidden by the storage limit, refused or a blocked
// canvas or list, question for the rest: not found, not visible), and
// Gated: ChannelTabLabelBaseComponent's eye-closed swap (not visible,
// access denied, a blocked canvas or list), which a labeled tab takes. The
// rest of the fields are the stand-in's, for the views' states.
type FileGone struct {
	Status, Name, Icon string
	Gated              bool
	Mode, FileAccess   string
	Filetype, Title    string
}

// fileGoneOf is f's FileGone, ok false for a file Slack shows.
func fileGoneOf(f *filesInfoFile) (FileGone, bool) {
	st := richtext.FileStatusOf(richtext.FileFacts{Mode: f.Mode, FileAccess: f.FileAccess, IsDeleted: f.IsDeleted,
		HiddenByLimit: f.HiddenLimit, Tombstoned: f.Tombstoned})
	if st == "normal" {
		return FileGone{}, false
	}
	g := FileGone{Status: st, Name: richtext.FileStatusName(st, f.Filetype, f.Mode), Mode: f.Mode, FileAccess: f.FileAccess,
		Filetype: f.Filetype, Title: f.Title}
	notVisible := f.Mode == "not_visible" || f.FileAccess == "not_visible" || f.Mode == "template_not_visible" || f.FileAccess == "template_not_visible"
	denied := f.Mode == "access_denied" || f.FileAccess == "access_denied"
	blocked := f.Mode == "canvas_slack_connect_blocked" || canvasBlockedErrors[f.FileAccess] ||
		f.Mode == "lists_slack_connect_blocked" || listBlockedErrors[f.FileAccess]
	limit := f.Mode == "hidden_by_limit" || f.HiddenLimit
	g.Gated = notVisible || denied || blocked
	switch {
	case st == "deleted":
		g.Icon = "trash"
	case limit || denied || blocked:
		g.Icon = "eye-closed"
	default:
		g.Icon = "question"
	}
	return g, true
}
