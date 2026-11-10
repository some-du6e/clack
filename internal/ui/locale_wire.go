package ui

import (
	"image"
	"strings"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// Language (users.prefs locale) and Slack's string catalog
// (internal/i18n, docs/I18N.md).
//
// The locale is the account's locale pref. ui.json keeps the last one, as
// Slack keeps localConfig user_locale, so the next start loads that bundle
// from the cache while users.prefs is still on its way. Changing it applies
// live, the way the desktop app (Sonic) does it: setLocale loads the
// bundle and the locale callbacks re-render, then setUserPrefByApi writes
// the pref. Nothing waits for a bundle on the UI goroutine. Until a
// bundle is in, the strings stay in the previous locale.

// prefsChain holds the Preferences pages' namespaces: "prefs", then the
// Slack components the pages draw strings from (Audio & video is
// huddles_audio_video_prefs, the sidebar options channel_sidebar, the
// sounds sounds, …).
var prefsChain = i18n.NewChain("prefs", "huddles_audio_video_prefs", "huddles", "channel_sidebar", "sounds",
	"connected-accounts", "connected-workflow-accounts-list", "app-config", "desktop", "availability_prefs",
	"ai-translations", "channel_details", "quip_collab", "listbox_with_contextual_actions",
	"client", "view", "ia4", "emoji")

// pt is a Preferences string in the active locale.
func pt(s string) string {
	if s == "" {
		return s
	}
	return prefsChain.T(s)
}

// ptLink splits a Preferences rt() string with one <a>…</a> link into
// the text before it, the link's text and the text after (Clack draws the
// link on its own line, as before).
func ptLink(src string) (before, link, after string) {
	s := pt(src)
	i := strings.Index(s, "<a>")
	j := strings.Index(s, "</a>")
	if i < 0 || j < i {
		return s, "", ""
	}
	return strings.TrimSpace(s[:i]), s[i+3 : j], strings.TrimSpace(s[j+4:])
}

// ptBetween is the text of a Preferences string between placeholders a
// and b, and after b ("{wave_emoji} Hi, I’m {name}" → " Hi, I’m ", "";
// ja-JP puts "です" after the name).
func ptBetween(src, a, b string) (between, after string) {
	s := prefsChain.T(src, a, "\x00", b, "\x01")
	i := strings.IndexByte(s, 0)
	j := strings.IndexByte(s, 1)
	if i < 0 || j < i {
		return s, ""
	}
	return s[i+1 : j], s[j+1:]
}

// soundsNS is the notification sounds' labels (Slack's sound map in the
// "sounds" namespace: "Knock Brush" is "Pincelada" in es-ES).
var soundsNS = i18n.NS("sounds")

func (a *App) startLocale() {
	i18n.OnChange(a.Invalidate)
	if l := a.prefs.Locale; l != "" && l != i18n.EnUS {
		i18n.SetLocale(l, nil) // from the cache: no frame waits for it
	}
}

// accountLocale is the locale pref ("" before users.prefs answers).
func (a *App) accountLocale() string {
	if c := a.Core(); c != nil {
		return c.PrefString("locale")
	}
	return ""
}

// applyLocalePref follows the locale pref (applyAccountPrefs calls it on
// the UI goroutine).
func (a *App) applyLocalePref() {
	raw := a.accountLocale()
	if raw == "" {
		return
	}
	loc := i18n.Normalize(raw)
	if loc != i18n.Wanted() {
		i18n.SetLocale(loc, nil)
	}
	if a.prefs.Locale != loc {
		a.prefs.Locale = loc
		a.savePrefsSoon()
	}
}

// setLocale is LocalePref's onSelectionChange (setLocaleForTeam).
func (p *prefsModal) setLocale(loc string) {
	a := p.a
	i18n.SetLocale(loc, func(error) { a.Invalidate() })
	a.prefs.Locale = loc
	a.savePrefs()
	p.setPref("locale", loc)
}

// localePref is LocalePref: a large full-width select of Slack's locales
// by label, greyed offline with "Go online to change your language.".
func (p *prefsModal) localePref(f *prefsForm) {
	loc := p.prefStr("locale")
	if !i18n.Valid(loc) {
		loc = i18n.Normalize(loc)
	}
	online := p.a.online()
	opts := i18n.Options()
	labels := make([]string, len(opts))
	sel := -1
	for i, o := range opts {
		labels[i] = o.Label
		if o.Code == loc {
			sel = i
		}
	}
	f.subheading("Language")
	p.selectList(f, "locale", i18n.Label(loc), online, func(anchor image.Rectangle) {
		p.a.ShowOverlay(popups.NewOptionList(p.a, anchor, labels, sel, func(i int) {
			if opts[i].Code != loc {
				p.setLocale(opts[i].Code)
			}
		}))
	})
	if online {
		f.muted("Choose the language you’d like to use with Slack.", false)
	} else {
		f.muted("Go online to change your language.", false)
	}
}

// soundLabel is a sound's label in the active locale.
func soundLabel(s string) string { return soundsNS.T(s) }
