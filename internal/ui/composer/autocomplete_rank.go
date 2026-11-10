package composer

import (
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/acrank"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/frecency"
)

// The @ # and / lists are ranked by Slack's Searcher, ported in
// internal/acrank (docs/AUTOCOMPLETE-RULES.md has the rules and the
// evidence). This file turns the composer's sources into candidates and
// the ranked candidates back into rows.

// acNow is the clock for status expiry and ranking (tests replace it).
var acNow = time.Now

// userName is the name Slack shows for u.
func userName(u core.User) string {
	switch {
	case u.Name != "":
		return u.Name
	case u.RealName != "":
		return u.RealName
	}
	return u.Handle
}

// rankCtx is what the ranking reads besides the candidates: the
// frecency pref, the model, the navigation history, the viewer's DMs
// and read state, and the experiments Slack's Searcher checks.
type rankCtx struct {
	frec    *frecency.Cache
	weights acrank.Weights
	history map[string]float64
	ims     map[string]core.Conv // user id → DM
	counts  map[string]core.Count
	starred map[string]bool

	convKind    string // the composer's conversation kind ("" unknown)
	threadUsers map[string]bool

	selfTeams           []string
	entID, team         string
	displayRealNames    bool
	passthrough, demote bool
	beyond, emojiBoost  bool

	// autocomplete_affinity_ml_model on: search.autocomplete.
	// offlineFeatures' user_affinity and channel_priority_90d, which
	// prepSuggestions hands the sort as userAffinityScore and
	// channelPriority90dScore (nil otherwise: the features stay 0)
	affinity, priority90D map[string]float64
}

// expOn reads a client experiment the way Slack's gates do.
func expOn(v string) bool { return v == "on" || strings.HasPrefix(v, "treatment") }

// rankCtxFrom collects the context from core (nil core: the zero ctx).
func rankCtxFrom(api coreAPI, s core.Snapshot, convKind string) rankCtx {
	r := rankCtx{convKind: convKind, ims: map[string]core.Conv{}, counts: s.Counts, starred: map[string]bool{}}
	for _, c := range s.Convs {
		if c.Kind == "im" && c.User != "" {
			r.ims[c.User] = c
		}
		if c.Starred {
			r.starred[c.ID] = true
		}
	}
	if api.a == nil {
		return r
	}
	a := api.a
	r.frec = a.FrecencyCache()
	r.weights = a.AutocompleteModel()
	r.history = a.ViewHistory()
	r.selfTeams = s.Self.Teams
	r.entID = s.EnterpriseID
	r.team = s.Team.ID
	r.displayRealNames = s.Prefs.DisplayRealNames
	r.passthrough = a.Experiment("autocomplete_flannel_fuzzy_match_passthrough") == "treatment"
	r.demote = expOn(a.Experiment("autocomplete_demote_flannel_passthrough_rltv_exact_match"))
	r.beyond = expOn(a.Experiment("ml_fuzzy_match_beyond_word_boundaries_v2"))
	r.emojiBoost = a.Experiment("emoji_exact_match_boost") == "on"
	if a.Experiment("autocomplete_affinity_ml_model") == "on" {
		r.affinity, r.priority90D = a.AutocompleteAffinity()
	}
	return r
}

func (r rankCtx) opts(src string) acrank.Options {
	return acrank.Options{
		Source: src, Limit: acMaxItems, Frecency: r.frec, Weights: r.weights, Now: acNow(),
		HasChannel: r.convKind != "", ChannelIsIM: r.convKind == "im",
		Passthrough: r.passthrough, DemotePassthrough: r.demote, BeyondWordBoundaries: r.beyond,
		EmojiExactBoost: r.emojiBoost, DisplayRealNames: r.displayRealNames,
		EnterpriseID: r.entID, DefaultWorkspace: r.team, SelfTeams: r.selfTeams,
	}
}

func tsF(s string) float64 {
	if s == "" {
		return 0
	}
	var f float64
	for i, c := range s {
		if c == '.' {
			frac, div := 0.0, 1.0
			for _, d := range s[i+1:] {
				if d < '0' || d > '9' {
					break
				}
				frac = frac*10 + float64(d-'0')
				div *= 10
			}
			return f + frac/div
		}
		if c < '0' || c > '9' {
			return f
		}
		f = f*10 + float64(c-'0')
	}
	return f
}

