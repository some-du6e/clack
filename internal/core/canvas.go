package core

// Canvases (docs/CANVASES.md). Reading: files.info for the header and
// url_private for Quip's HTML export of the document, parsed by
// internal/canvas. Live updates: files.open registers the viewer, after
// which the realtime gateway sends document_update {file_id} on every
// saved edit (Slack's web client re-syncs then; Clack re-reads the export).
// Editing: the public canvases.* methods (canvases.edit with section ids
// from the export, canvases.create, conversations.canvases.create,
// canvases.access.set). Kept in its own file; realtime events arrive
// through SubscribeRaw, so events.go is untouched.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// CanvasInfo is a canvas's header: what files.info says about the file.
type CanvasInfo struct {
	ID, Title string
	// TitleSpans: the title as Slack's QuipTitleParsedId draws it, from
	// title_blocks, else (TitleFromText) the title string with its
	// :shortcodes: as emoji (FormatTextWithEmoji, 16px c-emoji__small);
	// nil for a plain title.
	TitleSpans    []richtext.Span
	TitleFromText bool
	Creator       string   // user id (canvas_creator_id, else user)
	Created       int64    // unix seconds
	Updated       int64    // last edit (edit_timestamp, else updated)
	Editable      bool     // the viewer may edit
	Access        string   // read | write | owner …
	Editors       []string // users who edited
	Permalink     string
	URL           string // url_private: the HTML export
	Channels      []string
	IsPublic      bool
	CommentsN     int
	ShowBadge     bool // never opened by the viewer (Slack's blue dot)
	TemplateMod   bool
	// QuipThreadID is the canvas's Quip document (files.info
	// quip_thread_id); Slack's QuipEmbed keys the empty canvas's
	// placeholder on it (getIndexFromFileId over doc.id).
	QuipThreadID string
	// AccessRequested: the viewer already asked for more access
	// (most_recent_access_request_ts; QuipAccessIndicator's "already asked")
	AccessRequested bool
	// Locked is files.info is_locked: Lock edits (x1Bi isLocked), which
	// also stops resolving comment threads (canArchiveComment).
	Locked bool
}

// CanvasFile is an embedded Slack file's display data (images, file cards).
type CanvasFile struct {
	ID, Name, Title, Mime, PrettyType string
	Thumb                             string // best thumbnail (files.slack.com, needs the cookie)
	ThumbW, ThumbH                    int
	Permalink                         string
	IsImage                           bool
}

// CanvasDoc is one fetched canvas.
type CanvasDoc struct {
	Info  CanvasInfo
	Doc   canvas.Doc
	Files map[string]CanvasFile // the files Doc embeds, by id (those files.info could read)
	At    time.Time             // when it was fetched
}

type canvasFileJSON struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Title           string          `json:"title"`
	Mimetype        string          `json:"mimetype"`
	Filetype        string          `json:"filetype"`
	PrettyType      string          `json:"pretty_type"`
	Mode            string          `json:"mode"`
	User            string          `json:"user"`
	CanvasCreatorID string          `json:"canvas_creator_id"`
	Created         int64           `json:"created"`
	Updated         int64           `json:"updated"`
	EditTimestamp   int64           `json:"edit_timestamp"`
	Editable        bool            `json:"editable"`
	Access          string          `json:"access"`
	Editors         []string        `json:"editors"`
	Permalink       string          `json:"permalink"`
	URLPrivate      string          `json:"url_private"`
	Channels        []string        `json:"channels"`
	Groups          []string        `json:"groups"`
	IMs             []string        `json:"ims"`
	IsPublic        bool            `json:"is_public"`
	CommentsCount   int             `json:"comments_count"`
	IsLocked        bool            `json:"is_locked"`
	TemplateMode    json.RawMessage `json:"canvas_template_mode"` // x1Bi isFileCanvasTemplate: !!canvas_template_mode
	ShowBadge       bool            `json:"show_badge"`
	TitleBlocks     json.RawMessage `json:"title_blocks"`
	Thumb360        string          `json:"thumb_360"`
	Thumb480        string          `json:"thumb_480"`
	Thumb720        string          `json:"thumb_720"`
	Thumb1024       string          `json:"thumb_1024"`
	Thumb720W       int             `json:"thumb_720_w"`
	Thumb720H       int             `json:"thumb_720_h"`
	Thumb1024W      int             `json:"thumb_1024_w"`
	Thumb1024H      int             `json:"thumb_1024_h"`
	Thumb480W       int             `json:"thumb_480_w"`
	Thumb480H       int             `json:"thumb_480_h"`
	OriginalW       int             `json:"original_w"`
	OriginalH       int             `json:"original_h"`
	AccessRequestTS json.RawMessage `json:"most_recent_access_request_ts"`
	QuipThreadID    string          `json:"quip_thread_id"`
}

