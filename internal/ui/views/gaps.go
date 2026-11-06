package views

// Stopgap reads that internal/core doesn't offer yet (files browser,
// huddle history). They call the Slack client core exposes (App.C) and
// keep results in memory only. Move these into core and delete this file.

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

// fileItem is one row of the Files browser.
type fileItem struct {
	ID, Name, Title         string
	Filetype, PrettyType    string
	Mime, User              string
	Thumb, URL, Permalink   string
	ThumbW, ThumbH          int
	Size, Created, Modified int64
	Channels                []string
	Mode                    string
}

func (f *fileItem) title() string {
	if f.Title != "" {
		return f.Title
	}
	return f.Name
}

func (f *fileItem) isImage() bool { return strings.HasPrefix(f.Mime, "image/") }
func (f *fileItem) isCanvas() bool {
	return f.Filetype == "quip" || f.Filetype == "canvas" || strings.EqualFold(f.PrettyType, "canvas")
}
func (f *fileItem) isList() bool {
	return f.Filetype == "list" || strings.EqualFold(f.PrettyType, "list")
}

type filePage struct {
	Files              []fileItem
	Page, Pages, Total int
}

type rawFile struct {
	ID         string   `json:"id"`
	Created    int64    `json:"created"`
	Timestamp  int64    `json:"timestamp"`
	Updated    int64    `json:"updated"`
	Name       string   `json:"name"`
	Title      string   `json:"title"`
	Mimetype   string   `json:"mimetype"`
	Filetype   string   `json:"filetype"`
	PrettyType string   `json:"pretty_type"`
	User       string   `json:"user"`
	Size       int64    `json:"size"`
	URLPrivate string   `json:"url_private"`
	Permalink  string   `json:"permalink"`
	Thumb360   string   `json:"thumb_360"`
	Thumb360W  int      `json:"thumb_360_w"`
	Thumb360H  int      `json:"thumb_360_h"`
	ThumbPDF   string   `json:"thumb_pdf"`
	Channels   []string `json:"channels"`
	Groups     []string `json:"groups"`
	IMs        []string `json:"ims"`
	Mode       string   `json:"mode"`
}

func (r *rawFile) item() fileItem {
	f := fileItem{ID: r.ID, Name: r.Name, Title: r.Title, Filetype: r.Filetype, PrettyType: r.PrettyType,
		Mime: r.Mimetype, User: r.User, Thumb: r.Thumb360, URL: r.URLPrivate, Permalink: r.Permalink,
		ThumbW: r.Thumb360W, ThumbH: r.Thumb360H, Size: r.Size, Created: r.Created, Modified: r.Updated, Mode: r.Mode}
	if f.Created == 0 {
		f.Created = r.Timestamp
	}
	if f.Thumb == "" {
		f.Thumb = r.ThumbPDF
	}
	f.Channels = append(append(append(f.Channels, r.Channels...), r.Groups...), r.IMs...)
	return f
}

// fileQuery selects a page of the Files browser.
type fileQuery struct {
	Kind  string // "" all, "canvases", "lists"
	Query string
	Mine  bool // created by the viewer
	Page  int
}

// serverTypes maps a Files sub-view to files.list `types`.
var serverTypes = map[string]string{"canvases": "canvas", "lists": "list"}

// listFiles pages the files the viewer can see, newest first. If the
// server rejects a sub-view's type, it falls back to all files (the view
// filters client-side).
func listFiles(ctx context.Context, a *core.App, q fileQuery) (filePage, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if strings.TrimSpace(q.Query) != "" {
		return searchFiles(ctx, a, q.Kind, q.Query, q.Page)
	}
	p := url.Values{"count": {"40"}, "page": {strconv.Itoa(q.Page)}, "show_files_hidden_by_limit": {"true"}}
	if t := serverTypes[q.Kind]; t != "" {
		p.Set("types", t)
	}
	if q.Mine {
		p.Set("user", a.Self().ID)
	}
	var res struct {
		Files  []rawFile `json:"files"`
		Paging struct {
			Page  int `json:"page"`
			Pages int `json:"pages"`
			Total int `json:"total"`
		} `json:"paging"`
	}
	err := a.C.Call(ctx, "files.list", p, &res)
	if err != nil && p.Get("types") != "" {
		p.Del("types")
		err = a.C.Call(ctx, "files.list", p, &res)
	}
	if err != nil {
		return filePage{}, err
	}
	out := filePage{Page: res.Paging.Page, Pages: res.Paging.Pages, Total: res.Paging.Total}
	ids := make([]string, 0, len(res.Files))
	for i := range res.Files {
		out.Files = append(out.Files, res.Files[i].item())
		ids = append(ids, res.Files[i].User)
	}
	a.EnsureUsers(ctx, ids)
	return out, nil
}

