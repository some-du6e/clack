package demo

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Files: every file shared in the demo's history (pictures, PDFs, the
// canvases and lists posted in channels), for the channel's Files & links
// tab (search.modules.files with search_context desktop_files_channel_tab),
// search's Files results, files.info and the tab's Links
// (conversations.searchLinks). The Files page's canvases and lists are
// the fake's (fakeslack's canvas and list stores hold the demo's own).

// sharedFile is a file and where it was shared.
type sharedFile struct {
	f      slack.File
	ch, ts string
}

// filesLocked is every shared file, newest first, each once (where it
// was first shared).
func (w *World) filesLocked() []sharedFile {
	var out []sharedFile
	seen := map[string]bool{}
	add := func(ch string, m slack.Message) {
		for _, f := range m.Files {
			if f.ID == "" || seen[f.ID] {
				continue
			}
			seen[f.ID] = true
			out = append(out, sharedFile{f: f, ch: ch, ts: m.TS})
		}
	}
	var chans []string
	for ch := range w.hist {
		chans = append(chans, ch)
	}
	sort.Strings(chans)
	for _, ch := range chans {
		ms := w.hist[ch]
		for i := len(ms) - 1; i >= 0; i-- { // oldest first: the first share wins
			add(ch, ms[i])
		}
		for _, rs := range w.replies[ch] {
			for _, r := range rs {
				add(ch, r)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return tsBefore(out[j].ts, out[i].ts) })
	return out
}

// fileItemLocked is a file as search.modules.files and files.info carry
// it: the file, with when and where it was shared.
func (w *World) fileItemLocked(sf sharedFile) map[string]any {
	b, _ := json.Marshal(sf.f)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	m["timestamp"] = tsMicros(sf.ts) / 1e6
	if m["created"] == nil || m["created"] == float64(0) {
		m["created"] = tsMicros(sf.ts) / 1e6
	}
	key := "channels"
	if c, ok := w.convLocked(sf.ch); ok {
		switch {
		case c.IsIM:
			key = "ims"
		case c.IsMPIM || c.IsPrivate:
			key = "groups"
		}
	}
	m[key] = []string{sf.ch}
	m["shares"] = map[string]any{map[bool]string{true: "private", false: "public"}[key != "channels"]: map[string]any{
		sf.ch: []any{map[string]any{"ts": sf.ts, "channel_name": w.convNameLocked(sf.ch), "reply_count": 0}}}}
	m["is_public"] = key == "channels"
	m["file_access"] = "visible"
	return m
}

func (w *World) convNameLocked(id string) string {
	if c, ok := w.convLocked(id); ok {
		return c.Name
	}
	return ""
}

var inChanRE = regexp.MustCompile(`in:<#([A-Z0-9]+)(\|[^>]*)?>`)

// fileQuery is a search.modules.files query, parsed: in:<#C…>, from:,
// type:/-type: and words.
type fileQuery struct {
	in, from        string
	types, notTypes map[string]bool
	words           []string
}

func (w *World) parseFileQuery(q string) fileQuery {
	o := fileQuery{types: map[string]bool{}, notTypes: map[string]bool{}}
	if m := inChanRE.FindStringSubmatch(q); m != nil {
		o.in = m[1]
		q = strings.Replace(q, m[0], " ", 1)
	}
	for _, t := range strings.Fields(q) {
		switch {
		case strings.HasPrefix(t, "-type:"):
			o.notTypes[strings.TrimPrefix(t, "-type:")] = true
		case strings.HasPrefix(t, "type:"):
			o.types[strings.TrimPrefix(t, "type:")] = true
		case strings.HasPrefix(t, "from:"):
			v := strings.Trim(strings.TrimPrefix(t, "from:"), "<>@")
			if p, ok := w.person(v); ok {
				v = p.ID
			}
			o.from = v
		case strings.HasPrefix(t, "in:#"):
			name := strings.TrimPrefix(t, "in:#")
			w.mu.Lock()
			for _, c := range w.convs {
				if c.Name == name {
					o.in = c.ID
				}
			}
			w.mu.Unlock()
		case strings.Contains(t, ":"): // other modifiers (before:, during:…) don't narrow the demo's few files
		default:
			o.words = append(o.words, strings.ToLower(t))
		}
	}
	return o
}

// fileKind is a file's search type: images, videos, pdfs, quip (canvas),
// lists, documents.
func fileKind(f slack.File) string {
	switch {
	case f.Mode == "quip" || f.Filetype == "quip":
		return "quip"
	case f.Mode == "list" || f.Filetype == "list":
		return "lists"
	case strings.HasPrefix(f.Mimetype, "image/"):
		return "images"
	case strings.HasPrefix(f.Mimetype, "video/"):
		return "videos"
	case f.Filetype == "pdf":
		return "pdfs"
	}
	return "documents"
}

// searchFiles answers search.modules.files for the channel tab and the
// search results; the Files page's canvas and list browsers (type:quip,
// type:lists) go on to the fake.
func (w *World) searchFiles(f url.Values) (any, bool) {
	q := f.Get("query")
	tab := f.Get("search_context") == "desktop_files_channel_tab"
	if !tab && (strings.Contains(q, "type:quip") || strings.Contains(q, "type:lists")) {
		return nil, false
	}
	o := w.parseFileQuery(q)
	w.mu.Lock()
	var items []map[string]any
	for _, sf := range w.filesLocked() {
		k := fileKind(sf.f)
		if o.in != "" && sf.ch != o.in {
			continue
		}
		if o.from != "" && sf.f.User != o.from {
			continue
		}
		if len(o.types) > 0 && !o.types[k] {
			continue
		}
		if o.notTypes[k] {
			continue
		}
		text := strings.ToLower(sf.f.Title + " " + sf.f.Name)
		match := true
		for _, wd := range o.words {
			match = match && strings.Contains(text, wd)
		}
		if !match {
			continue
		}
		items = append(items, w.fileItemLocked(sf))
	}
	w.mu.Unlock()
	if f.Get("sort") == "timestamp" && f.Get("sort_dir") == "asc" {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}
	total := len(items)
	count, _ := strconv.Atoi(f.Get("count"))
	if count <= 0 && f.Get("count") != "0" {
		count = 20
	}
	page, _ := strconv.Atoi(f.Get("page"))
	page = max(page, 1)
	pages := 1
	if count > 0 {
		pages = max(1, (total+count-1)/count)
		from := min((page-1)*count, total)
		items = items[from:min(from+count, total)]
	} else {
		items = nil
	}
	if items == nil {
		items = []map[string]any{}
	}
	return okMap("items", items, "pagination", map[string]any{"total_count": total, "page": page, "page_count": pages,
		"per_page": count, "first": 1, "last": total}), true
}

// fileInfo answers files.info for a picture or PDF from the history; the
// canvases and lists are the fake's.
func (w *World) fileInfo(f url.Values) (any, bool) {
	id := f.Get("file")
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, sf := range w.filesLocked() {
		if sf.f.ID != id {
			continue
		}
		if k := fileKind(sf.f); k == "quip" || k == "lists" {
			return nil, false
		}
		return okMap("file", w.fileItemLocked(sf), "comments", []any{}, "response_metadata", map[string]any{"next_cursor": ""}), true
	}
	return nil, false
}

var linkRE = regexp.MustCompile(`<(https?://[^|>]+)(?:\|([^>]*))?>`)

// searchLinks answers conversations.searchLinks: the links in a
// channel's messages, newest first, titled by their unfurl or text.
func (w *World) searchLinks(f url.Values) any {
	ch := f.Get("channel_id")
	q := strings.ToLower(strings.TrimSpace(f.Get("query")))
	w.mu.Lock()
	var all []slack.Message
	all = append(all, w.hist[ch]...)
	for _, rs := range w.replies[ch] {
		all = append(all, rs...)
	}
	w.mu.Unlock()
	sort.SliceStable(all, func(i, j int) bool { return tsBefore(all[j].TS, all[i].TS) })
	items := []map[string]any{}
	seen := map[string]bool{}
	for _, m := range all {
		titles := map[string]string{}
		for _, a := range m.Attachments {
			if a.FromURL != "" {
				titles[a.FromURL] = a.Title
			}
		}
		for _, l := range linkRE.FindAllStringSubmatch(m.Text, -1) {
			u := l[1]
			if seen[u] {
				continue
			}
			title := titles[u]
			if title == "" {
				title = l[2]
			}
			if title == "" {
				title = u
			}
			if q != "" && !strings.Contains(strings.ToLower(title+" "+u), q) {
				continue
			}
			seen[u] = true
			items = append(items, map[string]any{"url": u, "title": title, "timestamp": m.TS, "ts": m.TS, "user": m.User, "channel_id": ch})
		}
	}
	return okMap("items", items, "pagination", map[string]any{"page": 1, "page_count": 1, "total_count": len(items), "next_cursor": ""})
}
