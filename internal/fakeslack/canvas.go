package fakeslack

// Canvases (docs/CANVASES.md): fixture canvases served like Slack serves
// them. files.info describes each quip file; its url_private (GET
// /files-pri/…) is Quip's HTML export; canvases.edit / canvases.create /
// conversations.canvases.create / canvases.access.set / canvases.delete
// change them and push document_update over the realtime socket. Every
// document is made by hand (kitchen.html covers each block type); nothing
// comes from a real workspace.

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/slack"
)

//go:embed canvases/kitchen.html
var kitchenHTML string

// Fixture canvas ids.
const (
	CanvasChannel = "F0CANVAS001" // C0FAKE00000's channel canvas: every block type
	CanvasShared  = "F0CANVAS002" // a standalone canvas shared in C0FAKE00000's history
	CanvasDM      = "F0CANVAS003" // a canvas tab in the first DM
	CanvasImage   = "F0CANVIMG01" // an image the kitchen sink embeds
	CanvasFile    = "F0CANVFILE1" // a PDF the kitchen sink embeds
)

// FakeCanvas is one canvas the fake holds.
type FakeCanvas struct {
	ID, Title, Channel string
	Doc                canvas.Doc
	Created, Updated   int64
	Creator            string
	Access             map[string]string // channel -> level (canvases.access.set)
	Opened             int               // files.open calls
}

type canvasStore struct {
	mu      sync.Mutex
	byID    map[string]*FakeCanvas
	order   []string
	next    int
	edits   []url.Values // canvases.edit calls, as sent
	sockets map[*websocket.Conn]bool
	shared  bool // the shared canvas's message is in the history
}

var canvasStores sync.Map // *Server -> *canvasStore

func (s *Server) canvasStore() *canvasStore {
	if v, ok := canvasStores.Load(s); ok {
		return v.(*canvasStore)
	}
	cs := &canvasStore{byID: map[string]*FakeCanvas{}, sockets: map[*websocket.Conn]bool{}}
	kd, _ := canvas.Parse(kitchenHTML)
	now := time.Now().Unix()
	add := func(c *FakeCanvas) {
		c.Created, c.Updated, c.Access = now-86400*3, now-3600, map[string]string{}
		if c.Creator == "" {
			c.Creator = "U0FAKE00001"
		}
		cs.byID[c.ID] = c
		cs.order = append(cs.order, c.ID)
	}
	add(&FakeCanvas{ID: CanvasChannel, Title: "Launch plan", Channel: "C0FAKE00000", Doc: kd})
	add(&FakeCanvas{ID: CanvasShared, Title: "Weekly sync notes", Doc: canvas.Doc{Blocks: canvas.ParseMarkdown(
		"# Weekly sync\n\nAgenda for this week, with :tada: and **bold** text.\n\n- [x] Ship the beta\n- [ ] Write the changelog\n  - [ ] Screenshots\n\n> Keep it short.", nil)}})
	dm := ""
	for _, c := range s.convs {
		if c.IsIM {
			dm = c.ID
			break
		}
	}
	add(&FakeCanvas{ID: CanvasDM, Title: "Our plans", Channel: dm, Creator: SelfID, Doc: canvas.Doc{Blocks: canvas.ParseMarkdown(
		"Things to do together:\n\n1. Pick a date\n2. Book tickets", nil)}})
	v, _ := canvasStores.LoadOrStore(s, cs)
	return v.(*canvasStore)
}

func (cs *canvasStore) secID() string {
	cs.next++
	return fmt.Sprintf("temp:C:FAK%06d", cs.next)
}

// Canvas returns a copy of a fixture canvas's state (tests).
func (s *Server) Canvas(id string) (FakeCanvas, bool) {
	cs := s.canvasStore()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	c, ok := cs.byID[id]
	if !ok {
		return FakeCanvas{}, false
	}
	return *c, true
}

// CanvasEdits returns the canvases.edit calls received (tests).
func (s *Server) CanvasEdits() []url.Values {
	cs := s.canvasStore()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return append([]url.Values(nil), cs.edits...)
}

