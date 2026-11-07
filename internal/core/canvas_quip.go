package core

// Native canvas editing over Quip's syncer protocol, as Slack's web client
// does it (recorded from the real client, shapes only):
//
//	POST <org>/canvas/-/load-data/editor/1  form: token, request_binary=b64(pb)
//	  request  {1: thread, 3: {1: 1, 2: thread}, 5: "editor", 6: 1}
//	  response {2: {2: {3: thread, 6: document, 7: section…}}}
//	  section  {1: id "temp:C:…", 2: version, 7: document, 8: order key,
//	            9: class, 10: style, 12: content, 13: list parent,
//	            16: {1: indent}, 21: tree order key}
//	  content  {1: {1: rtml}} text | {58: {1: text}} title | other kinds
//	POST <org>/canvas/-/edit-document
//	  form: thread, document, session, sequence, retry_count, title, token,
//	        data_binary=b64({1: section update})
//	  update {1: id, 2: version+1000, 12: content, 19: 1, 20: seq,
//	          33: "<session>-<n>"}; the response echoes the stored section.
//
// The browser also sends an Automerge merge blob (field 38); the server
// accepts updates without it and other open clients (the web client
// included) pick the change up live. RTML is the section's text as
// HTML-like markup: escaped text, inline tags (<b>, <i>, <a href>) and
// <control id="…"></control> placeholders for emoji, mentions and links.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ---- minimal protobuf codec ----

type pbField struct {
	num  int
	wire int
	v    uint64 // varint / fixed
	b    []byte // length-delimited
}

func pbVarint(b []byte, i int) (uint64, int, error) {
	var r uint64
	var s uint
	for {
		if i >= len(b) {
			return 0, i, errors.New("pb: truncated varint")
		}
		c := b[i]
		i++
		r |= uint64(c&0x7f) << s
		if c < 0x80 {
			return r, i, nil
		}
		s += 7
		if s > 63 {
			return 0, i, errors.New("pb: varint overflow")
		}
	}
}

// pbDecode splits a message into its fields (no schema).
func pbDecode(b []byte) ([]pbField, error) {
	var out []pbField
	for i := 0; i < len(b); {
		key, j, err := pbVarint(b, i)
		if err != nil {
			return nil, err
		}
		i = j
		f := pbField{num: int(key >> 3), wire: int(key & 7)}
		if f.num == 0 {
			return nil, errors.New("pb: field 0")
		}
		switch f.wire {
		case 0:
			f.v, i, err = pbVarint(b, i)
			if err != nil {
				return nil, err
			}
		case 1:
			if i+8 > len(b) {
				return nil, errors.New("pb: truncated fixed64")
			}
			i += 8
		case 5:
			if i+4 > len(b) {
				return nil, errors.New("pb: truncated fixed32")
			}
			i += 4
		case 2:
			l, j, err := pbVarint(b, i)
			if err != nil {
				return nil, err
			}
			i = j
			if l > uint64(len(b)-i) {
				return nil, errors.New("pb: truncated bytes")
			}
			f.b = b[i : i+int(l)]
			i += int(l)
		default:
			return nil, fmt.Errorf("pb: wire type %d", f.wire)
		}
		out = append(out, f)
	}
	return out, nil
}

func pbGet(fs []pbField, num int) (pbField, bool) {
	for _, f := range fs {
		if f.num == num {
			return f, true
		}
	}
	return pbField{}, false
}

func pbAll(fs []pbField, num int) []pbField {
	var out []pbField
	for _, f := range fs {
		if f.num == num {
			out = append(out, f)
		}
	}
	return out
}

func pbSub(fs []pbField, nums ...int) []pbField {
	for _, n := range nums {
		f, ok := pbGet(fs, n)
		if !ok || f.wire != 2 {
			return nil
		}
		sub, err := pbDecode(f.b)
		if err != nil {
			return nil
		}
		fs = sub
	}
	return fs
}

func pbStr(fs []pbField, num int) string {
	if f, ok := pbGet(fs, num); ok && f.wire == 2 {
		return string(f.b)
	}
	return ""
}

func pbInt(fs []pbField, num int) int64 {
	if f, ok := pbGet(fs, num); ok && f.wire == 0 {
		return int64(f.v)
	}
	return 0
}

type pbBuf struct{ b []byte }