// IsCanvasFile reports whether a file (filetype/mode/pretty_type) is a canvas.
func IsCanvasFile(filetype, mode, prettyType string) bool {
	return filetype == "quip" || filetype == "canvas" || mode == "quip" || strings.EqualFold(prettyType, "canvas")
}

// canvasState is per-App canvas bookkeeping (kept off App's struct).
type canvasState struct {
	mu       sync.Mutex
	open     map[string]int             // file id -> open views
	pending  map[string]bool            // a refetch is scheduled
	viewers  map[string]map[string]bool // file id -> users viewing it (files.open, file_opened/closed)
	watching bool
	infos    map[string]CanvasInfo // the last files.info of each canvas read (KnownCanvasInfo)
}

var canvasStates sync.Map // *App -> *canvasState

func (a *App) canvases() *canvasState {
	if v, ok := canvasStates.Load(a); ok {
		return v.(*canvasState)
	}
	v, _ := canvasStates.LoadOrStore(a, &canvasState{open: map[string]int{}, pending: map[string]bool{}, viewers: map[string]map[string]bool{},
		infos: map[string]CanvasInfo{}})
	return v.(*canvasState)
}

var errNotCanvas = errors.New("core: not a canvas")

// fileInfo reads files.info for id.
func (a *App) canvasFileInfo(ctx context.Context, id string) (*canvasFileJSON, error) {
	var r struct {
		File canvasFileJSON `json:"file"`
	}
	if err := a.C.Call(ctx, "files.info", url.Values{"file": {id}, "count": {"0"}}, &r); err != nil {
		return nil, err
	}
	return &r.File, nil
}

func (a *App) canvasInfo(f *canvasFileJSON) CanvasInfo {
	ci := CanvasInfo{ID: f.ID, Title: f.Title, Creator: firstNonEmpty(f.CanvasCreatorID, f.User), Created: f.Created,
		Updated: f.EditTimestamp, Editable: f.Editable, Access: f.Access, Editors: f.Editors, Permalink: f.Permalink,
		URL: f.URLPrivate, IsPublic: f.IsPublic, CommentsN: f.CommentsCount, ShowBadge: f.ShowBadge,
		QuipThreadID: f.QuipThreadID, Locked: f.IsLocked}
	switch strings.Trim(string(f.TemplateMode), `"`) {
	case "", "null", "0", "false":
	default:
		ci.TemplateMod = true
	}
	if ci.Updated == 0 {
		ci.Updated = f.Updated
	}
	if ci.Updated == 0 {
		ci.Updated = f.Created
	}
	switch t := strings.Trim(string(f.AccessRequestTS), `"`); t {
	case "", "null", "0", "false":
	default:
		ci.AccessRequested = true
	}
	ci.Channels = append(append(append([]string(nil), f.Channels...), f.Groups...), f.IMs...)
	if len(f.TitleBlocks) > 0 && string(f.TitleBlocks) != "null" {
		if d := richtext.FromMessage(a.S, &slack.Message{Text: f.Title, Blocks: f.TitleBlocks}); len(d.Blocks) > 0 {
			ci.TitleSpans = d.Blocks[0].Spans
		}
	}
	if ci.Title == "" {
		ci.Title = "Untitled"
	}
	if len(ci.TitleSpans) == 0 { // QuipTitleParsedId's string path: FormatTextWithEmoji
		ci.TitleSpans, ci.TitleFromText = richtext.EmojiText(a.S, ci.Title), true
	}
	a.rememberCanvasInfo(ci)
	return ci
}

