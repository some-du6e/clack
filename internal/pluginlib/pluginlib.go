// Package pluginlib is Clack's plugin Library: the official Taut and Slick
// plugins, built by upstream's own build in CI
// (.github/workflows/plugin-library.yml) and published with an index.json as
// the assets of the Clack repo's `plugins` pre-release. Preferences >
// Plugins > Browse lists them; installing one
// downloads its bundle, checks its sha256 and imports it through Taut or
// Slick as a user plugin (the bundle lands in that system's plugins/ dir).
//
// The bundles are third-party GPLv3 code (Taut by Jeremy and contributors,
// Slick by 3kh0 and contributors). Each entry records the upstream repo and
// commit it was built from. Nothing is fetched until the Library is opened;
// requests go over https with a plain client that carries no Slack cookies.
package pluginlib

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/slack/tlsfp"
)

// DefaultIndexURL is the published index (the `plugins` release's index.json).
const DefaultIndexURL = "https://github.com/some-du6e/clack/releases/download/plugins/index.json"

const (
	maxIndex  = 4 << 20
	maxBundle = 16 << 20
)

// Systems.
const (
	Taut  = "taut"
	Slick = "slick"
)

// Source is the upstream project a system's bundles were built from.
type Source struct {
	Name        string `json:"name"`
	Repo        string `json:"repo"`
	Commit      string `json:"commit"`
	License     string `json:"license"`
	LicenseFile string `json:"licenseFile"`
	Build       string `json:"build"`
}

// File is a release asset with its checksum.
type File struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Author is a credited plugin author.
type Author struct {
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	SlackID string `json:"slackId,omitempty"` // Hack Club Slack member id, when upstream names one
}

// Entry is one plugin in the Library.
type Entry struct {
	System           string   `json:"system"` // Taut | Slick
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	Category         string   `json:"category"`
	CategoryLabel    string   `json:"categoryLabel"`
	Authors          []Author `json:"authors"`
	Contributors     []string `json:"contributors"`
	HackClubOnly     bool     `json:"hackClubOnly"`
	DefaultEnabled   bool     `json:"defaultEnabled"`
	UpstreamDisabled bool     `json:"upstreamDisabled"`
	Source           string   `json:"source"` // path in the upstream repo
	File
	Main *File `json:"main,omitempty"` // a Slick plugin's privileged half
}

// Credit is the entry's authors, or its upstream contributors when it
// names none.
func (e Entry) Credit() []string {
	var out []string
	for _, a := range e.Authors {
		if a.Name != "" {
			out = append(out, a.Name)
		}
	}
	if len(out) == 0 {
		out = append(out, e.Contributors...)
	}
	return out
}

// Index is index.json.
type Index struct {
	Schema    int               `json:"schema"`
	Generated string            `json:"generated"`
	Sources   map[string]Source `json:"sources"`
	Plugins   []Entry           `json:"plugins"`
}

