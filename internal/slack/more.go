package slack

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ExperimentGroups returns the viewer's experiment assignments
// (experiments.getByUser): experiment name -> group ("on", "without_edu", …).
func (c *Client) ExperimentGroups(ctx context.Context) (map[string]string, error) {
	var r struct {
		Assignments map[string]struct {
			Group string `json:"group"`
		} `json:"assignments"`
	}
	if err := c.Call(ctx, "experiments.getByUser", nil, &r); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(r.Assignments))
	for k, v := range r.Assignments {
		out[k] = v.Group
	}
	return out, nil
}

// SidebarDM is one direct message or group DM from sidebar.dms.
type SidebarDM struct {
	ID       string `json:"id"`
	User     string `json:"user,omitempty"`
	IsOpen   bool   `json:"is_open"`
	IsIM     bool   `json:"is_im,omitempty"`
	IsMPIM   bool   `json:"is_mpim,omitempty"`
	LastRead string `json:"last_read,omitempty"`
}

// SidebarDMs lists every DM and group DM the viewer has, each with its
// is_open flag (sidebar.dms, on the enterprise host). Slack's own sidebar
// shows only the open ones: users.conversations returns closed DMs too and
// has no is_open. It follows response_metadata.next_cursor if Slack ever
// pages the answer.
func (c *Client) SidebarDMs(ctx context.Context) ([]SidebarDM, error) {
	var all []SidebarDM
	cursor := ""
	for i := 0; i < 50; i++ {
		p := url.Values{}
		if cursor != "" {
			p.Set("cursor", cursor)
		}
		var r struct {
			IMs   []SidebarDM `json:"ims"`
			MPDMs []SidebarDM `json:"mpdms"`
			MPIMs []SidebarDM `json:"mpims"`
			Meta  struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := c.Call(ctx, "sidebar.dms", p, &r); err != nil {
			return all, err
		}
		for _, d := range r.IMs {
			d.IsIM = true
			all = append(all, d)
		}
		for _, d := range append(r.MPDMs, r.MPIMs...) {
			d.IsMPIM = true
			all = append(all, d)
		}
		if r.Meta.NextCursor == "" || r.Meta.NextCursor == cursor {
			break
		}
		cursor = r.Meta.NextCursor
	}
	return all, nil
}

// SectionsAll lists sidebar sections, following the top-level cursor and
// each section's channel_ids_page cursor so big sections are complete.
func (c *Client) SectionsAll(ctx context.Context) ([]Section, error) {
	var all []Section
	seen := map[string]bool{}
	cursor := ""
	for i := 0; i < 20; i++ {
		p := url.Values{}
		if cursor != "" {
			p.Set("cursor", cursor)
		}
		var r struct {
			Sections []Section `json:"channel_sections"`
			Cursor   string    `json:"cursor"`
		}
		if err := c.Call(ctx, "users.channelSections.list", p, &r); err != nil {
			return all, err
		}
		added := 0
		for _, s := range r.Sections {
			if !seen[s.ID] {
				seen[s.ID] = true
				all = append(all, s)
				added++
			}
		}
		// Slack hands back a cursor even when there is nothing more; stop
		// as soon as a page brings no new sections.
		if r.Cursor == "" || r.Cursor == cursor || added == 0 {
			break
		}
		cursor = r.Cursor
	}
	// Page through any section whose channel list is truncated.
	for i := range all {
		s := &all[i]
		cur := s.ChannelIDsPage.Cursor
		for n := 0; cur != "" && n < 50; n++ {
			var r struct {
				Sections []Section `json:"channel_sections"`
			}
			err := c.Call(ctx, "users.channelSections.list", url.Values{"channel_section_id": {s.ID}, "cursor": {cur}}, &r)
			if err != nil {
				break
			}
			next := ""
			for _, rs := range r.Sections {
				if rs.ID == s.ID {
					s.ChannelIDsPage.ChannelIDs = append(s.ChannelIDsPage.ChannelIDs, rs.ChannelIDsPage.ChannelIDs...)
					next = rs.ChannelIDsPage.Cursor
				}
			}
			if next == cur {
				break
			}
			cur = next
		}
		s.ChannelIDsPage.ChannelIDs = uniq(s.ChannelIDsPage.ChannelIDs)
	}
	return all, nil
}

func uniq(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// MessagesList fetches specific messages in one round trip:
// want maps channel -> timestamps. Returns channel -> messages.
func (c *Client) MessagesList(ctx context.Context, want map[string][]string) (map[string][]Message, error) {
	type q struct {
		Channel    string   `json:"channel"`
		Timestamps []string `json:"timestamps"`
	}
	var ids []q
	for ch, ts := range want {
		if ch != "" && len(ts) > 0 {
			ids = append(ids, q{ch, uniq(append([]string(nil), ts...))})
		}
	}
	if len(ids) == 0 {
		return map[string][]Message{}, nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].Channel < ids[j].Channel })
	b, _ := json.Marshal(ids)
	var r struct {
		Messages map[string][]Message `json:"messages"`
	}
	err := c.Call(ctx, "messages.list", url.Values{"message_ids": {string(b)}, "org_wide_aware": {"true"}}, &r)
	if r.Messages == nil {
		r.Messages = map[string][]Message{}
	}
	return r.Messages, err
}

