package popups

// The search box's completers, as Slack's omniswitcher input has them
// (CDN build 4d0c78085232):
//
//   - the search-filter completer (gantry-v2-async-client-boot-shared
//     ltuT, wired into the box by dG9z BaseSearchInputWithAutocomplete):
//     a modifier being typed before the caret (from:, -in:, is:, …) opens a
//     menu of what it takes: people for from:, to:, with:, creator: and
//     dm:; "Activity", channels and people for in:; Slack's value lists
//     for is:, has:, type:, during: and threads: (before:, after: and on:
//     too while omni_date_quick_filters is off; on, its date picker takes
//     them); emoji after has: and hasmy:;
//   - Texty's @ and # completers (OmniswitcherContent passes
//     completeOnMembers and completeOnChannels to the box).
//
// A pick replaces the typed text with a token and a space: a filter
// (formatMemberInsertData, formatChannelInsertData,
// formatUnifiedFilterInsertData: "from:@Karim", which the search request
// gets as "from:<@U…>", d81O's MemberFilterQuerySuggestionToken) or a
// mention ("@Karim", "<@U…>"). While a menu is open the suggestion list
// and its footer hide (OmniswitcherContent: the autocomplete container is
// --hidden while isTextyAutocompleteOpen).

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/some-du6e/clack/internal/acrank"
	"github.com/some-du6e/clack/internal/core"
)

// srFilterTypes is fsda's Wp in its order: getFilterRegexes tries them so.
var srFilterTypes = [...]string{"from", "to", "with", "in", "is", "has", "hasmy", "creator", "type",
	"before", "after", "on", "during", "dm", "threads"}

// srFilterDateTypes is fsda's Xy, the modifiers getMatch skips while
// omni_date_quick_filters is on (its date picker takes them).
var srFilterDateTypes = map[string]bool{"before": true, "after": true, "on": true}

// srACKind is which completer a match opens.
type srACKind uint8

const (
	srACNone    srACKind = iota
	srACFilter           // ltuT's search-filter completer
	srACMember           // Texty's @ completer
	srACChannel          // Texty's # completer
)

// srACMatch is a completer's match in the box (rune offsets into the text).
type srACMatch struct {
	kind   srACKind
	typ    string // srACFilter: the filter type ("from")
	mod    string // srACFilter: the modifier as typed ("from:", "-From:")
	start  int    // where a pick's text goes (matchData.index; the @ or #)
	anchor int    // where the menu points (the value's start; the @ or #)
	end    int    // the caret
	match  string // the matched text (matchData.match, "@kar")
	value  string // what the completer searches (normalizeQuery)
}

// key identifies the match for Escape's dismissal.
func (m srACMatch) key() string {
	return string(rune('0'+m.kind)) + ":" + m.typ + ":" + m.mod + ":" + m.value
}

// srJSSpace is JavaScript's \s.
func srJSSpace(r rune) bool { return r != 0x85 && (unicode.IsSpace(r) || r == 0xfeff) }

// srMentionTrig is buildModifierRegex's [#＃@＠].
func srMentionTrig(r rune) bool { return r == '#' || r == '＃' || r == '@' || r == '＠' }

// srFoldPrefix reports whether rs starts with the ASCII word w, ignoring case.
func srFoldPrefix(rs []rune, w string) bool {
	if len(rs) < len(w) {
		return false
	}
	for i := 0; i < len(w); i++ {
		if unicode.ToLower(rs[i]) != rune(w[i]) {
			return false
		}
	}
	return true
}

// srModAt is buildModifierRegex's l, -?(?:from|to|…):, at rs[i:].
func srModAt(rs []rune, i int) bool {
	if i < len(rs) && rs[i] == '-' {
		i++
	}
	if i >= len(rs) {
		return false
	}
	for _, t := range srFilterTypes {
		if srFoldPrefix(rs[i:], t) && i+len(t) < len(rs) && rs[i+len(t)] == ':' {
			return true
		}
	}
	return false
}

