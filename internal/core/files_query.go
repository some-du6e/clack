package core

// The Files browser's request query as Slack's browser writes it
// (client-boot nzlP querySuggestionFromQueryAndFilter over pYM5's
// tokenListFromString, CDN build 8f19be21ed7e): the typed query becomes
// tokens, and each filter pass drops the typed tokens of its kind unless
// the filter holds the same value, then appends the filter's own. The
// browser always passes every list (from, in, type, has, is, with,
// creator, …), so a typed filter token survives only when a filter holds
// it: typing from:@grace with no From set searches without it, and typing
// before:2026-01-01 with Last 7 days set sends Last 7 days alone.

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// filesLookup is what the typed tokens resolve against (the client's
// store): getMemberById, getMemberByName, getChannelById,
// getChannelByName and the viewer.
type filesLookup interface {
	memberKnown(id string) bool
	memberByName(name string) string
	conv(id string) (ConvRef, bool)
	convByName(name string) string
	self() string
}

// filesModifiers are getLookupModifierNames' English names.
var filesModifiers = []string{"has", "-has", "type", "-type", "from", "-from", "to", "-to", "in", "-in", "is", "-is",
	"before", "-before", "during", "-during", "on", "-on", "after", "-after", "with", "-with", "creator", "-creator",
	"hasmy", "-hasmy", "threads", "-threads", "dm", "-dm"}

// filesTokenRe is getTokenRegex: a quoted phrase, a smart-quoted one, a
// modifier and its value (one space allowed after the colon), a mention
// (@… or <…>), or a word. Slack's regex also captures the rest, `(.*)$`,
// and runs again on it; this one matches the token alone at an advancing
// offset over a bounded span (filesSpans), so a long paste costs its
// length once, not once per token, whatever it holds.
var filesTokenRe = func() *regexp.Regexp {
	mods := make([]string, len(filesModifiers))
	for i, m := range filesModifiers {
		mods[i] = regexp.QuoteMeta(m)
	}
	n := `(` + strings.Join(mods, "|") + `):\s?((?:[^<>\s]+|<[^>]+>)?)`
	return regexp.MustCompile(`(?is)^\s*(("([^"]+)")|(“([^“”]+)”)|(` + n + `)|(@[^\s]+|<[^>]+>)|([^\s]+))`)
}()

// filesTok is one QuerySuggestionToken: its kind (a modifier, "" for text
// that is no filter) and what it writes.
type filesTok struct {
	kind   string // "from", "in", "before", "type", …; "" otherwise
	entity string // the member, channel, date, file type or thing
	text   string // toString
}

// filesRawToken is rawTokensFromString's entry.
type filesRawToken struct {
	phrase, smart, filter, member, word bool
	s                                   string
}

// filesRawTokens is rawTokensFromString: consecutive words join with one
// space. It stops early (returning what it has) once ctx is done.
func filesRawTokens(ctx context.Context, q string) []filesRawToken {
	var out []filesRawToken
	var words []string // the word run being built, joined once it ends
	flush := func() {
		if len(words) > 0 {
			out = append(out, filesRawToken{word: true, s: strings.Join(words, " ")})
			words = words[:0]
		}
	}
	rest := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(q)
	sp := newFilesSpans(rest)
	for off, n := 0, 0; off < len(rest); n++ {
		if n%256 == 255 && ctx.Err() != nil {
			break
		}
		m := filesTokenRe.FindStringSubmatchIndex(rest[off:sp.end(off)])
		if m == nil || m[1] == 0 {
			break
		}
		g := func(i int) string {
			if m[2*i] < 0 {
				return ""
			}
			return rest[off+m[2*i] : off+m[2*i+1]]
		}
		switch {
		case g(3) != "":
			flush()
			out = append(out, filesRawToken{phrase: true, s: g(3)})
		case g(5) != "":
			flush()
			out = append(out, filesRawToken{phrase: true, smart: true, s: g(5)})
		case g(6) != "":
			flush()
			out = append(out, filesRawToken{filter: true, s: g(6)})
		case g(9) != "":
			flush()
			out = append(out, filesRawToken{member: true, s: g(9)})
		case g(10) != "":
			words = append(words, g(10))
		}
		off += m[1]
	}
	flush()
	return out
}

