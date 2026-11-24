// Package emoji resolves standard (unicode) and custom Slack emoji names.
package emoji

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"sync"
)

// standard.json.gz is the standard emoji table (iamcal order), gzipped:
// 161 KB of JSON is 28 KB in the binary. It is Slack's 17.0 set; the 16.0
// set is the same table less the emoji_v17 additions (set.go).
//
//go:embed standard.json.gz
var standardGz []byte

func standardRaw() []byte {
	r, err := gzip.NewReader(bytes.NewReader(standardGz))
	if err != nil {
		return nil
	}
	b, _ := io.ReadAll(r)
	return b
}

// StandardData returns the standard emoji table of the set in use (V17)
// as JSON ({"categories":[…],"emoji":[{n,i,u,c,s}]}), built on each call
// (~1 ms; a few more for the 16.0 set): callers parse it into their own
// tables and drop the bytes, and build them again when V17 changes.
func StandardData() []byte { return StandardDataFor(V17()) }

// StandardDataFor is StandardData for the 17.0 set (v17) or the 16.0 one.
func StandardDataFor(v17 bool) []byte {
	raw := standardRaw()
	if v17 || raw == nil {
		return raw
	}
	var f struct {
		Categories json.RawMessage `json:"categories"`
	}
	_ = json.Unmarshal(raw, &f)
	once.Do(load)
	b, err := json.Marshal(struct {
		Categories json.RawMessage `json:"categories,omitempty"`
		Emoji      []std           `json:"emoji"`
	}{f.Categories, sets[0].list})
	if err != nil {
		return nil
	}
	return b
}

// CDN is where Slack serves the 16.0 set's images (emoji_v17 off), CDN17
// the 17.0 set's (getEmojiVersion, Ak7A). The 16.0 directories have no
// image the 17.0 set added: those answer 403.
const (
	CDN   = assetBase + "16.0/"
	CDN17 = assetBase + "17.0/"
)

// Base is the CDN of the set in use.
func Base() string {
	if V17() {
		return CDN17
	}
	return CDN
}

// TrimCDN cuts either set's CDN off a standard emoji URL ("google-medium/
// 1f44d.png"); ok is false for any other URL.
func TrimCDN(u string) (rest string, ok bool) {
	if rest, ok = strings.CutPrefix(u, CDN); ok {
		return rest, true
	}
	return strings.CutPrefix(u, CDN17)
}

type std struct {
	Names []string          `json:"n"`
	Image string            `json:"i"`
	Uni   string            `json:"u"`
	Cat   int               `json:"c"`
	Skins map[string]string `json:"s,omitempty"`
}

var (
	once      sync.Once
	sets      [2]*set // [0] the 16.0 set, [1] the 17.0 one (the embedded table)
	skinCodes = map[string]string{"2": "1F3FB", "3": "1F3FC", "4": "1F3FD", "5": "1F3FE", "6": "1F3FF"}
)

// set is one of Slack's standard emoji tables, parsed.
type set struct {
	list   []std // the table, in order
	byName map[string]*std

	uniOnce sync.Once // MatchUnicode's index
	uni     map[string]uniEnt
	uniMax  int

	natOnce sync.Once // Native
	nat     map[string]string
	natMax  int
}

func load() {
	var f struct {
		Emoji []std `json:"emoji"`
	}
	_ = json.Unmarshal(standardRaw(), &f)
	sets[1] = newSet(f.Emoji)
	sets[0] = newSet(without17(f.Emoji))
}

func newSet(list []std) *set {
	s := &set{list: list, byName: make(map[string]*std, len(list)*2)}
	for i := range list {
		e := &list[i]
		for _, n := range e.Names {
			s.byName[n] = e
		}
	}
	// common aliases Slack accepts
	alias := map[string]string{"thumbsup": "+1", "thumbsdown": "-1", "simple_smile": "slightly_smiling_face"}
	for a, n := range alias {
		if e, ok := s.byName[n]; ok {
			if _, exists := s.byName[a]; !exists {
				s.byName[a] = e
			}
		}
	}
	return s
}

// cur is the set in use.
func cur() *set { return setFor(V17()) }

// setFor is the 17.0 set (v17) or the 16.0 one.
func setFor(v17 bool) *set {
	once.Do(load)
	if v17 {
		return sets[1]
	}
	return sets[0]
}

// Standard resolves a standard emoji name (optionally with ::skin-tone-N)
// in the set in use. It returns the image file stem (e.g. "1f44d-1f3fd")
// and the unicode text.
func Standard(name string) (image, text string, ok bool) { return cur().standard(name) }

