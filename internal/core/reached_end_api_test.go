package core

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// The reached_end tests' one call into an API the fix added (the red
// commit swaps this file for a stub: red-evidence-branch).

// pingTimers shortens the socket's liveness check (Slack's 10 s ping, 90 s
// without a pong) for the test.
func pingTimers(t *testing.T, every, timeout time.Duration) {
	t.Cleanup(slack.SetPingTimers(every, timeout))
}
