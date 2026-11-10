package demo

import (
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Search over the live history, in the shapes fakeslack's search.go
// answers (search.modules.messages / dms, search.inline, the channel and
// people tabs): a plain substring match per term, with in:, from: and
// is:thread modifiers.

type searchHit struct {
	ch string
	m  slack.Message
}

type searchQuery struct {
	terms, not []string
	in, from   []string
	thread     bool
}

func (w *World) parseQuery(q string) searchQuery {
	var o searchQuery
	ref := func(v string) string {
		v = strings.Trim(v, "<>")
		v = strings.TrimLeft(v, "#@")
		id, _, _ := strings.Cut(v, "|")
		return id
	}
	for _, t := range strings.Fields(q) {
		neg := strings.HasPrefix(t, "-") && len(t) > 1
		if neg {
			t = t[1:]
		}
		if k, v, ok := strings.Cut(t, ":"); ok {
			switch strings.ToLower(k) {
			case "in":
				id := ref(v)
				w.mu.Lock()
				for _, c := range w.convs {
					if c.Name == id {
						id = c.ID
					}
				}
				w.mu.Unlock()
				o.in = append(o.in, id)
				continue
			case "from":
				id := ref(v)
				if strings.EqualFold(id, "me") {
					id = SelfID
				}
				if p, ok := w.person(id); ok {
					id = p.ID
				}
				o.from = append(o.from, id)
				continue
			case "is":
				o.thread = o.thread || strings.EqualFold(v, "thread")
				continue
			case "has", "after", "before", "on", "during", "type", "to", "with", "creator":
				continue
			}
		}
		t = strings.ToLower(strings.Trim(t, `"`))
		if neg {
			o.not = append(o.not, t)
		} else if t != "" {
			o.terms = append(o.terms, t)
		}
	}
	return o
}

func (o searchQuery) match(h searchHit) bool {
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
	if len(o.in) > 0 && !hasStr(o.in, h.ch) {
		return false
	}
	if len(o.from) > 0 && !hasStr(o.from, h.m.User) {
		return false
	}
	if o.thread && h.m.ThreadTS == "" {
		return false
	}
	return len(o.terms) > 0 || len(o.in) > 0 || len(o.from) > 0
}

// corpus is every visible message, newest first.
func (w *World) corpus() []searchHit {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []searchHit
	for _, c := range w.convs {
		for _, m := range w.hist[c.ID] {
			if !m.Hidden && m.Subtype != "tombstone" && m.Subtype != "thread_broadcast" {
				out = append(out, searchHit{c.ID, m})
			}
		}
		for _, rs := range w.replies[c.ID] {
			for _, m := range rs {
				out = append(out, searchHit{c.ID, m})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return tsBefore(out[j].m.TS, out[i].m.TS) })
	return out
}

func hlTerms(text string, terms []string) string {
	low := strings.ToLower(text)
	for _, t := range terms {
		if i := strings.Index(low, t); t != "" && i >= 0 {
			text = text[:i] + "" + text[i:i+len(t)] + "" + text[i+len(t):]
			low = strings.ToLower(text)
		}
	}
	return text
}

func demoPermalink(ch, ts string) string {
	return "https://" + TeamDomain + ".slack.com/archives/" + ch + "/p" + strings.ReplaceAll(ts, ".", "")
}

func (w *World) channelObj(id string) map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	c, _ := w.convLocked(id)
	if c == nil {
		return map[string]any{"id": id}
	}
	return map[string]any{"id": id, "name": c.Name, "is_channel": c.IsChannel, "is_group": c.IsGroup || (c.IsPrivate && !c.IsMPIM),
		"is_im": c.IsIM, "is_mpim": c.IsMPIM, "is_private": c.IsPrivate || c.IsIM || c.IsMPIM, "is_archived": c.IsArchived,
		"is_shared": false, "is_org_shared": false, "is_ext_shared": false, "teams": []string{TeamID}, "user": c.User}
}

func pageRange(f url.Values, n, per int) (page, pages, start, end int) {
	page, _ = strconv.Atoi(f.Get("page"))
	page = max(page, 1)
	if per <= 0 {
		return page, 0, 0, 0
	}
	pages = (n + per - 1) / per
	start = min((page-1)*per, n)
	end = min(start+per, n)
	return
}

func pageInfo(n, page, pages, per, start, end int) map[string]any {
	return map[string]any{"total_count": n, "page": page, "per_page": per, "page_count": pages, "first": start + 1, "last": end}
}

func (w *World) searchMessages(f url.Values, dms bool) any {
	o := w.parseQuery(f.Get("query"))
	var hits []searchHit
	for _, h := range w.corpus() {
		isDM := strings.HasPrefix(h.ch, "D") || strings.HasPrefix(h.ch, "G")
		if (dms && !isDM) || !o.match(h) {
			continue
		}
		if f.Get("search_exclude_bots") == "true" && h.m.BotID != "" {
			continue
		}
		hits = append(hits, h)
	}
	if f.Get("sort") == "timestamp" && f.Get("sort_dir") == "asc" {
		sort.SliceStable(hits, func(i, j int) bool { return tsBefore(hits[i].m.TS, hits[j].m.TS) })
	}
	per, _ := strconv.Atoi(f.Get("count"))
	if per <= 0 {
		per = 20
	}
	page, pages, start, end := pageRange(f, len(hits), per)
	items := []any{}
	inN, fromN := map[string]int{}, map[string]int{}
	for _, h := range hits {
		inN[h.ch]++
		fromN[h.m.User]++
	}
	for _, h := range hits[start:end] {
		text := h.m.Text
		if f.Get("highlight") == "1" {
			text = hlTerms(text, o.terms)
		}
		m := map[string]any{"type": "message", "ts": h.m.TS, "user": h.m.User, "iid": "I" + h.m.TS, "text": text,
			"permalink": demoPermalink(h.ch, h.m.TS), "extracts": []map[string]any{{"text": text, "truncated_head": false, "truncated_tail": false}}}
		if h.m.BotID != "" {
			m["bot_id"], m["bot_profile"] = h.m.BotID, h.m.BotProfile
		}
		if h.m.ThreadTS != "" {
			m["thread_ts"] = h.m.ThreadTS
			if h.m.ThreadTS == h.m.TS {
				m["reply_count"], m["latest_reply"], m["reply_users"] = h.m.ReplyCount, h.m.LatestReply, h.m.ReplyUsers
			}
		}
		if len(h.m.Reactions) > 0 {
			m["reactions"] = h.m.Reactions
		}
		items = append(items, map[string]any{"iid": "I" + h.m.TS, "team": TeamID, "channel": w.channelObj(h.ch), "messages": []any{m}})
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
		out := []map[string]any{}
		for _, id := range ids {
			if len(out) == 5 {
				break
			}
			out = append(out, map[string]any{"id": id, "facet_count": m[id], "checked": hasStr(checked, id)})
		}
		return out
	}
	module := "messages"
	if dms {
		module = "dms"
	}
	return okMap("query", f.Get("query"), "module", module, "filters", map[string]any{}, "items", items,
		"pagination", pageInfo(len(hits), page, pages, per, start, end),
		"filter_suggestions", map[string]any{"in": sugg(inN, o.in), "from": sugg(fromN, o.from)})
}

// searchLegacy answers search.messages (messages.matches).
func (w *World) searchLegacy(f url.Values) any {
	o := w.parseQuery(f.Get("query"))
	var matches []any
	for _, h := range w.corpus() {
		if o.match(h) {
			m := map[string]any{"type": "message", "ts": h.m.TS, "user": h.m.User, "text": h.m.Text, "channel": w.channelObj(h.ch),
				"permalink": demoPermalink(h.ch, h.m.TS)}
			matches = append(matches, m)
		}
	}
	per := 20
	page, pages, start, end := pageRange(f, len(matches), per)
	return okMap("query", f.Get("query"), "messages", map[string]any{"total": len(matches), "matches": append([]any{}, matches[start:end]...),
		"paging": map[string]any{"count": per, "total": len(matches), "page": page, "pages": pages}})
}

func (w *World) searchInline(f url.Values) any {
	o := w.parseQuery(f.Get("query"))
	cnt, _ := strconv.Atoi(f.Get("count"))
	items := []any{}
	n := 0
	for _, h := range w.corpus() {
		if !o.match(h) {
			continue
		}
		n++
		if len(items) >= max(cnt, 1) {
			continue
		}
		items = append(items, map[string]any{"ts": h.m.TS, "user": h.m.User, "channel_id": h.ch, "iid": "I" + h.m.TS,
			"permalink": demoPermalink(h.ch, h.m.TS),
			"extracts":  []map[string]any{{"text": hlTerms(h.m.Text, o.terms), "truncated_head": false, "truncated_tail": false}}})
	}
	return okMap("query", f.Get("query"), "items", items, "pagination", map[string]any{"total_count": n, "page": 1, "per_page": cnt, "page_count": 1})
}

func (w *World) searchChannels(f url.Values) any {
	q := strings.ToLower(strings.TrimSpace(f.Get("query")))
	items := []any{}
	w.mu.Lock()
	for _, c := range w.convs {
		if !c.IsChannel && !c.IsGroup && !(c.IsPrivate && !c.IsMPIM && !c.IsIM) {
			continue
		}
		if q != "" && !strings.Contains(c.Name, q) {
			continue
		}
		purpose := ""
		if c.Purpose != nil {
			purpose = c.Purpose.Value
		}
		items = append(items, map[string]any{"id": c.ID, "name": c.Name, "iid": "I" + c.ID, "is_archived": c.IsArchived,
			"member_count": w.memberCountLocked(c), "is_member": true, "context_team_id": TeamID, "is_private": c.IsPrivate,
			"purpose": map[string]any{"value": purpose, "creator": "", "last_set": 0}, "timestamp": c.Created})
	}
	w.mu.Unlock()
	page, pages, start, end := pageRange(f, len(items), 20)
	return okMap("module", "channels", "items", append([]any{}, items[start:end]...), "pagination", pageInfo(len(items), page, pages, 20, start, end))
}

func (w *World) searchPeople(f url.Values) any {
	q := strings.ToLower(strings.TrimSpace(f.Get("query")))
	items := []any{}
	for _, u := range w.matchUsers(q, false) {
		items = append(items, u)
	}
	page, pages, start, end := pageRange(f, len(items), 20)
	return okMap("module", "people", "items", append([]any{}, items[start:end]...), "pagination", pageInfo(len(items), page, pages, 20, start, end))
}

// matchUsers are the users whose handle or names contain q ("" all),
// sorted by real name; bots only with withBots.
func (w *World) matchUsers(q string, withBots bool) []slack.User {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []slack.User
	for _, u := range w.users {
		if u.IsBot && !withBots {
			continue
		}
		hay := strings.ToLower(u.Name + " " + u.RealName + " " + u.Profile.DisplayName + " " + u.Profile.RealName)
		if q == "" || strings.Contains(hay, q) {
			out = append(out, *u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RealName < out[j].RealName })
	return out
}

// sentList answers messages.sent.list (Drafts & sent: Sent): the
// viewer's messages, newest first, 25 a page; the client hydrates them
// with messages.list.
func (w *World) sentList(f url.Values) any {
	per, _ := strconv.Atoi(f.Get("count"))
	if per <= 0 {
		per = 25
	}
	off, _ := strconv.Atoi(strings.TrimPrefix(f.Get("cursor"), "demo:"))
	var mine []searchHit
	for _, h := range w.corpus() {
		if h.m.User == SelfID {
			mine = append(mine, h)
		}
	}
	off = min(off, len(mine))
	end := min(len(mine), off+per)
	items := []any{}
	for _, h := range mine[off:end] {
		items = append(items, map[string]any{"msg_timestamp": h.m.TS, "ts": h.m.TS, "channel_id": h.ch, "user": SelfID})
	}
	next := ""
	if end < len(mine) {
		next = "demo:" + strconv.Itoa(end)
	}
	page, _ := strconv.Atoi(f.Get("page"))
	return okMap("items", items, "pagination", map[string]any{"total_count": len(mine), "page": max(page, 1), "per_page": per,
		"page_count": (len(mine) + per - 1) / per, "next_cursor": next})
}
