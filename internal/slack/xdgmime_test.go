package slack

import (
	"bytes"
	"encoding/binary"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// cacheGlob is one leaf of a test mime.cache's reverse suffix tree: the
// suffix as code points (so one can hold a surrogate), the type, and the
// word after the type's offset (the weight in its low byte, flags above).
type cacheGlob struct {
	suffix []rune
	mime   string
	word   uint32
}

// mimeCacheBytes writes a mime.cache (format 1.2) whose reverse suffix tree
// holds globs, each node's leaves first in globs' order and then its child
// nodes by character, the nodes' blocks breadth first. leafAt[i] is the
// offset of globs[i]'s leaf record.
func mimeCacheBytes(globs []cacheGlob) (buf []byte, leafAt []uint32) {
	type trie struct {
		char     rune
		leaves   []int
		children []*trie
	}
	root := &trie{}
	for i, g := range globs {
		n := root
		for j := len(g.suffix) - 1; j >= 0; j-- {
			k := slices.IndexFunc(n.children, func(c *trie) bool { return c.char == g.suffix[j] })
			if k < 0 {
				n.children = append(n.children, &trie{char: g.suffix[j]})
				slices.SortFunc(n.children, func(a, b *trie) int { return int(a.char) - int(b.char) })
				k = slices.IndexFunc(n.children, func(c *trie) bool { return c.char == g.suffix[j] })
			}
			n = n.children[k]
		}
		n.leaves = append(n.leaves, i)
	}
	buf = make([]byte, mimeCacheHeader)
	strAt := map[string]uint32{}
	for _, g := range globs {
		if _, ok := strAt[g.mime]; !ok {
			strAt[g.mime] = uint32(len(buf))
			buf = append(append(buf, g.mime...), 0)
		}
	}
	for len(buf)%4 != 0 {
		buf = append(buf, 0)
	}
	alias := uint32(len(buf))
	buf = append(buf, 0, 0, 0, 0) // N_ALIASES 0, standing in for every empty list
	tree := uint32(len(buf))
	buf = append(buf, make([]byte, 8)...)
	binary.BigEndian.PutUint16(buf[0:], 1)
	binary.BigEndian.PutUint16(buf[2:], 2)
	for _, off := range []int{4, 8, 12, 20, 24, 28, 32, 36} {
		binary.BigEndian.PutUint32(buf[off:], alias)
	}
	binary.BigEndian.PutUint32(buf[16:], tree)
	var order []*trie
	for q := []*trie{root}; len(q) > 0; q = q[1:] {
		order = append(order, q[0])
		q = append(q, q[0].children...)
	}
	blockAt := map[*trie]uint32{}
	at := uint32(len(buf))
	for _, n := range order {
		blockAt[n] = at
		at += uint32(12 * (len(n.leaves) + len(n.children)))
	}
	binary.BigEndian.PutUint32(buf[tree:], uint32(len(root.leaves)+len(root.children)))
	binary.BigEndian.PutUint32(buf[tree+4:], blockAt[root])
	leafAt = make([]uint32, len(globs))
	for _, n := range order {
		for _, i := range n.leaves {
			leafAt[i] = uint32(len(buf))
			buf = binary.BigEndian.AppendUint32(buf, 0)
			buf = binary.BigEndian.AppendUint32(buf, strAt[globs[i].mime])
			buf = binary.BigEndian.AppendUint32(buf, globs[i].word)
		}
		for _, c := range n.children {
			buf = binary.BigEndian.AppendUint32(buf, uint32(c.char))
			buf = binary.BigEndian.AppendUint32(buf, uint32(len(c.leaves)+len(c.children)))
			buf = binary.BigEndian.AppendUint32(buf, blockAt[c])
		}
	}
	return buf, leafAt
}

func glob(suffix, mime string, weight uint32) cacheGlob {
	return cacheGlob{suffix: []rune(suffix), mime: mime, word: weight}
}

// xdgFixture is a database over testdata/xdg's two mime.cache files, which
// update-mime-database 2.4 (shared-mime-info 2.4) wrote from the
// packages/clack-test.xml next to each (run `update-mime-database .` in a
// mime dir to write them again): home as $XDG_DATA_HOME, then share and a
// dir with no cache as $XDG_DATA_DIRS.
func xdgFixture(now func() time.Time) *xdgMime {
	env := map[string]string{
		"XDG_DATA_HOME": "testdata/xdg/home",
		"XDG_DATA_DIRS": "testdata/xdg/share:testdata/xdg/none",
	}
	return &xdgMime{dirs: func() []string { return xdgDataDirs(func(k string) string { return env[k] }) }, now: now}
}

// The real file: what platform_mime_util_linux.cc gives each extension
// over update-mime-database's own output, and FileType asking it between
// Chromium's lists. A higher weight wins (*.py at 60), a tie goes to the
// leaf the file has first (*.ts) or to the earlier dir (*.go), a later dir
// wins by weight (*.md); only "*.ext" globs count, matched exactly, so
// "HEIC" misses the lowercased "*.HEIC" and "*.C" (case-sensitive) and
// "*.c" are two; image/x-ico is image/x-icon and application/octet-stream
// none.
func TestXDGFixtureLikeChromium(t *testing.T) {
	for _, f := range []string{"testdata/xdg/home/mime/mime.cache", "testdata/xdg/share/mime/mime.cache"} {
		buf, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !parseMimeCache(buf, map[string]weightedMime{}) {
			t.Errorf("%s doesn't parse", f)
		}
	}
	x := xdgFixture(time.Now)
	for _, c := range []struct{ ext, want string }{
		{"py", "text/x-clack-python3"},
		{"ts", "video/x-clack-mp2t"},
		{"go", "text/x-clack-go"},
		{"md", "text/x-clack-md-share"},
		{"mov", "text/x-clack-mov"},
		{"rs", "text/x-clack-rust"},
		{"ico", "image/x-icon"},
		{"bin", ""},
		{"C", "text/x-clack-cpp"},
		{"c", "text/x-clack-c"},
		{"heic", "image/x-clack-heic"},
		{"HEIC", ""},
		{"PY", ""},
		{"zst", ""},
		{"txt~", ""},
		{"é", "text/x-clack-e-acute"},
		{"日本", "text/x-clack-nihon"},
		{"🦀", "text/x-clack-crab"},
		{"go/", "text/x-clack-go"}, // FilePath("foo.go/").Extension() is ".go"
		{"a/go", ""},               // and FilePath("foo.a/go")'s base name has none
		{"", ""},
	} {
		got, ok := x.platformType(c.ext)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("platformType(%q) = %q, %v; want %q", c.ext, got, ok, c.want)
		}
	}
	if len(x.files) != 2 {
		t.Errorf("read %+v, want the two caches", x.files)
	}

	defer SetPlatformMimeTypes(x.platformType)()
	for _, c := range []struct{ name, want string }{
		{"icon.svg", "image/svg+xml"},          // the primary list over the OS
		{"README.md", "text/x-clack-md-share"}, // the OS over the secondary list
		{"walkthrough.mov", "text/x-clack-mov"},
		{"main.go", "text/x-clack-go"},
		{"script.py", "text/x-clack-python3"},
		{"IMG_0001.HEIC", ""},
		{"img_0001.heic", "image/x-clack-heic"},
		{"backup.tar.zst", ""},
		{"setup.bin", "application/octet-stream"}, // the OS's octet-stream is none: the secondary list's
		{"README", ""},
	} {
		if got := FileType(c.name); got != c.want {
			t.Errorf("FileType(%q) over the fixture = %q, want %q", c.name, got, c.want)
		}
	}
}

