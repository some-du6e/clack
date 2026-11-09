package acrank

import (
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/some-du6e/yoloslackgoclient/internal/frecency"
)

// Kind is a candidate's entity type.
type Kind uint8

const (
	Member    Kind = iota // people and bots ("member")
	UserGroup             // "userGroup"
	Keyword               // @here @channel @everyone ("broadcastKeyword")
	Channel               // public and private channels ("channel")
	Emoji                 // "emoji"
	Command               // slash commands ("slashCommand")
)

// Sources (sort.source; the model's feature lists depend on it).
const (
	SourceTexty = "texty-autocomplete"
	SourceSlash = "slash-command-menu"
)

// Cand is one candidate. Only the fields of its Kind matter.
type Cand struct {
	Kind Kind
	ID   string // user/usergroup/channel id; "BKhere"; emoji frecency id ("E1f44d", "Ecustom"); command id
	// Remote: it came from the server search (flannel) for this query;
	// with Options.Passthrough a fuzzy miss still shows at score 2.
	Remote bool

	// Name is item.name: a member's username, a channel's name, a
	// usergroup's name, a keyword's name, an emoji's canonical name, a
	// command's name with its "/".
	Name string

	// members
	RealName, DisplayName string // profile real_name and display_name
	FirstName, LastName   string
	Self, Bot             bool
	// Active is isActiveMember: a full member (not a bot, guest,
	// deactivated, invited or forgotten user).
	Active             bool
	Deleted, Suspended bool
	External, SameTeam bool
	HasPresence        bool // the member object has presence (the +25 channel-member bonus needs it)
	InChannel          bool // known member of the current conversation
	InThread           bool // replied in the current thread
	EnterpriseID       string
	Teams              []string // enterprise_user.teams
	IM                 bool     // the viewer has a DM with them
	IMLatest           float64  // the DM's latest message ts (0 none)
	IMLastRead         float64  // the DM's last_read ts (0 none)
	IMPriority         float64
	IMStarred          bool
	Affinity           float64 // offline features user_affinity

	// usergroups
	Handle string

	// keywords: Name is the keyword ("here"), Name2 its ms_name
	Name2 string

	// channels
	Private, IsMember, Archived, Starred, Unread, OtherTeam bool
	LastRead, Latest                                        float64 // ts
	PrevNames                                               []string
	Priority90D                                             float64
	// History is when the viewer last had the conversation (a channel, or
	// a member's DM) open, unix seconds (0: not in the history).
	History float64

	// emoji: Names are the canonical name and aliases
	Names    []string
	EmojiCtr float64 // offline features user_ctr_30dd (0: Slack's default)

	// commands
	Desc, AppName string
	Aliases       []string
}

// Options are the Searcher options (getAllOptions merged with the thunk's).
type Options struct {
	Source   string
	Limit    int             // 25
	Frecency *frecency.Cache // the "frecency" pref (nil: empty)
	Weights  Weights         // nil: FallbackWeights
	Now      time.Time

	// HasChannel: the composer has a current conversation (sort.
	// currentChannel); ChannelIsIM drops the channel-membership features.
	HasChannel, ChannelIsIM bool
	// Passthrough is autocomplete_flannel_fuzzy_match_passthrough
	// (treatment); DemotePassthrough autocomplete_demote_flannel_
	// passthrough_rltv_exact_match.
	Passthrough, DemotePassthrough bool
	BeyondWordBoundaries           bool // ml_fuzzy_match_beyond_word_boundaries_v2
	EmojiExactBoost                bool // emoji_exact_match_boost: exact emoji +300, not +100
	DisplayRealNames               bool // the display_real_names pref (tie-break name)

	// the viewer's Grid membership (MEMBER_OF_SEARCHER_DEFAULT_TEAM…)
	EnterpriseID, DefaultWorkspace string
	SelfTeams                      []string
}

// Result is a ranked candidate.
type Result struct {
	Index         int // into the candidates
	Score         float64
	FrecencyScore float64
	ModelScore    float64
	Exact         bool
	Passthrough   bool
	// BestName is an emoji's best matching name (emojiBestNameMatch: an
	// alias when the alias matched better).
	BestName string
}

// frecScore is what scoreItems adds to a result (frecency.Scored).
type frecScore struct {
	raw, score               float64
	weightHit, weightItem    float64
	hitCount, itemCount      float64
	logHitScaled, logItemSca float64
	origIndex                int
}

