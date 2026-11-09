package fakeslack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif" // DecodeConfig of uploads
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Drafts and file uploads, as Slack's web client uses them:
//
//   - files.getUploadURL {filename, length} → {file, upload_url}; the
//     bytes go to upload_url (this server's /api/upload/<id>) as a
//     multipart POST, then files.completeUpload {files: [{id, title}]}
//     finishes it (UploadQueue's eager upload). The file exists, unshared,
//     from then on.
//   - drafts.create {blocks, client_msg_id, destinations, file_ids,
//     attachments, is_from_composer} and drafts.update {draft_id, …,
//     client_last_updated_ts, date_scheduled} → {draft}; drafts.list
//     {is_active, limit} → {drafts}; drafts.delete {draft_id,
//     client_last_updated_ts, skip_file_deletion}. A draft answers its
//     file_ids and the file objects (files).
//   - files.share {files: "F1,F2", channel, thread_ts, broadcast, blocks,
//     comment, client_msg_id, resharing_aware, …} shares completed files
//     (shareFile, reason send_composer_message).
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

// UploadedFile is a file uploaded with files.getUploadURL.
type UploadedFile struct {
	ID, Name, Title string
	Size            int64
	Bytes           int  // bytes received at upload_url
	Cookie          bool // the upload POST carried a cookie (Slack's XHR sends none)
	Completed       bool // files.completeUpload finished it
	Processed       bool // ProcessDelay after completion: thumbs, file_created
	Shared          bool // files.share shared it
	Deleted         bool // a drafts.delete without skip_file_deletion removed it
	W, H            int  // an uploaded image's size (its bytes decode)
	data            []byte
}

// SharedFiles is one files.share call.
type SharedFiles struct {
	Channel, ThreadTS, Comment string
	IDs, Titles                []string
	Blocks                     json.RawMessage
	Broadcast                  string // as sent ("" when absent)
	ClientMsgID                string
	Args                       url.Values // everything files.share got
}

func (s *Server) newID(prefix string) string {
	s.nextID++
	return fmt.Sprintf("%s0FAKE%05d", prefix, s.nextID)
}

// object is Slack's file object for a draft's files; images get a
// thumb_360 this server draws (never a real files.slack.com URL). An
// uploaded image that decoded is served as its own bytes, at its size.
func (f *UploadedFile) object(base string) map[string]any {
	o := map[string]any{"id": f.ID, "name": f.Name, "title": f.Title, "size": f.Size, "mode": "hosted",
		"mimetype": mimeOf(f.Name), "filetype": strings.TrimPrefix(strings.ToLower(extOf(f.Name)), "."),
		"pretty_type": strings.ToUpper(strings.TrimPrefix(extOf(f.Name), ".")), "user": SelfID,
		"is_external": false, "is_public": false, "file_access": "visible",
		"url_private": base + "files-pri/" + f.ID + "/" + f.Name, "permalink": base + "files/" + SelfID + "/" + f.ID + "/" + f.Name}
	if strings.HasPrefix(mimeOf(f.Name), "image/") {
		o["thumb_360"] = base + "img/360x360/" + f.ID + ".png"
		if f.W > 0 && f.H > 0 {
			o["original_w"], o["original_h"] = f.W, f.H
			// Slack's thumbs: thumb_360, then each larger size while the
			// original is bigger than the one before (a 223px paste stops
			// at thumb_360; a 640px one gets thumb_720 at 640), all served
			// as the upload's own bytes
			prev := 0
			for _, side := range []int{360, 480, 720, 960, 1024} {
				if prev > 0 && max(f.W, f.H) <= prev {
					break
				}
				tw, th := f.W, f.H
				if tw > side || th > side {
					if tw >= th {
						tw, th = side, max(1, th*side/tw)
					} else {
						tw, th = max(1, tw*side/th), side
					}
				}
				k := "thumb_" + strconv.Itoa(side)
				o[k], o[k+"_w"], o[k+"_h"] = base+f.imgPath()[1:], tw, th
				prev = side
			}
		}
	}
	return o
}

