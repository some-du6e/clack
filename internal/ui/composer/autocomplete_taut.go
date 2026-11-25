package composer

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/acrank"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Slack's autocompleteMembers / autocompleteChannels / autocompleteEmoji
// thunks for plugins (NativeAtChannel filters @channel/@here,
// PrivateChannel merges private channels it found elsewhere, HMojis adds
// its hidden emoji).
//
// The first two are sync thunks: when patched, their native runs on the
// JS thread, so it must be pure and quick. autocompleteEmoji answers with
// a promise, as Slack's searcher does, and its native runs on a goroutine.
// The composer dispatches in the background with a native that ranks a
// snapshot of the sources taken at dispatch time (with the args the
// plugins pass, e.g. includeAllBroadcastKeywords), and puts the plugins'
// answer in on the UI goroutine. As Slack's TextyAutocomplete, the list
// keeps the rows it shows until the answer comes (acPluginHold, then the
// composer's own), a Tab or Enter waits for it (acPluginWait), and each
// query's dispatch ends the last's (its signal aborted), or its deadline
// (acPluginTimeout). Rows map back by id: rows the composer knows keep
// their avatars, presence and status; new ones are built from the
// result's item.

// memberSources is everything ranking an @ query needs, copied out from
// under ac.mu so it can be ranked on any goroutine.
type memberSources struct {
	users    []core.User
	remote   map[string]bool // users the server search returned
	members  map[string]core.User
	selfID   string
	selfTeam string
	groups   []core.Usergroup
	// keywords are the broadcast keywords the viewer may use here
	// (getBroadcastKeywordsForUser)
	keywords []string
	rk       rankCtx
}

// memberSourcesLocked snapshots the @ sources; ac.mu must be held. The
// maps aren't copied: async loads replace them wholesale (ac.land), they
// are never written in place.
func (ac *Autocomplete) memberSourcesLocked() memberSources {
	src := memberSources{
		users:   make([]core.User, 0, len(ac.known)+len(ac.members)+len(ac.searched)),
		remote:  map[string]bool{},
		members: ac.members,
		selfID:  ac.selfID,
		groups:  ac.groups,
		rk:      ac.rankCtxLocked(),
	}
	if self, ok := ac.known[ac.selfID]; ok {
		src.selfTeam = self.TeamID
	}
	seen := map[string]bool{}
	add := func(u core.User) {
		if u.ID == "" || seen[u.ID] {
			return
		}
		seen[u.ID] = true
		src.users = append(src.users, u)
	}
	// the store's people: the conversation's members, the ones the
	// client knows, then the server search's (marked remote for the
	// fuzzy passthrough). Sorted by id so equal ranks stay put.
	for _, id := range sortedKeys(ac.members) {
		add(ac.members[id])
	}
	for _, id := range sortedKeys(ac.known) {
		add(ac.known[id])
	}
	for _, id := range sortedKeys(ac.searched) {
		add(ac.searched[id])
		src.remote[id] = true
	}
	if ac.haveConv {
		src.keywords = core.BroadcastKeywords(ac.conv, ac.perms)
	}
	return src
}

// rank is the composer's @ ranking. Broadcast keywords are the ones the
// viewer may use here; includeAll offers every one (Slack's
// includeAllBroadcastKeywords), @everyone included.
func (s memberSources) rank(q string, includeAll bool) []acItem { return s.rankMembers(q, includeAll) }

// channelSources is what ranking a # query needs.
type channelSources struct {
	convs []core.Conv
	hits  []core.ChannelHit
	rk    rankCtx
}

func (ac *Autocomplete) channelSourcesLocked() channelSources {
	hits := make([]core.ChannelHit, 0, len(ac.chanHits))
	for _, id := range sortedKeys(ac.chanHits) {
		hits = append(hits, ac.chanHits[id])
	}
	return channelSources{convs: ac.convs, hits: hits, rk: ac.rankCtxLocked()}
}

func (s channelSources) rank(q string) []acItem { return s.rankChannels(q) }

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// ---- Slack's shapes ----

// membersArgs is autocompleteMembers' params, as TextyAutocomplete's
// search passes them (isThread: !!threadTs).
type membersArgs struct {
	Query                       string `json:"query"`
	ChannelID                   string `json:"channelId,omitempty"`
	IsThread                    bool   `json:"isThread"`
	ThreadTS                    string `json:"threadTs,omitempty"`
	EnableBroadcastKeywords     *bool  `json:"enableBroadcastKeywords,omitempty"`
	IncludeAllBroadcastKeywords bool   `json:"includeAllBroadcastKeywords,omitempty"`
}