func (p *pbBuf) varint(v uint64) {
	for v >= 0x80 {
		p.b = append(p.b, byte(v)|0x80)
		v >>= 7
	}
	p.b = append(p.b, byte(v))
}

func (p *pbBuf) Int(num int, v int64) *pbBuf {
	p.varint(uint64(num) << 3)
	p.varint(uint64(v))
	return p
}

func (p *pbBuf) Bytes(num int, b []byte) *pbBuf {
	p.varint(uint64(num)<<3 | 2)
	p.varint(uint64(len(b)))
	p.b = append(p.b, b...)
	return p
}

func (p *pbBuf) Str(num int, s string) *pbBuf { return p.Bytes(num, []byte(s)) }

func (p *pbBuf) Msg(num int, m *pbBuf) *pbBuf { return p.Bytes(num, m.b) }

func pb() *pbBuf { return &pbBuf{} }

// ---- quip document model ----

// Quip section classes (field 9) and styles (field 10) seen in canvases.
const (
	quipClassText  = 0
	quipClassList  = 1 // list container (style 5 bullets, 6 numbered, 7 checklist)
	quipClassTitle = 64
	quipClassQuote = 71 // blockquote container; its lines carry field 29
	quipStylePara  = 0
	quipStyleH1    = 1
	quipStyleH2    = 2
	quipStyleH3    = 3
	quipStyleCode  = 4
)

// quipSection is one section from load-data.
type quipSection struct {
	ID       string
	Version  int64
	Order    string // field 8
	Tree     string // field 21
	Class    int64
	Style    int64
	RTML     string // text sections
	Title    string // title section
	IsText   bool   // content is {1: {1: rtml}}
	IsTitle  bool   // content is {58: {1: text}}
	ListOf   string // field 13.1: the list container section
	ListKind int64  // field 13.4
	Indent   int64  // field 16.1
	InQuote  bool   // field 29
}

type quipDoc struct {
	Thread, Document string
	Sections         []quipSection
	byID             map[string]int
}

func parseQuipSection(b []byte) (quipSection, bool) {
	fs, err := pbDecode(b)
	if err != nil {
		return quipSection{}, false
	}
	s := quipSection{ID: pbStr(fs, 1), Version: pbInt(fs, 2), Order: pbStr(fs, 8), Tree: pbStr(fs, 21),
		Class: pbInt(fs, 9), Style: pbInt(fs, 10), InQuote: pbInt(fs, 29) == 1}
	if c := pbSub(fs, 12); c != nil {
		if t := pbSub(c, 1); t != nil {
			s.IsText, s.RTML = true, pbStr(t, 1)
		} else if _, ok := pbGet(c, 1); ok {
			s.IsText = true // empty text
		}
		if t := pbSub(c, 58); t != nil {
			s.IsTitle, s.Title = true, pbStr(t, 1)
		}
	}
	if l := pbSub(fs, 13); l != nil {
		s.ListOf, s.ListKind = pbStr(l, 1), pbInt(l, 4)
	}
	if st := pbSub(fs, 16); st != nil {
		s.Indent = pbInt(st, 1)
	}
	return s, s.ID != ""
}

func parseQuipLoad(b []byte) (*quipDoc, error) {
	top, err := pbDecode(b)
	if err != nil {
		return nil, err
	}
	inner := pbSub(top, 2, 2)
	if inner == nil {
		return nil, errors.New("core: canvas load: unexpected response")
	}
	d := &quipDoc{byID: map[string]int{}}
	if t := pbSub(inner, 3); t != nil {
		d.Thread = pbStr(t, 1)
	}
	if doc := pbSub(inner, 6); doc != nil {
		d.Document = pbStr(doc, 1)
	}
	for _, f := range pbAll(inner, 7) {
		if s, ok := parseQuipSection(f.b); ok {
			d.byID[s.ID] = len(d.Sections)
			d.Sections = append(d.Sections, s)
			if d.Document == "" {
				if fs, err := pbDecode(f.b); err == nil {
					d.Document = pbStr(fs, 7)
				}
			}
		}
	}
	if d.Document == "" {
		return nil, errors.New("core: canvas load: no document")
	}
	return d, nil
}

func (d *quipDoc) section(id string) (quipSection, bool) {
	if i, ok := d.byID[id]; ok {
		return d.Sections[i], true
	}
	return quipSection{}, false
}

