package composer

import (
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/some-du6e/clack/internal/acrank"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/frecency"
)

// A typed @name or #channel that names exactly one member, user group,
// broadcast keyword or channel becomes its mention at send, as Slack's
// prepareAndSendMessage does before anything reads the delta (boot-shared
// rNCs → tEEs replacePotentialMentionsSynchronously, build 9f450b8ad3db):
//
//   - getPotentialMentionMatches (gcvA, with channel tagging): in every
//     text op outside code, code blocks and links, each "@word" or
//     "#word" after the start or a space (one of {[(<>*_/"“‘': between),
//     up to a space, "'s", "…" or punctuation; trailing punctuation off
//     the label.
//   - searchFunc (rNCs): a #word is the public channels named exactly so
//     (the first one not archived wins, 1GCd); an @word is the broadcast
//     keyword of that name the viewer may use here (none in a thread),
//     else the store's members (self included, not Slackbot) and user
//     groups the autocomplete's search finds, 25 at most.
//   - createPotentialMentionObject (RZMQ): one keyword result is that
//     keyword; otherwise the results' names ("@" and the display name, a
//     group's "@handle", and "@" and the real name of a member with a
//     display name), longest first, while no shorter than the label: a
//     name the text continues with at the match, or the only result's,
//     names its result when the label's first word starts one of its words
//     (8KmV fN) and exactly one result has it (a user group only when the
//     text spells it out); several ties go to the one frecency puts 30
//     points ahead. Anything else stays text.
//   - buildPotentialMentionsDelta (fhSo): each match becomes the mention's
//     label (its display name, the real name under mentions_real_names)
//     with no other formats.
//
// Before it the smart-symbol pass (nC51, when smart quotes and symbols
// are on) puts a text presentation selector after a "↔" outside code so
// it doesn't turn into the emoji.

// slugStore is what rNCs' searchFunc reads, snapshotted on the UI
// goroutine at send.
type slugStore struct {
	users    []core.User
	groups   []core.Usergroup
	channels []core.Conv // public channels
	keywords []string    // the broadcast keywords the viewer may use here (none in a thread)
	selfID   string
	rk       rankCtx
	frec     *frecency.Cache
	// realNames is shouldDisplayRealNamesInMentions (mentions_real_names)
	realNames bool
	// smart: smart quotes and symbols are on (no disable_smart_symbols)
	smart bool
	// find, when set, answers the member search instead (tests)
	find func(q string) []slugHit
}

// slugStore snapshots the store for the composer's conversation.
func (c *Composer) slugStore() slugStore {
	a := c.api.a
	if a == nil {
		return slugStore{}
	}
	s := c.api.snapshot()
	conv, haveConv := c.api.conv(c.ch)
	st := slugStore{frec: a.FrecencyCache(), realNames: a.RealNamesInMentions(), smart: SmartSymbols(a)}
	kind := ""
	if haveConv {
		kind = conv.Kind
	}
	st.rk = rankCtxFrom(c.api, s, kind)
	seen := map[string]bool{}
	add := func(u core.User) {
		if u.ID == "" || u.ID == "USLACKBOT" || seen[u.ID] {
			return
		}
		seen[u.ID] = true
		st.users = append(st.users, u)
	}
	if s.Self.ID != "" {
		st.selfID = s.Self.ID
		add(s.Self)
	}
	for _, u := range a.KnownUsers() {
		add(u)
	}
	for _, id := range sortedKeys(s.Users) {
		add(s.Users[id])
	}
	st.groups = a.StoredUsergroups()
	for _, cv := range s.Convs {
		if cv.Kind == "channel" && !cv.File {
			st.channels = append(st.channels, cv)
		}
	}
	if c.ts == "" && haveConv && a.BroadcastEnabledCached(c.ch) {
		st.keywords = core.BroadcastKeywords(conv, a.BroadcastPerms(c.ch))
	}
	return st
}

