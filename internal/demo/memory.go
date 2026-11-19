package demo

import (
	"net"

	"github.com/some-du6e/clack/internal/memnet"
	"github.com/some-du6e/clack/internal/slack/tlsfp"
)

// InMemoryAddr is where the in-memory fake says it is: its URLs are
// http://127.0.0.1:80/…, so every check for a loopback host passes them.
var InMemoryAddr = &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 80}

// StartInMemory is Start for a process with no sockets, the browser demo
// (cmd/clack-web): the fake serves on an in-memory listener (memnet), and
// every connection the process opens, the API's, the realtime socket's
// and the images', goes to it instead of the network (tlsfp's loopback
// dialer). It locks the network first (LockNetwork), so a dial that isn't
// to a loopback address fails before it's tried, and the in-memory
// listener takes every one that is. There's no way back: one per process.
func StartInMemory(opts Options) (*Session, error) {
	LockNetwork()
	ln := memnet.Listen(InMemoryAddr)
	tlsfp.SetLoopbackDialer(ln.DialContext)
	opts.Listener = ln
	return Start(opts)
}
