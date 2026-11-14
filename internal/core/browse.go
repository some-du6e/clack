package core

// Browse views: the Files browser, Huddles, Drafts & sent → Sent, the
// Directories (channels, people, user groups), the channel tab bar,
// member counts and channel managers. Params mirror what Slack's web
// client sends (see the discovery notes); results live in memory only.

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// ---- types ----

// File is one row of the Files browser.
type File struct {
	ID, Name, Title         string
	Filetype, PrettyType    string
	Mime, User              string
	Thumb, URL, Permalink   string
	ThumbW, ThumbH          int
	Size, Created, Modified int64
	// Timestamp is the search item's timestamp (when it was shared), the
	// Files & links tab's sort key with Created as the fallback.
	Timestamp        int64
	Channels         []string // channels, groups and DMs it is shared in
	Mode             string   // hosted | external | quip | list | tombstone | …
	IsCanvas, IsList bool
	IsImage          bool
	// LastRead is when the viewer last opened it (last_read: milliseconds
	// or seconds, 0 never), for the collections' Last viewed sort.
	LastRead int64
}

type FilePage struct {
	Files              []File
	Page, Pages, Total int
}

// FilesOpts selects a page of the Files browser.
type FilesOpts struct {
	Kind         string // "" (all) | "canvases" | "lists"
	Query        string
	Mine         bool // "Created by you"
	SharedWithMe bool // "Shared with you"
	Page         int
	// Types are All files' Types menu picks as the browser's fileType
	// filter values ("lists", "quip", "documents", "pdfs", …; cX/T $Z),
	// sent as type: modifiers; nil is the default set.
	Types []string
	// Sort is the sort select's value: "" (Recently viewed:
	// last_engaged, or score with a query), "timestamp" (Last updated)
	// or "score" (Most relevant).
	Sort string
}

// Huddle is an active or past huddle.
type Huddle struct {
	ID, Name, CreatedBy string
	Channels            []string
	Participants        []string // in it now
	History             []string // everyone who joined
	Start, End          int64
	Active, DM          bool

	// The rest are the huddle object's fields the Huddles page reads
	// (huddles.history, huddles.get).
	HasEnded         bool
	ThreadTS         string   // thread_root_ts: the huddle message
	TranscriptFileID string   // transcript_file_id: the AI notes canvas
	RecordingType    string   // recording.recording_type
	FileIDs          []string // artifacts.file_ids
	Links            []HuddleLink
	AttachedFileIDs  []string // attached_file_ids
	// Missed is missed_participants' keys: the people who were invited
	// and didn't come.
	Missed       []string
	ChannelType  string         // channel_type: channel | private | im | mpim | file | other | …
	AccessByUser map[string]int // access_type_by_user
	BackgroundID string         // background_id
}

// HuddleLink is one of a huddle's artifacts.links.
type HuddleLink struct {
	URL, Title, IconURL string
}

// SentPage is a page of Drafts & sent → Sent.
type SentPage struct {
	Messages []Message // newest first
	Next     string    // pass back as the cursor (with Page+1)
	Page     int
	Total    int
	Convs    map[string]ConvRef
}

// ChannelBrowseOpts drives the channel directory.
type ChannelBrowseOpts struct {
	Query      string
	Page       int
	Sort       string // "" / "recommended" | "name" | "created" | "member_count"
	Desc       bool   // sort direction for name / created / member_count
	Type       string // "" (any, non-archived) | "public" | "private" | "archived" | "external"
	Membership string // "" (all) | "joined" | "other"
	// Team is the Workspaces filter (search_only_team), Orgs the
	// Organizations filter's external team ids (external_shared_team_ids).
	Team string
	Orgs []string
}

// PeopleBrowseOpts drives the people directory.
type PeopleBrowseOpts struct {
	Query       string
	Page        int
	Sort        string // "" / "recommended" | "name"
	Desc        bool   // name sort: Z to A
	AccountType string // "" | "owners" | "admins" | "members" | "guests" | "deactivated"
	// Team is the Workspaces filter (search_only_team); Titles and
	// Locations the Title and Location filters (profile_title,
	// profile_location).
	Team              string
	Titles, Locations []string
	// CustomFields filters by profile field: field id → values (Slack's
	// profileField.<id> browser filter, sent as custom_fields JSON).
	CustomFields map[string][]string
}

type PeoplePage struct {
	Users              []User
	Total, Page, Pages int
	// Titles and Locations are the response's filter_suggestions for the
	// Title and Location filters (Slack's searchSharedFacets).
	Titles, Locations []Facet
}

// Facet is a filter suggestion: a profile value and how many people have it.
type Facet struct {
	Value string
	Count int
}

// UsergroupOpts drives the user group directory.
type UsergroupOpts struct {
	Query string
	Sort  string // "name" (default) | "date_create" | "member_count"
	Desc  bool
	Count int // default 50
	// ExcludeDisabled leaves out disabled groups (Slack's directory shows them).
	ExcludeDisabled bool
	// Team is the Workspaces filter (search_only_team).
	Team string
}