// prepareMessage is the document a send or a scheduled send serializes:
// prepareSend's, then Slack's send processor (the smart-symbol pass and
// the mention auto-slugging).
func (c *Composer) prepareMessage(d *Doc) *Doc {
	return processSend(c.prepareSend(d), c.slugStore())
}

// processSend runs the processor over d; d itself when nothing changes.
func processSend(d *Doc, st slugStore) *Doc {
	q, _ := qFromDoc(d)
	out, changed := processQ(q, st)
	if !changed {
		return d
	}
	nd, _ := qToDoc(qNormalize(out))
	return nd
}

// processQ is the processor on a Quill document; changed reports a
// difference.
func processQ(q qDelta, st slugStore) (qDelta, bool) {
	changed := false
	if st.smart {
		if ch := qPresentationSelectors(q); len(ch.ops) > 0 {
			q, changed = q.compose(ch), true
		}
	}
	if out, ok := autoSlug(q, st); ok {
		q, changed = out, true
	}
	return q, changed
}

// qPresentationSelectors is buildAddTextPresentationSelectorsChangeDelta
// (nC51): U+FE0E after each "↔" that no variation selector follows and
// no zero-width joiner leads, outside code and code blocks.
func qPresentationSelectors(q qDelta) qDelta {
	var ch qDelta
	at, last := 0, 0
	qEachOpWithLine(q, func(op qOp, line qAttrs) {
		start := at
		at += op.length()
		if !qTextItem(op) || truthy(op.attrs["code"]) || truthy(line["code-block"]) {
			return
		}
		for i, r := range op.text {
			if r != 0x2194 { // the left right arrow
				continue
			}
			if i > 0 && op.text[i-1] == 0x200d { // after a zero-width joiner
				continue
			}
			if i+1 < len(op.text) && (op.text[i+1] == 0xfe0e || op.text[i+1] == 0xfe0f) {
				continue
			}
			pos := start + i + 1
			ch = ch.retain(pos-last).insertRunes([]rune{0xfe0e}, op.attrs.clone())
			last = pos
		}
	})
	return ch
}

// potentialMention is one getPotentialMentionMatches result.
type potentialMention struct {
	channel      bool   // "#…": a channel, else a member
	text         []rune // the op's text
	matchLength  int    // the match without its prefix punctuation
	label        string // "@name" with trailing punctuation off
	bare         string // the label without its "@" or "#"
	local, index int    // where it starts in the op and in the document
}

// pmBracket is the optional character between the space and the "@":
// [\{|\[|\(|<|>|\*|_|\/|"|“|‘|'|:] (the class holds "|" too).
func pmBracket(r rune) bool {
	return strings.ContainsRune("{|[(<>*_/\"':", r) || r == 0x201c || r == 0x2018
}

// pmPrefix starts a match: @ ＠ # ＃.
func pmPrefix(r rune) bool { return r == '@' || r == 0xff20 || r == '#' || r == 0xff03 }

// pmEnd is the lookahead that ends a match at i: a space, "'s", "‘s",
// "’s", "…", a punctuation mark (uikB Nb and @#`$%^&+=|"/, "_" not one
// with channel tagging) or the end.
func pmEnd(s []rune, i int) bool {
	if i >= len(s) {
		return true
	}
	r := s[i]
	if jsSpace(r) || r == 0x2026 || strings.ContainsRune(".,;:?!])*~>@#`$%^&+=|\"/", r) {
		return true
	}
	return (r == '\'' || r == 0x2018 || r == 0x2019) && i+1 < len(s) && s[i+1] == 's'
}

// pmTrailing is y's class, stripped from the label's end:
// [\.|\s|.,;:?!])*_~>@#`$%^&+=|"/].
func pmTrailing(r rune) bool {
	return jsSpace(r) || strings.ContainsRune(".|,;:?!])*_~>@#`$%^&+=\"/", r)
}