// entity CTR defaults (AutocompleteFeatures, used while the offline
// features are empty, as they are for Hack Club).
var entityCtr = map[Kind]float64{Member: .132, Channel: .148, Emoji: .094}

const otherCtr = .179

type entry struct {
	c   *Cand
	idx int

	score       float64
	exact       bool
	emojiExact  bool
	bestExact   bool // commands: bestNameIsExactMatch
	passthrough bool
	prevOnly    bool // bestPreviousNameMatch
	emailOnly   bool

	emojiScore float64
	emojiRatio float64
	emojiCombo int // -1: none
	emojiBest  string

	sortName  string
	firstWord string // commands: firstWordOfRawQuery
	fs        frecScore
	bonus     map[string]float64
	model     float64
	inChannel bool // isMemberOfCurrentChannel (prepSuggestions)
}

// Rank is Searcher.search for one query over cands: match
// (SearcherMatcher), group and fuzzy-sort (SearcherSorter), frecency,
// the linear model (Sorter) on the top 3×limit, the exact-match boost,
// then the limit. query is what the Searcher gets (the trigger already
// dropped: "kar", "gen", "smi", "rem").
func Rank(query string, cands []Cand, o Options) []Result {
	if o.Limit <= 0 {
		o.Limit = 25
	}
	if o.Weights == nil {
		o.Weights = FallbackWeights
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	m := SearcherQuery(query)
	q := Normalize(m)
	mo := MatcherOpts{FuzzyLimit: 3, FuzzyJumpLimit: 20, FuzzyCJK: true}
	matcher := NewMatcher(m, mo)
	mc := matcher
	if o.BeyondWordBoundaries {
		bo := mo
		bo.BeyondWordBoundaries = true
		mc = NewMatcher(m, bo)
	}

	var groups [3][]*entry // SearcherSorter: x, U, archived
	for i := range cands {
		c := &cands[i]
		e := &entry{c: c, idx: i, emojiCombo: -1}
		ok := false
		switch c.Kind {
		case Member:
			ok = matchMember(e, q, m, mc, o)
		case UserGroup:
			ok = matchUserGroup(e, q, matcher)
		case Keyword:
			ok = matchKeyword(e, q, matcher)
		case Channel:
			ok = matchChannel(e, q, m, mc, o)
		case Emoji:
			ok = matchEmoji(e, q)
		case Command:
			ok = matchCommand(e, q, matcher)
		}
		if !ok {
			continue
		}
		e.sortName = sortName(c, o)
		g := 0
		switch {
		case c.Kind == Channel && c.Archived:
			g = 2
		case c.Kind == Channel && !c.IsMember, c.Kind == Command:
			g = 1
		}
		groups[g] = append(groups[g], e)
	}
	// group order inside x is members, member channels, emoji,
	// usergroups, keywords (the stable sort keeps it for full ties)
	sortGroup := func(es []*entry) {
		sort.SliceStable(es, func(i, j int) bool { return kindOrder(es[i].c.Kind) < kindOrder(es[j].c.Kind) })
		sort.SliceStable(es, func(i, j int) bool {
			a, b := es[i], es[j]
			if a.score != b.score {
				return a.score < b.score
			}
			return collate(a.sortName, b.sortName) < 0
		})
	}
	var list []*entry
	for _, g := range groups {
		sortGroup(g)
		list = append(list, g...)
	}
	if len(list) == 0 {
		return nil
	}
	// preferExactMatch
	list = partition(list, func(e *entry) bool { return e.exact })

	// frecency (modelOnly when fewer than 3×limit: score, don't reorder)
	cut := min(o.Limit*3, 500)
	ids := make([]string, len(list))
	for i, e := range list {
		ids[i] = e.c.ID
	}
	sums := make(map[string]float64, len(list))
	for _, e := range list {
		e.bonus = bonusPoints(e, m, o)
		t := 0.0
		for _, v := range e.bonus {
			t += v
		}
		sums[e.c.ID] = t
	}
	fc := o.Frecency
	if fc == nil {
		fc = frecency.Parse(nil)
	}
	scored := fc.ScoreItems(fc.Hits(m, o.Now), ids, o.Now, func(id string) float64 { return sums[id] })
	for i, sc := range scored {
		list[i].fs = frecScore{raw: sc.RawFrecencyScore, score: sc.FrecencyScore, weightHit: sc.WeightHitVisits,
			weightItem: sc.WeightItemVisits, hitCount: float64(sc.HitCount), itemCount: float64(sc.ItemCount),
			logHitScaled: sc.LogHitCountScaled, logItemSca: sc.LogItemCountScaled, origIndex: sc.Index}
	}
	if len(list) >= cut {
		sort.SliceStable(list, func(i, j int) bool {
			a, b := list[i].fs, list[j].fs
			if a.score == b.score {
				return a.origIndex < b.origIndex
			}
			return a.score > b.score
		})
	}
	if len(list) > cut {
		list = list[:cut]
	}

	// the model
	nq := Normalize(FormatQuery(m))
	now := float64(o.Now.UnixMilli()) / 1000
	for _, e := range list {
		e.model = modelScore(e, m, nq, len(list), now, o)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].model > list[j].model })

	// postProcess: boostExactMatches(2), demoteFlannelPassthroughResults
	if len(list) > 2 {
		head := list[:2:2]
		rest := partition(append([]*entry(nil), list[2:]...), func(e *entry) bool { return e.exact || e.bestExact })
		list = append(head, rest...)
	}
	if o.DemotePassthrough {
		list = demotePassthrough(list, nq)
	}
	if len(list) > o.Limit {
		list = list[:o.Limit]
	}
	out := make([]Result, len(list))
	for i, e := range list {
		out[i] = Result{Index: e.idx, Score: e.score, FrecencyScore: e.fs.score, ModelScore: e.model, Exact: e.exact, Passthrough: e.passthrough, BestName: e.emojiBest}
	}
	return out
}

