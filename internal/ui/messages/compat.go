package messages

import (
	"sync"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

// isFailed reports whether an optimistic send failed. core is adding a
// Failed flag next to Pending; until it lands, failures are tracked here
// (markFailed) for sends started through Retry.
func isFailed(m *core.Message) bool {
	failedMu.Lock()
	defer failedMu.Unlock()
	return failedTS[m.Channel+"/"+m.TS]
}

var (
	failedMu sync.Mutex
	failedTS = map[string]bool{}
)

func markFailed(ch, ts string, on bool) {
	failedMu.Lock()
	defer failedMu.Unlock()
	if on {
		failedTS[ch+"/"+ts] = true
	} else {
		delete(failedTS, ch+"/"+ts)
	}
}