// channelsArgs is autocompleteChannels' params. Slack passes the typed
// token, sigil and all ("#gen"); PrivateChannel strips it.
type channelsArgs struct {
	Query     string `json:"query"`
	ChannelID string `json:"channelId,omitempty"`
}

// emojiArgs is autocompleteEmoji's params as TextyAutocomplete's search
// passes them (the typed token, ":tu"; the thunk takes the colons off;
// isThread: !!threadTs) and the thunk's own (limit 25,
// standardEmojiOnly), which plugins pass, read as JS reads them.
type emojiArgs struct {
	Query             string    `json:"query"`
	ChannelID         string    `json:"channelId,omitempty"`
	IsThread          bool      `json:"isThread"`
	ThreadTS          string    `json:"threadTs,omitempty"`
	Limit             looseInt  `json:"limit,omitempty"`
	StandardEmojiOnly looseBool `json:"standardEmojiOnly,omitempty"`
}

// looseInt is a number as JS uses one: a numeric string too; anything
// else 0 (the default).
type looseInt int

func (n *looseInt) UnmarshalJSON(b []byte) error {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	switch x := v.(type) {
	case float64:
		*n = looseInt(x)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		*n = looseInt(f)
	}
	return nil
}

// looseBool is JS's truthiness.
type looseBool bool

func (t *looseBool) UnmarshalJSON(b []byte) error {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	switch x := v.(type) {
	case bool:
		*t = looseBool(x)
	case float64:
		*t = x != 0
	case string:
		*t = x != ""
	case nil:
		*t = false
	default:
		*t = true
	}
	return nil
}

// emojiRow is one of autocompleteEmoji's results as Slack's searcher
// formats it (formatEmojiResult): the emoji (a standard one's unicode in
// hex, "1f431"; a custom one's image), its id (the emoji's id, else its
// name), its score (its place in the composer's ranking: lower first, as
// Slack's fuzzy scores) and where the query matched its best name.
type emojiRow struct {
	ID                          string         `json:"id"`
	Item                        map[string]any `json:"item,omitempty"`
	Score                       float64        `json:"score"`
	EmojiScore                  float64        `json:"emojiScore"`
	EmojiBestNameMatch          string         `json:"emojiBestNameMatch,omitempty"`
	EmojiBestNameMatchCanonical string         `json:"emojiBestNameMatchCanonical,omitempty"`
	EmojiMatchIndices           []int          `json:"emojiMatchIndices"`
}

// acRow is one autocomplete result as Slack's thunks return it.
type acRow struct {
	ID   string         `json:"id"`
	Type string         `json:"type,omitempty"`
	Item map[string]any `json:"item,omitempty"`
}

// broadcastRowID is Slack's id for a broadcast keyword row ("BKhere").
func broadcastRowID(rng string) string { return "BK" + rng }

// toRows turns composer rows into Slack's result rows.
func toRows(items []acItem) []acRow {
	rows := make([]acRow, 0, len(items))
	for i := range items {
		rows = append(rows, toRow(&items[i]))
	}
	return rows
}

func toRow(it *acItem) acRow {
	switch {
	case it.Kind == acChannel:
		return acRow{ID: it.ID, Type: "channel", Item: map[string]any{
			"id": it.ID, "name": it.Name, "is_private": it.Private, "is_archived": it.Archived, "is_member": it.Member,
		}}
	case it.IsBroadcast():
		id := broadcastRowID(it.ID)
		return acRow{ID: id, Type: "broadcast_keyword", Item: map[string]any{
			"id": id, "name": it.ID, "is_broadcast_keyword": true,
		}}
	case it.IsGroup():
		return acRow{ID: it.ID, Type: "usergroup", Item: map[string]any{
			"id": it.ID, "handle": it.Name, "name": it.Secondary, "user_count": it.Count, "is_usergroup": true,
		}}
	}
	return acRow{ID: it.ID, Type: "member", Item: map[string]any{
		"id": it.ID, "name": it.Name, "real_name": it.Secondary, "is_bot": it.IsBot,
		"profile": map[string]any{"display_name": it.Name, "real_name": it.Secondary, "image_72": it.Avatar},
	}}
}

