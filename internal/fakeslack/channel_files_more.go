package fakeslack

// The Files & links tab's other calls (Size.Files; core/channel_files_more.go
// and core/channel_tabs_edit.go):
//
//   - search.autocomplete.files with "in:<#C0FILES…> words": the tab's
//     files and media whose title or name has every word (the explorer
//     search's suggestions).
//   - files.getOverview {file_id}: GalleryOverview's counts for any file.
//   - files.share for a file the tab lists, into any conversation:
//     recorded (TabShares) and answered with a file_msg_ts.
//   - search.modules.messages with search_context desktop_link_details:
//     the conversations that shared the link, oldest first: #files-gallery
//     for every link of ChannelLinks, and one more channel for the first
//     (LinkSharedTwice).
//   - #files-gallery's tab bar is stateful: conversations.removeTab hides
//     a built-in tab (is_disabled) and drops a canvas, list or folder tab,
//     conversations.addTab {type} shows it again (or adds a built-in one),
//     conversations.moveTab {id, after} reorders and answers tabz,
//     conversations.updateTab sets a label; files.hasShares answers
//     SetGalleryHasShares. TabWrites records them.
//
// Synthetic names and URLs only.

import (
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// GalleryOverview is files.getOverview's answer here.
var GalleryOverview = map[string]any{"users_count": 6, "channels_count": 2, "files_count": 0,
	"automations_count": 0, "max_users_counted": false, "max_channels_counted": false, "max_files_counted": false}

// LinkSharedTwice is the link LinkShares finds in two conversations.
const LinkSharedTwice = "https://roadmap.example.com/2026/q1"

// TabShare is one files.share of a tab file.
type TabShare struct {
	Args url.Values
	TS   string
}

type cfMore struct {
	mu      sync.Mutex
	folders []map[string]any // #files-gallery's bookmarks (nil: GalleryFolder)
	adds    []url.Values     // its bookmarks.add calls
	tabs    []map[string]any // #files-gallery's properties.tabs (nil: the default)
	writes  []url.Values     // conversations.removeTab / addTab / moveTab / updateTab, files.hasShares, with "method"
	shares  []TabShare
	// hasShares is files.hasShares' answer (false: a file shared only
	// here, so removing its tab asks about access)
	hasShares bool
}

var cfMores sync.Map // *Server -> *cfMore

func (s *Server) cfmore() *cfMore {
	v, _ := cfMores.LoadOrStore(s, &cfMore{})
	return v.(*cfMore)
}

// galleryTabs is #files-gallery's tab list (caller holds mu).
func (m *cfMore) galleryTabs() []map[string]any {
	if m.tabs == nil {
		m.tabs = []map[string]any{{"id": FilesTabID, "type": "files", "label": ""}}
	}
	return m.tabs
}

// SetGalleryTabs replaces #files-gallery's tab bar with built-in tabs of
// these types (id = type), in order.
func (s *Server) SetGalleryTabs(types ...string) {
	m := s.cfmore()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tabs = nil
	for _, t := range types {
		m.tabs = append(m.tabs, map[string]any{"id": t, "type": t, "label": ""})
	}
}

// GalleryTab is one of #files-gallery's tabs for SetGalleryTabList: a
// canvas or list tab carries its file, a folder tab its folder.
func GalleryTab(id, typ, label, fileID, folderID string) map[string]any {
	t := map[string]any{"id": id, "type": typ, "label": label}
	data := map[string]any{}
	if fileID != "" {
		data["file_id"] = fileID
	}
	if folderID != "" {
		data["folder_bookmark_id"] = folderID
	}
	if len(data) > 0 {
		t["data"] = data
	}
	return t
}

// SetGalleryTabList replaces #files-gallery's tab bar with tabs, in order.
func (s *Server) SetGalleryTabList(tabs ...map[string]any) {
	m := s.cfmore()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tabs = tabs
}

// SetGalleryHasShares is files.hasShares' answer for the tab's files.
func (s *Server) SetGalleryHasShares(on bool) {
	m := s.cfmore()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hasShares = on
}

// GalleryTabLabels are #files-gallery's tabs' labels by id.
func (s *Server) GalleryTabLabels() map[string]string {
	m := s.cfmore()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for _, t := range m.galleryTabs() {
		id, _ := t["id"].(string)
		out[id], _ = t["label"].(string)
	}
	return out
}

// GalleryTabs is #files-gallery's tab bar now: each tab's id, with a
// "-" prefix when it is hidden.
func (s *Server) GalleryTabs() []string {
	m := s.cfmore()
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, t := range m.galleryTabs() {
		id, _ := t["id"].(string)
		if d, _ := t["is_disabled"].(bool); d {
			id = "-" + id
		}
		out = append(out, id)
	}
	return out
}

// TabWrites are the tab bar's writes for #files-gallery, in order; each
// form carries its method under "method".
func (s *Server) TabWrites() []url.Values {
	m := s.cfmore()
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]url.Values(nil), m.writes...)
}

