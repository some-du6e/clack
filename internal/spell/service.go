package spell

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// Engine checks words in one language (or, on macOS, whatever languages
// the system detects). Engines are only used on the service's worker
// goroutine.
type Engine interface {
	Check(word string) bool
	Suggest(word string) []string
	Add(word string)
	Close()
}

// Chromium's limits (components/spellcheck): words over 64 UTF-8 bytes
// count as correct (HunspellEngine kMaxCheckedLen), words over 24 get no
// suggestions (kMaxSuggestLen) and the menu shows at most 5
// (kMaxSuggestions).
const (
	maxCheckedLen  = 64
	maxSuggestLen  = 24
	MaxSuggestions = 5
)

// Opener opens the engines for languages; the default is the platform's
// (engine_*.go). Tests replace it.
type Opener func(langs []string, dictDir string) ([]Engine, error)

// Service checks words off the UI goroutine and caches the answers per
// word. Every method is safe from any goroutine and never blocks on the
// engines: unknown words are queued, and the service calls the notify
// function (App.Invalidate) when answers arrive.
type Service struct {
	mu   sync.Mutex
	cond *sync.Cond

	enabled  bool
	langs    []string
	tok      Tokenizer
	dictDir  string // Hunspell dictionaries
	userDir  string // Custom Dictionary.txt
	open     Opener
	notify   func()
	started  bool
	reload   bool
	ready    bool
	loadErr  error
	engines  []Engine // worker-owned
	custom   []string
	customOK map[string]bool

	cache     map[string]bool
	sugg      map[string][]string
	want      map[string]bool
	order     []string
	wantSugg  map[string]bool
	suggOrder []string
	adds      []string

	retries int

	gen   atomic.Uint64 // bumps whenever answers may change
	calls atomic.Int64  // engine calls (tests)
}

// New returns a stopped service; Start runs it.
func New(open Opener, dictDir, userDir string, notify func()) *Service {
	s := &Service{
		open: open, dictDir: dictDir, userDir: userDir, notify: notify,
		enabled: true, langs: DefaultLanguages(),
		cache: map[string]bool{}, sugg: map[string][]string{},
		want: map[string]bool{}, wantSugg: map[string]bool{}, customOK: map[string]bool{},
	}
	if s.open == nil {
		s.open = platformOpen
	}
	s.tok = NewTokenizer(s.langs)
	s.cond = sync.NewCond(&s.mu)
	return s
}

var (
	defMu  sync.Mutex
	defSvc *Service
)

// Default is the app's service (nil until SetDefault).
func Default() *Service {
	defMu.Lock()
	defer defMu.Unlock()
	return defSvc
}

// SetDefault installs the app's service (nil removes it).
func SetDefault(s *Service) {
	defMu.Lock()
	defSvc = s
	defMu.Unlock()
}

// Start launches the worker and loads the engines.
func (s *Service) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	s.started = true
	s.reload = true
	go s.worker()
}

// Stop ends the worker (tests).
func (s *Service) Stop() {
	s.mu.Lock()
	s.started = false
	s.cond.Broadcast()
	s.mu.Unlock()
}

// Gen changes whenever answers may have changed (languages, a word added,
// the pref): editors recheck then.
func (s *Service) Gen() uint64 { return s.gen.Load() }

// Calls counts engine calls (tests).
func (s *Service) Calls() int64 { return s.calls.Load() }

// Enabled is Slack's webapp_spellcheck.
func (s *Service) Enabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

// SetEnabled sets webapp_spellcheck.
func (s *Service) SetEnabled(on bool) {
	s.mu.Lock()
	ch := s.enabled != on
	s.enabled = on
	s.mu.Unlock()
	if ch {
		s.gen.Add(1)
		s.ping()
	}
}

// Languages are the languages checked.
func (s *Service) Languages() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.langs...)
}

