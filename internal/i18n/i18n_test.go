package i18n

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// a bundle shaped like Slack's: keys are SHA-1 prefixes of the English
// source, 0 means the source stands.
func testBundle(t *testing.T, loc string, entries map[string]map[string]any) *Catalog {
	t.Helper()
	b := []byte("{")
	firstNS := true
	for ns, m := range entries {
		if !firstNS {
			b = append(b, ',')
		}
		firstNS = false
		b = append(b, '"')
		b = append(b, ns...)
		b = append(b, `":{`...)
		first := true
		for src, v := range m {
			if !first {
				b = append(b, ',')
			}
			first = false
			b = append(b, '"')
			b = append(b, Key(src)[:7]...)
			b = append(b, `":`...)
			switch x := v.(type) {
			case string:
				b = append(b, quote(x)...)
			default:
				b = append(b, '0')
			}
		}
		b = append(b, '}')
	}
	b = append(b, '}')
	c, err := ParseBundle(loc, b)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func quote(s string) string {
	out := []byte{'"'}
	for _, r := range s {
		switch r {
		case '"', '\\':
			out = append(out, '\\', byte(r))
		default:
			out = append(out, string(r)...)
		}
	}
	return string(append(out, '"'))
}

func TestKeyIsSlacksSHA1(t *testing.T) {
	// ids seen in Slack's es-ES bundle (2fa_modal "77dfd21": "Cancelar";
	// prefs: "Language")
	if got := Key("Cancel")[:7]; got != "77dfd21" {
		t.Fatalf("Key(Cancel) = %s, want 77dfd21", got)
	}
	if Key(`you’d`) != Key("you’d") {
		t.Fatal(`decode: ’ should hash like ’`)
	}
}

func TestLookupAndFallback(t *testing.T) {
	c := testBundle(t, "es-ES", map[string]map[string]any{
		"prefs":   {"Language": "Idioma", "Slack": 0},
		"message": {"Today at {time}": "Hoy a las {time}"},
	})
	t.Cleanup(func() { Use(EnUS, nil) })
	Use("es-ES", c)
	prefs := NS("prefs")
	if got := prefs.T("Language"); got != "Idioma" {
		t.Fatalf("Language = %q", got)
	}
	if got := prefs.T("Slack"); got != "Slack" {
		t.Fatalf("a 0 entry should be the source, got %q", got)
	}
	if got := prefs.T("Not in the bundle"); got != "Not in the bundle" {
		t.Fatalf("missing key = %q, want the source", got)
	}
	if got := NS("other").T("Language"); got != "Language" {
		t.Fatalf("another namespace = %q, want the source (lookups are per namespace)", got)
	}
	if got := T("message", "Today at {time}", "time", "15:04"); got != "Hoy a las 15:04" {
		t.Fatalf("placeholder = %q", got)
	}
	if !Translated("prefs", "Language") || Translated("prefs", "Slack") {
		t.Fatal("Translated")
	}
	Use(EnUS, nil)
	if got := prefs.T("Language"); got != "Language" {
		t.Fatalf("en-US = %q", got)
	}
}

func TestMessageFormat(t *testing.T) {
	cases := []struct {
		loc, msg string
		args     Args
		want     string
	}{
		{"en-US", "{name}, {purpose} ({ memberCount, plural, =1 {# member} other {# members}})", Args{"name": "general", "purpose": "talk", "memberCount": 1}, "general, talk (1 member)"},
		{"en-US", "{n, plural, =0 {none} one {# file} other {# files}}", Args{"n": 1234}, "1,234 files"},
		{"es-ES", "{n, plural, one {# persona} other {# personas}}", Args{"n": 1234}, "1234 personas"},
		{"es-ES", "{n, plural, one {# persona} other {# personas}}", Args{"n": 12345}, "12.345 personas"},
		{"es-ES", "{n, plural, one {# persona} many {# de personas} other {# personas}}", Args{"n": 1000000}, "1.000.000 de personas"},
		{"fr-FR", "{n, plural, one {# fichier} other {# fichiers}}", Args{"n": 0}, "0 fichier"},
		{"fr-FR", "{n, number}", Args{"n": 12345.5}, "12 345,5"},
		{"de-DE", "{n, plural, one {# Datei} other {# Dateien}}", Args{"n": 2}, "2 Dateien"},
		{"ja-JP", "{n, plural, =1 {# 人} other {# 人のメンバー}}", Args{"n": 1}, "1 人"},
		{"ja-JP", "{n, plural, one {one} other {# 件}}", Args{"n": 1}, "1 件"}, // ja has only other
		{"en-US", "{n, plural, offset:1 =0 {nobody} =1 {{who}} other {{who} and # others}}", Args{"n": 3, "who": "Ann"}, "Ann and 2 others"},
		{"en-US", "{isConnecting, select, true {Connecting...} other {Connect}}", Args{"isConnecting": true}, "Connecting..."},
		{"en-US", "{years, selectordinal, one {#st} two {#nd} few {#rd} other {#th}} anniversary", Args{"years": 22}, "22nd anniversary"},
		{"en-US", "It''s '{literal}' and #", nil, "It's {literal} and #"},
		{"en-US", "don't", nil, "don't"},
		{"en-US", "<user>{a}</user> joined", Args{"a": "Ann"}, "<user>Ann</user> joined"},
		{"en-US", "{a} {missing}", Args{"a": 1}, "1 undefined"},
		{"en-US", "broken {", nil, "broken {"},
	}
	for _, c := range cases {
		if got := Parse(c.msg).Format(c.loc, c.args); got != c.want {
			t.Errorf("%s %q = %q, want %q", c.loc, c.msg, got, c.want)
		}
	}
}

func TestPlurals(t *testing.T) {
	cases := []struct {
		loc  string
		n    float64
		want string
	}{
		{"en-US", 1, "one"}, {"en-US", 0, "other"}, {"en-US", 1.5, "other"},
		{"de-DE", 1, "one"}, {"fr-FR", 0, "one"}, {"fr-FR", 1.5, "one"}, {"fr-FR", 2, "other"},
		{"fr-FR", 2000000, "many"}, {"pt-BR", 0, "one"}, {"es-ES", 1, "one"}, {"es-LA", 0, "other"},
		{"it-IT", 1000000, "many"}, {"ja-JP", 1, "other"}, {"ko-KR", 1, "other"}, {"zh-CN", 1, "other"},
	}
	for _, c := range cases {
		if got := Cardinal(c.loc, c.n); got != c.want {
			t.Errorf("Cardinal(%s, %v) = %s, want %s", c.loc, c.n, got, c.want)
		}
	}
	if Ordinal("en-US", 13) != "other" || Ordinal("en-US", 23) != "few" || Ordinal("it-IT", 8) != "many" || Ordinal("fr-FR", 1) != "one" {
		t.Error("ordinals")
	}
}

func TestNumbers(t *testing.T) {
	cases := map[string]string{"en-US": "1,234,567.891", "de-DE": "1.234.567,891", "es-ES": "1.234.567,891", "fr-FR": "1 234 567,891", "ja-JP": "1,234,567.891"}
	for loc, want := range cases {
		if got := FormatNumber(loc, 1234567.8914); got != want {
			t.Errorf("%s = %q, want %q", loc, got, want)
		}
	}
	if FormatNumber("es-ES", 1234) != "1234" || FormatNumber("pt-BR", 1234) != "1.234" || FormatNumber("en-US", -0.0001) != "0" {
		t.Error("grouping / rounding")
	}
}

func TestDates(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	mon := time.Date(2026, 11, 2, 15, 4, 0, 0, time.UTC)
	old := time.Date(2025, 12, 31, 9, 0, 0, 0, time.UTC)
	cases := []struct{ got, want string }{
		{DayDivider("en-US", mon, now, false), "Monday, November 2nd"},
		{DayDivider("en-US", now, now, false), "Today"},
		{DayDivider("en-US", now.AddDate(0, 0, -1), now, false), "Yesterday"},
		{DayDivider("en-US", old, now, false), "December 31st, 2025"},
		{DayDivider("es-ES", mon, now, false), "Lunes, 2 de noviembre"},
		{DayDivider("es-ES", now.AddDate(0, 0, -1), now, false), "Ayer"},
		{DayDivider("ja-JP", mon, now, false), "11月2日 (月)"},
		{DayDivider("ja-JP", now, now, false), "今日"},
		{DayDivider("de-DE", mon, now, false), "Montag, 2. November"},
		{DayDivider("fr-FR", mon, now, false), "Lundi 2 novembre"},
		{DayDivider("zh-CN", mon, now, false), "11 月 2 日星期一"},
		{DayDivider("ko-KR", mon, now, false), "11월 2일 월요일"},
		{DayDivider("pt-BR", time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC), now, false), "Domingo, 1º de novembro"},
		{Time("en-US", mon, false, false), "3:04 PM"},
		{Time("en-US", mon, true, false), "15:04"},
		{Time("de-DE", mon, false, false), "15:04 Uhr"},
		{Time("fr-FR", mon, true, false), "15 h 04"},
		{Time("fr-FR", time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), true, false), "9 h"},
		{Time("pt-BR", mon, true, false), "15h04"},
		{Time("ja-JP", mon, false, false), "15:04"},
		{Time("ko-KR", mon, false, false), "오후 3:04"},
		{Time("zh-CN", mon, false, false), "下午 3:04"},
		{Time("es-LA", time.Date(2026, 1, 1, 9, 5, 0, 0, time.UTC), true, false), "9:05"},
		{ShortTime("en-US", mon, false), "3:04"},
		{TimeAgo("en-US", now.Add(-10*time.Minute), now, false, false), "10 minutes ago"},
		{TimeAgo("es-ES", now.Add(-3*time.Hour), now, false, false), "hace 3 horas"},
		{TimeAgo("ja-JP", now.Add(-3*24*time.Hour), now, false, false), "3日前"},
		{TimeAgo("en-US", now.Add(3*time.Hour), now, true, false), "3h"},
		{LongDatePretty("en-US", mon, now, false), "Monday, November 2nd"},
		{CalendarDate("en-US", mon, CalendarOpts{ShortenMonth: true, ShortenDay: true, ExcludeYear: true}), "Nov 2"},
		{CalendarDate("es-ES", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), CalendarOpts{ExcludeYear: true}), "uno de marzo"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("%d: %q, want %q", i, c.got, c.want)
		}
	}
}

