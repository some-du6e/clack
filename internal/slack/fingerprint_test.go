package slack

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"

	"github.com/some-du6e/clack/internal/slack/tlsfp"
	"github.com/some-du6e/clack/internal/slack/tlsfp/tlsfptest"
)

// fpHost is the host the fingerprint tests ask for. .invalid never
// resolves (RFC 6761): every connection is redirected to the loopback
// recorder, and if a redirect were missed nothing would reach a real host.
const fpHost = "clack-fp.invalid"

type fpRecorder struct {
	*tlsfptest.Server
	mu     sync.Mutex
	protos []string // r.Proto of each request, with its path
}

// newFPRecorder starts the recorder and points the API and public dialers'
// TCP dial at it, trusting its test CA. Everything above the TCP dial (the
// ClientHello, verification, proxy choice) is the production code.
func newFPRecorder(t *testing.T) *fpRecorder {
	t.Helper()
	ca := tlsfptest.NewCA(t)
	rec := &fpRecorder{}
	rec.Server = tlsfptest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.protos = append(rec.protos, r.Proto+" "+r.URL.Path)
		rec.mu.Unlock()
		switch {
		case r.URL.Path == "/ws":
			c, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			c.Close(websocket.StatusNormalClosure, "")
		case strings.HasPrefix(r.URL.Path, "/files-pri/"):
			w.Header().Set("Content-Type", "text/plain")
			io.WriteString(w, "file")
		case strings.HasSuffix(r.URL.Path, ".png"):
			w.Header().Set("Content-Type", "image/png")
			io.WriteString(w, "PNG")
		default:
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"ok":true}`)
		}
	}), ca.Leaf(t, fpHost))
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != fpHost+":443" {
			return nil, errors.New("fingerprint test dialled " + addr)
		}
		return rec.Dial(network, addr)
	}
	oldAPI, oldPublic := apiDialer, *publicDialer
	apiDialer = &tlsfp.Dialer{Dial: dial, RootCAs: ca.Pool}
	*publicDialer = tlsfp.Dialer{Dial: dial, RootCAs: ca.Pool}
	t.Cleanup(func() {
		apiDialer = oldAPI
		*publicDialer = oldPublic
		publicTransport.CloseIdleConnections()
	})
	return rec
}

// check asserts that the connections made since n hellos ago all sent
// the recorded ClientHello for fpHost, that there was at least one, and
// that every request was HTTP/1.1.
func (rec *fpRecorder) check(t *testing.T, kind string, n int) int {
	t.Helper()
	want, err := tlsfptest.Parse(tlsfp.Record())
	if err != nil {
		t.Fatal(err)
	}
	hs := rec.Hellos()
	if len(hs) <= n {
		t.Errorf("%s: no TLS connection was made", kind)
	}
	for _, h := range hs[n:] {
		got, err := tlsfptest.Parse(h)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if d := tlsfptest.Diff(got, want, fpHost); d != nil {
			t.Errorf("%s: ClientHello differs from the recording: %s", kind, strings.Join(d, "; "))
		}
	}
	rec.mu.Lock()
	for _, p := range rec.protos {
		if !strings.HasPrefix(p, "HTTP/1.1 ") {
			t.Errorf("%s: request over %s", kind, p)
		}
	}
	rec.protos = nil
	rec.mu.Unlock()
	return len(hs)
}

// Every kind of connection Clack makes to Slack sends the recorded
// ClientHello and speaks HTTP/1.1: Web API calls (pasted and desktop
// sessions), edgeapi, multipart calls, uploads, downloads, the realtime
// socket, the sign-in exchange and credential-free CDN fetches. Each kind
// gets a fresh client, so none rides on another's kept-alive connection.
func TestEveryTransportSendsTheRecordedHello(t *testing.T) {
	rec := newFPRecorder(t)
	ctx := context.Background()
	creds := Creds{Token: "xoxc-fp", Cookie: "xoxd-fp", Team: "T1", TeamURL: "https://" + fpHost + "/"}
	client := func(desktop bool) *Client {
		cr := creds
		if desktop {
			cr.Session = SessionDesktop
		}
		c := New(cr)
		c.EdgeURL = "https://" + fpHost + "/edge/"
		return c
	}
	n := 0
	var out map[string]any
	steps := []struct {
		kind string
		run  func() error
	}{
		{"web API", func() error { return client(false).Call(ctx, "auth.test", nil, &out) }},
		{"web API, desktop session", func() error { return client(true).Call(ctx, "auth.test", nil, &out) }},
		{"edgeapi", func() error { return client(false).Edge(ctx, "users/info", map[string]any{}, &out) }},
		{"multipart", func() error {
			return client(false).CallMultipart(ctx, "files.upload", url.Values{}, "file", "a.txt", []byte("x"), &out)
		}},
		{"upload", func() error {
			return client(false).UploadBytes(ctx, "https://"+fpHost+"/upload", "a.txt", []byte("x"), nil)
		}},
		{"download", func() error {
			resp, err := client(false).Download(ctx, "https://"+fpHost+"/files-pri/T1-F1/a.txt")
			if err == nil {
				resp.Body.Close()
			}
			return err
		}},
		{"realtime websocket", func() error {
			r := client(false).NewRTM()
			r.reconnectURL = "wss://" + fpHost + "/ws"
			err := r.once(ctx)
			var ce websocket.CloseError
			if errors.As(err, &ce) && ce.Code == websocket.StatusNormalClosure {
				return nil // the server accepted, then closed
			}
			return err
		}},
		{"sign-in exchange", func() error {
			jar, _ := cookiejar.New(nil)
			resp, err := magicClient(jar).Get("https://" + fpHost + "/api/auth.loginMagicBulk")
			if err == nil {
				resp.Body.Close()
			}
			return err
		}},
		{"public CDN", func() error {
			_, _, err := GetNoCreds(ctx, "https://"+fpHost+"/a.png", 100)
			return err
		}},
	}
	for _, s := range steps {
		if err := s.run(); err != nil {
			t.Errorf("%s: %v", s.kind, err)
		}
		n = rec.check(t, s.kind, n)
	}
}

// The public transport's address guard still runs under TLS: a Slack
// name that resolves to loopback is refused before any handshake.
func TestPublicTransportGuardUnderTLS(t *testing.T) {
	srv := tlsfptest.NewServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), tlsfptest.NewCA(t).Leaf(t, "127.0.0.1"))
	_, err := (&http.Client{Transport: publicTransport}).Get(srv.URL + "/")
	if !errors.Is(err, ErrBlockedAddr) {
		t.Fatalf("loopback fetch: %v, want ErrBlockedAddr", err)
	}
	if len(srv.Hellos()) != 0 {
		t.Fatal("a ClientHello reached loopback")
	}
}

// The API transport honours HTTPS_PROXY (through its own CONNECT) and the
// public one never uses a proxy.
func TestTransportProxies(t *testing.T) {
	if p := reflect.ValueOf(apiDialer.Proxy).Pointer(); p != reflect.ValueOf(http.ProxyFromEnvironment).Pointer() {
		t.Error("the API dialer doesn't use HTTPS_PROXY")
	}
	if publicDialer.Proxy != nil {
		t.Error("the public dialer uses a proxy, which would bypass its address guard")
	}
	if publicTransport.Proxy != nil {
		if u, _ := publicTransport.Proxy(&http.Request{URL: &url.URL{Scheme: "http", Host: "a.slack-edge.com"}}); u != nil {
			t.Error("the public transport proxies plain http")
		}
	}
	tr := newTransport()
	if tr.DialTLSContext == nil || tr.TLSNextProto == nil || len(tr.TLSNextProto) != 0 || tr.ForceAttemptHTTP2 {
		t.Error("the API transport isn't tlsfp's HTTP/1.1 transport")
	}
}