// SearchMessages runs search.messages.
type SearchMatch struct {
	Message
	Permalink string `json:"permalink"`
	ChannelO  struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		IsIM      bool   `json:"is_im"`
		IsMPIM    bool   `json:"is_mpim"`
		IsPrivate bool   `json:"is_private"`
		User      string `json:"user"`
	} `json:"channel"`
}

type SearchResult struct {
	Matches []SearchMatch
	Total   int
	Page    int
	Pages   int
}

func (c *Client) SearchMessages(ctx context.Context, query string, page, count int, sort string) (*SearchResult, error) {
	if count <= 0 {
		count = 20
	}
	if page <= 0 {
		page = 1
	}
	p := url.Values{"query": {query}, "count": {strconv.Itoa(count)}, "page": {strconv.Itoa(page)},
		"highlight": {"false"}, "extracts": {"0"}}
	switch sort {
	case "timestamp", "newest":
		p.Set("sort", "timestamp")
		p.Set("sort_dir", "desc")
	case "oldest":
		p.Set("sort", "timestamp")
		p.Set("sort_dir", "asc")
	default:
		p.Set("sort", "score")
	}
	var raw struct {
		Messages struct {
			Total   int               `json:"total"`
			Matches []json.RawMessage `json:"matches"`
			Paging  struct {
				Page  int `json:"page"`
				Pages int `json:"pages"`
			} `json:"paging"`
		} `json:"messages"`
	}
	if err := c.Call(ctx, "search.messages", p, &raw); err != nil {
		return nil, err
	}
	res := &SearchResult{Total: raw.Messages.Total, Page: raw.Messages.Paging.Page, Pages: raw.Messages.Paging.Pages}
	for _, m := range raw.Messages.Matches {
		// "channel" is an object here, so decode it separately from Message.
		var sm SearchMatch
		var msg Message
		var aux struct {
			Permalink string          `json:"permalink"`
			Channel   json.RawMessage `json:"channel"`
		}
		if json.Unmarshal(m, &aux) != nil {
			continue
		}
		stripped := stripKey(m, "channel")
		_ = json.Unmarshal(stripped, &msg)
		sm.Message = msg
		sm.Permalink = aux.Permalink
		_ = json.Unmarshal(aux.Channel, &sm.ChannelO)
		res.Matches = append(res.Matches, sm)
	}
	return res, nil
}

func stripKey(raw json.RawMessage, key string) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	delete(m, key)
	b, _ := json.Marshal(m)
	return b
}

