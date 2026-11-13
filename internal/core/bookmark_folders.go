package core

// A channel's bookmark folders: Slack's bookmarks store and its calls, as
// the web client wires them (build 859cadc0e0ab, read from the public CDN
// without a cookie; docs/CHANNELS-AUDIT.md "Bookmark folders"):
//
//   - The store (+Ltq M4: the channel's bookmarks by id, null until they
//     load) is filled by callListBookmarksForChannelId (YQEj XV):
//     bookmarks.list {channel, include_folders: true,
//     include_legacy_workflows: true}, reason "bookmarks-list-api".
//   - A folder tab's rows (M+uF): the channel's bookmarks that
//     shouldShowBookmark keeps (ukRh: links, files, and folders other than
//     an empty "Workflows" one) and aren't folders, whose parentId is the
//     tab's data.folderBookmarkId (with none: no parentId), by rank
//     ascending (ADp7 lx); each a LINK row for a link, else FILE.
//   - Realtime (boot-render): bookmark_added (KI) and bookmark_updated
//     (mK) upsert the event's bookmark (extractBookmark: bookmark → id,
//     link → url, bookmark_type → type, image_url → iconUrl, …);
//     bookmark_removed {channel, bookmark} removes it (l7).
//   - Writes, each its fetcher's exact args: bookmarks.add {channel, link,
//     title, type: link, parent_id} (Kt, reason "bookmarks-add-api"),
//     bookmarks.edit {channel, link, title, bookmark, type, emoji} (h6,
//     "bookmarks-edit-api"), bookmarks.remove {channel, bookmark, type}
//     (N0, "bookmarks-remove-api") and bookmarks.reorder {channel,
//     parent_id, center, left, right, bookmark_type, entity_id} (nK,
//     "bookmarks-reorder-api").
//   - onReorderBookmarkInFolder (mF/7): the moved bookmark takes its new
//     place at once (ADp7 _M: the rank of the row it lands on, that row's
//     rank lengthened by its first letter so it sorts after), then
//     bookmarks.reorder with its new neighbours (left, right) in the
//     folder; the answer's bookmark replaces it and the displaced row's
//     rank comes back (bookmarkToRestore). A failure restores the old
//     order with a toast: "Something went wrong while reordering the
//     link." (file: "…the file."), or "Only channel managers can reorder
//     the link." when the folder is the channel's knowledge folder.

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

type bmState struct {
	mu   sync.Mutex
	chs  map[string]map[string]Bookmark // nil map: loading
	seen map[string]bool
}

var bmStates sync.Map // *App -> *bmState

func (a *App) bmState() *bmState {
	v, _ := bmStates.LoadOrStore(a, &bmState{chs: map[string]map[string]Bookmark{}, seen: map[string]bool{}})
	return v.(*bmState)
}

// bookmarkRaw is bookmarks.list's (and the writes' answers') bookmark.
type bookmarkRaw struct {
	ID          string `json:"id"`
	ChannelID   string `json:"channel_id"`
	Title       string `json:"title"`
	Link        string `json:"link"`
	Emoji       string `json:"emoji"`
	IconURL     string `json:"icon_url"`
	Type        string `json:"type"`
	EntityID    string `json:"entity_id"`
	DateCreated int64  `json:"date_created"`
	Rank        string `json:"rank"`
	ParentID    string `json:"parent_id"`
	ShortcutID  string `json:"shortcut_id"`
}

func (a *App) bookmarkOf(b bookmarkRaw) Bookmark {
	v := Bookmark{ID: b.ID, Title: b.Title, Link: b.Link, Emoji: b.Emoji, Icon: b.IconURL, Type: b.Type,
		EntityID: b.EntityID, Created: b.DateCreated, Rank: b.Rank, ParentID: b.ParentID, ShortcutID: b.ShortcutID}
	if b.Emoji != "" {
		v.EmojiURL, _, _ = a.S.Emoji(strings.Trim(b.Emoji, ":"))
	}
	return v
}

