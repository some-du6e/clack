package composer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"weak"

	"github.com/some-du6e/yoloslackgoclient/internal/taut"
)

// Pending attachments as Slack's pending files, for plugins: taut serves
// state.pendingFileUploads (and state.files once a file's bytes are up)
// from these hooks, and AnonymiseFileNames' toggle renames a file through
// them (updatePendingFile, then maybePersistPendingFileData, or editFileApi
// by its Slack file id). A rename changes the chip's title: the chip shows
// it and the file is shared under it (uploadBatch, Files).
//
// The hooks only run when a plugin reads or renames pending files; until
// the plugin runtime starts, pendingChanged is a nil check and one lock.

func init() {
	taut.PendingFiles = pendingFiles
	taut.RenamePendingFile = renamePendingFile
	taut.PersistPendingFile = persistPendingFile
	taut.EditFile = editPendingFile
}

// attNextID numbers chips across every composer, so pendingFileIds are
// unique in the one store plugins see.
var attNextID atomic.Int64

// attRegistry holds every composer's attachments, weakly: a dropped
// composer's files leave with it.
var attRegistry struct {
	sync.Mutex
	all []weak.Pointer[attachments]
}

func registerAttachments(a *attachments) {
	attRegistry.Lock()
	attRegistry.all = append(attRegistry.all, weak.Make(a))
	attRegistry.Unlock()
}

// liveAttachments is every composer's attachments still alive.
func liveAttachments() []*attachments {
	attRegistry.Lock()
	defer attRegistry.Unlock()
	out := make([]*attachments, 0, len(attRegistry.all))
	keep := attRegistry.all[:0]
	for _, w := range attRegistry.all {
		if a := w.Value(); a != nil {
			out = append(out, a)
			keep = append(keep, w)
		}
	}
	clear(attRegistry.all[len(keep):])
	attRegistry.all = keep
	return out
}

// pendingChanged tells plugins the pending files changed. Call without
// attachments.mu held.
func pendingChanged() { currentTaut().PendingFilesChanged() }

// pendingLocked is c as taut's PendingFile. Call with attachments.mu held.
func (c *attChip) pendingLocked() taut.PendingFile {
	p := taut.PendingFile{ID: attPendingID(c.id), Name: c.name, Title: c.title, Size: c.size(), Mimetype: attMime(c.name)}
	if c.prepared != nil {
		p.FileID = c.prepared.ID
	}
	return p
}

// pendingFiles is taut.PendingFiles: every composer's pending files. JS
// thread: memory only.
func pendingFiles() []taut.PendingFile {
	var out []taut.PendingFile
	for _, a := range liveAttachments() {
		a.mu.Lock()
		for _, c := range a.chips {
			out = append(out, c.pendingLocked())
		}
		a.mu.Unlock()
	}
	return out
}

// withChip runs f on the chip match picks, with its attachments locked,
// and reports whether there was one.
func withChip(match func(c *attChip) bool, f func(a *attachments, c *attChip)) (*attachments, bool) {
	for _, a := range liveAttachments() {
		a.mu.Lock()
		for _, c := range a.chips {
			if match(c) {
				f(a, c)
				a.mu.Unlock()
				return a, true
			}
		}
		a.mu.Unlock()
	}
	return nil, false
}

func byPendingID(id string) func(c *attChip) bool {
	return func(c *attChip) bool { return attPendingID(c.id) == id }
}

// renamePendingFile is taut.RenamePendingFile (updatePendingFile): the
// chip shows title and the file is shared under it.
func renamePendingFile(id, title string) bool {
	if title == "" {
		return false
	}
	a, ok := withChip(byPendingID(id), func(_ *attachments, c *attChip) {
		c.title = title
		if c.prepared != nil {
			c.prepared.Title = title
		}
	})
	if ok {
		a.h.Invalidate()
		// this runs on the JS thread, which already read the change
		// again; Go's views catch up off it (Started takes taut's lock)
		go pendingChanged()
	}
	return ok
}

// persistPendingFile is taut.PersistPendingFile
// (maybePersistPendingFileData). The title a rename set is the one the
// upload carries (uploadBatch reads it at send); this keeps the prepared
// upload's record in step, and fails for a file that is gone (removed or
// sent), so the plugin can say so.
func persistPendingFile(_ context.Context, id string) error {
	if _, ok := withChip(byPendingID(id), func(_ *attachments, c *attChip) {
		if c.prepared != nil {
			c.prepared.Title = c.title
		}
	}); !ok {
		return fmt.Errorf("no pending file %s", id)
	}
	return nil
}

// editPendingFile is taut.EditFile (editFileApi) for a pending file whose
// bytes are uploaded, by its Slack file id.
func editPendingFile(_ context.Context, fileID, title string) (taut.PendingFile, bool, error) {
	if title == "" {
		return taut.PendingFile{}, false, fmt.Errorf("editFileApi needs a title")
	}
	var p taut.PendingFile
	a, ok := withChip(func(c *attChip) bool { return c.prepared != nil && c.prepared.ID == fileID }, func(_ *attachments, c *attChip) {
		c.title = title
		c.prepared.Title = title
		p = c.pendingLocked()
	})
	if !ok {
		return taut.PendingFile{}, false, nil
	}
	a.h.Invalidate()
	pendingChanged()
	return p, true, nil
}
