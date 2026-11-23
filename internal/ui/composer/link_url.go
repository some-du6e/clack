package composer

import (
	"math"
	"regexp"
	"strings"
	"unicode/utf16"
)

// The link dialog's and the edit-link popover's URL helpers, 1:1 with
// Slack's (gantry-v2 build 83aff18a1847, client-boot and boot-shared,
// read from the public CDN without a cookie):
//
//   - normalizeLinkUrl and isLinkUrlValid (MqLx): what Save keeps of the
//     Link field;
//   - truncateUrl and truncateSmart (psdB) over parseUrl (PSzk): the
//     popover's "Shorten link";
//   - getTrimLength (+BPJ, openTextyLinkFormBasedOnSelection): the
//     punctuation Ctrl+Shift+U trims off a selection.
//
// They work in UTF-16 code units where Slack's JavaScript does (lengths,
// substr), so a URL with astral characters cuts where Slack's does.

// linkTokenRx is bUQE's join of nFyI's workflow token expressions (Mx),
// variable tokens (iJ) and template macros (fE), flags "gi": a Link field
// that is nothing but those stays as it is.
var linkTokenRx = regexp.MustCompile(`(?i)(?:(?<expression>\{([a-z]*)\.([a-z_]*)\((.*?)\)\})(\s?\|>\s?\{[^}]*\$\$[^}]*\})+)` +
	`|(?:\{\{\$?([a-z]+)\.?([a-zA-Z0-9-]+)?(?:\.|_{2})\.?([a-zA-Z0-9\-_.\[\]\x{0080}-\x{FFFF}]+)\}\})` +
	`|(?:(?<templateValue>\{(?<namespace>[a-z]*)\.(?<macroCallbackId>[a-z_]*)\((?<inputs>(.*?))\)\}))`)

// linkSchemeRx is normalizeLinkUrl's "already has a scheme" test.
var linkSchemeRx = regexp.MustCompile(`^[a-zA-Z]{0,63}:(//)?.`)

// normalizeLinkURL is MqLx normalizeLinkUrl: the Link field trimmed, its
// spaces %20; a lone email address becomes a mailto: link; anything with
// a scheme ("localhost:8080" counts) or made only of workflow tokens is
// kept; "//host" gets "http:" and the rest "http://".
func normalizeLinkURL(e string) string {
	t := strings.ReplaceAll(jsTrim(e), " ", "%20")
	if t == "" {
		return ""
	}
	if a := detectEmails(t); len(a) == 1 && a[0].text == t {
		return "mailto:" + t
	}
	if len(linkTokenRx.ReplaceAllString(jsTrim(e), "")) == 0 || linkSchemeRx.MatchString(t) {
		return t
	}
	if strings.HasPrefix(t, "//") {
		return "http:" + t
	}
	return "http://" + t
}

// isLinkURLValid is MqLx isLinkUrlValid: no javascript: or about: link.
func isLinkURLValid(s string) bool {
	l := strings.ToLower(jsTrim(s))
	return !strings.HasPrefix(l, "javascript:") && !strings.HasPrefix(l, "about:")
}

// u16 is a JS string: UTF-16 code units.
type u16 []uint16

func toU16(s string) u16     { return utf16.Encode([]rune(s)) }
func (s u16) String() string { return string(utf16.Decode(s)) }

// substr is String.prototype.substr(start, n); n < 0 means to the end.
func (s u16) substr(start, n int) u16 {
	if start < 0 {
		start = max(len(s)+start, 0)
	}
	if start > len(s) {
		start = len(s)
	}
	end := len(s)
	if n >= 0 && start+n < end {
		end = start + n
	}
	return s[start:end]
}