// ---- transport ----

// quipPost POSTs to the org host's /canvas/-/<path> endpoint and returns
// the protobuf body.
func (a *App) quipPost(ctx context.Context, path string, form url.Values) ([]byte, error) {
	cr := a.C.Creds
	host, tok := cr.EnterpriseURL, cr.EnterpriseToken
	if host == "" || tok == "" {
		host, tok = cr.TeamURL, cr.Token
	}
	if host == "" {
		return nil, errors.New("core: canvas editing needs the workspace URL")
	}
	u := strings.TrimSuffix(host, "/") + "/canvas/-/" + path
	form.Set("token", tok)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "d="+cr.Cookie)
	req.Header.Set("Origin", "https://app.slack.com")
	req.Header.Set("User-Agent", canvasUA)
	resp, err := a.C.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxFetch))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("core: canvas %s: http %d", path, resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "json") || bytes.HasPrefix(b, []byte("{")) {
		return nil, fmt.Errorf("core: canvas %s: %s", path, jsonErr(b))
	}
	return b, nil
}

const canvasUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"

func jsonErr(b []byte) string {
	s := string(b)
	if i := strings.Index(s, `"error":"`); i >= 0 {
		s = s[i+9:]
		if j := strings.IndexByte(s, '"'); j >= 0 {
			return s[:j]
		}
	}
	return "unexpected response"
}

// quipThread resolves a canvas file id to its quip thread id.
func (a *App) quipThread(ctx context.Context, fileID string) (string, error) {
	var r struct {
		Lookup map[string]string `json:"lookup"`
	}
	if err := a.C.Call(ctx, "quip.lookupThreadIds", url.Values{"file_ids": {fileID}}, &r); err == nil {
		if t := r.Lookup[fileID]; t != "" {
			return t, nil
		}
	}
	m, err := a.CanvasMeta(ctx, fileID)
	if err != nil {
		return "", err
	}
	if m.ThreadID == "" {
		return "", errors.New("core: not an editable canvas")
	}
	return m.ThreadID, nil
}

func (a *App) quipLoad(ctx context.Context, thread string) (*quipDoc, error) {
	req := pb().Str(1, thread).Msg(3, pb().Int(1, 1).Str(2, thread)).Str(5, "editor").Int(6, 1)
	b, err := a.quipPost(ctx, "load-data/editor/1", url.Values{"request_binary": {base64.StdEncoding.EncodeToString(req.b)}})
	if err != nil {
		return nil, err
	}
	d, err := parseQuipLoad(b)
	if err != nil {
		return nil, err
	}
	if d.Thread == "" {
		d.Thread = thread
	}
	return d, nil
}

// canvasEditor is per-canvas edit state: the last loaded document and the
// session counters edit-document expects. Memory only.
type canvasEditor struct {
	mu      sync.Mutex
	thread  string
	doc     *quipDoc
	loaded  time.Time
	session string
	seq     int
}

var (
	canvasEditMu sync.Mutex
	canvasEdits  = map[*App]map[string]*canvasEditor{}
)

func (a *App) canvasEditorFor(fileID string) *canvasEditor {
	canvasEditMu.Lock()
	defer canvasEditMu.Unlock()
	m := canvasEdits[a]
	if m == nil {
		m = map[string]*canvasEditor{}
		canvasEdits[a] = m
	}
	e := m[fileID]
	if e == nil {
		var b [6]byte
		_, _ = rand.Read(b[:])
		e = &canvasEditor{session: hex.EncodeToString(b[:])}
		m[fileID] = e
	}
	return e
}

// ensure loads (or reloads when stale) the quip document. e.mu held.
func (e *canvasEditor) ensure(ctx context.Context, a *App, fileID string, fresh bool) error {
	if e.thread == "" {
		t, err := a.quipThread(ctx, fileID)
		if err != nil {
			return err
		}
		e.thread = t
	}
	if e.doc != nil && !fresh && time.Since(e.loaded) < 2*time.Second {
		return nil
	}
	d, err := a.quipLoad(ctx, e.thread)
	if err != nil {
		return err
	}
	e.doc, e.loaded = d, time.Now()
	return nil
}