func kindOrder(k Kind) int {
	switch k {
	case Member:
		return 0
	case Channel:
		return 1
	case Emoji:
		return 2
	case UserGroup:
		return 3
	case Keyword:
		return 4
	}
	return 5
}

// partition moves the entries keep accepts to the front, stably.
func partition(es []*entry, keep func(*entry) bool) []*entry {
	out := make([]*entry, 0, len(es))
	for _, e := range es {
		if keep(e) {
			out = append(out, e)
		}
	}
	for _, e := range es {
		if !keep(e) {
			out = append(out, e)
		}
	}
	return out
}

func demotePassthrough(es []*entry, nq string) []*entry {
	any := false
	for _, e := range es {
		any = any || e.passthrough
	}
	t := dropSigil(nq)
	if !any || t == "" {
		return es
	}
	last := -1
	for i, e := range es {
		if e.passthrough {
			continue
		}
		n1 := dropSigil(Normalize(featName(e)))
		n2 := dropSigil(Normalize(e.c.Name))
		if (n1 != "" && strings.Contains(n1, t)) || (n2 != "" && strings.Contains(n2, t)) {
			last = i
		}
	}
	if last < 0 {
		return es
	}
	out := make([]*entry, 0, len(es))
	for i := 0; i <= last; i++ {
		if !es[i].passthrough {
			out = append(out, es[i])
		}
	}
	for i, e := range es {
		if e.passthrough || i > last {
			out = append(out, e)
		}
	}
	return out
}

func lowerDeburr(s string) string { return Deburr(strings.ToLower(s)) }

// matchMember is filterMembers/scoreMember: the lowest score of the real
// name and the display name ("me"/"you" match the viewer at 0). The
// username isn't matched.
func matchMember(e *entry, q, raw string, mc *Matcher, o Options) bool {
	c := e.c
	if c.Deleted && c.Bot {
		return false // excludeDeletedBots
	}
	if q == "" {
		e.score = 0
		return true
	}
	if (q == "me" || q == "you") && c.Self {
		e.score = 0
		return true
	}
	s := Inf
	if r := lowerDeburr(c.RealName); r != "" {
		s = math.Min(s, mc.Score(r))
	}
	if d := lowerDeburr(c.DisplayName); d != "" {
		s = math.Min(s, mc.Score(d))
	}
	e.score = s
	if s <= 3 {
		return true
	}
	if o.Passthrough && c.Remote {
		e.score, e.passthrough = 2, true
		return true
	}
	return false
}

// matchUserGroup scores the name (lowercased) and the handle.
func matchUserGroup(e *entry, q string, m *Matcher) bool {
	c := e.c
	if q == "" {
		return true
	}
	s := m.NormalizeAndScore(strings.ToLower(c.Name))
	if c.Handle != "" {
		s = math.Min(s, m.NormalizeAndScore(c.Handle))
	}
	e.score = s
	e.exact = Normalize(c.Handle) == q || Normalize(c.Name) == q
	return s <= 3
}

