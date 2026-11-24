package fakeslack

// Quip's canvas calls (docs/CANVASES.md "Slack's edit protocol"), as
// Slack's server would answer them, and strict where the real server's
// choices are unknown: every request has to look exactly like Slack's
// web client's, or the fake refuses it (docs/CANVAS-EDIT-PLAN.md
// "fakeslack"). Each request is logged decoded for tests (QuipRequests).

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/some-du6e/clack/internal/quip"
)

// QuipRequest is one canvas call the fake took, decoded.
type QuipRequest struct {
	Path   string     // load-data, fetch-latest or edit-document
	Fields []string   // the form's field names, in order
	Form   url.Values // the form's values
	Load   *quip.LoadRequest
	Fetch  *quip.FetchRequest
	Save   *quip.DocumentData
	Status int    // the fake's answer: 200, or the refusal's status
	Why    string // why it refused ("" for a 200)
}

// QuipRequests returns the canvas calls the fake took, in order.
func (s *Server) QuipRequests() []QuipRequest {
	cs := s.canvasStore()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return append([]QuipRequest(nil), cs.quipLog...)
}

// PreserializeQuipSections makes load-data send sections the way
// M455.updateResponseWithPreserialized reads them: as bytes in
// preserialized_items, not in objects.
func (s *Server) PreserializeQuipSections(on bool) {
	cs := s.canvasStore()
	cs.mu.Lock()
	cs.quipPreserialize = on
	cs.mu.Unlock()
}

// QuipThreadID is the canvas's quip_thread_id, as files.info gives it.
func (s *Server) QuipThreadID(id string) string {
	cs := s.canvasStore()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if c, ok := cs.byID[id]; ok {
		return quipThreadID(c)
	}
	return ""
}

// QuipSections returns copies of the canvas's live sections, in document
// order (its title first).
func (s *Server) QuipSections(id string) []quip.Section {
	cs := s.canvasStore()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	c, ok := cs.byID[id]
	if !ok {
		return nil
	}
	m := cs.quip(s, c)
	d, err := quip.ReadLoad(m.thread, m.loadAnswer(c, false))
	if err != nil {
		return nil
	}
	var out []quip.Section
	for _, sec := range d.Sections() {
		out = append(out, *sec)
	}
	return out
}

