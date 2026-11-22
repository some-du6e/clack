package views

import "github.com/some-du6e/clack/internal/core"

// vbBadgeText is the text an Activity badge's tooltip quotes (the red
// evidence run swaps this file for the old code's raw text).
func vbBadgeText(c *core.App, s string) string { return actMrkdwnPlain(c, s) }