// imgPath is where the server serves an uploaded image's bytes.
func (f *UploadedFile) imgPath() string { return fmt.Sprintf("/img/%dx%d/%s.png", f.W, f.H, f.ID) }

// stub is the file object Slack sends before processing is done
// (media_display_type "unknown", no mimetype or thumbs; see
// core/file_stub_test.go).
func (f *UploadedFile) stub(base string) map[string]any {
	return map[string]any{"id": f.ID, "name": f.Name, "title": f.Title, "mimetype": "", "filetype": "", "pretty_type": "",
		"size": 0, "mode": "hosted", "user": SelfID, "is_external": false, "is_public": false, "file_access": "visible",
		"media_display_type": "unknown", "url_private": base + "files-pri/" + f.ID + "/" + f.Name,
		"permalink": base + "files/" + SelfID + "/" + f.ID + "/" + f.Name}
}

// keepUploadBytes keeps an upload POST's file part (synthetic test bytes,
// memory only) so the file object can carry the image's size and the
// server can serve it.
func (s *Server) keepUploadBytes(id, ctype string, body []byte) {
	_, params, err := mime.ParseMediaType(ctype)
	if err != nil || params["boundary"] == "" {
		return
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		p, err := mr.NextPart()
		if err != nil {
			return
		}
		if p.FormName() != "file" {
			continue
		}
		data, _ := io.ReadAll(io.LimitReader(p, 64<<20))
		s.draftMu.Lock()
		up := s.files[id]
		if up != nil {
			up.data = data
			if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
				up.W, up.H = cfg.Width, cfg.Height
			}
		}
		s.draftMu.Unlock()
		if up != nil && up.W > 0 {
			s.imgMu.Lock()
			s.imgs[up.imgPath()] = data
			s.imgMu.Unlock()
		}
		return
	}
}