// memberCand is a person or bot as the Searcher sees them.
func (r rankCtx) memberCand(u core.User, s memberSources, remote bool) acrank.Cand {
	_, inCh := s.members[u.ID]
	c := acrank.Cand{
		Kind: acrank.Member, ID: u.ID, Name: u.Handle, Remote: remote,
		RealName: u.RealName, DisplayName: u.DisplayName, FirstName: u.FirstName, LastName: u.LastName,
		Self: u.ID == s.selfID, Bot: u.IsBot || u.AppUser, Deleted: u.Deleted,
		Active:   !u.IsBot && !u.AppUser && !u.Deleted && !u.Guest && u.ID != "USLACKBOT",
		External: u.External, SameTeam: u.TeamID != "" && u.TeamID == s.selfTeam,
		HasPresence: u.Presence != "", InChannel: inCh || (u.ID == s.selfID && r.convKind != ""),
		InThread: r.threadUsers[u.ID], EnterpriseID: u.EnterpriseID, Teams: u.Teams,
		Affinity: r.affinity[u.ID],
	}
	if c.DisplayName == "" && u.DisplaySet {
		c.DisplayName = u.Name
	}
	if im, ok := r.ims[u.ID]; ok {
		c.IM, c.IMPriority, c.IMStarred = true, im.Priority, im.Starred
		cnt := r.counts[im.ID]
		c.IMLatest, c.IMLastRead = tsF(cnt.Latest), tsF(cnt.LastRead)
		c.History = r.history[im.ID]
	}
	return c
}

// rankMembers ranks the @ list: people and bots, usergroups and the
// broadcast keywords the viewer may use here (all of them with
// includeAll).
func (s memberSources) rankMembers(q string, includeAll bool) []acItem {
	r := s.rk
	type src struct {
		u   *core.User
		g   *core.Usergroup
		kw  string
		rem bool
	}
	cands := make([]acrank.Cand, 0, len(s.users)+len(s.groups)+3)
	srcs := make([]src, 0, cap(cands))
	for i := range s.users {
		u := &s.users[i]
		if u.ID == "" {
			continue
		}
		cands = append(cands, r.memberCand(*u, s, s.remote[u.ID]))
		srcs = append(srcs, src{u: u})
	}
	for i := range s.groups {
		g := &s.groups[i]
		cands = append(cands, acrank.Cand{Kind: acrank.UserGroup, ID: g.ID, Name: g.Name, Handle: g.Handle})
		srcs = append(srcs, src{g: g})
	}
	kws := s.keywords
	if includeAll {
		kws = []string{core.KeywordEveryone, core.KeywordChannel, core.KeywordHere}
	}
	for _, k := range kws {
		cands = append(cands, acrank.Cand{Kind: acrank.Keyword, ID: broadcastRowID(k), Name: k, Name2: k})
		srcs = append(srcs, src{kw: k})
	}
	rs := acrank.Rank(q, cands, r.opts(acrank.SourceTexty))
	out := make([]acItem, 0, len(rs))
	for _, res := range rs {
		sr := srcs[res.Index]
		switch {
		case sr.u != nil:
			out = append(out, s.userItem(*sr.u))
		case sr.g != nil:
			out = append(out, groupItem(*sr.g))
		default:
			out = append(out, keywordItem(sr.kw))
		}
	}
	return out
}

func (s memberSources) userItem(u core.User) acItem {
	name := userName(u)
	_, mem := s.members[u.ID]
	it := acItem{
		Kind: acUser, ID: u.ID, Name: name, Secondary: u.RealName, Avatar: u.Avatar,
		Presence: u.Presence, IsBot: u.IsBot, Member: mem, Self: u.ID == s.selfID,
		NotIn: s.members != nil && !mem && u.ID != s.selfID, Deleted: u.Deleted,
		Atom: &Atom{Kind: AtomUser, ID: u.ID, Text: "@" + name},
	}
	if u.StatusExp == 0 || u.StatusExp > acNow().Unix() {
		it.Status, it.StatusURL, it.statusName = u.StatusText, u.StatusEmojiURL, u.StatusEmoji
	}
	return it
}

func groupItem(g core.Usergroup) acItem {
	name := g.Name
	if name == "" {
		name = g.Handle
	}
	return acItem{
		Kind: acUser, ID: g.ID, Name: name, Secondary: g.Handle, Count: g.UserCount,
		Atom: &Atom{Kind: AtomUsergroup, ID: g.ID, Text: "@" + g.Handle},
	}
}

// keywordHint is getBroadcastKeywordDescription.
func keywordHint(k string) string {
	switch k {
	case core.KeywordHere:
		return "Notify every online member in this channel."
	case core.KeywordChannel:
		return "Notify everyone in this channel."
	case core.KeywordEveryone:
		return "Notify everyone in your workspace."
	}
	return ""
}