// srFilterValueOK is the value group of buildModifierRegex,
// ( ?(?!.*  )(?!l)(?:(?!\s+l)(?!\s+[#＃@＠])[^\n])*)$: the rest of the
// text, without a newline or a double space, not starting another
// modifier, with no whitespace before another modifier, # or @.
func srFilterValueOK(v []rune) bool {
	ok := func(r []rune) bool {
		for i, c := range r {
			if c == '\n' || (c == ' ' && i+1 < len(r) && r[i+1] == ' ') {
				return false
			}
		}
		if srModAt(r, 0) {
			return false
		}
		for i := 0; i < len(r); i++ {
			if !srJSSpace(r[i]) {
				continue
			}
			j := i
			for j < len(r) && srJSSpace(r[j]) {
				j++
			}
			if j < len(r) && (srModAt(r, j) || srMentionTrig(r[j])) {
				return false
			}
			i = j - 1
		}
		return true
	}
	if len(v) > 0 && v[0] == ' ' && ok(v[1:]) {
		return true
	}
	return ok(v)
}

// srTryFilter is tryMatchFilter for one type: the leftmost match of its
// regex, (^|\n|\s)?(-)?(typ:…)$, kept only at the start of the text or
// after whitespace. It returns where the "-" or the modifier starts and
// whether a "-" was typed.
func srTryFilter(rs []rune, typ string) (index, neg int, ok bool) {
	has := typ == "has" || typ == "hasmy"
	rest := func(i int) (int, bool) {
		for _, n := range [2]int{1, 0} {
			j := i
			if n == 1 {
				if j >= len(rs) || rs[j] != '-' {
					continue
				}
				j++
			}
			if !srFoldPrefix(rs[j:], typ) || j+len(typ) >= len(rs) || rs[j+len(typ)] != ':' {
				continue
			}
			k := j + len(typ) + 1
			if !has && k < len(rs) && rs[k] == ':' {
				continue // (?!:)
			}
			if k+1 < len(rs) && rs[k] == ' ' && (rs[k+1] == ' ' || srMentionTrig(rs[k+1])) {
				continue // (?!  )(?! [#＃@＠])
			}
			if srFilterValueOK(rs[k:]) {
				return n, true
			}
		}
		return 0, false
	}
	for p := 0; p <= len(rs); p++ {
		if p == 0 {
			if n, ok := rest(0); ok {
				return 0, n, true
			}
		}
		if p < len(rs) && srJSSpace(rs[p]) {
			if n, ok := rest(p + 1); ok {
				return p + 1, n, true
			}
		}
		if p > 0 {
			if _, ok := rest(p); ok {
				return 0, 0, false // mid-word: tryMatchFilter keeps nothing
			}
		}
	}
	return 0, 0, false
}

// srFilterMatch is the search-filter completer's getMatch over the text
// before the caret. dateQuick is omni_date_quick_filters on.
func srFilterMatch(rs []rune, dateQuick bool) (srACMatch, bool) {
	if !slices.Contains(rs, ':') {
		return srACMatch{}, false
	}
	for _, typ := range srFilterTypes {
		if dateQuick && srFilterDateTypes[typ] {
			continue
		}
		idx, neg, ok := srTryFilter(rs, typ)
		if !ok {
			continue
		}
		match := string(rs[idx:])
		if strings.Count(match, " ") > 4 {
			return srACMatch{}, false
		}
		modEnd := idx + neg + len(typ) + 1
		return srACMatch{kind: srACFilter, typ: typ, mod: string(rs[idx:modEnd]), start: idx, anchor: modEnd,
			end: len(rs), match: match, value: srFilterQuery(match)}, true
	}
	return srACMatch{}, false
}

// srFilterQuery is normalizeQuery: the modifier dropped, trimmed.
func srFilterQuery(s string) string {
	rs := []rune(s)
	i := 0
	for k := 0; k < 2 && i < len(rs) && rs[i] == '-'; k++ {
		i++
	}
	for _, t := range srFilterTypes {
		if srFoldPrefix(rs[i:], t) && i+len(t) < len(rs) && rs[i+len(t)] == ':' {
			return strings.TrimSpace(string(rs[i+len(t)+1:]))
		}
	}
	return strings.TrimSpace(s)
}

