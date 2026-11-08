package fakeslack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Drafts and file uploads, as Slack's web client uses them:
//
//   - files.getUploadURLExternal {filename, length} → {upload_url, file_id};
//     the bytes go to upload_url (this server's /api/upload/<id>). The file
//     exists, unshared, from then on.
//   - drafts.create {blocks, client_msg_id, destinations, file_ids,
//     attachments, is_from_composer} and drafts.update {draft_id, …,
//     client_last_updated_ts, date_scheduled} → {draft}; drafts.list
//     {is_active, limit} → {drafts}; drafts.delete {draft_id,
//     client_last_updated_ts, skip_file_deletion}. A draft answers its
//     file_ids and the file objects (files).
//   - files.completeUploadExternal {files: [{id, title}], channel_id,
//     thread_ts, initial_comment} shares uploaded files.
//
// Everything stays in memory.

// Draft is a server draft as the fake keeps it.
type Draft struct {
	ID             string          `json:"id"`
	ClientMsgID    string          `json:"client_msg_id"`
	Blocks         json.RawMessage `json:"blocks"`
	Destinations   json.RawMessage `json:"destinations"`
	FileIDs        []string        `json:"file_ids"`
	DateCreated    int64           `json:"date_created"`
	DateScheduled  int64           `json:"date_scheduled"`
	LastUpdatedTS  string          `json:"last_updated_ts"`
	IsFromComposer bool            `json:"is_from_composer"`
	IsSent         bool            `json:"is_sent"`
	IsDeleted      bool            `json:"is_deleted"`
	// SkipFileDeletion is what drafts.delete was asked (deleted drafts only).
	SkipFileDeletion bool `json:"-"`
}

// UploadedFile is a file uploaded with files.getUploadURLExternal.
type UploadedFile struct {
	ID, Name, Title string
	Size            int64
	Bytes           int  // bytes received at upload_url
	Shared          bool // files.completeUploadExternal shared it
	Deleted         bool // a drafts.delete without skip_file_deletion removed it
}

// SharedFiles is one files.completeUploadExternal call.
type SharedFiles struct {
	Channel, ThreadTS, Comment string
	IDs, Titles                []string
}

func (s *Server) newID(prefix string) string {
	s.nextID++
	return fmt.Sprintf("%s0FAKE%05d", prefix, s.nextID)
}

// object is Slack's file object for a draft's files; images get a
// thumb_360 this server draws (never a real files.slack.com URL).
func (f *UploadedFile) object(base string) map[string]any {
	o := map[string]any{"id": f.ID, "name": f.Name, "title": f.Title, "size": f.Size, "mode": "hosted",
		"mimetype": mimeOf(f.Name), "filetype": strings.TrimPrefix(strings.ToLower(extOf(f.Name)), "."),
		"is_external": false, "is_public": false}
	if strings.HasPrefix(mimeOf(f.Name), "image/") {
		o["thumb_360"] = base + "img/360x360/" + f.ID + ".png"
	}
	return o
}

func extOf(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i:]
	}
	return ""
}