// For returns the entries of one system, by category label then name.
func (ix *Index) For(system string) []Entry {
	if ix == nil {
		return nil
	}
	var out []Entry
	for _, e := range ix.Plugins {
		if e.System == system {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CategoryLabel != out[j].CategoryLabel {
			return out[i].CategoryLabel < out[j].CategoryLabel
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// Options configure a Library.
type Options struct {
	IndexURL string        // "" = DefaultIndexURL
	CacheDir string        // index.json is kept here for offline listing; "" = no cache
	Client   *http.Client  // nil = a plain https-only client (no cookies)
	MaxAge   time.Duration // a cached index younger than this is used without a request; 0 = 1 hour
}

// Library fetches the index and bundles.
type Library struct {
	opts Options

	mu      sync.Mutex
	idx     *Index
	fetched time.Time
}

// New makes a Library. It does no I/O.
func New(o Options) *Library {
	if o.IndexURL == "" {
		o.IndexURL = DefaultIndexURL
	}
	if o.Client == nil {
		o.Client = plainClient()
	}
	if o.MaxAge == 0 {
		o.MaxAge = time.Hour
	}
	return &Library{opts: o}
}

// plainClient sends no cookies or credentials and refuses non-https
// redirects (GitHub release downloads redirect to its object storage).
func plainClient() *http.Client {
	return &http.Client{
		Transport: tlsfp.Mixed,
		Timeout:   60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" {
				return fmt.Errorf("refusing a redirect to %s", req.URL.Scheme)
			}
			return nil
		},
	}
}

// Cached returns the last index without any request (nil if none).
func (l *Library) Cached() *Index {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.idx == nil {
		l.idx, l.fetched = l.readCache()
	}
	return l.idx
}

// Index returns the Library's index: the cached copy when fresh, else a
// download (falling back to the cache when offline). refresh forces a download.
func (l *Library) Index(ctx context.Context, refresh bool) (*Index, error) {
	l.mu.Lock()
	if l.idx == nil {
		l.idx, l.fetched = l.readCache()
	}
	if !refresh && l.idx != nil && time.Since(l.fetched) < l.opts.MaxAge {
		ix := l.idx
		l.mu.Unlock()
		return ix, nil
	}
	l.mu.Unlock()

	b, err := l.get(ctx, l.opts.IndexURL, maxIndex)
	if err != nil {
		if c := l.Cached(); c != nil && !refresh {
			return c, nil
		}
		return nil, fmt.Errorf("couldn't load the plugin library: %w", err)
	}
	ix, err := parseIndex(b)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	l.idx, l.fetched = ix, time.Now()
	l.mu.Unlock()
	l.writeCache(b)
	return ix, nil
}

func parseIndex(b []byte) (*Index, error) {
	var ix Index
	if err := json.Unmarshal(b, &ix); err != nil {
		return nil, fmt.Errorf("the plugin library index is invalid: %w", err)
	}
	if ix.Schema != 1 {
		return nil, fmt.Errorf("the plugin library index has schema %d; update Clack", ix.Schema)
	}
	for _, e := range ix.Plugins {
		if err := validName(e.File.File); err != nil {
			return nil, err
		}
		if e.Main != nil {
			if err := validName(e.Main.File); err != nil {
				return nil, err
			}
		}
	}
	return &ix, nil
}

// validName keeps asset names to a single path element.
func validName(n string) error {
	if n == "" || n != path.Base(n) || strings.ContainsAny(n, `/\?#%`) || n == "." || n == ".." {
		return fmt.Errorf("the plugin library index names a bad file %q", n)
	}
	return nil
}

// Download fetches an entry's bundle (and its main half, for Slick) and
// checks their sha256 against the index.
func (l *Library) Download(ctx context.Context, e Entry) (code, main []byte, err error) {
	code, err = l.file(ctx, e.File)
	if err != nil {
		return nil, nil, err
	}
	if e.Main != nil {
		if main, err = l.file(ctx, *e.Main); err != nil {
			return nil, nil, err
		}
	}
	return code, main, nil
}

func (l *Library) file(ctx context.Context, f File) ([]byte, error) {
	if err := validName(f.File); err != nil {
		return nil, err
	}
	u, err := url.Parse(l.opts.IndexURL)
	if err != nil {
		return nil, err
	}
	u.Path = path.Join(path.Dir(u.Path), f.File)
	u.RawQuery, u.Fragment = "", ""
	b, err := l.get(ctx, u.String(), maxBundle)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", f.File, err)
	}
	if got := Hash(b); !strings.EqualFold(got, f.SHA256) {
		return nil, fmt.Errorf("%s failed its integrity check (sha256 %s, the library says %s)", f.File, short(got), short(f.SHA256))
	}
	return b, nil
}

func (l *Library) get(ctx context.Context, raw string, limit int64) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return nil, errors.New("the plugin library must be served over https")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream, application/json")
	res, err := l.opts.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("file too large")
	}
	return b, nil
}

func isLoopback(h string) bool { return h == "127.0.0.1" || h == "::1" || h == "localhost" }

// Hash is the hex sha256 the index uses.
func Hash(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func (l *Library) cachePath() string {
	if l.opts.CacheDir == "" {
		return ""
	}
	return filepath.Join(l.opts.CacheDir, "index.json")
}

func (l *Library) readCache() (*Index, time.Time) {
	p := l.cachePath()
	if p == "" {
		return nil, time.Time{}
	}
	st, err := os.Stat(p)
	if err != nil {
		return nil, time.Time{}
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, time.Time{}
	}
	ix, err := parseIndex(b)
	if err != nil {
		return nil, time.Time{}
	}
	return ix, st.ModTime()
}

func (l *Library) writeCache(b []byte) {
	p := l.cachePath()
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}

// State is an entry's install state.
type State int

const (
	NotInstalled    State = iota
	Installed             // the installed files match the library's
	UpdateAvailable       // installed, but the library has different files
)

// StateOf compares installed file hashes ("" = absent) with the entry.
func StateOf(e Entry, code, main string) State {
	if code == "" {
		return NotInstalled
	}
	if !strings.EqualFold(code, e.SHA256) {
		return UpdateAvailable
	}
	if e.Main != nil && !strings.EqualFold(main, e.Main.SHA256) {
		return UpdateAvailable
	}
	return Installed
}