// pmMatchAt is the regexp's match starting at i: where group 1 starts and
// ends.
func pmMatchAt(s []rune, i int) (g0, g1 int, ok bool) {
	var anchors []int
	if i == 0 {
		anchors = append(anchors, 0)
	}
	if i < len(s) && jsSpace(s[i]) {
		anchors = append(anchors, i+1)
	}
	for _, a := range anchors {
		bs := []int{a}
		if a < len(s) && pmBracket(s[a]) {
			bs = []int{a + 1, a}
		}
		for _, b := range bs {
			if b >= len(s) || !pmPrefix(s[b]) {
				continue
			}
			e := b + 1
			for !pmEnd(s, e) {
				if jsLineTerm(s[e]) {
					break // "." stops at a line break (a space anyway)
				}
				e++
			}
			return b, e, true
		}
	}
	return 0, 0, false
}

// runeIndex is String.prototype.indexOf for one rune.
func runeIndex(s []rune, r rune) int {
	for i, x := range s {
		if x == r {
			return i
		}
	}
	return -1
}

// potentialMentions is getPotentialMentionMatches with channel tagging.
func potentialMentions(q qDelta) []potentialMention {
	var out []potentialMention
	at := 0
	qEachOpWithLine(q, func(op qOp, line qAttrs) {
		start := at
		at += op.length()
		if truthy(line["code-block"]) || !qTextItem(op) || truthy(op.attrs["code"]) || truthy(op.attrs["link"]) {
			return
		}
		s := op.text
		for p := 0; p < len(s); {
			found := false
			for i := p; i < len(s); i++ {
				g0, g1, ok := pmMatchAt(s, i)
				if !ok {
					continue
				}
				found = true
				full := s[i:g1]
				l := i
				if d := runeIndex(full, '@'); d > 0 {
					l += d
				}
				if u := runeIndex(full, 0xff20); u > 0 {
					l += u
				}
				if e := runeIndex(full, '#'); e > 0 {
					l += e
				}
				if t := runeIndex(full, 0xff03); t > 0 {
					l += t
				}
				r := s[g0:g1]
				j := len(r)
				for j > 0 && pmTrailing(r[j-1]) {
					j--
				}
				label := string(r[:j])
				bare := label
				if lr := []rune(label); len(lr) > 0 && pmPrefix(lr[0]) {
					bare = string(lr[1:])
				}
				if bare != "" {
					out = append(out, potentialMention{channel: r[0] == '#' || r[0] == 0xff03, text: s, matchLength: len(r),
						label: label, bare: bare, local: l, index: l + start})
				}
				p = g1
				break
			}
			if !found {
				break
			}
		}
	})
	return out
}

// slugHit is one searchFunc result as RZMQ reads it.
type slugHit struct {
	id      string
	keyword bool   // is_broadcast_keyword
	group   bool   // is_usergroup
	name    string // a keyword's name, a group's handle, a member's getMemberMentionName
	display string // a member's profile display_name (getMemberProfileFieldDisplayName)
	real    string // a member's real name
}

// kx is getDisplayTextForSearchResult.
func (h slugHit) kx() string { return "@" + h.name }

// kp is getSecondaryNameForSearchResult: "@" and the real name of a
// member with a display name.
func (h slugHit) kp() string {
	if h.keyword || h.group || h.display == "" || h.real == "" {
		return ""
	}
	return "@" + h.real
}

// slugNormalize is 8KmV normalizeName: i18n_deburr, then lowercase.
func slugNormalize(s string) string { return jsToLower(acrank.Deburr(s)) }

