package slack

import (
	"fmt"
	"net/url"
	"strings"
)

// Slack's web client puts an image's URL through getImgProxyUrl (client
// boot d0up, build e277bbacc0bc) before an <img> or a CSS background
// loads it. ImgProxyURL is that function, for what an <img>'s src
// attribute says (Copy Image URL copies it, and Copy Image's markup names
// it); ProxyImage is the fetch Clack makes for the same image.

// ImgProxyOpts are getImgProxyUrl's options.
type ImgProxyOpts struct {
	// Width and Height size the proxied image (both, or neither).
	Width, Height int
	// SizeForRetina doubles them (at most 2048 a side, the ratio kept).
	SizeForRetina bool
	// Rotate asks the proxy to apply the image's EXIF orientation (o1 ro).
	Rotate bool
	// StopAnimations asks for the first frame only (o1 gu).
	StopAnimations bool
	// AnimationsOff is Accessibility > "Allow animated images and emoji"
	// off (the a11y_animations pref false): the first frame too, except
	// for an https image on Slack's own domains.
	AnimationsOff bool
}

// imgProxyRoot is getTeamImgProxyUrlRoot for a team with no image_proxy_url
// of its own: https://<the slack-imgs domain>/.
const imgProxyRoot = "https://slack-imgs.com/"

// imgProxyDirect are getRefererHidingAllowList's domains (j9Ck QL: /bpz
// slack.com, slack-imgs.com, slack-gov.com, slack-imgs-gov.com,
// slack-gov-dev.com, slack-imgs-gov-dev.com, slack-edge.com): an https
// image whose host's last two labels are one loads as it is.
var imgProxyDirect = map[string]bool{"slack.com": true, "slack-imgs.com": true, "slack-gov.com": true,
	"slack-imgs-gov.com": true, "slack-gov-dev.com": true, "slack-imgs-gov-dev.com": true, "slack-edge.com": true}

// ImgProxyURL is getImgProxyUrl(raw, o): raw itself when it is already on
// the image proxy (with gu added when a still is asked for), a /img/ path,
// or https on one of Slack's domains (imgProxyDirect); anything else goes
// through https://slack-imgs.com/ with c=1, o1 (wi, he and si for a size,
// ro, gu) and url=raw, serialized as URLSearchParams does. "" for "".
func ImgProxyURL(raw string, o ImgProxyOpts) string {
	if raw == "" {
		return ""
	}
	still := o.StopAnimations || o.AnimationsOff
	root := strings.TrimRight(imgProxyRoot, "/")
	if strings.HasPrefix(raw, root) && len(raw) > len(root) && (raw[len(root)] == '/' || raw[len(root)] == '?') {
		if still {
			return withSingleFrame(raw)
		}
		return raw
	}
	if strings.HasPrefix(raw, "/img/") {
		return raw
	}
	if strings.HasPrefix(raw, "https://") {
		if h := lastTwoLabels(raw); imgProxyDirect[h] {
			if o.StopAnimations && h == "slack-imgs.com" {
				return withSingleFrame(raw)
			}
			return raw
		}
	}
	var opts []string
	if o.Width > 0 && o.Height > 0 {
		w, h := float64(o.Width), float64(o.Height)
		if o.SizeForRetina {
			w, h = 2*w, 2*h
		}
		s := 1.0
		if w > 2048 || h > 2048 {
			s = min(2048/w, 2048/h)
		}
		if ww, hh := int(w*s), int(h*s); ww > 0 && hh > 0 {
			opts = append(opts, fmt.Sprintf("wi%d", ww), fmt.Sprintf("he%d", hh), "si")
		}
	}
	if o.Rotate {
		opts = append(opts, "ro")
	}
	if still {
		opts = append(opts, "gu")
	}
	q := searchParams{{"c", "1"}}
	if len(opts) > 0 {
		q = append(q, [2]string{"o1", strings.Join(opts, ".")})
	}
	q = append(q, [2]string{"url", raw})
	return imgProxyRoot + "?" + q.encode()
}