// Tab is one entry of a channel's tab bar, in server order. The UI adds
// its own leading "Messages" tab.
type Tab struct {
	ID, Type     string // Type: canvas | list | folder | bookmarks | files | workflows | channel_canvas | …
	Label, Title string // Title: what to show (file title, "Bookmarks", "Deleted file", …)
	FileID       string // canvas / list tabs
	Deleted      bool   // the tab's file is gone
	Unread       bool   // a canvas the viewer never opened (no last_read): Slack's blue tab dot
	Bookmarks    []Bookmark
	// DefaultView is a list tab's data.lists_channel_tab_default_refine_view_id
	// (Slack's listsChannelTabDefaultRefineViewId), ViewName that view's
	// name in the list's list_metadata.views (getListViewById), "" when
	// the list has no such view.
	DefaultView, ViewName string
	// FolderBookmarkID is a folder tab's data.folder_bookmark_id: the
	// folder bookmark whose children it shows ("" the loose bookmarks).
	FolderBookmarkID string
	// AppID is a tab's data.app_id (an agent's own tab: the Channel
	// Expert's knowledge folder).
	AppID string
}

// MemberCounts is edge users/counts. Slack's channel header shows People.
type MemberCounts struct {
	People, Everyone, Bots, Apps, Guests int
}

// ---- loose JSON helpers ----

// looseString reads a JSON string or number as a string.
func looseString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	return string(raw)
}

// looseInt reads a JSON number or numeric string ("12", "12.5" → 12).
func looseInt(raw json.RawMessage) int64 {
	s := looseString(raw)
	if s == "" {
		return 0
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(f)
	}
	return 0
}

// looseText reads a string or a {value} object (purpose / topic).
func looseText(raw json.RawMessage) string {
	if len(raw) == 0 || raw[0] != '{' {
		return looseString(raw)
	}
	var v struct {
		Value string `json:"value"`
	}
	_ = json.Unmarshal(raw, &v)
	return v.Value
}

type searchPagination struct {
	Total      int    `json:"total_count"`
	Page       int    `json:"page"`
	PageCount  int    `json:"page_count"`
	NextCursor string `json:"next_cursor"`
}

// ---- files ----

const (
	fileTypesAll   = "type:lists type:quip type:documents type:spreadsheets type:presentations type:pdfs type:folder_skill_set"
	fileTypesOwned = "type:lists type:quip type:documents" // "Created by you" / "Shared with you"
)

// stripHighlight drops search highlight markers (U+E000/U+E001).
var stripHighlight = strings.NewReplacer("", "", "", "")

type browseFileRaw struct {
	ID         string          `json:"id"`
	Created    int64           `json:"created"`
	Timestamp  int64           `json:"timestamp"`
	Updated    int64           `json:"updated"`
	LastRead   json.RawMessage `json:"last_read"` // ms number; absent/null/0 = never opened by the viewer
	Name       string          `json:"name"`
	Title      string          `json:"title"`
	Mimetype   string          `json:"mimetype"`
	Filetype   string          `json:"filetype"`
	PrettyType string          `json:"pretty_type"`
	User       string          `json:"user"`
	Size       int64           `json:"size"`
	URLPrivate string          `json:"url_private"`
	Permalink  string          `json:"permalink"`
	Thumb360   string          `json:"thumb_360"`
	Thumb360W  int             `json:"thumb_360_w"`
	Thumb360H  int             `json:"thumb_360_h"`
	ThumbPDF   string          `json:"thumb_pdf"`
	ThumbVideo string          `json:"thumb_video"`
	Channels   []string        `json:"channels"`
	Groups     []string        `json:"groups"`
	IMs        []string        `json:"ims"`
	Mode       string          `json:"mode"`
	IsDeleted  bool            `json:"is_deleted"`
	FileAccess string          `json:"file_access"`
}

func (r *browseFileRaw) file() File {
	f := File{ID: r.ID, Name: stripHighlight.Replace(r.Name), Title: stripHighlight.Replace(r.Title),
		Filetype: r.Filetype, PrettyType: r.PrettyType, Mime: r.Mimetype, User: r.User,
		Thumb: firstNonEmpty(r.Thumb360, r.ThumbPDF, r.ThumbVideo), URL: r.URLPrivate, Permalink: r.Permalink,
		ThumbW: r.Thumb360W, ThumbH: r.Thumb360H, Size: r.Size, Created: r.Created, Modified: r.Updated, Mode: r.Mode,
		Timestamp: r.Timestamp}
	if f.Created == 0 {
		f.Created = r.Timestamp
	}
	if f.Modified == 0 {
		f.Modified = f.Created
	}
	f.Channels = append(append(append(f.Channels, r.Channels...), r.Groups...), r.IMs...)
	f.IsCanvas = r.Filetype == "quip" || r.Filetype == "canvas" || r.Mode == "quip" || strings.EqualFold(r.PrettyType, "canvas")
	f.IsList = !f.IsCanvas && (r.Filetype == "list" || r.Mode == "list" || strings.EqualFold(r.PrettyType, "list"))
	f.IsImage = strings.HasPrefix(r.Mimetype, "image/")
	var lr json.Number
	if len(r.LastRead) > 0 && json.Unmarshal(r.LastRead, &lr) == nil {
		f.LastRead, _ = lr.Int64()
	}
	return f
}