// SearchUsers queries the edge user directory (what the web client's
// autocomplete and people browser use). Fast: ~400ms for the whole org.
func (c *Client) SearchUsers(ctx context.Context, query string, count int) ([]User, error) {
	if count <= 0 {
		count = 20
	}
	var r struct {
		Results []User `json:"results"`
	}
	err := c.Edge(ctx, "users/search", map[string]any{
		"query": query, "count": count, "fuzz": 1, "uax29_tokenizer": false, "include_profile_only_users": true,
		"enable_workspace_ranking": true,
	}, &r)
	return r.Results, err
}

// UserSearchOpts are the omniswitcher's extra users/search args (captured
// from app.slack.com, 2026-11-08: count 30, search_email, top_users =
// the frecency list, default_workspace).
type UserSearchOpts struct {
	Count            int
	SearchEmail      bool
	TopUsers         []string
	DefaultWorkspace string
}

// SearchUsersWith is edge users/search with the omniswitcher's args.
func (c *Client) SearchUsersWith(ctx context.Context, query string, o UserSearchOpts) ([]User, error) {
	if o.Count <= 0 {
		o.Count = 30
	}
	var r struct {
		Results []User `json:"results"`
	}
	body := map[string]any{
		"include_profile_only_users": true, "query": query, "count": o.Count, "fuzz": 1,
		"enable_workspace_ranking": true, "search_email": o.SearchEmail,
	}
	if o.TopUsers != nil {
		body["top_users"] = o.TopUsers
	}
	if o.DefaultWorkspace != "" {
		body["default_workspace"] = o.DefaultWorkspace
	}
	err := c.Edge(ctx, "users/search", body, &r)
	return r.Results, err
}

// ChannelSearchOpts are the omniswitcher's extra channels/search args
// (count 30, filter "xws", top_channels, default_workspace).
type ChannelSearchOpts struct {
	Count            int
	Filter           string
	TopChannels      []string
	DefaultWorkspace string
}

// SearchChannelsWith is edge channels/search with the omniswitcher's args.
func (c *Client) SearchChannelsWith(ctx context.Context, query string, o ChannelSearchOpts) ([]ChannelHit, error) {
	if o.Count <= 0 {
		o.Count = 30
	}
	var r struct {
		Results []ChannelHit `json:"results"`
	}
	body := map[string]any{
		"query": query, "count": o.Count, "fuzz": 1, "include_record_channels": true, "check_membership": true,
	}
	if o.Filter != "" {
		body["filter"] = o.Filter
	}
	if o.TopChannels != nil {
		body["top_channels"] = o.TopChannels
	}
	if o.DefaultWorkspace != "" {
		body["default_workspace"] = o.DefaultWorkspace
	}
	err := c.Edge(ctx, "channels/search", body, &r)
	return r.Results, err
}

// ChannelHit is a channel from the edge channel search / browse.
type ChannelHit struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	IsPrivate   bool     `json:"is_private"`
	IsArchived  bool     `json:"is_archived"`
	IsMember    bool     `json:"is_member"`
	IsChannel   bool     `json:"is_channel"`
	IsGeneral   bool     `json:"is_general"`
	IsMPIM      bool     `json:"is_mpim"`
	NumMembers  int      `json:"num_members"`
	MemberCount int      `json:"member_count"`
	Created     int64    `json:"created"`
	Creator     string   `json:"creator"`
	Topic       *Purpose `json:"topic"`
	Purpose     *Purpose `json:"purpose"`
}

func (c *Client) SearchChannels(ctx context.Context, query string, count int) ([]ChannelHit, error) {
	if count <= 0 {
		count = 20
	}
	var r struct {
		Results []ChannelHit `json:"results"`
	}
	err := c.Edge(ctx, "channels/search", map[string]any{
		"query": query, "count": count, "fuzz": 1, "uax29_tokenizer": false, "check_membership": true,
		"include_record_channels": true,
	}, &r)
	return r.Results, err
}