// process finishes an upload's processing after ProcessDelay and sends
// file_created (caller holds draftMu).
func (s *Server) processLater(up *UploadedFile) {
	d := s.ProcessDelay
	if d < 0 {
		return
	}
	if d == 0 {
		d = 30 * time.Millisecond
	}
	time.AfterFunc(d, func() {
		s.draftMu.Lock()
		up.Processed = true
		id := up.ID
		s.draftMu.Unlock()
		ev := map[string]any{"type": "file_created", "file_id": id, "file": map[string]any{"id": id}, "user_id": SelfID,
			"event_ts": fmt.Sprintf("%d.000100", time.Now().Unix())}
		s.emit(ev)
		if s.DuplicateEvents {
			s.emit(ev)
		}
	})
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

// uploadCookieKey carries "the request had a Cookie header" from the
// HTTP handler into handleDrafts for upload POSTs.
const uploadCookieKey = "\x00cookie"

func fail(code string) map[string]any { return map[string]any{"ok": false, "error": code} }

// handleDrafts answers the draft and upload methods (handled=false for
// anything else).
func (s *Server) handleDrafts(method string, f url.Values) (any, bool) {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	now := time.Now()
	switch {
	case method == "files.getUploadURL":
		n, _ := strconv.ParseInt(f.Get("length"), 10, 64)
		if f.Get("filename") == "" {
			return fail("invalid_arguments"), true
		}
		id := s.newID("F")
		s.files[id] = &UploadedFile{ID: id, Name: f.Get("filename"), Title: f.Get("filename"), Size: n}
		return ok("file", id, "upload_url", s.URL+"api/upload/"+id), true
	case strings.HasPrefix(method, "upload/"):
		if up := s.files[strings.TrimPrefix(method, "upload/")]; up != nil {
			up.Bytes = int(up.Size)
			up.Cookie = f.Get(uploadCookieKey) != ""
		}
		return []byte("OK - 1\n"), true
	case method == "files.completeUpload":
		var refs []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		if json.Unmarshal([]byte(f.Get("files")), &refs) != nil || len(refs) == 0 {
			return fail("invalid_arguments"), true
		}
		for _, r := range refs {
			up := s.files[r.ID]
			if up == nil || up.Deleted || up.Bytes == 0 {
				return fail("file_not_found"), true
			}
			up.Completed = true
			if r.Title != "" {
				up.Title = r.Title
			}
			s.processLater(up)
		}
		return ok(), true
	case method == "files.info":
		up := s.files[f.Get("file")]
		if up == nil || !up.Completed {
			return nil, false
		}
		if up.Deleted {
			return fail("file_deleted"), true
		}
		if !up.Processed {
			return ok("file", up.stub(s.URL), "comments", []any{}), true
		}
		return ok("file", up.object(s.URL), "comments", []any{}), true
	case method == "files.edit":
		up := s.files[f.Get("file")]
		if up == nil {
			return fail("file_not_found"), true
		}
		if t := f.Get("title"); t != "" {
			up.Title = t
		}
		return ok("file", up.object(s.URL)), true
	case method == "files.share":
		ids := strings.Split(f.Get("files"), ",")
		if f.Get("files") == "" || f.Get("channel") == "" {
			return fail("invalid_arguments"), true
		}
		args := url.Values{}
		for k, v := range f {
			if k != uploadCookieKey {
				args[k] = v
			}
		}
		sh := SharedFiles{Channel: f.Get("channel"), ThreadTS: f.Get("thread_ts"), Comment: f.Get("comment"),
			Broadcast: f.Get("broadcast"), ClientMsgID: f.Get("client_msg_id"), Args: args}
		if b := f.Get("blocks"); b != "" {
			sh.Blocks = json.RawMessage(b)
		}
		var objs []any
		for _, id := range ids {
			up := s.files[id]
			if up == nil || up.Deleted || !up.Completed {
				return fail("file_not_found"), true
			}
			up.Shared = true
			sh.IDs, sh.Titles = append(sh.IDs, id), append(sh.Titles, up.Title)
			if up.Processed {
				objs = append(objs, up.object(s.URL))
			} else {
				objs = append(objs, up.stub(s.URL))
			}
		}
		s.shared = append(s.shared, sh)
		s.postShare(sh, objs)
		return ok(), true
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

// postShare adds a files.share message to the conversation and sends its
// realtime echo, shaped like the one Slack sent for Clack's text-less
// send: {client_msg_id, display_as_bot: false, files, text: "", type,
// upload: false, user}, plus blocks when they were sent (caller holds
// draftMu).
func (s *Server) postShare(sh SharedFiles, files []any) {
	ts := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
	msg := map[string]any{"type": "message", "ts": ts, "user": SelfID, "text": sh.Comment, "files": files,
		"upload": false, "display_as_bot": false}
	if sh.ClientMsgID != "" {
		msg["client_msg_id"] = sh.ClientMsgID
	}
	if len(sh.Blocks) > 0 {
		msg["blocks"] = sh.Blocks
	}
	if sh.ThreadTS != "" {
		msg["thread_ts"] = sh.ThreadTS
	}
	b, _ := json.Marshal(msg)
	var m slack.Message
	_ = json.Unmarshal(b, &m)
	if sh.ThreadTS == "" {
		s.histMu.Lock()
		h, okh := s.hist[sh.Channel]
		if !okh {
			var gallery bool
			if h, gallery = s.galleryHistoryOf(sh.Channel); !gallery { // as historyPage builds it
				h = s.history(sh.Channel)
			}
		}
		s.hist[sh.Channel] = append([]slack.Message{m}, h...)
		s.histMu.Unlock()
	}
	ev := map[string]any{"channel": sh.Channel, "event_ts": ts}
	for k, v := range msg {
		ev[k] = v
	}
	dup := s.DuplicateEvents
	go func() {
		s.emit(ev)
		if dup {
			s.emit(ev)
		}
	}()
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

// Shared returns the files.share calls so far.
func (s *Server) Shared() []SharedFiles {
	s.draftMu.Lock()
	defer s.draftMu.Unlock()
	return append([]SharedFiles(nil), s.shared...)
}
