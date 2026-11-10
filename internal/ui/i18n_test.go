package ui

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
)

// The "no English left" checks: each wired surface drawn in en-US and in
// Slack's es-ES and ja-JP bundles. A string that comes out the same in
// both is English left, unless Slack's bundle says the source stands (a 0
// entry, or a translation equal to the source), or it's on the lists
// below. The bundles come from the CDN (CLACK_I18N_NET=1, the i18n
// workflow caches them in CLACK_I18N_CACHE).

var (
	bundleMu sync.Mutex
	bundles  = map[string]*i18n.Catalog{}
)

func slackBundle(t *testing.T, loc string) *i18n.Catalog {
	t.Helper()
	if os.Getenv("CLACK_I18N_NET") == "" {
		t.Skip("CLACK_I18N_NET not set (Slack's bundles come from the CDN)")
	}
	bundleMu.Lock()
	defer bundleMu.Unlock()
	if c := bundles[loc]; c != nil {
		return c
	}
	l := &i18n.Loader{CacheDir: os.Getenv("CLACK_I18N_CACHE")}
	c, err := l.Load(context.Background(), loc)
	if err != nil {
		t.Fatalf("%s bundle: %v", loc, err)
	}
	bundles[loc] = c
	return c
}

// clackOnly are strings with no Slack key: Clack's own UI (the updater,
// plugins, "not available yet" notes) stays English, and the strings
// Clack builds from parts where Slack has one placeholder string (listed
// so the gap stays visible; each is a wiring TODO).
var clackOnly = map[string]bool{
	"Not available in Clack yet": true,
	"Clack":                      true,
}

// clackOnlyRE matches the rest: keyboard shortcuts, hex colours, numbers,
// Clack and plugin names.
var clackOnlyRE = regexp.MustCompile(`^(Mod|Ctrl|Cmd|Alt|Shift|Option)(\+|$)|^#[0-9A-Fa-f]{6}|^[0-9 :.,%+-]*$|Clack|Taut|Slick|plugin`)

// englishLeft compares what a surface drew in en-US with what it drew
// in loc. untranslated: strings drawn unchanged that Slack's bundle
// translates (a key with a different text in some namespace): wiring
// misses. unkeyed: strings drawn unchanged that aren't Slack's at all (no
// namespace has them) and aren't listed in clackOnly or extra: Clack's
// own text, or text Clack words differently from Slack.
func englishLeft(cat *i18n.Catalog, en, loc []string, extra map[string]bool) (untranslated, unkeyed []string) {
	seen := map[string]bool{}
	locSet := map[string]bool{}
	for _, s := range loc {
		locSet[s] = true
	}
	for _, s := range en {
		if seen[s] || !locSet[s] || !wordy(s) || clackOnly[s] || extra[s] || clackOnlyRE.MatchString(s) {
			continue
		}
		seen[s] = true
		switch {
		case len(cat.NamespacesOf(s)) == 0:
			unkeyed = append(unkeyed, s)
		case !sameInBundle(cat, s):
			untranslated = append(untranslated, s)
		}
	}
	sort.Strings(untranslated)
	sort.Strings(unkeyed)
	return
}

var wordRE = regexp.MustCompile(`[A-Za-z]{3,}`)

func wordy(s string) bool { return wordRE.MatchString(s) }

// sameInBundle: some namespace has s with the source standing.
func sameInBundle(cat *i18n.Catalog, s string) bool {
	nss := cat.NamespacesOf(s)
	if len(nss) == 0 {
		return false
	}
	for _, ns := range nss {
		if t, _ := cat.Lookup(ns, s); t == "" || t == s {
			return true
		}
	}
	return false
}

// TestPrefsNoEnglishLeft: every Preferences page in es-ES and ja-JP.
func TestPrefsNoEnglishLeft(t *testing.T) {
	for _, loc := range []string{"es-ES", "ja-JP"} {
		cat := slackBundle(t, loc)
		a, _, _ := fakePrefsApp(t)
		t.Cleanup(func() { i18n.Use(i18n.EnUS, nil) })
		var all, unkeyed []string
		for _, it := range prefsNav {
			draw := func() []string {
				p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
				rows := pageRows(t, a, p, it.id)
				p.close()
				out := []string{pt(it.label)}
				for _, r := range rows {
					if r.Kind == "list" { // the account's people and channels
						continue
					}
					out = append(out, r.Label)
				}
				return out
			}
			i18n.Use(i18n.EnUS, nil)
			en := draw()
			i18n.Use(loc, cat)
			got := draw()
			u, k := englishLeft(cat, en, got, prefsUnkeyed)
			for _, s := range u {
				all = append(all, it.id+": "+s)
			}
			unkeyed = append(unkeyed, k...)
		}
		reportLeft(t, loc, all, unkeyed)
	}
}

func reportLeft(t *testing.T, loc string, untranslated, unkeyed []string) {
	t.Helper()
	if len(untranslated) > 0 {
		t.Errorf("%s: %d strings Slack translates left in English:\n  %s", loc, len(untranslated), strings.Join(untranslated, "\n  "))
	}
	if len(unkeyed) > 0 {
		sort.Strings(unkeyed)
		unkeyed = slices.Compact(unkeyed)
		var b strings.Builder
		for _, s := range unkeyed {
			fmt.Fprintf(&b, "\t%q: true,\n", s)
		}
		t.Errorf("%s: %d strings with no Slack key, not listed:\n%s", loc, len(unkeyed), b.String())
	}
}

// TestLocalePref: the Language row lists Slack's 12 locales by label;
// picking one loads its bundle off the UI goroutine, switches the strings
// live, writes users.prefs locale (setUserPrefByApi) and keeps it in
// ui.json for the next start.
func TestLocalePref(t *testing.T) {
	a, _, fs := fakePrefsApp(t)
	t.Cleanup(func() { i18n.Use(i18n.EnUS, nil) })
	old := i18n.DefaultLoader
	body := []byte(`{"prefs":{"` + i18n.Key("Language")[:7] + `":"Idioma"}}`)
	i18n.DefaultLoader = &i18n.Loader{CacheDir: t.TempDir(), Fetch: func(context.Context, string) ([]byte, error) { return body, nil }}
	t.Cleanup(func() { i18n.DefaultLoader = old })

	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	rows := pageRows(t, a, p, "language_region")
	found := false
	for _, r := range rows {
		if r.Key == "sel.locale" {
			found = true
			if r.Label != "English (US)" || !r.Enabled {
				t.Fatalf("Language select = %+v", r)
			}
		}
	}
	if !found {
		t.Fatalf("no Language select in %s", dumpRows(rows))
	}
	opts := i18n.Options()
	if len(opts) != 12 || opts[0].Label != "Deutsch (Deutschland)" {
		t.Fatalf("options %+v", opts)
	}

	p.setLocale("es-ES")
	a.dispatchWait()
	waitFor(t, a, "the es-ES bundle", func() bool { return i18n.Locale() == "es-ES" })
	if got := pt("Language"); got != "Idioma" {
		t.Fatalf("Language = %q after the switch", got)
	}
	waitFor(t, a, "users.prefs.set locale", func() bool { v, _ := fs.Pref("locale"); return v == "es-ES" })
	a.flushPrefs()
	b, err := os.ReadFile(a.prefsPath())
	if err != nil || !strings.Contains(string(b), `"locale": "es-ES"`) && !strings.Contains(string(b), `"locale":"es-ES"`) {
		t.Fatalf("ui.json = %s (%v)", b, err)
	}
}
