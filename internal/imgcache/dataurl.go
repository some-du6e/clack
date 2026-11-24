package imgcache

import (
	"encoding/base64"
	"fmt"
	"image"
	"strings"
	"sync"
)

// A plugin's own images can be data: URLs: Slick's build inlines a
// plugin's .gif, .png and .svg files as data: URLs (esbuild's dataurl
// loader; oneko's sprite is one), and a plugin may make one itself. In
// Slack's desktop app, Chromium decodes such a URL in a plugin's <img src>
// or CSS background-image from the URL's own text, with no request.
// PageURL's URL loads them that way: the bytes come from the URL, never
// from the network or the disk tier, and decode through the same decoders
// and limits as a fetched image's (the header's dimensions checked
// against MaxPixels and MaxSide before anything decodes). The
// URL's own length is bounded too (maxDataURL). A data: URL as message
// content gives one is still refused (the fetchers refuse it), as Slack's
// image proxy refuses one.

// maxDataURL bounds the length of a data: URL a page's image loads from:
// its body is then at most 16 MiB, under MaxFetchBytes (what one download
// may be).
const maxDataURL = 16 << 20

// pageData is the first byte of the cache key a page's data: URL loads
// under (pageKey): a byte no UTF-8 text has, so no URL that came as text
// (message content) is one.
const pageData = "\xff"

// errDataURL is a data: URL with no body (no comma), or one whose body
// should be base64 and isn't: not an image, so not retried soon
// (permanent).
var errDataURL = fmt.Errorf("%w: imgcache: malformed data: URL", image.ErrFormat)

// PageURL is the URL a plugin page's own image loads under, for Get,
// Failed, Size and Prefetch (an <img src>, a CSS background-image): a
// data: URL's is decoded from its own text, as Chromium decodes one, and
// any other URL's is the URL. A data: URL longer than maxDataURL fails.
func PageURL(u string) string {
	if !isDataURL(u) {
		return u
	}
	if len(u) > maxDataURL {
		return pageData // its body: none, so it fails as a malformed one
	}
	return pageKey(u)
}

// pageKeys memoizes pageKey for the data: URLs pages drew lately, so a
// sprite drawn every frame doesn't build its key's string every time. It
// starts over when it holds pageKeysMax of them; URLs longer than
// pageKeyMemoMax aren't kept (at most 4 MiB held).
var pageKeys struct {
	sync.Mutex
	m map[string]string // the URL (the key's own text after pageData) → key
}

const (
	pageKeysMax    = 64
	pageKeyMemoMax = 64 << 10
)

// pageKey is the cache key a page's data: URL u loads under: pageData,
// then u.
func pageKey(u string) string {
	if len(u) > pageKeyMemoMax {
		return pageData + u
	}
	pageKeys.Lock()
	defer pageKeys.Unlock()
	if k, ok := pageKeys.m[u]; ok {
		return k
	}
	if pageKeys.m == nil || len(pageKeys.m) >= pageKeysMax {
		pageKeys.m = make(map[string]string, pageKeysMax)
	}
	k := pageData + u
	pageKeys.m[k[len(pageData):]] = k // the map's key shares the key's bytes
	return k
}

// isDataURL reports whether u is a data: URL: its scheme in any case,
// after the spaces and control characters a URL parser strips first.
func isDataURL(u string) bool {
	u = trimC0Space(u)
	return len(u) >= 5 && strings.EqualFold(u[:5], "data:")
}

// trimC0Space strips the C0 controls and spaces a URL parser strips from
// both ends of its input.
func trimC0Space(s string) string {
	i, j := 0, len(s)
	for i < j && s[i] <= ' ' {
		i++
	}
	for j > i && s[j-1] <= ' ' {
		j--
	}
	return s[i:j]
}

// dataBody is the body of the data: URL u, as the Fetch standard's data:
// URL processor reads it (and Chromium's DataURL::Parse): the text after
// the first comma without its fragment, percent-decoded, then base64-
// decoded when the part before the comma ends in ";base64" (forgivingly:
// ASCII whitespace ignored, the padding optional). The media type isn't
// used: Chromium tells a raster image's format from its bytes, as the
// decoders here do (PNG, JPEG, GIF, WebP and BMP; an SVG doesn't decode).
// A URL longer than maxDataURL is ErrTooBig, before anything is decoded.
func dataBody(u string) ([]byte, error) {
	u = trimC0Space(u)
	if len(u) > maxDataURL {
		return nil, ErrTooBig
	}
	if !isDataURL(u) {
		return nil, errDataURL
	}
	u = u[len("data:"):]
	if strings.ContainsAny(u, "\t\n\r") { // a URL parser drops ASCII tabs and newlines anywhere
		u = strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(u)
	}
	if i := strings.IndexByte(u, '#'); i >= 0 {
		u = u[:i]
	}
	mt, body, ok := strings.Cut(u, ",")
	if !ok {
		return nil, errDataURL
	}
	body = percentDecode(body)
	if !isBase64(mt) {
		return []byte(body), nil
	}
	return forgivingBase64(body)
}

// isBase64 reports whether a data: URL's media type part ends in
// ";base64": any case, spaces allowed before "base64" and whitespace
// after it.
func isBase64(mt string) bool {
	mt = strings.TrimRight(mt, asciiWhitespace)
	n := len(mt) - len("base64")
	if n < 0 || !strings.EqualFold(mt[n:], "base64") {
		return false
	}
	return strings.HasSuffix(strings.TrimRight(mt[:n], " "), ";")
}

// asciiWhitespace is the infra standard's ASCII whitespace.
const asciiWhitespace = "\t\n\f\r "

// percentDecode is the URL standard's percent-decode: "%" and two hex
// digits is that byte; any other "%" stays as it is.
func percentDecode(s string) string {
	i := strings.IndexByte(s, '%')
	if i < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(s[:i])
	for ; i < len(s); i++ {
		if c := s[i]; c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c <= '9':
		return c - '0'
	case c <= 'F':
		return c - 'A' + 10
	}
	return c - 'a' + 10
}

// forgivingBase64 is the infra standard's forgiving-base64 decode: ASCII
// whitespace dropped, one or two "=" of padding optional, the standard
// alphabet only.
func forgivingBase64(s string) ([]byte, error) {
	if strings.ContainsAny(s, asciiWhitespace) {
		s = strings.NewReplacer("\t", "", "\n", "", "\f", "", "\r", "", " ", "").Replace(s)
	}
	if len(s)%4 == 0 {
		s = strings.TrimSuffix(s, "=")
		s = strings.TrimSuffix(s, "=")
	}
	if len(s)%4 == 1 {
		return nil, errDataURL
	}
	b, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		return nil, errDataURL
	}
	return b, nil
}
