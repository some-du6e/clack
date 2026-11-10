package i18n

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
)

// Catalog is one locale's bundle: namespace → key → translation. A key is
// the start of the hex SHA-1 of the English source (Slack ships the
// shortest unique prefix, 7 characters in practice; lookupTranslation
// tries 7, 10, 20 and 40). A translation of 0 in the JSON means the
// string is deliberately the same as the source (brand names, "OK" in
// some locales); it's stored as "" and Lookup reports it as present.
type Catalog struct {
	Locale string
	ns     map[string]map[string]string
}

// ParseBundle reads a gantry-v2-translations_<locale> JSON bundle.
func ParseBundle(locale string, b []byte) (*Catalog, error) {
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, errors.New("i18n: empty bundle")
	}
	c := &Catalog{Locale: locale, ns: make(map[string]map[string]string, len(raw))}
	for ns, keys := range raw {
		m := make(map[string]string, len(keys))
		for k, v := range keys {
			if len(v) > 0 && v[0] == '"' {
				var s string
				if json.Unmarshal(v, &s) == nil {
					m[k] = s
					continue
				}
			}
			m[k] = "" // 0: the source text
		}
		c.ns[ns] = m
	}
	return c, nil
}

// Namespaces is how many namespaces the bundle has.
func (c *Catalog) Namespaces() int {
	if c == nil {
		return 0
	}
	return len(c.ns)
}

// Len is the number of strings in the bundle.
func (c *Catalog) Len() int {
	n := 0
	if c != nil {
		for _, m := range c.ns {
			n += len(m)
		}
	}
	return n
}

var prefixes = [...]int{7, 10, 20, 40}

// Lookup finds src's translation in namespace ns. ok is false when the
// bundle doesn't have it (Slack then shows the source); text is "" with
// ok true when the bundle says the source stands (a 0 entry).
func (c *Catalog) Lookup(ns, src string) (text string, ok bool) {
	if c == nil {
		return "", false
	}
	m := c.ns[ns]
	if m == nil {
		return "", false
	}
	h := Key(src)
	for _, n := range prefixes {
		if t, ok := m[h[:n]]; ok {
			return t, true
		}
	}
	return "", false
}

// Has reports whether namespace ns has src, in any form.
func (c *Catalog) Has(ns, src string) bool { _, ok := c.Lookup(ns, src); return ok }

// NamespacesOf lists the namespaces holding src (a tool for wiring and
// tests: which of Slack's I18n instances owns a string).
func (c *Catalog) NamespacesOf(src string) []string {
	var out []string
	if c == nil {
		return nil
	}
	for ns := range c.ns {
		if c.Has(ns, src) {
			out = append(out, ns)
		}
	}
	return out
}

// Key is Slack's string id: the hex SHA-1 of the source after decode()
// turns literal \uXXXX escapes into characters.
func Key(src string) string {
	sum := sha1.Sum([]byte(decode(src)))
	return hex.EncodeToString(sum[:])
}

var escapeRE = regexp.MustCompile(`\\u[a-fA-F0-9]{4}`)

// decode is C91c's decode: "’" written out in a template becomes ’.
func decode(s string) string {
	if !containsBackslashU(s) {
		return s
	}
	return escapeRE.ReplaceAllStringFunc(s, func(e string) string {
		n, _ := strconv.ParseUint(e[2:], 16, 16)
		return string(rune(n))
	})
}

func containsBackslashU(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '\\' && s[i+1] == 'u' {
			return true
		}
	}
	return false
}