func (a *App) filePage(ctx context.Context, raws []browseFileRaw) []File {
	out := make([]File, 0, len(raws))
	ids := make([]string, 0, len(raws))
	for i := range raws {
		out = append(out, raws[i].file())
		ids = append(ids, raws[i].User)
	}
	a.EnsureUsers(ctx, ids)
	return out
}

// filesParams builds search.modules.files params for the Files browser.
func filesParams(o FilesOpts, self string) url.Values {
	page := o.Page
	if page < 1 {
		page = 1
	}
	types := fileTypesAll
	switch {
	case o.Kind == "canvases":
		types = "type:quip"
	case o.Kind == "lists":
		types = "type:lists"
	case len(o.Types) > 0:
		ts := make([]string, 0, len(o.Types))
		for _, t := range o.Types {
			ts = append(ts, "type:"+t)
		}
		types = strings.Join(ts, " ")
	case o.Mine || o.SharedWithMe:
		types = fileTypesOwned
	}
	parts := []string{types}
	sortBy := "last_engaged"
	q := strings.TrimSpace(o.Query)
	if q != "" || o.SharedWithMe {
		sortBy = "score"
	}
	if o.Mine {
		parts = append(parts, "creator:<@"+self+">")
		sortBy = "timestamp"
	}
	if o.Sort != "" {
		sortBy = o.Sort
	}
	if q != "" {
		parts = append(parts, q)
	}
	p := url.Values{"module": {"files"}, "query": {strings.Join(parts, " ")}, "page": {strconv.Itoa(page)},
		"count": {"50"}, "sort": {sortBy}, "search_context": {"desktop_files_browser"},
		"include_files_shares": {"1"}, "extracts": {"1"}, "highlight": {"1"}, "max_extract_len": {"200"},
		"extra_message_data": {"1"}, "no_user_profile": {"1"}, "file_title_only": {"false"},
		"query_rewrite_disabled": {"false"}, "max_filter_suggestions": {"10"}, "search_exclude_bots": {"false"},
		"search_only_my_channels": {"false"}, "shared_with_you": {"0"},
		"client_req_id": {clientMsgID()}, "browse_session_id": {clientMsgID()}}
	if o.SharedWithMe {
		p.Set("search_only_my_channels", "true")
		p.Set("shared_with_you", "1")
	}
	return p
}

// Files pages the Files browser (search.modules.files, as Slack's
// unified files view calls it).
func (a *App) Files(ctx context.Context, o FilesOpts) (FilePage, error) {
	t0 := time.Now()
	p := filesParams(o, a.S.SelfID())
	var res struct {
		Pagination searchPagination `json:"pagination"`
		Items      []browseFileRaw  `json:"items"`
	}
	if err := a.C.Call(ctx, "search.modules.files", p, &res); err != nil {
		return FilePage{}, err
	}
	out := FilePage{Files: a.filePage(ctx, res.Items), Page: res.Pagination.Page, Pages: res.Pagination.PageCount,
		Total: res.Pagination.Total}
	if out.Page == 0 {
		out.Page, _ = strconv.Atoi(p.Get("page"))
	}
	log.Printf("files: %d of %d in %s", len(out.Files), out.Total, time.Since(t0).Round(time.Millisecond))
	return out, nil
}

// DeletedFiles lists recently deleted files (files.recentlyDeleted).
func (a *App) DeletedFiles(ctx context.Context) (FilePage, error) {
	var res struct {
		Files []browseFileRaw `json:"files"`
	}
	if err := a.C.Call(ctx, "files.recentlyDeleted", url.Values{}, &res); err != nil {
		return FilePage{}, err
	}
	return FilePage{Files: a.filePage(ctx, res.Files), Page: 1, Pages: 1, Total: len(res.Files)}, nil
}

// ---- huddles ----

type huddleRaw struct {
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

	ThreadRootTS     string `json:"thread_root_ts"`
	TranscriptFileID string `json:"transcript_file_id"`
	Recording        *struct {
		RecordingType string `json:"recording_type"`
	} `json:"recording"`
	Artifacts *struct {
		FileIDs []string `json:"file_ids"`
		Links   []struct {
			URL     string `json:"url"`
			Title   string `json:"title"`
			IconURL string `json:"icon_url"`
		} `json:"links"`
	} `json:"artifacts"`
	AttachedFileIDs    []string                   `json:"attached_file_ids"`
	MissedParticipants map[string]json.RawMessage `json:"missed_participants"`
	ChannelType        string                     `json:"channel_type"`
	AccessTypeByUser   map[string]int             `json:"access_type_by_user"`
	BackgroundID       string                     `json:"background_id"`
}

