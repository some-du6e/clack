package ui

import "github.com/some-du6e/clack/internal/slick"

// svPanelOf reports whether a plugin set an api.settings.panel (in a file
// of its own: a branch proving the tests fail on the code before it stands
// it in).
func svPanelOf(p slick.PluginInfo) bool { return p.Panel }