// TabShares are the files.share calls for the tab's files.
func (s *Server) TabShares() []TabShare {
	m := s.cfmore()
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]TabShare(nil), m.shares...)
}

func (s *Server) tabFile(id string) map[string]any {
	for _, media := range []bool{true, false} {
		for _, it := range s.ChannelFilesTab(media) {
			if it["id"] == id {
				return it
			}
		}
	}
	return nil
}

// GalleryFolder is #files-gallery's bookmark folder.
const GalleryFolder = "Bk0FILESFOLD1"

// SetGalleryFolders empties #files-gallery's bookmarks (no folders).
func (s *Server) SetGalleryFolders(none bool) {
	m := s.cfmore()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.folders = []map[string]any{}
	if !none {
		m.folders = nil
	}
}

// GalleryBookmarkAdds are #files-gallery's bookmarks.add calls.
func (s *Server) GalleryBookmarkAdds() []url.Values {
	m := s.cfmore()
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]url.Values(nil), m.adds...)
}

func (m *cfMore) galleryBookmarks() []map[string]any {
	if m.folders == nil {
		m.folders = []map[string]any{{"id": GalleryFolder, "channel_id": FilesChannelID, "title": "Launch docs", "link": "",
			"type": "local_folder", "parent_id": "", "rank": "a", "date_created": filesBaseTS, "emoji": "", "icon_url": "", "entity_id": ""}}
	}
	return m.folders
}