// filesSpans bounds what the token regex is handed at each offset, so a
// branch that looks ahead for its closer (a quote, a smart quote, the ">"
// of <…>) can't scan the rest of the query again for every token: the
// next ">", '"' and “ or ” after each position come from one backward
// pass, and a token can reach no further than its run of non-spaces, the
// value run after "mod: ", or the closer its opening needs.
type filesSpans struct {
	s             string
	gt, dq, smart []int32 // the next '>', '"', “ or ” at or after i (-1: none)
}

func newFilesSpans(s string) *filesSpans {
	sp := &filesSpans{s: s}
	next := func(at func(i int) bool) []int32 {
		out := make([]int32, len(s)+1)
		out[len(s)] = -1
		for i := len(s) - 1; i >= 0; i-- {
			if at(i) {
				out[i] = int32(i)
			} else {
				out[i] = out[i+1]
			}
		}
		return out
	}
	if strings.IndexByte(s, '>') >= 0 {
		sp.gt = next(func(i int) bool { return s[i] == '>' })
	}
	if strings.IndexByte(s, '"') >= 0 {
		sp.dq = next(func(i int) bool { return s[i] == '"' })
	}
	if strings.Contains(s, "“") || strings.Contains(s, "”") {
		sp.smart = next(func(i int) bool { return strings.HasPrefix(s[i:], "“") || strings.HasPrefix(s[i:], "”") })
	}
	return sp
}

func filesSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r' } // RE2's \s

// end is how far the token at off can reach.
func (sp *filesSpans) end(off int) int {
	s := sp.s
	p := off
	for p < len(s) && filesSpace(s[p]) {
		p++
	}
	if p == len(s) {
		return p
	}
	run := func(i int) int {
		for i < len(s) && !filesSpace(s[i]) {
			i++
		}
		return i
	}
	e := run(p)
	closer := func(arr []int32, from, w int) {
		if arr != nil && from <= len(s) {
			if j := arr[from]; j >= 0 {
				e = max(e, int(j)+w)
			}
		}
	}
	switch {
	case s[p] == '"': // "…"
		closer(sp.dq, p+1, 1)
	case strings.HasPrefix(s[p:], "“"): // “…”
		closer(sp.smart, p+len("“"), len("”"))
	}
	runEnd := e
	if i := strings.IndexByte(s[p:runEnd], '<'); i >= 0 { // <…>: a mention, or a modifier's value
		closer(sp.gt, p+i+1, 1)
	}
	// "mod: value": the one space \s? may take (even before no value),
	// then the value's run (or its <…>)
	if s[runEnd-1] == ':' && runEnd < len(s) && filesSpace(s[runEnd]) {
		e = max(e, runEnd+1)
	}
	if s[runEnd-1] == ':' && runEnd+1 < len(s) && filesSpace(s[runEnd]) && !filesSpace(s[runEnd+1]) {
		v := runEnd + 1
		e = max(e, run(v))
		if s[v] == '<' {
			closer(sp.gt, v+1, 1)
		}
	}
	return e
}

var (
	filesMemberFilterRe = regexp.MustCompile(`^[^:]+:\s?((<@?([^\s|>]+)(\|@?([^>]+))?>)|(@?([^\s]+)))$`)
	filesInRe           = regexp.MustCompile(`^[^:]+:\s?(?:(?:<[@#%]?([A-Z0-9a-z]+)(?:\|[\s]?(?:[#@]?([^>]+)))?>)|(([#@%]?)([^\s]+)))$`)
	filesValueRe        = regexp.MustCompile(`^[^:]+:\s?([^\s]+)`)
	filesMentionRe      = regexp.MustCompile(`^((<@?([^\s|>]+)(\|[\s]?@?([^>]+))?>)|(@?([^\s]+)))$`)
	filesChanMentionRe  = regexp.MustCompile(`^(<#([^\s|>]+)(\|([^>]+))?>|#?([^\s]+))$`)
	filesUserIDRe       = regexp.MustCompile(`^[UW][A-Z0-9]+$`)
	filesConvIDRe       = regexp.MustCompile(`^[CGD][A-Z0-9]+$`)
	filesOtherIDRe      = regexp.MustCompile(`^[STE][A-Z0-9]+$`) // channel sections, teams
)

