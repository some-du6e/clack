package tlsfp

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

// Dialer opens TLS connections with the embedded ClientHello, directly or
// through a proxy.
type Dialer struct {
	// Dial opens the TCP connection, to the target or to the proxy. nil: a
	// net.Dialer with a 10 s timeout and 30 s keep-alive. A dial guard
	// (package slack's public-address check) goes here, under TLS.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
	// Proxy picks the proxy for a target (http.ProxyFromEnvironment for
	// HTTPS_PROXY/NO_PROXY). nil: always direct. An http(s) proxy gets a
	// CONNECT and the TLS handshake with Slack runs inside the tunnel, so
	// Slack sees this hello, not the proxy's; socks5 works too.
	Proxy func(*http.Request) (*url.URL, error)
	// RootCAs verifies servers (and an https proxy). nil: the system roots.
	RootCAs *x509.CertPool
	// HandshakeTimeout bounds the dial, the CONNECT and the handshake.
	// 0: 10 s.
	HandshakeTimeout time.Duration
}

func (d *Dialer) tcp(ctx context.Context, network, addr string) (net.Conn, error) {
	if d.Dial != nil {
		return d.Dial(ctx, network, addr)
	}
	return (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, addr)
}

// DialTLSContext connects to addr (host:port) and runs the handshake with
// host as SNI and verified name. It has http.Transport.DialTLSContext's
// signature.
func (d *Dialer) DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	to := d.HandshakeTimeout
	if to <= 0 {
		to = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	conn, err := d.conn(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return Handshake(ctx, conn, host, d.RootCAs)
}

// conn is the TCP connection to addr: direct, or a tunnel through the
// proxy d.Proxy picks for https://addr.
func (d *Dialer) conn(ctx context.Context, network, addr string) (net.Conn, error) {
	var pu *url.URL
	if d.Proxy != nil {
		var err error
		pu, err = d.Proxy(&http.Request{Method: http.MethodConnect, URL: &url.URL{Scheme: "https", Host: addr}, Header: http.Header{}})
		if err != nil {
			return nil, fmt.Errorf("tlsfp: proxy: %w", err)
		}
	}
	if pu == nil {
		return d.tcp(ctx, network, addr)
	}
	switch pu.Scheme {
	case "http", "https":
		return d.connect(ctx, network, pu, addr)
	case "socks5", "socks5h":
		pd, err := proxy.FromURL(pu, ctxDialer{d, ctx})
		if err != nil {
			return nil, fmt.Errorf("tlsfp: proxy: %w", err)
		}
		if cd, ok := pd.(proxy.ContextDialer); ok {
			return cd.DialContext(ctx, network, addr)
		}
		return pd.Dial(network, addr)
	}
	return nil, fmt.Errorf("tlsfp: unsupported proxy scheme %q", pu.Scheme)
}

// ctxDialer is proxy.Dialer over d.tcp.
type ctxDialer struct {
	d   *Dialer
	ctx context.Context
}

func (c ctxDialer) Dial(network, addr string) (net.Conn, error) {
	return c.d.tcp(c.ctx, network, addr)
}

func (c ctxDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return c.d.tcp(ctx, network, addr)
}

// connect opens an HTTP CONNECT tunnel to addr through the http(s) proxy pu.
func (d *Dialer) connect(ctx context.Context, network string, pu *url.URL, addr string) (net.Conn, error) {
	paddr := pu.Host
	if pu.Port() == "" {
		port := "80"
		if pu.Scheme == "https" {
			port = "443"
		}
		paddr = net.JoinHostPort(pu.Hostname(), port)
	}
	conn, err := d.tcp(ctx, network, paddr)
	if err != nil {
		return nil, err
	}
	if pu.Scheme == "https" {
		// TLS to the proxy itself: not Slack, so crypto/tls, verified.
		tc := tls.Client(conn, &tls.Config{ServerName: pu.Hostname(), RootCAs: d.RootCAs, MinVersion: tls.VersionTLS12})
		if err := tc.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, fmt.Errorf("tlsfp: proxy: %w", err)
		}
		conn = tc
	}
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { conn.SetDeadline(time.Unix(1, 0)) })
	req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: addr}, Host: addr, Header: http.Header{}}
	if u := pu.User; u != nil {
		pw, _ := u.Password()
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(u.Username()+":"+pw)))
	}
	err = req.Write(conn)
	var resp *http.Response
	br := bufio.NewReader(conn)
	if err == nil {
		resp, err = http.ReadResponse(br, req)
	}
	if !stop() && err == nil {
		err = ctx.Err()
	}
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("tlsfp: proxy CONNECT: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		conn.Close()
		return nil, fmt.Errorf("tlsfp: proxy CONNECT: %s", resp.Status)
	}
	if br.Buffered() > 0 {
		// the server can't speak before our ClientHello
		conn.Close()
		return nil, errors.New("tlsfp: proxy sent data after CONNECT")
	}
	conn.SetDeadline(time.Time{})
	return conn, nil
}