func cat(parts ...u16) u16 {
	var out u16
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// smartParts is truncateSmart's parse: the regexes' captures ("" when a
// part is absent, as JavaScript's falsy checks see it).
type smartParts struct {
	scheme, host, path, query, rawQuery, fragment u16
	hasRawQuery                                   bool
}

func indexAnyU(s u16, set string) int {
	for i, c := range s {
		if c < 0x80 && strings.IndexByte(set, byte(c)) >= 0 {
			return i
		}
	}
	return -1
}

// parseSmart is truncateSmart's inline parser.
func parseSmart(s u16) smartParts {
	var p smartParts
	i := s
	// /^([a-z]+):\/\//i
	n := 0
	for n < len(i) && (i[n] >= 'a' && i[n] <= 'z' || i[n] >= 'A' && i[n] <= 'Z') {
		n++
	}
	if n > 0 && n+2 < len(i) && i[n] == ':' && i[n+1] == '/' && i[n+2] == '/' {
		p.scheme, i = i[:n], i[n+3:]
	}
	// /^(.*?)(?=(\?|#|\/|$))/: up to the first ? # / (a link holds no
	// line break, which "." wouldn't cross)
	k := indexAnyU(i, "?#/")
	if k < 0 {
		k = len(i)
	}
	p.host, i = i[:k], i[k:]
	// /^\/(.*?)(?=(\?|#|$))/
	if len(i) > 0 && i[0] == '/' {
		rest := i[1:]
		k := indexAnyU(rest, "?#")
		if k < 0 {
			k = len(rest)
		}
		p.path, i = cat(u16{'/'}, rest[:k]), rest[k:]
	}
	// /^\?(.*?)(?=(#|$))/
	if len(i) > 0 && i[0] == '?' {
		rest := i[1:]
		k := indexAnyU(rest, "#")
		if k < 0 {
			k = len(rest)
		}
		p.query, p.rawQuery, p.hasRawQuery = rest[:k], i[:k+1], true
		i = rest[k:]
	}
	// /^#(.*?)$/
	if len(i) > 0 && i[0] == '#' {
		p.fragment = i[1:]
	}
	return p
}

// build is truncateSmart's buildUrl.
func (c smartParts) build() u16 {
	var t u16
	if len(c.scheme) > 0 && len(c.host) > 0 {
		t = cat(t, c.scheme, toU16("://"))
	}
	if len(c.host) > 0 {
		t = cat(t, c.host)
	}
	if len(c.path) > 0 {
		t = cat(t, c.path)
	}
	switch {
	case len(c.query) > 0:
		t = cat(t, u16{'?'}, c.query)
	case c.hasRawQuery && c.rawQuery.String() == "?":
		t = cat(t, u16{'?'})
	}
	if len(c.fragment) > 0 {
		t = cat(t, u16{'#'}, c.fragment)
	}
	return t
}

// truncateSmart is psdB's: e cut to at most t units with n in the middle
// of the part that doesn't fit, keeping the host and as much of the path,
// query and fragment as it can.
func truncateSmart(e string, t int, n string) string {
	s := toU16(e)
	nn := toU16(n)
	o, l := len(nn), len(nn)
	seg := func(e u16, t int) u16 {
		a := float64(t) / 2
		i := int(math.Ceil(a))
		r := -int(math.Floor(a))
		var s u16
		if r < 0 {
			s = e.substr(r, -1)
		}
		return cat(e.substr(0, i), nn, s)
	}
	if len(s) <= t {
		return s.String()
	}
	d := t - o
	c := parseSmart(s)
	if len(c.query) > 0 {
		if k := indexAnyU(c.query, "?#"); k >= 0 {
			c.query = c.query[:k]
			s = c.build()
		}
	}
	if len(s) <= t {
		return s.String()
	}
	if len(c.host) > 0 {
		s = c.build()
	}
	if len(s) <= t {
		return s.String()
	}
	var u u16
	if len(c.host) > 0 {
		u = cat(u, c.host)
	}
	if len(u) >= d {
		if len(c.host) == t {
			return cat(c.host.substr(0, t-o), nn).substr(0, d+l).String()
		}
		return seg(u, d).substr(0, d+l).String()
	}
	var m u16
	if len(c.path) > 0 {
		m = cat(m, c.path)
	}
	switch {
	case len(c.query) > 0:
		m = cat(m, u16{'?'}, c.query)
	case c.hasRawQuery && c.rawQuery.String() == "?":
		m = cat(m, u16{'?'})
	}
	if len(m) > 0 {
		if len(u)+len(m) >= d {
			if len(u)+len(m) == t {
				return cat(u, m).substr(0, t).String()
			}
			return cat(u, seg(m, d-len(u))).substr(0, d+l).String()
		}
		u = cat(u, m)
	}
	if len(c.fragment) > 0 {
		f := cat(u16{'#'}, c.fragment)
		if len(u)+len(f) >= d {
			if len(u)+len(f) == t {
				return cat(u, f).substr(0, t).String()
			}
			return cat(u, seg(f, d-len(u))).substr(0, d+l).String()
		}
		u = cat(u, f)
	}
	if len(c.scheme) > 0 && len(c.host) > 0 {
		sc := cat(c.scheme, toU16("://"))
		if len(u)+len(sc) < d {
			return cat(sc, u).substr(0, t).String()
		}
	}
	if len(u) <= t {
		return u.String()
	}
	var h u16
	if d > 0 {
		h = u.substr(-int(math.Floor(float64(d)/2)), -1)
	}
	return cat(u.substr(0, int(math.Ceil(float64(d)/2))), nn, h).substr(0, d+l).String()
}

// truncatedMark is truncateUrl's "[…]".
const truncatedMark = "[\u2026]"

// urlPathHash is what truncateUrl reads of parseUrl (PSzk): the URL's
// protocol, and whether its pathname is more than "/" or it has a hash.
// http(s):// URLs are parsed as they are (new URL(e)); anything else
// against the client's own page (new URL(e, location.href)), so a URL
// with no scheme reads as an https: one with a path. A URL WHATWG can't
// parse reads as no protocol at all.
func urlPathHash(s string) (protocol string, deep bool) {
	if !strings.HasPrefix(s, "https://") && !strings.HasPrefix(s, "http://") {
		if m := urlSchemeK.FindStringSubmatch(s); m != nil {
			sc := strings.ToLower(m[1])
			if sc != "http" && sc != "https" {
				return sc + ":", true // truncateUrl keeps it whatever its path
			}
			s = sc + s[len(m[1]):]
		} else {
			if strings.HasPrefix(s, "//") {
				s = "https:" + s
			} else {
				return "https:", true // relative to app.slack.com/client/\u2026
			}
		}
	}
	i := strings.Index(s, ":")
	proto := strings.ToLower(s[:i]) + ":"
	rest := strings.TrimLeft(s[i+1:], "/\\") // a special URL's slashes, \ read as /
	host, tail := rest, ""
	if k := strings.IndexAny(rest, "/\\?#"); k >= 0 {
		host, tail = rest[:k], rest[k:]
	}
	if at := strings.LastIndexByte(host, '@'); at >= 0 {
		host = host[at+1:]
	}
	if c := strings.LastIndexByte(host, ':'); c >= 0 && !strings.HasSuffix(host, "]") {
		if strings.Trim(host[c+1:], "0123456789") != "" {
			return "", false // not a port: new URL throws
		}
		host = host[:c]
	}
	bracketed := strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]")
	if host == "" || strings.ContainsAny(host, " \t\n\r<>^|") || !bracketed && strings.ContainsAny(host, "[]") {
		return "", false // a forbidden host code point: new URL throws, the empty object
	}
	path, hash := tail, ""
	if j := strings.IndexByte(path, '#'); j >= 0 {
		path, hash = path[:j], path[j+1:]
	}
	if j := strings.IndexByte(path, '?'); j >= 0 {
		path = path[:j]
	}
	return proto, path != "" && path != "/" && path != "\\" || hash != ""
}

