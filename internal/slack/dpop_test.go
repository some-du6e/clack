package slack

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// checkProof verifies a Dpop header: its shape, its signature, and that it
// names method and htu. It returns the claims and the public key.
func checkProof(t *testing.T, p, method, htu string) (map[string]any, *ecdsa.PublicKey) {
	t.Helper()
	parts := strings.Split(p, ".")
	if len(parts) != 3 {
		t.Fatalf("proof has %d parts", len(parts))
	}
	dec := func(s string) []byte {
		b, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			t.Fatalf("not base64url: %v", err)
		}
		return b
	}
	var hdr struct {
		Typ string            `json:"typ"`
		Alg string            `json:"alg"`
		JWK map[string]string `json:"jwk"`
	}
	if err := json.Unmarshal(dec(parts[0]), &hdr); err != nil {
		t.Fatal(err)
	}
	if hdr.Typ != "dpop+jwt" || hdr.Alg != "ES256" || hdr.JWK["kty"] != "EC" || hdr.JWK["crv"] != "P-256" || len(hdr.JWK) != 4 {
		t.Errorf("header %+v", hdr)
	}
	pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(dec(hdr.JWK["x"])), Y: new(big.Int).SetBytes(dec(hdr.JWK["y"]))}
	sig := dec(parts[2])
	if len(sig) != 64 {
		t.Fatalf("signature is %d bytes, want 64 (r||s)", len(sig))
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Error("signature doesn't verify")
	}
	var claims map[string]any
	if err := json.Unmarshal(dec(parts[1]), &claims); err != nil {
		t.Fatal(err)
	}
	if len(claims) != 4 || claims["htm"] != method || claims["htu"] != htu || claims["jti"] == "" {
		t.Errorf("claims %v, want htm %s htu %s", claims, method, htu)
	}
	if iat, _ := claims["iat"].(float64); time.Since(time.Unix(int64(iat), 0)) > time.Minute {
		t.Errorf("iat %v", claims["iat"])
	}
	return claims, pub
}

func TestDPoPProofShape(t *testing.T) {
	dir := t.TempDir()
	k, err := loadOrCreateDPoPKey(dir, "slack.com")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse("https://hackclub.slack.com/api/conversations.history?x=1#f")
	p1, _ := k.proof(http.MethodPost, u, false)
	p2, _ := k.proof(http.MethodPost, u, false)
	c1, pub := checkProof(t, p1, "POST", "https://hackclub.slack.com/api/conversations.history")
	c2, _ := checkProof(t, p2, "POST", "https://hackclub.slack.com/api/conversations.history")
	if c1["jti"] == c2["jti"] {
		t.Error("jti repeated")
	}
	ws, _ := url.Parse("https://wss-primary.slack.com/?token=xoxc-secret")
	pw, _ := k.proof(http.MethodGet, ws, true)
	checkProof(t, pw, "GET", "wss://wss-primary.slack.com/")
	if strings.Contains(pw, "secret") {
		t.Error("query in the proof")
	}

	// The same key comes back from disk, owner-only.
	k2, err := loadOrCreateDPoPKey(dir, "SLACK.com")
	if err != nil {
		t.Fatal(err)
	}
	if !k2.priv.PublicKey.Equal(pub) || k2.header != k.header {
		t.Error("key not reloaded")
	}
	p := filepath.Join(dir, "dpop-keys", "slack.com.pem")
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("key file %v %v", fi, err)
		}
		if fi, _ := os.Stat(filepath.Dir(p)); fi.Mode().Perm() != 0o700 {
			t.Errorf("key dir mode %v", fi.Mode().Perm())
		}
	}
	// Another domain, another key.
	k3, _ := loadOrCreateDPoPKey(dir, "hackclub.enterprise.slack.com")
	if k3 == nil || k3.priv.PublicKey.Equal(pub) {
		t.Error("domains share a key")
	}
	for _, d := range []string{"../slack.com", "evil.com", "slack.com/../../x", "", "a/slack.com"} {
		if _, err := loadOrCreateDPoPKey(dir, d); err == nil {
			t.Errorf("key for %q", d)
		}
	}
	if err := os.WriteFile(p, []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateDPoPKey(dir, "slack.com"); err == nil {
		t.Error("a corrupt key was replaced silently")
	}
}

