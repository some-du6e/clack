package tlsfp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	utls "github.com/refraction-networking/utls"

	"github.com/some-du6e/yoloslackgoclient/internal/slack/tlsfp/tlsfptest"
)

// helloFor is the ClientHello record uTLS would send for serverName,
// without sending it.
func helloFor(t *testing.T, serverName string) []byte {
	t.Helper()
	s, err := spec(serverName)
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	uc := utls.UClient(a, &utls.Config{ServerName: serverName}, utls.HelloCustom)
	if err := uc.ApplyPreset(s); err != nil {
		t.Fatal(err)
	}
	if err := uc.BuildHandshakeState(); err != nil {
		t.Fatal(err)
	}
	msg := uc.HandshakeState.Hello.Raw
	rec := []byte{22, 3, 1, byte(len(msg) >> 8), byte(len(msg))}
	return append(rec, msg...)
}

// mask zeroes what changes on every connection: the random, the session
// id and the key share's public key.
func mask(t *testing.T, rec []byte) []byte {
	t.Helper()
	b := bytes.Clone(rec)
	m := b[5:]
	clear(m[6:38]) // random
	sid := int(m[38])
	clear(m[39 : 39+sid])
	o := 39 + sid
	o += 2 + int(binary.BigEndian.Uint16(m[o:])) // cipher suites
	o += 1 + int(m[o])                           // compression
	end := o + 2 + int(binary.BigEndian.Uint16(m[o:]))
	for o += 2; o < end; {
		typ, l := binary.BigEndian.Uint16(m[o:]), int(binary.BigEndian.Uint16(m[o+2:]))
		if typ == 51 { // key_share: list len, then group, len, key
			for k := o + 6; k < o+4+l; {
				kl := int(binary.BigEndian.Uint16(m[k+2:]))
				clear(m[k+4 : k+4+kl])
				k += 4 + kl
			}
		}
		o += 4 + l
	}
	return b
}

// For slack.com, the hello is the recording byte for byte apart from the
// per-connection random, session id and key share.
func TestHelloIsTheRecording(t *testing.T) {
	got := helloFor(t, "slack.com")
	if len(got) != len(record) {
		t.Fatalf("hello is %d bytes, the recording %d", len(got), len(record))
	}
	if g, w := mask(t, got), mask(t, record); !bytes.Equal(g, w) {
		for i := range g {
			if g[i] != w[i] {
				t.Fatalf("hello differs from the recording at byte %d: % x vs % x", i, g[i:min(i+16, len(g))], w[i:min(i+16, len(w))])
			}
		}
	}
	if bytes.Equal(got[11:43], record[11:43]) {
		t.Error("the recorded random was reused")
	}
}

// Other Slack host names change only the SNI, and the padding keeps the
// hello at 512 bytes as OpenSSL does.
func TestHelloForSlackHosts(t *testing.T) {
	want, err := tlsfptest.Parse(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"slack.com", "hackclub.slack.com", "hackclub.enterprise.slack.com", "app.slack.com",
		"edgeapi.slack.com", "wss-primary.slack.com", "files.slack.com", "a.slack-edge.com", "slack-imgs.com", "ca.slack-edge.com"} {
		got, err := tlsfptest.Parse(helloFor(t, h))
		if err != nil {
			t.Fatal(h, err)
		}
		if d := tlsfptest.Diff(got, want, h); d != nil {
			t.Errorf("%s: %s", h, strings.Join(d, "; "))
		}
		if got.Len != want.Len {
			t.Errorf("%s: hello is %d bytes, OpenSSL pads to %d", h, got.Len, want.Len)
		}
	}
	if !SlackHost("hackclub.slack.com") {
		t.Fatal("SlackHost")
	}
}

func TestSpecFields(t *testing.T) {
	w, _ := tlsfptest.Parse(record)
	if strings.Join(w.ALPN, ",") != "http/1.1" || w.SNI != "slack.com" || len(w.CipherSuites) != 31 {
		t.Fatalf("recording: %+v", w)
	}
	if v := w.Versions; len(v) != 2 || v[0] != tls.VersionTLS13 || v[1] != tls.VersionTLS12 {
		t.Fatalf("supported_versions %x", v)
	}
}

func TestSlackHost(t *testing.T) {
	for h, want := range map[string]bool{
		"slack.com": true, "SLACK.COM": true, "slack.com.": true, "hackclub.slack.com": true, "wss-primary.slack.com:443": true,
		"edgeapi.slack.com": true, "a.slack-edge.com": true, "slack-imgs.com": true, "files.slack-files.com": true,
		"evilslack.com": false, "slack.com.evil.io": false, "github.com": false, "": false, "127.0.0.1": false,
	} {
		if SlackHost(h) != want {
			t.Errorf("SlackHost(%q) = %v", h, !want)
		}
	}
}