// srMentionMatch is Texty's @ and # completers over the text before the
// caret (the composer's detectTrigger): the word before the caret starting
// with @ or #, or an @ at a word boundary whose name has single spaces
// ("@karim h").
func srMentionMatch(rs []rune) (srACMatch, bool) {
	caret := len(rs)
	start := caret
	for start > 0 && !srWordSep(rs[start-1]) {
		start--
	}
	if start < caret {
		tok := rs[start:caret]
		q := string(tok[1:])
		switch tok[0] {
		case '@':
			if srValidQuery(q, ".-_'") {
				return srACMatch{kind: srACMember, start: start, anchor: start, end: caret, match: string(tok), value: q}, true
			}
			return srACMatch{}, false
		case '#':
			if srValidQuery(q, ".-_") {
				return srACMatch{kind: srACChannel, start: start, anchor: start, end: caret, match: string(tok), value: q}, true
			}
			return srACMatch{}, false
		}
	}
	const maxLen = 48
	for i := caret; i > 0 && caret-i < maxLen; i-- {
		r := rs[i-1]
		if r == '@' {
			if i-1 > 0 && !srWordSep(rs[i-2]) {
				break
			}
			q := string(rs[i:caret])
			if strings.TrimSpace(q) == "" || !srValidQuery(q, ".-_' ") || strings.Contains(q, "  ") {
				return srACMatch{}, false
			}
			return srACMatch{kind: srACMember, start: i - 1, anchor: i - 1, end: caret, match: string(rs[i-1 : caret]), value: q}, true
		}
		if r == '\n' || r == '#' || r == ':' || r == '/' {
			break
		}
	}
	return srACMatch{}, false
}

func srWordSep(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == ' ' }

// srValidQuery reports whether q holds only letters, digits and extra.
func srValidQuery(q, extra string) bool {
	for _, r := range q {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(extra, r) {
			return false
		}
	}
	return true
}

// ---- what each modifier takes (getSearchFilterCompleters) ----

// srACValue is a static completion: its id and label.
type srACValue struct{ id, label string }

var srACDateValues = []srACValue{{"today", "Today"}, {"yesterday", "Yesterday"}, {"week", "This week"},
	{"month", "This month"}, {"year", "This year"}}

// srACValues is a modifier's static values; todos is later_todos on,
// surfaces surfaces_synth_view on.
func srACValues(typ string, todos, surfaces bool) []srACValue {
	switch typ {
	case "in":
		return []srACValue{{"activity", "Activity"}}
	case "is":
		v := []srACValue{{"saved", "Saved"}}
		if todos {
			v = append(v, srACValue{"todos", "To-dos"})
		}
		return append(v, srACValue{"thread", "Thread"}, srACValue{"dm", "DM"})
	case "has":
		return []srACValue{{"action", "Action"}, {"link", "Link"}, {"pin", "Pin"}, {"reaction", "Reaction"},
			{"file", "File"}, {"star", "Star"}}
	case "type":
		v := []srACValue{{"lists", "Lists"}}
		if surfaces {
			v = append(v, srACValue{"surfaces", "Surfaces"})
		}
		return append(v, srACValue{"canvases", "Canvases & Posts"}, srACValue{"documents", "Documents"},
			srACValue{"emails", "Emails"}, srACValue{"images", "Images"}, srACValue{"pdfs", "PDFs"},
			srACValue{"presentations", "Presentations"}, srACValue{"snippets", "Snippets"},
			srACValue{"spreadsheets", "Spreadsheets"}, srACValue{"audio", "Audio"}, srACValue{"videos", "Videos"},
			srACValue{"docs", "Posts"})
	case "before", "after", "on", "during":
		return srACDateValues
	case "threads":
		return []srACValue{{"all", "All"}, {"replies", "Replies"}}
	}
	return nil
}

// srACMemberTypes take people (completionTypes ["member"]).
var srACMemberTypes = map[string]bool{"from": true, "to": true, "with": true, "creator": true, "dm": true}