func (s *Server) handleChannelFilesMore(method string, f url.Values) (any, bool) {
	switch method {
	case "bookmarks.list", "bookmarks.add":
		if f.Get("channel") != FilesChannelID {
			return nil, false
		}
		m := s.cfmore()
		m.mu.Lock()
		defer m.mu.Unlock()
		bms := m.galleryBookmarks()
		if method == "bookmarks.list" {
			out := make([]any, 0, len(bms))
			for _, b := range bms {
				out = append(out, b)
			}
			return ok("bookmarks", out), true
		}
		m.adds = append(m.adds, withoutToken(f))
		typ := f.Get("type")
		if typ == "folder" {
			typ = "local_folder" // the server stores a new folder as local_folder
		}
		b := map[string]any{"id": "Bk0FILESNEW" + strconv.Itoa(len(m.adds)), "channel_id": FilesChannelID, "title": f.Get("title"),
			"link": f.Get("link"), "type": typ, "entity_id": f.Get("entity_id"), "parent_id": f.Get("parent_id"), "rank": "z" + strconv.Itoa(len(m.adds)),
			"date_created": filesBaseTS + 100, "emoji": "", "icon_url": ""}
		m.folders = append(bms, b)
		return ok("bookmark", b), true
	case "search.autocomplete.files":
		in, _, words := channelFilesQuery(f.Get("query"))
		if in != FilesChannelID {
			return nil, false
		}
		l := s.slog()
		l.mu.Lock()
		l.forms[method] = append(l.forms[method], f)
		l.mu.Unlock()
		var files []any
		for _, media := range []bool{true, false} {
			for _, it := range s.ChannelFilesTab(media) {
				title, _ := it["title"].(string)
				name, _ := it["name"].(string)
				if len(words) > 0 && matchWords(title+" "+name, words) {
					files = append(files, it)
				}
			}
		}
		return ok("files", append([]any{}, files...)), true
	case "files.getOverview":
		if f.Get("file_id") == "" {
			return fail("invalid_arguments"), true
		}
		out := ok()
		for k, v := range GalleryOverview {
			out[k] = v
		}
		return out, true
	case "files.share":
		if s.tabFile(f.Get("files")) == nil {
			return nil, false // an upload (drafts.go)
		}
		m := s.cfmore()
		m.mu.Lock()
		ts := strconv.Itoa(filesBaseTS+90000+len(m.shares)) + ".000100"
		m.shares = append(m.shares, TabShare{Args: withoutToken(f), TS: ts})
		m.mu.Unlock()
		return ok("file_msg_ts", ts), true
	case "search.modules.messages":
		if f.Get("search_context") != "desktop_link_details" {
			return nil, false
		}
		l := s.slog()
		l.mu.Lock()
		l.forms[method] = append(l.forms[method], f)
		l.mu.Unlock()
		link := f.Get("query")
		var items []any
		for i, k := range s.ChannelLinks() {
			if k.URL != link {
				continue
			}
			users := []string{SelfID}
			for _, u := range s.users {
				users = append(users, u.ID)
			}
			msg := func(ts, user string) map[string]any {
				return map[string]any{"type": "message", "ts": ts, "user": user, "text": "Sharing <" + k.URL + ">",
					"permalink": "https://fake.slack.com/archives/" + FilesChannelID + "/p" + strings.ReplaceAll(ts, ".", "")}
			}
			first := map[string]any{"channel": map[string]any{"id": FilesChannelID, "name": "files-gallery", "is_channel": true},
				"messages": []any{msg(k.TS, users[(i+1)%len(users)])}}
			if k.URL == LinkSharedTwice {
				for _, c := range s.convs {
					if c.IsChannel && c.ID != FilesChannelID && !c.IsArchived {
						// the earlier share: oldest first
						early := strconv.Itoa(filesBaseTS-3600) + ".000300"
						items = append(items, map[string]any{"channel": map[string]any{"id": c.ID, "name": c.Name, "is_channel": true},
							"messages": []any{msg(early, users[0])}})
						break
					}
				}
			}
			items = append(items, first)
		}
		return ok("module", "messages", "items", append([]any{}, items...), "pagination",
			map[string]any{"total_count": len(items), "page": 1, "page_count": 1}), true
	case "files.hasShares":
		if f.Get("channel_id") != FilesChannelID {
			return nil, false
		}
		m := s.cfmore()
		m.mu.Lock()
		defer m.mu.Unlock()
		w := withoutToken(f)
		w.Set("method", method)
		m.writes = append(m.writes, w)
		return ok("has_shares", m.hasShares), true
	case "conversations.removeTab", "conversations.addTab", "conversations.moveTab", "conversations.updateTab":
		if f.Get("channel_id") != FilesChannelID || method == "conversations.addTab" && f.Get("data") != "" {
			return nil, false // a canvas or list tab goes to lists_add.go
		}
		m := s.cfmore()
		m.mu.Lock()
		defer m.mu.Unlock()
		w := withoutToken(f)
		w.Set("method", method)
		m.writes = append(m.writes, w)
		tabs := m.galleryTabs()
		find := func(id string) int {
			for i, t := range tabs {
				if t["id"] == id || t["type"] == id {
					return i
				}
			}
			return -1
		}
		switch method {
		case "conversations.removeTab":
			i := find(f.Get("id"))
			if i < 0 {
				return fail("tab_not_found"), true
			}
			switch tabs[i]["type"] {
			case "canvas", "list", "folder": // gone; a built-in one is only hidden
				m.tabs = append(append([]map[string]any{}, tabs[:i]...), tabs[i+1:]...)
			default:
				tabs[i]["is_disabled"] = true
			}
		case "conversations.updateTab":
			i := find(f.Get("id"))
			if i < 0 {
				return fail("tab_not_found"), true
			}
			tabs[i]["label"] = f.Get("label")
		case "conversations.addTab":
			typ := f.Get("type")
			if i := find(typ); i >= 0 {
				delete(tabs[i], "is_disabled")
			} else {
				m.tabs = append(tabs, map[string]any{"id": typ, "type": typ, "label": ""})
			}
		case "conversations.moveTab":
			i := find(f.Get("id"))
			if i < 0 {
				return fail("tab_not_found"), true
			}
			t := tabs[i]
			rest := append(append([]map[string]any{}, tabs[:i]...), tabs[i+1:]...)
			at := 0
			if a := f.Get("after"); a != "" {
				for j, x := range rest {
					if x["id"] == a {
						at = j + 1
					}
				}
			}
			m.tabs = append(append(append([]map[string]any{}, rest[:at]...), t), rest[at:]...)
		}
		out := make([]any, 0, len(m.tabs))
		for _, t := range m.tabs {
			out = append(out, t)
		}
		return ok("tabz", out), true
	}
	return nil, false
}

// galleryTabsJSON is #files-gallery's properties.tabs.
func (s *Server) galleryTabsJSON() []any {
	m := s.cfmore()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []any{}
	for _, t := range m.galleryTabs() {
		c := map[string]any{}
		for k, v := range t {
			c[k] = v
		}
		out = append(out, c)
	}
	return out
}

// withoutToken is a copy of f without the auth token (the API's, not the
// method's args).
func withoutToken(f url.Values) url.Values {
	out := url.Values{}
	for k, v := range f {
		if k != "token" {
			out[k] = append([]string(nil), v...)
		}
	}
	return out
}
