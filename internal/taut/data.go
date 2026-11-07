package taut

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Plugin data on disk, under Dir:
//
//	storage/<namespace>.json  api.storage, api.Cache, localStorage: the
//	                          plugin's own data (Taut keeps the same)
//	secrets.json (0600)       opt.secret values and saved accounts
//	secrets/<namespace>.json  api.secrets (0600)
//
// Clack itself never stores message contents. What a plugin chooses to
// keep in its own storage is the plugin's data, exactly as in Taut.
//
// Everything is held in memory and written back shortly after a change
// (and on Close), so plugin reads never touch the disk.

const flushDelay = 500 * time.Millisecond

type dataStore struct {
	dir string

	mu      sync.Mutex
	ns      map[string]map[string]string // namespace → key → value
	loaded  map[string]bool
	dirty   map[string]bool
	secrets map[string]string // secret name → value
	secLoad bool
	secDirt bool
	timer   *time.Timer
}

func newDataStore(dir string) *dataStore {
	return &dataStore{dir: dir, ns: map[string]map[string]string{}, loaded: map[string]bool{}, dirty: map[string]bool{}}
}

// fileFor maps a namespace like "plugin:Nicknames:storage" to a file name.
// Plugins choose namespaces freely (__clack.taut_kv), so the name is
// always one path element inside storage/ or secrets/: ":" becomes "~", and
// a namespace with any other character outside [A-Za-z0-9._-], a leading
// ".", a Windows device name or more than 120 bytes gets a hash suffix
// instead, so "a/b", "a~b" and "a:b" don't share a file. (Isolation between
// plugins isn't the point: every plugin can call taut_kv with any
// namespace, and has the session anyway. Staying inside Dir is.)
func (d *dataStore) fileFor(ns string) string {
	safe := nsFileName(ns)
	if strings.HasSuffix(ns, ":secrets") {
		// api.secrets: the separate secret store, never next to plain storage
		return filepath.Join(d.dir, "secrets", safe+".json")
	}
	return filepath.Join(d.dir, "storage", safe+".json")
}

func nsFileName(ns string) string {
	plain := ns != "" && len(ns) <= 120 && ns[0] != '.'
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		case r == ':':
			return '~'
		}
		plain = false
		return '~'
	}, ns)
	if plain && !windowsDeviceName(safe) {
		return safe
	}
	// no "." left either: "CON.x" or ".x" can't come back that way
	safe = strings.ReplaceAll(safe, ".", "~")
	if len(safe) > 80 {
		safe = safe[:80]
	}
	h := sha256.Sum256([]byte(ns))
	return safe + "~" + hex.EncodeToString(h[:8])
}

func (d *dataStore) load(ns string) map[string]string {
	if m := d.ns[ns]; m != nil || d.loaded[ns] {
		if m == nil {
			m = map[string]string{}
			d.ns[ns] = m
		}
		return m
	}
	m := map[string]string{}
	if b, err := os.ReadFile(d.fileFor(ns)); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	d.ns[ns] = m
	d.loaded[ns] = true
	return m
}

