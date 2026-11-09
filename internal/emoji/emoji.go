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
// 161 KB of JSON is 28 KB in the binary.
//
//go:embed standard.json.gz
var standardGz []byte

// StandardData returns the standard emoji table as JSON
// ({"categories":[…],"emoji":[{n,i,u,c,s}]}), decompressed on each call
// (~1 ms): callers parse it into their own tables and drop the bytes.
func StandardData() []byte {
	r, err := gzip.NewReader(bytes.NewReader(standardGz))
	if err != nil {
		return nil
	}
	b, _ := io.ReadAll(r)
	return b
}

// CDN is where Slack serves its standard emoji images.
const CDN = "https://a.slack-edge.com/production-standard-emoji-assets/16.0/"

type std struct {
	Names []string          `json:"n"`
	Image string            `json:"i"`
	Uni   string            `json:"u"`
	Cat   int               `json:"c"`
	Skins map[string]string `json:"s,omitempty"`
}

var (
	once      sync.Once
	allStd    []std // the table, in order
	byName    map[string]*std
	skinCodes = map[string]string{"2": "1F3FB", "3": "1F3FC", "4": "1F3FD", "5": "1F3FE", "6": "1F3FF"}
)

func load() {
	var f struct {
		Emoji []std `json:"emoji"`
	}
	_ = json.Unmarshal(StandardData(), &f)
	allStd = f.Emoji
	byName = make(map[string]*std, len(f.Emoji)*2)
	for i := range f.Emoji {
		e := &f.Emoji[i]
		for _, n := range e.Names {
			byName[n] = e
		}
	}
	// common aliases Slack accepts
	alias := map[string]string{"thumbsup": "+1", "thumbsdown": "-1", "simple_smile": "slightly_smiling_face"}
	for a, n := range alias {
		if e, ok := byName[n]; ok {
			if _, exists := byName[a]; !exists {
				byName[a] = e
			}
		}
	}
}

// Standard resolves a standard emoji name (optionally with ::skin-tone-N).
// It returns the image file stem (e.g. "1f44d-1f3fd") and the unicode text.
func Standard(name string) (image, text string, ok bool) {
	once.Do(load)
	skin := ""
	if i := strings.Index(name, "::skin-tone-"); i >= 0 {
		skin = name[i+len("::skin-tone-"):]
		name = name[:i]
	}
	e, ok := byName[name]
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

// StandardURL returns the Slack CDN image URL for a standard emoji.
func StandardURL(name, style, size string) (string, bool) {
	img, _, ok := Standard(name)
	if !ok {
		return "", false
	}
	if style == "" {
		style = "apple"
	}
	if size == "" {
		size = "medium"
	}
	return CDN + style + "-" + size + "/" + img + ".png", true
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
type Custom struct {
	mu  sync.RWMutex
	m   map[string]string
	ver uint64 // bumped by every change (the disk cache saves only on change)
}

// Version changes whenever the map does.
func (c *Custom) Version() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ver
}

// Set replaces the whole map; the caller must not modify m afterwards.
func (c *Custom) Set(m map[string]string) {
	c.mu.Lock()
	c.m = m
	c.ver++
	c.mu.Unlock()
}

func (c *Custom) clone(extra int) map[string]string {
	n := make(map[string]string, len(c.m)+extra)
	for k, v := range c.m {
		n[k] = v
	}
	return n
}

func (c *Custom) Put(name, v string) {
	c.mu.Lock()
	n := c.clone(1)
	n[name] = v
	c.m = n
	c.ver++
	c.mu.Unlock()
}

func (c *Custom) Remove(names ...string) {
	c.mu.Lock()
	n := c.clone(0)
	for _, name := range names {
		delete(n, name)
	}
	c.m = n
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

// StandardList returns the standard emoji in table order (a fresh slice
// sharing the name slices; don't modify them).
func StandardList() []StdEmoji {
	once.Do(load)
	out := make([]StdEmoji, len(allStd))
	for i := range allStd {
		out[i] = StdEmoji{Names: allStd[i].Names, Unicode: allStd[i].Image}
	}
	return out
}

// IsStandard reports whether name (without a skin tone) is a standard
// emoji name or one of the aliases Slack accepts.
func IsStandard(name string) bool {
	once.Do(load)
	_, ok := byName[name]
	return ok
}

// StandardUnicode is a standard emoji's image stem / unicode ("1f44d"),
// "" when name is not standard.
func StandardUnicode(name string) string {
	once.Do(load)
	if e, ok := byName[name]; ok {
		return e.Image
	}
	return ""
}