// A dpop=1 link binds: the exchange, the boot page and auth.test carry
// proofs from one key, and the creds say which domain's key.
func TestLoginMagicDPoP(t *testing.T) {
	f := newFakeDesktopSlack(t)
	sets, _ := ParseMagicLink("slack://T08PSQ7BQ/magic-login/abc?dpop=1")
	if _, err := LoginMagic(context.Background(), sets, MagicConfig{}); err == nil || !strings.Contains(err.Error(), "DPoP") {
		t.Errorf("no key dir: %v", err)
	}
	n0 := len(f.requests())
	dir := t.TempDir()
	c, err := LoginMagic(context.Background(), sets, MagicConfig{DPoPDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if c.DPoP != "slack.com" {
		t.Errorf("creds DPoP %q", c.DPoP)
	}
	reqs := f.requests()[n0:]
	want := []string{"https://slack.com/api/auth.loginMagicBulk", "https://app.slack.com/ssb/first", "https://hackclub.slack.com/api/auth.test"}
	if len(reqs) != len(want) {
		t.Fatalf("%d requests", len(reqs))
	}
	var key *ecdsa.PublicKey
	for i, r := range reqs {
		_, pub := checkProof(t, r.Header.Get("Dpop"), r.Method, want[i])
		if key != nil && !key.Equal(pub) {
			t.Errorf("request %d used another key", i)
		}
		key = pub
	}
	if _, err := os.Stat(filepath.Join(dir, "dpop-keys", "slack.com.pem")); err != nil {
		t.Error(err)
	}
	// Unbound links send no proof at all.
	n0 = len(f.requests())
	sets, _ = ParseMagicLink("slack://T08PSQ7BQ/magic-login/abc")
	if c, err := LoginMagic(context.Background(), sets, MagicConfig{DPoPDir: dir}); err != nil || c.DPoP != "" {
		t.Fatalf("unbound: %v %q", err, c.DPoP)
	}
	for _, r := range f.requests()[n0:] {
		if r.Header.Get("Dpop") != "" {
			t.Errorf("unbound %s sent a proof", r.URL.Path)
		}
	}
}

type dpopServer struct {
	srv   *httptest.Server
	hits  atomic.Int32
	mu    sync.Mutex
	proof []string
	path  []string
	// answer, when set, writes the response
	answer func(w http.ResponseWriter, r *http.Request) bool
}

func newDPoPServer(t *testing.T) *dpopServer {
	s := &dpopServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		s.mu.Lock()
		s.proof = append(s.proof, r.Header.Get("Dpop"))
		s.path = append(s.path, r.URL.Path)
		ans := s.answer
		s.mu.Unlock()
		if ans != nil && ans(w, r) {
			return
		}
		if strings.HasPrefix(r.URL.Path, "/files-pri/") {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("png"))
			return
		}
		if r.Header.Get("Upgrade") != "" {
			http.Error(w, "no", http.StatusForbidden)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *dpopServer) lastProof() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proof[len(s.proof)-1]
}

func boundClient(t *testing.T, s *dpopServer, dir string) *Client {
	t.Helper()
	cr := desktopCreds(s.srv.URL)
	cr.DPoP = "slack.com"
	return New(cr)
}

func TestBoundClientProofs(t *testing.T) {
	s := newDPoPServer(t)
	dir := t.TempDir()
	if _, err := loadOrCreateDPoPKey(dir, "slack.com"); err != nil {
		t.Fatal(err)
	}
	c := boundClient(t, s, dir)
	ctx := context.Background()
	// Until the key is loaded, nothing is sent.
	err := c.Call(ctx, "plain.method", nil, nil)
	var de *DPoPError
	if !errors.As(err, &de) || s.hits.Load() != 0 {
		t.Fatalf("before EnableDPoP: %v, %d requests", err, s.hits.Load())
	}
	if err := c.EnableDPoP(dir); err != nil {
		t.Fatal(err)
	}
	if err := c.Call(NoBackoff(ctx), "plain.method", nil, nil); err != nil {
		t.Fatal(err)
	}
	checkProof(t, s.lastProof(), "POST", s.srv.URL+"/api/plain.method")
	// The websocket handshake carries one, named by its ws:// URL.
	r := c.NewRTM()
	r.reconnectURL = "ws" + strings.TrimPrefix(s.srv.URL, "http") + "/websocket?token=x"
	_ = r.once(ctx)
	checkProof(t, s.lastProof(), "GET", "ws"+strings.TrimPrefix(s.srv.URL, "http")+"/websocket")
	// File downloads don't (not in the evidence).
	resp, err := c.Download(ctx, s.srv.URL+"/files-pri/T1-F1/x.png")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if s.lastProof() != "" {
		t.Error("download carried a proof")
	}
}

// dpop-unbind deletes the key, stops the proofs and saves the creds unbound.
func TestDPoPUnbind(t *testing.T) {
	s := newDPoPServer(t)
	dir := t.TempDir()
	loadOrCreateDPoPKey(dir, "slack.com")
	c := boundClient(t, s, dir)
	if err := c.EnableDPoP(dir); err != nil {
		t.Fatal(err)
	}
	var saved atomic.Value
	c.PersistSession(func(nc Creds) error { saved.Store(nc); return nil })
	s.answer = func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("dpop-unbind", "true")
		return false
	}
	ctx := context.Background()
	if err := c.Call(ctx, "plain.method", nil, nil); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.answer = nil
	s.mu.Unlock()
	if err := c.Call(ctx, "plain.method", nil, nil); err != nil {
		t.Fatal(err)
	}
	if s.lastProof() != "" {
		t.Error("still sending proofs after dpop-unbind")
	}
	if _, err := os.Stat(filepath.Join(dir, "dpop-keys", "slack.com.pem")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("key file kept: %v", err)
	}
	c.FlushSession()
	if nc, _ := saved.Load().(Creds); nc.DPoP != "" || nc.Token == "" {
		t.Errorf("saved creds DPoP %q", nc.DPoP)
	}
}

