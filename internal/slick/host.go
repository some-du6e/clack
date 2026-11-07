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
	"fmt"
	"hash"
	"mime"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/jsrt"
)

// dataNS is where a Slick blob namespace lives in Taut's data store
// (storage/slick~plugin~<id>.json; ":secrets" ones in the 0600 store).
func dataNS(ns string) string { return "slick:" + ns }

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

	// Slick's BlobStore for plugin:<id> (api.storage, Cache, stored files, MainCtx.storage/secrets)
	sync("blob", func(a []json.RawMessage) (any, error) {
		op, ns := strArg(a, 0), dataNS(strArg(a, 1))
		switch op {
		case "list":
			return s.t.DataList(ns), nil
		case "read":
			if v, ok := s.t.DataRead(ns, strArg(a, 2)); ok {
				return v, nil
			}
			return nil, nil
		case "readAll":
			prefix := strArg(a, 2)
			out := map[string]string{}
			for _, k := range s.t.DataList(ns) {
				if strings.HasPrefix(k, prefix) {
					if v, ok := s.t.DataRead(ns, k); ok {
						out[k] = v
					}
				}
			}
			return out, nil
		case "write":
			s.t.DataWrite(ns, strArg(a, 2), strArg(a, 3))
			return true, nil
		case "delete":
			s.t.DataDelete(ns, strArg(a, 2))
			return true, nil
		case "clear":
			s.t.DataClear(ns)
			return true, nil
		}
		return nil, fmt.Errorf("slick: blob %s", op)
	})

	// api.settings.set: persists, then applies (live or restart) like a change in Preferences
	async("setSetting", func(ctx context.Context, a []json.RawMessage) (any, error) {
		id, key := strArg(a, 0), strArg(a, 1)
		var v any
		if len(a) > 2 {
			_ = json.Unmarshal(a[2], &v)
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

	// api.storedFileUrl for a desktop `file` setting (a path): the file's bytes
	sync("readFile", func(a []json.RawMessage) (any, error) {
		path := s.fileSetting(strArg(a, 0), strArg(a, 1))
		if path == "" {
			return nil, nil
		}
		b, err := readCapped(path)
		if err != nil {
			return nil, nil
		}
		return map[string]any{"data": base64.StdEncoding.EncodeToString(b), "type": mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))}, nil
	})

	// node:fs for main halves: read-only, and only the files a plugin's
	// `file` settings point at (what CustomFonts / CustomSounds serve)
	sync("fs", func(a []json.RawMessage) (any, error) {
		op, path := strArg(a, 0), expandHome(strArg(a, 1))
		if !s.allowedFile(path) {
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
			r, err := filepath.EvalSymlinks(path)
			if err != nil {
				return nil, nil
			}
			abs, _ := filepath.Abs(r)
			return abs, nil
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
			return runtime.GOOS, nil
		case "cwd":
			wd, _ := os.Getwd()
			return wd, nil
		}
		return nil, nil
	})

	sync("openURL", func(a []json.RawMessage) (any, error) {
		if s.opts.OpenURL != nil {
			s.opts.OpenURL(strArg(a, 0))
		}
		return nil, nil
	})

	async("openFile", func(ctx context.Context, a []json.RawMessage) (any, error) {
		if s.opts.OpenFile == nil {
			return "", nil
		}
		p, err := s.opts.OpenFile(strArg(a, 0), strArg(a, 1))
		if err != nil {
			return "", nil
		}
		return p, nil
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

// fileSetting is the path in plugin id's `file` setting key, or "".
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
	v, _ := s.settingsFor(p)[key].(string)
	if v == "" || strings.HasPrefix(v, "stored:") {
		return ""
	}
	return expandHome(v)
}

// allowedFile: path (or what it resolves to) is some plugin's `file`
// setting.
func (s *Slick) allowedFile(path string) bool {
	if path == "" {
		return false
	}
	canon := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return p
		}
		return abs
	}
	want := canon(path)
	s.mu.Lock()
	ids := make([]string, 0, len(s.plugins))
	for id := range s.plugins {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.mu.Lock()
		p := s.plugins[id]
		var keys []string
		if p != nil {
			for _, k := range p.meta.Settings.Keys {
				if p.meta.Settings.Settings[k].Type == "file" {
					keys = append(keys, k)
				}
			}
		}
		s.mu.Unlock()
		for _, k := range keys {
			if f := s.fileSetting(id, k); f != "" && canon(f) == want {
				return true
			}
		}
	}
	return false
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