// truncateURL is psdB truncateUrl: a URL over 140 units (with at most one
// "?", http(s) and somewhere past its root) as truncateSmart makes it at
// 140 with "[…]", or the URL itself when that gains nothing.
func truncateURL(e string) string {
	if e == "" {
		return ""
	}
	if len(toU16(e)) <= 140 {
		return e
	}
	if strings.Count(e, "?") > 1 {
		return e
	}
	proto, deep := urlPathHash(e)
	if proto != "http:" && proto != "https:" || !deep {
		return e
	}
	r := proto + "//" + truncateSmart(e, 140, truncatedMark)
	if len(toU16(r)) >= len(toU16(e)) {
		return e
	}
	s := strings.Index(r, truncatedMark)
	if s == -1 || s+len(truncatedMark) == len(r) || strings.Replace(r, "\u2026", "", 1) == e {
		return e
	}
	return r
}

// The selection Ctrl+Shift+U links is trimmed first (getTrimLength):
// leading . , ; : ? [ ( < and trailing . , ; : ? ! ] ) > (UbfT Fh and
// uikB Nb without "!" in front and without * _ ~ either way), white space
// and embeds, but a bracket stays while the rest of the text closes it.
var (
	linkTrimLead  = ".,;:?[(<"
	linkTrimTrail = ".,;:?!])>"
	linkTrimPairs = [][2]rune{{'(', ')'}, {'[', ']'}, {'<', '>'}}
)

// linkTrimLength is getTrimLength over rs (the selection's text, embeds
// as ObjRune): how many runes to trim at its start, or at its end with
// trailing.
func linkTrimLength(rs []rune, trailing bool) int {
	set := linkTrimLead
	if trailing {
		set = linkTrimTrail
	}
	trimmable := func(r rune) bool { return jsSpace(r) || r == ObjRune || strings.ContainsRune(set, r) }
	run := 0
	for run < len(rs) {
		r := rs[run]
		if trailing {
			r = rs[len(rs)-1-run]
		}
		if !trimmable(r) {
			break
		}
		run++
	}
	a := 0
	for n := 0; n < run; n++ {
		var o rune
		if trailing {
			o = rs[len(rs)-1-n]
		} else {
			o = rs[n]
		}
		var pair [2]rune
		found := false
		for _, p := range linkTrimPairs {
			if !trailing && p[0] == o || trailing && p[1] == o {
				pair, found = p, true
			}
		}
		if !found {
			a++
			continue
		}
		m := 0
		if trailing {
			for i := len(rs) - 1 - n; i >= 0; i-- {
				switch rs[i] {
				case pair[1]:
					m++
				case pair[0]:
					m--
				}
			}
		} else {
			for _, r := range rs[n:] {
				switch r {
				case pair[0]:
					m++
				case pair[1]:
					m--
				}
			}
		}
		if m > 0 {
			a++
		} else {
			break
		}
	}
	return a
}
