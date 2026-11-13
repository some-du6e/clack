package fakeslack

// The Files & links tab's fixtures (Size.Files): #files-gallery's tab bar
// has Slack's files tab ({id: files, type: files}), and the tab's three
// browsers answer from the channel's messages (core/channel_files.go):
//
//   - search.modules.files with search_context desktop_files_channel_tab:
//     the query's in:<#C…> picks the channel, type: / -type: images and
//     videos split the media from the rest (folder matches nothing), the
//     other words match titles. Each file carries the share's timestamp
//     (its message's ts). Besides the gallery's files the channel has a
//     canvas (CanvasShared) and a list (ListKitchen) shared in it. Pages
//     are the request's count.
//   - conversations.searchLinks {channel_id, page, query, sort, sort_dir}:
//     ChannelLinksCount links (a titled one with a favicon, titled ones
//     without, a bare host, a path, one shared twice), ten a page, so the
//     tab has a second page. The page size is the fake's: Slack's isn't
//     visible from the client.
//
// Every call is recorded: search.modules.files in SearchForms like
// search.go's, conversations.searchLinks in ChannelLinksForms. Synthetic
// names and URLs only.

import (
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// FilesTabID is #files-gallery's Files & links tab id (Slack's built-in
// files tab, fY.Files).
const FilesTabID = "files"

// ChannelLinksPerPage is conversations.searchLinks' page size here.
const ChannelLinksPerPage = 10

// ChannelLink is one link shared in #files-gallery.
type ChannelLink struct {
	URL, Title, Icon string
	TS               string
}

// ChannelLinks are #files-gallery's links, newest first (the API's
// default sort).
func (s *Server) ChannelLinks() []ChannelLink {
	ts := func(i int) string { return strconv.Itoa(filesBaseTS+5000-i*300) + ".000200" }
	out := []ChannelLink{
		{URL: "https://roadmap.example.com/2026/q1", Title: "Roadmap: Q1 2026", Icon: s.URL + "img/32x32/fav-roadmap.png"},
		{URL: "https://docs.example.org/guides/onboarding?step=2", Title: "Onboarding guide"},
		{URL: "https://status.example.net/", Title: ""},
		{URL: "https://example.com/blog/launch-week", Title: ""},
		{URL: "https://design.example.com/files/board-7", Title: "Design board", Icon: s.URL + "img/32x32/fav-design.png"},
		{URL: "https://tickets.example.com/browse/OPS-1412", Title: "OPS-1412 Rotate the staging keys"},
		{URL: "https://calendar.example.com/event/standup", Title: "Weekly standup"},
		{URL: "https://wiki.example.org/Release_process", Title: "Release process"},
		{URL: "https://news.example.net/2026/11/fixtures", Title: "A very long headline about fixtures that keeps going well past the width of the row so it truncates"},
		{URL: "https://metrics.example.com/d/latency", Title: "Latency dashboard"},
		{URL: "https://forms.example.com/r/feedback", Title: "Feedback form"},
		{URL: "https://example.org/", Title: ""},
		{URL: "https://videos.example.com/watch/demo", Title: "Demo recording", Icon: s.URL + "img/32x32/fav-video.png"},
		{URL: "https://archive.example.com/old-notes", Title: "Old notes"},
	}
	for i := range out {
		out[i].TS = ts(i)
	}
	return out
}

// ChannelLinksCount is how many links #files-gallery has.
const ChannelLinksCount = 14

type channelFilesLog struct {
	mu    sync.Mutex
	links []url.Values
}

var channelFilesLogs sync.Map // *Server -> *channelFilesLog

func (s *Server) cflog() *channelFilesLog {
	v, _ := channelFilesLogs.LoadOrStore(s, &channelFilesLog{})
	return v.(*channelFilesLog)
}

// ChannelLinksForms are the conversations.searchLinks forms in order.
func (s *Server) ChannelLinksForms() []url.Values {
	l := s.cflog()
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]url.Values(nil), l.links...)
}

// ChannelFilesTab lists the files the tab shows for #files-gallery,
// newest share first: media (images and videos) or the rest. Each map
// is a search item (the file object plus timestamp).
func (s *Server) ChannelFilesTab(media bool) []map[string]any {
	var out []map[string]any
	seen := map[string]bool{}
	add := func(file any, ts int64) {
		b, _ := json.Marshal(file)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		id, _ := m["id"].(string)
		mime, _ := m["mimetype"].(string)
		isMedia := strings.HasPrefix(mime, "image/") || strings.HasPrefix(mime, "video/")
		if id == "" || seen[id] || isMedia != media {
			return
		}
		seen[id] = true
		m["timestamp"] = ts
		m["channels"] = []string{FilesChannelID}
		out = append(out, m)
	}
	var walk func(fx []filesFixture, base func(j int) int64)
	walk = func(fx []filesFixture, base func(j int) int64) {
		for j, c := range fx {
			user := SelfID
			if c.user >= 0 {
				user = s.users[c.user%len(s.users)].ID
			}
			for _, file := range c.files {
				if file.User == "" {
					file.User = user
				}
				add(file, base(j))
			}
		}
	}
	fx := s.filesFixtures()
	walk(fx, func(i int) int64 { return int64(filesBaseTS + i*60) })
	for i, c := range fx {
		i := i
		walk(c.replies, func(j int) int64 { return int64(filesBaseTS + i*60 + 10*(j+1)) })
	}
	if !media {
		// a canvas and a list shared in the channel, after the gallery
		cs := s.canvasStore()
		cs.mu.Lock()
		if c, okc := cs.byID[CanvasShared]; okc {
			add(s.canvasFile(c), int64(filesBaseTS+len(fx)*60+30))
		}
		cs.mu.Unlock()
		ls := s.listStore()
		ls.mu.Lock()
		if l, okl := ls.byID[ListKitchen]; okl {
			add(s.listFile(l), int64(filesBaseTS+len(fx)*60+90))
		}
		ls.mu.Unlock()
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i]["timestamp"].(int64) > out[j]["timestamp"].(int64) })
	return out
}