// send posts one section update and stores the echoed section.
func (e *canvasEditor) send(ctx context.Context, a *App, title string, sec *pbBuf) error {
	e.seq++
	form := url.Values{
		"thread":      {e.thread},
		"document":    {e.doc.Document},
		"retry_count": {"0"},
		"session":     {e.session},
		"sequence":    {fmt.Sprint(e.seq)},
		"title":       {title},
		"data_binary": {base64.StdEncoding.EncodeToString(pb().Msg(1, sec).b)},
	}
	b, err := a.quipPost(ctx, "edit-document", form)
	if err != nil {
		return err
	}
	fs, err := pbDecode(b)
	if err != nil {
		return fmt.Errorf("core: canvas edit: %w", err)
	}
	if out := pbSub(fs, 2); out != nil {
		for _, f := range pbAll(out, 1) {
			if s, ok := parseQuipSection(f.b); ok {
				if i, ok := e.doc.byID[s.ID]; ok {
					e.doc.Sections[i] = s
				} else {
					e.doc.byID[s.ID] = len(e.doc.Sections)
					e.doc.Sections = append(e.doc.Sections, s)
				}
				return nil
			}
		}
	}
	if pbInt(fs, 1) != 1 {
		return errors.New("core: canvas edit was not accepted")
	}
	e.loaded = time.Time{} // unknown result: reload next time
	return nil
}

func (e *canvasEditor) title() string {
	for _, s := range e.doc.Sections {
		if s.IsTitle {
			return s.Title
		}
	}
	return ""
}

// CanvasSectionText returns a text section's current editable text (the
// RTML's text with controls shown as their display text) as stored on the
// server. controls comes from Canvas.Controls.
func (a *App) CanvasSectionText(ctx context.Context, fileID, sectionID string, controls map[string]string) (string, error) {
	e := a.canvasEditorFor(fileID)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensure(ctx, a, fileID, true); err != nil {
		return "", err
	}
	s, ok := e.doc.section(sectionID)
	if !ok {
		return "", errors.New("core: that part of the canvas changed; reload")
	}
	if s.IsTitle {
		return s.Title, nil
	}
	if !s.IsText {
		return "", errors.New("core: this block can't be edited here")
	}
	return rtmlPlain(s.RTML, controls), nil
}

// EditCanvasText replaces a text section's text. Inline formatting and
// controls (emoji, mentions, links) outside the changed range are kept.
// The title section (Canvas.TitleID) renames the canvas.
func (a *App) EditCanvasText(ctx context.Context, fileID, sectionID, text string, controls map[string]string) error {
	e := a.canvasEditorFor(fileID)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensure(ctx, a, fileID, false); err != nil {
		return err
	}
	s, ok := e.doc.section(sectionID)
	if !ok {
		if err := e.ensure(ctx, a, fileID, true); err != nil {
			return err
		}
		if s, ok = e.doc.section(sectionID); !ok {
			return errors.New("core: that part of the canvas was removed")
		}
	}
	title := e.title()
	var content *pbBuf
	switch {
	case s.IsTitle:
		text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
		content = pb().Msg(58, pb().Str(1, text))
		title = text
	case s.IsText:
		content = pb().Msg(1, pb().Str(1, rtmlEdit(s.RTML, text, controls, s.Style == quipStyleCode)))
	default:
		return errors.New("core: this block can't be edited here")
	}
	upd := pb().Str(1, s.ID).Int(2, s.Version+1000).Msg(12, content).Int(19, 1).Int(20, int64(e.seq+1)).
		Str(33, fmt.Sprintf("%s-%d", e.session, e.seq))
	return e.send(ctx, a, title, upd)
}

// CanvasNewBlock is the kind of block InsertCanvasBlock adds.
type CanvasNewBlock uint8

const (
	NewParagraph CanvasNewBlock = iota
	NewH1
	NewH2
	NewH3
	NewCode
)