// EditCanvasAsOther changes a canvas as another user would in Slack and
// pushes document_update to every socket (live-update tests, CI shots).
func (s *Server) EditCanvasAsOther(id, markdown string) {
	cs := s.canvasStore()
	cs.mu.Lock()
	if c, ok := cs.byID[id]; ok {
		c.Doc.Blocks = append(c.Doc.Blocks, canvas.ParseMarkdown(markdown, s.names())...)
		c.Updated = time.Now().Unix()
	}
	cs.mu.Unlock()
	s.pushDocumentUpdate(id)
}

// ViewCanvasAsOther pushes file_opened (open) or file_closed for user, as
// Slack does when someone else opens or leaves a canvas.
func (s *Server) ViewCanvasAsOther(id, user string, open bool) {
	typ := "file_closed"
	if open {
		typ = "file_opened"
	}
	s.Push(map[string]any{"type": typ, "file": map[string]string{"id": id}, "viewer": map[string]string{"user_id": user},
		"event_ts": fmt.Sprintf("%d.000200", time.Now().Unix())})
}

// Push sends a realtime event to every connected socket.
func (s *Server) Push(ev any) {
	b, _ := json.Marshal(ev)
	cs := s.canvasStore()
	cs.mu.Lock()
	var conns []*websocket.Conn
	for c := range cs.sockets {
		conns = append(conns, c)
	}
	cs.mu.Unlock()
	for _, c := range conns {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = c.Write(ctx, websocket.MessageText, b)
		cancel()
	}
}

func (s *Server) pushDocumentUpdate(id string) {
	s.Push(map[string]any{"type": "document_update", "file_id": id, "payload": "", "event_ts": fmt.Sprintf("%d.000100", time.Now().Unix())})
}

func (s *Server) addSocket(c *websocket.Conn) {
	cs := s.canvasStore()
	cs.mu.Lock()
	cs.sockets[c] = true
	cs.mu.Unlock()
}

func (s *Server) dropSocket(c *websocket.Conn) {
	cs := s.canvasStore()
	cs.mu.Lock()
	delete(cs.sockets, c)
	cs.mu.Unlock()
}

type fakeNames struct{ s *Server }

func (n fakeNames) UserName(id string) string {
	if u, ok := n.s.userByID(id); ok {
		return u.Name
	}
	return ""
}

func (n fakeNames) ChannelName(id string) string {
	for _, c := range n.s.convs {
		if c.ID == id {
			return c.Name
		}
	}
	return ""
}

func (s *Server) names() canvas.Names { return fakeNames{s} }

func (s *Server) canvasFile(c *FakeCanvas) map[string]any {
	f := map[string]any{"id": c.ID, "name": c.Title, "title": c.Title, "mimetype": "application/vnd.slack-docs",
		"filetype": "quip", "pretty_type": "Canvas", "mode": "quip", "user": c.Creator, "canvas_creator_id": c.Creator,
		"created": c.Created, "timestamp": c.Created, "updated": c.Updated, "edit_timestamp": c.Updated,
		"editable": true, "access": "owner", "editors": []string{c.Creator, SelfID},
		"url_private":          s.URL + "files-pri/" + TeamID + "-" + c.ID + "/canvas",
		"url_private_download": s.URL + "files-pri/" + TeamID + "-" + c.ID + "/download/canvas",
		"permalink":            "https://fake.slack.com/docs/" + TeamID + "/" + c.ID,
		"is_public":            true, "comments_count": 0, "show_badge": false, "size": len(c.Title),
		"title_blocks": []any{}, "quip_thread_id": "FAKE" + c.ID[len(c.ID)-4:],
		"channels": []string{}, "groups": []string{}, "ims": []string{}}
	if c.Channel != "" {
		if strings.HasPrefix(c.Channel, "D") {
			f["ims"] = []string{c.Channel}
		} else {
			f["channels"] = []string{c.Channel}
		}
	}
	return f
}

