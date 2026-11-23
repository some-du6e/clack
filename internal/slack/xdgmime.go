package slack

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// xdgMime is Chromium 156's shared-mime-info lookup on Linux
// (base/nix/mime_util_xdg.cc GetFileMimeType, which
// platform_mime_util_linux.cc asks): the suffix globs of every mime.cache
// in the XDG data dirs, read once and read again when a file it read
// changed or went away (checked at most every 5 s, as xdgmime does). It
// only reads mime.cache's reverse suffix tree, so only "*.ext" globs
// count, matched exactly: update-mime-database stores a glob that ignores
// case in lower case, so "SHOT.HEIC" isn't "*.heic" here, as in Chromium.
type xdgMime struct {
	dirs func() []string  // GetXDGDataSearchLocations
	now  func() time.Time // Time::Now

	mu        sync.Mutex
	loaded    bool
	types     map[string]weightedMime // extension (no ".") → its type
	files     []xdgCacheFile          // the files read, to check for changes
	lastCheck time.Time
}

// weightedMime is mime_util_xdg.h's WeightedMime.
type weightedMime struct {
	mime   string
	weight uint8
}

// xdgCacheFile is a mime.cache file read and its modification time then.
type xdgCacheFile struct {
	path    string
	modTime time.Time
}

// platformType is platform_mime_util_linux.cc's
// GetPlatformMimeTypeFromExtension over this database: the type of
// "foo."+ext; none for application/octet-stream (what GetFileMimeType
// "likes to return for everything it doesn't know") or "", and
// image/x-icon for the database's image/x-ico (KDE's name for .ico).
func (x *xdgMime) platformType(ext string) (string, bool) {
	out := x.fileMimeType(ext)
	switch out {
	case "application/octet-stream", "":
		return "", false
	case "image/x-ico":
		out = "image/x-icon"
	}
	return out, true
}

// fileMimeType is GetFileMimeType(FilePath("foo." + ext)). The path's
// Extension() is "." and the base name's text after its last "."; ext has
// no "." (it's what follows a name's last one), so that's ext without its
// trailing "/"s, or nothing when another "/" puts the base name past the
// ".".
func (x *xdgMime) fileMimeType(ext string) string {
	key := strings.TrimRight(ext, "/")
	if strings.Contains(key, "/") {
		return ""
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.loaded {
		x.types = map[string]weightedMime{}
		x.files = loadMimeCaches(x.dirs(), x.types)
		x.loaded = true
	}
	if now := x.now(); x.lastCheck.Add(5 * time.Second).Before(now) {
		if slices.ContainsFunc(x.files, xdgCacheChanged) {
			clear(x.types)
			x.files = loadMimeCaches(x.dirs(), x.types)
		}
		x.lastCheck = now
	}
	return x.types[key].mime
}

// xdgCacheChanged: f can't be stat'ed or has another modification time.
func xdgCacheChanged(f xdgCacheFile) bool {
	fi, err := os.Stat(f.path)
	return err != nil || !fi.ModTime().Equal(f.modTime)
}

// loadMimeCaches is mime_util_xdg.cc's LoadAllMimeCacheFiles: each dir's
// mime/mime.cache that exists, parsed into m in dir order (a later file's
// type wins only by a higher weight). It returns the files that parsed;
// one that fails part way leaves the types it gave before the failure in
// m, as Chromium's does.
func loadMimeCaches(dirs []string, m map[string]weightedMime) []xdgCacheFile {
	var files []xdgCacheFile
	for _, d := range dirs {
		p := appendFilePath(d, "mime/mime.cache")
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		if buf, ok := readMimeCache(p); ok && parseMimeCache(buf, m) {
			files = append(files, xdgCacheFile{p, fi.ModTime()})
		}
	}
	return files
}

// mime.cache limits and constants (mime_util_xdg.cc).
const (
	mimeCacheMaxSize  = 10 << 20 // kMaxMimeTypesFileSize: the file is ~100 KB
	mimeCacheMaxNodes = 30000    // kMaxNodes in the reverse suffix tree
	mimeCacheMaxExt   = 100      // kMaxExtSize, in bytes
	mimeCacheHeader   = 40       // kHeaderSize
	mimeCacheMaxChar  = 0x10ffff // kMaxUnicode
	defaultGlobWeight = 50       // kDefaultGlobWeight
)

// readMimeCache is ReadFileToStringWithMaxSize: a file larger than
// mimeCacheMaxSize, or one that can't be read (a directory), fails.
func readMimeCache(path string) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, mimeCacheMaxSize+1))
	if err != nil || len(buf) > mimeCacheMaxSize {
		return nil, false
	}
	return buf, true
}

