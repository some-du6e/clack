package ui

import (
	"strings"

	"github.com/some-du6e/clack/internal/pluginbench"
	"github.com/some-du6e/clack/internal/pluginlib"
)

// The library-dupes tests (library_pairs_test.go) reach what the rows,
// suggestions and menus are made of only through this file.

// libFace is the version a library row shows and its line about the
// versions both systems have, as the row draws it.
func libFace(v *libraryPage, g libGroup) (pluginlib.Entry, string) {
	e, pick := v.face(g)
	which, alias := v.versions(g, e, pick)
	return e, strings.TrimSpace(which + " " + alias)
}

// libSuggestAction is a search suggestion's button for g.
func libSuggestAction(v *libraryPage, g libGroup) libAction { return v.suggestAction(g) }

// libSettingsMenu is the menu of each installed version's settings.
func libSettingsMenu(v *libraryPage, installed []pluginlib.Entry) []MenuItem {
	return v.settingsMenu(installed)
}

// benchChecked reports a bench pick that checked what the plugin does.
func benchChecked(p pluginbench.Pick) bool { return p.Checked }