// BrowseChannels is the channel browser (search.modules.channels), paged.
func (c *Client) BrowseChannels(ctx context.Context, query string, page int, sort string) ([]ChannelHit, int, int, error) {
	if page <= 0 {
		page = 1
	}
	// browse + search_context make an empty query list every channel
	p := url.Values{"module": {"channels"}, "query": {query}, "page": {strconv.Itoa(page)}, "count": {"30"},
		"extracts": {"0"}, "highlight": {"0"}, "no_user_profile": {"1"}, "browse": {"standard"},
		"search_context": {"desktop_channel_browser"}}
	switch sort {
	case "name":
		p.Set("sort", "name")
		p.Set("sort_dir", "asc")
	case "created":
		p.Set("sort", "created")
		p.Set("sort_dir", "desc")
	case "members":
		p.Set("sort", "member_count")
		p.Set("sort_dir", "desc")
	}
	var r struct {
		Items      []ChannelHit `json:"items"`
		Pagination struct {
			Total     int `json:"total_count"`
			PageCount int `json:"page_count"`
		} `json:"pagination"`
	}
	err := c.Call(ctx, "search.modules.channels", p, &r)
	return r.Items, r.Pagination.Total, r.Pagination.PageCount, err
}

// ChannelMembers pages the members of a channel via the edge directory
// (conversations.members is enterprise_is_restricted for session tokens).
func (c *Client) ChannelMembers(ctx context.Context, ch, marker string, count int, peopleOnly bool) ([]User, string, error) {
	if count <= 0 {
		count = 100
	}
	filter := "everyone"
	if peopleOnly {
		filter = "everyone AND NOT bots AND NOT apps"
	}
	body := map[string]any{"channels": []string{ch}, "present_first": true, "filter": filter, "count": count}
	if marker != "" {
		body["marker"] = marker
	}
	var r struct {
		Results    []User `json:"results"`
		NextMarker string `json:"next_marker"`
	}
	err := c.Edge(ctx, "users/list", body, &r)
	return r.Results, r.NextMarker, err
}

// Usergroup is a user group (subteam).
type Usergroup struct {
	ID          string `json:"id"`
	Handle      string `json:"handle"`
	Name        string `json:"name"`
	Description string `json:"description"`
	UserCount   int    `json:"user_count"`
	DateDelete  int64  `json:"date_delete"`
}

// Usergroups lists the workspace's groups (the team host has them; the
// enterprise host answers with an empty list).
func (c *Client) Usergroups(ctx context.Context) ([]Usergroup, error) {
	var r struct {
		Usergroups []Usergroup `json:"usergroups"`
	}
	err := c.Call(ctx, "usergroups.list", url.Values{"include_count": {"true"}, "include_disabled": {"false"}}, &r)
	return r.Usergroups, err
}

// Prefs returns the user's client prefs (users.prefs.get).
func (c *Client) Prefs(ctx context.Context) (map[string]json.RawMessage, error) {
	var r struct {
		Prefs map[string]json.RawMessage `json:"prefs"`
	}
	err := c.Call(ctx, "users.prefs.get", nil, &r)
	return r.Prefs, err
}

// SetPref writes one user pref the way Slack's setUserPrefByApi does:
// users.prefs.set with name and value. Slack's client JSON-encodes the
// JSON-typed prefs (ia_theme, …) and sends every other value as its plain
// form value ("true", "3", "default"), so value arrives here already
// serialized.
func (c *Client) SetPref(ctx context.Context, name, value string) error {
	return c.Call(ctx, "users.prefs.set", url.Values{"name": {name}, "value": {value}}, nil)
}

// Command is a slash command from commands.list.
type Command struct {
	Name    string         `json:"name"`
	Desc    string         `json:"desc"`
	Usage   string         `json:"usage"`
	Type    string         `json:"type"`
	AppID   string         `json:"app,omitempty"`
	AppName string         `json:"app_name,omitempty"`
	Icons   map[string]any `json:"icons,omitempty"` // image_32 … image_512 (+ non-string flags)
}