// InsertCanvasBlock adds a text block after the section afterID ("" = at
// the end) and returns its section id.
func (a *App) InsertCanvasBlock(ctx context.Context, fileID, afterID string, kind CanvasNewBlock, text string) (string, error) {
	e := a.canvasEditorFor(fileID)
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.ensure(ctx, a, fileID, true); err != nil {
		return "", err
	}
	top := e.doc.topLevel()
	anchor := -1
	if afterID != "" {
		if s, ok := e.doc.section(afterID); ok {
			want := s.ID
			if s.ListOf != "" {
				want = s.ListOf // a list item: after its whole list
			}
			for i, t := range top {
				if t.ID == want {
					anchor = i
				}
			}
			// a quote: after its last line
			for anchor >= 0 && anchor+1 < len(top) && (top[anchor].InQuote || top[anchor].Class == quipClassQuote) &&
				top[anchor+1].InQuote {
				anchor++
			}
		}
	}
	if anchor < 0 {
		anchor = len(top) - 1
	}
	var prev, next string
	if anchor >= 0 {
		prev = top[anchor].Order
		if anchor+1 < len(top) {
			next = top[anchor+1].Order
		}
	}
	key := orderBetween(prev, next)
	style := int64(quipStylePara)
	switch kind {
	case NewH1:
		style = quipStyleH1
	case NewH2:
		style = quipStyleH2
	case NewH3:
		style = quipStyleH3
	case NewCode:
		style = quipStyleCode
	}
	id := e.newSectionID()
	rtml := rtmlEscape(text, kind == NewCode)
	upd := pb().Str(1, id).Int(2, 1000).Str(6, e.doc.Thread).Str(7, e.doc.Document).Str(8, key).
		Int(9, quipClassText).Int(10, style).Msg(12, pb().Msg(1, pb().Str(1, rtml))).
		Msg(16, pb().Int(4, 0)).Int(19, 1).Int(20, int64(e.seq+1)).Str(21, key).
		Str(33, fmt.Sprintf("%s-%d", e.session, e.seq))
	if err := e.send(ctx, a, e.title(), upd); err != nil {
		return "", err
	}
	return id, nil
}

// topLevel returns the document's top-level sections (not list items, quote
// lines, controls or the title) in order.
func (d *quipDoc) topLevel() []quipSection {
	var out []quipSection
	for _, s := range d.Sections {
		if s.Order == "" || s.ListOf != "" {
			continue
		}
		out = append(out, s)
	}
	sortSections(out)
	return out
}

func sortSections(s []quipSection) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Order < s[j-1].Order; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// newSectionID makes a section id like the editor's: "temp:C:" + the
// thread's 3-letter prefix + 25 hex digits.
func (e *canvasEditor) newSectionID() string {
	var b [13]byte
	_, _ = rand.Read(b[:])
	prefix := e.thread
	if len(prefix) > 3 {
		prefix = prefix[:3]
	}
	return "temp:C:" + prefix + hex.EncodeToString(b[:])[:25]
}

// orderBetween returns a key that sorts strictly between prev and next
// ("" = open end). Keys use the letters a–z, like quip's.
func orderBetween(prev, next string) string {
	if next == "" || next <= prev {
		return prev + "n"
	}
	if strings.HasPrefix(next, prev) {
		rest := next[len(prev):]
		if len(rest) > 1 {
			return next[:len(next)-1]
		}
		if rest[0] > 'a' {
			return prev + string(rune(rest[0]-1)) + "n"
		}
		return prev + "n" // no room left between them
	}
	i := 0
	for i < len(prev) && i < len(next) && prev[i] == next[i] {
		i++
	}
	if i < len(prev) && i < len(next) && next[i] > prev[i]+1 {
		return prev[:i] + string(rune(prev[i]+(next[i]-prev[i])/2))
	}
	return prev + "n"
}

// ---- RTML ----

// rtmlTok is one token of RTML: a rune of text, or a tag.
type rtmlTok struct {
	tag  string // non-empty for markup
	r    rune   // text rune (tag == "")
	disp []rune // controls: the text they display
}

func rtmlTokens(rtml string, controls map[string]string) []rtmlTok {
	var out []rtmlTok
	for i := 0; i < len(rtml); {
		c := rtml[i]
		switch c {
		case '<':
			j := strings.IndexByte(rtml[i:], '>')
			if j < 0 {
				out = append(out, rtmlTok{r: '<'})
				i++
				continue
			}
			tag := rtml[i : i+j+1]
			i += j + 1
			t := rtmlTok{tag: tag}
			low := strings.ToLower(tag)
			switch {
			case low == "<br>" || low == "<br/>" || low == "<br />":
				t = rtmlTok{tag: tag, disp: []rune{'\n'}}
			case strings.HasPrefix(low, "<control"):
				// swallow the closing tag into the control token
				if k := strings.Index(strings.ToLower(rtml[i:]), "</control>"); k >= 0 {
					t.tag += rtml[i : i+k+len("</control>")]
					i += k + len("</control>")
				}
				if id := tagAttr(tag, "id"); id != "" {
					t.disp = []rune(controls[id])
				}
			}
			out = append(out, t)
		case '&':
			j := strings.IndexByte(rtml[i:], ';')
			if j > 0 && j < 12 {
				ent := html.UnescapeString(rtml[i : i+j+1])
				for _, r := range ent {
					out = append(out, rtmlTok{r: r})
				}
				i += j + 1
				continue
			}
			out = append(out, rtmlTok{r: '&'})
			i++
		default:
			r, n := utf8.DecodeRuneInString(rtml[i:])
			out = append(out, rtmlTok{r: r})
			i += n
		}
	}
	return out
}

