package uifont

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gioui.org/font"

	"github.com/some-du6e/clack/assets/fonts"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func labels(os []Option) []string {
	var out []string
	for _, o := range os {
		out = append(out, o.Label)
	}
	return out
}

// TestOptionsSlackOrder: getAllFontOptions sorted by label ("ofdg".tw),
// with the system font only where "wj/S".qL names one.
func TestOptionsSlackOrder(t *testing.T) {
	base := []string{"Arial", "Atkinson Hyperlegible Next", "Comic Sans", "Georgia", "Lato (Default)", "Noto Sans", "OpenDyslexic", "Roboto Mono"}
	for goos, want := range map[string][]string{
		"darwin":  append(append([]string(nil), base...), "San Francisco Pro (System)", "Verdana"),
		"windows": append(append([]string(nil), base...), "Segoe UI (System)", "Verdana"),
		"linux":   append(append([]string(nil), base...), "Verdana"),
	} {
		if got := labels(Options(goos)); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %q\nwant %q", goos, got, want)
		}
	}
	o, ok := Find("darwin", SystemFont)
	if !ok || o.FontFamily != SystemStack {
		t.Errorf("system-font on macOS: %+v %v", o, ok)
	}
	if o, ok := Find("linux", SystemFont); ok || o.Value != Lato {
		t.Errorf("system-font on Linux: %+v %v, want Lato (no such option)", o, ok)
	}
	if d := Default(); !d.Default || d.Value != Lato || UsesThemeFont(d) {
		t.Errorf("default %+v", d)
	}
	for _, o := range Options("darwin") {
		if o.Value != Lato && !UsesThemeFont(o) {
			t.Errorf("%s: no use-theme-font", o.Label)
		}
	}
}

func TestWeights(t *testing.T) {
	for _, c := range []struct {
		label       string
		bold, black float32
	}{
		{"Arial", 700, 900}, {"Georgia", 700, 900}, {"OpenDyslexic", 700, 900}, {"Comic Sans", 700, 900}, {"Verdana", 700, 900},
		{"Roboto Mono", 600, 700}, {"Noto Sans", 600, 700}, {"San Francisco Pro (System)", 600, 700}, {"Segoe UI (System)", 600, 700},
		{"Atkinson Hyperlegible Next", 600, 700},
	} {
		if b, k := Weights(Option{Label: c.label}); b != c.bold || k != c.black {
			t.Errorf("%s: %v/%v, want %v/%v", c.label, b, k, c.bold, c.black)
		}
	}
}

// fakeSys is an installed-font index: family → files, files → bytes.
type fakeSys map[string][]string

func (f fakeSys) Faces(family string) []File {
	var out []File
	for _, p := range f[strings.ToLower(family)] {
		out = append(out, File{Path: p})
	}
	return out
}

// lato files stand in for system fonts: Regular 400, Bold 700, Italic,
// Black 900.
var latoFiles = map[string][]byte{
	"/fonts/R.ttf": fonts.Regular, "/fonts/B.ttf": fonts.Bold, "/fonts/I.ttf": fonts.Italic, "/fonts/K.ttf": fonts.Black,
}

func fakeRead(p string) ([]byte, error) {
	if b, ok := latoFiles[p]; ok {
		return b, nil
	}
	return nil, os.ErrNotExist
}

func slotFiles(t *testing.T, u *theme.UIFont) []string {
	t.Helper()
	if u == nil {
		t.Fatal("UI font is the embedded Lato")
	}
	var out []string
	for _, f := range u.Faces {
		for p, b := range latoFiles {
			if len(b) == len(f.Data) && &b[0] == &f.Data[0] {
				out = append(out, filepath.Base(p))
			}
		}
	}
	return out
}