// handleChannelFiles answers the tab's reads for #files-gallery;
// handled=false passes on.
func (s *Server) handleChannelFiles(method string, f url.Values) (any, bool) {
	if !s.Size.Files {
		return nil, false
	}
	if out, handled := s.handleChannelFilesMore(method, f); handled { // channel_files_more.go
		return out, true
	}
	switch method {
	case "conversations.info":
		if f.Get("channel") != FilesChannelID {
			return nil, false
		}
		c, okc := s.conv(FilesChannelID)
		if !okc {
			return nil, false
		}
		b, _ := json.Marshal(c)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		m["properties"] = map[string]any{"tabs": s.galleryTabsJSON()} // stateful (channel_files_more.go)
		addCounts(m, c, f)
		return ok("channel", m), true
	case "search.modules.files":
		if f.Get("search_context") != "desktop_files_channel_tab" {
			return nil, false
		}
		l := s.slog()
		l.mu.Lock()
		l.forms[method] = append(l.forms[method], f)
		l.mu.Unlock()
		in, media, words := channelFilesQuery(f.Get("query"))
		var items []map[string]any
		if in == FilesChannelID {
			for _, it := range s.ChannelFilesTab(media) {
				title, _ := it["title"].(string)
				name, _ := it["name"].(string)
				if !matchWords(title+" "+name, words) {
					continue
				}
				items = append(items, it)
			}
		}
		if f.Get("sort") == "timestamp" && f.Get("sort_dir") == "asc" {
			for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
				items[i], items[j] = items[j], items[i]
			}
		}
		per, _ := strconv.Atoi(f.Get("count"))
		if per <= 0 {
			per = 20
		}
		page, pages, start, end := pageOf(f, len(items), per)
		return ok("module", "files", "items", append([]map[string]any{}, items[start:end]...),
			"pagination", pagination(len(items), page, pages, per, start, end)), true
	case "conversations.searchLinks":
		l := s.cflog()
		l.mu.Lock()
		l.links = append(l.links, f)
		l.mu.Unlock()
		var items []any
		if f.Get("channel_id") == FilesChannelID {
			words := strings.Fields(strings.ToLower(f.Get("query")))
			links := s.ChannelLinks()
			if f.Get("sort_dir") == "asc" {
				for i, j := 0, len(links)-1; i < j; i, j = i+1, j-1 {
					links[i], links[j] = links[j], links[i]
				}
			}
			for _, k := range links {
				if !matchWords(k.Title+" "+k.URL, words) {
					continue
				}
				it := map[string]any{"url": k.URL, "timestamp": k.TS, "channel_id": FilesChannelID}
				if k.Title != "" {
					it["title"] = k.Title
				} else {
					it["title"] = nil
				}
				if k.Icon != "" {
					it["icon_url"] = k.Icon
				}
				items = append(items, it)
			}
		}
		page, pages, start, end := pageOf(f, len(items), ChannelLinksPerPage)
		if pages == 0 {
			pages = 1
		}
		return ok("items", append([]any{}, items[start:end]...), "pagination", map[string]any{
			"page": page, "page_count": pages, "total_count": len(items), "next_cursor": ""}), true
	}
	return nil, false
}

// channelFilesQuery reads the tab's search query: the in:<#C…> channel,
// whether type:images / type:videos ask for media, and the other words.
func channelFilesQuery(q string) (in string, media bool, words []string) {
	for _, w := range strings.Fields(q) {
		switch {
		case strings.HasPrefix(w, "in:<#"):
			id := strings.TrimSuffix(strings.TrimPrefix(w, "in:<#"), ">")
			if i := strings.IndexByte(id, '|'); i >= 0 {
				id = id[:i]
			}
			in = id
		case w == "type:images" || w == "type:videos":
			media = true
		case strings.HasPrefix(w, "type:") || strings.HasPrefix(w, "-type:"):
		default:
			words = append(words, strings.ToLower(w))
		}
	}
	return
}

func matchWords(s string, words []string) bool {
	s = strings.ToLower(s)
	for _, w := range words {
		if !strings.Contains(s, w) {
			return false
		}
	}
	return true
}