// Synthetic: ParseMimeTypes' rules and limits, over trees written to
// order. A tie keeps the first leaf; the word's flags are ignored; the
// weight is 50 when the file ends before it; a suffix not starting with "."
// counts for nothing; characters are written as CBU8_APPEND_UNSAFE does
// (a surrogate too); a suffix past 100 bytes is skipped with its subtree.
func TestMimeCacheParseLikeChromium(t *testing.T) {
	long := ".z" + strings.Repeat("a", 98) // 100 bytes: kept
	tooLong := "." + strings.Repeat("b", 100)
	wide := "." + strings.Repeat("é", 50) // 101 bytes
	buf, _ := mimeCacheBytes([]cacheGlob{
		glob(".tie", "text/x-first", 50), glob(".tie", "text/x-second", 50),
		glob(".up", "text/x-low", 40), glob(".up", "text/x-high", 90),
		glob(".flag", "text/x-flagged", 0x100|80), glob(".flag", "text/x-plain", 70),
		glob("~", "text/x-tilde", 50), glob("", "text/x-root", 50),
		glob(".tar.zst", "application/x-zst", 50),
		{suffix: []rune{'.', 0xd800, 'x'}, mime: "text/x-surrogate", word: 50},
		{suffix: []rune{'.', 0x10ffff}, mime: "text/x-max", word: 50},
		glob(long, "text/x-long", 50), glob(tooLong, "text/x-too-long", 50), glob(wide, "text/x-wide", 50),
	})
	m := map[string]weightedMime{}
	if !parseMimeCache(buf, m) {
		t.Fatal("the tree doesn't parse")
	}
	want := map[string]weightedMime{
		"tie":              {"text/x-first", 50},
		"up":               {"text/x-high", 90},
		"flag":             {"text/x-flagged", 80},
		"tar.zst":          {"application/x-zst", 50},
		"\xed\xa0\x80x":    {"text/x-surrogate", 50},
		"\xf4\x8f\xbf\xbf": {"text/x-max", 50},
		long[1:]:           {"text/x-long", 50},
	}
	if !maps.Equal(m, want) {
		t.Errorf("parsed %q,\nwant %q", m, want)
	}

	// the last leaf's weight word cut off: 50, which beats the 40 before it
	cut, _ := mimeCacheBytes([]cacheGlob{glob(".w", "text/x-forty", 40), glob(".w", "text/x-cut", 99)})
	m = map[string]weightedMime{}
	if !parseMimeCache(cut[:len(cut)-4], m) || m["w"] != (weightedMime{"text/x-cut", 50}) {
		t.Errorf("a cache ending before the last weight: %q, want text/x-cut at 50", m)
	}

	// an earlier file's type stays unless a later one weighs more
	m = map[string]weightedMime{"tie": {"text/x-earlier", 50}, "up": {"text/x-earlier", 80}}
	parseMimeCache(buf, m)
	if m["tie"].mime != "text/x-earlier" || m["up"].mime != "text/x-high" {
		t.Errorf("over earlier types: %q", m)
	}
}

