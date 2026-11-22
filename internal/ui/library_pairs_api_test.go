package ui

import (
	"strings"

	"github.com/some-du6e/clack/internal/pluginlib"
)

// libFace is the version a library row shows and its line about the
// versions both systems have, as the row draws it (library_pairs_test.go
// reads them through this file only).
func libFace(v *libraryPage, g libGroup) (pluginlib.Entry, string) {
	e, pick := v.face(g)
	which, alias := v.versions(g, e, pick)
	return e, strings.TrimSpace(which + " " + alias)
}