// handshake dials srv with tlsfp and with crypto/tls under the same
// settings, and returns both errors.
func handshake(t *testing.T, srv *tlsfptest.Server, name string, roots *x509.CertPool) (fp, std error) {
	t.Helper()
	d := &Dialer{Dial: func(ctx context.Context, network, addr string) (net.Conn, error) { return srv.Dial(network, addr) }, RootCAs: roots}
	c, fp := d.DialTLSContext(context.Background(), "tcp", net.JoinHostPort(name, "443"))
	if fp == nil {
		c.Close()
	}
	raw, err := srv.Dial("tcp", "")
	if err != nil {
		t.Fatal(err)
	}
	tc := tls.Client(raw, &tls.Config{ServerName: name, RootCAs: roots, NextProtos: []string{"http/1.1"}})
	std = tc.Handshake()
	tc.Close()
	return fp, std
}

// Certificate verification is crypto/tls's: every certificate crypto/tls
// refuses, tlsfp refuses, and the ones it accepts, tlsfp accepts.
func TestVerification(t *testing.T) {
	ok := func(w http.ResponseWriter, r *http.Request) {}
	ca, other := tlsfptest.NewCA(t), tlsfptest.NewCA(t)
	selfCert, selfPool := tlsfptest.SelfSigned(t, "self.test")
	for _, c := range []struct {
		name   string
		cert   tls.Certificate
		host   string
		roots  *x509.CertPool
		accept bool
	}{
		{"trusted CA", ca.Leaf(t, "fp.test"), "fp.test", ca.Pool, true},
		{"self-signed, trusted via a custom pool", selfCert, "self.test", selfPool, true},
		{"untrusted CA, system roots", ca.Leaf(t, "fp.test"), "fp.test", nil, false},
		{"untrusted CA, another pool", ca.Leaf(t, "fp.test"), "fp.test", other.Pool, false},
		{"self-signed, not trusted", selfCert, "self.test", ca.Pool, false},
		{"wrong host name", ca.Leaf(t, "other.test"), "fp.test", ca.Pool, false},
		{"wildcard one level only", ca.Leaf(t, "*.slack.com"), "a.b.slack.com", ca.Pool, false},
		{"wildcard", ca.Leaf(t, "*.slack.com"), "hackclub.slack.com", ca.Pool, true},
		{"expired", ca.ExpiredLeaf(t, "fp.test"), "fp.test", ca.Pool, false},
	} {
		srv := tlsfptest.NewServer(t, http.HandlerFunc(ok), c.cert)
		fp, std := handshake(t, srv, c.host, c.roots)
		if (fp == nil) != c.accept || (std == nil) != c.accept {
			t.Errorf("%s: tlsfp err %v, crypto/tls err %v, want accepted=%v", c.name, fp, std, c.accept)
		}
	}
}

// With no RootCAs, tlsfp uses the system roots, so SSL_CERT_FILE works.
// The system pool is loaded once per process: the check runs in a child.
func TestSSLCertFile(t *testing.T) {
	if addr := os.Getenv("TLSFP_CHILD_ADDR"); addr != "" {
		d := &Dialer{Dial: func(ctx context.Context, network, _ string) (net.Conn, error) { return net.Dial(network, addr) }}
		c, err := d.DialTLSContext(context.Background(), "tcp", "fp.test:443")
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
		return
	}
	ca := tlsfptest.NewCA(t)
	srv := tlsfptest.NewServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), ca.Leaf(t, "fp.test"))
	pemFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(pemFile, ca.PEM(), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(certFile string) error {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSSLCertFile$", "-test.count=1")
		cmd.Env = append(os.Environ(), "TLSFP_CHILD_ADDR="+srv.Addr, "SSL_CERT_FILE="+certFile, "SSL_CERT_DIR="+t.TempDir())
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("child: %s", out)
		}
		return err
	}
	if err := run(pemFile); err != nil {
		t.Errorf("a CA in SSL_CERT_FILE wasn't trusted: %v", err)
	}
	if err := run(filepath.Join(t.TempDir(), "none.pem")); err == nil {
		t.Error("trusted the test CA without SSL_CERT_FILE")
	}
}

// NewTransport speaks HTTP/1.1 even to a server that prefers h2, sends
// the recorded hello and reuses its connections.
func TestTransport(t *testing.T) {
	ca := tlsfptest.NewCA(t)
	srv := tlsfptest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, r.Proto)
	}), ca.Leaf(t, "clack-fp.invalid"))
	tr := NewTransport(&Dialer{Dial: func(ctx context.Context, n, a string) (net.Conn, error) { return srv.Dial(n, a) }, RootCAs: ca.Pool})
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr}
	for range 3 {
		resp, err := c.Get("https://clack-fp.invalid/")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(b) != "HTTP/1.1" || resp.Proto != "HTTP/1.1" {
			t.Fatalf("server saw %q, client %q", b, resp.Proto)
		}
	}
	hs := srv.Hellos()
	if len(hs) != 1 {
		t.Fatalf("%d connections for 3 sequential requests, want 1 (keep-alive)", len(hs))
	}
	assertHello(t, hs[0], "clack-fp.invalid")
}

