package fakeslack

// The Files view's writes (ui/views/files.go): files.favorites.add /
// remove / list (starring), files.delete, and files.edit on files that
// weren't uploaded through files.getUploadURL (those are drafts.go's).
// Deleting a canvas or a list takes it out of canvas.go's / lists.go's
// store, so search.modules.files stops returning it, and keeps it for
// 24 hours: files.recentlyDeleted lists it ({id, title, date_delete,
// file_type: quip | list}, filtered by file_type) and files.undelete
// {file_id} puts it back. Every write is recorded (FilesViewCalls).

import (
	"net/url"
	"sort"
	"sync"
	"time"
)

type filesViewStore struct {
	mu      sync.Mutex
	starred []string          // files.favorites.add order, oldest first (as Slack's file_ids)
	deleted map[string]bool   // files.delete
	titles  map[string]string // files.edit titles
	trash   map[string]trashed
	calls   []FilesViewCall
	// file collections (files_sections.go): the custom ones, the starred
	// files' positions and the starred collection's sort
	colls             []*fakeCollection
	starPos           map[string]string
	starSort          string
	nextPos, nextColl int
}

// trashed is a deleted canvas or list (files.recentlyDeleted).
type trashed struct {
	canvas *FakeCanvas
	list   *FakeList
	at     int64
}

// TrashFile marks a canvas or list deleted at (tests: an old deletion).
func (s *Server) TrashFile(id string, at time.Time) {
	fv := s.filesView()
	fv.mu.Lock()
	defer fv.mu.Unlock()
	if t, ok := fv.trash[id]; ok {
		t.at = at.Unix()
		fv.trash[id] = t
	}
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
	v, _ := filesViewStores.LoadOrStore(s, &filesViewStore{deleted: map[string]bool{}, titles: map[string]string{}, trash: map[string]trashed{}})
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
	case "lists.getMyItems": // files_sections.go
		return s.handleMyItems(f), true
	case "files.favorites.add", "files.favorites.remove", "files.favorites.list", "files.recentlyDeleted", "files.undelete",
		"files.collections.list", "files.collections.create", "files.collections.update", "files.collections.delete":
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
	if out, handled := s.handleFileSections(fv, method, f); handled { // files_sections.go
		return out, true
	}
	if method != "files.favorites.list" && method != "files.recentlyDeleted" {
		fv.calls = append(fv.calls, FilesViewCall{Method: method, Args: f})
	}
	switch method {
	case "files.recentlyDeleted":
		type del struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			DateDelete int64  `json:"date_delete"`
			FileType   string `json:"file_type"`
		}
		out := []del{}
		cut := time.Now().Add(-24 * time.Hour).Unix()
		for id, t := range fv.trash {
			d := del{ID: id, DateDelete: t.at, FileType: "quip"}
			if t.canvas != nil {
				d.Title = t.canvas.Title
			} else {
				d.Title, d.FileType = t.list.Title, "list"
			}
			if t.at < cut || f.Get("file_type") != "" && f.Get("file_type") != d.FileType {
				continue
			}
			out = append(out, d)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		return ok("files", out), true
	case "files.undelete":
		id := f.Get("file_id")
		t, found := fv.trash[id]
		if !found || t.at < time.Now().Add(-24*time.Hour).Unix() {
			return fail("file_not_found"), true
		}
		delete(fv.trash, id)
		delete(fv.deleted, id)
		if t.canvas != nil {
			cs := s.canvasStore()
			cs.mu.Lock()
			cs.byID[id] = t.canvas
			cs.order = append(cs.order, id)
			cs.mu.Unlock()
		} else {
			ls := s.listStore()
			ls.mu.Lock()
			ls.byID[id] = t.list
			ls.order = append(ls.order, id)
			ls.mu.Unlock()
		}
		return ok(), true
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
		fv.starPosLocked(id)
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
		if c, found := cs.byID[id]; found {
			delete(cs.byID, id)
			cs.order = dropID(cs.order, id)
			fv.trash[id] = trashed{canvas: c, at: time.Now().Unix()}
		}
		cs.mu.Unlock()
		ls := s.listStore()
		ls.mu.Lock()
		if l, found := ls.byID[id]; found {
			delete(ls.byID, id)
			ls.order = dropID(ls.order, id)
			fv.trash[id] = trashed{list: l, at: time.Now().Unix()}
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