func (r *huddleRaw) huddle() Huddle {
	h := Huddle{ID: r.ID, Name: r.Name, CreatedBy: r.CreatedBy, Channels: r.Channels,
		Participants: r.Participants, History: r.ParticipantHistory, Start: r.DateStart, End: r.DateEnd,
		Active: r.DateEnd == 0 && !r.HasEnded && len(r.Participants) > 0, DM: r.IsDMCall,
		HasEnded: r.HasEnded, ThreadTS: r.ThreadRootTS, TranscriptFileID: r.TranscriptFileID,
		AttachedFileIDs: r.AttachedFileIDs, ChannelType: r.ChannelType, AccessByUser: r.AccessTypeByUser,
		BackgroundID: r.BackgroundID}
	if r.Recording != nil {
		h.RecordingType = r.Recording.RecordingType
	}
	if r.Artifacts != nil {
		h.FileIDs = r.Artifacts.FileIDs
		for _, l := range r.Artifacts.Links {
			h.Links = append(h.Links, HuddleLink{URL: l.URL, Title: l.Title, IconURL: l.IconURL})
		}
	}
	if len(r.MissedParticipants) > 0 {
		h.Missed = make([]string, 0, len(r.MissedParticipants))
		for id := range r.MissedParticipants {
			h.Missed = append(h.Missed, id)
		}
		sort.Strings(h.Missed)
	}
	return h
}