// parseMimeCache is mime_util_xdg.cc's ParseMimeTypes over a mime.cache's
// bytes (shared-mime-info spec 0.21; big-endian CARD32s): the header's
// ALIAS_LIST_OFFSET (at 4; the byte before it must be NUL, which ends every
// string below it) and REVERSE_SUFFIX_TREE_OFFSET (at 16), then the tree
// from its N_ROOTS and FIRST_ROOT_OFFSET, depth first off a stack. A node
// is CHARACTER, N_CHILDREN, FIRST_CHILD_OFFSET and prepends its character
// (UTF-8, a surrogate's three bytes too) to its parent's suffix; a leaf is
// 0, MIME_TYPE_OFFSET and a word whose low byte is the weight (50 when the
// file ends first; the case-sensitive flag isn't read). A leaf counts only
// under a suffix starting with ".", for the text after it, and replaces an
// earlier type for that extension only with a higher weight. Every word
// read must lie in the file on a 4-byte boundary, within its field's
// range, or the parse fails; so does a tree of more than 30000 nodes. A
// suffix longer than 100 bytes is skipped with its subtree.
func parseMimeCache(buf []byte, m map[string]weightedMime) bool {
	if len(buf) < mimeCacheHeader {
		return false
	}
	size := uint64(len(buf))
	aliasList, ok := readCacheWord(buf, 4, mimeCacheHeader, size)
	if !ok || buf[aliasList-1] != 0 {
		return false
	}
	tree, ok := readCacheWord(buf, 16, mimeCacheHeader, size)
	if !ok {
		return false
	}
	type node struct {
		ext                   string
		nChildren, firstChild uint32
	}
	var root node
	if root.nChildren, ok = readCacheWord(buf, tree, 0, mimeCacheMaxChar); !ok {
		return false
	}
	if root.firstChild, ok = readCacheWord(buf, tree+4, uint64(tree), size); !ok {
		return false
	}
	stack := []node{root}
	nodes := 0
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		p := n.firstChild
		for i := uint32(0); i < n.nChildren; i++ {
			c, ok := readCacheWord(buf, p, 0, mimeCacheMaxChar)
			if !ok {
				return false
			}
			p += 4
			if c == 0 { // a leaf: n.ext's type
				mt, ok := readCacheWord(buf, p, mimeCacheHeader, uint64(aliasList)-1)
				if !ok {
					return false
				}
				p += 4
				weight := uint8(defaultGlobWeight)
				if uint64(p)+3 < size {
					weight = buf[p+3]
				}
				p += 4
				if ext, ok := strings.CutPrefix(n.ext, "."); ok {
					if old, had := m[ext]; !had || weight > old.weight {
						s := buf[mt:]
						s = s[:bytes.IndexByte(s, 0)] // the NUL before aliasList ends it
						m[ext] = weightedMime{string(s), weight}
					}
				}
				continue
			}
			child := node{ext: string(appendCodePoint(nil, c)) + n.ext}
			if child.nChildren, ok = readCacheWord(buf, p, 0, mimeCacheMaxChar); !ok {
				return false
			}
			p += 4
			if child.firstChild, ok = readCacheWord(buf, p, uint64(tree), size); !ok {
				return false
			}
			p += 4
			if nodes++; nodes > mimeCacheMaxNodes {
				return false
			}
			if len(child.ext) > mimeCacheMaxExt {
				continue
			}
			stack = append(stack, child)
		}
	}
	return true
}

// readCacheWord is mime_util_xdg.cc's ReadInt: the big-endian word at off,
// which must be 4-byte aligned and in buf, and lie in [lo, hi].
func readCacheWord(buf []byte, off uint32, lo, hi uint64) (uint32, bool) {
	if uint64(off) > uint64(len(buf))-4 || off&3 != 0 {
		return 0, false
	}
	v := binary.BigEndian.Uint32(buf[off:])
	if uint64(v) < lo || uint64(v) > hi {
		return 0, false
	}
	return v, true
}

// appendCodePoint is base's WriteUnicodeCharacter (CBU8_APPEND_UNSAFE):
// c's UTF-8 bytes, a surrogate's three bytes too (utf8.AppendRune would
// write U+FFFD for one).
func appendCodePoint(b []byte, c uint32) []byte {
	switch {
	case c <= 0x7f:
		return append(b, byte(c))
	case c <= 0x7ff:
		return append(b, byte(c>>6|0xc0), byte(c&0x3f|0x80))
	case c <= 0xffff:
		return append(b, byte(c>>12|0xe0), byte(c>>6&0x3f|0x80), byte(c&0x3f|0x80))
	}
	return append(b, byte(c>>18|0xf0), byte(c>>12&0x3f|0x80), byte(c>>6&0x3f|0x80), byte(c&0x3f|0x80))
}

// xdgDataDirs is base/nix/xdg_util.cc's GetXDGDataSearchLocations:
// $XDG_DATA_HOME, or the home dir's .local/share (base's GetHomeDir: $HOME,
// else $TMPDIR, else /tmp), then each dir in $XDG_DATA_DIRS (its empty
// entries skipped), or /usr/local/share and /usr/share when it's unset or
// "".
func xdgDataDirs(getenv func(string) string) []string {
	home := getenv("XDG_DATA_HOME")
	if home == "" {
		h := getenv("HOME")
		if h == "" {
			if h = getenv("TMPDIR"); h == "" {
				h = "/tmp"
			}
		}
		home = appendFilePath(h, ".local/share")
	}
	dirs := []string{home}
	if v := getenv("XDG_DATA_DIRS"); v != "" {
		for _, d := range strings.Split(v, ":") {
			if d != "" {
				dirs = append(dirs, d)
			}
		}
	} else {
		dirs = append(dirs, "/usr/local/share", "/usr/share")
	}
	return dirs
}

// appendFilePath is base::FilePath::Append on POSIX for a relative rel:
// dir and rel with one "/" between them (none after a dir that ends in
// one, and rel alone for dir ""), uncleaned, so ".." in dir still walks
// through a symlink as Chromium's open does.
func appendFilePath(dir, rel string) string {
	switch {
	case dir == "":
		return rel
	case strings.HasSuffix(dir, "/"):
		return dir + rel
	}
	return dir + "/" + rel
}
