package core

// The channel's Files & links tab (Slack's files_tab_view, boot-shared
// 3ztk, with useChannelAttachmentsExplorer JG8Q; build 0e695dd18302 /
// boot-shared 0fb3750ed9646c84, docs/FILES-AUDIT.md "Files & links tab").
// It runs three browsers, each its own request:
//
//   - media: the "channel-files" browser (permanent_browser_instance_
//     channel_tab_media_<channel>) with filters {in: [channel], fileType:
//     [images, videos]};
//   - files: the same browser kind (…_channel_tab_files_<channel>) with
//     {in: [channel], excludedFileType: [images, videos, folder]};
//   - links: the "channel-links" browser (browser_instance_channel_tab_
//     links_<channel>), fetchChannelLinks.
//
// The two file browsers are fetchBrowserEntity → fetchSearchResults →
// fetchSearchModule: search.modules.files with the filters as query
// modifiers (querySuggestionFromQueryAndFilter: in, then type, then
// -type), search_context desktop_files_channel_tab, getSearchAPIParams'
// files arguments and getSortParams' sort / sort_dir. The links browser is
// conversations.searchLinks {channel_id, page, query, sort, sort_dir}
// (its reason, fetch-channel-links, is request metadata).

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ChannelFilesSort is the tab's sort select (ogf8 QX): Newest
// (timestamp_desc), Oldest (timestamp_asc) or, with a search, Most
// relevant (score).
type ChannelFilesSort string

const (
	ChannelFilesNewest   ChannelFilesSort = "timestamp_desc"
	ChannelFilesOldest   ChannelFilesSort = "timestamp_asc"
	ChannelFilesRelevant ChannelFilesSort = "score"
)

// ChannelFilesOpts selects a page of one of the tab's file browsers.
type ChannelFilesOpts struct {
	Channel string
	Media   bool // the media browser (type:images type:videos); else files
	Query   string
	Sort    ChannelFilesSort
	Page    int
}

// ChannelLink is one conversations.searchLinks item.
type ChannelLink struct {
	URL, Title, IconURL string
	Timestamp           float64 // the share's ts, as the tab sorts it
}

// ChannelLinkPage is a page of a channel's links.
type ChannelLinkPage struct {
	Links       []ChannelLink
	Page, Pages int
	Total       int
}

// channelFilesQuery is querySuggestionFromQueryAndFilter for the tab's
// filters: the user's words, then in:<#C|name> (InQuerySuggestionToken:
// a group DM or a DM without the name), then type: (media) or -type:
// (files) for each file type, joined by spaces.
func channelFilesQuery(o ChannelFilesOpts, name string, mpim bool) string {
	var parts []string
	if q := strings.TrimSpace(o.Query); q != "" {
		parts = append(parts, q)
	}
	name = strings.ReplaceAll(name, ">", "") // sanitizeChannelRefName
	if name != "" && !mpim {
		parts = append(parts, "in:<#"+o.Channel+"|"+name+">")
	} else {
		parts = append(parts, "in:<#"+o.Channel+">")
	}
	if o.Media {
		parts = append(parts, "type:images", "type:videos")
	} else {
		parts = append(parts, "-type:images", "-type:videos", "-type:folder")
	}
	return strings.Join(parts, " ")
}

// channelFilesSortParams is getSortParams for the tab's sorts.
func channelFilesSortParams(s ChannelFilesSort) (sortBy, dir string) {
	switch s {
	case ChannelFilesOldest:
		return "timestamp", "asc"
	case ChannelFilesRelevant:
		return "score", "desc"
	}
	return "timestamp", "desc"
}

// channelFilesParams is fetchSearchModule's args for one of the tab's
// file browsers. The query always carries modifiers, so it is never an
// empty browse: highlight and extracts are on (getSearchAPIParams), and
// count is getSearchAPIParams' 50 (the browser sets no pageSize). The
// fetch's reason is request metadata (_x_reason), which Clack sends for
// no call, and team, which getSearchAPIParams adds outside an org, is
// left out as the Files browser's call leaves it (browse.go).
func channelFilesParams(o ChannelFilesOpts, name string, mpim bool) url.Values {
	page := o.Page
	if page < 1 {
		page = 1
	}
	sortBy, dir := channelFilesSortParams(o.Sort)
	p := url.Values{"module": {"files"}, "query": {channelFilesQuery(o, name, mpim)}, "page": {strconv.Itoa(page)},
		"client_req_id": {clientMsgID()}, "browse_session_id": {clientMsgID()},
		"extracts": {"1"}, "highlight": {"1"}, "max_extract_len": {"200"}, "extra_message_data": {"1"},
		"no_user_profile": {"1"}, "count": {"50"}, "file_title_only": {"false"}, "query_rewrite_disabled": {"false"},
		"include_files_shares": {"1"}, "search_context": {"desktop_files_channel_tab"},
		"max_filter_suggestions": {"10"}, "sort": {sortBy}, "sort_dir": {dir},
		"search_exclude_bots": {"false"}, "search_only_my_channels": {"false"}, "shared_with_you": {"0"}}
	return p
}