// ParseHuddle decodes a huddle room: an sh_room_* event ({room} or
// {huddle}), a huddle message's room JSON (Message.Huddle), or a bare room.
func ParseHuddle(raw json.RawMessage) (Huddle, bool) {
	if len(raw) == 0 {
		return Huddle{}, false
	}
	var ev struct {
		Room   *huddleRaw `json:"room"`
		Huddle *huddleRaw `json:"huddle"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return Huddle{}, false
	}
	for _, r := range []*huddleRaw{ev.Room, ev.Huddle} {
		if r != nil && r.ID != "" {
			return r.huddle(), true
		}
	}
	var r huddleRaw
	if json.Unmarshal(raw, &r) != nil || r.ID == "" {
		return Huddle{}, false
	}
	return r.huddle(), true
}

// parseHuddles decodes a list of rooms (or wrapped rooms), skipping
// entries without an id. active marks every room still open as active
// (edge huddles/list may omit participants).
func parseHuddles(items []json.RawMessage, active bool) []Huddle {
	out := make([]Huddle, 0, len(items))
	for _, it := range items {
		h, ok := ParseHuddle(it)
		if !ok {
			continue
		}
		if active && h.End == 0 {
			h.Active = true
		}
		out = append(out, h)
	}
	return out
}

func (a *App) ensureHuddleUsers(ctx context.Context, hs []Huddle) {
	var ids []string
	for _, h := range hs {
		ids = append(ids, h.CreatedBy)
		ids = append(ids, h.Participants...)
		ids = append(ids, h.History...)
	}
	a.EnsureUsers(ctx, ids)
}

// HuddleHistory lists huddles the viewer was part of, newest first
// (huddles.history, as Slack's Huddles page calls it). Pass next back as
// cursor; "" means no more.
func (a *App) HuddleHistory(ctx context.Context, cursor string) ([]Huddle, string, error) {
	return a.HuddleHistoryPage(ctx, HuddleHistoryOpts{Cursor: cursor})
}

// parseActiveHuddles decodes an edge huddles/list response.
func parseActiveHuddles(raw json.RawMessage) ([]Huddle, error) {
	var res struct {
		Huddles []json.RawMessage `json:"huddles"`
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, err
	}
	return parseHuddles(append(res.Huddles, res.Results...), true), nil
}

// ActiveHuddles lists huddles running now that the viewer can see
// (edge huddles/list).
func (a *App) ActiveHuddles(ctx context.Context) ([]Huddle, error) {
	var raw json.RawMessage
	if err := a.C.Edge(ctx, "huddles/list", map[string]any{}, &raw); err != nil {
		return nil, err
	}
	out, err := parseActiveHuddles(raw)
	if err != nil {
		return nil, err
	}
	a.ensureHuddleUsers(ctx, out)
	return out, nil
}

// ---- sent ----

type sentItemRaw struct {
	TS           json.RawMessage `json:"ts"`
	MsgTimestamp json.RawMessage `json:"msg_timestamp"`
	ID           string          `json:"id"`
	ChannelID    string          `json:"channel_id"`
	Channel      json.RawMessage `json:"channel"`
}

func (r *sentItemRaw) ref() (ch, ts string) {
	return firstNonEmpty(r.ChannelID, chanID(r.Channel), r.ID), firstNonEmpty(looseString(r.TS), looseString(r.MsgTimestamp))
}

// Sent pages the viewer's sent messages (messages.sent.list, hydrated with
// messages.list). For the first page pass cursor "" and page 1; then pass
// SentPage.Next with page+1.
func (a *App) Sent(ctx context.Context, cursor string, page int) (SentPage, error) {
	t0 := time.Now()
	if page < 1 {
		page = 1
	}
	if cursor == "" {
		cursor = "*"
	}
	p := url.Values{"count": {"25"}, "page": {strconv.Itoa(page)}, "view": {"channel_and_thread"}, "cursor": {cursor},
		"client_req_id": {clientMsgID()}, "search_session_id": {clientMsgID()}}
	var res struct {
		Pagination searchPagination `json:"pagination"`
		Items      []sentItemRaw    `json:"items"`
	}
	if err := a.C.Call(ctx, "messages.sent.list", p, &res); err != nil {
		return SentPage{}, err
	}
	type sentRef struct{ ch, ts string }
	refs := make([]sentRef, 0, len(res.Items))
	want := map[string][]string{}
	for i := range res.Items {
		ch, ts := res.Items[i].ref()
		if ch == "" || ts == "" {
			continue
		}
		refs = append(refs, sentRef{ch, ts})
		want[ch] = append(want[ch], ts)
	}
	got := a.hydrate(ctx, want)
	out := SentPage{Next: res.Pagination.NextCursor, Page: res.Pagination.Page, Total: res.Pagination.Total}
	if out.Page == 0 {
		out.Page = page
	}
	chans := map[string]bool{}
	seen := map[string]bool{}
	for _, r := range refs {
		k := r.ch + "/" + r.ts
		m, ok := got[k]
		if !ok || seen[k] {
			continue
		}
		seen[k] = true
		chans[r.ch] = true
		out.Messages = append(out.Messages, a.message(r.ch, &m))
	}
	sort.SliceStable(out.Messages, func(i, j int) bool { return store.TSLess(out.Messages[j].TS, out.Messages[i].TS) })
	out.Convs = a.convRefs(chans)
	log.Printf("sent: %d of %d items in %s", len(out.Messages), len(res.Items), time.Since(t0).Round(time.Millisecond))
	return out, nil
}

// ---- directories ----

var browseChannelTypes = map[string]string{
	"": "exclude_archived", "any": "exclude_archived", "public": "private_exclude", "private": "private",
	"archived": "archived", "external": "external_shared",
}

func browseSortDir(desc bool) string {
	if desc {
		return "desc"
	}
	return "asc"
}

// channelBrowseParams builds search.modules.channels params for the
// channel directory.
func channelBrowseParams(o ChannelBrowseOpts) url.Values {
	page := o.Page
	if page < 1 {
		page = 1
	}
	ct, ok := browseChannelTypes[o.Type]
	if !ok {
		ct = "exclude_archived"
	}
	p := url.Values{"module": {"channels"}, "query": {o.Query}, "page": {strconv.Itoa(page)}, "count": {"50"},
		"browse": {"standard"}, "search_context": {"desktop_channel_browser"}, "sort": {"recommended"},
		"recommend_source": {"channel-browser"}, "channel_type": {ct},
		"exclude_my_channels": {"0"}, "search_only_my_channels": {"false"},
		"extracts": {"0"}, "highlight": {"0"}, "extra_message_data": {"0"}, "no_user_profile": {"1"}}
	switch o.Sort {
	case "name", "created", "member_count", "members":
		s := o.Sort
		if s == "members" {
			s = "member_count"
		}
		p.Set("sort", s)
		p.Set("sort_dir", browseSortDir(o.Desc))
	}
	switch o.Membership {
	case "joined":
		p.Set("search_only_my_channels", "true")
		if p.Get("sort") == "recommended" { // recommendations ignore the membership filter (verified)
			p.Set("sort", "name")
			p.Set("sort_dir", browseSortDir(o.Desc))
		}
	case "other":
		p.Set("exclude_my_channels", "1")
	}
	// XjMX getSearchAPIParams: organizations narrow the type to the shared
	// channels (archived and private ones keep their kind)
	if len(o.Orgs) > 0 && o.Type != "external" {
		switch o.Type {
		case "archived":
			p.Set("channel_type", "external_shared_archived")
		case "private":
			p.Set("channel_type", "external_shared_private")
		default:
			p.Set("channel_type", "external_shared")
		}
	}
	if len(o.Orgs) > 0 {
		p.Set("external_shared_team_ids", strings.Join(o.Orgs, ","))
	}
	if o.Team != "" {
		p.Set("search_only_team", o.Team)
	}
	return p
}

// channelItemRaw is a search.modules.channels item; purpose/topic and
// timestamp are decoded loosely (string or object / number or string).
type channelItemRaw struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	IsPrivate   bool            `json:"is_private"`
	IsArchived  bool            `json:"is_archived"`
	IsMember    bool            `json:"is_member"`
	IsMPIM      bool            `json:"is_mpim"`
	NumMembers  int             `json:"num_members"`
	MemberCount int             `json:"member_count"`
	Created     json.RawMessage `json:"created"`
	Timestamp   json.RawMessage `json:"timestamp"`
	Purpose     json.RawMessage `json:"purpose"`
	Topic       json.RawMessage `json:"topic"`
}

func (r *channelItemRaw) hit() slack.ChannelHit {
	h := slack.ChannelHit{ID: r.ID, Name: r.Name, IsPrivate: r.IsPrivate, IsArchived: r.IsArchived, IsMember: r.IsMember,
		IsMPIM: r.IsMPIM, NumMembers: r.NumMembers, MemberCount: r.MemberCount, Created: looseInt(r.Created)}
	if h.Created == 0 {
		h.Created = looseInt(r.Timestamp)
	}
	if v := looseText(r.Purpose); v != "" {
		h.Purpose = &slack.Purpose{Value: v}
	}
	if v := looseText(r.Topic); v != "" {
		h.Topic = &slack.Purpose{Value: v}
	}
	return h
}

// BrowseChannelsWith is the channel directory with Slack's filters and
// sorts (search.modules.channels).
func (a *App) BrowseChannelsWith(ctx context.Context, o ChannelBrowseOpts) (ChannelPage, error) {
	p := channelBrowseParams(o)
	var res struct {
		Items      []channelItemRaw `json:"items"`
		Pagination searchPagination `json:"pagination"`
	}
	if err := a.C.Call(ctx, "search.modules.channels", p, &res); err != nil {
		return ChannelPage{}, err
	}
	hs := make([]slack.ChannelHit, 0, len(res.Items))
	for i := range res.Items {
		hs = append(hs, res.Items[i].hit())
	}
	out := ChannelPage{Channels: a.channelHits(hs), Total: res.Pagination.Total, Pages: res.Pagination.PageCount,
		Page: res.Pagination.Page}
	if out.Page == 0 {
		out.Page, _ = strconv.Atoi(p.Get("page"))
	}
	return out, nil
}

var browseAccountTypes = map[string]string{
	"owners": "0,1", "admins": "0,1,2", "members": "0,1,2,3,7", "guests": "4,5,8", "deactivated": "6",
}

// peopleBrowseParams builds search.modules.people params for the people
// directory.
func peopleBrowseParams(o PeopleBrowseOpts) url.Values {
	page := o.Page
	if page < 1 {
		page = 1
	}
	p := url.Values{"module": {"people"}, "query": {o.Query}, "page": {strconv.Itoa(page)}, "count": {"50"},
		"browse": {"standard"}, "search_context": {"desktop_people_browser"}, "sort": {"recommended"},
		"hide_deactivated_users": {"1"}, "custom_fields": {"{}"}, "recommend_source": {"people-browser"},
		"extracts": {"0"}, "highlight": {"0"}, "extra_message_data": {"1"}, "no_user_profile": {"1"},
		"include_files_shares": {"1"}, "max_filter_suggestions": {"10"}, "file_title_only": {"false"},
		"query_rewrite_disabled": {"false"}, "client_req_id": {clientMsgID()}, "browse_session_id": {clientMsgID()}}
	if o.Sort == "name" {
		p.Set("sort", "name")
		p.Set("sort_dir", browseSortDir(o.Desc))
	}
	if at := browseAccountTypes[o.AccountType]; at != "" {
		p.Set("account_type", at)
	}
	if o.AccountType == "deactivated" {
		p.Set("hide_deactivated_users", "0")
	}
	if o.Team != "" {
		p.Set("search_only_team", o.Team)
	}
	if len(o.Locations) > 0 {
		p.Set("profile_location", strings.Join(o.Locations, ","))
	}
	if len(o.Titles) > 0 {
		p.Set("profile_title", strings.Join(o.Titles, ","))
	}
	if len(o.CustomFields) > 0 {
		j, _ := json.Marshal(o.CustomFields)
		p.Set("custom_fields", string(j))
	}
	return p
}

// BrowsePeople is the people directory (search.modules.people). Results
// are cached in memory and subscribed for presence.
func (a *App) BrowsePeople(ctx context.Context, o PeopleBrowseOpts) (PeoplePage, error) {
	p := peopleBrowseParams(o)
	var res struct {
		Items       []slack.User     `json:"items"`
		Pagination  searchPagination `json:"pagination"`
		Suggestions struct {
			Title    []facetRaw `json:"title"`
			Location []facetRaw `json:"location"`
		} `json:"filter_suggestions"`
	}
	if err := a.C.Call(ctx, "search.modules.people", p, &res); err != nil {
		return PeoplePage{}, err
	}
	a.S.PutUsers(res.Items)
	out := PeoplePage{Users: make([]User, 0, len(res.Items)), Total: res.Pagination.Total, Page: res.Pagination.Page,
		Pages: res.Pagination.PageCount, Titles: facets(res.Suggestions.Title), Locations: facets(res.Suggestions.Location)}
	// hydrateFacetSuggestions (ujfX): locations by count, most first
	sort.SliceStable(out.Locations, func(i, j int) bool { return out.Locations[i].Count > out.Locations[j].Count })
	if out.Page == 0 {
		out.Page, _ = strconv.Atoi(p.Get("page"))
	}
	ids := make([]string, 0, len(res.Items))
	for i := range res.Items {
		out.Users = append(out.Users, a.userOf(&res.Items[i]))
		ids = append(ids, res.Items[i].ID)
	}
	a.SubscribePresence(ids)
	return out, nil
}

// SearchUsergroups is the user group directory (usergroups.search).
func (a *App) SearchUsergroups(ctx context.Context, o UsergroupOpts) ([]Usergroup, error) {
	sortBy := o.Sort
	switch sortBy {
	case "date_create", "member_count":
	default:
		sortBy = "name"
	}
	count := o.Count
	if count <= 0 {
		count = 50
	}
	p := url.Values{"keyword": {strings.TrimPrefix(strings.TrimSpace(o.Query), "@")}, "sort": {sortBy},
		"sort_dir": {browseSortDir(o.Desc)}, "include_hidden": {"false"}, "include_disabled": {strconv.FormatBool(!o.ExcludeDisabled)},
		"count": {strconv.Itoa(count)}}
	if o.Team != "" {
		p.Set("search_only_team", o.Team)
	}
	var res struct {
		Usergroups []struct {
			ID          string          `json:"id"`
			Handle      string          `json:"handle"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			UserCount   json.RawMessage `json:"user_count"`
		} `json:"usergroups"`
	}
	if err := a.C.Call(ctx, "usergroups.search", p, &res); err != nil {
		return nil, err
	}
	out := make([]Usergroup, 0, len(res.Usergroups))
	for _, g := range res.Usergroups {
		out = append(out, Usergroup{ID: g.ID, Handle: g.Handle, Name: g.Name, Description: g.Description,
			UserCount: int(looseInt(g.UserCount))})
	}
	return out, nil
}

