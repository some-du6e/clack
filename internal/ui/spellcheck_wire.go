package ui

import (
	"sync/atomic"

	"github.com/some-du6e/clack/internal/spell"
	"github.com/some-du6e/clack/internal/ui/composer"
)

// Spellcheck (internal/spell, composer/spellcheck.go): the service checks
// words off the UI goroutine and asks for a frame when answers arrive;
// webapp_spellcheck turns it on and off; the languages (Windows, Linux)
// are this device's, in ui.json.

func (a *App) startSpell() {
	composer.MenuHost = a
	if !a.opts.Spellcheck {
		return
	}
	s := spell.New(nil, spell.DictionaryDir(), a.opts.ConfigDir, a.Invalidate)
	if langs := a.prefs.SpellLanguages; len(langs) > 0 && !spell.SystemLanguages {
		s.SetLanguages(langs)
	}
	spell.SetDefault(s)
	s.Start()
}

// spellcheckOn is webapp_spellcheck (Slack's default: on), unless a Slick
// main half turned the session's spellchecker off (Electron's
// session.setSpellCheckerEnabled(false): Snappy's "Disable composer
// spellcheck").
func (a *App) spellcheckOn() bool {
	if spellSessionOff.Load() {
		return false
	}
	if c := a.Core(); c != nil {
		return c.PrefBool("webapp_spellcheck", true)
	}
	return true
}

// spellSessionOff is the session's spellchecker turned off by a plugin.
var spellSessionOff atomic.Bool

// setSessionSpellChecker is slick.Options.SpellChecker (any goroutine).
func (a *App) setSessionSpellChecker(on bool) {
	if spellSessionOff.Swap(!on) == !on {
		return
	}
	a.Post(func() {
		a.applySpellPref()
		a.Invalidate()
	})
}

// applySpellPref follows the pref (applyAccountPrefs calls it).
func (a *App) applySpellPref() {
	if s := spell.Default(); s != nil {
		s.SetEnabled(a.spellcheckOn())
	}
}

// setSpellLanguages is the multi-select's onSelectedItemsChange
// (setCurrentLanguages): the session's languages, kept in ui.json.
func (a *App) setSpellLanguages(langs []string) {
	a.prefs.SpellLanguages = append([]string(nil), langs...)
	if a.prefs.SpellLanguages == nil {
		a.prefs.SpellLanguages = []string{}
	}
	a.savePrefs()
	if s := spell.Default(); s != nil {
		s.SetLanguages(langs)
	}
	a.Invalidate()
}

// spellLanguages are the current languages.
func (a *App) spellLanguages() []string {
	if s := spell.Default(); s != nil {
		return s.Languages()
	}
	if a.prefs.SpellLanguages != nil {
		return a.prefs.SpellLanguages
	}
	return spell.DefaultLanguages()
}
