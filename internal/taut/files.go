package taut

import (
	"context"
	"fmt"
)

// Slack's pending files are the composer's attachments: plugins read them
// in state.pendingFileUploads (and state.files once their bytes are up),
// rename them with the updatePendingFile action, and commit the rename
// with the maybePersistPendingFileData or editFileApi thunks
// (AnonymiseFileNames' toggle on the attachment chip).
//
// The composer owns the files, so it sets the hooks below. Each is a plain
// func var, nil-checked where taut calls it: unset, plugins see no pending
// files and nothing runs. They are read on the JS thread (PendingFiles,
// RenamePendingFile) or a thunk's goroutine, so set them before the runtime
// starts (an init func) and keep the JS-thread ones quick, with no network.

// PendingFile is one file attached to a composer and not yet sent.
type PendingFile struct {
	ID       string // pendingFileId (MultiFileUploadItem's pendingFile)
	Name     string // the file name it is uploaded under
	Title    string // the name it is shown and shared under
	Size     int64
	Mimetype string
	FileID   string // Slack's file id once its bytes are uploaded ("" before)
}

var (
	// PendingFiles lists every composer's pending files.
	PendingFiles func() []PendingFile
	// RenamePendingFile sets a pending file's title (updatePendingFile):
	// its chip and the upload follow. false when there is no such file.
	RenamePendingFile func(id, title string) bool
	// PersistPendingFile is maybePersistPendingFileData: the pending file's
	// edited data is kept for its upload. An error when the file is gone.
	PersistPendingFile func(ctx context.Context, id string) error
	// EditFile is editFileApi for a file the composer holds (by its Slack
	// file id): it renames it and returns it. ok is false when the
	// composer has no such file.
	EditFile func(ctx context.Context, fileID, title string) (f PendingFile, ok bool, err error)
)

// PendingFilesChanged tells plugins the pending files changed (added,
// removed, renamed, uploaded). Free before the plugin runtime starts.
func (t *Taut) PendingFilesChanged() {
	if t == nil || !t.Started() {
		return
	}
	t.mirror.invalidate("pendingFileUploads")
	t.mirror.invalidate("files")
}

// pendingEntry is pendingFileUploads[key] or files[key], or nil.
func pendingEntry(slice, key string) any {
	list := PendingFiles
	if list == nil || key == "" {
		return nil
	}
	for _, f := range list() {
		switch {
		case slice == "pendingFileUploads" && f.ID == key:
			return f.pendingUpload()
		case slice == "files" && f.FileID == key:
			return f.slackFile()
		}
	}
	return nil
}

func pendingKeys(slice string) []string {
	list := PendingFiles
	if list == nil {
		return []string{}
	}
	out := []string{}
	for _, f := range list() {
		if slice == "pendingFileUploads" {
			out = append(out, f.ID)
		} else if f.FileID != "" {
			out = append(out, f.FileID)
		}
	}
	return out
}

// pendingUpload is Slack's pendingFileUploads entry.
func (f PendingFile) pendingUpload() map[string]any {
	e := map[string]any{
		"id": f.ID, "pendingFileId": f.ID, "title": f.Title, "name": f.Name, "size": f.Size,
		"mimetype": f.Mimetype, "status": "pending",
		"file": map[string]any{"name": f.Name, "title": f.Title, "size": f.Size, "type": f.Mimetype},
	}
	if f.FileID != "" {
		e["fileId"] = f.FileID
		e["status"] = "uploaded"
	}
	return e
}

// slackFile is Slack's files entry for an uploaded pending file.
func (f PendingFile) slackFile() map[string]any {
	return map[string]any{"id": f.FileID, "name": f.Name, "title": f.Title, "size": f.Size, "mimetype": f.Mimetype, "is_external": false}
}

func persistPendingFile(ctx context.Context, id string) error {
	if f := PersistPendingFile; f != nil {
		return f(ctx, id)
	}
	return nil
}

// editFile is editFileApi's native: the composer renames a file it holds.
// Clack has no other files to rename (shared files go through Slack's own
// file dialog, which Clack doesn't have), so any other id is an error.
func (t *Taut) editFile(ctx context.Context, fileID, title string) (any, error) {
	if fileID == "" {
		return nil, fmt.Errorf("editFileApi needs a fileId")
	}
	if edit := EditFile; edit != nil {
		f, ok, err := edit(ctx, fileID, title)
		if err != nil {
			return nil, err
		}
		if ok {
			t.PendingFilesChanged()
			return map[string]any{"file": f.slackFile()}, nil
		}
	}
	return nil, fmt.Errorf("editFileApi: no file %s in Clack", fileID)
}
