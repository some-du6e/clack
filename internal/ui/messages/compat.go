package messages

import "github.com/some-du6e/yoloslackgoclient/internal/core"

// isFailed reports whether an optimistic send failed (core sets Failed and
// keeps the local copy until Resend or DiscardPending).
func isFailed(m *core.Message) bool { return m.Failed }
