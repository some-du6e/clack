package ui

import "github.com/some-du6e/clack/internal/core"

// botGone is store.BotNonexistent: whether nothing returned bot id (the
// red evidence commit, on a tree without it, stubs this file).
func botGone(c *core.App, id string) bool { return c.S.BotNonexistent(id) }