// srACValueIcon is ltuT's T: a value row's icon.
var srACValueIcon = map[string]string{
	"dm": "message", "thread": "threads", "saved": "bookmark", "todos": "check-circle", "action": "play",
	"link": "link", "star": "star", "pin": "pin", "reaction": "reaction", "file": "files",
	"lists": "lists", "surfaces": "gauge", "canvases": "canvas", "documents": "document", "emails": "email",
	"images": "image", "pdfs": "pdf-file", "presentations": "presentation", "snippets": "text-snippet",
	"spreadsheets": "spreadsheet", "audio": "sound", "videos": "video", "docs": "posts", "quip": "quip",
	"today": "calendar", "yesterday": "calendar", "week": "calendar", "month": "calendar", "year": "calendar",
	"all": "threads", "replies": "threads", "activity": "notifications",
}

// srACValueText is getLocalizedFilterValue: what a value pick inserts
// after the modifier (type: takes its label lower-cased, oI).
func srACValueText(typ string, v srACValue) string {
	if typ == "type" {
		return strings.ToLower(v.label)
	}
	return v.id
}

// ---- rows ----

// srACItemKind is a completer row's kind.
type srACItemKind uint8

const (
	srAIValue   srACItemKind = iota // a static value (valueResult)
	srAIMember                      // a person (TextyAutocompleteMemberResult)
	srAIChannel                     // a channel (the channel result)
	srAIEmoji                       // an emoji (emojiResult)
)

// srACItem is one row of the completer's menu.
type srACItem struct {
	kind  srACItemKind
	id    string // value id, user id, channel id, emoji name
	label string // value label, member name, channel name, emoji name
	icon  string // value icon
	// value rows: the value's insert text
	text string
	// members
	alt, avatar, presence, mention string
	self, bot                      bool
	// channels
	private bool
	// emoji
	emojiURL string
}

// srToken is a filter or mention token in the box: its label as typed
// and what the search request gets.
type srToken struct {
	start   int    // rune offset in the box's text
	label   string // "from:@Karim"
	req     string // "from:<@U…>"
	icon    string // getSearchFilterIconName ("" for mentions)
	mention bool   // a plain @/# mention (c-member_slug), not a filter
	// mod and entity are a filter's slacksearchfilter modifier ("from",
	// "-in") and entityId ("U…", "link", "2026-11-04"); a mention's
	// entity is its id
	mod, entity string
	// self is the filter's mention attribute (shouldHighlightMention: the
	// viewer): drawn as c-member_slug--mention
	self bool
}

func (t srToken) end() int { return t.start + len([]rune(t.label)) }

// srACPick is getInsertData for item it under match m: the token a pick
// inserts.
func srACPick(m srACMatch, it srACItem) srToken {
	mod := strings.ToLower(m.mod)
	switch m.kind {
	case srACMember:
		return srToken{label: "@" + it.mention, req: "<@" + it.id + ">", mention: true, entity: it.id}
	case srACChannel:
		return srToken{label: "#" + it.label, req: "<#" + it.id + "|" + it.label + ">", mention: true, entity: it.id}
	}
	// getModifierForSearchFilterId: the type, "-" kept
	t := srToken{icon: srFilterIcon(m.typ, it), mod: m.typ, entity: it.id}
	if strings.HasPrefix(m.mod, "-") {
		t.mod = "-" + m.typ
	}
	switch it.kind {
	case srAIMember:
		t.label, t.req = m.mod+"@"+it.mention, mod+"<@"+it.id+">"
		t.self = it.self // formatMemberInsertData's mention: shouldHighlightMention
	case srAIChannel:
		t.label, t.req = m.mod+it.label, mod+"<#"+it.id+"|"+it.label+">"
	case srAIEmoji:
		// emojiBestNameMatchLocalized || name
		t.label, t.req = m.mod+":"+it.label+":", mod+":"+it.label+":"
	default:
		t.label, t.req = m.mod+it.text, mod+it.id
	}
	return t
}