func (d *dataStore) list(ns string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.load(ns)
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (d *dataStore) read(ns, key string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	v, ok := d.load(ns)[key]
	return v, ok
}

func (d *dataStore) write(ns, key, value string) {
	d.mu.Lock()
	d.load(ns)[key] = value
	d.dirty[ns] = true
	d.schedule()
	d.mu.Unlock()
}

func (d *dataStore) delete(ns, key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.load(ns)
	if _, ok := m[key]; !ok {
		return true
	}
	delete(m, key)
	d.dirty[ns] = true
	d.schedule()
	return true
}

func (d *dataStore) clear(ns string) {
	d.mu.Lock()
	d.ns[ns] = map[string]string{}
	d.loaded[ns] = true
	d.dirty[ns] = true
	d.schedule()
	d.mu.Unlock()
}

// snapshot copies a namespace (for rollback around installs).
func (d *dataStore) snapshot(ns string) map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.load(ns)
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (d *dataStore) restore(ns string, snap map[string]string) {
	d.mu.Lock()
	m := make(map[string]string, len(snap))
	for k, v := range snap {
		m[k] = v
	}
	d.ns[ns] = m
	d.loaded[ns] = true
	d.dirty[ns] = true
	d.schedule()
	d.mu.Unlock()
}

// ---- secrets ----

func (d *dataStore) secretsPath() string { return filepath.Join(d.dir, "secrets.json") }

func (d *dataStore) loadSecrets() {
	if d.secLoad {
		return
	}
	d.secLoad = true
	d.secrets = map[string]string{}
	if b, err := os.ReadFile(d.secretsPath()); err == nil {
		_ = json.Unmarshal(b, &d.secrets)
	}
}

func (d *dataStore) readSecret(name string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loadSecrets()
	v, ok := d.secrets[name]
	return v, ok
}

func (d *dataStore) writeSecret(name, value string) {
	d.mu.Lock()
	d.loadSecrets()
	if value == "" {
		delete(d.secrets, name)
	} else {
		d.secrets[name] = value
	}
	d.secDirt = true
	d.schedule()
	d.mu.Unlock()
}

func (d *dataStore) secretNames(prefix string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loadSecrets()
	var out []string
	for k := range d.secrets {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// ---- writing back ----

func (d *dataStore) schedule() {
	if d.timer != nil {
		return
	}
	d.timer = time.AfterFunc(flushDelay, func() { _ = d.flush() })
}

func (d *dataStore) flush() error {
	d.mu.Lock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	type job struct {
		path string
		data []byte
		mode os.FileMode
	}
	var jobs []job
	for ns := range d.dirty {
		b, _ := json.Marshal(d.ns[ns])
		jobs = append(jobs, job{d.fileFor(ns), b, 0o600})
	}
	d.dirty = map[string]bool{}
	if d.secDirt {
		b, _ := json.MarshalIndent(d.secrets, "", "  ")
		jobs = append(jobs, job{d.secretsPath(), b, 0o600})
		d.secDirt = false
	}
	d.mu.Unlock()
	var errs []error
	for _, j := range jobs {
		if err := writeFileAtomic(j.path, j.data, j.mode); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// writeFileAtomic writes path through a fresh O_EXCL temp file renamed over
// it: a symlink planted at path (or at a guessable temp name) is replaced,
// never written through, and readers never see a half-written file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	err = f.Chmod(mode)
	if _, werr := f.Write(data); err == nil {
		err = werr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// ---- jsrt localStorage / sessionStorage ----

type kv struct {
	d  *dataStore
	ns string
}

func (d *dataStore) localStorage(ns string) *kv {
	if ns == "sessionStorage" {
		d.mu.Lock()
		d.loaded["\x00session"] = true
		d.mu.Unlock()
		return &kv{d: d, ns: "\x00session"}
	}
	return &kv{d: d, ns: "localStorage"}
}

func (k *kv) Get(key string) (string, bool) { return k.d.read(k.ns, key) }
func (k *kv) Set(key, value string) {
	if k.ns == "\x00session" {
		k.d.mu.Lock()
		k.d.load(k.ns)[key] = value
		k.d.mu.Unlock()
		return
	}
	k.d.write(k.ns, key, value)
}
func (k *kv) Delete(key string) {
	k.d.delete(k.ns, key)
	k.d.forgetSession(k.ns)
}
func (k *kv) Keys() []string { return k.d.list(k.ns) }
func (k *kv) Clear() {
	k.d.clear(k.ns)
	k.d.forgetSession(k.ns)
}

// forgetSession keeps sessionStorage out of the write-back.
func (d *dataStore) forgetSession(ns string) {
	if ns != "\x00session" {
		return
	}
	d.mu.Lock()
	delete(d.dirty, ns)
	d.mu.Unlock()
}