// AnimatedImgSrc is d0up's Ac: src with gu taken out of its o1 (and o1
// gone once empty), what PlayPauseAnimations' <img> shows while it plays;
// src itself when it has no gu or isn't a URL.
func AnimatedImgSrc(src string) string {
	base, q, frag, ok := splitQuery(src)
	if !ok {
		return src
	}
	opts := strings.Split(q.get("o1"), ".")
	has := false
	var keep []string
	for _, s := range opts {
		if s == "gu" {
			has = true
		} else if s != "" {
			keep = append(keep, s)
		}
	}
	if !has {
		return src
	}
	if len(keep) > 0 {
		q.set("o1", strings.Join(keep, "."))
	} else {
		q.del("o1")
	}
	return withParams(base, q, frag)
}

// withSingleFrame is d0up's withSingleFrameOption: u with gu in its o1.
func withSingleFrame(u string) string {
	base, q, frag, ok := splitQuery(u)
	if !ok {
		return u
	}
	var opts []string
	for _, s := range strings.Split(q.get("o1"), ".") {
		if s == "gu" {
			return u
		}
		if s != "" {
			opts = append(opts, s)
		}
	}
	q.set("o1", strings.Join(append(opts, "gu"), "."))
	return withParams(base, q, frag)
}

// lastTwoLabels is N0Jf getHostName(u, true): u's host name, cut to its
// last two labels.
func lastTwoLabels(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	h := strings.ToLower(p.Hostname())
	if l := strings.Split(h, "."); len(l) > 2 {
		h = strings.Join(l[len(l)-2:], ".")
	}
	return h
}

// searchParams is a URLSearchParams: its name/value pairs in order.
type searchParams [][2]string

func (q searchParams) get(name string) string {
	for _, p := range q {
		if p[0] == name {
			return p[1]
		}
	}
	return ""
}

// set is URLSearchParams.set: the first name's value replaced and the
// others removed, or a pair appended.
func (q *searchParams) set(name, value string) {
	out := (*q)[:0:0]
	done := false
	for _, p := range *q {
		if p[0] != name {
			out = append(out, p)
		} else if !done {
			out = append(out, [2]string{name, value})
			done = true
		}
	}
	if !done {
		out = append(out, [2]string{name, value})
	}
	*q = out
}

func (q *searchParams) del(name string) {
	out := (*q)[:0:0]
	for _, p := range *q {
		if p[0] != name {
			out = append(out, p)
		}
	}
	*q = out
}

// encode is URLSearchParams' serializer (application/x-www-form-urlencoded).
func (q searchParams) encode() string {
	var b strings.Builder
	for i, p := range q {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(formEscape(p[0]))
		b.WriteByte('=')
		b.WriteString(formEscape(p[1]))
	}
	return b.String()
}

// formEscape is the application/x-www-form-urlencoded byte serializer:
// ASCII letters, digits and *-._ as they are, a space as +, every other
// byte %XX.
func formEscape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '*', c == '-', c == '.', c == '_':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// formUnescape is the application/x-www-form-urlencoded parser's decoding:
// + as a space, %XX as its byte, any other % as it is.
func formUnescape(s string) string {
	unhex := func(c byte) int {
		switch {
		case c >= '0' && c <= '9':
			return int(c - '0')
		case c >= 'a' && c <= 'f':
			return int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			return int(c-'A') + 10
		}
		return -1
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '+' {
			b.WriteByte(' ')
			continue
		}
		if c == '%' && i+2 < len(s) {
			if h, l := unhex(s[i+1]), unhex(s[i+2]); h >= 0 && l >= 0 {
				b.WriteByte(byte(h<<4 | l))
				i += 2
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// splitQuery splits an absolute URL into what comes before its query, the
// query's pairs (as URLSearchParams parses them) and its fragment; ok is
// false when u isn't an absolute URL.
func splitQuery(u string) (base string, q searchParams, frag string, ok bool) {
	p, err := url.Parse(u)
	if err != nil || p.Scheme == "" {
		return "", nil, "", false
	}
	rest := u
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		rest, frag = rest[:i], rest[i:]
	}
	base, raw, _ := strings.Cut(rest, "?")
	for _, s := range strings.Split(raw, "&") {
		if s == "" {
			continue
		}
		k, v, _ := strings.Cut(s, "=")
		q = append(q, [2]string{formUnescape(k), formUnescape(v)})
	}
	return base, q, frag, true
}

// withParams is URL.href after a URLSearchParams change: the query re-serialized
// ("" drops the ?).
func withParams(base string, q searchParams, frag string) string {
	if len(q) == 0 {
		return base + frag
	}
	return base + "?" + q.encode() + frag
}