// filesTokens is tokenListFromString for the typed query.
func filesTokens(ctx context.Context, q string, lk filesLookup) []filesTok {
	var out []filesTok
	for _, rt := range filesRawTokens(ctx, q) {
		switch {
		case rt.phrase: // PhraseQuerySuggestionToken
			if rt.smart {
				out = append(out, filesTok{text: "“" + rt.s + "”"})
			} else {
				out = append(out, filesTok{text: `"` + rt.s + `"`})
			}
		case rt.filter:
			i := strings.Index(rt.s, ":")
			mod := strings.ToLower(rt.s[:i])
			out = append(out, filesFilterToken(mod, mod+":"+rt.s[i+1:], lk))
		case rt.member:
			out = append(out, filesMentionToken(rt.s, lk))
		default:
			out = append(out, filesTok{text: rt.s})
		}
	}
	return out
}

// filesFilterToken is a modifier's fromString (s: "mod:value"); what
// doesn't resolve is a StringQuerySuggestionToken of s.
func filesFilterToken(mod, s string, lk filesLookup) filesTok {
	str := filesTok{text: s}
	switch strings.TrimPrefix(mod, "-") {
	case "from", "to", "with", "creator", "dm": // MemberFilterQuerySuggestionToken
		r := filesMemberFilterRe.FindStringSubmatch(s)
		if r == nil || lk == nil {
			return str
		}
		id := ""
		if r[3] != "" && lk.memberKnown(r[3]) {
			id = r[3]
		}
		if id == "" && r[7] != "" {
			if strings.EqualFold(r[7], "me") { // isSelfKeyword
				id = lk.self()
			} else {
				id = lk.memberByName(r[7])
			}
		}
		if id == "" {
			return str
		}
		return filesTok{kind: mod, entity: id, text: mod + ":<@" + id + ">"}
	case "in": // InQuerySuggestionToken
		r := filesInRe.FindStringSubmatch(s)
		if r == nil {
			return str
		}
		if id := r[1]; id != "" && (filesUserIDRe.MatchString(id) || filesConvIDRe.MatchString(id) || filesOtherIDRe.MatchString(id)) {
			return filesTok{kind: mod, entity: id, text: mod + ":" + filesInRef(id, r[2], lk)}
		}
		if name := r[5]; name != "" && lk != nil {
			if id := lk.convByName(name); id != "" {
				return filesTok{kind: mod, entity: id, text: mod + ":" + filesInRef(id, name, lk)}
			}
			if id := lk.memberByName(name); id != "" {
				return filesTok{kind: mod, entity: id, text: mod + ":<@" + id + ">"}
			}
		}
		if r[3] != "" { // an in: Slack can't resolve: no id, so no filter keeps it
			return filesTok{kind: mod, text: mod + ":" + r[3]}
		}
		return str
	case "before", "after", "on", "during": // DateQuerySuggestionToken
		r := filesValueRe.FindStringSubmatch(s)
		if r == nil || !filesDateUsable(r[1], time.Now()) {
			return str
		}
		return filesTok{kind: mod, entity: r[1], text: mod + ":" + r[1]}
	case "type", "has", "hasmy": // Type / Has / MyReaction tokens: any value
		r := filesValueRe.FindStringSubmatch(s)
		if r == nil {
			return str
		}
		return filesTok{kind: mod, entity: r[1], text: mod + ":" + r[1]}
	case "is": // IsQuerySuggestionToken: Slack's is: values
		r := filesValueRe.FindStringSubmatch(s)
		if r == nil {
			return str
		}
		switch v := strings.ToLower(r[1]); v {
		case "saved", "thread", "dm", "todos", "to-dos":
			if v == "to-dos" {
				v = "todos"
			}
			return filesTok{kind: mod, entity: v, text: mod + ":" + v}
		}
		return str
	}
	return str // threads: (no browser pass)
}

