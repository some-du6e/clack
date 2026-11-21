package composer

// detectLinks (nVll) as linkifyPhoneNumbers (cyvU) calls it, with its
// defaults: detectUrls (7Jnr), then detectEmails (hlZ9) merged in, then
// detectPhoneNumbers (L9Ir) merged in (mergeMatches, a0ov: a later kind
// that touches an earlier match is dropped). cyvU keeps the phones.
//
// detectUrls' pattern needs lookbehind and backreferences, so it is
// matched by hand here in the order the JavaScript engine tries it: at
// each position the scheme form, then www., then a bare domain ending in
// one of QHZp's TLDs; each domain label is atomic (the longest run of
// label characters up to 63, minus trailing hyphens); then an optional
// port and path. Offsets are in runes, like the phone matches.

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

func inRuneRanges(r rune, t [][2]rune) bool {
	for _, g := range t {
		if r >= g[0] && r <= g[1] {
			return true
		}
	}
	return false
}

// urlY is 7Jnr's y: letters and digits.
func urlY(r rune) bool { return inRuneRanges(r, urlLetters) || inRuneRanges(r, urlDigits) }

// urlB is its b: y, the emoji class (every astral character: the class
// holds both surrogate halves) and combining marks.
func urlB(r rune) bool {
	return r > 0xffff || urlY(r) || inRuneRanges(r, urlEmoji) || inRuneRanges(r, urlMarks)
}

// urlLabel is getDomainLabelStr / ...WithUnderscore at p: the atomic
// label's end, or -1.
func urlLabel(rs []rune, p int, under bool) int {
	if p >= len(rs) || rs[p] == '-' {
		return -1
	}
	j := p
	for j < len(rs) && j-p < 63 && (urlY(rs[j]) || rs[j] == '-' || under && rs[j] == '_') {
		j++
	}
	for j > p && rs[j-1] == '-' { // (?<!-)
		j--
	}
	if j == p {
		return -1
	}
	return j
}

// urlLabelEnds are the ends of getDomainNameStrWithUnderscore's labels at
// p: the first label, then each "."-label the {0,126} repetition takes.
func urlLabelEnds(rs []rune, p int) []int {
	e := urlLabel(rs, p, true)
	if e < 0 {
		return nil
	}
	ends := []int{e}
	for len(ends) <= 126 && e < len(rs) && rs[e] == '.' {
		n := urlLabel(rs, e+1, false)
		if n < 0 {
			break
		}
		e = n
		ends = append(ends, e)
	}
	return ends
}

// urlTLDAt is `\.(?:TLD|now)(?![-b]|_+[y])` at the "." at p: the end, or -1.
func urlTLDAt(rs []rune, p int) int {
	if p >= len(rs) || rs[p] != '.' {
		return -1
	}
	ok := func(at int) bool {
		if at >= len(rs) {
			return true
		}
		if rs[at] == '-' || urlB(rs[at]) {
			return false
		}
		if rs[at] == '_' {
			k := at
			for k < len(rs) && rs[k] == '_' {
				k++
			}
			if k < len(rs) && urlY(rs[k]) {
				return false
			}
		}
		return true
	}
	for _, t := range append(urlTLDs[:len(urlTLDs):len(urlTLDs)], "now") {
		e := p + 1 + len(t)
		if e <= len(rs) && strings.EqualFold(string(rs[p+1:e]), t) && ok(e) {
			return e
		}
	}
	return -1
}

