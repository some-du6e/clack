// Package tlsfp makes every TLS connection Clack opens to Slack.
//
// Hack Club's Enterprise Grid ends a session everywhere a few seconds after
// it sees the session's cookie and token on a connection whose ClientHello
// is Go crypto/tls's (docs/SIGNOUT-AUDIT.md has the A/B/C tests). The same
// requests over Python urllib's OpenSSL 3.0.13 ClientHello survived. So
// connections to Slack present that ClientHello, recorded byte for byte in
// openssl-3.0.13.bin, through uTLS (github.com/refraction-networking/utls,
// a fork of crypto/tls that lets the client choose its hello). They speak
// HTTP/1.1 only, as the surviving client did: the hello offers ALPN
// http/1.1 and nothing else.
//
// Only this package may import crypto/tls or utls (scan_test.go). Every
// transport that can reach Slack is built by NewTransport; anything that
// may also reach other hosts goes through Mixed, which sends Slack's hosts
// here and everything else through Go's default transport.
package tlsfp

import (
	"context"
	"crypto/x509"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"strings"

	utls "github.com/refraction-networking/utls"
)

// record is the ClientHello Python 3 urllib sent (OpenSSL 3.0.13, SNI
// slack.com, ALPN http/1.1), as one TLS record, header included. It holds
// no secret: the random, session id and x25519 key share are replaced on
// every connection.
//
//go:embed openssl-3.0.13.bin
var record []byte

// Record returns a copy of the embedded ClientHello record.
func Record() []byte { return append([]byte(nil), record...) }

func init() {
	if _, err := spec(""); err != nil {
		panic("tlsfp: embedded ClientHello: " + err.Error())
	}
}

// spec builds a fresh ClientHelloSpec from the record (uTLS mutates specs
// while applying them, so every connection gets its own) with serverName as
// the SNI.
//
// What it does to the parsed record, and why:
//   - AllowBluntMimicry: encrypt_then_mac (22) and post_handshake_auth (49)
//     have no uTLS type and are sent as the same empty extensions. They only
//     announce support: Slack negotiates TLS 1.3, where neither applies. A
//     TLS 1.2 server that chose encrypt-then-MAC with a CBC suite, or that
//     asked for a post-handshake certificate, would make the handshake fail
//     (bad record MAC / unexpected message): it fails closed, it is never
//     weaker.
//   - Versions come from supported_versions (TLS 1.3 and 1.2), not from the
//     record header's legacy 0x0301.
//   - SNI is the host being dialled. The record's slack.com is dropped.
//   - Padding follows OpenSSL's rule (pad a 256–511 byte hello to 512), not
//     uTLS's default for parsed hellos (always pad to the recorded length).
//     They agree for every Slack host name; they differ only for a name
//     long enough to push the hello past 511 bytes, where both send none.
func spec(serverName string) (*utls.ClientHelloSpec, error) {
	s, err := (&utls.Fingerprinter{AllowBluntMimicry: true}).FingerprintClientHello(record)
	if err != nil {
		return nil, err
	}
	s.TLSVersMin, s.TLSVersMax = 0, 0
	sni := 0
	for _, e := range s.Extensions {
		switch e := e.(type) {
		case *utls.SNIExtension:
			e.ServerName = serverName
			sni++
		case *utls.UtlsPaddingExtension:
			e.GetPaddingLen = utls.BoringPaddingStyle
		case *utls.ALPNExtension:
			if len(e.AlpnProtocols) != 1 || e.AlpnProtocols[0] != "http/1.1" {
				return nil, fmt.Errorf("ALPN %q, want only http/1.1", e.AlpnProtocols)
			}
		}
	}
	if sni != 1 {
		return nil, errors.New("no server_name extension")
	}
	return s, nil
}

// Handshake runs a TLS client handshake on conn with the embedded
// ClientHello and serverName as SNI. The server's certificate is verified
// exactly as crypto/tls does (uTLS runs crypto/tls's own code with
// crypto/x509): chain to roots, or to the system roots when roots is nil
// (SSL_CERT_FILE and SSL_CERT_DIR honoured), and valid for serverName.
// Nothing here skips or relaxes verification. On error conn is closed.
func Handshake(ctx context.Context, conn net.Conn, serverName string, roots *x509.CertPool) (net.Conn, error) {
	if serverName == "" {
		conn.Close()
		return nil, errors.New("tlsfp: no server name")
	}
	s, err := spec(serverName)
	if err != nil {
		conn.Close()
		return nil, err
	}
	cfg := &utls.Config{
		ServerName: serverName,
		RootCAs:    roots,
		MinVersion: utls.VersionTLS12,
		NextProtos: []string{"http/1.1"},
	}
	uc := utls.UClient(conn, cfg, utls.HelloCustom)
	if err := uc.ApplyPreset(s); err != nil {
		conn.Close()
		return nil, fmt.Errorf("tlsfp: %w", err)
	}
	if err := uc.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	if p := uc.ConnectionState().NegotiatedProtocol; p != "" && p != "http/1.1" {
		uc.Close()
		return nil, fmt.Errorf("tlsfp: %s negotiated %q, want http/1.1", serverName, p)
	}
	return uc, nil
}

// SlackDomains are the registrable domains Slack serves from. A host is
// Slack's when it is one of them or a subdomain (SlackHost).
var SlackDomains = []string{"slack.com", "slack-edge.com", "slack-imgs.com", "slack-files.com"}

// SlackHost reports whether host (a URL's Hostname, maybe with a port) is
// on one of SlackDomains. It is deliberately looser than the credential
// checks in package slack: case, a port and a trailing dot ("slack.com.")
// are ignored, so anything that resolves to Slack gets Slack's hello.
func SlackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	h := strings.ToLower(strings.TrimRight(host, "."))
	for _, d := range SlackDomains {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}
