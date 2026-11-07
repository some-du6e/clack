package pluginlib

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeRelease struct {
	srv     *httptest.Server
	files   map[string][]byte
	hits    atomic.Int32
	cookies atomic.Int32
}

func newFakeRelease(t *testing.T, files map[string][]byte) *fakeRelease {
	f := &fakeRelease{files: files}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			f.cookies.Add(1)
		}
		name := strings.TrimPrefix(r.URL.Path, "/releases/download/plugins/")
		b, ok := f.files[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRelease) indexURL() string { return f.srv.URL + "/releases/download/plugins/index.json" }

func testIndex(t *testing.T, files map[string][]byte, mutate func(*Index)) []byte {
	ix := Index{Schema: 1, Sources: map[string]Source{
		Taut:  {Name: "Taut", Repo: "https://github.com/jeremy46231/taut", Commit: "abc1234def", License: "GPL-3.0"},
		Slick: {Name: "Slick", Repo: "https://github.com/3kh0/slick", Commit: "0123456789", License: "GPL-3.0"},
	}, Plugins: []Entry{
		{System: Taut, ID: "WhoReacted", Name: "Who Reacted", Description: "avatars", CategoryLabel: "Messages",
			Authors: []Author{{Name: "Jeremy"}}, File: File{File: "taut-WhoReacted.js", SHA256: Hash(files["taut-WhoReacted.js"])}},
		{System: Taut, ID: "AccountSwitcher", Name: "Account Switcher", CategoryLabel: "App",
			File: File{File: "taut-AccountSwitcher.js", SHA256: Hash(files["taut-AccountSwitcher.js"])}},
		{System: Slick, ID: "Oneko", Name: "oneko", CategoryLabel: "Fun", Contributors: []string{"Echo"},
			File: File{File: "slick-oneko.js", SHA256: Hash(files["slick-oneko.js"])},
			Main: &File{File: "slick-oneko.main.js", SHA256: Hash(files["slick-oneko.main.js"])}},
	}}
	if mutate != nil {
		mutate(&ix)
	}
	b, err := json.Marshal(ix)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func bundles() map[string][]byte {
	return map[string][]byte{
		"taut-WhoReacted.js":      []byte("(() => { class WhoReacted {}; return WhoReacted })()"),
		"taut-AccountSwitcher.js": []byte("(() => { class AccountSwitcher {}; return AccountSwitcher })()"),
		"slick-oneko.js":          []byte("// slick-plugin {}\n"),
		"slick-oneko.main.js":     []byte("// slick-main {}\n"),
	}
}

func TestIndexDownloadAndIntegrity(t *testing.T) {
	files := bundles()
	files["index.json"] = testIndex(t, files, nil)
	rel := newFakeRelease(t, files)
	dir := t.TempDir()
	l := New(Options{IndexURL: rel.indexURL(), CacheDir: dir})
	if l.Cached() != nil {
		t.Fatal("an index before any fetch")
	}
	ctx := context.Background()
	ix, err := l.Index(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	taut := ix.For(Taut)
	if len(taut) != 2 || taut[0].ID != "AccountSwitcher" || taut[1].ID != "WhoReacted" {
		t.Fatalf("taut entries by category, name: %+v", taut)
	}
	sl := ix.For(Slick)
	if len(sl) != 1 || sl[0].Credit()[0] != "Echo" || taut[1].Credit()[0] != "Jeremy" {
		t.Fatalf("slick entries / credit: %+v", sl)
	}
	code, main, err := l.Download(ctx, sl[0])
	if err != nil || string(code) != string(files["slick-oneko.js"]) || string(main) != string(files["slick-oneko.main.js"]) {
		t.Fatalf("download: %v %q %q", err, code, main)
	}
	if rel.cookies.Load() != 0 {
		t.Error("the library sent cookies or credentials")
	}

	// a fresh index is reused without a request; the cache survives a restart
	before := rel.hits.Load()
	if _, err := l.Index(ctx, false); err != nil || rel.hits.Load() != before {
		t.Errorf("fresh index re-fetched (%v)", err)
	}
	l2 := New(Options{IndexURL: rel.indexURL(), CacheDir: dir})
	if c := l2.Cached(); c == nil || len(c.Plugins) != 3 {
		t.Errorf("cached index after restart: %+v", c)
	}

	// a tampered bundle fails its sha256 check
	files["taut-WhoReacted.js"] = []byte("evil")
	if _, _, err := l.Download(ctx, taut[1]); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Errorf("tampered bundle: %v", err)
	}
}

func TestIndexRejectsBadNamesAndSchemes(t *testing.T) {
	files := bundles()
	files["index.json"] = testIndex(t, files, func(ix *Index) { ix.Plugins[0].File.File = "../creds.json" })
	rel := newFakeRelease(t, files)
	if _, err := New(Options{IndexURL: rel.indexURL()}).Index(context.Background(), true); err == nil {
		t.Error("an index naming ../creds.json was accepted")
	}
	if _, err := New(Options{IndexURL: "http://example.com/index.json"}).Index(context.Background(), true); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("plain http to a remote host: %v", err)
	}
	files["index.json"] = testIndex(t, files, func(ix *Index) { ix.Schema = 2 })
	if _, err := New(Options{IndexURL: rel.indexURL()}).Index(context.Background(), true); err == nil {
		t.Error("an unknown schema was accepted")
	}
}

func TestOfflineFallsBackToCache(t *testing.T) {
	files := bundles()
	files["index.json"] = testIndex(t, files, nil)
	rel := newFakeRelease(t, files)
	dir := t.TempDir()
	if _, err := New(Options{IndexURL: rel.indexURL(), CacheDir: dir}).Index(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	rel.srv.Close()
	l := New(Options{IndexURL: rel.indexURL(), CacheDir: dir, MaxAge: time.Nanosecond})
	ix, err := l.Index(context.Background(), false)
	if err != nil || len(ix.Plugins) != 3 {
		t.Errorf("offline: %v %v", ix, err)
	}
}

func TestStateOf(t *testing.T) {
	e := Entry{File: File{SHA256: "AA"}, Main: &File{SHA256: "bb"}}
	for _, c := range []struct {
		code, main string
		want       State
	}{{"", "", NotInstalled}, {"aa", "bb", Installed}, {"aa", "", UpdateAvailable}, {"cc", "bb", UpdateAvailable}} {
		if got := StateOf(e, c.code, c.main); got != c.want {
			t.Errorf("StateOf(%q,%q) = %v, want %v", c.code, c.main, got, c.want)
		}
	}
	if StateOf(Entry{File: File{SHA256: "aa"}}, "aa", "") != Installed {
		t.Error("taut entry without main")
	}
}
