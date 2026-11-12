package fakeslack

// Search: search.modules.messages / dms / files / channels / people,
// search.inline, search.autocomplete.files and search.save in the shapes
// app.slack.com answers (key names and types captured 2026-11-08; no
// content). Every result is synthetic.
//
// Fixtures: SearchWord ("zephyr") is in SearchFixtureCount fixed
// messages across three channels and a DM, so the results page has two
// pages, threads, a link, a message from the viewer and one in a DM. Any
// other query matches the generated histories of the first channels.

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

const (
	// SearchWord is in every search fixture message.
	SearchWord = "zephyr"
	// SearchFixtureCount is how many fixture messages contain SearchWord.
	SearchFixtureCount = 27
	// SearchNothing matches nothing anywhere.
	SearchNothing = "qqxnothingqq"
	// SearchFileTitle and SearchFileID are the file fixture.
	SearchFileTitle = "Zephyr launch checklist"
	SearchFileID    = "F0FAKESRCH1"
	// SearchPeopleWord is in most fixture people's names (the People
	// tab's filter tests).
	SearchPeopleWord = "a"
)

// SearchLocations are the People tab's locations: fixture user i lives
// in SearchLocations[i%3] (profiles have no location field here).
var SearchLocations = []string{"Brooklyn, NY", "London", "Remote"}

// channelTypeMatch is search.modules.channels' channel_type and
// exclude_my_channels / search_only_my_channels for a channel.
func channelTypeMatch(c *slack.Conversation, f url.Values) bool {
	if f.Get("exclude_my_channels") == "1" && c.IsMember {
		return false
	}
	if f.Get("search_only_my_channels") == "true" && !c.IsMember {
		return false
	}
	switch f.Get("channel_type") {
	case "private_exclude":
		return !c.IsPrivate
	case "private":
		return c.IsPrivate
	case "archived":
		return c.IsArchived
	case "external_shared":
		return c.IsExtShared
	}
	return true
}

// facetList is a filter_suggestions list: {value, facet_count, checked},
// most first, ten at most (max_filter_suggestions).
func facetList(counts map[string]int, checked []string) []any {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		if k != "" {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > 10 {
		keys = keys[:10]
	}
	out := make([]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{"value": k, "facet_count": counts[k], "checked": containsStr(checked, k)})
	}
	return out
}

// searchLog records search calls' forms (SearchForms).
type searchLog struct {
	mu    sync.Mutex
	forms map[string][]url.Values
	saved []string
	// hist is the search history search.autocomplete answers: search.save
	// adds to it (or bumps a query's count), search.delete drops a query.
	hist []savedSearch
}

// savedSearch is one search history entry.
type savedSearch struct {
	terms, typ string
	ts         float64
	count      int
}

// searchHash is the history entry's hash (search.autocomplete use_hash).
func searchHash(terms string) string {
	h := uint32(2166136261)
	for i := 0; i < len(terms); i++ {
		h = (h ^ uint32(terms[i])) * 16777619
	}
	return fmt.Sprintf("%08x", h)
}

// SetSearchHistory replaces the search history (oldest first); every
// entry is type "default".
func (s *Server) SetSearchHistory(terms ...string) {
	l := s.slog()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hist = nil
	base := float64(time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC).Unix())
	for i, t := range terms {
		l.hist = append(l.hist, savedSearch{terms: t, typ: "default", ts: base + float64(i)*60, count: 1})
	}
}

var searchLogs sync.Map // *Server -> *searchLog

func (s *Server) slog() *searchLog {
	v, _ := searchLogs.LoadOrStore(s, &searchLog{forms: map[string][]url.Values{}})
	return v.(*searchLog)
}

// SearchForms returns the forms of every call to method (a search.*
// method) in order.
func (s *Server) SearchForms(method string) []url.Values {
	l := s.slog()
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]url.Values(nil), l.forms[method]...)
}

// SavedSearches are search.save's terms in order.
func (s *Server) SavedSearches() []string {
	l := s.slog()
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.saved...)
}

