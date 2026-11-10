package core

// The Files view's file actions (ui/views/files.go), ported from Slack's
// web client (CDN build a84662d2d135, gantry-v2-async-client-boot-shared):
//
//   - starring: addCanvasToFavorites / addListToFavorites (files.favorites.add
//     {file_id}), removeCanvasFromFavorites / removeListFromFavorites
//     (files.favorites.remove {file_id}), and maybeFetchStarredUnifiedFiles
//     (files.favorites.list {type: "all"}, file_ids reversed). The fetchers
//     come from createApiMethodFetcher, which snake-cases its arguments
//     (fileId → file_id) and keeps reason out of them.
//   - editing: editFile (files.edit {file, title, is_canvas_template: false}).
//
// The starred set lives in memory per App, like Slack's redux store.

import (
	"context"
	"net/url"
	"sync"
)

type fileStars struct {
	mu     sync.Mutex
	ids    []string // newest first (Slack reverses file_ids)
	loaded bool
}

var fileStarStores sync.Map // *App -> *fileStars

func (a *App) fileStars() *fileStars {
	if v, ok := fileStarStores.Load(a); ok {
		return v.(*fileStars)
	}
	v, _ := fileStarStores.LoadOrStore(a, &fileStars{})
	return v.(*fileStars)
}

func (s *fileStars) set(id string, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.ids[:0:0]
	for _, x := range s.ids {
		if x != id {
			out = append(out, x)
		}
	}
	if on {
		out = append([]string{id}, out...)
	}
	s.ids = out
}

// StarredFiles loads the viewer's starred files (files.favorites.list
// {type: all}) and returns their ids, the most recently starred first.
func (a *App) StarredFiles(ctx context.Context) ([]string, error) {
	var r struct {
		FileIDs []string `json:"file_ids"`
	}
	if err := a.C.Call(ctx, "files.favorites.list", url.Values{"type": {"all"}}, &r); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(r.FileIDs))
	for i := len(r.FileIDs) - 1; i >= 0; i-- {
		ids = append(ids, r.FileIDs[i])
	}
	s := a.fileStars()
	s.mu.Lock()
	s.ids, s.loaded = ids, true
	s.mu.Unlock()
	return append([]string(nil), ids...), nil
}

// StarredFileIDs returns the starred set as last loaded or changed (nil
// before StarredFiles answers).
func (a *App) StarredFileIDs() []string {
	s := a.fileStars()
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ids...)
}

// FileStarred reports whether file id is starred.
func (a *App) FileStarred(id string) bool {
	s := a.fileStars()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.ids {
		if x == id {
			return true
		}
	}
	return false
}

// SetFileStarred marks id starred or not locally, before the call goes out
// (Slack's AP / f_ actions, dispatched ahead of the fetch).
func (a *App) SetFileStarred(id string, on bool) { a.fileStars().set(id, on) }

// StarFile stars (on) or unstars a file: files.favorites.add or
// files.favorites.remove {file_id}. The local set changes first and goes
// back if the call fails, as Slack's thunks do.
func (a *App) StarFile(ctx context.Context, id string, on bool) error {
	m := "files.favorites.add"
	if !on {
		m = "files.favorites.remove"
	}
	a.SetFileStarred(id, on)
	if err := a.C.Call(ctx, m, url.Values{"file_id": {id}}, nil); err != nil {
		a.SetFileStarred(id, !on)
		return err
	}
	return nil
}

// FileInfo reads one file for the Files view (files.info).
func (a *App) FileInfo(ctx context.Context, id string) (File, error) {
	var r struct {
		File browseFileRaw `json:"file"`
	}
	if err := a.C.Call(ctx, "files.info", url.Values{"file": {id}, "count": {"0"}}, &r); err != nil {
		return File{}, err
	}
	return r.File.file(), nil
}

// RenameFile is EditFileDialog's Done: editFile's files.edit {file, title,
// is_canvas_template: false} (Slack always sends is_canvas_template).
func (a *App) RenameFile(ctx context.Context, id, title string) error {
	return a.C.Call(ctx, "files.edit", url.Values{"file": {id}, "title": {title}, "is_canvas_template": {"false"}}, nil)
}
