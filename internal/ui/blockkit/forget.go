package blockkit

import "github.com/some-du6e/clack/internal/core"

// ForgetCore drops what the package keeps of a finished session's core:
// its app DMs and the conversations known not to be ones (keyed by the
// core App). Each entry kept the session reachable for good after a
// sign-out or a switch; in internal/ui's tests, every finished test's
// (ci-speed-3: notDM was the last root a heap dump showed).
func ForgetCore(c *core.App) {
	dmsMu.Lock()
	defer dmsMu.Unlock()
	for k := range dms {
		if k.c == c {
			delete(dms, k)
		}
	}
	for k := range notDM {
		if k.c == c {
			delete(notDM, k)
		}
	}
}

// ForgetHost drops the message hosts made for h, a finished App: the
// registry keeps up to maxHosts, each holding its Host.
func ForgetHost(h Host) {
	msgsMu.Lock()
	defer msgsMu.Unlock()
	for k, me := range msgs {
		if me.h == h {
			delete(msgs, k)
		}
	}
}