// matchKeyword: name and ms_name; @channel also answers to "group".
func matchKeyword(e *entry, q string, m *Matcher) bool {
	c := e.c
	if q == "" {
		return true
	}
	s := m.NormalizeAndScore(c.Name)
	if c.ID == "BKchannel" {
		s = math.Min(s, m.NormalizeAndScore("group"))
	}
	if c.Name2 != "" && c.Name2 != c.Name {
		s = math.Min(s, m.NormalizeAndScore(c.Name2))
	}
	e.score = s
	e.exact = Normalize(c.Name) == q || (c.Name2 != "" && Normalize(c.Name2) == q)
	return s <= 3
}

// matchChannel is filterChannels with searchPreviousNames.
func matchChannel(e *entry, q, raw string, mc *Matcher, o Options) bool {
	c := e.c
	if q == "" {
		return true
	}
	n := Normalize(c.Name)
	s := mc.Score(n)
	e.score = s
	e.exact = n == q
	prevOK := false
	best, bestS := "", Inf
	for _, p := range c.PrevNames {
		if strings.HasPrefix(p, "mpdm-") {
			continue
		}
		ps := mc.NormalizeAndScore(p)
		if ps <= 3 {
			prevOK = true
		}
		if ps < bestS {
			best, bestS = p, ps
		}
	}
	if prevOK && s > 3 && best != "" {
		e.prevOnly = true
	}
	switch {
	case s <= 3 || prevOK:
		return true
	case o.Passthrough && c.Remote:
		e.score, e.passthrough = 2, true
		return true
	case raw == c.ID:
		e.score = 0
		return true
	}
	return false
}

// matchCommand is filterShortcuts/scoreShortcut for slash commands: the
// name (one jump for the "/" forgiven), label, description, app name and
// aliases; an exact name ("/remind" for "remind") is an exact match.
func matchCommand(e *entry, q string, m *Matcher) bool {
	c := e.c
	if q == "" {
		return true
	}
	l := Normalize(c.Name)
	s := m.Score(l)
	if !math.IsInf(s, 1) && s > 0 {
		s--
	}
	s = math.Min(s, m.NormalizeAndScore(c.Desc))
	s = math.Min(s, m.NormalizeAndScore(c.AppName))
	exact := false
	for _, a := range c.Aliases {
		t := FormatQuery(Normalize(a))
		as := m.Score(t)
		s = math.Min(s, as)
		if q == t {
			exact = true
		}
	}
	if q == FormatQuery(l) {
		exact = true
	}
	e.score = s
	e.bestExact = exact
	e.firstWord = strings.Fields(q + " ")[0]
	// commandMatch: the first word names the command (isOpenedWithSlash)
	fw := "/" + e.firstWord
	if fw == l {
		e.exact = true
	}
	for _, a := range c.Aliases {
		if Normalize(a) == fw {
			e.exact = true
		}
	}
	e.exact = e.exact || exact
	return s <= 3 || e.exact
}

func sortName(c *Cand, o Options) string {
	switch c.Kind {
	case Member:
		if !o.DisplayRealNames && c.DisplayName != "" {
			return c.DisplayName
		}
		return c.RealName
	case UserGroup:
		if c.Handle != "" {
			return c.Handle
		}
	}
	return c.Name
}

// featName is prepSuggestions' name: a member's mention name (display
// name, else real name), an emoji's best matching name, a command
// without its "/", else item.name.
func featName(e *entry) string {
	c := e.c
	switch c.Kind {
	case Member:
		if c.DisplayName != "" {
			return c.DisplayName
		}
		return c.RealName
	case Emoji:
		return e.emojiBest
	case Command:
		if len(c.Name) > 0 {
			return c.Name[1:]
		}
	}
	return c.Name
}

