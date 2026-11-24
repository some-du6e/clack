package slick

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"log"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/jsrt"
)

// dataNS is where a Slick blob namespace lives in Taut's data store
// (storage/slick~plugin~<id>.json; ":secrets" ones in the 0600 store).
func dataNS(ns string) string { return "slick:" + ns }

// blobNamespace is what Slick's blobStore is opened with here.
var blobNamespace = regexp.MustCompile(`^plugin:[A-Za-z0-9_.-]{1,100}(:secrets)?$`)

func strArg(a []json.RawMessage, i int) string {
	var s string
	if i < len(a) {
		_ = json.Unmarshal(a[i], &s)
	}
	return s
}

// installHost registers __clack.slick_* in a fresh runtime (Taut calls it
// before the prelude).
func (s *Slick) installHost(rt *jsrt.Runtime) {
	sync := func(name string, fn func(a []json.RawMessage) (any, error)) { rt.ExposeJSON("slick_"+name, fn) }
	async := func(name string, fn func(ctx context.Context, a []json.RawMessage) (any, error)) {
		rt.ExposeAsync("slick_"+name, fn)
	}

	// Slick's BlobStore for plugin:<id> (api.storage, Cache, stored files,
	// MainCtx.storage/secrets). Only plugin:<id>[:secrets] namespaces; the
	// ones of plugins that keep message contents stay in memory unless the
	// user chose "Keep on disk" (see messages.go).
	sync("blob", func(a []json.RawMessage) (any, error) {
		op, raw := strArg(a, 0), strArg(a, 1)
		if !blobNamespace.MatchString(raw) {
			return nil, fmt.Errorf("slick: blob namespace %q isn't a plugin's", raw)
		}
		return s.blob(op, raw, strArg(a, 2), strArg(a, 3))
	})

	// api.settings.set: persists, then applies (live or restart) like a change in Preferences
	async("setSetting", func(ctx context.Context, a []json.RawMessage) (any, error) {
		id, key := strArg(a, 0), strArg(a, 1)
		var v any
		if len(a) > 2 {
			_ = json.Unmarshal(a[2], &v)
		}
		// a `file` setting's path is what the user picked: a plugin may
		// clear it or point it at its own stored blob, never at a path
		if s.isFileSetting(id, key) {
			if str, _ := v.(string); str != "" && !strings.HasPrefix(str, "stored:") {
				log.Printf("slick: %s: refused settings.set(%q) to a path (file settings are picked by the user)", id, key)
				return false, nil
			}
		}
		// saved now, applied after: the plugin may be awaiting this inside
		// its own start() (Slick applies it on the config change event too)
		if err := s.saveOption(id, key, v); err != nil {
			return false, nil
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*s.opts.LifecycleTimeout+5*time.Second)
			defer cancel()
			s.reconcile(ctx, id)
			s.changed()
		}()
		return true, nil
	})

	// main.notifications.filter: how many predicates there are, so a
	// toast asks the chain only while one is (notify.go)
	sync("notificationFilters", func(a []json.RawMessage) (any, error) {
		var n int32
		if len(a) > 0 {
			_ = json.Unmarshal(a[0], &n)
		}
		s.noteFilters.Store(n)
		return nil, nil
	})

	// api.settings.panel: the plugins with a panel on their Preferences page
	sync("panels", func(a []json.RawMessage) (any, error) {
		var ids []string
		if len(a) > 0 {
			_ = json.Unmarshal(a[0], &ids)
		}
		m := make(map[string]bool, len(ids))
		for _, id := range ids {
			m[id] = true
		}
		s.setPanels(m)
		return nil, nil
	})

	// Electron's session.setSpellCheckerEnabled / isSpellCheckerEnabled for
	// main halves: Clack's composer spellcheck (Options.SpellChecker)
	sync("spellchecker", func(a []json.RawMessage) (any, error) {
		var on bool
		if len(a) > 0 && json.Unmarshal(a[0], &on) == nil {
			if s.spellOff.Swap(!on) != !on && s.opts.SpellChecker != nil {
				s.opts.SpellChecker(on)
			}
		}
		return !s.spellOff.Load(), nil
	})

	// api.storedFileUrl for a desktop `file` setting (a path): the file's bytes
	sync("readFile", func(a []json.RawMessage) (any, error) {
		path := s.readablePath(s.fileSetting(strArg(a, 0), strArg(a, 1)))
		if path == "" {
			return nil, nil
		}
		b, err := readCapped(path)
		if err != nil {
			return nil, nil
		}
		return map[string]any{"data": base64.StdEncoding.EncodeToString(b), "type": mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))}, nil
	})

	// node:fs for main halves: read-only, and only files the user picked
	// for a plugin's `file` setting (what CustomFonts / CustomSounds serve)
	sync("fs", func(a []json.RawMessage) (any, error) {
		op := strArg(a, 0)
		path := s.allowedFile(expandHome(strArg(a, 1)))
		if path == "" {
			return nil, nil
		}
		switch op {
		case "stat":
			st, err := os.Stat(path)
			if err != nil {
				return nil, nil
			}
			return map[string]any{"file": st.Mode().IsRegular(), "dir": st.IsDir(), "size": st.Size(), "mtimeMs": st.ModTime().UnixMilli()}, nil
		case "realpath":
			return path, nil
		case "read":
			b, err := readCapped(path)
			if err != nil {
				return nil, nil
			}
			return base64.StdEncoding.EncodeToString(b), nil
		}
		return nil, fmt.Errorf("slick: fs %s", op)
	})

	sync("os", func(a []json.RawMessage) (any, error) {
		switch strArg(a, 0) {
		case "home":
			h, _ := os.UserHomeDir()
			return h, nil
		case "tmp":
			return os.TempDir(), nil
		case "platform":
			return nodePlatform(runtime.GOOS), nil
		case "arch":
			return nodeArch(runtime.GOARCH), nil
		case "cwd":
			wd, _ := os.Getwd()
			return wd, nil
		case "resources":
			return s.slackResources(), nil
		}
		return nil, nil
	})

	// MainCtx.shell.openExternal (and electron.shell): http, https and
	// mailto only, rate-limited, checked here whatever the JS side did
	sync("openURL", func(a []json.RawMessage) (any, error) {
		u, err := externalURL(strArg(a, 0))
		if err != nil {
			return nil, err
		}
		if !s.openLimit.allow(time.Now()) {
			log.Printf("slick: dropped a request to open a link (more than %d in %v)", openBurst, openWindow)
			return nil, errors.New("[slick] too many links opened; try again in a few seconds")
		}
		if s.opts.OpenURL != nil {
			s.opts.OpenURL(u)
		}
		return nil, nil
	})

	// MainCtx.dialog.openFile and the bridge's openFile: the native picker,
	// one at a time (a plugin can't stack pickers in a loop); the result is
	// the path the user chose, which doesn't make it readable by itself
	async("openFile", func(ctx context.Context, a []json.RawMessage) (any, error) {
		if s.opts.OpenFile == nil {
			return "", nil
		}
		if !s.picking.CompareAndSwap(false, true) {
			return "", nil
		}
		defer s.picking.Store(false)
		p, err := s.opts.OpenFile(strArg(a, 0), strArg(a, 1))
		if err != nil {
			return "", nil
		}
		s.notePick(strArg(a, 2), p) // a main half's pick: its id last
		return p, nil
	})

	// Electron's nativeImage for main halves (nativeimage.go): an image is
	// {png, w, h}; the empty image is null
	sync("image", func(a []json.RawMessage) (any, error) {
		img := func(i int) *nativeImg {
			var n nativeImg
			if i < len(a) && json.Unmarshal(a[i], &n) == nil && n.PNG != "" {
				return &n
			}
			return nil
		}
		num := func(i int) int {
			var n float64
			if i < len(a) {
				_ = json.Unmarshal(a[i], &n)
			}
			return int(n)
		}
		switch strArg(a, 0) {
		case "fromDataURL":
			return nativeFromDataURL(strArg(a, 1)), nil
		case "fromBuffer":
			s64 := strArg(a, 1)
			if base64.StdEncoding.DecodedLen(len(s64)) > maxImageFile {
				return nil, nil
			}
			b, err := base64.StdEncoding.DecodeString(s64)
			if err != nil {
				return nil, nil
			}
			m, err := decodeImageBytes(b)
			if err != nil {
				return nil, nil
			}
			return encodeNative(m)
		case "fromPath":
			p := s.imageFile(strArg(a, 1), strArg(a, 2))
			if p == "" {
				return nil, nil
			}
			return nativeFromPath(p), nil
		case "thumbnail":
			p := s.imageFile(strArg(a, 1), strArg(a, 2))
			if p == "" {
				return map[string]any{"error": "Failed to get thumbnail from local thumbnail cache"}, nil
			}
			n, err := nativeThumbnail(p, num(3), num(4))
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			return n, nil
		case "resize":
			in := img(1)
			if in == nil {
				return nil, nil
			}
			n, err := nativeResize(in, num(2), num(3), strArg(a, 4))
			if err != nil {
				return nil, nil
			}
			return n, nil
		case "bitmap":
			in := img(1)
			if in == nil {
				return "", nil
			}
			b, err := nativeBitmap(in)
			if err != nil {
				return "", nil
			}
			return base64.StdEncoding.EncodeToString(b), nil
		case "fromBitmap":
			s64 := strArg(a, 1)
			if base64.StdEncoding.DecodedLen(len(s64)) > maxNativeBytes {
				return nil, nil
			}
			b, err := base64.StdEncoding.DecodeString(s64)
			if err != nil {
				return nil, nil
			}
			n, err := nativeFromBitmap(b, num(2), num(3))
			if err != nil {
				return nil, nil
			}
			return n, nil
		}
		return nil, fmt.Errorf("slick: image %s", strArg(a, 0))
	})

	// app.dock.setIcon and a window's setIcon: Clack's Dock or window icon
	// (null: Clack's own)
	sync("appIcon", func(a []json.RawMessage) (any, error) {
		if s.opts.SetAppIcon == nil {
			return nil, nil
		}
		var b64 *string
		if len(a) > 0 {
			_ = json.Unmarshal(a[0], &b64)
		}
		if b64 == nil || *b64 == "" {
			s.opts.SetAppIcon(nil)
			return nil, nil
		}
		if base64.StdEncoding.DecodedLen(len(*b64)) > maxImageFile {
			return nil, errNoImage
		}
		b, err := base64.StdEncoding.DecodeString(*b64)
		if err != nil {
			return nil, err
		}
		// within the limits and fit to what an OS draws (appIconPNG)
		icon, err := appIconPNG(b)
		if err != nil {
			return nil, err
		}
		s.opts.SetAppIcon(icon)
		return nil, nil
	})
	// nativeTheme.shouldUseDarkColors, a window's isFocused, and
	// systemPreferences.getUserDefault
	sync("nativeTheme", func(a []json.RawMessage) (any, error) { return s.dark.Load(), nil })
	sync("focused", func(a []json.RawMessage) (any, error) { return s.focused.Load(), nil })
	sync("userDefault", func(a []json.RawMessage) (any, error) {
		if s.opts.UserDefault == nil {
			return nil, nil
		}
		return s.opts.UserDefault(strArg(a, 0), strArg(a, 1)), nil
	})

	// node:crypto createHash for main halves
	sync("hash", func(a []json.RawMessage) (any, error) {
		data, err := base64.StdEncoding.DecodeString(strArg(a, 1))
		if err != nil {
			return nil, err
		}
		var h hash.Hash
		switch strArg(a, 0) {
		case "sha256":
			h = sha256.New()
		case "sha1":
			h = sha1.New()
		case "sha512":
			h = sha512.New()
		case "md5":
			h = md5.New()
		default:
			return nil, fmt.Errorf("slick: hash %s isn't supported", strArg(a, 0))
		}
		h.Write(data)
		return hex.EncodeToString(h.Sum(nil)), nil
	})

	// the bridge's initialSettings / readSettings: settings.json as Slick wrote it
	sync("settingsText", func(a []json.RawMessage) (any, error) {
		b, err := os.ReadFile(s.path("settings.json"))
		if err != nil {
			return "{}", nil
		}
		return string(b), nil
	})

	// MainCtx.sessions.navigate: Clack has one session; deep links go to the UI
	sync("navigate", func(a []json.RawMessage) (any, error) { return nil, nil })
}