func TestOptionsSortedByLabel(t *testing.T) {
	o := Options()
	if len(o) != 12 || o[0].Code != "de-DE" || o[1].Label != "English (UK)" || o[len(o)-1].Code != "ko-KR" {
		t.Fatalf("%+v", o)
	}
	if Normalize("es-419") != "es-LA" || Normalize("xx") != EnUS {
		t.Fatal("Normalize")
	}
}

func TestGroupCJK(t *testing.T) {
	got := groupCJK("[[[社内]]][[[表示]]]")
	if got != "​社⁠内​​表⁠示​" {
		t.Fatalf("%q", got)
	}
	if StripTags("<link>3 agents</link> are here, 1 < 2") != "3 agents are here, 1 < 2" {
		t.Fatal("StripTags")
	}
}

func TestLoaderCachesAndSetLocale(t *testing.T) {
	dir := t.TempDir()
	fetches := 0
	body := []byte(`{"prefs":{"` + Key("Language")[:7] + `":"Idioma"}}`)
	old := DefaultLoader
	DefaultLoader = &Loader{CacheDir: dir, Fetch: func(_ context.Context, url string) ([]byte, error) {
		fetches++
		if url != BundleURL("es-ES") {
			t.Errorf("fetched %s", url)
		}
		return body, nil
	}}
	t.Cleanup(func() { DefaultLoader = old; Use(EnUS, nil) })
	changed := make(chan struct{}, 4)
	// the listener outlives the test (OnChange has no undo): later tests'
	// Use calls must not block on a full channel
	OnChange(func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	done := make(chan error, 1)
	SetLocale("es-ES", func(err error) { done <- err })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-changed
	if Locale() != "es-ES" || NS("prefs").T("Language") != "Idioma" {
		t.Fatalf("locale %s: %q", Locale(), NS("prefs").T("Language"))
	}
	if _, err := os.Stat(filepath.Join(dir, bundleFiles["es-ES"])); err != nil {
		t.Fatal("bundle not cached:", err)
	}
	// from the cache, offline
	l := &Loader{CacheDir: dir, Offline: true}
	if c, err := l.Load(context.Background(), "es-ES"); err != nil || c.Len() != 1 {
		t.Fatal(c, err)
	}
	if fetches != 1 {
		t.Fatalf("%d fetches", fetches)
	}
}

// TestSlackBundles loads Slack's real es-ES and ja-JP bundles from the
// CDN (CLACK_I18N_NET=1; CI caches them) and checks strings Clack wires.
func TestSlackBundles(t *testing.T) {
	if os.Getenv("CLACK_I18N_NET") == "" {
		t.Skip("CLACK_I18N_NET not set")
	}
	l := &Loader{CacheDir: os.Getenv("CLACK_I18N_CACHE")}
	want := map[string][][3]string{
		"es-ES": {{"prefs", "Language", "Idioma"}, {"message", "Today at {time}", "Hoy a las {time}"}},
		"ja-JP": {{"prefs", "Language", "表示言語"}},
	}
	for loc, rows := range want {
		c, err := l.Load(context.Background(), loc)
		if err != nil {
			t.Fatal(loc, err)
		}
		t.Logf("%s: %d namespaces, %d strings", loc, c.Namespaces(), c.Len())
		for _, r := range rows {
			if got, _ := c.Lookup(r[0], r[1]); got != r[2] {
				t.Errorf("%s %s %q = %q, want %q", loc, r[0], r[1], got, r[2])
			}
		}
	}
}