// ---- channel tab bar ----

// builtinTabTitles names built-in tabs that have no label, by type.
var builtinTabTitles = map[string]string{
	"folder": "Bookmarks", "bookmarks": "Pins", "files": "Files & links", "workflows": "Workflows",
	"channel_canvas": "Canvas", "canvas": "Untitled", "list": "Untitled list",
}

// builtinTabIDTitles is Slack's channel tab label switch on the tab id
// (boot-shared ChannelTabLabel, 2026-11): the built-in tabs keep these
// names whatever their label says. "bookmarks" is the Pins tab since the
// 2026 pins/bookmarks merge; only a folder tab without a label is
// "Bookmarks".
var builtinTabIDTitles = map[string]string{
	"bookmarks": "Pins", "pins": "Pins", "files": "Files & links", "workflows": "Workflows", "channel": "Messages",
}

// tabFileFetchers bounds parallel files.info calls for tab titles.
const tabFileFetchers = 4

type tabRaw struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Label      string `json:"label"`
	IsDisabled bool   `json:"is_disabled"`
	Data       struct {
		FileID           string `json:"file_id"`
		DefaultView      string `json:"lists_channel_tab_default_refine_view_id"`
		FolderBookmarkID string `json:"folder_bookmark_id"`
		AppID            string `json:"app_id"`
	} `json:"data"`
}