func mimeOf(name string) string {
	switch strings.ToLower(extOf(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".pdf":
		return "application/pdf"
	case ".txt":
		return "text/plain"
	}
	return "application/octet-stream"
}

func (s *Server) draftJSON(d *Draft) map[string]any {
	b, _ := json.Marshal(d)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	files := []any{}
	for _, id := range d.FileIDs {
		if f := s.files[id]; f != nil && !f.Deleted {
			files = append(files, f.object(s.URL))
		}
	}
	m["files"] = files
	m["user_id"], m["team_id"] = SelfID, TeamID
	return m
}

func fail(code string) map[string]any { return map[string]any{"ok": false, "error": code} }

// handleDrafts answers the draft and upload methods (handled=false for
// anything else).
func (s *Server) handleDrafts(method string, f url.Values) (any, bool) {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	now := time.Now()
	switch {
	case method == "files.getUploadURLExternal":
		n, _ := strconv.ParseInt(f.Get("length"), 10, 64)
		id := s.newID("F")
		s.files[id] = &UploadedFile{ID: id, Name: f.Get("filename"), Title: f.Get("filename"), Size: n}
		return ok("upload_url", s.URL+"api/upload/"+id, "file_id", id), true
	case strings.HasPrefix(method, "upload/"):
		if up := s.files[strings.TrimPrefix(method, "upload/")]; up != nil {
			up.Bytes = int(up.Size)
		}
		return []byte("OK - 1\n"), true
	case method == "files.completeUploadExternal":
		var refs []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		if json.Unmarshal([]byte(f.Get("files")), &refs) != nil || len(refs) == 0 {
			return fail("invalid_arguments"), true
		}
		sh := SharedFiles{Channel: f.Get("channel_id"), ThreadTS: f.Get("thread_ts"), Comment: f.Get("initial_comment")}
		var out []any
		for _, r := range refs {
			up := s.files[r.ID]
			if up == nil || up.Deleted {
				return fail("file_not_found"), true
			}
			up.Shared = true
			if r.Title != "" {
				up.Title = r.Title
			}
			sh.IDs, sh.Titles = append(sh.IDs, r.ID), append(sh.Titles, up.Title)
			out = append(out, map[string]any{"id": up.ID, "title": up.Title, "name": up.Name})
		}
		s.shared = append(s.shared, sh)
		return ok("files", out), true
	case method == "drafts.create":
		d := &Draft{ID: s.newID("Dr"), ClientMsgID: f.Get("client_msg_id"), DateCreated: now.Unix(),
			LastUpdatedTS: strconv.FormatInt(now.UnixMilli(), 10), IsFromComposer: f.Get("is_from_composer") == "true"}
		if err := s.fillDraft(d, f); err != "" {
			return fail(err), true
		}
		s.drafts[d.ID] = d
		return ok("draft", s.draftJSON(d)), true
	case method == "drafts.update":
		d := s.drafts[f.Get("draft_id")]
		if d == nil || d.IsDeleted || d.IsSent {
			return fail("draft_not_found"), true
		}
		if err := s.fillDraft(d, f); err != "" {
			return fail(err), true
		}
		if v := f.Get("date_scheduled"); v != "" {
			d.DateScheduled, _ = strconv.ParseInt(v, 10, 64)
		}
		d.LastUpdatedTS = strconv.FormatInt(now.UnixMilli(), 10)
		return ok("draft", s.draftJSON(d)), true
	case method == "drafts.delete":
		d := s.drafts[f.Get("draft_id")]
		if d == nil || d.IsDeleted {
			return fail("draft_not_found"), true
		}
		d.IsDeleted = true
		d.SkipFileDeletion = f.Get("skip_file_deletion") == "true"
		if !d.SkipFileDeletion {
			for _, id := range d.FileIDs {
				if up := s.files[id]; up != nil && !up.Shared {
					up.Deleted = true
				}
			}
		}
		return ok(), true
	case method == "drafts.list":
		var ds []*Draft
		for _, d := range s.drafts {
			if !d.IsDeleted && !d.IsSent {
				ds = append(ds, d)
			}
		}
		sort.Slice(ds, func(i, j int) bool { return ds[i].ID < ds[j].ID })
		out := make([]any, 0, len(ds))
		for _, d := range ds {
			out = append(out, s.draftJSON(d))
		}
		return ok("drafts", out, "has_more", false), true
	}
	return nil, false
}

// fillDraft sets a draft's content from a create/update form; it answers
// Slack's error code for file ids it doesn't know.
func (s *Server) fillDraft(d *Draft, f url.Values) string {
	if v := f.Get("blocks"); v != "" {
		d.Blocks = json.RawMessage(v)
	}
	if v := f.Get("destinations"); v != "" {
		d.Destinations = json.RawMessage(v)
	}
	if v := f.Get("file_ids"); v != "" {
		var ids []string
		if json.Unmarshal([]byte(v), &ids) != nil {
			return "invalid_file_ids"
		}
		for _, id := range ids {
			if up := s.files[id]; up == nil || up.Deleted {
				return "file_not_found"
			}
		}
		d.FileIDs = ids
	}
	if d.FileIDs == nil {
		d.FileIDs = []string{}
	}
	return ""
}

// Drafts returns the server's drafts (deleted ones too), by id.
func (s *Server) Drafts() []Draft {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	out := make([]Draft, 0, len(s.drafts))
	for _, d := range s.drafts {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// LiveDrafts returns the drafts not deleted or sent.
func (s *Server) LiveDrafts() []Draft {
	var out []Draft
	for _, d := range s.Drafts() {
		if !d.IsDeleted && !d.IsSent {
			out = append(out, d)
		}
	}
	return out
}

// File returns an uploaded file.
func (s *Server) File(id string) (UploadedFile, bool) {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	if f := s.files[id]; f != nil {
		return *f, true
	}
	return UploadedFile{}, false
}

// Shared returns the files.completeUploadExternal calls so far.
func (s *Server) Shared() []SharedFiles {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	return append([]SharedFiles(nil), s.shared...)
}