// srFilterIcon is getSearchFilterIconName: the token's icon.
func srFilterIcon(typ string, it srACItem) string {
	switch typ {
	case "after", "before", "during", "on":
		return "calendar"
	case "in":
		switch {
		case it.id == "activity":
			return "notifications"
		case it.kind == srAIMember:
			return "user"
		case it.private:
			return "lock-filled"
		}
		return "channel"
	case "dm":
		return "direct-messages"
	case "hasmy":
		return "reaction"
	case "has":
		if ic := srACValueIcon[it.id]; ic != "" && it.kind == srAIValue {
			return ic
		}
		return "reaction"
	case "type":
		if ic := srACValueIcon[it.id]; ic != "" {
			return ic
		}
		return "document"
	case "is":
		if ic := srACValueIcon[it.id]; ic != "" {
			return ic
		}
		return "comment"
	case "threads":
		if ic := srACValueIcon[it.id]; ic != "" {
			return ic
		}
		return "threads"
	}
	return "user"
}

// srMentionName is getMemberMentionName: the display name, else the real
// name.
func srMentionName(u core.User) string {
	switch {
	case u.DisplayName != "":
		return u.DisplayName
	case u.RealName != "":
		return u.RealName
	}
	return userName(u)
}

// srQueryIsSelf is queryIsMaybeSelf (selfFirst).
func srQueryIsSelf(q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	return q == "me" || q == "you"
}

// ---- the sources (searchMembers, searchChannelsMembersAndWorkspaces) ----

// srACRanker ranks the completer's candidates with Slack's Searcher
// (internal/acrank, source texty-autocomplete).
type srACRanker struct {
	h     Host
	c     *core.App
	opts  acrank.Options
	self  string
	team  string
	ims   map[string]core.Conv
	cnt   map[string]core.Count
	hist  map[string]float64
	users []core.User // every known person, by id
}

func newSRACRanker(h Host) *srACRanker {
	c := h.Core()
	if c == nil {
		return nil
	}
	s := c.Snapshot()
	r := &srACRanker{h: h, c: c, self: s.Self.ID, team: s.Self.TeamID, ims: map[string]core.Conv{}, cnt: s.Counts,
		hist: c.ViewHistory()}
	for _, cv := range s.Convs {
		if cv.Kind == "im" && cv.User != "" {
			r.ims[cv.User] = cv
		}
	}
	on := func(v string) bool { return v == "on" || strings.HasPrefix(v, "treatment") }
	r.opts = acrank.Options{Source: acrank.SourceTexty, Limit: 25, Frecency: c.FrecencyCache(),
		Weights: c.AutocompleteModel(), Now: time.Now(),
		Passthrough:          c.Experiment("autocomplete_flannel_fuzzy_match_passthrough") == "treatment",
		DemotePassthrough:    on(c.Experiment("autocomplete_demote_flannel_passthrough_rltv_exact_match")),
		BeyondWordBoundaries: on(c.Experiment("ml_fuzzy_match_beyond_word_boundaries_v2")),
		DisplayRealNames:     s.Prefs.DisplayRealNames,
		ExactNameGroup:       c.Experiment("search_autocomplete_promote_exact_name_matches"),
		EnterpriseID:         s.EnterpriseID, DefaultWorkspace: s.Team.ID, SelfTeams: s.Self.Teams}
	known := map[string]core.User{}
	for _, u := range c.KnownUsers() {
		known[u.ID] = u
	}
	for id, u := range s.Users {
		known[id] = u
	}
	if s.Self.ID != "" {
		known[s.Self.ID] = s.Self
	}
	for _, u := range known {
		r.users = append(r.users, u)
	}
	sort.Slice(r.users, func(i, j int) bool { return r.users[i].ID < r.users[j].ID })
	return r
}