// searchHit is a message with where it is.
type searchHit struct {
	ch string
	m  slack.Message
}

// searchFixtures are the SearchWord messages, newest first.
func (s *Server) searchFixtures() []searchHit {
	base := time.Date(2026, 11, 6, 16, 0, 0, 0, time.UTC).Unix()
	var chans []string
	for _, c := range s.convs {
		if c.IsChannel && !c.IsPrivate && len(chans) < 3 {
			chans = append(chans, c.ID)
		}
	}
	dm := ""
	for _, c := range s.convs {
		if c.IsIM {
			dm = c.ID
			break
		}
	}
	lines := []string{
		"the " + SearchWord + " rollout is done, thanks everyone",
		"has anyone tried the new " + SearchWord + " build on Linux?",
		"notes from the " + SearchWord + " sync are in the canvas",
		SearchWord + " ships on Friday if CI stays green",
		"I wrote up how " + SearchWord + " handles retries",
	}
	var out []searchHit
	for i := 0; i < SearchFixtureCount; i++ {
		ch := chans[i%len(chans)]
		u := s.users[1+i%min(len(s.users)-1, 9)].ID
		if i == 4 {
			u = SelfID
		}
		ts := fmt.Sprintf("%d.%06d", base-int64(i)*5400, 100+i)
		text := lines[i%len(lines)]
		m := slack.Message{Type: "message", TS: ts, User: u, Text: text}
		switch {
		case i == 2:
			m.Text += " <https://example.com/" + SearchWord + "|the doc>"
		case i%7 == 1:
			m.ThreadTS, m.ReplyCount, m.LatestReply = ts, 3+i%5, fmt.Sprintf("%d.000200", base-int64(i)*5400+900)
			m.ReplyUsers = []string{s.users[2].ID, s.users[3].ID}
		case i == 5 && dm != "":
			ch = dm
		}
		out = append(out, searchHit{ch, m})
	}
	return out
}

// sq is a parsed search query.
type sq struct {
	terms, not          []string
	in, from            []string // ids
	thread, link, exact bool
	after, before, on   string
}

