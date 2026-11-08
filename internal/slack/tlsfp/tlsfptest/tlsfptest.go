// Package tlsfptest is for tests: a loopback HTTPS server that records the
// raw ClientHello of every connection, a test CA, and a ClientHello parser
// that compares the fields a fingerprint (JA3/JA4) is made of.
package tlsfptest

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"
)

// Hello is a parsed ClientHello: everything a JA3/JA4 fingerprint reads,
// plus the SNI and ALPN.
type Hello struct {
	Version        uint16 // legacy_version
	CipherSuites   []uint16
	Compression    []byte
	Extensions     []uint16 // types, in order
	SupportedGroup []uint16
	PointFormats   []byte
	SigAlgs        []uint16
	ALPN           []string
	SNI            string
	Versions       []uint16 // supported_versions
	PSKModes       []byte
	KeyShareGroups []uint16
	Len            int // handshake message length, header included
}

// Parse reads a ClientHello from a TLS record (header included).
func Parse(rec []byte) (Hello, error) {
	var h Hello
	if len(rec) < 9 || rec[0] != 22 {
		return h, errors.New("not a handshake record")
	}
	n := int(binary.BigEndian.Uint16(rec[3:5]))
	if len(rec) < 5+n {
		return h, errors.New("short record")
	}
	p := rec[5 : 5+n]
	if p[0] != 1 {
		return h, errors.New("not a ClientHello")
	}
	ml := int(p[1])<<16 | int(p[2])<<8 | int(p[3])
	if ml+4 > len(p) {
		return h, errors.New("ClientHello spans records")
	}
	h.Len = ml + 4
	b := p[4 : 4+ml]
	rd := func(k int) []byte {
		if len(b) < k {
			panic("short")
		}
		v := b[:k]
		b = b[k:]
		return v
	}
	var err error
	func() {
		defer func() {
			if recover() != nil {
				err = errors.New("malformed ClientHello")
			}
		}()
		h.Version = binary.BigEndian.Uint16(rd(2))
		rd(32)
		rd(int(rd(1)[0]))
		cs := rd(int(binary.BigEndian.Uint16(rd(2))))
		h.CipherSuites = u16s(cs)
		h.Compression = append([]byte(nil), rd(int(rd(1)[0]))...)
		if len(b) == 0 {
			return
		}
		exts := rd(int(binary.BigEndian.Uint16(rd(2))))
		for len(exts) >= 4 {
			t := binary.BigEndian.Uint16(exts)
			l := int(binary.BigEndian.Uint16(exts[2:]))
			d := exts[4 : 4+l]
			exts = exts[4+l:]
			h.Extensions = append(h.Extensions, t)
			switch t {
			case 0: // server_name
				if len(d) >= 5 {
					h.SNI = string(d[5:][:binary.BigEndian.Uint16(d[3:])])
				}
			case 10:
				h.SupportedGroup = u16s(d[2:])
			case 11:
				h.PointFormats = append([]byte(nil), d[1:]...)
			case 13:
				h.SigAlgs = u16s(d[2:])
			case 16:
				for x := d[2:]; len(x) > 0; {
					h.ALPN = append(h.ALPN, string(x[1:1+x[0]]))
					x = x[1+x[0]:]
				}
			case 43:
				h.Versions = u16s(d[1:])
			case 45:
				h.PSKModes = append([]byte(nil), d[1:]...)
			case 51:
				for x := d[2:]; len(x) >= 4; {
					h.KeyShareGroups = append(h.KeyShareGroups, binary.BigEndian.Uint16(x))
					x = x[4+binary.BigEndian.Uint16(x[2:]):]
				}
			}
		}
	}()
	return h, err
}

func u16s(b []byte) []uint16 {
	var out []uint16
	for i := 0; i+1 < len(b); i += 2 {
		out = append(out, binary.BigEndian.Uint16(b[i:]))
	}
	return out
}