// memberCand is a person as the Searcher sees them.
func (r *srACRanker) memberCand(u core.User, remote bool) acrank.Cand {
	c := acrank.Cand{Kind: acrank.Member, ID: u.ID, Name: u.Handle, Remote: remote,
		RealName: u.RealName, DisplayName: u.DisplayName, FirstName: u.FirstName, LastName: u.LastName,
		Self: u.ID == r.self, Bot: u.IsBot || u.AppUser, Deleted: u.Deleted,
		Active:   !u.IsBot && !u.AppUser && !u.Deleted && !u.Guest && u.ID != "USLACKBOT",
		External: u.External, SameTeam: u.TeamID != "" && u.TeamID == r.team, HasPresence: u.Presence != "",
		EnterpriseID: u.EnterpriseID, Teams: u.Teams}
	if c.DisplayName == "" && u.DisplaySet {
		c.DisplayName = u.Name
	}
	if im, ok := r.ims[u.ID]; ok {
		c.IM, c.IMPriority, c.IMStarred = true, im.Priority, im.Starred
		cnt := r.cnt[im.ID]
		c.IMLatest, c.IMLastRead = srTSF(cnt.Latest), srTSF(cnt.LastRead)
		c.History = r.hist[im.ID]
	}
	return c
}

func (r *srACRanker) memberItem(u core.User) srACItem {
	u = memberView(u)
	it := srACItem{kind: srAIMember, id: u.ID, label: userName(u), avatar: u.Avatar, presence: u.Presence,
		self: u.ID == r.self, bot: u.IsBot || u.AppUser, mention: srMentionName(u)}
	if u.DisplaySet && u.RealName != "" && u.RealName != it.label {
		it.alt = u.RealName
	}
	return it
}

