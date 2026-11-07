package composer

import (
	"context"
	"strings"
	"sync"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

// Slack's autocompleteMembers / autocompleteChannels thunks for plugins
// (NativeAtChannel filters @channel/@here, PrivateChannel merges private
// channels it found elsewhere).
//
// They are sync thunks: when patched, their native runs on the JS thread,
// so it must be pure and quick. The composer therefore shows its own rows
// at once, dispatches in the background with a native that ranks a
// snapshot of the sources taken at dispatch time (with the args the
// plugins pass, e.g. includeAllBroadcastKeywords), and swaps in the
// plugins' answer on the UI goroutine. Rows map back by id: rows the
// composer knows keep their avatars, presence and status; new ones are
// built from the result's item.

// memberSources is everything ranking an @ query needs, copied out from
// under ac.mu so it can be ranked on any goroutine.
type memberSources struct {
	users    []core.User
	boost    map[string]bool
	members  map[string]core.User
	selfID   string
	groups   []core.Usergroup
	convKind string
	general  bool
}

// memberSourcesLocked snapshots the @ sources; ac.mu must be held. The
// maps aren't copied: async loads replace them wholesale (ac.land), they
// are never written in place.
func (ac *Autocomplete) memberSourcesLocked() memberSources {
	src := memberSources{
		users:   make([]core.User, 0, len(ac.known)+len(ac.members)+len(ac.searched)),
		boost:   map[string]bool{},
		members: ac.members,
		selfID:  ac.selfID,
		groups:  ac.groups,
	}
	seen := map[string]bool{}
	add := func(u core.User) {
		if u.ID == "" || seen[u.ID] {
			return
		}
		seen[u.ID] = true
		src.users = append(src.users, u)
	}
	for id, u := range ac.members {
		src.boost[id] = true
		add(u)
	}
	for id := range ac.dmUsers {
		src.boost[id] = true
	}
	for _, u := range ac.known {
		add(u)
	}
	for _, u := range ac.searched {
		add(u)
	}
	if ac.haveConv {
		src.convKind, src.general = ac.conv.Kind, ac.conv.IsGeneral
	}
	return src
}

// rank is the composer's @ ranking. includeAll offers every broadcast
// keyword (Slack's includeAllBroadcastKeywords), @everyone included.
func (s memberSources) rank(q string, includeAll bool) []acItem {
	items := rankUsers(q, s.users, s.boost, s.members, s.selfID, s.groups, acMaxItems)
	kind, general := s.convKind, s.general
	if includeAll {
		kind, general = "channel", true
	}
	return append(items, rankBroadcasts(q, kind, general)...)
}

// channelSources is what ranking a # query needs.
type channelSources struct {
	convs []core.Conv
	hits  []core.ChannelHit
}

func (ac *Autocomplete) channelSourcesLocked() channelSources {
	hits := make([]core.ChannelHit, 0, len(ac.chanHits))
	for _, h := range ac.chanHits {
		hits = append(hits, h)
	}
	return channelSources{convs: ac.convs, hits: hits}
}

func (s channelSources) rank(q string) []acItem { return rankChannels(q, s.convs, s.hits, acMaxItems) }

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

// rowKey is the id a composer row answers to in Slack's results.
func rowKey(it *acItem) string {
	if it.IsBroadcast() {
		return broadcastRowID(it.ID)
	}
	return it.ID
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

// pluginRank runs the patched thunk for t in the background and applies
// the answer if the query is still t's. items are the rows just computed
// (shown meanwhile). Called from Update on the UI goroutine.
func (ac *Autocomplete) pluginRank(t acTrigger, items []acItem) {
	tt := currentTaut()
	name := thACMembers
	if t.Kind == acChannel {
		name = thACChannels
	}
	if t.Kind != acUser && t.Kind != acChannel || !tt.ThunkPatched(name) || ac.later == nil {
		return
	}
	ac.mu.Lock()
	var mem memberSources
	var chs channelSources
	if t.Kind == acUser {
		mem = ac.memberSourcesLocked()
	} else {
		chs = ac.channelSourcesLocked()
	}
	ac.mu.Unlock()

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