// SetLanguages switches languages (session.setSpellCheckerLanguages):
// unknown codes are dropped, as the desktop app's SET_CURRENT_LANGUAGES
// handler filters by its label table. The cache starts over.
func (s *Service) SetLanguages(langs []string) {
	var ok []string
	seen := map[string]bool{}
	for _, l := range langs {
		if Supported(l) && !seen[l] {
			seen[l] = true
			ok = append(ok, l)
		}
	}
	s.mu.Lock()
	if equal(ok, s.langs) {
		s.mu.Unlock()
		return
	}
	s.langs = ok
	s.tok = NewTokenizer(ok)
	s.reload = true
	s.ready = false
	s.flushLocked()
	s.cond.Broadcast()
	s.mu.Unlock()
	s.gen.Add(1)
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *Service) flushLocked() {
	s.cache = map[string]bool{}
	s.sugg = map[string][]string{}
	s.want = map[string]bool{}
	s.order = nil
	s.wantSugg = map[string]bool{}
	s.suggOrder = nil
}

// Tokenizer is the word iterator for the current languages.
func (s *Service) Tokenizer() Tokenizer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tok
}

// Ready reports whether the engines are loaded.
func (s *Service) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

// Err is the last engine load error (a dictionary that failed to
// download).
func (s *Service) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadErr
}

