package core

import (
	"encoding/json"
	"errors"
	"sync"

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
	// is_deleted, is_hidden_by_limit, is_tombstoned (DLP) and its preview
	IsDeleted, HiddenLimit, Tombstoned bool
	Preview                            string
}

// vITP's predicates, which pick the views' states.

// Tombstone is isTombstone: the store's tombstone mode.
func (g FileGone) Tombstone() bool { return g.Mode == "tombstone" }

// Deleted is isTombstone || isFileDeleted.
func (g FileGone) Deleted() bool { return g.Tombstone() || g.IsDeleted }

// NotFound is isNotFound.
func (g FileGone) NotFound() bool { return g.Mode == "not_found" || g.FileAccess == "file_not_found" }

// Denied is isExternalAccessDenied.
func (g FileGone) Denied() bool { return g.Mode == "access_denied" || g.FileAccess == "access_denied" }

// CanvasBlocked is isBlockedExternalCanvas.
func (g FileGone) CanvasBlocked() bool {
	return g.Mode == "canvas_slack_connect_blocked" || canvasBlockedErrors[g.FileAccess]
}

// ListBlocked is isBlockedExternalList.
func (g FileGone) ListBlocked() bool {
	return g.Mode == "lists_slack_connect_blocked" || listBlockedErrors[g.FileAccess]
}

// TemplateHidden is isTemplateNotVisible (X0cl).
func (g FileGone) TemplateHidden() bool {
	return g.Mode == "template_not_visible" || g.FileAccess == "template_not_visible"
}

// NotVisible is isNotVisible, which takes in TemplateHidden.
func (g FileGone) NotVisible() bool {
	return g.Mode == "not_visible" || g.FileAccess == "not_visible" || g.TemplateHidden()
}

// Limit is isHiddenByStorageLimit.
func (g FileGone) Limit() bool { return g.Mode == "hidden_by_limit" || g.HiddenLimit }

// DLPMessage is getDLPTombstoneMessage: the file's content or preview,
// else "Tombstoned" (Clack keeps no canvas content: the preview).
func (g FileGone) DLPMessage() string {
	if g.Preview != "" {
		return g.Preview
	}
	return "Tombstoned"
}

// FileGoneError is a canvas or list load whose file Slack doesn't show:
// fetchFileById's stand-in or a files.info file shouldShowFile rejects.
// The views draw its state from Gone (QuipDeletedState,
// BaseListErrorState), not a failed load.
type FileGoneError struct {
	ID   string
	Gone FileGone
}

func (e *FileGoneError) Error() string { return "core: file " + e.ID + " is " + e.Gone.Status }

// fileGoneErr is files.info's err for id as a *FileGoneError when
// fetchFileById stores a stand-in for it, else err.
func fileGoneErr(id string, err error) error {
	if f, ok := fileStandIn(err); ok {
		if g, ok := fileGoneOf(&f); ok {
			return &FileGoneError{ID: id, Gone: g}
		}
	}
	return err
}

// fileGoneOf is f's FileGone, ok false for a file Slack shows.
func fileGoneOf(f *filesInfoFile) (FileGone, bool) {
	st := richtext.FileStatusOf(richtext.FileFacts{Mode: f.Mode, FileAccess: f.FileAccess, IsDeleted: f.IsDeleted,
		HiddenByLimit: f.HiddenLimit, Tombstoned: f.Tombstoned})
	if st == "normal" {
		return FileGone{}, false
	}
	g := FileGone{Status: st, Name: richtext.FileStatusName(st, f.Filetype, f.Mode), Mode: f.Mode, FileAccess: f.FileAccess,
		Filetype: f.Filetype, Title: f.Title, IsDeleted: f.IsDeleted, HiddenLimit: f.HiddenLimit, Tombstoned: f.Tombstoned,
		Preview: f.Preview}
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

// Slack's files store follows file_deleted and file_undeleted as they
// come: filesDeleted (boot-render) runs tombstoneFiles on every deleted id
// the store holds, so a tab and an open canvas or list of it change at
// once, with no fetch; filesUndeleted (under file_trash_updates) refetches
// the restored files it holds (fetchAndUpsertFilesById, reason
// "file-undeleted-rtm"). Clack keeps the tombstones here: every files.info
// read of a canvas, a list or a tab file answers with the tombstone until
// file_undeleted clears it, and the event goes out as Event{Type:
// "file_gone", ID, Name: "file_deleted" or "file_undeleted"} for the views
// and the tab bar.

type fileTombs struct {
	mu sync.Mutex
	m  map[string]bool
}

var fileTombStates = newPerApp() // *App -> *fileTombs

func (a *App) fileTombs() *fileTombs {
	if v, ok := fileTombStates.Load(a); ok {
		return v.(*fileTombs)
	}
	v, _ := fileTombStates.LoadOrStore(a, &fileTombs{m: map[string]bool{}})
	return v.(*fileTombs)
}

// tombstoned is id's tombstone (fetchFileById's file_deleted stand-in),
// ok false when file_deleted didn't name it.
func (a *App) tombstoned(id string) (FileGone, bool) {
	t := a.fileTombs()
	t.mu.Lock()
	ok := t.m[id]
	t.mu.Unlock()
	if !ok {
		return FileGone{}, false
	}
	f := filesInfoFile{Mode: "tombstone", IsDeleted: true}
	return fileGoneOf(&f)
}

// TombstoneTab is t once its file is deleted, as Tabs would read it.
func TombstoneTab(t Tab) Tab {
	f := filesInfoFile{Mode: "tombstone", IsDeleted: true}
	g, _ := fileGoneOf(&f)
	t.applyGone(g)
	t.Unread = false
	return t
}

// applyGone is a gone file's tab: the tab keeps its own label
// (useTabLabel), but a gated file (ChannelTabLabelBaseComponent's
// eye-closed swap) and an unlabeled one read the file's Name: its status
// string first (maaT → maybeGetFileStatusString).
func (t *Tab) applyGone(g FileGone) {
	t.Gone, t.Deleted = &g, g.Status == "deleted"
	t.TitleSpans = nil
	if t.Label == "" || g.Gated {
		t.Title = g.Name
	} else {
		t.Title = t.Label
	}
}

// onFileTrash is filesDeleted / filesUndeleted for the tombstones. Raw
// carries file_id, or file_ids.
func (a *App) onFileTrash(typ string, raw json.RawMessage) {
	var e struct {
		FileID  string   `json:"file_id"`
		FileIDs []string `json:"file_ids"`
	}
	_ = json.Unmarshal(raw, &e)
	ids := e.FileIDs
	if e.FileID != "" {
		ids = append(ids, e.FileID)
	}
	t := a.fileTombs()
	t.mu.Lock()
	for _, id := range ids {
		if typ == "file_deleted" {
			t.m[id] = true
		} else {
			delete(t.m, id)
		}
	}
	t.mu.Unlock()
	for _, id := range ids {
		a.emit(Event{Type: "file_gone", ID: id, Name: typ})
	}
}

// untombstone clears id's tombstone (a restore this client made).
func (a *App) untombstone(id string) {
	t := a.fileTombs()
	t.mu.Lock()
	delete(t.m, id)
	t.mu.Unlock()
}
