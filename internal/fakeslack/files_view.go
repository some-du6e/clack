package fakeslack

// The Files view's writes (ui/views/files.go): files.favorites.add /
// remove / list (starring), files.delete, and files.edit on files that
// weren't uploaded through files.getUploadURL (those are drafts.go's).
// Deleting a canvas or a list takes it out of canvas.go's / lists.go's
// store, so search.modules.files stops returning it. Every call is
// recorded (FilesViewCalls).

import (
	"net/url"
	"sync"
)

type filesViewStore struct {
	mu      sync.Mutex
	starred []string          // files.favorites.add order, oldest first (as Slack's file_ids)
	deleted map[string]bool   // files.delete
	titles  map[string]string // files.edit titles
	calls   []FilesViewCall
}

// FilesViewCall is one recorded Files view write.
type FilesViewCall struct {
	Method string
	Args   url.Values
}

var filesViewStores sync.Map // *Server -> *filesViewStore

func (s *Server) filesView() *filesViewStore {
	if v, ok := filesViewStores.Load(s); ok {
		return v.(*filesViewStore)
	}
	v, _ := filesViewStores.LoadOrStore(s, &filesViewStore{deleted: map[string]bool{}, titles: map[string]string{}})
	return v.(*filesViewStore)
}

// FilesViewCalls returns the recorded files.favorites.* / files.delete /
// files.edit calls, in order.
func (s *Server) FilesViewCalls() []FilesViewCall {
	fv := s.filesView()
	fv.mu.Lock()
	defer fv.mu.Unlock()
	return append([]FilesViewCall(nil), fv.calls...)
}

// StarredFiles returns the starred file ids, oldest first.
func (s *Server) StarredFiles() []string {
	fv := s.filesView()
	fv.mu.Lock()
	defer fv.mu.Unlock()
	return append([]string(nil), fv.starred...)
}

// FileDeleted reports whether files.delete removed id.
func (s *Server) FileDeleted(id string) bool {
	fv := s.filesView()
	fv.mu.Lock()
	defer fv.mu.Unlock()
	return fv.deleted[id]
}

// FileTitle returns the title files.edit last set on id ("" if none).
func (s *Server) FileTitle(id string) string {
	fv := s.filesView()
	fv.mu.Lock()
	defer fv.mu.Unlock()
	return fv.titles[id]
}

func (s *Server) uploaded(id string) bool {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	return s.files[id] != nil
}

// handleFilesView answers the Files view's writes; handled=false passes on.
func (s *Server) handleFilesView(method string, f url.Values) (any, bool) {
	switch method {
	case "files.favorites.add", "files.favorites.remove", "files.favorites.list":
	case "files.delete", "files.edit":
		if s.uploaded(f.Get("file")) {
			return nil, false
		}
	default:
		return nil, false
	}
	fv := s.filesView()
	fv.mu.Lock()
	defer fv.mu.Unlock()
	if method != "files.favorites.list" {
		fv.calls = append(fv.calls, FilesViewCall{Method: method, Args: f})
	}
	switch method {
	case "files.favorites.list":
		return ok("file_ids", append([]string{}, fv.starred...)), true
	case "files.favorites.add":
		id := f.Get("file_id")
		if id == "" {
			return fail("invalid_arguments"), true
		}
		for _, x := range fv.starred {
			if x == id {
				return fail("already_starred"), true
			}
		}
		fv.starred = append(fv.starred, id)
		return ok(), true
	case "files.favorites.remove":
		id := f.Get("file_id")
		for i, x := range fv.starred {
			if x == id {
				fv.starred = append(fv.starred[:i:i], fv.starred[i+1:]...)
				return ok(), true
			}
		}
		return fail("not_starred"), true
	case "files.delete":
		id := f.Get("file")
		if id == "" {
			return fail("invalid_arguments"), true
		}
		if fv.deleted[id] {
			return fail("file_deleted"), true
		}
		fv.deleted[id] = true
		cs := s.canvasStore()
		cs.mu.Lock()
		if _, found := cs.byID[id]; found {
			delete(cs.byID, id)
			cs.order = dropID(cs.order, id)
		}
		cs.mu.Unlock()
		ls := s.listStore()
		ls.mu.Lock()
		if _, found := ls.byID[id]; found {
			delete(ls.byID, id)
			ls.order = dropID(ls.order, id)
		}
		ls.mu.Unlock()
		return ok(), true
	case "files.edit":
		id, title := f.Get("file"), f.Get("title")
		if id == "" {
			return fail("invalid_arguments"), true
		}
		if title != "" {
			fv.titles[id] = title
		}
		return ok(), true
	}
	return nil, false
}

func dropID(ids []string, id string) []string {
	out := ids[:0:0]
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}
