package fakeslack

import (
	"strings"
	"unicode/utf8"
)

// slugDisplayURL is the fake chat.slugifyUrl's display_url for a link
// (slug_type "link"): a stand-in for the server's short form of a URL,
// the label Slack's composer gives a pasted link (formatLinkSlugAttributesForDelta)
// and sends as the link's text with truncated: true. Built to reproduce
// the live answers in slug_display_test.go (enterprise host, 2026-11-14,
// synthetic example.com URLs):
//
//   - no scheme, userinfo, port or "www."; the host keeps its case
//   - no trailing "/"; a bare host keeps "/" only before "?" or "#"
//   - a path segment with a percent-encoded non-ASCII byte becomes "…",
//     one over 36 characters its first 12 and "…" (36 stays, 40 is cut)
//   - three or more segments collapse to the first one or two, "…" and
//     the last one when there are more than four, or the URL is long
//     (seen at 300 characters, not at 72; 100 here is a guess)
//   - at most two query parameters, then "…"; a parameter without "=" or
//     with a key over 20 characters is "…", a value over 20 characters
//     its first 20 and "…"
//   - any fragment is "#…"
func slugDisplayURL(raw string) string {
	s := raw
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	frag := ""
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s, frag = s[:i], s[i+1:]
	}
	query, hasQuery := "", false
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s, query, hasQuery = s[:i], s[i+1:], true
	}
	host, path := s, ""
	if i := strings.IndexByte(s, '/'); i >= 0 {
		host, path = s[:i], s[i:]
	}
	if i := strings.LastIndexByte(host, '@'); i >= 0 {
		host = host[i+1:]
	}
	if i := strings.LastIndexByte(host, ':'); i >= 0 && !strings.Contains(host[i:], "]") {
		host = host[:i]
	}
	if len(host) > 4 && strings.EqualFold(host[:4], "www.") {
		host = host[4:]
	}
	var segs []string
	for _, p := range strings.Split(path, "/") {
		if p != "" {
			segs = append(segs, p)
		}
	}
	if n := len(segs); n >= 3 && (n > 4 || utf8.RuneCountInString(raw) > 100) {
		keep := min(2, n-2)
		segs = append(append(segs[:keep:keep], "…"), segs[n-1])
	}
	for i, p := range segs {
		if p != "…" {
			segs[i] = slugDisplaySegment(p)
		}
	}
	out := host
	if len(segs) > 0 {
		out += "/" + strings.Join(segs, "/")
	} else if (hasQuery && query != "") || frag != "" {
		out += "/"
	}
	if hasQuery && query != "" {
		out += "?" + slugDisplayQuery(query)
	}
	if frag != "" {
		out += "#…"
	}
	return out
}

func slugDisplaySegment(p string) string {
	for i := 0; i+2 < len(p); i++ {
		if p[i] == '%' && strings.IndexByte("89abcdefABCDEF", p[i+1]) >= 0 {
			return "…"
		}
	}
	if rs := []rune(p); len(rs) > 36 {
		return string(rs[:12]) + "…"
	}
	return p
}

func slugDisplayQuery(q string) string {
	var parts []string
	for i, p := range strings.Split(q, "&") {
		if i == 2 {
			parts = append(parts, "…")
			break
		}
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" || utf8.RuneCountInString(k) > 20 {
			parts = append(parts, "…")
			break
		}
		if rs := []rune(v); len(rs) > 20 {
			parts = append(parts, k+"="+string(rs[:20])+"…")
			break
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, "&")
}