// Diff lists the fingerprint fields where got differs from want. The SNI
// is compared with wantSNI, not want's (the recording's is slack.com).
// Padding (21) is left out of the extension order when one side omits it.
func Diff(got, want Hello, wantSNI string) []string {
	var d []string
	cmp := func(name string, a, b any) {
		if fmt.Sprint(a) != fmt.Sprint(b) {
			d = append(d, fmt.Sprintf("%s: got %v, want %v", name, a, b))
		}
	}
	cmp("legacy_version", got.Version, want.Version)
	cmp("cipher_suites", got.CipherSuites, want.CipherSuites)
	cmp("compression", got.Compression, want.Compression)
	cmp("extensions", got.Extensions, want.Extensions)
	cmp("supported_groups", got.SupportedGroup, want.SupportedGroup)
	cmp("ec_point_formats", got.PointFormats, want.PointFormats)
	cmp("signature_algorithms", got.SigAlgs, want.SigAlgs)
	cmp("alpn", got.ALPN, want.ALPN)
	cmp("supported_versions", got.Versions, want.Versions)
	cmp("psk_modes", got.PSKModes, want.PSKModes)
	cmp("key_share groups", got.KeyShareGroups, want.KeyShareGroups)
	cmp("sni", got.SNI, wantSNI)
	return d
}

// CA is a throwaway certificate authority.
type CA struct {
	Pool *x509.CertPool
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

// NewCA makes a CA.
func NewCA(t testing.TB) *CA {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "tlsfptest CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(c)
	return &CA{Pool: pool, cert: c, key: key}
}

// Leaf issues a server certificate for names (DNS names or IPs).
func (ca *CA) Leaf(t testing.TB, names ...string) tls.Certificate {
	return ca.leaf(t, time.Now().Add(time.Hour), names)
}

// ExpiredLeaf issues a certificate for names that expired an hour ago.
func (ca *CA) ExpiredLeaf(t testing.TB, names ...string) tls.Certificate {
	return ca.leaf(t, time.Now().Add(-time.Hour), names)
}

// SelfSigned makes a self-signed (non-CA) certificate for names and a pool
// holding just it.
func SelfSigned(t testing.TB, names ...string) (tls.Certificate, *x509.CertPool) {
	ca := &CA{}
	var err error
	if ca.key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader); err != nil {
		t.Fatal(err)
	}
	cert := ca.leaf(t, time.Now().Add(time.Hour), names)
	c, _ := x509.ParseCertificate(cert.Certificate[0])
	pool := x509.NewCertPool()
	pool.AddCert(c)
	return cert, pool
}

// PEM is the CA certificate, PEM-encoded (for SSL_CERT_FILE).
func (ca *CA) PEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.cert.Raw})
}

func (ca *CA) leaf(t testing.TB, notAfter time.Time, names []string) tls.Certificate {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: names[0]},
		NotBefore: notAfter.Add(-2 * time.Hour), NotAfter: notAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, n)
		}
	}
	parent, signer := ca.cert, ca.key
	if parent == nil { // self-signed
		parent, key = tmpl, ca.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// Server is an HTTPS server on loopback that records each connection's
// ClientHello before the TLS server reads it.
type Server struct {
	*httptest.Server
	Addr string // 127.0.0.1:port

	mu     sync.Mutex
	hellos [][]byte
}

// NewServer starts h over TLS with cert, offering ALPN h2 and http/1.1
// (so a client that offered h2 would get it).
func NewServer(t testing.TB, h http.Handler, cert tls.Certificate) *Server {
	s := &Server{}
	srv := httptest.NewUnstartedServer(h)
	srv.Listener.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.Listener = &recordingListener{Listener: ln, s: s}
	srv.EnableHTTP2 = true
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	s.Server = srv
	s.Addr = ln.Addr().String()
	t.Cleanup(srv.Close)
	return s
}

// Hellos returns the raw ClientHello records seen so far.
func (s *Server) Hellos() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.hellos)
}

// Dial ignores addr and connects to the server (a Dialer.Dial for tests).
func (s *Server) Dial(network, addr string) (net.Conn, error) {
	return net.Dial("tcp", s.Addr)
}

type recordingListener struct {
	net.Listener
	s *Server
}

func (l *recordingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	hdr := make([]byte, 5)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return &prefixConn{Conn: c, r: bytes.NewReader(hdr)}, nil
	}
	body := make([]byte, binary.BigEndian.Uint16(hdr[3:]))
	n, _ := io.ReadFull(c, body)
	c.SetReadDeadline(time.Time{})
	rec := append(hdr, body[:n]...)
	l.s.mu.Lock()
	l.s.hellos = append(l.s.hellos, rec)
	l.s.mu.Unlock()
	return &prefixConn{Conn: c, r: bytes.NewReader(rec)}, nil
}

// prefixConn replays what Accept read before the rest of the connection.
type prefixConn struct {
	net.Conn
	r *bytes.Reader
}

func (c *prefixConn) Read(b []byte) (int, error) {
	if c.r.Len() > 0 {
		return c.r.Read(b)
	}
	return c.Conn.Read(b)
}
