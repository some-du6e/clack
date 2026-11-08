package slack

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DPoP (RFC 9449) as Slack desktop 4.53.276 does it (docs/DESKTOP-SIGNIN.md,
// "Optional DPoP binding"), for sign-in links whose token set says dpop=1:
//
//   - one P-256 key per base domain (Slack desktop: userData/dpop-keys/<domain>,
//     encrypted with Electron safeStorage; Clack: <config>/dpop-keys/<domain>.pem,
//     owner-only like creds.json, not encrypted);
//   - a proof is a JWT with header {typ:"dpop+jwt", alg:"ES256", jwk} and
//     claims {jti, htm, htu, iat}, sent in a "Dpop" header;
//   - the auth.loginMagicBulk exchange carries one (that is the binding), and
//     after it every slack.com API and websocket request carries a fresh one;
//   - a response header "dpop-unbind: true" deletes the key for that domain.
//
// Unverified (static reading only; nothing was sent to Slack): whether htu
// keeps the query string (Clack follows the RFC and drops it), whether file
// downloads and the desktop boot page need a proof (Clack sends none on
// downloads and one on the boot page, which is a request to Slack with the
// bound session), whether Slack ever asks for a server nonce, and what Slack
// answers when it rejects a proof. A rejection Clack recognises (HTTP 401
// with a DPoP WWW-Authenticate, or an error code naming dpop) stops all
// traffic of the session with a clear error instead of retrying.

// DPoPError means the session's DPoP binding can't be used: Clack sends
// nothing more on this session. Sign in again with a new link.
type DPoPError struct{ Reason string }

func (e *DPoPError) Error() string {
	return "slack: this session is bound to a DPoP key and " + e.Reason + "; sign in again with a new Slack link"
}

// dpopKey is a per-domain DPoP key.
type dpopKey struct {
	priv   *ecdsa.PrivateKey
	path   string
	header string // base64url of the JWT header (it carries the public key)
}

// dpopKeyPath is where the key for domain lives under dir. domain must be
// slack.com or a subdomain: it becomes a file name.
func dpopKeyPath(dir, domain string) (string, error) {
	d, ok := normHost(domain)
	if !ok || !CookieHost(d) {
		return "", fmt.Errorf("slack: no DPoP key for %q", Redact(domain))
	}
	return filepath.Join(dir, "dpop-keys", d+".pem"), nil
}

// loadOrCreateDPoPKey reads the key for domain, or makes and saves one.
func loadOrCreateDPoPKey(dir, domain string) (*dpopKey, error) {
	p, err := dpopKeyPath(dir, domain)
	if err != nil {
		return nil, err
	}
	if k, err := loadDPoPKey(p); err == nil {
		return k, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	if err := WriteSecretFile(p, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})); err != nil {
		return nil, err
	}
	_ = RestrictDir(filepath.Dir(p))
	return newDPoPKey(priv, p), nil
}

// loadDPoPKey reads an existing key (os.ErrNotExist if there is none).
func loadDPoPKey(p string) (*dpopKey, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	_ = RestrictFile(p)
	blk, _ := pem.Decode(b)
	if blk == nil {
		return nil, fmt.Errorf("slack: %s isn't a PEM key", p)
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("slack: %s: %w", p, err)
	}
	priv, ok := k.(*ecdsa.PrivateKey)
	if !ok || priv.Curve != elliptic.P256() {
		return nil, fmt.Errorf("slack: %s isn't a P-256 key", p)
	}
	return newDPoPKey(priv, p), nil
}

func newDPoPKey(priv *ecdsa.PrivateKey, path string) *dpopKey {
	h, _ := json.Marshal(map[string]any{"typ": "dpop+jwt", "alg": "ES256", "jwk": publicJWK(&priv.PublicKey)})
	return &dpopKey{priv: priv, path: path, header: b64(h)}
}

// publicJWK is the key's public half as a JWK (RFC 7518 6.2.1).
func publicJWK(pub *ecdsa.PublicKey) map[string]string {
	return map[string]string{"kty": "EC", "crv": "P-256", "x": b64(pad32(pub.X)), "y": b64(pad32(pub.Y))}
}

func pad32(n *big.Int) []byte {
	b := make([]byte, 32)
	return n.FillBytes(b)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// dpopHTU is the proof's htu: the request URL without query or fragment
// (RFC 9449 4.2). A websocket handshake is named by its wss:// URL.
func dpopHTU(u *url.URL, websocket bool) string {
	scheme := strings.ToLower(u.Scheme)
	if websocket {
		switch scheme {
		case "https":
			scheme = "wss"
		case "http":
			scheme = "ws"
		}
	}
	return scheme + "://" + strings.ToLower(u.Host) + u.EscapedPath()
}

// proof signs a proof for one request.
func (k *dpopKey) proof(method string, u *url.URL, websocket bool) (string, error) {
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", err
	}
	c, _ := json.Marshal(map[string]any{"jti": b64(jti), "htm": method, "htu": dpopHTU(u, websocket), "iat": time.Now().Unix()})
	in := k.header + "." + b64(c)
	sum := sha256.Sum256([]byte(in))
	r, s, err := ecdsa.Sign(rand.Reader, k.priv, sum[:])
	if err != nil {
		return "", err
	}
	return in + "." + b64(append(pad32(r), pad32(s)...)), nil
}

