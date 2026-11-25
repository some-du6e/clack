package composer

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"

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
// The composer shows its own rows at once, dispatches in the background
// with a native that ranks a snapshot of the sources taken at dispatch
// time (with the args the plugins pass, e.g. includeAllBroadcastKeywords),
// and swaps in the plugins' answer on the UI goroutine. Rows map back by
// id: rows the composer knows keep their avatars, presence and status;
// new ones are built from the result's item.

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

// membersArgs is autocompleteMembers' params.
type membersArgs struct {
	Query                       string `json:"query"`
	ChannelID                   string `json:"channelId,omitempty"`
	IsThread                    bool   `json:"isThread,omitempty"`
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
// passes them (the typed token, ":tu"; the thunk takes the colons off)
// and the thunk's own (limit 25, standardEmojiOnly), which plugins pass.
type emojiArgs struct {
	Query             string `json:"query"`
	ChannelID         string `json:"channelId,omitempty"`
	IsThread          bool   `json:"isThread,omitempty"`
	Limit             int    `json:"limit,omitempty"`
	StandardEmojiOnly bool   `json:"standardEmojiOnly,omitempty"`
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
// emoji's is its name (Slack's emoji have no id but a custom one's; the
// skin tone goes on as it's inserted).
func rowKey(it *acItem) string {
	if it.IsBroadcast() {
		return broadcastRowID(it.ID)
	}
	if it.Kind == acEmoji {
		return it.Name
	}
	return it.ID
}

// toEmojiRows turns the composer's ":" rows for query q into Slack's.
func toEmojiRows(items []acItem, q string) []emojiRow {
	rows := make([]emojiRow, 0, len(items))
	for i := range items {
		it := &items[i]
		item := map[string]any{"name": it.Name}
		if it.Uni != "" {
			item["unicode"] = emojiHex(it.Uni)
		} else if it.Avatar != "" {
			item["url"] = it.Avatar
		}
		rows = append(rows, emojiRow{ID: it.Name, Item: item, Score: float64(i), EmojiScore: float64(i),
			EmojiBestNameMatch: it.Name, EmojiBestNameMatchCanonical: it.Name, EmojiMatchIndices: emojiMatchIndices(it.Name, q)})
	}
	return rows
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

// fromRows maps the plugins' answer back to composer rows, in the
// plugins' order. known holds every row the natives produced.
func fromRows(kind acKind, rows []acRow, known map[string]acItem) []acItem {
	out := make([]acItem, 0, len(rows))
	seen := map[string]bool{}
	for _, r := range rows {
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

// itemFromRow builds a row a plugin added (e.g. a private channel, a
// hidden emoji with an image of its own).
func itemFromRow(kind acKind, id string, r acRow) (acItem, bool) {
	str := func(k string) string { s, _ := r.Item[k].(string); return s }
	flag := func(k string) bool { b, _ := r.Item[k].(bool); return b }
	name := str("name")
	if kind == acEmoji {
		if name == "" || strings.ContainsAny(name, ": \t\n") {
			return acItem{}, false
		}
		url, uni := str("url"), emojiFromHex(str("unicode"))
		if url == "" && uni == "" {
			if _, text, ok := emoji.Standard(name); ok {
				uni = text
			}
		}
		return acItem{Kind: acEmoji, ID: name, Name: name, Avatar: url, Uni: uni,
			Atom: &Atom{Kind: AtomEmoji, ID: name, Text: ":" + name + ":", URL: url, Uni: uni}}, true
	}
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

// pluginRank runs the patched thunk for t in the background and applies
// the answer if the query is still t's. items are the rows just computed
// (shown meanwhile). Called from Update on the UI goroutine.
func (ac *Autocomplete) pluginRank(t acTrigger, items []acItem) {
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
		return
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
	h, coreEmoji := ac.h, ac.coreEmoji()

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
	channelID, isThread, later, includeAll := ac.channelID, ac.thread, ac.later, ac.ov.includeAll

	go func() {
		ctx := context.Background()
		var rows []acRow
		var err error
		if t.Kind == acUser {
			args := membersArgs{Query: t.Query, ChannelID: channelID, IsThread: isThread, IncludeAllBroadcastKeywords: includeAll}
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
		} else if t.Kind == acEmoji {
			args := emojiArgs{Query: ":" + t.Query, ChannelID: channelID, IsThread: isThread}
			var ers []emojiRow
			ers, err = taut.Dispatch(ctx, tt, name, args, func(_ context.Context, a emojiArgs) ([]emojiRow, error) {
				// the thunk's (query||"").replace(/:|\uFF1A/g, "")
				q := strings.NewReplacer(":", "", "\uff1a", "").Replace(a.Query)
				xs := items
				if q != t.Query && coreEmoji {
					xs = emojiMatchItems(popups.AutocompleteEmoji(h, q, emojiOpts))
				}
				if a.StandardEmojiOnly {
					xs = slices.DeleteFunc(slices.Clone(xs), func(it acItem) bool { return it.Uni == "" })
				}
				if n := cmpOr(a.Limit, 25); len(xs) > n {
					xs = xs[:n]
				}
				remember(xs)
				return toEmojiRows(xs, q), nil
			})
			for _, r := range ers {
				rows = append(rows, acRow{ID: r.ID, Type: "emoji", Item: r.Item})
			}
		} else {
			args := channelsArgs{Query: "#" + t.Query, ChannelID: channelID}
			rows, err = taut.Dispatch(ctx, tt, name, args, func(_ context.Context, a channelsArgs) ([]acRow, error) {
				q := strings.TrimPrefix(strings.TrimSpace(a.Query), "#")
				xs := items
				if q != t.Query {
					xs = chs.rank(q)
				}
				remember(xs)
				return toRows(xs), nil
			})
		}
		if err != nil {
			return // keep the native rows
		}
		mu.Lock()
		out := fromRows(t.Kind, rows, known)
		mu.Unlock()
		later(func() { ac.applyPlugin(t, out) })
	}()
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

// applyPlugin installs the plugins' rows for t if t is still the open
// query (UI goroutine). The selected row stays selected when present.
func (ac *Autocomplete) applyPlugin(t acTrigger, items []acItem) {
	if !ac.active || ac.trig.Kind != t.Kind || ac.trig.Query != t.Query {
		return
	}
	selID := ""
	if !ac.pseudo && ac.sel < len(ac.items) {
		selID = ac.items[ac.sel].ID
	}
	ac.items = ac.ov.keep(items)
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

// keep drops the disallowed rows (in place: items is freshly built).
func (o *acOverrides) keep(items []acItem) []acItem {
	if len(o.disallowed) == 0 {
		return items
	}
	out := items[:0]
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
