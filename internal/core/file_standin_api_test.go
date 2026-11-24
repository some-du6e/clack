package core

// TestFileStandInSlugs' reach into the fix's API (a red run on the old
// code swaps this file for stubs).

import "errors"

// fsTrash is file_deleted (deleted) or file_undeleted for id.
func fsTrash(a *App, id string, deleted bool) { a.trashFiles([]string{id}, deleted) }

// fsDeletedErr is whether err is a deleted file's read.
func fsDeletedErr(err error) bool {
	var ge *FileGoneError
	return errors.As(err, &ge) && ge.Gone.Deleted()
}