// rank runs the Searcher over people (remote: the server's hits for the
// query) and channels: the sidebar's and the server's hits.
func (r *srACRanker) rank(q string, remote map[string]core.User, people, channels bool, hits []core.ChannelHit) []srACItem {
	var cands []acrank.Cand
	var items []srACItem
	if people {
		seen := map[string]bool{}
		for _, u := range r.users {
			_, rem := remote[u.ID]
			seen[u.ID] = true
			cands = append(cands, r.memberCand(u, rem))
			items = append(items, r.memberItem(u))
		}
		ids := make([]string, 0, len(remote))
		for id := range remote {
			if !seen[id] {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			cands = append(cands, r.memberCand(remote[id], true))
			items = append(items, r.memberItem(remote[id]))
		}
	}
	if channels {
		seen := map[string]bool{}
		add := func(id, name, kind string, member, archived, rem bool) {
			if id == "" || seen[id] || archived || (kind != "channel" && kind != "private") {
				return
			}
			seen[id] = true
			cnt := r.cnt[id]
			cands = append(cands, acrank.Cand{Kind: acrank.Channel, ID: id, Name: name, Remote: rem,
				Private: kind == "private", IsMember: member, Unread: cnt.Unread,
				LastRead: srTSF(cnt.LastRead), Latest: srTSF(cnt.Latest), History: r.hist[id]})
			items = append(items, srACItem{kind: srAIChannel, id: id, label: name, private: kind == "private"})
		}
		for _, cv := range r.c.Snapshot().Convs {
			add(cv.ID, cv.Name, cv.Kind, true, cv.IsArchived, false)
		}
		for _, h := range hits {
			add(h.ID, h.Name, h.Kind, h.IsMember, h.IsArchived, true)
		}
	}
	rs := acrank.Rank(q, cands, r.opts)
	out := make([]srACItem, 0, len(rs))
	for _, res := range rs {
		out = append(out, items[res.Index])
	}
	if srQueryIsSelf(q) {
		// sort.selfFirst
		for i, it := range out {
			if it.kind == srAIMember && it.self && i > 0 {
				out = append(append([]srACItem{it}, out[:i]...), out[i+1:]...)
				break
			}
		}
	}
	return out
}

// srTSF parses a Slack ts into seconds.
func srTSF(s string) float64 {
	var f, div float64 = 0, 0
	for _, c := range s {
		switch {
		case c == '.' && div == 0:
			div = 1
		case c >= '0' && c <= '9':
			if div > 0 {
				div *= 10
				f += float64(c-'0') / div
			} else {
				f = f*10 + float64(c-'0')
			}
		default:
			return f
		}
	}
	return f
}

// srACStatic is searchStaticValues: the values whose label starts with q.
func srACStatic(vals []srACValue, typ, q string) []srACItem {
	q = strings.ToLower(q)
	var out []srACItem
	for _, v := range vals {
		if q != "" && !strings.HasPrefix(strings.ToLower(v.label), q) {
			continue
		}
		out = append(out, srACItem{kind: srAIValue, id: v.id, label: v.label, icon: srACValueIcon[v.id],
			text: srACValueText(typ, v)})
	}
	return out
}

// srACEmoji is searchEmoji (the colons dropped from the query).
func srACEmoji(h Host, q string, o acrank.Options) []srACItem {
	q = strings.NewReplacer(":", "", "：", "").Replace(q)
	var out []srACItem
	for _, e := range AutocompleteEmoji(h, q, o) {
		out = append(out, srACItem{kind: srAIEmoji, id: e.Name, label: e.Name, emojiURL: e.URL})
	}
	return out
}

// ---- queries that already hold refs ----

// srRefRe finds a request query's refs: a modifier's (from:<@U…>,
// in:<#C…|name>) or a bare mention (<@U…>, <#C…|name>).
var srRefRe = regexp.MustCompile(`(?i)(^|\s)((-?(?:from|to|with|in|creator|dm)):)?<([@#])([A-Z0-9]+)(?:\|([^>]*))?>`)

// srDisplayQuery is a request query as the box shows it (the omniswitcher's
// display query): each ref a token labelled as a pick would have it,
// "from:<@U…>" → "from:@Karim", "in:<#C…|general>" → "in:general",
// "<@U…>" → "@Karim". Unknown people and channels stay as they are.
func srDisplayQuery(h Host, q string) (string, []srToken) {
	ms := srRefRe.FindAllStringSubmatchIndex(q, -1)
	if ms == nil {
		return q, nil
	}
	var b strings.Builder
	var toks []srToken
	at, runes := 0, 0
	write := func(s string) {
		b.WriteString(s)
		runes += utf8.RuneCountInString(s)
	}
	for _, m := range ms {
		start := m[3]
		write(q[at:start])
		at = m[1]
		mod := ""
		if m[4] >= 0 {
			mod = q[m[4]:m[5]]
		}
		sigil, id, name := q[m[8]:m[9]], q[m[10]:m[11]], ""
		if m[12] >= 0 {
			name = q[m[12]:m[13]]
		}
		tok, ok := srRefToken(h, mod, sigil, id, name)
		if !ok {
			write(q[start:m[1]])
			continue
		}
		tok.req, tok.start = q[start:m[1]], runes
		write(tok.label)
		toks = append(toks, tok)
	}
	write(q[at:])
	return b.String(), toks
}

// srRefToken labels one ref the way srACPick would.
func srRefToken(h Host, mod, sigil, id, name string) (srToken, bool) {
	typ := strings.TrimPrefix(strings.ToLower(strings.TrimSuffix(mod, ":")), "-")
	fmod := typ
	if strings.HasPrefix(mod, "-") {
		fmod = "-" + typ
	}
	if sigil == "@" {
		u, ok := lookupUser(h, id)
		if !ok {
			return srToken{}, false
		}
		n := srMentionName(u)
		if mod == "" {
			return srToken{label: "@" + n, mention: true, entity: id}, true
		}
		self := false
		if c := h.Core(); c != nil {
			self = c.Self().ID == id
		}
		return srToken{label: mod + "@" + n, icon: srFilterIcon(typ, srACItem{kind: srAIMember, id: id}), mod: fmod, entity: id, self: self}, true
	}
	if mod != "" && typ != "in" {
		return srToken{}, false
	}
	private := false
	if c := h.Core(); c != nil {
		ref := c.ConvRef(id)
		if name == "" {
			name = ref.Name
		}
		private = ref.Kind == "private"
	}
	if name == "" {
		return srToken{}, false
	}
	if mod == "" {
		return srToken{label: "#" + name, mention: true, entity: id}, true
	}
	return srToken{label: mod + name, icon: srFilterIcon(typ, srACItem{kind: srAIChannel, id: id, private: private}), mod: fmod, entity: id}, true
}

// SearchDisplayText is a search query as Slack's search box shows it
// (BaseSearch's plainTextQuery): refs read as their tokens' labels.
func SearchDisplayText(h Host, q string) string {
	t, _ := srDisplayQuery(h, q)
	return t
}