// deletedFiles lists recently deleted files (files.recentlyDeleted).
func deletedFiles(ctx context.Context, a *core.App) (filePage, error) {
	var res struct {
		Files []rawFile `json:"files"`
	}
	if err := a.C.Call(ctx, "files.recentlyDeleted", url.Values{}, &res); err != nil {
		return filePage{}, err
	}
	out := filePage{Page: 1, Pages: 1, Total: len(res.Files)}
	for i := range res.Files {
		out.Files = append(out.Files, res.Files[i].item())
	}
	return out, nil
}

func searchFiles(ctx context.Context, a *core.App, kind, query string, page int) (filePage, error) {
	q := query
	switch kind {
	case "canvases":
		q += " type:canvases"
	case "lists":
		q += " type:lists"
	}
	p := url.Values{"query": {q}, "count": {"40"}, "page": {strconv.Itoa(page)}, "sort": {"timestamp"}, "highlight": {"false"}}
	var res struct {
		Files struct {
			Matches []rawFile `json:"matches"`
			Paging  struct {
				Page  int `json:"page"`
				Pages int `json:"pages"`
				Total int `json:"total"`
			} `json:"paging"`
		} `json:"files"`
	}
	if err := a.C.Call(ctx, "search.files", p, &res); err != nil {
		return filePage{}, err
	}
	out := filePage{Page: res.Files.Paging.Page, Pages: res.Files.Paging.Pages, Total: res.Files.Paging.Total}
	ids := make([]string, 0, len(res.Files.Matches))
	for i := range res.Files.Matches {
		out.Files = append(out.Files, res.Files.Matches[i].item())
		ids = append(ids, res.Files.Matches[i].User)
	}
	a.EnsureUsers(ctx, ids)
	return out, nil
}

// huddleRoom is an active or past huddle.
type huddleRoom struct {
	ID, Name, CreatedBy string
	Channels            []string
	Participants        []string
	History             []string // everyone who joined
	Start, End          int64
	Active              bool
	DM                  bool
}

type rawRoom struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	CreatedBy          string   `json:"created_by"`
	DateStart          int64    `json:"date_start"`
	DateEnd            int64    `json:"date_end"`
	Participants       []string `json:"participants"`
	ParticipantHistory []string `json:"participant_history"`
	Channels           []string `json:"channels"`
	IsDMCall           bool     `json:"is_dm_call"`
	HasEnded           bool     `json:"has_ended"`
}

func (r *rawRoom) room() huddleRoom {
	return huddleRoom{ID: r.ID, Name: r.Name, CreatedBy: r.CreatedBy, Channels: r.Channels,
		Participants: r.Participants, History: r.ParticipantHistory, Start: r.DateStart, End: r.DateEnd,
		Active: r.DateEnd == 0 && !r.HasEnded && len(r.Participants) > 0, DM: r.IsDMCall}
}

// roomFromEvent decodes the room carried by an sh_room_* event.
func roomFromEvent(raw json.RawMessage) (huddleRoom, bool) {
	var ev struct {
		Room   *rawRoom `json:"room"`
		Huddle *rawRoom `json:"huddle"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return huddleRoom{}, false
	}
	r := ev.Room
	if r == nil {
		r = ev.Huddle
	}
	if r == nil || r.ID == "" {
		return huddleRoom{}, false
	}
	return r.room(), true
}

// roomFromMessage decodes the room of a huddle_thread message.
func roomFromMessage(raw json.RawMessage) (huddleRoom, bool) {
	if len(raw) == 0 {
		return huddleRoom{}, false
	}
	var r rawRoom
	if json.Unmarshal(raw, &r) != nil || r.ID == "" {
		return huddleRoom{}, false
	}
	return r.room(), true
}

// recentHuddles lists huddles the viewer was part of, newest first, from
// the huddle_thread messages search finds ("is:huddle" isn't a Slack
// modifier; huddle threads carry the subtype and a room).
func recentHuddles(ctx context.Context, a *core.App) ([]huddleRoom, error) {
	var res struct {
		Huddles []rawRoom `json:"huddles"`
		Rooms   []rawRoom `json:"rooms"`
	}
	err := a.C.Call(ctx, "huddles.history", url.Values{"count": {"30"}}, &res)
	if err != nil {
		return nil, err
	}
	rooms := append(res.Huddles, res.Rooms...)
	out := make([]huddleRoom, 0, len(rooms))
	var ids []string
	for i := range rooms {
		r := rooms[i].room()
		out = append(out, r)
		ids = append(ids, r.CreatedBy)
		ids = append(ids, r.History...)
	}
	a.EnsureUsers(ctx, ids)
	return out, nil
}
