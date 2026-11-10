package i18n

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// state is one active locale: its catalog (nil for en-US) and the
// formatted-message cache (I18n's per-locale, per-namespace cache).
type state struct {
	locale string
	cat    *Catalog
	mu     sync.RWMutex
	msgs   map[msgKey]*Message
}

type msgKey struct{ ns, src string }

var (
	cur     atomic.Pointer[state]
	gen     atomic.Uint64
	wanted  atomic.Value // string: the locale asked for (may still be loading)
	mu      sync.Mutex   // SetLocale and listeners
	onDone  []func()
	loadSeq uint64
	// DefaultLoader loads bundles for SetLocale.
	DefaultLoader = &Loader{CacheDir: DefaultCacheDir()}
)

func init() {
	cur.Store(&state{locale: EnUS, msgs: map[msgKey]*Message{}})
	wanted.Store(EnUS)
}

// Locale is the active locale: its strings are what T returns. While a
// bundle loads it is still the previous locale.
func Locale() string { return cur.Load().locale }

// Wanted is the locale last asked for with SetLocale or Use.
func Wanted() string { return wanted.Load().(string) }

// Gen changes whenever the active locale does (caches of formatted text
// key on it).
func Gen() uint64 { return gen.Load() }

// OnChange registers f to run (on the loading goroutine) after the
// active locale changes. The UI passes its Invalidate.
func OnChange(f func()) {
	mu.Lock()
	onDone = append(onDone, f)
	mu.Unlock()
}

// Use makes c the active catalog at once (nil with locale en-US: the
// source strings). Tests, and SetLocale once a bundle is in.
func Use(locale string, c *Catalog) {
	locale = Normalize(locale)
	if locale == EnUS {
		c = nil
	}
	wanted.Store(locale)
	install(locale, c)
}

func install(locale string, c *Catalog) {
	cur.Store(&state{locale: locale, cat: c, msgs: map[msgKey]*Message{}})
	gen.Add(1)
	mu.Lock()
	fs := append([]func(){}, onDone...)
	mu.Unlock()
	for _, f := range fs {
		f()
	}
}

// SetLocale switches to loc (Slack's setLocale: ensureTranslationData,
// then the locale callbacks re-render). The bundle loads on a goroutine
// (cache, else the CDN); until it's in, the old strings stay. done (may
// be nil) gets the result on that goroutine. A failed load keeps the
// current locale, as Slack's catch does.
func SetLocale(loc string, done func(error)) {
	loc = Normalize(loc)
	wanted.Store(loc)
	mu.Lock()
	loadSeq++
	seq := loadSeq
	mu.Unlock()
	if loc == EnUS {
		if Locale() != EnUS {
			install(EnUS, nil)
		}
		if done != nil {
			done(nil)
		}
		return
	}
	if s := cur.Load(); s.locale == loc && s.cat != nil {
		if done != nil {
			done(nil)
		}
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		c, err := DefaultLoader.Load(ctx, loc)
		mu.Lock()
		stale := seq != loadSeq
		mu.Unlock()
		if err == nil && !stale {
			install(loc, c)
		}
		if done != nil {
			done(err)
		}
	}()
}

// Catalog is the active catalog (nil for en-US).
func ActiveCatalog() *Catalog { return cur.Load().cat }

// NS is one of Slack's I18n instances (new I18n("prefs")): T looks
// strings up in that namespace.
type NS string

// T is I18n#t: src's translation in the active locale, formatted with
// args (name, value pairs: T("Today at {time}", "time", clock)).
func (n NS) T(src string, args ...any) string { return T(string(n), src, args...) }

// A is T with an Args map.
func (n NS) A(src string, args Args) string { return lookup(string(n), src, args) }

// T is NS(ns).T(src, args...).
func T(ns, src string, args ...any) string {
	if len(args) == 0 {
		return lookup(ns, src, nil)
	}
	return lookup(ns, src, pairs(args))
}

func lookup(ns, src string, args Args) string { return lookupIn(ns, nil, src, args) }

// Chain is several of Slack's I18n instances tried in order: a Clack
// surface that draws strings from more than one Slack component (the
// Preferences pages: "prefs", then the sections Slack renders from other
// components). The first namespace holding src wins.
type Chain struct {
	key string
	nss []string
}