// A rejected proof ends the session's traffic at once: one request, a
// clear error, no retries (calls, other methods, the socket).
func TestDPoPRejectionStopsTraffic(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(w http.ResponseWriter, r *http.Request) bool
	}{
		{"401", func(w http.ResponseWriter, r *http.Request) bool {
			w.Header().Set("WWW-Authenticate", `DPoP error="invalid_dpop_proof", algs="ES256"`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
			return true
		}},
		{"error code", func(w http.ResponseWriter, r *http.Request) bool {
			w.Write([]byte(`{"ok":false,"error":"invalid_dpop_proof"}`))
			return true
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newDPoPServer(t)
			s.answer = tc.answer
			dir := t.TempDir()
			loadOrCreateDPoPKey(dir, "slack.com")
			c := boundClient(t, s, dir)
			fired := make(chan string, 2)
			c.OnAuthFailed(func(code string) { fired <- code })
			if err := c.EnableDPoP(dir); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			c.Call(ctx, "plain.method", nil, nil)
			for i := 0; i < 5; i++ {
				c.Call(NoBackoff(ctx), "other.method", nil, nil)
			}
			r := c.NewRTM()
			r.reconnectURL = "ws" + strings.TrimPrefix(s.srv.URL, "http") + "/websocket"
			r.once(ctx)
			if n := s.hits.Load(); n != 1 {
				t.Errorf("%d requests reached Slack, want 1", n)
			}
			if !c.SignedOut() {
				t.Error("not signed out")
			}
			select {
			case code := <-fired:
				if !strings.Contains(code, "dpop") {
					t.Errorf("code %q", code)
				}
			case <-time.After(time.Second):
				t.Error("OnAuthFailed not called")
			}
		})
	}
}

// A bound session whose key is gone sends nothing and says why.
func TestEnableDPoPMissingKey(t *testing.T) {
	s := newDPoPServer(t)
	c := boundClient(t, s, "")
	fired := make(chan string, 1)
	c.OnAuthFailed(func(code string) { fired <- code })
	err := c.EnableDPoP(t.TempDir())
	var de *DPoPError
	if !errors.As(err, &de) || !strings.Contains(err.Error(), "missing") || !strings.Contains(err.Error(), "sign in again") {
		t.Errorf("err %v", err)
	}
	if err := c.Call(context.Background(), "plain.method", nil, nil); err == nil || s.hits.Load() != 0 {
		t.Errorf("sent without a key: %v, %d", err, s.hits.Load())
	}
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Error("OnAuthFailed not called")
	}
	// Unbound sessions have nothing to enable.
	if err := New(desktopCreds(s.srv.URL)).EnableDPoP(""); err != nil {
		t.Error(err)
	}
}
