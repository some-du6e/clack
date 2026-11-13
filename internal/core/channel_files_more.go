package core

// The Files & links tab's other calls (files-links-2; CDN build
// 0e695dd18302, boot-shared 0fb3750ed9646c84 and client-boot, read
// without a cookie; docs/FILES-AUDIT.md "Files & links tab, round 2"):
//
//   - The search's suggestions: ExplorerSearch is the Omniswitcher with
//     useChannelAttachmentsExplorer's searcherOptions (entities.files
//     {channel}); the searcher's remote files source (client-boot QcwJ
//     getRemoteFiles) is NQkX fetchFilesWithQuery: search.autocomplete.files
//     {query: "in:<#C> <words>", include_shares: true}.
//   - Share (FileActions onShare → openShareDialog, the share-dialog chunk
//     zUYy): on open getFileOverviewAndUpdateFile (ut+Q) asks
//     files.getOverview {file_id} for the "{n} users and {n} channels have
//     access" line; Share is shareFile (boot-shared H8G4): files.share
//     {files, channel, blocks, resharing_aware: true,
//     skip_dlp_user_warning: false, from_share_modal: false}.
//   - View link details (LinkEntity's info button → toLinkDetails, the
//     client-link-details-view chunk DEJt): fetchSearchModule (rZ6M e3)
//     search.modules.messages {query: the URL, sort timestamp, sort_dir
//     asc, search_context desktop_link_details}, the module's base args
//     and no session ids.

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// ChannelFileSuggestions is the tab search's remote file suggestions:
// fetchFilesWithQuery with channelId, search.autocomplete.files
// {query: "in:<#C> q", include_shares: true}. Slack's prefix cache
// (containsPrefixOf) is skipped with a channel, so every query asks.
func (a *App) ChannelFileSuggestions(ctx context.Context, ch, query string) ([]File, error) {
	q := strings.TrimSpace(query)
	if q == "" || ch == "" {
		return nil, nil
	}
	var raw json.RawMessage
	p := url.Values{"query": {"in:<#" + ch + "> " + q}, "include_shares": {"true"}}
	if err := a.C.Call(ctx, "search.autocomplete.files", p, &raw); err != nil {
		return nil, err
	}
	var res struct {
		Files []browseFileRaw `json:"files"`
	}
	if err := json.Unmarshal(stripHighlightJSON(raw), &res); err != nil {
		return nil, err
	}
	out := make([]File, 0, len(res.Files))
	for i := range res.Files {
		if !shownFile(&res.Files[i]) {
			continue
		}
		out = append(out, res.Files[i].file())
	}
	return out, nil
}

// FileOverview is files.getOverview's counts (ut+Q puts them on the file
// as share_metadata).
type FileOverview struct {
	Users, Channels, Files          int
	MaxUsers, MaxChannels, MaxFiles bool // max_*_counted: "{count}+"
}

// FileOverview asks files.getOverview {file_id} (getFileOverviewAndUpdateFile,
// reason fetch-file-overview).
func (a *App) FileOverview(ctx context.Context, id string) (FileOverview, error) {
	var r struct {
		Users       int  `json:"users_count"`
		Channels    int  `json:"channels_count"`
		Files       int  `json:"files_count"`
		MaxUsers    bool `json:"max_users_counted"`
		MaxChannels bool `json:"max_channels_counted"`
		MaxFiles    bool `json:"max_files_counted"`
	}
	if err := a.C.Call(ctx, "files.getOverview", url.Values{"file_id": {id}}, &r); err != nil {
		return FileOverview{}, err
	}
	return FileOverview{Users: r.Users, Channels: r.Channels, Files: r.Files,
		MaxUsers: r.MaxUsers, MaxChannels: r.MaxChannels, MaxFiles: r.MaxFiles}, nil
}