// slugGoodEnough is isNameMatchGoodEnoughForRealMention (8KmV fN): one
// of the name's words (split on " ._", each up to its first "-") is the
// label's first word (split on " -._").
func slugGoodEnough(v, label string) bool {
	trim := func(s string) string {
		if r := []rune(s); len(r) > 0 && (r[0] == '@' || r[0] == '\uff20') {
			return string(r[1:])
		}
		return s
	}
	is := jsSplitAny(slugNormalize(trim(v)), " ._")
	rs := jsSplitAny(slugNormalize(trim(label)), " -._")
	for _, p := range is {
		if strings.SplitN(p, "-", 2)[0] == rs[0] {
			return true
		}
	}
	return false
}

// jsSplitAny is String.prototype.split on a one-character class: empty
// pieces kept ("" is [""]).
func jsSplitAny(s, seps string) []string {
	var out []string
	start := 0
	for i, r := range s {
		if strings.ContainsRune(seps, r) {
			out = append(out, s[start:i])
			start = i + utf8.RuneLen(r)
		}
	}
	return append(out, s[start:])
}

// unitsToRunes is how many runes of text from start make n UTF-16 units.
func unitsToRunes(text []rune, start, n int) int {
	runes, units := 0, 0
	for i := start; i < len(text) && units < n; i++ {
		runes++
		units += max(utf16.RuneLen(text[i]), 1)
	}
	return runes
}

// slugSubstr is text.substr(start, n) in UTF-16 units.
func slugSubstr(text []rune, start, n int) string {
	u := utf16.Encode(text)
	us := utf16.Encode(text[:min(start, len(text))])
	from := len(us)
	to := min(from+n, len(u))
	if from > len(u) {
		return ""
	}
	return string(utf16.Decode(u[from:to]))
}

// slugMention is a potential mention resolved: what replaces the match.
type slugMention struct {
	atom          *Atom
	index, length int
}

// memberMention is createPotentialMentionObject; ok false for
// UNVERIFIED (the text stays).
func (st slugStore) memberMention(pm potentialMention, g []slugHit) (slugMention, bool) {
	label := pm.label
	mk := func(h slugHit, length int) slugMention {
		lbl := h.kx()
		if st.realNames && !h.group && !h.keyword {
			lbl = "@" + h.real
		}
		return slugMention{atom: hitAtom(h, lbl), index: pm.index, length: length}
	}
	if len(g) == 1 && g[0].keyword {
		return slugMention{atom: hitAtom(g[0], g[0].kx()), index: pm.index, length: pm.matchLength}, true
	}
	var k []string
	seen := map[string]bool{}
	for _, f := range []func(slugHit) string{slugHit.kx, slugHit.kp} {
		for _, h := range g {
			v := slugNormalize(f(h))
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			k = append(k, v)
		}
	}
	sort.SliceStable(k, func(i, j int) bool { return jsLen(k[i]) > jsLen(k[j]) })
	for _, v := range k {
		if jsLen(label) > jsLen(v) {
			break
		}
		spelled := slugNormalize(v) == slugNormalize(slugSubstr(pm.text, pm.local, jsLen(v)))
		if !spelled && len(g) > 1 {
			continue
		}
		var hits []slugHit
		for _, h := range g {
			if v == slugNormalize(h.kx()) || v == slugNormalize(h.kp()) {
				hits = append(hits, h)
			}
		}
		length := unitsToRunes(pm.text, pm.local, jsLen(label))
		if len(g) != 1 || spelled {
			length = unitsToRunes(pm.text, pm.local, jsLen(v))
		}
		good := slugGoodEnough(v, label)
		if good && len(hits) == 1 && (spelled || !hits[0].group) {
			return mk(hits[0], length), true
		}
		if good && len(hits) > 1 && st.frec != nil {
			ids := make([]string, len(hits))
			for i, h := range hits {
				ids[i] = h.id
			}
			now := time.Now()
			sc := st.frec.ScoreItems(st.frec.Hits(label, now), ids, now, nil)
			frecency.SortItems(sc)
			if len(sc) > 1 && sc[0].RawFrecencyScore-sc[1].RawFrecencyScore >= 30 {
				return mk(hits[sc[0].Index], length), true
			}
		}
		if spelled {
			return slugMention{}, false // a maybe-mention: UNVERIFIED
		}
	}
	return slugMention{}, false
}