func assertHello(t *testing.T, rec []byte, sni string) {
	t.Helper()
	want, _ := tlsfptest.Parse(record)
	got, err := tlsfptest.Parse(rec)
	if err != nil {
		t.Fatal(err)
	}
	if d := tlsfptest.Diff(got, want, sni); d != nil {
		t.Fatalf("ClientHello differs from the recording: %s", strings.Join(d, "; "))
	}
}

// fakeProxy is an HTTP CONNECT proxy that tunnels every CONNECT to target.
func fakeProxy(t *testing.T, target string, status int, connects *atomic.Value) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				br := bufio.NewReader(c)
				req, err := http.ReadRequest(br)
				if err != nil {
					return
				}
				connects.Store(req.Method + " " + req.Host + " " + req.Header.Get("Proxy-Authorization"))
				if status != 200 {
					io.WriteString(c, "HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n")
					return
				}
				up, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer up.Close()
				io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
				go io.Copy(up, br)
				io.Copy(c, up)
			}()
		}
	}()
	return "http://u:p@" + ln.Addr().String()
}

// Through an HTTPS_PROXY the handshake with Slack runs inside the CONNECT
// tunnel: the server sees the recorded hello, not the proxy's.
func TestProxyConnect(t *testing.T) {
	ca := tlsfptest.NewCA(t)
	srv := tlsfptest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }), ca.Leaf(t, "clack-fp.invalid"))
	var connects atomic.Value
	pu, _ := url.Parse(fakeProxy(t, srv.Addr, 200, &connects))
	tr := NewTransport(&Dialer{Proxy: http.ProxyURL(pu), RootCAs: ca.Pool})
	defer tr.CloseIdleConnections()
	resp, err := (&http.Client{Transport: tr}).Get("https://clack-fp.invalid/x")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "ok" {
		t.Fatalf("body %q", b)
	}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte("u:p"))
	if got := connects.Load(); got != "CONNECT clack-fp.invalid:443 "+auth {
		t.Fatalf("proxy saw %q", got)
	}
	hs := srv.Hellos()
	if len(hs) != 1 {
		t.Fatalf("%d hellos", len(hs))
	}
	assertHello(t, hs[0], "clack-fp.invalid")

	// a refused CONNECT is an error, not a plaintext fallback
	pu2, _ := url.Parse(fakeProxy(t, srv.Addr, 407, &connects))
	tr2 := NewTransport(&Dialer{Proxy: http.ProxyURL(pu2), RootCAs: ca.Pool})
	if _, err := (&http.Client{Transport: tr2}).Get("https://clack-fp.invalid/x"); err == nil || !strings.Contains(err.Error(), "407") {
		t.Fatalf("refused CONNECT: %v", err)
	}
}

// Mixed sends Slack's hosts through the recorded hello and leaves every
// other host on Go's default transport.
func TestMixed(t *testing.T) {
	ca := tlsfptest.NewCA(t)
	srv := tlsfptest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }),
		ca.Leaf(t, "fp-test.slack.com", "127.0.0.1"))
	old := *pluginDialer
	*pluginDialer = Dialer{Dial: func(ctx context.Context, n, a string) (net.Conn, error) { return srv.Dial(n, a) }, RootCAs: ca.Pool}
	t.Cleanup(func() { Mixed.(mixed).slack.CloseIdleConnections(); *pluginDialer = old })

	resp, err := Client.Get("https://fp-test.slack.com/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	hs := srv.Hellos()
	if len(hs) != 1 {
		t.Fatalf("%d hellos", len(hs))
	}
	assertHello(t, hs[0], "fp-test.slack.com")

	// not Slack: Go's own hello (the default transport doesn't trust the
	// test CA, so the request fails after the hello is recorded)
	Client.Get(srv.URL + "/")
	hs = srv.Hellos()
	if len(hs) != 2 {
		t.Fatalf("%d hellos", len(hs))
	}
	got, _ := tlsfptest.Parse(hs[1])
	want, _ := tlsfptest.Parse(record)
	if tlsfptest.Diff(got, want, "") == nil {
		t.Fatal("a non-Slack host got the Slack hello")
	}
}

func TestHandshakeTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close() // never answers
		}
	}()
	d := &Dialer{Dial: func(ctx context.Context, n, _ string) (net.Conn, error) { return net.Dial(n, ln.Addr().String()) }, HandshakeTimeout: 200 * time.Millisecond}
	t0 := time.Now()
	if _, err := d.DialTLSContext(context.Background(), "tcp", "fp.test:443"); err == nil || time.Since(t0) > 5*time.Second {
		t.Fatalf("err %v after %v", err, time.Since(t0))
	}
}