// filesInRef is how an in: token writes id: a DM's member, a group DM
// bare, a channel with its name (">" taken out), a person <@U…>, else
// <id>.
func filesInRef(id, name string, lk filesLookup) string {
	switch {
	case filesUserIDRe.MatchString(id):
		return "<@" + id + ">"
	case filesConvIDRe.MatchString(id):
		if lk != nil {
			if r, ok := lk.conv(id); ok {
				switch r.Kind {
				case "im":
					if r.User != "" {
						return "<@" + r.User + ">"
					}
				case "mpim":
					return "<#" + id + ">"
				}
				if r.Name != "" {
					name = r.Name
				}
			}
		}
		if n := strings.ReplaceAll(name, ">", ""); n != "" {
			return "<#" + id + "|" + n + ">"
		}
		return "<#" + id + ">"
	}
	return "<" + id + ">"
}

// filesMentionToken is a mention word: a channel (<#C…> or #…:
// ChannelQuerySuggestionToken) or a person (MemberQuerySuggestionToken:
// <@U…>); unresolved it stays as typed.
func filesMentionToken(s string, lk filesLookup) filesTok {
	str := filesTok{text: s}
	if lk == nil {
		return str
	}
	if strings.HasPrefix(s, "<#") || strings.HasPrefix(s, "#") {
		if r := filesChanMentionRe.FindStringSubmatch(s); r != nil {
			id := ""
			if r[2] != "" {
				if _, ok := lk.conv(r[2]); ok {
					id = r[2]
				}
			}
			if id == "" && r[5] != "" {
				id = lk.convByName(r[5])
			}
			if id != "" {
				name := r[4]
				if c, ok := lk.conv(id); ok && c.Name != "" {
					name = c.Name
				}
				if n := strings.ReplaceAll(name, ">", ""); n != "" {
					return filesTok{text: "<#" + id + "|" + n + ">"}
				}
				return filesTok{text: "<#" + id + ">"}
			}
		}
	}
	r := filesMentionRe.FindStringSubmatch(s)
	if r == nil {
		return str
	}
	id := ""
	if r[3] != "" && lk.memberKnown(r[3]) {
		id = r[3]
	}
	if id == "" && r[7] != "" {
		id = lk.memberByName(r[7])
	}
	if id == "" {
		return str
	}
	return filesTok{text: "<@" + id + ">"}
}

var (
	filesYearRe = regexp.MustCompile(`^\d{4}$`)
	filesYMRe   = regexp.MustCompile(`^(\d{4})年(\d{1,2})月$`)
	filesYMDRe  = regexp.MustCompile(`^(\d{4})年(\d{1,2})月(\d{1,2})日$`)
	filesISORe  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	filesDateRe = regexp.MustCompile(`^(\d{4})[-/](\d{1,2})[-/](\d{1,2})$`)
)

// filesDateUsable is getMaybeUsableDateFromDateString(value, "before") !==
// undefined (7Nox): a year, a CJK year-month or date, today, yesterday, a
// weekday, an ISO date, week / month / year, or a month's name. Dates
// dayjs would read from other free-form strings aren't recognised.
func filesDateUsable(v string, now time.Time) bool {
	l := strings.ToLower(v)
	switch {
	case filesYearRe.MatchString(v), filesYMRe.MatchString(v):
		return true
	case l == "today", l == "yesterday", l == "week", l == "month", l == "year":
		return true
	}
	for d := time.Sunday; d <= time.Saturday; d++ {
		if n := strings.ToLower(d.String()); l == n || l == n[:3] {
			return true
		}
	}
	if r := filesYMDRe.FindStringSubmatch(v); r != nil {
		v = r[1] + "-" + filesPad2(r[2]) + "-" + filesPad2(r[3])
	}
	if filesISORe.MatchString(v) {
		_, err := time.ParseInLocation("2006-01-02", v, now.Location())
		return err == nil
	}
	if r := filesDateRe.FindStringSubmatch(v); r != nil {
		_, err := time.ParseInLocation("2006-1-2", r[1]+"-"+r[2]+"-"+r[3], now.Location())
		return err == nil
	}
	for m := time.January; m <= time.December; m++ {
		if l == strings.ToLower(m.String()) {
			return true
		}
	}
	return false
}

