package fakeslack

// Files Slack doesn't show, per file id (Size.Files: #files-gallery's
// tab bar holds them through SetGalleryTabList):
//
//   - SetFileGone makes files.info {file} answer an error for one id, as
//     Slack's API refuses it: {ok: false, error: code} with data's fields
//     beside it (not_visible's and access_denied's filetype and title:
//     fetchFileById's make_not_visible reads them off the error's data,
//     the whole answer). Other ids answer as before; a method-wide
//     SetFault would refuse them all.
//   - SetCanUndelete is files.canUndelete {file_id}'s answer for one id
//     ({can_undelete, file: {id, title, date_delete}}); an id without one
//     answers {can_undelete: false}. Every call is recorded
//     (CanUndeleteCalls).
//   - files.requestPermission for one of those ids answers ok and is
//     recorded (GoneAccessRequests).
//   - PushFileDeleted / PushFileUndeleted send file_deleted /
//     file_undeleted {file_id, event_ts} and make files.info answer
//     file_deleted (or as before) from then on.
//
// A tab whose file is gone stays in the bar: the gallery's tabs are the
// fixture's list, never built from the live files.

import (
	"net/url"
	"strconv"
	"sync"
	"time"
)

type fileGone struct {
	mu    sync.Mutex
	errs  map[string]map[string]any // file id -> the error answer
	undel map[string]map[string]any // file id -> files.canUndelete's answer
	calls []url.Values
	asks  []url.Values // files.requestPermission for those ids
}

var fileGones = newPerServer() // *Server -> *fileGone

func (s *Server) fileGone() *fileGone {
	v, _ := fileGones.LoadOrStore(s, &fileGone{errs: map[string]map[string]any{}, undel: map[string]map[string]any{}})
	return v.(*fileGone)
}

// SetFileGone makes files.info for id answer code, with data's fields in
// the answer ("" code: answer as before).
func (s *Server) SetFileGone(id, code string, data map[string]any) {
	g := s.fileGone()
	g.mu.Lock()
	defer g.mu.Unlock()
	if code == "" {
		delete(g.errs, id)
		return
	}
	out := map[string]any{"ok": false, "error": code}
	for k, v := range data {
		out[k] = v
	}
	g.errs[id] = out
}

// SetFileInfoFile makes files.info for id answer file as it is (a
// tombstone's {id, mode: "tombstone"}, an is_deleted file).
func (s *Server) SetFileInfoFile(id string, file map[string]any) {
	g := s.fileGone()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.errs[id] = map[string]any{"ok": true, "file": file}
}

// SetCanUndelete is files.canUndelete's answer for id.
func (s *Server) SetCanUndelete(id string, can bool, title string, dateDelete int64) {
	g := s.fileGone()
	g.mu.Lock()
	defer g.mu.Unlock()
	g.undel[id] = map[string]any{"ok": true, "can_undelete": can,
		"file": map[string]any{"id": id, "title": title, "date_delete": dateDelete}}
}

// CanUndeleteCalls are the files.canUndelete calls so far.
func (s *Server) CanUndeleteCalls() []url.Values {
	g := s.fileGone()
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]url.Values(nil), g.calls...)
}

// GoneAccessRequests are the files.requestPermission calls for the ids
// SetFileGone refuses.
func (s *Server) GoneAccessRequests() []url.Values {
	g := s.fileGone()
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]url.Values(nil), g.asks...)
}

// PushFileDeleted is the file_deleted event for id; files.info answers
// file_deleted from then on.
func (s *Server) PushFileDeleted(id string) {
	s.SetFileGone(id, "file_deleted", nil)
	s.Push(map[string]any{"type": "file_deleted", "file_id": id, "event_ts": strconv.FormatInt(time.Now().Unix(), 10) + ".000100"})
}

// PushFileUndeleted is the file_undeleted event for id; files.info
// answers as before.
func (s *Server) PushFileUndeleted(id string) {
	s.SetFileGone(id, "", nil)
	s.Push(map[string]any{"type": "file_undeleted", "file_id": id, "event_ts": strconv.FormatInt(time.Now().Unix(), 10) + ".000200"})
}

// handleFileGone answers files.info and files.canUndelete for the ids
// above; handled=false passes on.
func (s *Server) handleFileGone(method string, f url.Values) (any, bool) {
	g := s.fileGone()
	g.mu.Lock()
	defer g.mu.Unlock()
	switch method {
	case "files.info":
		if out, found := g.errs[f.Get("file")]; found {
			c := map[string]any{}
			for k, v := range out {
				c[k] = v
			}
			return c, true
		}
	case "files.requestPermission": // FileRequestAccessButton on a file the viewer can't see
		if _, found := g.errs[f.Get("file_id")]; found {
			g.asks = append(g.asks, withoutToken(f))
			return ok(), true
		}
	case "files.canUndelete":
		g.calls = append(g.calls, withoutToken(f))
		if out, found := g.undel[f.Get("file_id")]; found {
			return out, true
		}
		return ok("can_undelete", false), true
	}
	return nil, false
}