// quipCall serves /canvas/-/<path>.
func (s *Server) quipCall(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/canvas/-/")
	method := "canvas/" + path
	s.count(method)
	if s.Latency > 0 {
		time.Sleep(s.Latency)
	}
	fault, faulted := s.takeFault(method)
	if faulted && fault.Delay > 0 {
		time.Sleep(fault.Delay)
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 4*quip.MaxMessage))
	req := QuipRequest{Path: path}
	if faulted && !fault.AfterHandle && (fault.HangUp || fault.Status != 0 || fault.Error != "") {
		// the call is read and logged, never applied
		s.quipAnswer(r, path, string(body), &req, false)
		req.Status, req.Why = fault.Status, "a fault"
		cs := s.canvasStore()
		cs.mu.Lock()
		cs.quipLog = append(cs.quipLog, req)
		cs.mu.Unlock()
		s.applyFault(w, fault)
		return
	}
	status, answer, jsonErr := s.quipAnswer(r, path, string(body), &req, true)
	cs := s.canvasStore()
	cs.mu.Lock()
	cs.quipLog = append(cs.quipLog, req)
	cs.mu.Unlock()
	if faulted && fault.ReplyDelay > 0 {
		time.Sleep(fault.ReplyDelay)
	}
	if faulted && fault.AfterHandle && s.applyFault(w, fault) {
		return
	}
	switch {
	case jsonErr != "": // what Slack's API layer answers before Quip sees the call
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"ok":false,"error":%q}`, jsonErr)
	case status != http.StatusOK: // Quip's errors are bare statuses; the client reads no body
		w.WriteHeader(status)
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(answer)
	}
}

// canvasFormFields are each call's form, in Slack's order
// (getSavePayloadAndOptions for edit-document; title only with a title
// section, activity_log only for a save made of programmatic updates,
// which Clack never makes), the token last (applyCanvasAuth).
var canvasFormFields = map[string][]string{
	"load-data":     {"request_binary", "token"},
	"fetch-latest":  {"request_binary", "token"},
	"edit-document": {"thread", "document", "retry_count", "session", "sequence", "data_binary", "title", "token"},
}

var sessionRE = regexp.MustCompile(`^[0-9a-f]{12}$`)

// quipAnswer checks a call and answers it: the status, the answer's
// bytes, or the JSON error Slack's API layer would give instead.
func (s *Server) quipAnswer(r *http.Request, path, body string, req *QuipRequest, apply bool) (status int, answer []byte, jsonErr string) {
	bad := func(code int, why string, args ...any) (int, []byte, string) {
		req.Status, req.Why = code, fmt.Sprintf(why, args...)
		return code, nil, ""
	}
	want, known := canvasFormFields[path]
	switch {
	case !known:
		return bad(http.StatusNotFound, "no canvas call %q", path)
	case r.Method != http.MethodPost:
		return bad(http.StatusMethodNotAllowed, "%s, not POST", r.Method)
	case r.URL.RawQuery != "_x_version_ts=noversion":
		return bad(http.StatusBadRequest, "query %q, want _x_version_ts=noversion", r.URL.RawQuery)
	case !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded"):
		return bad(http.StatusBadRequest, "content type %q", r.Header.Get("Content-Type"))
	case r.Header.Get("Origin") != "" || r.Header.Get("Referer") != "":
		return bad(http.StatusBadRequest, "an Origin or Referer header (Clack's honest headers send neither)")
	case strings.Contains(body, "+"):
		return bad(http.StatusBadRequest, "a + in the body: encodeURIComponent writes %%2B and %%20")
	}
	if ck, err := r.Cookie("d"); err != nil || ck.Value == "" {
		req.Status, req.Why = http.StatusOK, "no d cookie"
		return http.StatusOK, nil, "invalid_auth"
	}
	req.Form = url.Values{}
	for _, pair := range strings.Split(body, "&") {
		name, value, _ := strings.Cut(pair, "=")
		n, err1 := url.QueryUnescape(name)
		v, err2 := url.QueryUnescape(value)
		if err1 != nil || err2 != nil || req.Form.Has(n) {
			return bad(http.StatusBadRequest, "form field %q can't be read or repeats", name)
		}
		req.Fields = append(req.Fields, n)
		req.Form.Set(n, v)
	}
	// the fields in Slack's order; title is the only optional one
	wi := 0
	for _, f := range req.Fields {
		for wi < len(want) && want[wi] != f && want[wi] == "title" {
			wi++
		}
		if wi >= len(want) || want[wi] != f {
			return bad(http.StatusBadRequest, "form fields %v, want %v in that order", req.Fields, want)
		}
		wi++
	}
	if wi < len(want) {
		return bad(http.StatusBadRequest, "form fields %v, want %v", req.Fields, want)
	}
	tok := OrgToken
	if s.OffGrid {
		tok = TeamToken
	}
	if req.Form.Get("token") != tok {
		req.Status, req.Why = http.StatusOK, "wrong token"
		return http.StatusOK, nil, "invalid_auth"
	}
	field := "request_binary"
	if path == "edit-document" {
		field = "data_binary"
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(req.Form.Get(field))
	if err != nil {
		return bad(http.StatusBadRequest, "%s isn't standard base64: %v", field, err)
	}
	var msg any
	switch path {
	case "load-data":
		req.Load = &quip.LoadRequest{}
		msg = req.Load
	case "fetch-latest":
		req.Fetch = &quip.FetchRequest{}
		msg = req.Fetch
	default:
		req.Save = &quip.DocumentData{}
		msg = req.Save
	}
	if err := quip.Unmarshal(raw, msg); err != nil {
		return bad(http.StatusBadRequest, "%s: %v", field, err)
	}
	// Slack's encoder writes one form of each message: the same bytes back,
	// so no unknown field, no field out of order, every number packed
	if again := quip.Marshal(msg); string(again) != string(raw) {
		return bad(http.StatusBadRequest, "%s isn't as Slack's encoder writes it (unknown fields, order or packing)", field)
	}
	if !apply {
		return http.StatusOK, nil, ""
	}
	cs := s.canvasStore()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	switch path {
	case "load-data":
		return cs.quipLoad(s, req)
	case "fetch-latest":
		return cs.quipFetch(s, req)
	}
	return cs.quipSave(s, req)
}

// quipCanvas is the canvas whose quip_thread_id is thread (cs.mu held).
func (cs *canvasStore) quipCanvas(thread string) *FakeCanvas {
	for _, id := range cs.order {
		if c := cs.byID[id]; quipThreadID(c) == thread {
			return c
		}
	}
	return nil
}

func (cs *canvasStore) quipLoad(s *Server, req *QuipRequest) (int, []byte, string) {
	l := req.Load
	thread := ""
	if len(l.ObjectIDs) == 1 {
		thread = l.ObjectIDs[0]
	}
	// srhK.reloadThreadBulkLoad's request, and nothing else
	if thread == "" || len(l.IndexIDs) > 0 || l.UserAction != nil || l.UserActionSequence != nil || len(l.BulkLoads) != 1 ||
		quip.FieldsString(&l.BulkLoads[0]) != "1,2" || *l.BulkLoads[0].Type != quip.BulkThread || *l.BulkLoads[0].Arg0 != thread {
		req.Status, req.Why = http.StatusBadRequest, "not load-data's request for one thread (object_ids [t], bulk_loads [{THREAD, t}])"
		return http.StatusBadRequest, nil, ""
	}
	c := cs.quipCanvas(thread)
	if c == nil {
		req.Status = http.StatusOK
		return http.StatusOK, quip.Marshal(&quip.LoadWrapper{Response: &quip.LoadResponse{Success: quip.Ptr(false),
			Failures: []quip.Failure{{Reason: quip.Ptr(quip.FailNotFound)}}}}), ""
	}
	m := cs.quip(s, c)
	req.Status = http.StatusOK
	return http.StatusOK, quip.Marshal(m.loadAnswer(c, cs.quipPreserialize)), ""
}

// thread and document are the model's thread and document objects.
func (m *quipModel) objects(c *FakeCanvas) (quip.Thread, quip.Document) {
	title := ""
	if c != nil {
		title = c.Title
	}
	return quip.Thread{ID: quip.Ptr(m.thread), Sequence: quip.Ptr(m.threadSeq), Title: quip.Ptr(title), DocumentID: quip.Ptr(m.doc)},
		quip.Document{ID: quip.Ptr(m.doc), Sequence: quip.Ptr(m.docSeq), ThreadID: quip.Ptr(m.thread), Title: quip.Ptr(title),
			MaxSectionSequence: quip.Ptr(m.maxSeq)}
}

// loadAnswer is load-data's answer: the thread, the document, the live
// sections (in objects, or preserialized) and the section index.
func (m *quipModel) loadAnswer(c *FakeCanvas, preserialize bool) *quip.LoadWrapper {
	th, doc := m.objects(c)
	p := &quip.Payload{Threads: []quip.Thread{th}, Documents: []quip.Document{doc}}
	ix := quip.Index{Name: quip.Ptr(quip.SectionIndex(m.doc))}
	var pre *quip.Preserialized
	for _, id := range m.ordered() {
		sec := m.secs[id]
		if sec.Deleted != nil && *sec.Deleted {
			continue
		}
		ix.Entries = append(ix.Entries, quip.IndexEntry{ObjectID: quip.Ptr(id), SortValue: quip.Ptr(pos(sec))})
		if preserialize {
			if pre == nil {
				pre = &quip.Preserialized{Items: []quip.PreserializedItem{{IDType: quip.Ptr(quip.IDTypeSection)}}}
			}
			pre.Items[0].Values = append(pre.Items[0].Values, quip.Raw(quip.Marshal(sec)))
			continue
		}
		p.Sections = append(p.Sections, *sec)
	}
	return &quip.LoadWrapper{Response: &quip.LoadResponse{Success: quip.Ptr(true), Objects: p, Indexes: []quip.Index{ix}}, Preserialized: pre}
}

// ordered is every section id by position, then id.
func (m *quipModel) ordered() []string {
	var ids []string
	for id := range m.secs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := pos(m.secs[ids[i]]), pos(m.secs[ids[j]])
		return a < b || a == b && ids[i] < ids[j]
	})
	return ids
}

func (cs *canvasStore) quipFetch(s *Server, req *QuipRequest) (int, []byte, string) {
	f := req.Fetch
	if len(f.Requests) != 1 || quip.FieldsString(&f.Requests[0]) != "1,2,3" {
		req.Status, req.Why = http.StatusBadRequest, "not fetch-latest's request for one thread (thread_id, client_thread_sequence, client_max_section_sequence)"
		return http.StatusBadRequest, nil, ""
	}
	t := f.Requests[0]
	c := cs.quipCanvas(*t.ThreadID)
	if c == nil {
		req.Status, req.Why = http.StatusNotFound, "no thread "+*t.ThreadID
		return http.StatusNotFound, nil, ""
	}
	m := cs.quip(s, c)
	ch := &quip.Changes{}
	var secs []quip.Section
	ix := quip.IndexChange{IndexID: quip.Ptr(quip.SectionIndex(m.doc))}
	for _, id := range m.ordered() {
		sec := m.secs[id]
		if *sec.Sequence <= *t.ClientMaxSectionSequence {
			continue
		}
		secs = append(secs, *sec)
		e := quip.ChangedEntry{ID: quip.Ptr(id)}
		if sec.Deleted != nil && *sec.Deleted {
			e.Remove = quip.Ptr(true)
		} else {
			e.SortValue = quip.Ptr(pos(sec))
		}
		ix.ChangedEntries = append(ix.ChangedEntries, e)
	}
	if len(secs) > 0 || m.threadSeq > *t.ClientThreadSequence {
		th, doc := m.objects(c)
		ch.Data = &quip.Payload{Threads: []quip.Thread{th}, Documents: []quip.Document{doc}, Sections: secs}
		if len(ix.ChangedEntries) > 0 {
			ch.Indexes = []quip.IndexChange{ix}
		}
	}
	req.Status = http.StatusOK
	return http.StatusOK, quip.Marshal(&quip.FetchResponse{Changes: ch}), ""
}

// quipSave is edit-document: it checks the bundle as strictly as the
// fake can, applies it, answers with the stored sections and pushes
// document_update.
func (cs *canvasStore) quipSave(s *Server, req *QuipRequest) (int, []byte, string) {
	bad := func(code int, why string, args ...any) (int, []byte, string) {
		req.Status, req.Why = code, fmt.Sprintf(why, args...)
		return code, nil, ""
	}
	f := req.Form
	c := cs.quipCanvas(f.Get("thread"))
	if c == nil {
		return bad(http.StatusNotFound, "no thread %q", f.Get("thread"))
	}
	if c.ViewerAccess == "read" || c.ViewerAccess == "comment" {
		return bad(http.StatusForbidden, "the viewer can't edit this canvas (%s)", c.ViewerAccess)
	}
	m := cs.quip(s, c)
	retry, err1 := strconv.Atoi(f.Get("retry_count"))
	seq, err2 := strconv.ParseInt(f.Get("sequence"), 10, 64)
	switch {
	case f.Get("document") != m.doc:
		return bad(http.StatusBadRequest, "document %q, want %q", f.Get("document"), m.doc)
	case err1 != nil || retry < 0:
		return bad(http.StatusBadRequest, "retry_count %q", f.Get("retry_count"))
	case !sessionRE.MatchString(f.Get("session")):
		return bad(http.StatusBadRequest, "session %q isn't 7yDq's 12 hex digits", f.Get("session"))
	case err2 != nil || seq < 0:
		return bad(http.StatusBadRequest, "sequence %q", f.Get("sequence"))
	}
	if last, ok := m.sessions[f.Get("session")]; ok && seq <= last {
		return bad(http.StatusConflict, "session sequence %d after %d (SEQUENCE_OUT_OF_ORDER)", seq, last)
	}
	d := req.Save
	if d.DocumentID != nil && *d.DocumentID != m.doc {
		return bad(http.StatusBadRequest, "document_id %q", *d.DocumentID)
	}
	if len(d.Sections) == 0 {
		return bad(http.StatusBadRequest, "no sections")
	}
	// a document with a title section (every canvas) sends the title with
	// every save: extractTitleMetadata's text, trimmed, at most 255
	if t := strings.TrimSpace(f.Get("title")); !f.Has("title") || t == "" || utf8.RuneCountInString(t) > 255 {
		return bad(http.StatusBadRequest, "title %q: every save sends the title section's text (at most 255 characters)", f.Get("title"))
	}
	inBundle := map[string]*quip.Section{}
	for i := range d.Sections {
		sec := &d.Sections[i]
		if sec.ID == nil || *sec.ID == "" {
			return bad(http.StatusBadRequest, "a section without an id")
		}
		if inBundle[*sec.ID] != nil {
			return bad(http.StatusBadRequest, "section %s twice", *sec.ID)
		}
		inBundle[*sec.ID] = sec
	}
	for i := range d.Sections {
		sec := &d.Sections[i]
		id := *sec.ID
		switch {
		case sec.ThreadID != nil, sec.DocumentID != nil, sec.CrdtSession != nil, sec.Status != nil, sec.Transient != nil,
			len(sec.Dirty) > 0, len(sec.Pending) > 0, sec.MergedState != nil, sec.TempID != nil:
			return bad(http.StatusBadRequest, "section %s sets a field only the server writes (fields %s)", id, quip.FieldsString(sec))
		case sec.Sequence == nil:
			return bad(http.StatusBadRequest, "section %s has no sequence", id)
		}
		old := m.secs[id]
		if old != nil && (old.Deleted == nil || !*old.Deleted) {
			if *sec.Sequence < *old.Sequence {
				return bad(http.StatusConflict, "section %s at sequence %d, the server's is %d", id, *sec.Sequence, *old.Sequence)
			}
			if *sec.Sequence > *old.Sequence {
				return bad(http.StatusBadRequest, "section %s at sequence %d, ahead of the server's %d", id, *sec.Sequence, *old.Sequence)
			}
		} else {
			switch {
			case old != nil:
				return bad(http.StatusBadRequest, "section %s was deleted", id)
			case !quip.IsNewSectionID(id, m.shard):
				return bad(http.StatusBadRequest, "unknown section %s (a new one is temp:C:%s and 25 hex digits)", id, m.shard)
			case *sec.Sequence != 0:
				return bad(http.StatusBadRequest, "new section %s at sequence %d, not 0", id, *sec.Sequence)
			case sec.Position == nil || !quip.IsTemp(*sec.Position):
				return bad(http.StatusBadRequest, "new section %s without a :temp position", id)
			}
		}
		typ, style := sec.Type, sec.Style
		if typ == nil && old != nil {
			typ = old.Type
		}
		if style == nil && old != nil {
			style = old.Style
		}
		if typ != nil && style != nil && (sec.SectionClass == nil || *sec.SectionClass != quip.ClassControl) {
			if t, ok := quip.TypeOfStyle(*style); !ok || t != *typ {
				return bad(http.StatusBadRequest, "section %s: type %d with style %d isn't a pair of nTyX.pW", id, *typ, *style)
			}
		}
		if rt := quip.SectionRTML(sec); rt != "" {
			if _, _, ok := quip.ParseRTML(rt); !ok {
				return bad(http.StatusBadRequest, "section %s: RTML Slack can't parse", id)
			}
			for _, cid := range quip.ControlIDs(rt) {
				live := m.secs[cid] != nil && (m.secs[cid].Deleted == nil || !*m.secs[cid].Deleted)
				if !live && inBundle[cid] == nil {
					return bad(http.StatusBadRequest, "section %s names control %s, which doesn't exist", id, cid)
				}
			}
		}
		if !quip.ValidUTF8(sec) {
			return bad(http.StatusBadRequest, "section %s: invalid UTF-8", id)
		}
	}
	// the title metadata every save sends: the title section's, after
	// this save (Slack's extractTitleMetadata, quip.TitleMetadata)
	titleRTML := ""
	var stored []quip.EmojiInfo
	for _, sec := range m.secs {
		if sec.Type != nil && *sec.Type == quip.TypeTitle && (sec.Deleted == nil || !*sec.Deleted) {
			titleRTML = quip.SectionRTML(sec)
			if sec.Content != nil && sec.Content.Title != nil {
				stored = sec.Content.Title.EmojiInfo
			}
			if b := inBundle[*sec.ID]; b != nil && b.Content != nil && b.Content.Title != nil {
				titleRTML = quip.SectionRTML(b)
			}
		}
	}
	// its controls as the document holds them after this save, the
	// emoji info Slack's last save left on it, channels as
	// conversations.info has them
	tc := quip.TitleControls{EmojiInfo: stored,
		Section: func(id string) *quip.Section {
			if b := inBundle[id]; b != nil {
				return b
			}
			return m.secs[id]
		},
		ChannelPrivate: func(id string) (bool, bool) {
			c, ok := s.conv(id)
			if !ok || c.IsIM || c.IsMPIM {
				return false, false
			}
			return c.IsPrivate, true
		}}
	if want, err := quip.TitleMetadata(titleRTML, tc); err == nil {
		switch {
		case f.Get("title") != want.Title:
			return bad(http.StatusBadRequest, "title %q, the title section gives %q", f.Get("title"), want.Title)
		case string(quip.Marshal(d.TitleRichText)) != string(quip.Marshal(want.RichText)):
			return bad(http.StatusBadRequest, "title_rich_text_api isn't the title section's")
		case !sameEmojiInfo(d.TitleSlackEmojiInfo, want.EmojiInfo):
			return bad(http.StatusBadRequest, "title_slack_emoji_info isn't the title's emoji's")
		}
	}
	// apply: each set field replaces the stored one; a new sequence each
	var out []quip.Section
	for i := range d.Sections {
		sec := &d.Sections[i]
		id := *sec.ID
		var cur quip.Section
		if old := m.secs[id]; old != nil {
			cur = *old
		} else {
			cur = quip.Section{ID: quip.Ptr(id), ThreadID: quip.Ptr(m.thread), DocumentID: quip.Ptr(m.doc)}
		}
		if sec.Deleted != nil {
			cur.Deleted = sec.Deleted
		}
		if sec.Position != nil {
			cur.Position = quip.Ptr(quip.StripTemp(*sec.Position))
		}
		if sec.PositionPath != nil {
			cur.PositionPath = quip.Ptr(strings.ReplaceAll(*sec.PositionPath, quip.TempSuffix, ""))
		}
		if sec.Type != nil {
			cur.Type = sec.Type
		}
		if sec.Style != nil {
			cur.Style = sec.Style
		}
		if sec.SectionClass != nil {
			cur.SectionClass = sec.SectionClass
		}
		if sec.Content != nil {
			cur.Content = sec.Content
		}
		if sec.Parents != nil {
			cur.Parents = sec.Parents
		}
		if sec.AltRTML != nil {
			cur.AltRTML = sec.AltRTML
		}
		if sec.Attributes != nil {
			cur.Attributes = sec.Attributes
		}
		if sec.HasLayoutParent != nil {
			cur.HasLayoutParent = sec.HasLayoutParent
		}
		cur.Sequence = quip.Ptr(m.nextSeq())
		m.secs[id] = &cur
		if _, ok := m.sig[id]; !ok {
			m.sig[id] = "saved" // a section only a save made: its source is the save
		}
		out = append(out, cur)
	}
	first := false
	if _, ok := m.sessions[f.Get("session")]; !ok {
		first = true
	}
	m.sessions[f.Get("session")] = seq
	m.threadSeq, m.docSeq = m.maxSeq, m.maxSeq
	m.writeBack(c)
	c.Updated, c.HTML = time.Now().Unix(), ""
	go s.pushDocumentUpdate(c.ID)
	req.Status = http.StatusOK
	return http.StatusOK, quip.Marshal(&quip.EditAnswer{Changed: quip.Ptr(true), FinalData: &quip.DocumentData{Sections: out},
		IsNewEditSession: quip.Ptr(first)}), ""
}

// sameEmojiInfo reports whether two emoji info lists encode the same.
func sameEmojiInfo(a, b []quip.EmojiInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if string(quip.Marshal(&a[i])) != string(quip.Marshal(&b[i])) {
			return false
		}
	}
	return true
}