// Synthetic: a malformed cache fails, keeping what it gave before the
// failure (the stack takes the root's last child first, so ".b" is read
// before ".a").
func TestMimeCacheMalformed(t *testing.T) {
	good, leafAt := mimeCacheBytes([]cacheGlob{glob(".a", "text/x-a", 50), glob(".b", "text/x-b", 50)})
	alias := binary.BigEndian.Uint32(good[4:])
	tree := binary.BigEndian.Uint32(good[16:])
	firstRoot := binary.BigEndian.Uint32(good[tree+4:])
	patch := func(off, v uint32) []byte {
		b := bytes.Clone(good)
		binary.BigEndian.PutUint32(b[off:], v)
		return b
	}
	for _, c := range []struct {
		name string
		buf  []byte
		left []string // the extensions parsed before the failure
	}{
		{"shorter than the header", good[:39], nil},
		{"alias list past the file", patch(4, uint32(len(good))+4), nil},
		{"alias list inside the header", patch(4, 36), nil},
		{"no NUL before the alias list", func() []byte { b := bytes.Clone(good); b[alias-1] = 'x'; return b }(), nil},
		{"tree inside the header", patch(16, 36), nil},
		{"tree off a word boundary", patch(16, tree+1), nil},
		{"tree at the file's end", patch(16, uint32(len(good))), nil},
		{"too many roots", patch(tree, mimeCacheMaxChar+1), nil},
		{"roots before the tree", patch(tree+4, tree-4), nil},
		{"a character past U+10FFFF", patch(firstRoot, mimeCacheMaxChar+1), nil},
		{"a child block before the tree", patch(firstRoot+8, tree-4), nil},
		{"a type inside the header", patch(leafAt[0]+4, 36), []string{"b"}},
		{"a type at the alias list", patch(leafAt[0]+4, alias), []string{"b"}},
		{"a leaf past the file", good[:leafAt[1]+4], nil},
	} {
		m := map[string]weightedMime{}
		if parseMimeCache(c.buf, m) {
			t.Errorf("%s: parsed", c.name)
		}
		if got := slices.Sorted(maps.Keys(m)); !slices.Equal(got, c.left) {
			t.Errorf("%s: left %q, want %q", c.name, got, c.left)
		}
	}
	m := map[string]weightedMime{}
	if !parseMimeCache(good, m) || len(m) != 2 {
		t.Errorf("the unpatched cache: %v, %q", parseMimeCache(good, map[string]weightedMime{}), m)
	}
}