// bonusPoints is getFrecencyBonusPointsFunction for the texty sources.
func bonusPoints(e *entry, query string, o Options) map[string]float64 {
	c := e.c
	b := map[string]float64{}
	gone := (c.Kind == Channel && c.Archived) || (c.Kind == Member && (c.Deleted || c.Suspended))
	switch {
	case e.exact:
		b["isExactMatch"] = 100
		if e.emojiExact {
			b["isEmojiExactMatch"] = 100
			if o.EmojiExactBoost {
				b["isEmojiExactMatch"] = 300
			}
		}
	case c.Kind == Emoji && e.emojiScore != 0 && !math.IsInf(e.emojiScore, 0):
		b["emojiScoreBonus"] = Similarity(query, c.Name) * 150
	case !gone:
		b["fuzzyScoreBonus"] = contFuzzy(e, query) * 50
	}
	if e.prevOnly {
		b["onlyMatchesPreviousName"] = -50
	}
	if c.Kind == Member && o.HasChannel && c.HasPresence && e.c.InChannel && !o.ChannelIsIM {
		b["isMemberOfThisChannel"] = 25
	}
	if (c.Kind == Channel && c.Starred) || (c.Kind == Member && c.IMStarred) {
		b["isStarred"] = 10
	}
	if c.Kind == Emoji && (c.Name == "thumbsup" || c.Name == "point_up") {
		b["isPopularEmojiVariant"] = 1
	}
	if c.Kind == Channel {
		if c.Archived {
			b["isArchived"] = -50
		}
		if !c.Private && !c.Archived && !c.IsMember {
			b["isNotChannelMember"] = -25
		}
		if c.OtherTeam {
			b["isChannelFromAnotherTeam"] = -15
		}
	}
	if c.Kind == Member && (c.Deleted || c.Suspended) {
		b["isDeactivatedMember"] = -50
	}
	if c.Kind == UserGroup || c.Kind == Keyword {
		b["isUserGroupOrKeyword"] = -25
	}
	if c.Kind == Member && c.Affinity > 0 {
		b["userAffinity"] = 50
	}
	if c.Kind == Channel && c.Priority90D > 0 {
		b["channelPriority90D"] = 50
	}
	return b
}

// contFuzzy is calculateContinuousFuzzyBonusPoints without the factor:
// members take the better of real and display name (lowercased; the
// username when one is empty).
func contFuzzy(e *entry, query string) float64 {
	c := e.c
	if c.Kind != Member {
		return Similarity(query, c.Name)
	}
	safe := func(s string) string {
		if strings.TrimSpace(s) != "" {
			return s
		}
		return c.Name
	}
	r, d := safe(lowerDeburr(c.RealName)), safe(lowerDeburr(c.DisplayName))
	if r == d {
		return Similarity(query, r)
	}
	return math.Max(Similarity(query, r), Similarity(query, d))
}

// collate approximates Intl.Collator("en-US", {numeric: true}).compare:
// case and accents only break ties, digit runs compare as numbers, and
// punctuation sorts before digits and letters.
func collate(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return -1
	}
	if b == "" {
		return 1
	}
	if c := collateKey(Deburr(strings.ToLower(a)), Deburr(strings.ToLower(b))); c != 0 {
		return c
	}
	// tertiary: lowercase before uppercase
	ra, rb := []rune(a), []rune(b)
	for i := 0; i < len(ra) && i < len(rb); i++ {
		if ra[i] != rb[i] {
			if unicode.IsLower(ra[i]) && unicode.IsUpper(rb[i]) {
				return -1
			}
			if unicode.IsUpper(ra[i]) && unicode.IsLower(rb[i]) {
				return 1
			}
			if ra[i] < rb[i] {
				return -1
			}
			return 1
		}
	}
	return len(ra) - len(rb)
}

func collateKey(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		ca, cb := ra[i], rb[j]
		if unicode.IsDigit(ca) && unicode.IsDigit(cb) {
			si, sj := i, j
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}
			na := strings.TrimLeft(string(ra[si:i]), "0")
			nb := strings.TrimLeft(string(rb[sj:j]), "0")
			if len(na) != len(nb) {
				return len(na) - len(nb)
			}
			if na != nb {
				return strings.Compare(na, nb)
			}
			continue
		}
		wa, wb := primary(ca), primary(cb)
		if wa != wb {
			if wa < wb {
				return -1
			}
			return 1
		}
		i++
		j++
	}
	return (len(ra) - i) - (len(rb) - j)
}

// primary orders whitespace, then punctuation (CLDR's order for the
// ASCII set), symbols, digits, letters.
func primary(r rune) int {
	const punct = "_-,;:!?.'\"()[]{}@*/\\&#%`^+<=>|~$"
	switch {
	case unicode.IsSpace(r):
		return 0
	case strings.ContainsRune(punct, r):
		return 1 + strings.IndexRune(punct, r)
	case unicode.IsDigit(r):
		return 100 + int(r-'0')
	case unicode.IsLetter(r):
		return 200 + int(r)
	}
	return 50 + int(r)%40
}