// TestSystemFontFallback: a missing system font falls back like Chrome:
// the next family of the CSS list, Linux's metric-compatible substitutes,
// then the standard font, then the last resort, and at worst Lato.
func TestSystemFontFallback(t *testing.T) {
	ctx := context.Background()
	opt := func(v string) Option { o, _ := Find("linux", v); return o }
	four := []string{"/fonts/R.ttf", "/fonts/B.ttf", "/fonts/I.ttf", "/fonts/K.ttf"}

	l := &Loader{GOOS: "linux", ReadFile: fakeRead, System: fakeSys{"arial": four}}
	f, err := l.Load(ctx, opt(Arial))
	if err != nil || f.Family != "Arial" || f.Fallback {
		t.Fatalf("Arial installed: %+v %v", f, err)
	}
	// slots: Regular, Bold (700), Black (900), Light (→ Regular), then
	// Italic for all four italic slots (the family's only italic face)
	if got, want := slotFiles(t, f.UI), []string{"R.ttf", "B.ttf", "K.ttf", "R.ttf", "I.ttf", "I.ttf", "I.ttf", "I.ttf"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Arial slots %v, want %v", got, want)
	}

	// Arial missing on Linux: Skia accepts Liberation Sans (metric-compatible)
	l = &Loader{GOOS: "linux", ReadFile: fakeRead, System: fakeSys{"liberation sans": four[:2]}}
	if f, _ := l.Load(ctx, opt(Arial)); f.Family != "Liberation Sans" || f.UI == nil {
		t.Errorf("Arial → %+v, want Liberation Sans", f)
	}
	// …but not on macOS, and not for Georgia: the standard font (Times)
	l = &Loader{GOOS: "darwin", ReadFile: fakeRead, System: fakeSys{"liberation sans": four, "times": four[:1]}}
	if f, _ := l.Load(ctx, opt(Arial)); f.Family != "Times" || !f.Fallback {
		t.Errorf("macOS without Arial → %+v, want Times", f)
	}
	l = &Loader{GOOS: "linux", ReadFile: fakeRead, System: fakeSys{"liberation sans": four, "liberation serif": four[:1]}}
	if f, _ := l.Load(ctx, opt(Georgia)); f.Family != "Liberation Serif" || !f.Fallback {
		t.Errorf("Linux without Georgia → %+v, want Liberation Serif (Times New Roman's substitute)", f)
	}
	// Comic Sans: "Comic Sans MS", cursive (Comic Sans MS again on Linux),
	// sans-serif (Arial → Liberation Sans)
	if f, _ := l.Load(ctx, opt(ComicSans)); f.Family != "Liberation Sans" {
		t.Errorf("Linux without Comic Sans → %+v, want Liberation Sans (sans-serif)", f)
	}
	// nothing at all: fontconfig's sans-serif preference, else Lato
	l = &Loader{GOOS: "linux", ReadFile: fakeRead, System: fakeSys{"dejavu sans": four[:1]}}
	if f, _ := l.Load(ctx, opt(Verdana)); f.Family != "DejaVu Sans" {
		t.Errorf("Linux without Verdana → %+v, want DejaVu Sans", f)
	}
	l = &Loader{GOOS: "linux", ReadFile: fakeRead, System: fakeSys{}}
	if f, _ := l.Load(ctx, opt(Verdana)); f.Family != "Lato" || f.UI != nil || !f.Fallback {
		t.Errorf("no fonts at all → %+v, want the embedded Lato", f)
	}
	// Lato is the embedded font, no lookup
	if f, _ := l.Load(ctx, Default()); f.UI != nil || f.Family != "Lato" || f.Fallback {
		t.Errorf("Lato → %+v", f)
	}
}

// TestSystemFontMacOS: San Francisco Pro is -apple-system (SFNS), with
// Slack's 600/700 weights; Windows' stack reaches Segoe UI.
func TestSystemFontWindows(t *testing.T) {
	o, _ := Find("windows", SystemFont)
	l := &Loader{GOOS: "windows", ReadFile: fakeRead, System: fakeSys{"segoe ui": {"/fonts/R.ttf", "/fonts/B.ttf", "/fonts/K.ttf"}}}
	f, err := l.Load(context.Background(), o)
	if err != nil || f.Family != "Segoe UI" {
		t.Fatalf("%+v %v", f, err)
	}
	// bold is 600 → the Bold (700) face; black is 700 → Bold too
	if got := slotFiles(t, f.UI)[:3]; !reflect.DeepEqual(got, []string{"R.ttf", "B.ttf", "B.ttf"}) {
		t.Errorf("Segoe UI regular/bold/black: %v", got)
	}
}

// cdnFiles serves the real CDN fonts' stand-ins: every woff2 is Slack's
// Roboto Mono Cyrillic (variable, wght 100-700) from the woff2 tests.
type cdn struct {
	mu   sync.Mutex
	urls []string
	b    []byte
	fail bool
}

func (c *cdn) fetch(ctx context.Context, url string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.urls = append(c.urls, url)
	if c.fail {
		return nil, errors.New("offline")
	}
	return c.b, nil
}