func (s *set) standard(name string) (image, text string, ok bool) {
	skin := ""
	if i := strings.Index(name, "::skin-tone-"); i >= 0 {
		skin = name[i+len("::skin-tone-"):]
		name = name[:i]
	}
	e, ok := s.byName[name]
	if !ok {
		return "", "", false
	}
	image, uni := e.Image, e.Uni
	if code, has := skinCodes[skin]; has && e.Skins != nil {
		if img, ok := e.Skins[code]; ok {
			image = img
			uni = strings.ToUpper(img)
		}
	}
	return image, unicodeText(uni), true
}

// StandardURL returns the Slack CDN image URL for a standard emoji, in
// style ("" for Style(): Google's set except on macOS).
func StandardURL(name, style, size string) (string, bool) {
	v17 := V17()
	img, _, ok := setFor(v17).standard(name)
	if !ok {
		return "", false
	}
	if style == "" {
		style = Style()
	}
	if size == "" {
		size = "medium"
	}
	base := CDN
	if v17 {
		base = CDN17
	}
	return base + style + "-" + size + "/" + img + ".png", true
}

func unicodeText(uni string) string {
	var b strings.Builder
	for _, p := range strings.Split(uni, "-") {
		if v, err := strconv.ParseUint(p, 16, 32); err == nil {
			b.WriteRune(rune(v))
		}
	}
	return b.String()
}

// Custom holds a workspace's custom emoji (name -> url or "alias:name").
// It is copy-on-write: writers build a new map and swap it in, so a map
// returned by Snapshot is never mutated afterwards and may be iterated
// without locks while emoji_changed events arrive.
//
// Two writers fill it, as Slack's one custom emoji store has them: the
// server's list (emoji.list, emoji_changed: Set, Put, Remove) and the
// plugins' upsertEmoji (Upsert). Slack's store keeps whichever wrote a
// name last and a reload upserts too, so a plugin's emoji survive Set
// unless the list itself names them. The plugins' entries live only in
// memory: ServerSnapshot and ServerVersion are the list's part, what the
// disk cache keeps (a plugin's image can be an object URL that dies with
// its runtime).
type Custom struct {
	mu     sync.RWMutex
	m      map[string]string // what's read: the server's, the upserts over it
	server map[string]string // the server's part when upserts differ it from m (nil: m)
	up     map[string]string // the plugins' upserts still in force (nil: none)
	ver    uint64            // bumped by every change
	sver   uint64            // bumped by every change of the server's part
}

// Version changes whenever the map does.
func (c *Custom) Version() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ver
}

// ServerVersion changes whenever the server's part does (the disk cache
// saves only on change).
func (c *Custom) ServerVersion() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sver
}

func (c *Custom) serverLocked() map[string]string {
	if c.server != nil {
		return c.server
	}
	return c.m
}

// installLocked makes server the server's part, the upserts over it.
func (c *Custom) installLocked(server map[string]string) {
	if len(c.up) == 0 {
		c.m, c.server, c.up = server, nil, nil
	} else {
		if server == nil { // never nil beside upserts: nil means "the same as m"
			server = map[string]string{}
		}
		n := make(map[string]string, len(server)+len(c.up))
		for k, v := range server {
			n[k] = v
		}
		for k, v := range c.up {
			n[k] = v
		}
		c.m, c.server = n, server
	}
	c.ver++
	c.sver++
}

// dropUpsLocked forgets the upserts of names the server wrote since.
func (c *Custom) dropUpsLocked(has func(name string) bool) {
	if len(c.up) == 0 {
		return
	}
	var keep map[string]string
	for k, v := range c.up {
		if !has(k) {
			if keep == nil {
				keep = map[string]string{}
			}
			keep[k] = v
		}
	}
	c.up = keep
}

// Set replaces the server's part; the caller must not modify m afterwards.
func (c *Custom) Set(m map[string]string) {
	c.mu.Lock()
	c.dropUpsLocked(func(name string) bool { _, ok := m[name]; return ok })
	c.installLocked(m)
	c.mu.Unlock()
}

// SetIfEmpty installs m only while the server's part is empty (nothing
// loaded yet) and reports whether it did: a fuller map installed
// meanwhile stays.
func (c *Custom) SetIfEmpty(m map[string]string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.serverLocked()) > 0 {
		return false
	}
	c.dropUpsLocked(func(name string) bool { _, ok := m[name]; return ok })
	c.installLocked(m)
	return true
}