// rowKey is the id a composer row answers to in Slack's results: an
// emoji's is its own name (Slack's id is item.id ?? item.name, and its
// item is the emoji, an alias's target; the skin tone goes on as it's
// inserted).
func rowKey(it *acItem) string {
	if it.IsBroadcast() {
		return broadcastRowID(it.ID)
	}
	if it.Kind == acEmoji {
		return emojiCanon(it)
	}
	return it.ID
}

// emojiCanon is an emoji row's own name (Slack's emojiBestNameMatchCanonical).
func emojiCanon(it *acItem) string {
	if it.Canon != "" {
		return it.Canon
	}
	return it.Name
}

// toEmojiRows turns the composer's ":" rows for query q into Slack's
// (formatEmojiResult): item is the emoji (its own name, a standard one's
// unicode without the skin tone, a custom one's image), the name that
// matched (an alias, say) its emojiBestNameMatch and its own name its
// emojiBestNameMatchCanonical.
func toEmojiRows(items []acItem, q string) []emojiRow {
	rows := make([]emojiRow, 0, len(items))
	for i := range items {
		it := &items[i]
		canon := emojiCanon(it)
		item := map[string]any{"name": canon}
		if it.Uni != "" {
			item["unicode"] = emojiHex(untoned(it.Uni))
		} else if it.Avatar != "" {
			item["url"] = it.Avatar
		}
		rows = append(rows, emojiRow{ID: canon, Item: item, Score: float64(i), EmojiScore: float64(i),
			EmojiBestNameMatch: it.Name, EmojiBestNameMatchCanonical: canon, EmojiMatchIndices: emojiMatchIndices(it.Name, q)})
	}
	return rows
}

// untoned is emoji text without its skin tone modifiers (U+1F3FB-1F3FF).
func untoned(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 0x1f3fb && r <= 0x1f3ff {
			return -1
		}
		return r
	}, s)
}

// emojiHex is Slack's unicode field for emoji text: each code point in
// hex, joined with "-" ("1f44d-1f3fd").
func emojiHex(s string) string {
	parts := make([]string, 0, 2)
	for _, r := range s {
		parts = append(parts, strconv.FormatInt(int64(r), 16))
	}
	return strings.Join(parts, "-")
}

// emojiFromHex reads emojiHex's form back ("" when it isn't one).
func emojiFromHex(s string) string {
	var b strings.Builder
	for _, p := range strings.Split(s, "-") {
		n, err := strconv.ParseInt(p, 16, 32)
		if err != nil || n <= 0 {
			return ""
		}
		b.WriteRune(rune(n))
	}
	return b.String()
}

// emojiMatchIndices is where q's letters fall in name, in order (Slack's
// matchIndices); none for a keyword match.
func emojiMatchIndices(name, q string) []int {
	out := []int{}
	qs := []rune(strings.ToLower(q))
	j := 0
	for i, r := range []rune(strings.ToLower(name)) {
		if j < len(qs) && r == qs[j] {
			out = append(out, i)
			j++
		}
	}
	if j < len(qs) {
		return []int{}
	}
	return out
}

// acPluginMaxRows caps the rows a plugins' answer shows.
const acPluginMaxRows = 200

