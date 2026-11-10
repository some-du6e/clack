package spell

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func words(t Tokenizer, s string) []string {
	var out []string
	for _, w := range t.Words([]rune(s), nil) {
		out = append(out, w.Text)
	}
	return out
}

// The word iterator follows Chromium's rules: letters and digits, joined
// through one contraction character between letters.
func TestWords(t *testing.T) {
	tok := NewTokenizer([]string{"en-US"})
	cases := map[string][]string{
		"Hello wrold, teh end.":    {"Hello", "wrold", "teh", "end"},
		"don't won’t":              {"don't", "won’t"},
		"e.g. example.com foo:bar": {"e.g", "example.com", "foo:bar"},
		":smile: 'quoted' (paren)": {"smile", "quoted", "paren"},
		"21st 3.14 abc123":         {"21st", "3.14", "abc123"},
		"naïve café":               {"naïve", "café"},
		"привет hello":             {"hello"}, // Cyrillic isn't English's script
		"under_score dash-ed":      {"under", "score", "dash", "ed"},
		"ﬁne":                      {"fine"}, // NFKC
		"tail' 'head ..dots.. a.":  {"tail", "head", "dots", "a"},
	}
	for in, want := range cases {
		if got := words(tok, in); !slices.Equal(got, want) {
			t.Errorf("Words(%q) = %q, want %q", in, got, want)
		}
	}
	ru := NewTokenizer([]string{"ru"})
	if got := words(ru, "привет hello"); !slices.Equal(got, []string{"привет"}) {
		t.Errorf("Russian words = %q", got)
	}
	// offsets are runes into the input
	ws := tok.Words([]rune("ab café x"), nil)
	if len(ws) != 3 || ws[1].Start != 3 || ws[1].End != 7 {
		t.Errorf("offsets %+v", ws)
	}
	// skipped positions break words like spaces
	ws = tok.Words([]rune("helloworld"), func(i int) bool { return i >= 3 && i < 5 })
	if len(ws) != 2 || ws[0].Text != "hel" || ws[1].Text != "world" {
		t.Errorf("skip: %+v", ws)
	}
	if w := tok.Words([]rune("hello.world"), nil); len(w) != 1 || !slices.Equal(w[0].Parts, []string{"hello", "world"}) {
		t.Errorf("parts %+v", w)
	}
}

// fakeEngine knows a few words and counts calls.
type fakeEngine struct {
	mu    sync.Mutex
	good  map[string]bool
	sugg  map[string][]string
	added []string
	delay time.Duration
}

func (f *fakeEngine) Check(w string) bool {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.good[w]
}
func (f *fakeEngine) Suggest(w string) []string { return f.sugg[w] }
func (f *fakeEngine) Add(w string) {
	f.mu.Lock()
	f.good[w] = true
	f.added = append(f.added, w)
	f.mu.Unlock()
}
func (f *fakeEngine) Close() {}

func newFake() *fakeEngine {
	return &fakeEngine{
		good: map[string]bool{"hello": true, "world": true, "the": true, "example": true, "com": true},
		sugg: map[string][]string{"teh": {"the", "tech", "eh", "meh", "ten", "tea", "tee"}, "wrold": {"world", "would"}},
	}
}

func waitKnown(t *testing.T, s *Service, w Word) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c, ok := s.Lookup(w); ok {
			return c
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("%q never got an answer", w.Text)
	return false
}