// Lookup returns a word's cached answer; an unknown word is queued and
// known is false. Words Chromium doesn't check (empty, over 64 bytes)
// are correct.
func (s *Service) Lookup(w Word) (correct, known bool) {
	if w.Text == "" || len(w.Text) > maxCheckedLen {
		return true, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.enabled {
		return true, true
	}
	if s.customOK[w.Text] {
		return true, true
	}
	if c, ok := s.cache[w.Text]; ok {
		if c || len(w.Parts) == 0 {
			return c, true
		}
		// IsValidContraction: correct when every piece is
		all := true
		for _, p := range w.Parts {
			if s.customOK[p] {
				continue
			}
			pc, ok := s.cache[p]
			if !ok {
				s.queueLocked(p)
				all = false
				known = false
				continue
			}
			if !pc {
				return false, true
			}
		}
		if all {
			return true, true
		}
		return false, false
	}
	s.queueLocked(w.Text)
	return false, false
}

func (s *Service) queueLocked(t string) {
	if !s.want[t] {
		s.want[t] = true
		s.order = append(s.order, t)
		s.cond.Broadcast()
	}
}

// Suggestions returns a misspelled word's suggestions (at most 5,
// filtered like the desktop app's menu); ok is false while they're being
// computed.
func (s *Service) Suggestions(word string) (out []string, ok bool) {
	if len(word) > maxSuggestLen {
		return nil, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.sugg[word]; ok {
		return v, true
	}
	if !s.wantSugg[word] {
		s.wantSugg[word] = true
		s.suggOrder = append(s.suggOrder, word)
		s.cond.Broadcast()
	}
	return nil, false
}

// Add is "Add to Dictionary" (session.addWordToSpellCheckerDictionary):
// the word is correct from now on, in the custom dictionary file and, on
// macOS and Windows, the system's dictionary.
func (s *Service) Add(word string) {
	if !validCustomWord(word) {
		return
	}
	s.mu.Lock()
	if !s.customOK[word] {
		s.customOK[word] = true
		s.custom = append(s.custom, word)
		s.adds = append(s.adds, word)
		s.cache[word] = true
		s.cond.Broadcast()
	}
	s.mu.Unlock()
	s.gen.Add(1)
	s.ping()
}

func (s *Service) ping() {
	if s.notify != nil {
		s.notify()
	}
}

// worker owns the engines.
func (s *Service) worker() {
	runtime.LockOSThread() // COM on Windows wants one thread
	defer runtime.UnlockOSThread()
	platformThreadInit()
	defer func() {
		for _, e := range s.engines {
			e.Close()
		}
	}()
	custom := loadCustom(s.userDir)
	s.mu.Lock()
	for _, w := range custom {
		if !s.customOK[w] {
			s.customOK[w] = true
			s.custom = append(s.custom, w)
		}
	}
	s.mu.Unlock()
	for {
		s.mu.Lock()
		for s.started && !s.reload && len(s.order) == 0 && len(s.suggOrder) == 0 && len(s.adds) == 0 {
			s.cond.Wait()
		}
		if !s.started {
			s.mu.Unlock()
			return
		}
		if s.reload {
			s.reload = false
			langs := append([]string(nil), s.langs...)
			words := append([]string(nil), s.custom...)
			s.mu.Unlock()
			for _, e := range s.engines {
				e.Close()
			}
			s.engines = nil
			engines, err := s.open(langs, s.dictDir)
			for _, e := range engines {
				for _, w := range words {
					e.Add(w)
				}
			}
			s.mu.Lock()
			if s.reload { // changed again while loading
				s.mu.Unlock()
				for _, e := range engines {
					e.Close()
				}
				continue
			}
			s.engines = engines
			s.loadErr = err
			s.ready = true
			if err != nil && s.retries < 5 {
				// gF in main.bundle.cjs: up to five retries of a dictionary
				// that failed to download
				s.retries++
				d := time.Duration(1<<s.retries) * 5 * time.Second
				time.AfterFunc(d, func() {
					s.mu.Lock()
					s.reload = true
					s.cond.Broadcast()
					s.mu.Unlock()
				})
			} else if err == nil {
				s.retries = 0
			}
			s.flushLocked()
			s.mu.Unlock()
			s.gen.Add(1)
			s.ping()
			continue
		}
		adds := s.adds
		s.adds = nil
		var sw string
		if len(s.suggOrder) > 0 { // a menu waits for these
			sw = s.suggOrder[0]
			s.suggOrder = s.suggOrder[1:]
		}
		n := min(len(s.order), 64)
		batch := append([]string(nil), s.order[:n]...)
		s.order = s.order[n:]
		engines := s.engines
		custom := append([]string(nil), s.custom...)
		s.mu.Unlock()

		for _, w := range adds {
			for _, e := range engines {
				e.Add(w)
			}
		}
		if len(adds) > 0 {
			_ = saveCustom(s.userDir, custom)
		}
		var sugg []string
		if sw != "" {
			sugg = s.suggest(engines, sw)
		}
		res := make([]bool, len(batch))
		for i, w := range batch {
			res[i] = s.check(engines, w)
		}
		s.mu.Lock()
		if sw != "" && s.wantSugg[sw] {
			s.sugg[sw] = sugg
			delete(s.wantSugg, sw)
		}
		for i, w := range batch {
			if s.want[w] {
				s.cache[w] = res[i]
				delete(s.want, w)
			}
		}
		s.mu.Unlock()
		s.ping()
	}
}

// check is SpellCheck::SpellCheckWord across languages: correct when any
// language says so. With no engines (no language, or none loaded) every
// word is correct.
func (s *Service) check(engines []Engine, w string) bool {
	if len(engines) == 0 {
		return true
	}
	for _, e := range engines {
		s.calls.Add(1)
		if e.Check(w) {
			return true
		}
	}
	return false
}

// suggest is FillSuggestions: the languages' lists interleaved, unique,
// at most 5, then the desktop app's filter.
func (s *Service) suggest(engines []Engine, w string) []string {
	if utf8.RuneCountInString(w) == 0 || len(w) > maxSuggestLen {
		return nil
	}
	var lists [][]string
	for _, e := range engines {
		s.calls.Add(1)
		lists = append(lists, e.Suggest(w))
	}
	var out []string
	seen := map[string]bool{}
	longest := 0
	for _, l := range lists {
		longest = max(longest, len(l))
	}
	for i := 0; i < longest*len(lists) && len(out) < MaxSuggestions; i++ {
		lang, idx := i%len(lists), i/len(lists)
		if idx >= len(lists[lang]) {
			continue
		}
		sg := lists[lang][idx]
		if !seen[sg] {
			seen[sg] = true
			out = append(out, sg)
		}
	}
	return FilterSuggestions(out)
}