// handleCanvases answers the canvas methods; handled=false passes on.
func (s *Server) handleCanvases(method string, f url.Values) (any, bool) {
	switch method {
	case "files.info", "files.open", "files.close", "canvases.edit", "canvases.create", "conversations.canvases.create",
		"canvases.access.set", "canvases.delete", "canvases.sections.lookup", "conversations.info", "conversations.history",
		"search.modules.files":
	default:
		return nil, false
	}
	cs := s.canvasStore()
	switch method {
	case "conversations.info": // channels with a canvas tab
		ch := f.Get("channel")
		cs.mu.Lock()
		var tabs []map[string]any
		for _, id := range cs.order {
			if c := cs.byID[id]; c.Channel == ch {
				tabs = append(tabs, map[string]any{"id": "Ct" + c.ID, "type": "canvas", "label": "",
					"data": map[string]any{"file_id": c.ID, "shared_ts": "1762500000.000100"}})
			}
		}
		cs.mu.Unlock()
		if len(tabs) == 0 {
			return nil, false
		}
		tabs = append(tabs, map[string]any{"id": "Ctfiles", "type": "files", "label": ""})
		for _, c := range s.convs {
			if c.ID == ch {
				b, _ := json.Marshal(c)
				var m map[string]any
				_ = json.Unmarshal(b, &m)
				m["properties"] = map[string]any{"tabs": tabs, "canvas": map[string]any{"file_id": tabs[0]["data"].(map[string]any)["file_id"], "is_empty": false}}
				addCounts(m, c, f)
				return ok("channel", m), true
			}
		}
		return nil, false
	case "conversations.history": // the shared canvas's message, newest in C0FAKE00000
		if f.Get("channel") == "C0FAKE00000" {
			s.seedCanvasShare()
		}
		return nil, false
	case "search.modules.files":
		if !strings.Contains(f.Get("query"), "type:quip") && !strings.Contains(f.Get("query"), "type:lists type:quip") {
			return nil, false
		}
		cs.mu.Lock()
		var items []map[string]any
		for _, id := range cs.order {
			items = append(items, s.canvasFile(cs.byID[id]))
		}
		cs.mu.Unlock()
		return ok("items", items, "pagination", map[string]any{"total_count": len(items), "page": 1, "page_count": 1}), true
	case "files.info":
		id := f.Get("file")
		switch id {
		case CanvasImage:
			img := func(w, h int) string {
				return s.URL + "img/" + strconv.Itoa(w) + "x" + strconv.Itoa(h) + "/canvasimg.png"
			}
			return ok("file", map[string]any{"id": id, "name": "diagram.png", "title": "diagram.png", "mimetype": "image/png",
				"filetype": "png", "pretty_type": "PNG", "mode": "hosted", "user": "U0FAKE00001",
				"url_private": img(1280, 720), "thumb_1024": img(1024, 576), "thumb_1024_w": 1024, "thumb_1024_h": 576,
				"thumb_360": img(360, 203), "original_w": 1280, "original_h": 720,
				"permalink": "https://fake.slack.com/files/U0FAKE00001/" + id + "/diagram.png"}), true
		case CanvasFile:
			return ok("file", map[string]any{"id": id, "name": "report.pdf", "title": "Quarterly report", "mimetype": "application/pdf",
				"filetype": "pdf", "pretty_type": "PDF", "mode": "hosted", "user": "U0FAKE00002", "size": 248000,
				"url_private": s.URL + "files-pri/" + TeamID + "-" + id + "/report.pdf",
				"permalink":   "https://fake.slack.com/files/U0FAKE00002/" + id + "/report.pdf"}), true
		}
		if id == CodeHTMLFileID && s.Size.CodeChannels { // the code channel's html view (codechannels.go)
			return ok("file", map[string]any{"id": id, "name": "report.html", "title": "Login redirect report", "mimetype": "text/html",
				"filetype": "html", "pretty_type": "HTML", "mode": "hosted", "user": CodeAgentID, "size": 2048,
				"url_private":          s.URL + "files-pri/" + TeamID + "-" + id + "/report.html",
				"url_private_download": s.URL + "files-pri/" + TeamID + "-" + id + "/download/report.html",
				"permalink":            "https://fake.slack.com/files/" + CodeAgentID + "/" + id + "/report.html"}), true
		}
		cs.mu.Lock()
		defer cs.mu.Unlock()
		if c, okc := cs.byID[id]; okc {
			return ok("file", s.canvasFile(c), "comments", []any{}), true
		}
		return nil, false
	case "files.open", "files.close":
		cs.mu.Lock()
		defer cs.mu.Unlock()
		c, okc := cs.byID[f.Get("file_id")]
		if !okc {
			return map[string]any{"ok": false, "error": "file_not_found"}, true
		}
		if method == "files.open" {
			c.Opened++
			return ok("viewers", []map[string]any{{"user_id": SelfID, "is_focused": true}}, "should_subscribe_and_ping", true), true
		}
		return ok(), true
	case "canvases.create", "conversations.canvases.create":
		title := f.Get("title")
		var content struct {
			Markdown string `json:"markdown"`
		}
		_ = json.Unmarshal([]byte(f.Get("document_content")), &content)
		ch := f.Get("channel_id")
		if method == "conversations.canvases.create" && ch == "" {
			return map[string]any{"ok": false, "error": "invalid_arguments"}, true
		}
		cs.mu.Lock()
		cs.next++
		id := fmt.Sprintf("F0CANVNEW%02d", cs.next)
		now := time.Now().Unix()
		c := &FakeCanvas{ID: id, Title: title, Channel: ch, Creator: SelfID, Created: now, Updated: now, Access: map[string]string{},
			Doc: canvas.Doc{Blocks: canvas.ParseMarkdown(content.Markdown, s.names())}}
		if c.Title == "" {
			c.Title = "Untitled"
		}
		cs.byID[id] = c
		cs.order = append(cs.order, id)
		cs.mu.Unlock()
		return ok("canvas_id", id), true
	case "canvases.delete":
		cs.mu.Lock()
		defer cs.mu.Unlock()
		id := f.Get("canvas_id")
		if _, okc := cs.byID[id]; !okc {
			return map[string]any{"ok": false, "error": "canvas_not_found"}, true
		}
		delete(cs.byID, id)
		for i, x := range cs.order {
			if x == id {
				cs.order = append(cs.order[:i], cs.order[i+1:]...)
				break
			}
		}
		return ok(), true
	case "canvases.access.set":
		cs.mu.Lock()
		defer cs.mu.Unlock()
		c, okc := cs.byID[f.Get("canvas_id")]
		if !okc {
			return map[string]any{"ok": false, "error": "canvas_not_found"}, true
		}
		var chans []string
		_ = json.Unmarshal([]byte(f.Get("channel_ids")), &chans)
		lvl := f.Get("access_level")
		if lvl != "read" && lvl != "write" && lvl != "comment" && lvl != "owner" {
			return map[string]any{"ok": false, "error": "invalid_arguments"}, true
		}
		for _, ch := range chans {
			c.Access[ch] = lvl
		}
		return ok(), true
	case "canvases.sections.lookup":
		cs.mu.Lock()
		defer cs.mu.Unlock()
		c, okc := cs.byID[f.Get("canvas_id")]
		if !okc {
			return map[string]any{"ok": false, "error": "canvas_not_found"}, true
		}
		var crit struct {
			SectionTypes []string `json:"section_types"`
			ContainsText string   `json:"contains_text"`
		}
		_ = json.Unmarshal([]byte(f.Get("criteria")), &crit)
		var secs []map[string]string
		c.Doc.Walk(func(b *canvas.Block) bool {
			if b.ID == "" {
				return true
			}
			if crit.ContainsText != "" && !strings.Contains(b.Text(), crit.ContainsText) {
				return true
			}
			if len(crit.SectionTypes) > 0 {
				want := false
				for _, t := range crit.SectionTypes {
					switch t {
					case "h1":
						want = want || b.Kind == canvas.Heading1
					case "h2":
						want = want || b.Kind == canvas.Heading2
					case "h3":
						want = want || b.Kind == canvas.Heading3
					case "any_header":
						want = want || b.Kind == canvas.Heading1 || b.Kind == canvas.Heading2 || b.Kind == canvas.Heading3
					}
				}
				if !want {
					return true
				}
			}
			secs = append(secs, map[string]string{"id": b.ID})
			return true
		})
		return ok("sections", secs), true
	case "canvases.edit":
		id := f.Get("canvas_id")
		cs.mu.Lock()
		cs.edits = append(cs.edits, f)
		c, okc := cs.byID[id]
		if !okc {
			cs.mu.Unlock()
			return map[string]any{"ok": false, "error": "canvas_not_found"}, true
		}
		var changes []struct {
			Operation string `json:"operation"`
			SectionID string `json:"section_id"`
			Doc       struct {
				Markdown string `json:"markdown"`
			} `json:"document_content"`
			Title struct {
				Markdown string `json:"markdown"`
			} `json:"title_content"`
		}
		if err := json.Unmarshal([]byte(f.Get("changes")), &changes); err != nil || len(changes) == 0 {
			cs.mu.Unlock()
			return map[string]any{"ok": false, "error": "invalid_arguments"}, true
		}
		for _, ch := range changes {
			if errc := cs.apply(s, c, ch.Operation, ch.SectionID, ch.Doc.Markdown, ch.Title.Markdown); errc != "" {
				cs.mu.Unlock()
				return map[string]any{"ok": false, "error": errc}, true
			}
		}
		c.Updated = time.Now().Unix()
		cs.mu.Unlock()
		go s.pushDocumentUpdate(id)
		return ok(), true
	}
	return nil, false
}