// ChannelBookmarks is the channel's bookmarks store (+Ltq M4): every
// bookmark, folders too, and whether it has loaded. The first read
// fetches it; "bookmarks" events tell when it changes.
func (a *App) ChannelBookmarks(ch string) (all []Bookmark, loaded bool) {
	st := a.bmState()
	st.mu.Lock()
	if !st.seen[ch] {
		st.seen[ch] = true
		st.mu.Unlock()
		go a.fetchChannelBookmarks(ch)
		return nil, false
	}
	m, ok := st.chs[ch]
	if !ok || m == nil {
		st.mu.Unlock()
		return nil, false
	}
	all = make([]Bookmark, 0, len(m))
	for _, b := range m {
		all = append(all, b)
	}
	st.mu.Unlock()
	sortBookmarks(all)
	return all, true
}

func sortBookmarks(bs []Bookmark) {
	// lodash orderBy rank asc (stable over the store's insertion order;
	// ids break the map's ties)
	sort.SliceStable(bs, func(i, j int) bool {
		if bs[i].Rank != bs[j].Rank {
			return bs[i].Rank < bs[j].Rank
		}
		return bs[i].ID < bs[j].ID
	})
}

func (a *App) fetchChannelBookmarks(ch string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var res struct {
		Bookmarks []bookmarkRaw `json:"bookmarks"`
	}
	err := a.C.Call(ctx, "bookmarks.list", bookmarksListArgs(ch), &res)
	st := a.bmState()
	st.mu.Lock()
	if err != nil {
		log.Printf("bookmarks %s: bookmarks.list: %v", ch, err)
		delete(st.seen, ch)
		st.mu.Unlock()
		a.emit(Event{Type: "bookmarks", Channel: ch})
		return
	}
	m := make(map[string]Bookmark, len(res.Bookmarks))
	for _, b := range res.Bookmarks {
		m[b.ID] = a.bookmarkOf(b)
	}
	st.chs[ch] = m
	st.mu.Unlock()
	a.emit(Event{Type: "bookmarks", Channel: ch})
}

// bookmarksListArgs is callListBookmarksForChannelId's args.
func bookmarksListArgs(ch string) url.Values {
	return url.Values{"channel": {ch}, "include_folders": {"true"}, "include_legacy_workflows": {"true"},
		"reason": {"bookmarks-list-api"}}
}

// shouldShowBookmark is ukRh.
func shouldShowBookmark(all []Bookmark, b Bookmark) bool {
	switch b.Type {
	case "link", "file":
		return true
	case "local_folder":
		if b.Title != "Workflows" {
			return true
		}
		return slices.ContainsFunc(all, func(k Bookmark) bool { return k.ParentID == b.ID && shouldShowBookmark(all, k) })
	}
	return false
}

// FolderRows are a folder tab's bookmarks (M+uF): the shown, non-folder
// bookmarks of folder (its folderBookmarkId; "" the channel's loose
// ones) by rank. hasWorkflows: the folder holds a workflow bookmark
// (hasBookmarkedWorkflows).
func FolderRows(all []Bookmark, folder string) (rows []Bookmark, hasWorkflows bool) {
	for _, b := range all {
		if b.ParentID == folder && (b.Type == "shortcut" || b.Type == "auto_gen_shortcut") {
			hasWorkflows = true
		}
		if !shouldShowBookmark(all, b) || b.Type == "local_folder" {
			continue
		}
		if folder == "" && b.ParentID == "" || folder != "" && b.ParentID == folder {
			rows = append(rows, b)
		}
	}
	sortBookmarks(rows)
	return rows, hasWorkflows
}