// Synthetic: the tree may hold 30000 nodes, not one more; a file may be 10
// MiB, not a byte more.
func TestMimeCacheLimits(t *testing.T) {
	roots := func(n int) []byte {
		buf := make([]byte, mimeCacheHeader, mimeCacheHeader+8+12*n)
		binary.BigEndian.PutUint32(buf[4:], mimeCacheHeader) // the header's last byte is NUL
		binary.BigEndian.PutUint32(buf[16:], mimeCacheHeader)
		buf = binary.BigEndian.AppendUint32(buf, uint32(n))
		buf = binary.BigEndian.AppendUint32(buf, mimeCacheHeader+8)
		for c := 1; c <= n; c++ { // a childless node per character
			buf = binary.BigEndian.AppendUint32(buf, uint32(c))
			buf = binary.BigEndian.AppendUint32(buf, 0)
			buf = binary.BigEndian.AppendUint32(buf, mimeCacheHeader)
		}
		return buf
	}
	if !parseMimeCache(roots(mimeCacheMaxNodes), map[string]weightedMime{}) {
		t.Errorf("%d nodes don't parse", mimeCacheMaxNodes)
	}
	if parseMimeCache(roots(mimeCacheMaxNodes+1), map[string]weightedMime{}) {
		t.Errorf("%d nodes parse", mimeCacheMaxNodes+1)
	}

	good, _ := mimeCacheBytes([]cacheGlob{glob(".a", "text/x-a", 50)})
	dir := t.TempDir()
	for size, ok := range map[int]bool{mimeCacheMaxSize: true, mimeCacheMaxSize + 1: false} {
		p := filepath.Join(dir, "mime.cache")
		if err := os.WriteFile(p, append(bytes.Clone(good), make([]byte, size-len(good))...), 0o644); err != nil {
			t.Fatal(err)
		}
		if buf, read := readMimeCache(p); read != ok || ok && !parseMimeCache(buf, map[string]weightedMime{}) {
			t.Errorf("a %d-byte cache: read %v, want %v", size, read, ok)
		}
	}
	if _, read := readMimeCache(dir); read {
		t.Error("read a directory as a cache")
	}
}

// Synthetic: the database reads every dir's cache once, checks the files
// it read at most every 5 s, and reads them all again when one changed or
// went away; a cache that turns up in a dir that had none isn't noticed
// until then (Chromium watches only the files it read).
func TestXDGReloadsChangedCaches(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write := func(dir, suffix, mime string, mod time.Time) {
		t.Helper()
		p := filepath.Join(dir, "mime", "mime.cache")
		buf, _ := mimeCacheBytes([]cacheGlob{glob(suffix, mime, 50)})
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, buf, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	t0 := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	now := t0
	x := &xdgMime{dirs: func() []string { return []string{a, b} }, now: func() time.Time { return now }}
	check := func(at time.Duration, ext, want string) {
		t.Helper()
		now = t0.Add(at)
		if got := x.fileMimeType(ext); got != want {
			t.Errorf("at +%v: %q is %q, want %q", at, ext, got, want)
		}
	}
	write(a, ".go", "text/x-one", t0)
	check(0, "go", "text/x-one")
	check(0, "rs", "")
	write(b, ".rs", "text/x-rust", t0)
	check(6*time.Second, "rs", "") // a's file is as it was: b's new one isn't read
	write(a, ".go", "text/x-two", t0.Add(time.Minute))
	check(8*time.Second, "go", "text/x-one") // 2 s since the last check
	check(12*time.Second, "go", "text/x-two")
	check(12*time.Second, "rs", "text/x-rust")
	if err := os.Remove(filepath.Join(a, "mime", "mime.cache")); err != nil {
		t.Fatal(err)
	}
	check(18*time.Second, "go", "")
	check(18*time.Second, "rs", "text/x-rust")
}

// Synthetic: GetXDGDataSearchLocations' dirs.
func TestXDGDataDirs(t *testing.T) {
	for _, c := range []struct {
		env  map[string]string
		want []string
	}{
		{map[string]string{"HOME": "/home/k"}, []string{"/home/k/.local/share", "/usr/local/share", "/usr/share"}},
		{map[string]string{"HOME": "/home/k/", "XDG_DATA_DIRS": ""}, []string{"/home/k/.local/share", "/usr/local/share", "/usr/share"}},
		{map[string]string{"XDG_DATA_HOME": "/x", "HOME": "/home/k", "XDG_DATA_DIRS": "/a::/b:"}, []string{"/x", "/a", "/b"}},
		{map[string]string{"TMPDIR": "/t"}, []string{"/t/.local/share", "/usr/local/share", "/usr/share"}},
		{map[string]string{}, []string{"/tmp/.local/share", "/usr/local/share", "/usr/share"}},
	} {
		if got := xdgDataDirs(func(k string) string { return c.env[k] }); !slices.Equal(got, c.want) {
			t.Errorf("%v: %q, want %q", c.env, got, c.want)
		}
	}
}
