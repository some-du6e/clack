package ui

import "github.com/some-du6e/clack/internal/slick"

// svPanelOf is a plugin's api.settings.panel number (in a file of its own:
// a branch proving the tests fail on the code before it stands it in).
func svPanelOf(p slick.PluginInfo) int { return p.Panel }