// apply runs one canvases.edit change on c (cs.mu held). Sections are the
// export's ids at any depth (list items, table-cell paragraphs).
func (cs *canvasStore) apply(s *Server, c *FakeCanvas, op, sec, md, title string) string {
	if op == "rename" {
		if strings.TrimSpace(title) == "" {
			return "invalid_arguments"
		}
		c.Title = strings.TrimSpace(title)
		return ""
	}
	nb := s.embedUploads(canvas.ParseMarkdown(md, s.names()))
	switch op {
	case "insert_at_start":
		c.Doc.Blocks = append(nb, c.Doc.Blocks...)
		return ""
	case "insert_at_end":
		c.Doc.Blocks = append(c.Doc.Blocks, nb...)
		return ""
	case "replace", "delete", "insert_after", "insert_before":
	default:
		return "invalid_arguments"
	}
	if sec == "" {
		if op == "replace" { // the whole document
			c.Doc.Blocks = nb
			return ""
		}
		return "invalid_arguments"
	}
	if op == "replace" && len(nb) == 1 {
		nb[0].ID = sec // a one-block replace keeps its section
	}
	if !editIn(&c.Doc.Blocks, op, sec, nb) {
		return "canvas_editing_failed"
	}
	return ""
}

// embedUploads turns "![…](permalink)" of a file uploaded to this server
// into what Quip's export holds for it: an image section pointing at the
// file's blob (Clack resolves it with files.info), or an embedded file.
// Called with cs.mu held; takes draftMu for the uploads.
func (s *Server) embedUploads(bs []canvas.Block) []canvas.Block {
	for i := range bs {
		b := &bs[i]
		if b.Kind != canvas.Image || b.Image == nil {
			continue
		}
		u := b.Image.URL
		k := strings.Index(u, "/files/")
		if !strings.HasPrefix(u, s.URL) || k < 0 {
			continue
		}
		parts := strings.Split(u[k+len("/files/"):], "/") // U…/F…/name
		if len(parts) < 2 {
			continue
		}
		s.draftMu.Lock()
		up := s.files[parts[1]]
		var name string
		var w, h int
		if up != nil {
			name, w, h = up.Title, up.W, up.H
		}
		s.draftMu.Unlock()
		if up == nil {
			continue
		}
		if strings.HasPrefix(mimeOf(name), "image/") {
			*b = canvas.Block{Kind: canvas.Image, FileID: up.ID, Image: &canvas.Img{Alt: b.Image.Alt, W: w, H: h,
				URL: "/collab-slack-blob/FAKE/" + up.ID + "?size_name=original", FileID: up.ID}}
			continue
		}
		*b = canvas.Block{Kind: canvas.File, FileID: up.ID, Title: name}
	}
	return bs
}