// Tabs lists a channel's tab bar (conversations.info properties.tabs),
// in server order, without disabled built-ins. Canvas and list tabs are
// titled from files.info; folder tabs carry the channel's bookmarks.
func (a *App) Tabs(ctx context.Context, ch string) ([]Tab, error) {
	t0 := time.Now()
	var res struct {
		Channel struct {
			Properties struct {
				Tabs []tabRaw `json:"tabs"`
			} `json:"properties"`
		} `json:"channel"`
	}
	if err := a.C.Call(ctx, "conversations.info", url.Values{"channel": {ch}}, &res); err != nil {
		return nil, err
	}
	var out []Tab
	var fileTabs, folderTabs []int
	for _, r := range res.Channel.Properties.Tabs {
		if r.IsDisabled {
			continue
		}
		t := Tab{ID: r.ID, Type: r.Type, Label: r.Label, Title: r.Label, FileID: r.Data.FileID, DefaultView: r.Data.DefaultView,
			FolderBookmarkID: r.Data.FolderBookmarkID, AppID: r.Data.AppID}
		if n, ok := builtinTabIDTitles[r.ID]; ok {
			t.Title = n
		}
		if t.Title == "" {
			t.Title = builtinTabTitles[r.Type]
		}
		switch {
		case (r.Type == "canvas" || r.Type == "list") && t.FileID != "":
			fileTabs = append(fileTabs, len(out))
		case r.Type == "folder":
			folderTabs = append(folderTabs, len(out))
		}
		out = append(out, t)
	}
	var wg sync.WaitGroup
	if len(folderTabs) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bms, err := a.Bookmarks(ctx, ch)
			if err != nil {
				log.Printf("tabs %s: bookmarks.list: %v", ch, err)
				return
			}
			byParent := map[string][]Bookmark{}
			for _, b := range bms {
				byParent[b.ParentID] = append(byParent[b.ParentID], b)
			}
			for _, i := range folderTabs {
				if kids, ok := byParent[out[i].ID]; ok {
					out[i].Bookmarks = kids
				} else {
					out[i].Bookmarks = bms
				}
			}
		}()
	}
	sem := make(chan struct{}, tabFileFetchers)
	for _, i := range fileTabs {
		wg.Add(1)
		go func(t *Tab) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			title, deleted, unread, views, err := a.tabFileTitle(ctx, t.FileID)
			if err != nil {
				log.Printf("tabs %s: files.info %s: %v", ch, t.FileID, err)
				return
			}
			if deleted {
				t.Title, t.Deleted = "Deleted file", true
			} else if title != "" && t.Label == "" {
				t.Title = title // a tab's own label wins (RenameTabModal: "Clear text to default to file name")
			}
			if t.Type == "list" && t.DefaultView != "" {
				t.ViewName = views[t.DefaultView]
			}
			t.Unread = unread && !deleted && t.Type == "canvas"
		}(&out[i])
	}
	wg.Wait()
	log.Printf("tabs %s: %d tabs (%d files) in %s", ch, len(out), len(fileTabs), time.Since(t0).Round(time.Millisecond))
	return out, nil
}

