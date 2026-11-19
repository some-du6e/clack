// Package memnet is a net.Listener whose connections are in-memory pipes
// (net.Pipe): a server and its clients in one process, without sockets.
// The browser demo (cmd/clack-web) serves its fake Slack server on one,
// because a web page has no sockets, and every connection Clack opens goes
// to it (tlsfp.SetLoopbackDialer).
package memnet

import (
	"context"
	"net"
	"sync"
)

// Listener hands the server end of each Dial's pipe to Accept.
type Listener struct {
	addr  net.Addr
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

// Listen returns a listener that says it's at addr, so the URLs a server
// builds from Addr look like that address's (a loopback one for the demo).
func Listen(addr *net.TCPAddr) *Listener {
	return &Listener{addr: addr, conns: make(chan net.Conn), done: make(chan struct{})}
}

// Accept waits for the next Dial.
func (l *Listener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

// Close stops Accept and every later Dial. Connections already made stay
// open.
func (l *Listener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

// Addr is the address Listen was given.
func (l *Listener) Addr() net.Addr { return l.addr }

// DialContext connects to the listener, whatever network and addr say.
// It returns once Accept has taken the other end.
func (l *Listener) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.done:
	case <-ctx.Done():
	}
	client.Close()
	server.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, &net.OpError{Op: "dial", Net: network, Addr: l.addr, Err: net.ErrClosed}
}