// IconURL picks the best icon for a ~20-36 px command row.
func (c *Command) IconURL() string {
	for _, k := range []string{"image_72", "image_48", "image_64", "image_36", "image_96", "image_128", "image_32", "image_192", "image_512"} {
		if s, ok := c.Icons[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func (c *Client) Commands(ctx context.Context) ([]Command, error) {
	var r struct {
		Commands map[string]Command `json:"commands"`
	}
	if err := c.Call(ctx, "commands.list", nil, &r); err != nil {
		return nil, err
	}
	out := make([]Command, 0, len(r.Commands))
	for name, cmd := range r.Commands {
		if cmd.Name == "" {
			cmd.Name = name
		}
		out = append(out, cmd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// UploadFile is one file to upload (held in memory).
type UploadFile struct {
	Name  string
	Title string
	Data  []byte
}

// PrepareUpload reserves an upload slot (files.getUploadURLExternal) and
// sends the bytes. The file is not shared until CompleteUpload.
func (c *Client) PrepareUpload(ctx context.Context, f UploadFile, progress func(sent, total int64)) (string, error) {
	var r struct {
		UploadURL string `json:"upload_url"`
		FileID    string `json:"file_id"`
	}
	if err := c.Call(ctx, "files.getUploadURLExternal", url.Values{"filename": {f.Name}, "length": {strconv.Itoa(len(f.Data))}}, &r); err != nil {
		return "", err
	}
	if err := c.UploadBytes(ctx, r.UploadURL, f.Name, f.Data, progress); err != nil {
		return "", err
	}
	return r.FileID, nil
}

// UploadedRef names a prepared file to share.
type UploadedRef struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// CompleteUpload shares prepared files (files.completeUploadExternal).
func (c *Client) CompleteUpload(ctx context.Context, channel, threadTS, comment string, files []UploadedRef) ([]map[string]string, error) {
	b, _ := json.Marshal(files)
	p := url.Values{"files": {string(b)}}
	if channel != "" {
		p.Set("channel_id", channel)
	}
	if threadTS != "" {
		p.Set("thread_ts", threadTS)
	}
	if strings.TrimSpace(comment) != "" {
		p.Set("initial_comment", comment)
	}
	var r struct {
		Files []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Name  string `json:"name"`
		} `json:"files"`
	}
	if err := c.Call(ctx, "files.completeUploadExternal", p, &r); err != nil {
		return nil, err
	}
	out := make([]map[string]string, 0, len(r.Files))
	for _, f := range r.Files {
		out = append(out, map[string]string{"id": f.ID, "title": f.Title, "name": f.Name})
	}
	return out, nil
}

// Upload performs the three-step external upload and shares the files.
func (c *Client) Upload(ctx context.Context, channel, threadTS, comment string, files []UploadFile) ([]map[string]string, error) {
	var refs []UploadedRef
	for _, f := range files {
		id, err := c.PrepareUpload(ctx, f, nil)
		if err != nil {
			return nil, err
		}
		refs = append(refs, UploadedRef{ID: id, Title: firstNonEmptyS(f.Title, f.Name)})
	}
	return c.CompleteUpload(ctx, channel, threadTS, comment, refs)
}

func firstNonEmptyS(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// UsersInfoParallel resolves many users via the edge cache in parallel batches.
func (c *Client) UsersInfoParallel(ctx context.Context, ids []string) ([]User, error) {
	const batch = 100
	if len(ids) <= batch {
		return c.UsersInfo(ctx, ids)
	}
	var mu sync.Mutex
	var out []User
	var firstErr error
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := 0; i < len(ids); i += batch {
		j := i + batch
		if j > len(ids) {
			j = len(ids)
		}
		part := ids[i:j]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			us, err := c.UsersInfo(ctx, part)
			mu.Lock()
			out = append(out, us...)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out, firstErr
}