// tabFileTitle returns a tab file's title, or deleted when it is gone, and
// unread when the viewer never opened it (files.info has no last_read);
// for a list, its views' names by id (list_metadata.views).
func (a *App) tabFileTitle(ctx context.Context, fileID string) (title string, deleted, unread bool, views map[string]string, err error) {
	var r struct {
		File struct {
			browseFileRaw
			ListMetadata struct {
				Views []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"views"`
			} `json:"list_metadata"`
		} `json:"file"`
	}
	err = a.C.Call(ctx, "files.info", url.Values{"file": {fileID}}, &r)
	if isCode(err, "file_not_found", "file_deleted") {
		return "", true, false, nil, nil
	}
	if err != nil {
		return "", false, false, nil, err
	}
	if r.File.Mode == "tombstone" || r.File.IsDeleted {
		return "", true, false, nil, nil
	}
	if vs := r.File.ListMetadata.Views; len(vs) > 0 {
		views = make(map[string]string, len(vs))
		for _, v := range vs {
			views[v.ID] = v.Name
		}
	}
	lr := strings.TrimSpace(string(r.File.LastRead))
	return firstNonEmpty(r.File.Title, r.File.Name), false, lr == "" || lr == "null" || lr == "0", views, nil
}

// RemoveTab deletes a tab from a channel's tab bar (conversations.removeTab;
// how Slack's "Delete folder" works).
func (a *App) RemoveTab(ctx context.Context, ch, tabID string) error {
	return a.C.Call(ctx, "conversations.removeTab", url.Values{"id": {tabID}, "channel_id": {ch}}, nil)
}

// ---- member counts & managers ----

// parseMemberCounts decodes an edge users/counts response.
func parseMemberCounts(raw json.RawMessage) (MemberCounts, error) {
	var res struct {
		Counts struct {
			Everyone int `json:"everyone"`
			People   int `json:"people"`
			Members  int `json:"members"`
			Guests   int `json:"guests"`
			Bots     int `json:"bots"`
			Apps     int `json:"apps"`
		} `json:"counts"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return MemberCounts{}, err
	}
	c := res.Counts
	people := c.People
	if people == 0 {
		people = c.Members
	}
	return MemberCounts{People: people, Everyone: c.Everyone, Bots: c.Bots, Apps: c.Apps, Guests: c.Guests}, nil
}

// MemberCounts returns a channel's member counts (edge users/counts, what
// Slack's channel header uses: People).
func (a *App) MemberCounts(ctx context.Context, ch string) (MemberCounts, error) {
	var raw json.RawMessage
	if err := a.C.Edge(ctx, "users/counts", map[string]any{"channel": ch, "as_admin": false}, &raw); err != nil {
		return MemberCounts{}, err
	}
	return parseMemberCounts(raw)
}

// channelManagerRole is the role id of Slack's channel managers.
const channelManagerRole = "Rl0A"

// Managers lists a channel's managers ("Managed by"):
// fetchAndUpsertChannelManagers (evGV), admin.roles.entity.listAssignments
// {entity_id, reason "fetch-channel-managers"}; the answer replaces the
// channel managers store (setChannelManagers, channel_managers.go).
func (a *App) Managers(ctx context.Context, ch string) ([]string, error) {
	var res struct {
		Assignments []struct {
			RoleID string   `json:"role_id"`
			Users  []string `json:"users"`
		} `json:"role_assignments"`
	}
	if err := a.C.Call(ctx, "admin.roles.entity.listAssignments", url.Values{"entity_id": {ch}, "reason": {"fetch-channel-managers"}}, &res); err != nil {
		return nil, err
	}
	out := []string{}
	seen := map[string]bool{}
	for _, r := range res.Assignments {
		if r.RoleID != channelManagerRole {
			continue
		}
		for _, u := range r.Users {
			if u != "" && !seen[u] {
				seen[u] = true
				out = append(out, u)
			}
		}
	}
	a.setChannelManagers(ch, out)
	a.EnsureUsers(ctx, out)
	return out, nil
}