func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return h + p[1:]
		}
	}
	return p
}

func readCapped(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > maxStoredFile {
		return nil, fmt.Errorf("not a readable file")
	}
	return os.ReadFile(path)
}

// fileSetting is the path the user picked for plugin id's `file` setting
// key, or "". Only a value stored in settings.json counts (Preferences, or a
// settings.json the user dropped in): never the schema's default, which the
// plugin wrote, and never a path the plugin set itself (setSetting refuses).
func (s *Slick) fileSetting(id, key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.plugins[id]
	if p == nil {
		return ""
	}
	set, ok := p.meta.Settings.Settings[key]
	if !ok || set.Type != "file" {
		return ""
	}
	v, _ := s.cfg.Plugins[id][key].(string)
	if v == "" || strings.HasPrefix(v, "stored:") {
		return ""
	}
	return expandHome(v)
}

func (s *Slick) isFileSetting(id, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.plugins[id]
	return p != nil && p.meta.Settings.Settings[key].Type == "file"
}

// canonical is p made absolute with every symlink resolved, or "".
func canonical(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	r, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return ""
	}
	return filepath.Clean(r)
}

// protectedRoots are directories plugins never read from, even when a
// file setting points there: Clack's config dir (creds.json, Taut's data
// and secret stores, Slick's own files) and the kernel's pseudo-files.
func (s *Slick) protectedRoots() []string {
	roots := []string{clackHome(), s.dir, s.t.Dir()}
	if filepath.Base(s.dir) == "slick" {
		roots = append(roots, filepath.Dir(s.dir)) // $CLACK_HOME/slick: $CLACK_HOME
	}
	if runtime.GOOS == "linux" {
		roots = append(roots, "/proc", "/sys", "/dev")
	}
	var out []string
	for _, r := range roots {
		out = append(out, filepath.Clean(r))
		if c := canonical(r); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func within(path, root string) bool {
	if root == "" || root == "." {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// readablePath is path's canonical form when a plugin may read it, or "".
// The file is read through the resolved path, so a symlink that is
// swapped later is checked again on the next read (between the check and
// the read only someone who can already write the user's files could swap
// it; plugins can't write files).
func (s *Slick) readablePath(path string) string {
	c := canonical(path)
	if c == "" {
		return ""
	}
	for _, root := range s.protectedRoots() {
		if within(c, root) {
			log.Printf("slick: refused to let a plugin read a file in %s", root)
			return ""
		}
	}
	return c
}

// allowedFile is path's canonical form when it (or what it resolves to)
// is a file the user picked for one of the plugins' `file` settings, or
// "". Main halves all share one runtime, so this can't tell which
// plugin asks; every picked file is readable by each of them.
func (s *Slick) allowedFile(path string) string {
	want := s.readablePath(path)
	if want == "" {
		return ""
	}
	s.mu.Lock()
	var picked []string
	for id, p := range s.plugins {
		for _, k := range p.meta.Settings.Keys {
			if p.meta.Settings.Settings[k].Type != "file" {
				continue
			}
			if v, _ := s.cfg.Plugins[id][k].(string); v != "" && !strings.HasPrefix(v, "stored:") {
				picked = append(picked, expandHome(v))
			}
		}
	}
	s.mu.Unlock()
	for _, f := range picked {
		if canonical(f) == want {
			return want
		}
	}
	return ""
}

// slackResources is the installed Slack's resources folder (cached):
// process.resourcesPath for main halves.
func (s *Slick) slackResources() string {
	s.resources.once.Do(func() { s.resources.path = slackResources() })
	return s.resources.path
}

// notePick keeps a file the user picked through main half owner's
// dialog.openFile: the image is that plugin's to decode (imageFile), though
// never its bytes to read.
func (s *Slick) notePick(owner, path string) {
	c := canonical(path)
	if owner == "" || c == "" {
		return
	}
	s.pickedMu.Lock()
	defer s.pickedMu.Unlock()
	if s.picked == nil {
		s.picked = map[string]map[string]bool{}
	}
	if s.picked[owner] == nil {
		s.picked[owner] = map[string]bool{}
	}
	s.picked[owner][c] = true
}

// systemIconDirs are where Linux keeps installed apps' icons (Slack's
// /usr/share/pixmaps/slack.png among them): images any app may show.
var systemIconDirs = []string{"/usr/share/pixmaps", "/usr/share/icons"}

// imageFile is path's canonical form when main half owner's nativeImage
// may decode it, or "": a file the user picked for it (its
// dialog.openFile), one of the plugins' file settings (allowedFile), or
// Slack's own icons, in the installed Slack's resources or the system's
// icon folders. A decode gives only an image's pixels, never a file's
// bytes, and never from Clack's own folders (readablePath).
func (s *Slick) imageFile(owner, path string) string {
	want := s.readablePath(expandHome(path))
	if want == "" {
		return ""
	}
	s.pickedMu.Lock()
	picked := s.picked[owner][want]
	s.pickedMu.Unlock()
	if picked {
		return want
	}
	roots := append([]string(nil), systemIconDirs...)
	if r := canonical(s.slackResources()); r != "" {
		roots = append(roots, r)
		if runtime.GOOS == "windows" {
			roots = append(roots, filepath.Dir(r)) // resources\..\slack.exe
		}
	}
	for _, root := range roots {
		if c := canonical(root); c != "" && within(want, c) {
			return want
		}
	}
	return s.allowedFile(want)
}

// ---- shell.openExternal ----

const (
	openBurst  = 3
	openWindow = 10 * time.Second
	maxOpenURL = 4096
)

// externalURL checks a URL a plugin wants opened in the OS: http, https
// or mailto, no control characters or spaces, nothing an opener could
// read as an option.
func externalURL(raw string) (string, error) {
	if raw == "" || len(raw) > maxOpenURL || strings.HasPrefix(raw, "-") {
		return "", errors.New("[slick] refusing to open that link")
	}
	for _, r := range raw {
		if r <= ' ' || r == 0x7f {
			return "", errors.New("[slick] refusing to open a link with spaces or control characters")
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errors.New("[slick] refusing to open an invalid link")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return "", errors.New("[slick] refusing to open a link without a host")
		}
	case "mailto":
	default:
		return "", fmt.Errorf("[slick] refusing to open %s: links externally (only http, https and mailto)", u.Scheme)
	}
	return u.String(), nil
}

// rateLimit allows burst events per window.
type rateLimit struct {
	mu    sync.Mutex
	times []time.Time
}

func (l *rateLimit) allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	keep := l.times[:0]
	for _, t := range l.times {
		if now.Sub(t) < openWindow {
			keep = append(keep, t)
		}
	}
	l.times = keep
	if len(l.times) >= openBurst {
		return false
	}
	l.times = append(l.times, now)
	return true
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// nodePlatform is GOOS as Node's process.platform (and Electron's) names
// it: "win32" on Windows, which a main half's Windows branch tests for
// (CustomAppIcon reads Slack's icon from slack.exe there).
func nodePlatform(goos string) string {
	switch goos {
	case "windows":
		return "win32"
	case "solaris", "illumos":
		return "sunos"
	}
	return goos // darwin, linux, freebsd, openbsd, aix, android
}

// nodeArch is GOARCH as Node's process.arch names it.
func nodeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	case "mipsle":
		return "mipsel"
	case "ppc64le":
		return "ppc64"
	}
	return goarch // arm, arm64, loong64, mips, ppc64, riscv64, s390x
}