func parseSQ(q string) sq {
	var o sq
	var toks []string
	var b strings.Builder
	inQ := false
	for _, r := range q {
		switch {
		case r == '"':
			inQ = !inQ
		case r == ' ' && !inQ:
			if b.Len() > 0 {
				toks = append(toks, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		toks = append(toks, b.String())
	}
	ref := func(v string) string {
		v = strings.Trim(v, "<>")
		v = strings.TrimLeft(v, "#@")
		id, _, _ := strings.Cut(v, "|")
		return id
	}
	for _, t := range toks {
		neg := strings.HasPrefix(t, "-") && len(t) > 1
		if neg {
			t = t[1:]
		}
		k, v, isMod := strings.Cut(t, ":")
		if isMod && !strings.Contains(k, " ") {
			switch strings.ToLower(k) {
			case "in":
				o.in = append(o.in, ref(v))
				continue
			case "from":
				if strings.EqualFold(v, "me") {
					v = SelfID
				}
				o.from = append(o.from, ref(v))
				continue
			case "is":
				o.thread = o.thread || strings.EqualFold(v, "thread")
				continue
			case "has":
				o.link = o.link || strings.EqualFold(v, "link")
				continue
			case "after":
				o.after = v
				continue
			case "before":
				o.before = v
				continue
			case "on", "during":
				o.on = v
				continue
			case "type", "to", "with", "creator":
				continue
			}
		}
		if neg {
			o.not = append(o.not, strings.ToLower(t))
		} else {
			o.terms = append(o.terms, strings.ToLower(t))
		}
	}
	return o
}

func (o sq) match(h searchHit) bool {
	text := strings.ToLower(h.m.Text)
	for _, t := range o.terms {
		if !strings.Contains(text, t) {
			return false
		}
	}
	for _, t := range o.not {
		if strings.Contains(text, t) {
			return false
		}
	}
	if len(o.in) > 0 && !containsStr(o.in, h.ch) {
		return false
	}
	if len(o.from) > 0 && !containsStr(o.from, h.m.User) {
		return false
	}
	if o.thread && h.m.ThreadTS == "" {
		return false
	}
	if o.link && !strings.Contains(h.m.Text, "<http") {
		return false
	}
	day := time.Unix(int64(atof(h.m.TS)), 0).UTC().Format("2006-01-02")
	if o.after != "" && day <= o.after {
		return false
	}
	if o.before != "" && day >= o.before {
		return false
	}
	if o.on != "" && !strings.HasPrefix(day, o.on) {
		return false
	}
	return len(o.terms) > 0 || len(o.in) > 0 || len(o.from) > 0
}

func containsStr(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func atof(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }

// searchCorpus is the fixtures, then the first channels' histories.
func (s *Server) searchCorpus(withHistory bool) []searchHit {
	out := s.searchFixtures()
	if !withHistory {
		return out
	}
	n := 0
	for _, c := range s.convs {
		if !c.IsChannel || n == 8 {
			continue
		}
		n++
		s.histMu.Lock()
		msgs, okc := s.hist[c.ID]
		if !okc {
			msgs = s.history(c.ID)
			s.hist[c.ID] = msgs
		}
		s.histMu.Unlock()
		for _, m := range msgs {
			out = append(out, searchHit{c.ID, m})
		}
	}
	return out
}

// hl wraps the query terms in Slack's highlight markers.
func hl(text string, terms []string) string {
	low := strings.ToLower(text)
	for _, t := range terms {
		if t == "" {
			continue
		}
		if i := strings.Index(low, t); i >= 0 {
			text = text[:i] + "" + text[i:i+len(t)] + "" + text[i+len(t):]
			low = strings.ToLower(text)
		}
	}
	return text
}

func pageOf(f url.Values, n, per int) (page, pages, start, end int) {
	page, _ = strconv.Atoi(f.Get("page"))
	page = max(page, 1)
	pages = (n + per - 1) / per
	if per <= 0 {
		return page, 0, 0, 0
	}
	start = min((page-1)*per, n)
	end = min(start+per, n)
	return
}

func pagination(n, page, pages, per, start, end int) map[string]any {
	return map[string]any{"total_count": n, "page": page, "per_page": per, "page_count": pages, "first": start + 1, "last": end}
}

func (s *Server) channelObj(id string) map[string]any {
	c, _ := s.conv(id)
	return map[string]any{"id": id, "name": c.Name, "is_channel": c.IsChannel, "is_group": c.IsPrivate, "is_im": c.IsIM,
		"is_mpim": c.IsMPIM, "is_private": c.IsPrivate || c.IsIM || c.IsMPIM, "is_archived": c.IsArchived,
		"is_shared": false, "is_org_shared": false, "is_ext_shared": false, "teams": []string{TeamID}, "user": c.User}
}

func (s *Server) searchMessages(f url.Values, dms bool) any {
	o := parseSQ(f.Get("query"))
	var hits []searchHit
	for _, h := range s.searchCorpus(len(s.convs) <= 80) {
		isDM := strings.HasPrefix(h.ch, "D") || strings.HasPrefix(h.ch, "G")
		if (dms && !isDM) || !o.match(h) { // the messages tab has DMs too
			continue
		}
		if f.Get("search_exclude_bots") == "true" && h.m.BotID != "" {
			continue
		}
		hits = append(hits, h)
	}
	if f.Get("sort") == "timestamp" {
		asc := f.Get("sort_dir") == "asc"
		sort.SliceStable(hits, func(i, j int) bool {
			if asc {
				return atof(hits[i].m.TS) < atof(hits[j].m.TS)
			}
			return atof(hits[i].m.TS) > atof(hits[j].m.TS)
		})
	}
	per, _ := strconv.Atoi(f.Get("count"))
	if per <= 0 {
		per = 20
	}
	page, pages, start, end := pageOf(f, len(hits), per)
	items := []any{}
	inN, fromN := map[string]int{}, map[string]int{}
	for _, h := range hits {
		inN[h.ch]++
		fromN[h.m.User]++
	}
	for _, h := range hits[start:end] {
		link := "https://fake.slack.com/archives/" + h.ch + "/p" + strings.ReplaceAll(h.m.TS, ".", "")
		text := h.m.Text
		if f.Get("highlight") == "1" {
			text = hl(text, o.terms)
		}
		m := map[string]any{"ts": h.m.TS, "user": h.m.User, "iid": "I" + h.m.TS, "text": text, "permalink": link,
			"extracts": []map[string]any{{"text": text, "truncated_head": false, "truncated_tail": false}}}
		if h.m.ThreadTS != "" {
			m["thread_ts"], m["reply_count"], m["latest_reply"] = h.m.ThreadTS, h.m.ReplyCount, h.m.LatestReply
			m["reply_users"] = h.m.ReplyUsers
		}
		items = append(items, map[string]any{"iid": "I" + h.m.TS, "team": TeamID, "channel": s.channelObj(h.ch),
			"messages": []any{m}})
	}
	sugg := func(m map[string]int, checked []string) []map[string]any {
		var ids []string
		for id := range m {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			if m[ids[i]] != m[ids[j]] {
				return m[ids[i]] > m[ids[j]]
			}
			return ids[i] < ids[j]
		})
		var out []map[string]any
		for _, id := range ids {
			if len(out) == 5 {
				break
			}
			out = append(out, map[string]any{"id": id, "facet_count": m[id], "checked": containsStr(checked, id)})
		}
		return out
	}
	module := "messages"
	if dms {
		module = "dms"
	}
	out := ok("query", f.Get("query"), "module", module, "filters", map[string]any{}, "items", items,
		"pagination", pagination(len(hits), page, pages, per, start, end),
		"filter_suggestions", map[string]any{"in": sugg(inN, o.in), "from": sugg(fromN, o.from)})
	// nothing with the in:/from: filters, something without them: Slack's
	// relaxation (unfiltered_result_count, unfiltered_result_filter)
	if len(hits) == 0 && !dms && (len(o.in) > 0 || len(o.from) > 0) {
		bare := o
		bare.in, bare.from = nil, nil
		n := 0
		for _, h := range s.searchCorpus(len(s.convs) <= 80) {
			if len(bare.terms) > 0 && bare.match(h) {
				n++
			}
		}
		if n > 0 {
			out["unfiltered_result_count"] = n
			out["unfiltered_result_filter"] = map[string]any{"in": append([]string{}, o.in...), "from": append([]string{}, o.from...)}
		}
	}
	return out
}

// sortItems orders the Channels and People tabs' items the way their
// Sort select asks (name, member_count; asc or desc); relevance keeps
// the fake's order.
func sortItems(items []any, by, dir string, key func(any) (string, int)) {
	less := func(i, j int) bool {
		ni, ci := key(items[i])
		nj, cj := key(items[j])
		switch by {
		case "name":
			return ni < nj
		case "member_count":
			return ci < cj
		}
		return false
	}
	if by != "name" && by != "member_count" {
		return
	}
	sort.SliceStable(items, func(i, j int) bool {
		if dir == "desc" {
			return less(j, i)
		}
		return less(i, j)
	})
}

// searchFile is the file fixture.
func (s *Server) searchFile() map[string]any {
	ch := ""
	for _, c := range s.convs {
		if c.IsChannel {
			ch = c.ID
			break
		}
	}
	return map[string]any{"id": SearchFileID, "created": 1762440000, "timestamp": 1762440000, "updated": 1762440000,
		"name": "zephyr-checklist.pdf", "title": SearchFileTitle, "mimetype": "application/pdf", "filetype": "pdf",
		"pretty_type": "PDF", "user": s.users[1].ID, "size": 48213, "mode": "hosted",
		"url_private": s.URL + "files-pri/" + TeamID + "-" + SearchFileID + "/zephyr-checklist.pdf",
		"permalink":   "https://fake.slack.com/files/" + s.users[1].ID + "/" + SearchFileID + "/zephyr-checklist.pdf",
		"channels":    []string{ch}, "groups": []string{}, "ims": []string{}}
}

// handleSearch answers the search methods; handled=false passes on.
// Search feedback (feedback.submitEnterpriseSearchFeedback, search.feedback)
// is only recorded (SearchForms): tests check the call, nothing is sent
// anywhere.
func (s *Server) handleSearch(method string, f url.Values) (any, bool) {
	if !strings.HasPrefix(method, "search.") && method != "feedback.submitEnterpriseSearchFeedback" {
		return nil, false
	}
	l := s.slog()
	l.mu.Lock()
	l.forms[method] = append(l.forms[method], f)
	l.mu.Unlock()
	switch method {
	case "search.modules.messages":
		return s.searchMessages(f, false), true
	case "search.modules.dms":
		return s.searchMessages(f, true), true
	case "search.modules.files": // canvases (type:quip) are canvas.go's
		o := parseSQ(f.Get("query"))
		var items []any
		if len(o.terms) > 0 && strings.Contains(strings.ToLower(SearchFileTitle), o.terms[0]) {
			items = append(items, s.searchFile())
		}
		n := len(items)
		if f.Get("count") == "0" {
			items = []any{}
		}
		return ok("items", items, "pagination", map[string]any{"total_count": n, "page": 1, "page_count": min(n, 1)}), true
	case "search.modules.channels":
		if f.Get("search_context") != "desktop_channels_tab" {
			return nil, false // the channel browser's (channels.go)
		}
		q := strings.ToLower(strings.TrimSpace(f.Get("query")))
		var items []any
		for _, c := range s.convs {
			if !c.IsChannel || q == "" || !strings.Contains(c.Name, q) || !channelTypeMatch(&c, f) {
				continue
			}
			items = append(items, map[string]any{"id": c.ID, "name": c.Name, "iid": "I" + c.ID, "is_archived": c.IsArchived,
				"member_count": c.NumMembers, "is_member": c.IsMember, "context_team_id": TeamID, "is_private": c.IsPrivate,
				"purpose": map[string]any{"value": c.Purpose.Value, "creator": "", "last_set": 0}, "timestamp": c.Created})
		}
		sortItems(items, f.Get("sort"), f.Get("sort_dir"), func(x any) (string, int) {
			m := x.(map[string]any)
			return m["name"].(string), m["member_count"].(int)
		})
		page, pages, start, end := pageOf(f, len(items), 20)
		return ok("module", "channels", "items", append([]any{}, items[start:end]...),
			"pagination", pagination(len(items), page, pages, 20, start, end)), true
	case "search.modules.people":
		if f.Get("search_context") != "desktop_people_tab" {
			return nil, false
		}
		q := strings.ToLower(strings.TrimSpace(f.Get("query")))
		titles, locs := splitList(f.Get("profile_title")), splitList(f.Get("profile_location"))
		var items []any
		tc, lc := map[string]int{}, map[string]int{}
		for i, u := range s.users {
			if q == "" || !(strings.Contains(u.Name, q) || strings.Contains(strings.ToLower(u.RealName), q)) {
				continue
			}
			loc := SearchLocations[i%len(SearchLocations)]
			if (len(titles) > 0 && !containsStr(titles, u.Profile.Title)) || (len(locs) > 0 && !containsStr(locs, loc)) {
				continue
			}
			items = append(items, u)
			tc[u.Profile.Title]++
			lc[loc]++
		}
		sortItems(items, f.Get("sort"), f.Get("sort_dir"), func(x any) (string, int) { return x.(slack.User).Name, 0 })
		page, pages, start, end := pageOf(f, len(items), 20)
		return ok("module", "people", "items", append([]any{}, items[start:end]...),
			"pagination", pagination(len(items), page, pages, 20, start, end),
			"filter_suggestions", map[string]any{"title": facetList(tc, titles), "location": facetList(lc, locs)}), true
	case "search.inline":
		if f.Get("find_in_thread") == "true" {
			return s.findInThread(f), true
		}
		o := parseSQ(f.Get("query"))
		var items []any
		n := 0
		cnt, _ := strconv.Atoi(f.Get("count"))
		dm := ""
		if u := f.Get("user"); u != "" { // in:@user: the DM with them
			for _, c := range s.convs {
				if c.IsIM && c.User == u {
					dm = c.ID
				}
			}
		}
		for _, h := range s.searchCorpus(len(s.convs) <= 80) {
			if !o.match(h) || (f.Get("channel") != "" && h.ch != f.Get("channel")) || (f.Get("user") != "" && h.ch != dm) {
				continue
			}
			n++
			if len(items) >= max(cnt, 1) {
				continue
			}
			text := h.m.Text
			head := false
			if len(text) > 110 {
				text, head = text[len(text)-110:], true
			}
			items = append(items, map[string]any{"ts": h.m.TS, "user": h.m.User, "channel_id": h.ch, "iid": "I" + h.m.TS,
				"permalink": "https://fake.slack.com/archives/" + h.ch + "/p" + strings.ReplaceAll(h.m.TS, ".", ""),
				"extracts":  []map[string]any{{"text": hl(text, o.terms), "truncated_head": head, "truncated_tail": false}}})
		}
		return ok("query", f.Get("query"), "items", items, "pagination", map[string]any{"total_count": n, "page": 1,
			"per_page": cnt, "page_count": 1}), true
	case "search.autocomplete.files":
		q := strings.ToLower(strings.TrimSpace(f.Get("query")))
		var files []any
		if q != "" && strings.Contains(strings.ToLower(SearchFileTitle), q) {
			files = append(files, s.searchFile())
		}
		cs := s.canvasStore()
		cs.mu.Lock()
		for _, id := range cs.order {
			if c := cs.byID[id]; q != "" && strings.Contains(strings.ToLower(c.Title), q) {
				files = append(files, s.canvasFile(c))
			}
		}
		cs.mu.Unlock()
		return ok("files", append([]any{}, files...)), true
	case "search.save":
		l.mu.Lock()
		t := f.Get("terms")
		l.saved = append(l.saved, t)
		at := -1
		for i, h := range l.hist {
			if h.terms == t {
				at = i
			}
		}
		now := float64(time.Now().Unix())
		if at >= 0 {
			h := l.hist[at]
			h.count++
			h.ts = now
			l.hist = append(append(l.hist[:at:at], l.hist[at+1:]...), h)
		} else {
			l.hist = append(l.hist, savedSearch{terms: t, typ: f.Get("type"), ts: now, count: 1})
		}
		l.mu.Unlock()
		return ok(), true
	case "search.delete":
		l.mu.Lock()
		keep := l.hist[:0:0]
		for _, h := range l.hist {
			if (f.Get("hash") != "" && searchHash(h.terms) == f.Get("hash")) || (f.Get("terms") != "" && h.terms == f.Get("terms")) {
				continue
			}
			keep = append(keep, h)
		}
		l.hist = keep
		l.mu.Unlock()
		return ok(), true
	case "search.autocomplete": // the search history (Slack's searchAutocomplete fetcher)
		lim, _ := strconv.Atoi(f.Get("limit"))
		if lim <= 0 {
			lim = 10
		}
		q := strings.ToLower(f.Get("query"))
		l.mu.Lock()
		sugg := []any{}
		for i := len(l.hist) - 1; i >= 0 && len(sugg) < lim; i-- {
			h := l.hist[i]
			if q != "" && !strings.HasPrefix(strings.ToLower(h.terms), q) {
				continue
			}
			typ := h.typ
			if typ == "" {
				typ = "default"
			}
			e := map[string]any{"text": h.terms, "count": h.count, "ts": h.ts, "type": typ}
			if f.Get("use_hash") == "true" {
				e["hash"] = searchHash(h.terms)
			}
			sugg = append(sugg, e)
		}
		l.mu.Unlock()
		return ok("suggestions", sugg), true
	case "search.autocomplete.triggers":
		return ok("triggers", []any{}), true
	case "feedback.submitEnterpriseSearchFeedback", "search.feedback":
		return ok(), true
	}
	return nil, false
}