func filesPad2(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

// filesKeep is one queryTokenListFor…Filter pass over the token list for
// kind: a typed token of that kind stays only while the filter's list
// holds its value (each value once); the rest of the list is appended,
// written by write.
func filesKeep(toks []filesTok, kind string, list []string, write func(v string) string) []filesTok {
	left := append([]string(nil), list...)
	out := toks[:0:0]
	for _, t := range toks {
		if t.kind != kind {
			out = append(out, t)
			continue
		}
		if i := indexOf(left, t.entity); t.entity != "" && i >= 0 {
			left = append(left[:i], left[i+1:]...)
			out = append(out, t)
		}
	}
	for _, v := range left {
		out = append(out, filesTok{kind: kind, entity: v, text: write(v)})
	}
	return out
}

func indexOf(xs []string, v string) int {
	for i, x := range xs {
		if x == v {
			return i
		}
	}
	return -1
}

// filesQueryTokens is querySuggestionFromQueryAndFilter over the typed
// query and the browser's filters (types are the fileType list's values,
// creator the creator list). Its passes run in nzlP's order: from, -from,
// in, -in, before, after, on, type, -type (excludedFileType: the
// browser's handlers all set it to []), has, hasmy, is, -is, with,
// creator, -creator.
func filesQueryTokens(ctx context.Context, query string, o FilesOpts, types, creator []string, lk filesLookup) string {
	toks := filesTokens(ctx, query, lk)
	member := func(mod string) func(string) string { return func(id string) string { return mod + ":<@" + id + ">" } }
	date := func(mod string) func(string) string { return func(d string) string { return mod + ":" + d } }
	one := func(s string) []string {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	toks = filesKeep(toks, "from", o.From, member("from"))
	toks = filesKeep(toks, "-from", nil, member("-from"))
	toks = filesKeep(toks, "in", o.In, func(id string) string { return "in:" + filesInRef(id, "", lk) })
	toks = filesKeep(toks, "-in", nil, nil)
	toks = filesKeep(toks, "before", one(o.Before), date("before"))
	toks = filesKeep(toks, "after", one(o.After), date("after"))
	toks = filesKeep(toks, "on", nil, nil)
	toks = filesKeep(toks, "type", types, func(t string) string { return "type:" + t })
	for _, k := range []string{"-type", "has", "hasmy", "is", "-is", "with"} {
		toks = filesKeep(toks, k, nil, nil)
	}
	toks = filesKeep(toks, "creator", creator, member("creator"))
	toks = filesKeep(toks, "-creator", nil, nil)
	parts := make([]string, 0, len(toks))
	for _, t := range toks {
		parts = append(parts, t.text)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// appFilesLookup resolves typed tokens against the app's store. One is
// made per query: the name indexes are built once, on first use.
type appFilesLookup struct {
	a              *App
	byName, byChan map[string]string
}

func newFilesLookup(a *App) *appFilesLookup { return &appFilesLookup{a: a} }

func (l *appFilesLookup) memberKnown(id string) bool { _, ok := l.a.S.RawUser(id); return ok }

// memberByName is getMemberByName: the name, display name or normalized
// display name, case folded (the first member with it).
func (l *appFilesLookup) memberByName(name string) string {
	if l.byName == nil {
		l.byName = map[string]string{}
		for _, id := range l.a.S.UserIDs() {
			u, ok := l.a.S.RawUser(id)
			if !ok {
				continue
			}
			for _, n := range []string{u.Name, u.Profile.DisplayName, u.Profile.DisplayNameNormalized} {
				if k := strings.ToLower(n); k != "" {
					if _, seen := l.byName[k]; !seen {
						l.byName[k] = id
					}
				}
			}
		}
	}
	return l.byName[strings.ToLower(name)]
}

func (l *appFilesLookup) conv(id string) (ConvRef, bool) {
	c, ok := l.a.Conv(id)
	if !ok {
		return ConvRef{}, false
	}
	return ConvRef{ID: id, Name: c.Name, Kind: c.Kind, User: c.User}, true
}

// convByName is getChannelByName: a channel's name, case folded.
func (l *appFilesLookup) convByName(name string) string {
	if l.byChan == nil {
		l.byChan = map[string]string{}
		for _, id := range l.a.S.ConvIDs() {
			if c, ok := l.a.S.Conv(id); ok && !c.IsIM && !c.IsMPIM && c.Name != "" {
				if k := strings.ToLower(c.Name); l.byChan[k] == "" {
					l.byChan[k] = id
				}
			}
		}
	}
	return l.byChan[strings.ToLower(name)]
}

func (l *appFilesLookup) self() string { return l.a.S.SelfID() }