// TestCDNFontsFetchCacheAndInstance: a web family is fetched from
// Slack's CDN once, kept in the cache dir, read back offline, and its
// variable faces are instanced at Slack's weights.
func TestCDNFontsFetchCacheAndInstance(t *testing.T) {
	b, err := os.ReadFile("../../woff2/testdata/robotomono-cyr.woff2")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	c := &cdn{b: b}
	l := &Loader{GOOS: "linux", CacheDir: dir, Fetch: c.fetch}
	o, _ := Find("linux", RobotoMono)
	f, err := l.Load(context.Background(), o)
	if err != nil || f.UI == nil || f.Family != "Slack-Roboto-Mono" || f.Fallback {
		t.Fatalf("%+v %v", f, err)
	}
	if len(c.urls) != 5 || c.urls[0] != CDN+"RobotoMono-Regular-6ddf6fb.woff2" {
		t.Errorf("fetched %q", c.urls)
	}
	wght := func(s theme.FaceSlot) float32 {
		for _, v := range s.Vars {
			if v.Tag.String() == "wght" {
				return v.Value
			}
		}
		return -1
	}
	for i, want := range []float32{400, 600, 700, 300, 400, 600, 700, 300} {
		if got := wght(f.UI.Faces[i]); got != want {
			t.Errorf("slot %d (%v): wght %v, want %v", i, theme.Slots[i], got, want)
		}
	}
	if f.UI.Faces[4].Style != font.Italic || len(f.UI.Ext) != 16 || f.UI.Ext[0].Family != theme.ExtFamilies[0] || f.UI.Ext[8].Family != theme.ExtFamilies[1] {
		t.Errorf("italic slot / companions: %v, %d ext", f.UI.Faces[4].Slot, len(f.UI.Ext))
	}
	if _, err := theme.NewShaperWith(f.UI); err != nil {
		t.Fatalf("shaper: %v", err)
	}

	// cached: offline loads it without the network
	c.fail = true
	l2 := &Loader{GOOS: "linux", CacheDir: dir, Fetch: c.fetch, Offline: true}
	if f, err := l2.Load(context.Background(), o); err != nil || f.UI == nil || f.Pending {
		t.Fatalf("offline from cache: %+v %v", f, err)
	}
	if n := len(c.urls); n != 5 {
		t.Errorf("offline load fetched (%d requests)", n)
	}
	// not cached, offline: pending, falling back through the list (no
	// list: the standard font → here nothing, so Lato)
	l3 := &Loader{GOOS: "linux", CacheDir: t.TempDir(), Fetch: c.fetch, Offline: true, System: fakeSys{}}
	if f, err := l3.Load(context.Background(), o); err != nil || !f.Pending || f.UI != nil {
		t.Fatalf("offline, uncached: %+v %v", f, err)
	}
	// a failed fetch falls back the same way, and isn't cached
	l4 := &Loader{GOOS: "linux", CacheDir: t.TempDir(), Fetch: c.fetch, System: fakeSys{"arial": {"/fonts/R.ttf"}}, ReadFile: fakeRead}
	n, _ := Find("linux", NotoSans)
	if f, err := l4.Load(context.Background(), n); err != nil || f.Family != "Arial" || !f.Fallback {
		t.Fatalf("CDN down, Noto Sans, sans-serif → %+v %v, want Arial", f, err)
	}
	if ents, _ := os.ReadDir(l4.CacheDir); len(ents) != 0 {
		t.Errorf("cached a failed fetch: %v", ents)
	}
}

// TestMatchCSS is CSS font matching on weights.
func TestMatchCSS(t *testing.T) {
	fs := []*face{{wmin: 300, wmax: 300}, {wmin: 400, wmax: 400}, {wmin: 600, wmax: 600}, {wmin: 800, wmax: 800}}
	for w, want := range map[float32]float32{100: 300, 300: 300, 450: 400, 500: 400, 550: 600, 700: 800, 900: 800, 350: 300} {
		if f, _ := match(fs, w, false); f.wmin != want {
			t.Errorf("weight %v → %v, want %v", w, f.wmin, want)
		}
	}
	v := []*face{{wmin: 100, wmax: 700}}
	if _, at := match(v, 900, false); at != 700 {
		t.Errorf("variable 100-700 at 900 → %v, want 700", at)
	}
}