// editIn finds section sec in bs (or nested containers) and edits there.
func editIn(bs *[]canvas.Block, op, sec string, nb []canvas.Block) bool {
	for i := range *bs {
		b := &(*bs)[i]
		if b.ID == sec {
			var repl []canvas.Block
			switch op {
			case "replace":
				repl = nb
			case "delete":
			case "insert_after":
				repl = append([]canvas.Block{*b}, nb...)
			case "insert_before":
				repl = append(append([]canvas.Block(nil), nb...), *b)
			}
			out := append([]canvas.Block(nil), (*bs)[:i]...)
			out = append(out, repl...)
			*bs = append(out, (*bs)[i+1:]...)
			return true
		}
		kids := nb
		if b.Kind == canvas.Quote { // a quote's paragraphs are Quip sections of style 16: "> …" lands as one
			kids = unquote(nb)
		}
		if editIn(&b.Children, op, sec, kids) {
			return true
		}
		for j := range b.Columns {
			if editIn(&b.Columns[j], op, sec, nb) {
				return true
			}
		}
		for r := range b.Rows {
			for k := range b.Rows[r] {
				if editIn(&b.Rows[r][k].Blocks, op, sec, nb) {
					return true
				}
			}
		}
	}
	return false
}

// unquote is quote blocks as their paragraphs (the first keeps the
// quote's section id).
func unquote(bs []canvas.Block) []canvas.Block {
	var out []canvas.Block
	for _, b := range bs {
		if b.Kind != canvas.Quote {
			out = append(out, b)
			continue
		}
		for i, c := range b.Children {
			if i == 0 && c.ID == "" {
				c.ID = b.ID
			}
			out = append(out, c)
		}
	}
	return out
}