// bookmarkEvent is handleBookmarkAdded / Updated / Removed.
func (a *App) bookmarkEvent(typ string, raw json.RawMessage) string {
	var e struct {
		Bookmark  json.RawMessage `json:"bookmark"`
		Channel   string          `json:"channel"`
		ImageURL  string          `json:"image_url"`
		Emoji     string          `json:"emoji"`
		Link      string          `json:"link"`
		Title     string          `json:"title"`
		EntityID  string          `json:"entity_id"`
		Rank      string          `json:"rank"`
		Type      string          `json:"bookmark_type"`
		ParentID  string          `json:"parent_id"`
		Shortcut  string          `json:"shortcut_id"`
		DateCreat int64           `json:"date_create"`
	}
	if json.Unmarshal(raw, &e) != nil || e.Channel == "" {
		return ""
	}
	id := looseString(e.Bookmark)
	if id == "" {
		// the gateway's object form: {"bookmark": {id, …}}
		var b bookmarkRaw
		if json.Unmarshal(e.Bookmark, &b) == nil && b.ID != "" {
			id = b.ID
			e.Title, e.Link, e.Emoji, e.ImageURL, e.EntityID, e.Rank, e.Type, e.ParentID =
				b.Title, b.Link, b.Emoji, b.IconURL, b.EntityID, b.Rank, b.Type, b.ParentID
			e.Shortcut, e.DateCreat = b.ShortcutID, b.DateCreated
		}
	}
	if id == "" {
		return ""
	}
	st := a.bmState()
	st.mu.Lock()
	m := st.chs[e.Channel]
	if m == nil { // not loaded: the list answers with it
		st.mu.Unlock()
		return e.Channel
	}
	m = cloneBookmarks(m)
	if typ == "bookmark_removed" {
		delete(m, id)
	} else {
		m[id] = a.bookmarkOf(bookmarkRaw{ID: id, Title: e.Title, Link: e.Link, Emoji: e.Emoji, IconURL: e.ImageURL, Type: e.Type,
			EntityID: e.EntityID, Rank: e.Rank, ParentID: e.ParentID, ShortcutID: e.Shortcut, DateCreated: e.DateCreat})
	}
	st.chs[e.Channel] = m
	st.mu.Unlock()
	a.emit(Event{Type: "bookmarks", Channel: e.Channel})
	return e.Channel
}