// NewChain is a Chain over nss.
func NewChain(nss ...string) *Chain { return &Chain{key: "\x00" + strings.Join(nss, "\x00"), nss: nss} }

// T is NS.T over the chain.
func (c *Chain) T(src string, args ...any) string {
	if len(args) == 0 {
		return lookupIn(c.key, c.nss, src, nil)
	}
	return lookupIn(c.key, c.nss, src, pairs(args))
}

// A is T with an Args map.
func (c *Chain) A(src string, args Args) string { return lookupIn(c.key, c.nss, src, args) }

// Translated reports whether a namespace of the chain has a translation
// of src that differs from it.
func (c *Chain) Translated(src string) bool {
	for _, ns := range c.nss {
		if Translated(ns, src) {
			return true
		}
	}
	return false
}

func pairs(args []any) Args {
	a := make(Args, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		if k, ok := args[i].(string); ok {
			a[k] = args[i+1]
		}
	}
	return a
}

// maxCached bounds a locale's message cache (callers that pass dynamic
// text, a name, mustn't grow it forever).
const maxCached = 50000

func lookupIn(ns string, chain []string, src string, args Args) string {
	s := cur.Load()
	hasBrace := strings.IndexByte(src, '{') >= 0
	if s.cat == nil && !hasBrace {
		return src // en-US: the source, unformatted (C91c: l&&!n)
	}
	k := msgKey{ns, src}
	s.mu.RLock()
	m := s.msgs[k]
	s.mu.RUnlock()
	if m == nil {
		text := src
		if s.cat != nil {
			if chain == nil {
				chain = []string{ns}
			}
			for _, n := range chain {
				if t, ok := s.cat.Lookup(n, src); ok {
					if t != "" {
						text = t
					}
					break
				}
			}
			if IsCJK(s.locale) {
				text = groupCJK(text)
			}
		}
		m = Parse(text)
		s.mu.Lock()
		if len(s.msgs) >= maxCached {
			s.msgs = map[msgKey]*Message{}
		}
		s.msgs[k] = m
		s.mu.Unlock()
	}
	return m.Format(s.locale, args)
}

// Translated reports whether the active catalog has a translation of src
// in ns that differs from it (tests: English left on screen).
func Translated(ns, src string) bool {
	c := cur.Load().cat
	t, ok := c.Lookup(ns, src)
	return ok && t != "" && t != src
}

// groupCJK is C91c's group_cjk: "[[[…]]]" groups become runs held
// together with word joiners (U+2060) between their characters and
// zero-width spaces (U+200B) around them, so lines break only between
// groups. Tags, entities and {placeholders} are left alone.
func groupCJK(s string) string {
	if !strings.Contains(s, "[[[") {
		return s
	}
	var b strings.Builder
	for {
		i := strings.Index(s, "[[[")
		if i < 0 {
			b.WriteString(s)
			break
		}
		j := strings.Index(s[i+3:], "]]]")
		if j < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		inner := s[i+3 : i+3+j]
		b.WriteString("​")
		// keep {…} placeholders whole; join everything else
		first := true
		for k := 0; k < len(inner); {
			if inner[k] == '{' {
				if e := strings.IndexByte(inner[k:], '}'); e > 0 {
					if !first {
						b.WriteString("⁠")
					}
					b.WriteString(inner[k : k+e+1])
					k += e + 1
					first = false
					continue
				}
			}
			r := []rune(inner[k:])[0]
			if !first {
				b.WriteString("⁠")
			}
			b.WriteRune(r)
			k += len(string(r))
			first = false
		}
		b.WriteString("​")
		s = s[i+3+j+3:]
	}
	return b.String()
}

// StripTags removes Slack's inline tags ("<b>", "</link>") from a
// formatted string, for plain-text surfaces that draw the parts alike
// (rt() would render them as elements).
func StripTags(s string) string {
	if strings.IndexByte(s, '<') < 0 {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '<' {
			if j := strings.IndexByte(s[i:], '>'); j > 0 && isTag(s[i+1:i+j]) {
				i += j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func isTag(t string) bool {
	t = strings.TrimPrefix(t, "/")
	if t == "" {
		return false
	}
	for _, c := range t {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == ' ' || c == '=' || c == '"') {
			return false
		}
	}
	return true
}