func TestServiceCachesAndNotifies(t *testing.T) {
	fe := newFake()
	var pings int
	var mu sync.Mutex
	s := New(func([]string, string) ([]Engine, error) { return []Engine{fe}, nil }, "", t.TempDir(), func() { mu.Lock(); pings++; mu.Unlock() })
	s.SetLanguages([]string{"en-US"})
	s.Start()
	defer s.Stop()
	if c, ok := s.Lookup(Word{Text: "teh"}); ok || c {
		t.Fatalf("first lookup answered at once: %v %v", c, ok)
	}
	if waitKnown(t, s, Word{Text: "teh"}) {
		t.Error("teh is correct")
	}
	if !waitKnown(t, s, Word{Text: "hello"}) {
		t.Error("hello is misspelled")
	}
	calls := s.Calls()
	for i := 0; i < 100; i++ {
		s.Lookup(Word{Text: "teh"})
	}
	if s.Calls() != calls {
		t.Error("cached words went back to the engine")
	}
	// a contraction is correct when every piece is
	if !waitKnown(t, s, Word{Text: "example.com", Parts: []string{"example", "com"}}) {
		t.Error("example.com is misspelled")
	}
	if waitKnown(t, s, Word{Text: "example.cmo", Parts: []string{"example", "cmo"}}) {
		t.Error("example.cmo is correct")
	}
	// over 64 bytes: not checked
	if c, ok := s.Lookup(Word{Text: strings.Repeat("x", 65)}); !c || !ok {
		t.Error("a long word was checked")
	}
	// suggestions: five at most, in order
	var sg []string
	for i := 0; i < 1000; i++ {
		var ok bool
		if sg, ok = s.Suggestions("teh"); ok {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !slices.Equal(sg, []string{"the", "tech", "eh", "meh", "ten"}) {
		t.Errorf("suggestions %q", sg)
	}
	mu.Lock()
	defer mu.Unlock()
	if pings == 0 {
		t.Error("the service never asked for a frame")
	}
}

// Off, every word is correct (webapp_spellcheck false).
func TestServiceDisabled(t *testing.T) {
	s := New(func([]string, string) ([]Engine, error) { return []Engine{newFake()}, nil }, "", "", nil)
	s.SetEnabled(false)
	if c, ok := s.Lookup(Word{Text: "teh"}); !c || !ok {
		t.Error("disabled service flagged a word")
	}
}

// Two languages: correct if either knows the word; suggestions alternate.
func TestServiceTwoLanguages(t *testing.T) {
	en := newFake()
	fr := &fakeEngine{good: map[string]bool{"bonjour": true}, sugg: map[string][]string{"teh": {"thé", "the"}}}
	s := New(func(l []string, _ string) ([]Engine, error) { return []Engine{en, fr}, nil }, "", "", nil)
	s.SetLanguages([]string{"en-US", "fr"})
	s.Start()
	defer s.Stop()
	if !waitKnown(t, s, Word{Text: "bonjour"}) || !waitKnown(t, s, Word{Text: "hello"}) {
		t.Error("a word one language knows was flagged")
	}
	var sg []string
	for i := 0; i < 1000; i++ {
		var ok bool
		if sg, ok = s.Suggestions("teh"); ok {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !slices.Equal(sg, []string{"the", "thé", "tech", "eh", "meh"}) {
		t.Errorf("interleaved suggestions %q", sg)
	}
}

func TestAddToDictionaryPersists(t *testing.T) {
	dir := t.TempDir()
	fe := newFake()
	s := New(func([]string, string) ([]Engine, error) { return []Engine{fe}, nil }, "", dir, nil)
	s.Start()
	if waitKnown(t, s, Word{Text: "Clack"}) {
		t.Fatal("Clack is known")
	}
	g := s.Gen()
	s.Add("Clack")
	if c, ok := s.Lookup(Word{Text: "Clack"}); !c || !ok {
		t.Error("an added word is still misspelled")
	}
	if s.Gen() == g {
		t.Error("Add didn't bump the generation (editors recheck on it)")
	}
	deadline := time.Now().Add(5 * time.Second)
	var b []byte
	for time.Now().Before(deadline) {
		b, _ = os.ReadFile(filepath.Join(dir, "Custom Dictionary.txt"))
		if len(b) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.Stop()
	if !strings.HasPrefix(string(b), "Clack\nchecksum_v1 = ") {
		t.Fatalf("custom dictionary %q", b)
	}
	if got := loadCustom(dir); !slices.Equal(got, []string{"Clack"}) {
		t.Errorf("reloaded %q", got)
	}
	// a tampered file is ignored (its checksum fails)
	os.WriteFile(filepath.Join(dir, "Custom Dictionary.txt"), []byte("Clack\nEvil\nchecksum_v1 = 00"), 0o600)
	os.Remove(filepath.Join(dir, "Custom Dictionary.txt.backup"))
	if got := loadCustom(dir); got != nil {
		t.Errorf("a bad checksum loaded %q", got)
	}
	// a new service starts with the saved words
	saveCustom(dir, []string{"Clack"})
	s2 := New(func([]string, string) ([]Engine, error) { return []Engine{newFake()}, nil }, "", dir, nil)
	s2.Start()
	defer s2.Stop()
	if !waitKnown(t, s2, Word{Text: "Clack"}) {
		t.Error("a saved word was flagged after a restart")
	}
}

func TestFilterSuggestions(t *testing.T) {
	bad := ""
	for w := range func() map[string]bool { isBlocked(""); return blocked }() {
		if w != "" {
			bad = w
			break
		}
	}
	got := FilterSuggestions([]string{"fine", bad, "a " + bad, "x-" + bad, "okay"})
	if !slices.Equal(got, []string{"fine", "okay"}) {
		t.Errorf("filter %q", got)
	}
	if len(blocked) != 71 {
		t.Errorf("%d blocked words, Slack has 71", len(blocked))
	}
}

func TestBDicFileNames(t *testing.T) {
	for in, want := range map[string]string{"en-US": "en-US-10-1.bdic", "en": "en-US-10-1.bdic", "de": "de-DE-3-0.bdic",
		"es-MX": "es-ES-3-0.bdic", "fa": "fa-IR-9-0.bdic", "uk": "uk-UA-5-0.bdic", "pt": "pt-BR-3-0.bdic", "hy": "hy-1-0.bdic"} {
		if got := BDicFile(in); got != want {
			t.Errorf("BDicFile(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"de_DE.UTF-8": "de-DE", "fr_CA": "fr", "pt_BR.UTF-8": "pt-BR", "C": "en-US", "": "en-US", "en_IN": "en", "xx": "en-US"} {
		if got := correspond(in); got != want {
			t.Errorf("correspond(%q) = %q, want %q", in, got, want)
		}
	}
	if a := Available(); len(a) != len(Labels) || a[0][1] != "Afrikaans" {
		t.Errorf("Available %v", a[:2])
	}
}

// TestPlatformBackend runs the real engine of this OS: Hunspell with
// Chromium's en-US dictionary on Linux (CLACK_TEST_BDIC_DIR, fetched by
// CI from Slack's dictionary CDN), NSSpellChecker on macOS, the Windows
// spell checker on Windows (Hunspell when Windows has no en-US checker).
func TestPlatformBackend(t *testing.T) {
	dir := os.Getenv("CLACK_TEST_BDIC_DIR")
	if runtime.GOOS == "linux" && dir == "" {
		t.Skip("CLACK_TEST_BDIC_DIR not set")
	}
	if dir == "" {
		dir = t.TempDir()
	}
	s := New(nil, dir, t.TempDir(), nil)
	s.SetLanguages([]string{"en-US"})
	s.Start()
	defer s.Stop()
	deadline := time.Now().Add(2 * time.Minute)
	for !s.Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := s.Err(); err != nil {
		t.Fatalf("engine: %v", err)
	}
	tok := s.Tokenizer()
	for _, tc := range []struct {
		text string
		bad  []string
	}{
		{"Hello world, this is a sentence.", nil},
		{"Teh quick brwon fox", []string{"Teh", "brwon"}},
		{"I definately recieve it", []string{"definately", "recieve"}},
	} {
		var bad []string
		for _, w := range tok.Words([]rune(tc.text), nil) {
			if !waitKnown(t, s, w) {
				bad = append(bad, w.Text)
			}
		}
		if !slices.Equal(bad, tc.bad) {
			t.Errorf("%q: misspelled %q, want %q", tc.text, bad, tc.bad)
		}
	}
	var sg []string
	for i := 0; i < 5000; i++ {
		var ok bool
		if sg, ok = s.Suggestions("recieve"); ok {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Logf("%s: suggestions for recieve: %q", runtime.GOOS, sg)
	if !slices.Contains(sg, "receive") {
		t.Errorf("suggestions for recieve %q lack receive", sg)
	}
	if len(sg) > MaxSuggestions {
		t.Errorf("%d suggestions", len(sg))
	}
}