func urlASCIILetter(r rune) bool { return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' }

func urlSchemeRune(r rune) bool {
	return urlASCIILetter(r) || r >= '0' && r <= '9' || r == '-' || r == '.' || r == '+'
}

// urlScheme is S's [A-Za-z][-.+A-Za-z0-9]{0,63}: at p; the ':' 's index or -1.
func urlScheme(rs []rune, p int) int {
	if p >= len(rs) || !urlASCIILetter(rs[p]) {
		return -1
	}
	j := p + 1
	for j < len(rs) && j-p-1 < 63 && urlSchemeRune(rs[j]) {
		j++
	}
	if j < len(rs) && rs[j] == ':' {
		return j
	}
	return -1
}

func urlUserRune(r rune) bool { return r >= 'A' && r <= 'z' || r >= '0' && r <= '9' }

// urlUserinfo is A's [A-za-z0-9][A-za-z0-9%+.]*(?::…)?@ at p: the end, or -1.
func urlUserinfo(rs []rune, p int) int {
	run := func(j int) int {
		for j < len(rs) && (urlUserRune(rs[j]) || rs[j] == '%' || rs[j] == '+' || rs[j] == '.') {
			j++
		}
		return j
	}
	if p >= len(rs) || !urlUserRune(rs[p]) {
		return -1
	}
	j := run(p + 1)
	if j+1 < len(rs) && rs[j] == ':' && urlUserRune(rs[j+1]) {
		if k := run(j + 2); k < len(rs) && rs[k] == '@' {
			return k + 1
		}
	}
	if j < len(rs) && rs[j] == '@' {
		return j + 1
	}
	return -1
}

// urlPathClass and urlPathEnd are v's two classes.
func urlPathClass(r rune) bool {
	return urlB(r) || strings.ContainsRune("-+&@#/%=~_()|'$*[]{}?!:,.;^", r) || r == 0x2026 || r == 0x2014 || r == 0x2013 || r == 0x2713
}

func urlPathEnd(r rune) bool {
	return urlB(r) || strings.ContainsRune("-+&@#/%=~_()'$*[]{}", r) || r == 0x2013 || r == 0x2713
}

// urlTail is (?::[0-9]+)?(?:v)? at p.
func urlTail(rs []rune, p int) int {
	if p+1 < len(rs) && rs[p] == ':' && rs[p+1] >= '0' && rs[p+1] <= '9' {
		p += 2
		for p < len(rs) && rs[p] >= '0' && rs[p] <= '9' {
			p++
		}
	}
	if p < len(rs) && (rs[p] == '/' || rs[p] == '?' || rs[p] == '#') {
		p++
		j := p
		for j < len(rs) && urlPathClass(rs[j]) {
			j++
		}
		for j > p && !urlPathEnd(rs[j-1]) {
			j--
		}
		p = j
	}
	return p
}

// urlRxMatch is one match of 7Jnr's T: [u, end) and its groups (m the
// scheme form, h the www. form, p the bare form, each without port and
// path; f a leading "//").
type urlRxMatch struct {
	u, end  int
	m, p    string
	h, f    bool
	matched bool
}

// urlMatchAt tries T's three forms at u.
func urlMatchAt(rs []rune, u int) urlRxMatch {
	// (S(A)?Domain)
	if c := urlScheme(rs, u); c >= 0 {
		q := c + 1
		lookOK := !(q < len(rs) && rs[q] >= '0' && rs[q] <= '9')
		if s := urlScheme(rs, q); s >= 0 && s+2 < len(rs) && rs[s+1] == '/' && rs[s+2] == '/' {
			lookOK = false
		}
		if lookOK {
			if q+1 < len(rs) && rs[q] == '/' && rs[q+1] == '/' {
				q += 2
			}
			dom := -1
			if a := urlUserinfo(rs, q); a >= 0 {
				if ends := urlLabelEnds(rs, a); ends != nil {
					dom = ends[len(ends)-1]
				}
			}
			if dom < 0 {
				if ends := urlLabelEnds(rs, q); ends != nil {
					dom = ends[len(ends)-1]
				}
			}
			if dom >= 0 {
				return urlRxMatch{u: u, end: urlTail(rs, dom), m: string(rs[u:dom]), matched: true}
			}
		}
	}
	// ((//)?(?:www\.)Domain)
	for _, slash := range []bool{true, false} {
		q := u
		if slash {
			if !(q+1 < len(rs) && rs[q] == '/' && rs[q+1] == '/') {
				continue
			}
			q += 2
		}
		if q+4 <= len(rs) && strings.EqualFold(string(rs[q:q+4]), "www.") {
			if ends := urlLabelEnds(rs, q+4); ends != nil {
				return urlRxMatch{u: u, end: urlTail(rs, ends[len(ends)-1]), h: true, f: slash, matched: true}
			}
		}
	}
	// ((//)?Domain\.TLD)
	for _, slash := range []bool{true, false} {
		q := u
		if slash {
			if !(q+1 < len(rs) && rs[q] == '/' && rs[q+1] == '/') {
				continue
			}
			q += 2
		}
		ends := urlLabelEnds(rs, q)
		for k := len(ends) - 1; k >= 0; k-- {
			if e := urlTLDAt(rs, ends[k]); e >= 0 {
				return urlRxMatch{u: u, end: urlTail(rs, e), p: string(rs[u:e]), f: slash, matched: true}
			}
		}
	}
	return urlRxMatch{}
}

type linkMatch struct {
	kind      string // url | email | phone
	at, n     int    // rune offset and length
	text, url string
}

var (
	urlSchemeK   = regexp.MustCompile(`^([A-Za-z][-.+A-Za-z0-9]*):`)
	urlSchemeC   = regexp.MustCompile(`^([A-Za-z][-.+A-Za-z0-9]*)://`)
	urlBareDotD  = regexp.MustCompile(`^\.[A-Za-z0-9]+(?:[)}\]?*_~:` + jsS[1:len(jsS)-1] + `]|$)`)
	urlSpotifyM  = regexp.MustCompile(`^spotify:(track|user|artist|album|playlist):[a-zA-Z0-9]+`)
	urlPipeTrim  = regexp.MustCompile(`[!:,.;^\x{2026}\x{2014}]+$`)
	urlDaylite   = regexp.MustCompile(`^daylite://Command=ShowObject&` + jsNS + `+`)
	urlHansoft   = regexp.MustCompile(`^hansoft://` + jsNS + `+`)
	urlRdar      = regexp.MustCompile(`^rdar://[0-9][0-9&]*`)
	urlBasicProt = regexp.MustCompile(`^[A-Za-z][-.+A-Za-z0-9]*://`)
	urlBareRareH = regexp.MustCompile(`(?i)^[^./]{1,63}\.(?:` + strings.Join(urlTLDsUncommonBare, "|") + `)$`)
)

// urlAfterUnderscore is E, /^(?:\.[y\-]+)+_+[y]/, at p: a domain that
// goes on into an underscored label.
func urlAfterUnderscore(rs []rune, p int) bool {
	groups := 0
	for p < len(rs) && rs[p] == '.' {
		j := p + 1
		for j < len(rs) && (urlY(rs[j]) || rs[j] == '-') {
			j++
		}
		if j == p+1 {
			break
		}
		groups++
		p = j
	}
	if groups == 0 || p >= len(rs) || rs[p] != '_' {
		return false
	}
	for p < len(rs) && rs[p] == '_' {
		p++
	}
	return p < len(rs) && urlY(rs[p])
}

func urlBadScheme(s string) bool { return s == "mailto" || s == "javascript" || s == "vbscript" }

// hasUnbalancedBrackets is 7Jnr's: a closing ), } or ] one more than its
// openers.
func hasUnbalancedBrackets(s string) bool {
	if s == "" {
		return false
	}
	last := s[len(s)-1]
	open := map[byte]byte{')': '(', '}': '{', ']': '['}[last]
	if open == 0 {
		return false
	}
	return strings.Count(s, string(open)) == strings.Count(s, string(last))-1
}

// urlHostWWW is hostHasWWWPrefix.
func urlHostWWW(h string) bool {
	return strings.HasPrefix(h, "www.") && len(strings.Split(h, ".")) >= 3
}

// uncommonTLDNoPrefix is isMatchWithUncommonTLDAndNoPrefix: a match
// detectUrls drops.
func uncommonTLDNoPrefix(m linkMatch) bool {
	if "http://"+m.text == m.url && urlBareRareH.MatchString(m.text) {
		return true
	}
	var host, proto, path string
	base, _ := url.Parse("https://app.slack.com/client")
	if pu, err := url.Parse(m.url); err == nil {
		if pu.Scheme == "" {
			pu = base.ResolveReference(pu)
		}
		host, proto, path = strings.ToLower(pu.Hostname()), pu.Scheme+":", pu.EscapedPath()
		if path == "" && (pu.Scheme == "http" || pu.Scheme == "https") {
			path = "/"
		}
	}
	if proto == "hansoft:" || proto == "spotify:" {
		return false
	}
	if host == "" {
		return m.text != "" && !strings.Contains(m.text, "://")
	}
	if !strings.Contains(host, ".") {
		return false
	}
	s := host[strings.LastIndexByte(host, '.')+1:]
	if slices.Contains(urlTLDs, s) || host == "ms.now" {
		if slices.Contains(urlTLDsCountry, s) && !urlHostWWW(host) && (path == "" || path == "/") && "http://"+m.text == m.url {
			first := m.text
			if i := strings.IndexAny(first, "/:?#"); i >= 0 {
				first = first[:i]
			}
			t := first[strings.LastIndexByte(first, '.')+1:]
			if !(t == s || t == strings.ToUpper(s)) {
				return true
			}
		}
		return false
	}
	return !(urlHostWWW(host) || urlBasicProt.MatchString(m.text))
}

// cjkLastNon is findLastIndex(chars, not CJK) + 1.
func cjkLastNon(rs []rune) int {
	for i := len(rs) - 1; i >= 0; i-- {
		if !cjkRune(rs[i]) {
			return i + 1
		}
	}
	return 0
}

// detectURLs is detectUrls (7Jnr) with its defaults.
func detectURLs(s string) []linkMatch {
	rs := []rune(s)
	var out []linkMatch
	for last := 0; last < len(rs); {
		var l urlRxMatch
		for u := last; u < len(rs); u++ {
			if l = urlMatchAt(rs, u); l.matched {
				break
			}
		}
		if !l.matched {
			break
		}
		last = l.end
		u := l.u
		c := string(rs[l.u:l.end])
		m := l.m
		if l.p != "" && urlAfterUnderscore(rs, l.end) {
			continue
		}
		var g rune
		if u > 0 {
			g = rs[u-1]
		}
		if g == '@' || l.end < len(rs) && rs[l.end] == '@' && m == "" && !l.h && l.p != "" || g == '/' && m == "" && !l.h {
			continue
		}
		if m != "" {
			if k := urlSchemeK.FindStringSubmatch(m); k != nil && urlBadScheme(k[1]) {
				continue
			}
		}
		if m == "" && !l.h {
			if n := len(c) - len(strings.TrimLeft(c, "_")); n > 0 {
				u += n
				c = c[n:]
			}
		}
		hasProto := m != "" && urlSchemeC.MatchString(m)
		spotify := urlSpotifyM.MatchString(c)
		probe := c
		if m != "" {
			probe = c[strings.IndexByte(c, ':'):]
		}
		if !spotify && (m == "" || !hasProto) && !strings.Contains(probe, ".") {
			continue
		}
		b := c
		if hasProto {
			b = strings.SplitN(c, "://", 3)[1]
		}
		if strings.Contains(strings.SplitN(b, "/", 2)[0], "..") {
			continue
		}
		if !l.f && l.p != "" && l.p == c && urlBareDotD.MatchString(string(rs[u+utf8.RuneCountInString(l.p):])) {
			continue
		}
		for _, sch := range []string{"http", "https", "sftp", "ftp"} {
			if m != "" && strings.Contains(m, sch+"://") {
				if a := strings.Index(c, sch); a > 0 {
					u += utf8.RuneCountInString(c[:a])
					c, m = c[a:], m[a:]
				}
				break
			}
		}
		if spotify {
			if t := urlSpotifyM.FindString(string(rs[u:])); t != "" {
				c = t
			}
		}
		if v := strings.IndexByte(c, '|'); g == '|' && v > 0 {
			last = u + utf8.RuneCountInString(c[:v])
			c = urlPipeTrim.ReplaceAllString(c[:v], "")
		}
		if hasUnbalancedBrackets(c) {
			c = c[:len(c)-1]
		}
		c = strings.TrimSuffix(c, "?")
		w := c
		if !l.f && m == "" {
			w = "http://" + c
		}
		lm := linkMatch{kind: "url", at: u, text: c, url: w}
		if m != "" {
			from := string(rs[u:])
			for _, fix := range []struct {
				pre string
				rx  *regexp.Regexp
			}{{"hansoft://", urlHansoft}, {"rdar://", urlRdar}, {"daylite://", urlDaylite}} {
				if strings.HasPrefix(lm.url, fix.pre) {
					if t := fix.rx.FindString(from); t != "" {
						lm.url, lm.text = t, t
					}
				}
			}
		}
		// maybeStripTrailingSingleQuote
		if strings.HasSuffix(lm.url, "'") {
			seg := lm.url[strings.LastIndexByte(lm.url, '/')+1:]
			if strings.Count(seg, "'")%2 == 1 {
				lm.url, lm.text = lm.url[:len(lm.url)-1], strings.TrimSuffix(lm.text, "'")
			}
		}
		// correctMatchContainingCJKChars
		if ur := []rune(lm.url); len(ur) > 0 && lm.at > 0 && cjkRune(rs[lm.at-1]) && cjkRune(ur[len(ur)-1]) {
			if k := cjkLastNon(ur); k > 0 && !strings.ContainsFunc(string(ur[:k]), cjkRune) {
				tr := []rune(lm.text)
				lm.url, lm.text = string(ur[:k]), string(tr[:cjkLastNon(tr)])
			}
		}
		lm.n = utf8.RuneCountInString(lm.text)
		last = max(last, lm.at+lm.n)
		if !uncommonTLDNoPrefix(lm) {
			out = append(out, lm)
		}
	}
	return out
}

// emailTLDs is detectEmails' list: its own and QHZp's Ay.
var emailTLDs = append([]string{"aero", "app", "asia", "blog", "by", "cat", "club", "coop", "cu", "do", "engineering", "fun", "gu", "help", "id",
	"immo", "ir", "is", "jobs", "kp", "law", "loan", "me", "menu", "mobi", "museum", "name", "online", "party", "post", "pro", "sd", "shop",
	"site", "so", "space", "sy", "team", "tel", "top", "travel", "ve", "vip", "wang", "wtf", "xin", "xxx", "xyz"}, urlTLDs...)

// emailAt is hlZ9's
// /(?:\b|_)([A-Z0-9][A-Z0-9._%+-]{0,69})@(?:([A-Z0-9-]{1,63})\.){1,125}([A-Z]{2,63})(\b|_)/gi
// tried at rune i, matched by hand (Go's regexp refuses the nested
// repeat): the match's end, the TLD and whether a trailing "_" ended it.
// The local part and each label can't backtrack (the characters after
// them aren't in their classes), so only the number of labels does, from
// the most down, and the TLD is the longest letter run that ends at a
// word boundary or an underscore.
func emailAt(rs []rune, i int) (end int, tld string, under bool, ok bool) {
	word := func(r rune) bool { return urlASCIILetter(r) || r >= '0' && r <= '9' || r == '_' }
	alnum := func(r rune) bool { return urlASCIILetter(r) || r >= '0' && r <= '9' }
	try := func(l int) (int, string, bool, bool) {
		if l >= len(rs) || !alnum(rs[l]) {
			return 0, "", false, false
		}
		j := l + 1
		for j < len(rs) && j-l-1 < 69 && (alnum(rs[j]) || strings.ContainsRune("._%+-", rs[j])) {
			j++
		}
		if j >= len(rs) || rs[j] != '@' {
			return 0, "", false, false
		}
		starts := []int{j + 1}
		for p := j + 1; len(starts) <= 125; {
			q := p
			for q < len(rs) && q-p < 63 && (alnum(rs[q]) || rs[q] == '-') {
				q++
			}
			if q == p || q >= len(rs) || rs[q] != '.' {
				break
			}
			p = q + 1
			starts = append(starts, p)
		}
		for k := len(starts) - 1; k >= 1; k-- {
			t := starts[k]
			e := t
			for e < len(rs) && e-t < 63 && urlASCIILetter(rs[e]) {
				e++
			}
			if e-t < 2 {
				continue
			}
			switch {
			case e == len(rs) || !word(rs[e]):
				return e, string(rs[t:e]), false, true
			case rs[e] == '_':
				return e + 1, string(rs[t:e]), true, true
			}
		}
		return 0, "", false, false
	}
	if boundary := (i == 0 || !word(rs[i-1])) != (i >= len(rs) || !word(rs[i])); boundary {
		if e, t, u, ok := try(i); ok {
			return e, t, u, true
		}
	}
	if i < len(rs) && rs[i] == '_' {
		return try(i + 1)
	}
	return 0, "", false, false
}

// detectEmails is hlZ9's.
func detectEmails(s string) []linkMatch {
	rs := []rune(s)
	var out []linkMatch
	for i := 0; i < len(rs); {
		end, tld, under, ok := emailAt(rs, i)
		if !ok {
			i++
			continue
		}
		at := i
		i = end
		a := rs[at:end]
		if under {
			a = a[:len(a)-1]
		}
		if jsRunes(a) > 254 || !slices.Contains(emailTLDs, strings.ToLower(tld)) {
			continue
		}
		if len(a) > 0 && strings.ContainsRune("*`_~\uff0a\uff40\uff3f", a[0]) { // maybeStripLeadingDelimiterFromEmail
			a = a[1:]
			at++
		}
		lm := linkMatch{kind: "email", at: at, n: len(a), text: string(a), url: "mailto:" + string(a)}
		if at > 0 && rs[at-1] == '@' {
			continue
		}
		if strings.HasPrefix(lm.url, "mailto:git@") && at+lm.n < len(rs) && rs[at+lm.n] == ':' { // isGitSshUrl
			continue
		}
		out = append(out, lm)
	}
	return out
}

// mergeMatches is a0ov: e, then each of t that touches none of e.
func mergeMatches(e, t []linkMatch) []linkMatch {
	if len(e) == 0 {
		return t
	}
	out := append([]linkMatch(nil), e...)
	for _, m := range t {
		lo, hi := m.at, m.at+m.n
		clash := false
		for _, x := range e {
			s, en := x.at, x.at+x.n
			if lo == s || hi == en || lo <= s && hi >= en || lo >= s && lo < en || hi >= s && hi < en {
				clash = true
				break
			}
		}
		if !clash {
			out = append(out, m)
		}
	}
	return out
}

// detectLinks is nVll's detectLinks with its defaults, sorted by offset.
func detectLinks(s string) []linkMatch {
	h := mergeMatches(detectURLs(s), detectEmails(s))
	var phones []linkMatch
	for _, p := range detectPhones(s) {
		phones = append(phones, linkMatch{kind: "phone", at: p.at, n: p.n, text: p.text, url: p.url})
	}
	h = mergeMatches(h, phones)
	slices.SortStableFunc(h, func(a, b linkMatch) int { return a.at - b.at })
	return h
}