// dpopApplies reports whether req gets a proof: Slack's API (/api/… and
// edgeapi) and the websocket, on slack.com hosts, but not the sign-in
// exchange itself, whose proof LoginMagic adds (Slack desktop exempts it
// from the per-request header the same way, main:a7).
func dpopApplies(req *http.Request) bool {
	if isWebsocket(req) {
		return true
	}
	p := req.URL.Path
	if strings.HasSuffix(p, "/api/auth.loginMagicBulk") {
		return false
	}
	return strings.HasPrefix(p, "/api/") || strings.EqualFold(req.URL.Hostname(), "edgeapi.slack.com") || p == "/auth" && strings.EqualFold(req.URL.Hostname(), "app.slack.com")
}

func isWebsocket(req *http.Request) bool {
	return strings.EqualFold(req.Header.Get("Upgrade"), "websocket")
}

// dpopBinding is a bound session's DPoP state.
type dpopBinding struct {
	mu       sync.Mutex
	key      *dpopKey // nil after dpop-unbind
	failed   error    // set: nothing more is sent
	onUnbind func()
	onFail   func(code string)
}

// sign returns req with a fresh proof when it needs one, or the binding's
// failure (nothing is sent then).
func (b *dpopBinding) sign(req *http.Request) (*http.Request, error) {
	b.mu.Lock()
	k, failed := b.key, b.failed
	b.mu.Unlock()
	if failed != nil {
		return nil, failed
	}
	if k == nil || !dpopApplies(req) {
		return req, nil
	}
	p, err := k.proof(req.Method, req.URL, isWebsocket(req))
	if err != nil {
		return nil, b.fail(&DPoPError{Reason: "Clack couldn't sign a proof (" + err.Error() + ")"}, "dpop_sign_failed")
	}
	r := req.Clone(req.Context())
	r.Header.Set("Dpop", p)
	return r, nil
}

// observe reads a response for dpop-unbind and for a rejected proof.
func (b *dpopBinding) observe(resp *http.Response) {
	if strings.EqualFold(strings.TrimSpace(resp.Header.Get("Dpop-Unbind")), "true") {
		b.mu.Lock()
		k, f := b.key, b.onUnbind
		b.key = nil
		b.mu.Unlock()
		if k != nil {
			_ = os.Remove(k.path)
			if f != nil {
				f()
			}
		}
		return
	}
	if resp.StatusCode == http.StatusUnauthorized {
		if wa := resp.Header.Get("WWW-Authenticate"); strings.Contains(strings.ToLower(wa), "dpop") {
			b.fail(&DPoPError{Reason: "Slack rejected its proof (HTTP 401: " + Redact(trimTo(wa, 200)) + ")"}, "dpop_rejected")
		}
	}
}

// failCode is called with a Slack error code; one that names DPoP ends the
// binding.
func (b *dpopBinding) failCode(code string) {
	if strings.Contains(strings.ToLower(code), "dpop") {
		b.fail(&DPoPError{Reason: "Slack rejected its proof (" + code + ")"}, code)
	}
}

func (b *dpopBinding) fail(err error, code string) error {
	b.mu.Lock()
	first := b.failed == nil || b.failed == errDPoPNotLoaded
	if first {
		b.failed = err
	}
	err, f := b.failed, b.onFail
	b.mu.Unlock()
	if first && f != nil {
		f(code)
	}
	return err
}

func trimTo(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// EnableDPoP loads a DPoP-bound session's key from dir (next to
// creds.json). Without it (or if it fails) the session sends nothing and
// is marked signed out: an unbound request from a bound session is the kind
// of mismatch to avoid. No-op for unbound sessions.
func (c *Client) EnableDPoP(dir string) error {
	if c.dpop == nil {
		return nil
	}
	p, err := dpopKeyPath(dir, c.Creds.DPoP)
	if err == nil {
		var k *dpopKey
		if k, err = loadDPoPKey(p); err == nil {
			c.dpop.mu.Lock()
			if c.dpop.failed == errDPoPNotLoaded {
				c.dpop.key, c.dpop.failed = k, nil
			}
			c.dpop.mu.Unlock()
			return nil
		}
	}
	reason := "its key couldn't be loaded"
	if errors.Is(err, os.ErrNotExist) {
		reason = "its key file is missing"
	}
	return c.dpop.fail(&DPoPError{Reason: reason + " (" + Redact(err.Error()) + ")"}, "dpop_key_missing")
}

// errDPoPNotLoaded is a bound session's state until EnableDPoP.
var errDPoPNotLoaded = &DPoPError{Reason: "its key hasn't been loaded (EnableDPoP)"}

// newDPoPBinding is a bound client's binding, failing until EnableDPoP.
func (c *Client) newDPoPBinding() *dpopBinding {
	return &dpopBinding{
		failed:   errDPoPNotLoaded,
		onUnbind: func() { c.sess.unbind() },
		onFail:   func(code string) { c.markAuth(code) },
	}
}