// ChannelFiles pages one of the Files & links tab's file browsers. Files
// Slack wouldn't show (shouldShowFile: tombstones, deleted or hidden
// files) are dropped, and repeats kept once (uniqBy id).
func (a *App) ChannelFiles(ctx context.Context, o ChannelFilesOpts) (FilePage, error) {
	t0 := time.Now()
	name, mpim := "", false
	if c, ok := a.Conv(o.Channel); ok {
		name, mpim = c.Name, c.Kind == "mpim" || c.Kind == "im"
	}
	p := channelFilesParams(o, name, mpim)
	var res struct {
		Pagination searchPagination `json:"pagination"`
		Items      []browseFileRaw  `json:"items"`
	}
	if err := a.C.Call(ctx, "search.modules.files", p, &res); err != nil {
		return FilePage{}, err
	}
	seen := map[string]bool{}
	raws := res.Items[:0]
	for _, r := range res.Items {
		if r.ID == "" || seen[r.ID] || !shownFile(&r) {
			continue
		}
		seen[r.ID] = true
		raws = append(raws, r)
	}
	out := FilePage{Files: a.filePage(ctx, raws), Page: res.Pagination.Page, Pages: res.Pagination.PageCount,
		Total: res.Pagination.Total}
	if out.Page == 0 {
		out.Page = max(o.Page, 1)
	}
	log.Printf("channel files %s (media %v): %d of %d, page %d/%d in %s", o.Channel, o.Media, len(out.Files), out.Total,
		out.Page, out.Pages, time.Since(t0).Round(time.Millisecond))
	return out, nil
}

// shownFile is shouldShowFile's checks a search item can fail: a
// tombstone, a deleted file, a file the viewer can't see.
func shownFile(r *browseFileRaw) bool {
	return r.Mode != "tombstone" && !r.IsDeleted && r.FileAccess != "access_denied" && r.FileAccess != "file_not_found"
}

// ChannelLinks pages the links shared in a channel (fetchChannelLinks:
// conversations.searchLinks, the browser's sort as sort / sort_dir).
func (a *App) ChannelLinks(ctx context.Context, ch, query string, s ChannelFilesSort, page int) (ChannelLinkPage, error) {
	if page < 1 {
		page = 1
	}
	// fetchChannelLinks' switch matches NEWEST and OLDEST (QX) and the
	// directories' RELEVANT ("relevant", n5), never the browser's own
	// Most relevant ("score"), which takes the default: timestamp desc.
	sortBy, dir := "timestamp", "desc"
	if s == ChannelFilesOldest {
		dir = "asc"
	}
	p := url.Values{"channel_id": {ch}, "page": {strconv.Itoa(page)},
		"sort": {sortBy}, "sort_dir": {dir}}
	if q := strings.TrimSpace(query); q != "" {
		p.Set("query", q)
	}
	var res struct {
		Items []struct {
			URL       string          `json:"url"`
			Title     *string         `json:"title"`
			IconURL   *string         `json:"icon_url"`
			Timestamp json.RawMessage `json:"timestamp"`
		} `json:"items"`
		Pagination struct {
			Page       int    `json:"page"`
			PageCount  int    `json:"page_count"`
			TotalCount int    `json:"total_count"`
			NextCursor string `json:"next_cursor"`
		} `json:"pagination"`
	}
	if err := a.C.Call(ctx, "conversations.searchLinks", p, &res); err != nil {
		return ChannelLinkPage{}, err
	}
	out := ChannelLinkPage{Page: res.Pagination.Page, Pages: res.Pagination.PageCount, Total: res.Pagination.TotalCount}
	if out.Page == 0 {
		out.Page = page
	}
	if out.Pages == 0 {
		out.Pages = 1
	}
	seen := map[string]bool{}
	for _, it := range res.Items {
		if it.URL == "" || seen[it.URL] { // uniqBy url
			continue
		}
		seen[it.URL] = true
		l := ChannelLink{URL: it.URL}
		if it.Title != nil {
			l.Title = *it.Title
		}
		if it.IconURL != nil {
			l.IconURL = *it.IconURL
		}
		l.Timestamp = looseFloat(it.Timestamp)
		out.Links = append(out.Links, l)
	}
	log.Printf("channel links %s: %d, page %d/%d", ch, len(out.Links), out.Page, out.Pages)
	return out, nil
}

// looseFloat reads a number sent as a JSON number or a string ("1762….0001").
func looseFloat(raw json.RawMessage) float64 {
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	f, _ := strconv.ParseFloat(s, 64)
	return f
}