// fromRows maps the plugins' answer back to composer rows, in the
// plugins' order, at most acPluginMaxRows. known holds every row the
// natives produced.
func fromRows(kind acKind, rows []acRow, known map[string]acItem) []acItem {
	out := make([]acItem, 0, min(len(rows), acPluginMaxRows))
	seen := map[string]bool{}
	for _, r := range rows {
		if len(out) == acPluginMaxRows {
			break
		}
		id := r.ID
		if id == "" {
			id, _ = r.Item["id"].(string)
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if it, ok := known[id]; ok {
			out = append(out, it)
			continue
		}
		if it, ok := itemFromRow(kind, id, r); ok {
			out = append(out, it)
		}
	}
	return out
}

// emojiLooks is what a plugin's ":" row is drawn and inserted with, taken
// on the UI goroutine: the skin tone the lists insert at, and the
// workspace's custom emoji (nil without a core).
type emojiLooks struct {
	tone   int
	custom func(name string) (url string, ok bool)
}

// fromEmojiRows maps the plugins' ":" answer back to composer rows, in
// their order, at most acPluginMaxRows. A row the composer knows (by its
// id: Slack's id, else item.id, else item.name) is its own, inserting the
// plugins' emojiBestNameMatchCanonical when they changed it; any other is
// built from the row (emojiRowItem).
func fromEmojiRows(rows []emojiRow, known map[string]acItem, looks emojiLooks) []acItem {
	out := make([]acItem, 0, min(len(rows), acPluginMaxRows))
	seen := map[string]bool{}
	for _, r := range rows {
		if len(out) == acPluginMaxRows {
			break
		}
		id := r.ID
		for _, k := range []string{"id", "name"} {
			if id == "" {
				id, _ = r.Item[k].(string)
			}
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if it, ok := known[id]; ok {
			if c := r.EmojiBestNameMatchCanonical; c != "" && c != emojiCanon(&it) && !strings.ContainsAny(c, ": \t\n") {
				it.Canon, it.ID = c, c
				if it.Atom != nil {
					a := *it.Atom
					a.ID, a.Text = c, ":"+c+":"
					it.Atom = &a
				}
			}
			out = append(out, it)
			continue
		}
		if it, ok := emojiRowItem(r, looks); ok {
			out = append(out, it)
		}
	}
	return out
}

// emojiRowItem builds a ":" row a plugin added (a hidden emoji) as Slack
// inserts and draws one: it inserts :emojiBestNameMatchCanonical: (else
// :item.name:), at the skin tone in use when the item has skin
// variations, and draws that name as Slack's Emoji component does, from
// the store (a plugin's upsert, the workspace's emoji), else item.url,
// else the standard emoji's image.
func emojiRowItem(r emojiRow, looks emojiLooks) (acItem, bool) {
	str := func(k string) string { s, _ := r.Item[k].(string); return s }
	bad := func(s string) bool { return s == "" || strings.ContainsAny(s, ": \t\n") }
	name := r.EmojiBestNameMatchCanonical
	if bad(name) {
		name = str("name")
	}
	if bad(name) {
		return acItem{}, false
	}
	shown := r.EmojiBestNameMatch // the bold name: what matched
	if bad(shown) {
		shown = name
	}
	ins, url, uni := name, "", ""
	if looks.custom != nil {
		url, _ = looks.custom(name)
	}
	if url == "" {
		url = str("url")
	}
	if _, text, ok := emoji.Standard(name); ok {
		if r.Item["skinVariations"] != nil && looks.tone >= 2 && looks.tone <= 6 {
			ins = name + "::skin-tone-" + strconv.Itoa(looks.tone)
			_, text, _ = emoji.Standard(ins)
		}
		uni = text
		if url == "" {
			url, _ = emoji.StandardURL(ins, "", "")
		}
	} else if url == "" {
		uni = emojiFromHex(str("unicode"))
	}
	if url == "" && uni == "" {
		return acItem{}, false
	}
	return acItem{Kind: acEmoji, ID: ins, Name: shown, Canon: name, Avatar: url, Uni: uni,
		Atom: &Atom{Kind: AtomEmoji, ID: ins, Text: ":" + ins + ":", URL: url, Uni: uni}}, true
}

// itemFromRow builds a row a plugin added (e.g. a private channel).
func itemFromRow(kind acKind, id string, r acRow) (acItem, bool) {
	str := func(k string) string { s, _ := r.Item[k].(string); return s }
	flag := func(k string) bool { b, _ := r.Item[k].(bool); return b }
	name := str("name")
	if kind == acChannel {
		if name == "" {
			return acItem{}, false
		}
		return acItem{Kind: acChannel, ID: id, Name: name, Private: flag("is_private"), Archived: flag("is_archived"), Member: flag("is_member"),
			Atom: &Atom{Kind: AtomChannel, ID: id, Text: "#" + name}}, true
	}
	if flag("is_broadcast_keyword") || strings.HasPrefix(id, "BK") {
		rng := strings.TrimPrefix(id, "BK")
		if name != "" {
			rng = name
		}
		return acItem{Kind: acUser, ID: rng, Name: rng, Atom: &Atom{Kind: AtomBroadcast, ID: rng, Text: "@" + rng}}, true
	}
	if flag("is_usergroup") {
		h := str("handle")
		if h == "" {
			h = name
		}
		return acItem{Kind: acUser, ID: id, Name: h, Secondary: name, Atom: &Atom{Kind: AtomUsergroup, ID: id, Text: "@" + h}}, true
	}
	if name == "" {
		if p, ok := r.Item["profile"].(map[string]any); ok {
			name, _ = p["display_name"].(string)
		}
	}
	if name == "" {
		return acItem{}, false
	}
	return acItem{Kind: acUser, ID: id, Name: name, Secondary: str("real_name"), Atom: &Atom{Kind: AtomUser, ID: id, Text: "@" + name}}, true
}

// ---- dispatch ----

const (
	// acPluginHold: while the plugins' answer for a query is out, the list
	// keeps the rows it shows, as Slack's TextyAutocomplete keeps
	// state.results until its search answers (one change a keystroke, no
	// rows flickering out and back); past it the composer's own rows for
	// the query show.
	acPluginHold = 150 * time.Millisecond
	// acPluginWait bounds a Tab or Enter's wait for that answer: Slack's
	// possiblySelectFirstItem defers a pick made while a search is in
	// flight (deferredSelection), so it takes the answered row 0.
	acPluginWait = time.Second
)

// acPluginTimeout: the plugins' answer is given up on after it, as
// Slack's search times out (docs/AUTOCOMPLETE-RULES.md). A var for tests.
var acPluginTimeout = 10 * time.Second

// acPluginCall is the plugins' answer out for one query. The UI goroutine
// owns it, but for out and ok, which the dispatch sets before done closes.
type acPluginCall struct {
	trig   acTrigger
	native []acItem // the composer's rows for trig
	cancel context.CancelFunc
	holdAt time.Time // when the composer's rows show if no answer came
	hold   *time.Timer
	done   chan struct{}
	out    []acItem // the plugins' rows
	ok     bool     // out is their answer (false: it failed, timed out or was cancelled)
	over   bool     // applied (or given up): nothing more of it shows
}

// sameQuery: a and b are the same list's same query.
func sameQuery(a, b acTrigger) bool { return a.Kind == b.Kind && a.Query == b.Query }

// pluginRank sends the patched thunk out for t, its answer to replace the
// rows when it comes. items are the composer's rows for t. It reports
// whether it went out; the caller then keeps the rows shown. Slack's next
// search aborts the last: so does this, whether it goes out or not.
// Called from Update on the UI goroutine.
func (ac *Autocomplete) pluginRank(t acTrigger, items []acItem) bool {
	prev := ac.plug
	ac.stopPlugin()
	tt := currentTaut()
	var name string
	switch t.Kind {
	case acUser:
		name = thACMembers
	case acChannel:
		name = thACChannels
	case acEmoji:
		name = thACEmoji
	}
	if name == "" || !tt.ThunkPatched(name) || ac.later == nil {
		return false
	}
	ac.mu.Lock()
	var mem memberSources
	var chs channelSources
	var emojiOpts acrank.Options
	switch t.Kind {
	case acUser:
		mem = ac.memberSourcesLocked()
	case acChannel:
		chs = ac.channelSourcesLocked()
	case acEmoji:
		emojiOpts = ac.rankCtxLocked().opts(acrank.SourceTexty)
	}
	ac.mu.Unlock()
	// what ranks a query the plugins changed, and draws the rows they
	// add, taken here: the emoji index and the skin tone are the UI
	// goroutine's (popups.EmojiRanker)
	var rankEmoji func(string, acrank.Options) []popups.EmojiMatch
	var looks emojiLooks
	if t.Kind == acEmoji && ac.coreEmoji() {
		rankEmoji = popups.EmojiRanker(ac.h)
		looks.tone = popups.SkinTone(ac.h)
		app := ac.h.Core()
		looks.custom = func(n string) (string, bool) {
			u, text, ok := app.S.CustomEmoji.Resolve(n, "")
			return u, ok && text == "" // the workspace's (or a plugin's), not a standard one
		}
	}

	// Every row a native produced, so the answer maps back to full rows.
	var mu sync.Mutex
	known := make(map[string]acItem, len(items))
	remember := func(xs []acItem) {
		mu.Lock()
		for i := range xs {
			known[rowKey(&xs[i])] = xs[i]
		}
		mu.Unlock()
	}
	remember(items)
	channelID, isThread, threadTS, later, includeAll := ac.channelID, ac.thread || ac.threadTS != "", ac.threadTS, ac.later, ac.ov.includeAll

	ctx, cancel := context.WithTimeout(ac.ctx, acPluginTimeout)
	c := &acPluginCall{trig: t, native: items, cancel: cancel, done: make(chan struct{})}
	// a dispatch again for the same query (async rows, the index) keeps the hold it had
	c.holdAt = time.Now().Add(acPluginHold)
	if prev != nil && sameQuery(prev.trig, t) {
		c.holdAt = prev.holdAt
	}
	c.hold = time.AfterFunc(max(time.Until(c.holdAt), 0), func() { later(func() { ac.holdOver(c) }) })
	ac.plug = c

	go func() {
		defer cancel()
		var out []acItem
		var err error
		switch t.Kind {
		case acUser:
			args := membersArgs{Query: t.Query, ChannelID: channelID, IsThread: isThread, ThreadTS: threadTS, IncludeAllBroadcastKeywords: includeAll}
			var rows []acRow
			rows, err = taut.Dispatch(ctx, tt, name, args, func(_ context.Context, a membersArgs) ([]acRow, error) {
				q := strings.TrimPrefix(a.Query, "@")
				var xs []acItem
				if q == t.Query && a.IncludeAllBroadcastKeywords == includeAll {
					xs = items
				} else {
					xs = mem.rank(q, a.IncludeAllBroadcastKeywords)
				}
				if a.EnableBroadcastKeywords != nil && !*a.EnableBroadcastKeywords {
					xs = withoutBroadcasts(xs)
				}
				remember(xs)
				return toRows(xs), nil
			})
			if err == nil {
				mu.Lock()
				out = fromRows(t.Kind, rows, known)
				mu.Unlock()
			}
		case acEmoji:
			args := emojiArgs{Query: ":" + t.Query, ChannelID: channelID, IsThread: isThread, ThreadTS: threadTS}
			var ers []emojiRow
			ers, err = taut.Dispatch(ctx, tt, name, args, func(_ context.Context, a emojiArgs) ([]emojiRow, error) {
				// the thunk's (query||"").replace(/:|\uFF1A/g, "")
				q := strings.NewReplacer(":", "", "\uff1a", "").Replace(a.Query)
				xs := items
				if q != t.Query && rankEmoji != nil {
					xs = emojiMatchItems(rankEmoji(q, emojiOpts))
				}
				if a.StandardEmojiOnly {
					xs = slices.DeleteFunc(slices.Clone(xs), func(it acItem) bool { return it.Uni == "" })
				}
				if n := cmpOr(int(a.Limit), 25); len(xs) > n {
					xs = xs[:n]
				}
				remember(xs)
				return toEmojiRows(xs, q), nil
			})
			if err == nil {
				mu.Lock()
				out = fromEmojiRows(ers, known, looks)
				mu.Unlock()
			}
		default:
			args := channelsArgs{Query: "#" + t.Query, ChannelID: channelID}
			var rows []acRow
			rows, err = taut.Dispatch(ctx, tt, name, args, func(_ context.Context, a channelsArgs) ([]acRow, error) {
				q := strings.TrimPrefix(strings.TrimSpace(a.Query), "#")
				xs := items
				if q != t.Query {
					xs = chs.rank(q)
				}
				remember(xs)
				return toRows(xs), nil
			})
			if err == nil {
				mu.Lock()
				out = fromRows(t.Kind, rows, known)
				mu.Unlock()
			}
		}
		c.out, c.ok = out, err == nil
		close(c.done)
		later(func() { ac.applyPluginCall(c) })
	}()
	return true
}

// stopPlugin ends the plugins' answer out (the next query's dispatch,
// Dismiss, the list closing): its dispatch returns and its JS hears its
// signal abort (taut.AbortableThunks), as Slack's search abort does.
func (ac *Autocomplete) stopPlugin() {
	if c := ac.plug; c != nil {
		ac.plug = nil
		c.over = true
		c.hold.Stop()
		c.cancel()
	}
}

// applyPluginCall puts c's answer in when it is still the list's (UI
// goroutine): the plugins' rows; when it failed (an error, its deadline)
// the composer's, unless the rows shown are this query's already.
func (ac *Autocomplete) applyPluginCall(c *acPluginCall) {
	if c != ac.plug || c.over {
		return
	}
	select {
	case <-c.done:
	default:
		return
	}
	c.over = true
	ac.plug = nil
	c.hold.Stop()
	if !ac.active || !sameQuery(ac.trig, c.trig) {
		return
	}
	switch {
	case c.ok:
		ac.setRows(c.out)
	case !sameQuery(ac.rowsFor, c.trig):
		ac.setRows(c.native)
	}
}

// holdOver: c's hold ran out with no answer, so the composer's rows for
// its query show, unless the rows shown are that query's already (a
// dispatch again for it).
func (ac *Autocomplete) holdOver(c *acPluginCall) {
	if c != ac.plug || c.over || !ac.active || !sameQuery(ac.trig, c.trig) || sameQuery(ac.rowsFor, c.trig) {
		return
	}
	ac.setRows(c.native)
}

// awaitPlugin is a Tab or Enter's wait for the plugins' rows for the open
// query (Slack's deferredSelection), at most acPluginWait: then the
// answer, else the composer's rows, is what it picks from.
func (ac *Autocomplete) awaitPlugin() {
	c := ac.plug
	if c == nil || c.over || !ac.active || !sameQuery(ac.trig, c.trig) {
		return
	}
	t := time.NewTimer(acPluginWait)
	defer t.Stop()
	select {
	case <-c.done:
		ac.applyPluginCall(c)
	case <-t.C:
		if !sameQuery(ac.rowsFor, c.trig) {
			ac.setRows(c.native)
		}
	}
}

// cmpOr is n, or or when n isn't positive.
func cmpOr(n, or int) int {
	if n > 0 {
		return n
	}
	return or
}

func withoutBroadcasts(xs []acItem) []acItem {
	out := make([]acItem, 0, len(xs))
	for _, it := range xs {
		if !it.IsBroadcast() {
			out = append(out, it)
		}
	}
	return out
}

// setRows shows items as the open query's rows (UI goroutine). The
// selected row stays selected when present.
func (ac *Autocomplete) setRows(items []acItem) {
	selID := ""
	if !ac.pseudo && ac.sel < len(ac.items) {
		selID = ac.items[ac.sel].ID
	}
	ac.items = ac.ov.keep(items)
	ac.rowsFor = ac.trig
	ac.itemsVer++
	ac.sel = 0
	for i, it := range ac.items {
		if selID != "" && it.ID == selID {
			ac.sel = i
			break
		}
	}
	if ac.h != nil {
		ac.h.Invalidate()
	}
}

// ---- TextyAutocomplete's props (chrome_slack.go) ----

// acOverrides are the TextyAutocomplete props a plugin changed:
// includeAllBroadcastKeywords (bChannel offers @channel/@here/@everyone
// where Slack wouldn't) and disallowedResults (row ids to leave out;
// BetterBlock's blocked people). The zero value changes nothing.
type acOverrides struct {
	includeAll bool
	disallowed map[string]bool
}

// keep drops the disallowed rows, into a slice of its own (a native's
// rows are also its dispatch's).
func (o *acOverrides) keep(items []acItem) []acItem {
	if len(o.disallowed) == 0 {
		return items
	}
	out := make([]acItem, 0, len(items))
	for _, it := range items {
		if !o.disallowed[it.ID] {
			out = append(out, it)
		}
	}
	return out
}

// setOverrides takes TextyAutocomplete's props as the plugins left them
// (nil when it isn't patched) and recomputes the rows when they changed.
// Called by Composer.Layout every frame, before Update.
func (ac *Autocomplete) setOverrides(p slackc.Props) {
	if p == nil {
		if ac.ov.includeAll || ac.ov.disallowed != nil {
			ac.ov = acOverrides{}
			ac.itemsOK = false
		}
		return
	}
	inc := p.Bool("includeAllBroadcastKeywords")
	ids := p.List("disallowedResults")
	same := inc == ac.ov.includeAll && len(ids) == len(ac.ov.disallowed)
	for _, v := range ids {
		if !same {
			break
		}
		id, _ := v.(string)
		same = ac.ov.disallowed[id]
	}
	if same {
		return
	}
	ac.ov.includeAll = inc
	ac.ov.disallowed = nil
	if len(ids) > 0 {
		ac.ov.disallowed = make(map[string]bool, len(ids))
		for _, v := range ids {
			if id, ok := v.(string); ok && id != "" {
				ac.ov.disallowed[id] = true
			}
		}
	}
	ac.itemsOK = false
}