// CanvasInfo reads a canvas's header only.
func (a *App) CanvasInfo(ctx context.Context, id string) (CanvasInfo, error) {
	f, err := a.canvasFileInfo(ctx, id)
	if err != nil {
		return CanvasInfo{}, err
	}
	if !IsCanvasFile(f.Filetype, f.Mode, f.PrettyType) {
		return CanvasInfo{}, errNotCanvas
	}
	return a.canvasInfo(f), nil
}

// Canvas fetches a canvas: files.info, then the HTML export (never cached:
// a canvas changes under the viewer), then files.info for embedded files.
func (a *App) Canvas(ctx context.Context, id string) (*CanvasDoc, error) {
	f, err := a.canvasFileInfo(ctx, id)
	if err != nil {
		return nil, err
	}
	if !IsCanvasFile(f.Filetype, f.Mode, f.PrettyType) {
		return nil, errNotCanvas
	}
	cd := &CanvasDoc{Info: a.canvasInfo(f), At: time.Now(), Files: map[string]CanvasFile{}}
	if f.URLPrivate == "" {
		return cd, nil
	}
	src, err := a.downloadCanvas(ctx, f.URLPrivate)
	if err != nil {
		return nil, err
	}
	doc, err := canvas.Parse(src)
	if err != nil {
		var pe *canvas.PanicError
		if errors.As(err, &pe) {
			log.Printf("core: canvas %s: %v (recovered)\n%s", id, pe, pe.Stack)
		}
		return nil, err
	}
	a.resolveCanvasSpans(&doc)
	cd.Doc = doc
	ids := doc.FileIDs()
	if len(ids) > 24 {
		ids = ids[:24]
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, fid := range ids {
		wg.Add(1)
		go func(fid string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ff, err := a.canvasFileInfo(ctx, fid)
			if err != nil {
				return
			}
			cf := CanvasFile{ID: ff.ID, Name: ff.Name, Title: ff.Title, Mime: ff.Mimetype, PrettyType: ff.PrettyType,
				Permalink: ff.Permalink, IsImage: strings.HasPrefix(ff.Mimetype, "image/")}
			switch {
			case ff.Thumb1024 != "":
				cf.Thumb, cf.ThumbW, cf.ThumbH = ff.Thumb1024, ff.Thumb1024W, ff.Thumb1024H
			case ff.Thumb720 != "":
				cf.Thumb, cf.ThumbW, cf.ThumbH = ff.Thumb720, ff.Thumb720W, ff.Thumb720H
			case ff.Thumb480 != "":
				cf.Thumb, cf.ThumbW, cf.ThumbH = ff.Thumb480, ff.Thumb480W, ff.Thumb480H
			case ff.Thumb360 != "":
				cf.Thumb = ff.Thumb360
			}
			if cf.ThumbW == 0 {
				cf.ThumbW, cf.ThumbH = ff.OriginalW, ff.OriginalH
			}
			mu.Lock()
			cd.Files[fid] = cf
			mu.Unlock()
		}(fid)
	}
	wg.Wait()
	return cd, nil
}

// downloadCanvas reads the export (slack.DownloadCanvasHTML: file URLs
// only, every redirect hop too, and the body must be Quip's export;
// loading Slack's pages with the session cookie can end the session).
func (a *App) downloadCanvas(ctx context.Context, raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || !a.C.CredentialsOK(raw) || !slack.FileURL(u) {
		return "", fmt.Errorf("core: canvas export URL refused: %s", slack.Redact(raw))
	}
	return a.C.DownloadCanvasHTML(ctx, raw, canvas.MaxHTML)
}

// resolveCanvasSpans gives emoji the workspace's own images (custom emoji,
// the standard set) instead of the export's slack-imgs proxy URLs, and
// mentions the ids the export leaves out (ResolveCanvasMention).
func (a *App) resolveCanvasSpans(d *canvas.Doc) {
	d.Walk(func(b *canvas.Block) bool {
		for i := range b.Spans {
			s := &b.Spans[i]
			if (s.Kind == richtext.User || s.Kind == richtext.Channel) && s.ID == "" {
				if id, ok := a.ResolveCanvasMention(s.Kind, s.Text); ok {
					s.ID = id
				}
			}
			if s.Kind == richtext.Emoji {
				if u, txt, ok := a.S.Emoji(s.ID); ok {
					if u != "" {
						s.URL = u
					}
					if txt != "" && s.Text == "" {
						s.Text = txt
					}
				}
			}
		}
		return true
	})
}

// ResolveCanvasMention finds the id behind a mention in a canvas export,
// which carries only the name Quip showed ("@name", "#channel"). Users are
// matched from the cache by display name, then username, then real name
// (case-insensitively); channels by name. A name is accepted only when
// exactly one user (channel) has it at the first tier that matches at all;
// an ambiguous name stays unresolved, so an edit can't turn it into a
// mention of the wrong person.
func (a *App) ResolveCanvasMention(kind richtext.SpanKind, text string) (string, bool) {
	name := strings.TrimSpace(text)
	switch kind {
	case richtext.User:
		name = strings.TrimPrefix(name, "@")
	case richtext.Channel:
		name = strings.TrimPrefix(name, "#")
	default:
		return "", false
	}
	if name == "" {
		return "", false
	}
	a.S.Lock()
	defer a.S.Unlock()
	if kind == richtext.Channel {
		id := ""
		for _, c := range a.S.Convs {
			if c == nil || c.IsIM || c.IsMPIM || !strings.EqualFold(c.Name, name) {
				continue
			}
			if id != "" && id != c.ID {
				return "", false
			}
			id = c.ID
		}
		return id, id != ""
	}
	tiers := []func(u *slack.User) string{
		func(u *slack.User) string { return u.Profile.DisplayName },
		func(u *slack.User) string { return u.Name },
		func(u *slack.User) string { return firstNonEmpty(u.Profile.RealName, u.RealName) },
	}
	for _, field := range tiers {
		id := ""
		n := 0
		for _, u := range a.S.Users {
			if u == nil || u.Deleted {
				continue
			}
			if f := strings.TrimSpace(field(u)); f != "" && strings.EqualFold(f, name) && u.ID != id {
				id = u.ID
				n++
			}
		}
		switch {
		case n == 1:
			return id, true
		case n > 1:
			return "", false // ambiguous: never guess
		}
	}
	return "", false
}

// OpenCanvas tells Slack the viewer has the canvas open (files.open), so the
// gateway sends its document_update events, and starts watching for them.
// Call CloseCanvas when the view goes away. Like Slack's web client this
// also clears the canvas's unread badge.
func (a *App) OpenCanvas(ctx context.Context, id string) error {
	st := a.canvases()
	st.mu.Lock()
	st.open[id]++
	first := st.open[id] == 1
	watch := !st.watching
	st.watching = true
	st.mu.Unlock()
	if watch {
		go a.watchCanvases()
	}
	if !first {
		return nil
	}
	var r struct {
		Viewers []struct {
			UserID string `json:"user_id"`
		} `json:"viewers"`
	}
	if err := a.C.Call(ctx, "files.open", url.Values{"file_id": {id}, "supports_read_only_check": {"true"},
		"last_updated": {strconv.FormatInt(time.Now().UnixMilli(), 10)}}, &r); err != nil {
		return err
	}
	vs := map[string]bool{}
	var ids []string
	for _, v := range r.Viewers {
		if v.UserID != "" {
			vs[v.UserID] = true
			ids = append(ids, v.UserID)
		}
	}
	st.mu.Lock()
	st.viewers[id] = vs
	st.mu.Unlock()
	a.EnsureUsers(ctx, ids)
	a.emit(Event{Type: "canvas_viewers", ID: id})
	return nil
}

// CanvasViewers lists the other people viewing an open canvas (Slack's
// facepile in the canvas header), in a stable order.
func (a *App) CanvasViewers(id string) []string {
	st := a.canvases()
	st.mu.Lock()
	defer st.mu.Unlock()
	self := a.S.SelfID()
	var out []string
	for u := range st.viewers[id] {
		if u != self {
			out = append(out, u)
		}
	}
	sort.Strings(out)
	return out
}

// canvasViewerEvent applies file_opened / file_closed {file: {id},
// viewer: {user_id}} to the viewer lists of open canvases.
func (a *App) canvasViewerEvent(ev slack.Event) {
	var e struct {
		File struct {
			ID string `json:"id"`
		} `json:"file"`
		Viewer struct {
			UserID string `json:"user_id"`
		} `json:"viewer"`
	}
	if json.Unmarshal(ev.Raw, &e) != nil || e.File.ID == "" || e.Viewer.UserID == "" || !a.CanvasOpen(e.File.ID) {
		return
	}
	st := a.canvases()
	st.mu.Lock()
	vs := st.viewers[e.File.ID]
	if vs == nil {
		vs = map[string]bool{}
		st.viewers[e.File.ID] = vs
	}
	if ev.Type == "file_opened" {
		vs[e.Viewer.UserID] = true
	} else {
		delete(vs, e.Viewer.UserID)
	}
	st.mu.Unlock()
	if ev.Type == "file_opened" {
		ctx, cancel := context.WithTimeout(a.bg, 15*time.Second)
		a.EnsureUsers(ctx, []string{e.Viewer.UserID})
		cancel()
	}
	a.emit(Event{Type: "canvas_viewers", ID: e.File.ID})
}

// CloseCanvas undoes OpenCanvas (files.close once the last view closes).
func (a *App) CloseCanvas(ctx context.Context, id string) error {
	st := a.canvases()
	st.mu.Lock()
	if st.open[id] > 0 {
		st.open[id]--
	}
	last := st.open[id] == 0
	if last {
		delete(st.open, id)
		delete(st.viewers, id)
	}
	st.mu.Unlock()
	if !last {
		return nil
	}
	return a.C.Call(ctx, "files.close", url.Values{"file_id": {id}}, nil)
}

// CanvasOpen reports whether some view has id open.
func (a *App) CanvasOpen(id string) bool {
	st := a.canvases()
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.open[id] > 0
}

// watchCanvases turns realtime events about open canvases into "canvas"
// events (ID = file id) for the views, which then refetch.
func (a *App) watchCanvases() {
	evs, cancel := a.SubscribeRaw()
	defer cancel()
	done := a.bg.Done()
	for {
		select {
		case <-done:
			return
		case ev, ok := <-evs:
			if !ok {
				return
			}
			if ev.Type == "file_opened" || ev.Type == "file_closed" {
				a.canvasViewerEvent(ev)
				continue
			}
			if id := canvasEventFile(ev); id != "" && a.CanvasOpen(id) {
				a.canvasChanged(id)
			}
		}
	}
}

// canvasEventFile returns the canvas a realtime event is about: Slack's
// document_update {file_id, payload} (an edit was saved), file_change /
// file_shared {file_id}, and the channel_canvas_updated message.
func canvasEventFile(ev slack.Event) string {
	var e struct {
		FileID string          `json:"file_id"`
		File   json.RawMessage `json:"file"`
		Files  []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	switch ev.Type {
	case "document_update", "file_change", "file_shared", "file_channel_canvas_reset":
	case "message":
		if ev.Subtype != "channel_canvas_updated" && ev.Subtype != "tabbed_canvas_updated" {
			return ""
		}
	default:
		return ""
	}
	_ = json.Unmarshal(ev.Raw, &e)
	if e.FileID != "" {
		return e.FileID
	}
	if len(e.File) > 0 {
		var f struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(e.File, &f) == nil && f.ID != "" {
			return f.ID
		}
		var s string
		if json.Unmarshal(e.File, &s) == nil {
			return s
		}
	}
	if len(e.Files) > 0 {
		return e.Files[0].ID
	}
	return ""
}

// canvasChanged coalesces bursts of edits (one event per saved
// transaction) into one "canvas" event per 400 ms.
func (a *App) canvasChanged(id string) {
	st := a.canvases()
	st.mu.Lock()
	if st.pending[id] {
		st.mu.Unlock()
		return
	}
	st.pending[id] = true
	st.mu.Unlock()
	time.AfterFunc(400*time.Millisecond, func() {
		st.mu.Lock()
		delete(st.pending, id)
		st.mu.Unlock()
		a.emit(Event{Type: "canvas", ID: id})
	})
}

// CanvasChange is one canvases.edit change.
type CanvasChange struct {
	Operation string // insert_after | insert_before | insert_at_start | insert_at_end | replace | delete | rename
	SectionID string // replace / delete / insert_before / insert_after
	Markdown  string // document_content (or title_content for rename)
}

func (c CanvasChange) json() map[string]any {
	m := map[string]any{"operation": c.Operation}
	if c.SectionID != "" {
		m["section_id"] = c.SectionID
	}
	switch c.Operation {
	case "delete":
	case "rename":
		m["title_content"] = map[string]string{"type": "markdown", "markdown": c.Markdown}
	default:
		m["document_content"] = map[string]string{"type": "markdown", "markdown": c.Markdown}
	}
	return m
}

// EditCanvas applies changes with canvases.edit. The realtime echo
// (document_update) makes open views refetch; callers update optimistically.
func (a *App) EditCanvas(ctx context.Context, id string, changes ...CanvasChange) error {
	if len(changes) == 0 {
		return nil
	}
	var cs []map[string]any
	for _, c := range changes {
		cs = append(cs, c.json())
	}
	b, _ := json.Marshal(cs)
	if err := a.C.Call(ctx, "canvases.edit", url.Values{"canvas_id": {id}, "changes": {string(b)}}, nil); err != nil {
		return err
	}
	if a.CanvasOpen(id) {
		a.canvasChanged(id) // without realtime there is no echo
	}
	return nil
}

// RequestFileAccess asks a file's owner for more access
// (files.requestPermission {file_id, reason, request_type}, Slack's
// useQuipAccess): requestType "upgrade" asks to edit, "comment_upgrade"
// to comment.
func (a *App) RequestFileAccess(ctx context.Context, id, reason, requestType string) error {
	return a.C.Call(ctx, "files.requestPermission", url.Values{"file_id": {id}, "reason": {reason}, "request_type": {requestType}}, nil)
}

// RenameCanvas sets a canvas's title.
func (a *App) RenameCanvas(ctx context.Context, id, title string) error {
	return a.EditCanvas(ctx, id, CanvasChange{Operation: "rename", Markdown: title})
}

// CreateCanvas makes a canvas: standalone (channel ""), or a channel canvas
// tab (conversations.canvases.create, which Slack's channel "+" uses). It
// returns the new file id.
func (a *App) CreateCanvas(ctx context.Context, channel, title, markdown string) (string, error) {
	p := url.Values{}
	if title != "" {
		p.Set("title", title)
	}
	if markdown != "" {
		b, _ := json.Marshal(map[string]string{"type": "markdown", "markdown": markdown})
		p.Set("document_content", string(b))
	}
	method := "canvases.create"
	if channel != "" {
		method = "conversations.canvases.create"
		p.Set("channel_id", channel)
	}
	var r struct {
		CanvasID string `json:"canvas_id"`
	}
	if err := a.C.Call(ctx, method, p, &r); err != nil {
		return "", err
	}
	if r.CanvasID == "" {
		return "", errors.New("core: create canvas: no canvas_id")
	}
	a.emit(Event{Type: "tabs", Channel: channel, Name: "canvas_created"})
	return r.CanvasID, nil
}

// ShareCanvas gives conversations access (canvases.access.set, level read
// or write) and, unless quiet, posts the canvas's link to each with an
// optional note, which Slack unfurls as the canvas card.
func (a *App) ShareCanvas(ctx context.Context, id string, channels []string, level, note string) error {
	if len(channels) == 0 {
		return nil
	}
	if level == "" {
		level = "read"
	}
	b, _ := json.Marshal(channels)
	if err := a.C.Call(ctx, "canvases.access.set", url.Values{"canvas_id": {id}, "access_level": {level}, "channel_ids": {string(b)}}, nil); err != nil {
		return err
	}
	info, err := a.CanvasInfo(ctx, id)
	if err != nil {
		return err
	}
	for _, ch := range channels {
		text := info.Permalink
		if note != "" {
			text = note + "\n" + info.Permalink
		}
		if _, err := a.Send(ctx, SendOpts{Channel: ch, Text: text}); err != nil {
			return err
		}
	}
	return nil
}

// CanvasMarkdown is block b as canvas markdown (internal/canvas.Markdown).
func CanvasMarkdown(b canvas.Block) string { return canvas.Markdown(b) }