// ShareFileTo is the share dialog's Share for a file (shareMessageOrFile
// with shareType file → H8G4 shareFile): files.share {files: [id],
// channel, blocks: the note as rich text ([] without one),
// resharing_aware, skip_dlp_user_warning false, from_share_modal false}.
// ts is the answer's ts or file_msg_ts, where the dialog then navigates.
func (a *App) ShareFileTo(ctx context.Context, id, ch, note string) (ts string, err error) {
	blocks := json.RawMessage("[]")
	if n := strings.TrimSpace(note); n != "" {
		b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": n}}},
		}}})
		blocks = b
	}
	p := url.Values{"files": {id}, "channel": {ch}, "blocks": {string(blocks)},
		"resharing_aware": {"true"}, "skip_dlp_user_warning": {"false"}, "from_share_modal": {"false"}}
	var r struct {
		TS        string `json:"ts"`
		FileMsgTS string `json:"file_msg_ts"`
	}
	if err := a.C.Call(ctx, "files.share", p, &r); err != nil {
		return "", err
	}
	if r.TS != "" {
		return r.TS, nil
	}
	return r.FileMsgTS, nil
}

// LinkShare is one conversation in a link's details: the search item's
// channel and its first message.
type LinkShare struct {
	Channel  string
	Name     string // the item's channel name (search metadata)
	IM, MPIM bool
	Message  Message
}

// linkDetailsParams is fetchSearchModule's args for the link details view.
func linkDetailsParams(link string) url.Values {
	return url.Values{"module": {ModMessages}, "query": {link}, "page": {"1"},
		"extracts": {"1"}, "highlight": {"1"}, "max_extract_len": {"200"}, "extra_message_data": {"1"},
		"no_user_profile": {"1"}, "count": {"20"}, "file_title_only": {"false"}, "query_rewrite_disabled": {"false"},
		"include_files_shares": {"1"}, "search_context": {"desktop_link_details"},
		"sort": {"timestamp"}, "sort_dir": {"asc"}}
}

// LinkShares is the link details view's read: the conversations that
// shared link, oldest first (the first one's first message is "First
// shared by").
func (a *App) LinkShares(ctx context.Context, link string) ([]LinkShare, error) {
	var raw json.RawMessage
	if err := a.C.Call(ctx, "search.modules.messages", linkDetailsParams(link), &raw); err != nil {
		return nil, err
	}
	var res struct {
		Items []searchItemRaw `json:"items"`
	}
	if err := json.Unmarshal(stripHighlightJSON(raw), &res); err != nil {
		return nil, err
	}
	var out []LinkShare
	var users []string
	for _, it := range res.Items {
		if it.Channel.ID == "" || len(it.Messages) == 0 {
			continue
		}
		var m slack.Message
		if json.Unmarshal(it.Messages[0], &m) != nil || m.TS == "" {
			continue
		}
		if it.Channel.Name != "" && !it.Channel.IsIM && !it.Channel.IsMPIM {
			a.S.PutName(it.Channel.ID, it.Channel.Name)
		}
		out = append(out, LinkShare{Channel: it.Channel.ID, Name: it.Channel.Name, IM: it.Channel.IsIM,
			MPIM: it.Channel.IsMPIM, Message: a.message(it.Channel.ID, &m)})
		users = append(users, m.User)
	}
	a.EnsureUsers(ctx, users)
	return out, nil
}

// AddBookmarkFolder is addBookmarkFolderForChannel without a bookmark to
// move in (YQEj callAddBookmarksFolderForChannelId): bookmarks.add
// {channel, title, type: folder}; the folder goes into the store. The Add
// link to folder modal makes the "Bookmarks" folder this way when the
// channel has none.
func (a *App) AddBookmarkFolder(ctx context.Context, ch, title string) (Bookmark, error) {
	var res struct {
		Bookmark bookmarkRaw `json:"bookmark"`
	}
	if err := a.C.Call(ctx, "bookmarks.add", url.Values{"channel": {ch}, "title": {title}, "type": {"folder"}}, &res); err != nil {
		return Bookmark{}, err
	}
	b := a.bookmarkOf(res.Bookmark)
	if b.ID != "" {
		a.putBookmark(ch, b)
	}
	return b, nil
}

// ChannelFolders are the channel's bookmark folders (the store's folder
// bookmarks by rank), loaded or not.
func (a *App) ChannelFolders(ch string) []Bookmark {
	all, _ := a.ChannelBookmarks(ch)
	var out []Bookmark
	for _, b := range all {
		if b.Type == "folder" {
			out = append(out, b)
		}
	}
	return out
}