func cloneBookmarks(m map[string]Bookmark) map[string]Bookmark {
	out := make(map[string]Bookmark, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// putBookmark upserts b (a write's answer) into ch's store.
func (a *App) putBookmark(ch string, b Bookmark) {
	st := a.bmState()
	st.mu.Lock()
	if m := st.chs[ch]; m != nil {
		m = cloneBookmarks(m)
		m[b.ID] = b
		st.chs[ch] = m
	}
	st.mu.Unlock()
	a.emit(Event{Type: "bookmarks", Channel: ch})
}

func (a *App) dropBookmark(ch, id string) (Bookmark, bool) {
	st := a.bmState()
	st.mu.Lock()
	m := st.chs[ch]
	b, ok := m[id]
	if ok {
		m = cloneBookmarks(m)
		delete(m, id)
		st.chs[ch] = m
	}
	st.mu.Unlock()
	if ok {
		a.emit(Event{Type: "bookmarks", Channel: ch})
	}
	return b, ok
}

// AddFolderLink is the Add link modal's save in a folder
// (callAddBookmarksForChannelId): bookmarks.add {channel, link, title,
// type: link, parent_id}. The answer's bookmark goes into the store.
func (a *App) AddFolderLink(ctx context.Context, ch, parentID, link, title string) (Bookmark, error) {
	p := url.Values{"channel": {ch}, "link": {link}, "title": {title}, "type": {"link"}, "reason": {"bookmarks-add-api"}}
	if parentID != "" {
		p.Set("parent_id", parentID)
	}
	var res struct {
		Bookmark bookmarkRaw `json:"bookmark"`
	}
	if err := a.C.Call(ctx, "bookmarks.add", p, &res); err != nil {
		return Bookmark{}, err
	}
	b := a.bookmarkOf(res.Bookmark)
	if b.ID != "" {
		a.putBookmark(ch, b)
	}
	return b, nil
}

// EditFolderLink is the Edit link modal's save
// (callEditBookmarksForChannelId): bookmarks.edit {channel, link, title,
// bookmark, type, emoji}.
func (a *App) EditFolderLink(ctx context.Context, ch string, b Bookmark, link, title string) (Bookmark, error) {
	typ := b.Type
	if typ == "" {
		typ = "link"
	}
	p := url.Values{"channel": {ch}, "link": {link}, "title": {title}, "bookmark": {b.ID}, "type": {typ}, "emoji": {b.Emoji},
		"reason": {"bookmarks-edit-api"}}
	var res struct {
		Bookmark bookmarkRaw `json:"bookmark"`
	}
	if err := a.C.Call(ctx, "bookmarks.edit", p, &res); err != nil {
		return Bookmark{}, err
	}
	nb := a.bookmarkOf(res.Bookmark)
	if nb.ID == "" {
		nb = b
		nb.Link, nb.Title = link, title
	}
	a.putBookmark(ch, nb)
	return nb, nil
}

// RemoveFolderBookmark is Delete link / Remove file
// (callRemoveBookmarksForChannelId): bookmarks.remove {channel, bookmark,
// type}; the row goes once Slack has said yes.
func (a *App) RemoveFolderBookmark(ctx context.Context, ch string, b Bookmark) error {
	typ := b.Type
	if typ == "" {
		typ = "link"
	}
	if err := a.C.Call(ctx, "bookmarks.remove", url.Values{"channel": {ch}, "bookmark": {b.ID}, "type": {typ},
		"reason": {"bookmarks-remove-api"}}, nil); err != nil {
		return err
	}
	a.dropBookmark(ch, b.ID)
	return nil
}

// ErrReorderFailed is a failed reorder: the rows are back in their old
// order and the UI shows Slack's toast.
var ErrReorderFailed = reorderError("reorder failed")

type reorderError string

func (e reorderError) Error() string { return string(e) }

// ReorderFolderBookmark is onReorderBookmarkInFolder: id moves to row to
// of folder's rows (FolderRows order).
func (a *App) ReorderFolderBookmark(ctx context.Context, ch, folder, id string, to int) error {
	st := a.bmState()
	st.mu.Lock()
	old := st.chs[ch]
	if old == nil {
		st.mu.Unlock()
		return ErrReorderFailed
	}
	all := make([]Bookmark, 0, len(old))
	for _, b := range old {
		all = append(all, b)
	}
	rows, _ := FolderRows(all, folder)
	from := slices.IndexFunc(rows, func(b Bookmark) bool { return b.ID == id })
	if from < 0 || to < 0 || to >= len(rows) || to == from {
		st.mu.Unlock()
		return nil
	}
	// ADp7 _M: the moved row takes the rank of the row at to; moving up,
	// that row's rank grows by its first letter so it sorts just after
	m := cloneBookmarks(old)
	moved := m[id]
	target := rows[to]
	var restore *Bookmark
	if to < from {
		moved.Rank = target.Rank
		t := target
		r := t
		restore = &r
		if t.Rank != "" {
			t.Rank += t.Rank[:1]
		}
		m[t.ID] = t
	} else if target.Rank != "" {
		moved.Rank = target.Rank + target.Rank[:1]
	}
	m[id] = moved
	st.chs[ch] = m
	st.mu.Unlock()
	a.emit(Event{Type: "bookmarks", Channel: ch})

	// the new neighbours in the folder (oQ, lx, then the left and right
	// of the moved one)
	now := make([]Bookmark, 0, len(m))
	for _, b := range m {
		if b.ParentID == moved.ParentID {
			now = append(now, b)
		}
	}
	sortBookmarks(now)
	i := slices.IndexFunc(now, func(b Bookmark) bool { return b.ID == id })
	p := url.Values{"channel": {ch}, "center": {id}, "bookmark_type": {moved.Type}, "reason": {"bookmarks-reorder-api"}}
	if moved.ParentID != "" {
		p.Set("parent_id", moved.ParentID)
	}
	if i > 0 {
		p.Set("left", now[i-1].ID)
	}
	if i >= 0 && i+1 < len(now) {
		p.Set("right", now[i+1].ID)
	}
	if moved.EntityID != "" {
		p.Set("entity_id", moved.EntityID)
	}
	var res struct {
		OK       bool        `json:"ok"`
		Bookmark bookmarkRaw `json:"bookmark"`
	}
	err := a.C.Call(ctx, "bookmarks.reorder", p, &res)
	if err != nil || res.Bookmark.ID == "" {
		st.mu.Lock()
		st.chs[ch] = old
		st.mu.Unlock()
		a.emit(Event{Type: "bookmarks", Channel: ch})
		if err == nil {
			err = ErrReorderFailed
		}
		return err
	}
	a.putBookmark(ch, a.bookmarkOf(res.Bookmark))
	if restore != nil {
		a.putBookmark(ch, *restore)
	}
	return nil
}

// RenameFolderTab is updateTabLabel (Fx4h): conversations.updateTab {id,
// channel_id, label, reason}: "folder_tab_view_header" from the view's
// title, "updateTabLabel" (the default) from the Rename folder modal.
func (a *App) RenameFolderTab(ctx context.Context, ch, tabID, label, reason string) error {
	if reason == "" {
		reason = "updateTabLabel"
	}
	return a.C.Call(ctx, "conversations.updateTab", url.Values{"id": {tabID}, "channel_id": {ch}, "label": {label},
		"reason": {reason}}, nil)
}
