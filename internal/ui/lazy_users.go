package ui

import (
	"sync"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// The members a message mentions are fetched when a view draws the
// mention, as Slack's MemberSlug reads its member through getMemberById
// on render (core/lazy_users.go): richview's MemberRead reads the store
// for every mention it draws, asking for a missing member, and lays the
// text out again once the name arrives.

var memberReadOwner struct {
	mu sync.Mutex
	c  *core.App
}

// installMemberRead points richview's mentions at c's store.
func installMemberRead(c *core.App) {
	if c == nil {
		return
	}
	memberReadOwner.mu.Lock()
	defer memberReadOwner.mu.Unlock()
	memberReadOwner.c = c
	richview.SetMemberRead(func(id string) (string, bool) {
		u, ok := c.S.User(id) // getMemberById: a miss is asked for, a hit is an access
		if !ok {
			return "", false
		}
		return store.DisplayName(u), true // richtext's userSpan reads the same name
	})
}

// uninstallMemberRead removes the hook if c's backend installed it last.
func uninstallMemberRead(c *core.App) {
	memberReadOwner.mu.Lock()
	defer memberReadOwner.mu.Unlock()
	if memberReadOwner.c == c {
		memberReadOwner.c = nil
		richview.SetMemberRead(nil)
	}
}