func keywordItem(k string) acItem {
	return acItem{
		Kind: acUser, ID: k, Name: k, Secondary: keywordHint(k),
		Atom: &Atom{Kind: AtomBroadcast, ID: k, Text: "@" + k},
	}
}

// rankBroadcasts lists the keywords the conversation kind can carry
// (none in DMs; @everyone only in the general channel) that match q.
// Who may use them is memberSources.keywords (BroadcastKeywords).
func rankBroadcasts(q, convKind string, general bool) []acItem {
	if convKind == "im" {
		return nil
	}
	var ks []string
	for _, k := range []string{core.KeywordEveryone, core.KeywordChannel, core.KeywordHere} {
		if k != core.KeywordEveryone || general {
			ks = append(ks, k)
		}
	}
	return memberSources{keywords: ks}.rankMembers(q, false)
}

// rankChannels ranks the # list: the sidebar's channels and the server
// search's hits.
func (s channelSources) rankChannels(q string) []acItem {
	r := s.rk
	cands := make([]acrank.Cand, 0, len(s.convs)+len(s.hits))
	items := make([]acItem, 0, cap(cands))
	seen := map[string]bool{}
	add := func(id, name, kind string, member, archived, remote bool) {
		if id == "" || seen[id] || kind == "im" || kind == "mpim" {
			return
		}
		seen[id] = true
		cnt := r.counts[id]
		cands = append(cands, acrank.Cand{
			Kind: acrank.Channel, ID: id, Name: name, Remote: remote, Private: kind == "private",
			IsMember: member, Archived: archived, Starred: r.starred[id], Unread: cnt.Unread,
			LastRead: tsF(cnt.LastRead), Latest: tsF(cnt.Latest), History: r.history[id],
			Priority90D: r.priority90D[id],
		})
		items = append(items, acItem{
			Kind: acChannel, ID: id, Name: name, Private: kind == "private", Member: member, Archived: archived,
			Atom: &Atom{Kind: AtomChannel, ID: id, Text: "#" + name},
		})
	}
	for _, c := range s.convs {
		add(c.ID, c.Name, c.Kind, true, c.IsArchived, false)
	}
	for _, h := range s.hits {
		add(h.ID, h.Name, h.Kind, h.IsMember, h.IsArchived, true)
	}
	rs := acrank.Rank(strings.TrimPrefix(q, "#"), cands, r.opts(acrank.SourceTexty))
	out := make([]acItem, len(rs))
	for i, res := range rs {
		out[i] = items[res.Index]
	}
	return out
}

// rankCommands ranks the / list (source slash-command-menu: the common
// features only). Names are stored without the leading "/".
func rankCommands(q string, cmds []core.Command, r rankCtx) []acItem {
	cands := make([]acrank.Cand, 0, len(cmds))
	items := make([]acItem, 0, len(cmds))
	seen := map[string]bool{}
	for i := range cmds {
		c := cmds[i]
		name := strings.TrimPrefix(c.Name, "/")
		n := strings.ToLower(name)
		if name == "" || seen[n] {
			continue
		}
		seen[n] = true
		cands = append(cands, acrank.Cand{Kind: acrank.Command, ID: "/" + name, Name: "/" + name, Desc: c.Desc, AppName: c.AppName})
		items = append(items, acItem{Kind: acCommand, ID: name, Name: name, Secondary: c.Desc, Command: &c})
	}
	rs := acrank.Rank(q, cands, r.opts(acrank.SourceSlash))
	out := make([]acItem, len(rs))
	for i, res := range rs {
		out[i] = items[res.Index]
	}
	return out
}

// frecencyID is getResultFrecencyId: the row's item id (commands:
// "/name"; keywords: "BKhere").
func frecencyID(it acItem) string {
	switch {
	case it.Kind == acCommand:
		return "/" + it.ID
	case it.IsBroadcast():
		return broadcastRowID(it.ID)
	}
	return it.ID
}

// recordPick is TextyAutocomplete.handleSelect's recordFrecencyEvent
// {id: the completer's frecency id, text: matchText}; emoji go through
// recordEmojiFrecencyEvent ("E"+unicode or "E"+name).
func (ac *Autocomplete) recordPick(it acItem, t acTrigger) {
	if ac.h == nil || ac.h.Core() == nil {
		return
	}
	trig := map[acKind]string{acUser: "@", acChannel: "#", acEmoji: ":", acCommand: "/"}[t.Kind]
	if it.Kind == acEmoji {
		ac.h.Core().RecordEmojiFrecency(it.ID, trig+t.Query)
		return
	}
	coreAPI{ac.h.Core()}.recordFrecency(frecencyID(it), trig+t.Query)
}