func isCloseTag(t rtmlTok) bool { return strings.HasPrefix(t.tag, "</") }

func tagAttr(tag, key string) string {
	i := strings.Index(tag, key+`="`)
	if i < 0 {
		return ""
	}
	s := tag[i+len(key)+2:]
	if j := strings.IndexByte(s, '"'); j >= 0 {
		return html.UnescapeString(s[:j])
	}
	return ""
}

// rtmlPlain is the editable text of an RTML string.
func rtmlPlain(rtml string, controls map[string]string) string {
	var b strings.Builder
	for _, t := range rtmlTokens(rtml, controls) {
		if t.tag == "" {
			b.WriteRune(t.r)
		} else {
			b.WriteString(string(t.disp))
		}
	}
	return b.String()
}

func rtmlEscape(s string, code bool) string {
	s = html.EscapeString(s)
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&#34;", "\"")
	return strings.ReplaceAll(s, "\n", "<br>")
}

// rtmlEdit applies an edit of the plain text to the RTML: the common prefix
// and suffix keep their tokens; the changed middle becomes plain text.
// Zero-width tags in the middle are kept (so markup stays balanced);
// controls in it are dropped.
func rtmlEdit(rtml, text string, controls map[string]string, code bool) string {
	toks := rtmlTokens(rtml, controls)
	// flatten to display runes with the owning token index
	var plain []rune
	var owner []int
	for i, t := range toks {
		if t.tag == "" {
			plain = append(plain, t.r)
			owner = append(owner, i)
			continue
		}
		for _, r := range t.disp {
			plain = append(plain, r)
			owner = append(owner, i)
		}
	}
	nw := []rune(text)
	p := 0
	for p < len(plain) && p < len(nw) && plain[p] == nw[p] {
		p++
	}
	s := 0
	for s < len(plain)-p && s < len(nw)-p && plain[len(plain)-1-s] == nw[len(nw)-1-s] {
		s++
	}
	// a multi-rune token (control) must be wholly in the prefix or suffix
	for p > 0 && p < len(plain) && owner[p-1] == owner[p] {
		p--
	}
	for s > 0 && len(plain)-s > 0 && owner[len(plain)-s] == owner[len(plain)-s-1] {
		s--
	}
	if p+s > len(nw) {
		s = len(nw) - p
	}
	// token ranges: [0,startTok) prefix, [startTok,endTok) replaced,
	// [endTok,…) suffix
	startTok := len(toks)
	if p < len(plain) {
		startTok = owner[p]
	}
	endTok := len(toks)
	if s > 0 {
		endTok = owner[len(plain)-s]
	}
	// typing right after a styled run continues it (Slack's editor): the
	// new text goes before the run's closing tags
	for startTok > 0 && isCloseTag(toks[startTok-1]) {
		startTok--
	}
	if endTok < startTok {
		endTok = startTok
	}
	var b strings.Builder
	writeTok := func(t rtmlTok) {
		if t.tag != "" {
			b.WriteString(t.tag)
			return
		}
		b.WriteString(rtmlEscape(string(t.r), code))
	}
	for i := 0; i < startTok && i < len(toks); i++ {
		writeTok(toks[i])
	}
	b.WriteString(rtmlEscape(string(nw[p:len(nw)-s]), code))
	for i := startTok; i < endTok && i < len(toks); i++ {
		if t := toks[i]; t.tag != "" && len(t.disp) == 0 {
			b.WriteString(t.tag) // zero-width markup stays balanced
		}
	}
	for i := endTok; i < len(toks); i++ {
		writeTok(toks[i])
	}
	return b.String()
}
