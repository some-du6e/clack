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
// for every mention it draws, asking for a missing member, draws
// BaseMissingMember's bar until it is known (never its id), and lays the
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
	richview.SetMemberRead(func(id string) (string, richview.MemberMiss) {
		u, ok := c.S.User(id) // getMemberById: a miss is asked for, a hit is an access
		switch {
		case ok:
			return store.DisplayName(u), richview.MemberLoaded // richtext's userSpan reads the same name
		case !core.LooksLikeMemberID(id):
			// getMemberById makes any other id a non-existent member at
			// once, which MemberSlug draws as "@id": the parsed text
			return "", richview.MemberLoaded
		case c.MissingMember(id) == core.MemberNonExistent:
			return "", richview.MemberGone // BaseMissingMember "non-existent"
		}
		return "", richview.MemberLoading // the unknown stub until "names"
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
