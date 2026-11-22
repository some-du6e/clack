package ui

import (
	"context"

	"github.com/some-du6e/clack/internal/core"
)

// The threads-ship tests' calls into what the fix added
// (threads_ship_test.go calls only these, so an evidence run on the code
// before can swap them).

// lastEditable is the message Up in an empty composer edits when the list
// has no row editor of its own (editLast's lookup).
func lastEditable(ctx context.Context, svc *core.App, ch, threadTS, self string) (core.Message, bool) {
	m, found, ok := lastOwnEditable(ctx, svc, ch, threadTS, self)
	return m, found && ok
}