func cloneMap(m map[string]string, extra int) map[string]string {
	n := make(map[string]string, len(m)+extra)
	for k, v := range m {
		n[k] = v
	}
	return n
}

func (c *Custom) Put(name, v string) {
	c.mu.Lock()
	n := cloneMap(c.serverLocked(), 1)
	n[name] = v
	c.dropUpsLocked(func(k string) bool { return k == name })
	c.installLocked(n)
	c.mu.Unlock()
}

func (c *Custom) Remove(names ...string) {
	c.mu.Lock()
	n := cloneMap(c.serverLocked(), 0)
	for _, name := range names {
		delete(n, name)
	}
	c.dropUpsLocked(func(k string) bool {
		for _, name := range names {
			if k == name {
				return true
			}
		}
		return false
	})
	c.installLocked(n)
	c.mu.Unlock()
}

// Upsert puts the plugins' entries over the map (Slack's upsertEmoji):
// each name's url, or "alias:name". They stay until the server writes
// the name (Set naming it, Put, Remove), and never reach the server's part.
func (c *Custom) Upsert(entries map[string]string) {
	if len(entries) == 0 {
		return
	}
	c.mu.Lock()
	server := c.serverLocked()
	if server == nil { // nothing loaded yet: the server's part is empty, not the upserts
		server = map[string]string{}
	}
	up := cloneMap(c.up, len(entries))
	n := cloneMap(c.m, len(entries))
	for k, v := range entries {
		up[k] = v
		n[k] = v
	}
	c.m, c.server, c.up = n, server, up
	c.ver++
	c.mu.Unlock()
}

func (c *Custom) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.m)
}

// Snapshot returns the current map. It is immutable (copy-on-write): safe
// to read and iterate concurrently, but callers must not modify it.
func (c *Custom) Snapshot() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.m
}

// ServerSnapshot is the server's part of the map (Snapshot without the
// plugins' upserts), as immutable as Snapshot's.
func (c *Custom) ServerSnapshot() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serverLocked()
}

// AliasChain is the names an alias name resolves through (its target,
// the target's target…, as Resolve follows them), nil for one that isn't
// an alias.
func (c *Custom) AliasChain(name string) []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []string
	v, has := c.m[name]
	for i := 0; has && strings.HasPrefix(v, "alias:") && i < 5; i++ {
		target := strings.TrimPrefix(v, "alias:")
		out = append(out, target)
		v, has = c.m[target]
	}
	return out
}

// Resolve returns an image URL (custom or standard) and unicode text for name.
func (c *Custom) Resolve(name, style string) (url, text string, ok bool) {
	c.mu.RLock()
	v, has := c.m[name]
	for i := 0; has && strings.HasPrefix(v, "alias:") && i < 5; i++ {
		target := strings.TrimPrefix(v, "alias:")
		if nv, ok := c.m[target]; ok {
			v = nv
		} else {
			name, has = target, false
		}
	}
	c.mu.RUnlock()
	if has {
		return v, "", true
	}
	if u, ok := StandardURL(name, style, "medium"); ok {
		_, t, _ := Standard(name)
		return u, t, true
	}
	return "", "", false
}

// StdEmoji is one standard emoji: its names (primary first) and its
// image stem, Slack's lower-case unicode ("1f44d", "263a-fe0f").
type StdEmoji struct {
	Names   []string
	Unicode string
}

// StandardList returns the set in use's standard emoji in table order (a
// fresh slice sharing the name slices; don't modify them).
func StandardList() []StdEmoji { return StandardListFor(V17()) }

// StandardListFor is StandardList for the 17.0 set (v17) or the 16.0 one.
func StandardListFor(v17 bool) []StdEmoji {
	s := setFor(v17)
	out := make([]StdEmoji, len(s.list))
	for i := range s.list {
		out[i] = StdEmoji{Names: s.list[i].Names, Unicode: s.list[i].Image}
	}
	return out
}

// IsStandard reports whether name (without a skin tone) is a standard
// emoji name of the set in use or one of the aliases Slack accepts.
func IsStandard(name string) bool {
	_, ok := cur().byName[name]
	return ok
}

// StandardUnicode is a standard emoji's image stem / unicode ("1f44d"),
// "" when name is not standard in the set in use.
func StandardUnicode(name string) string {
	if e, ok := cur().byName[name]; ok {
		return e.Image
	}
	return ""
}