// seedCanvasShare puts a message sharing CanvasShared at the top of
// C0FAKE00000's history (once).
func (s *Server) seedCanvasShare() {
	cs := s.canvasStore()
	cs.mu.Lock()
	if cs.shared {
		cs.mu.Unlock()
		return
	}
	cs.shared = true
	c := cs.byID[CanvasShared]
	file := s.canvasFile(c)
	cs.mu.Unlock()
	b, _ := json.Marshal(file)
	var sf slack.File
	_ = json.Unmarshal(b, &sf)
	ch := "C0FAKE00000"
	s.histMu.Lock()
	msgs, okc := s.hist[ch]
	if !okc {
		msgs = s.history(ch)
	}
	ts := "1762516900.000100"
	if len(msgs) > 0 {
		if f, err := strconv.ParseFloat(msgs[0].TS, 64); err == nil {
			ts = fmt.Sprintf("%.6f", f+60)
		}
	}
	m := slack.Message{Type: "message", User: "U0FAKE00001", TS: ts, Text: "Notes from today's sync", Files: []slack.File{sf}}
	s.hist[ch] = append([]slack.Message{m}, msgs...)
	s.histMu.Unlock()
}

// filesPri serves GET /files-pri/<team>-<file>/…: a canvas's HTML export
// (Quip's shape, regenerated from the current document), or a fixture
// PDF's bytes.
func (s *Server) filesPri(w http.ResponseWriter, r *http.Request) {
	s.count("files-pri")
	rest := strings.TrimPrefix(r.URL.Path, "/files-pri/")
	key, _, _ := strings.Cut(rest, "/")
	_, id, _ := strings.Cut(key, "-")
	if id == CanvasFile {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4\n% fake\n"))
		return
	}
	cs := s.canvasStore()
	cs.mu.Lock()
	c, okc := cs.byID[id]
	var page string
	if okc {
		page = canvas.HTML(c.Doc, cs.secID)
	}
	cs.mu.Unlock()
	if !okc {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(page))
}