// hitAtom is the mention's atom over label.
func hitAtom(h slugHit, label string) *Atom {
	switch {
	case h.keyword:
		return &Atom{Kind: AtomBroadcast, ID: strings.TrimPrefix(h.id, "BK"), Text: label}
	case h.group:
		return &Atom{Kind: AtomUsergroup, ID: h.id, Text: label}
	}
	return &Atom{Kind: AtomUser, ID: h.id, Text: label}
}

// search is rNCs' searchFunc for a member query: the keyword, else the
// members and user groups the autocomplete's search ranks (25 at most).
func (st slugStore) search(q string) []slugHit {
	for _, k := range st.keywords {
		if k == q || acrank.Deburr(k) == q {
			return []slugHit{{id: broadcastRowID(k), keyword: true, name: k}}
		}
	}
	if st.find != nil {
		return st.find(q)
	}
	cands := make([]acrank.Cand, 0, len(st.users)+len(st.groups))
	hits := make([]slugHit, 0, cap(cands))
	ms := memberSources{selfID: st.selfID, rk: st.rk}
	for _, u := range st.users {
		cands = append(cands, st.rk.memberCand(u, ms, false))
		disp := ""
		if u.DisplaySet {
			disp = u.Name
		}
		hits = append(hits, slugHit{id: u.ID, name: u.Name, display: disp, real: u.RealName})
	}
	for _, g := range st.groups {
		cands = append(cands, acrank.Cand{Kind: acrank.UserGroup, ID: g.ID, Name: g.Name, Handle: g.Handle})
		hits = append(hits, slugHit{id: g.ID, group: true, name: g.Handle})
	}
	o := st.rk.opts(acrank.SourceTexty)
	o.Limit = acMaxItems
	var out []slugHit
	for _, r := range acrank.Rank(q, cands, o) {
		out = append(out, hits[r.Index])
	}
	return out
}

// channelMention is createPotentialMentionChannelObject over the public
// channels named exactly so: the first one not archived, else the first.
func (st slugStore) channelMention(pm potentialMention) (slugMention, bool) {
	var first, open *core.Conv
	for i := range st.channels {
		c := &st.channels[i]
		if c.Name != pm.bare {
			continue
		}
		if first == nil {
			first = c
		}
		if open == nil && !c.IsArchived {
			open = c
		}
	}
	u := open
	if u == nil {
		u = first
	}
	if u == nil {
		return slugMention{}, false
	}
	return slugMention{atom: &Atom{Kind: AtomChannel, ID: u.ID, Text: "#" + u.Name}, index: pm.index, length: pm.matchLength}, true
}

// autoSlug is replacePotentialMentionsSynchronously: q with every
// resolved match replaced by its mention; ok false when none was.
func autoSlug(q qDelta, st slugStore) (qDelta, bool) {
	var ms []slugMention
	for _, pm := range potentialMentions(q) {
		var m slugMention
		var ok bool
		if pm.channel {
			m, ok = st.channelMention(pm)
		} else {
			g := st.search(pm.bare)
			if len(g) == 0 {
				continue
			}
			m, ok = st.memberMention(pm, g)
		}
		if ok {
			ms = append(ms, m)
		}
	}
	if len(ms) == 0 {
		return q, false
	}
	sort.SliceStable(ms, func(i, j int) bool { return ms[i].index < ms[j].index })
	var ch qDelta
	n := 0
	for i, m := range ms {
		if d := m.index - n; d > 0 {
			ch = ch.retain(d)
		}
		ch = ch.delete(m.length)
		ch = ch.insert(m.atom.Text, qAttrs{"slackmention": qAtomRef{a: m.atom, seq: 1<<30 + i}})
		n = m.index + m.length
	}
	return q.compose(ch), true
}
