package slack

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type canvasHit struct {
	host, path, query, ctype, origin, referer, ua, cookie, body string
	method                                                      string
}

func canvasServer(t *testing.T, answer func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, func() []canvasHit) {
	t.Helper()
	var mu sync.Mutex
	var hits []canvasHit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		hits = append(hits, canvasHit{host: r.Host, path: r.URL.Path, query: r.URL.RawQuery, ctype: r.Header.Get("Content-Type"),
			origin: r.Header.Get("Origin"), referer: r.Header.Get("Referer"), ua: r.Header.Get("User-Agent"),
			cookie: r.Header.Get("Cookie"), body: string(b), method: r.Method})
		mu.Unlock()
		answer(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []canvasHit {
		mu.Lock()
		defer mu.Unlock()
		return append([]canvasHit(nil), hits...)
	}
}

func TestCanvasCallIsSlacksRequest(t *testing.T) {
	answer := []byte{0x12, 0x02, 0x08, 0x01, 0x7b} // binary, with a "{" in it
	team, teamHits := canvasServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(answer)
	})
	ent, entHits := canvasServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(answer)
	})
	form := []CanvasField{{"thread", "ABCdEfGhIjK"}, {"document", "ABC9"}, {"retry_count", "0"}, {"session", "0123456789ab"},
		{"sequence", "3"}, {"data_binary", "Cgt+/w=="}, {"title", "Plan & café (1) *x* 'y' ~z!"}}

	// in an org: the enterprise host and token (XgZX.Kc's top-level team)
	c := New(Creds{Token: "xoxc-team", Cookie: "xoxd-c", EnterpriseToken: "xoxc-org", EnterpriseID: "E1", Team: "T1",
		TeamURL: team.URL + "/", EnterpriseURL: ent.URL + "/"})
	got, err := c.CanvasCall(context.Background(), "edit-document", form)
	if err != nil || string(got) != string(answer) {
		t.Fatalf("CanvasCall = %x, %v", got, err)
	}
	if n := len(teamHits()); n != 0 {
		t.Errorf("%d calls went to the team host", n)
	}
	h := entHits()
	if len(h) != 1 {
		t.Fatalf("%d calls to the enterprise host", len(h))
	}
	want := "thread=ABCdEfGhIjK&document=ABC9&retry_count=0&session=0123456789ab&sequence=3&data_binary=Cgt%2B%2Fw%3D%3D" +
		"&title=Plan%20%26%20caf%C3%A9%20(1)%20*x*%20'y'%20~z!&token=xoxc-org"
	switch hit := h[0]; {
	case hit.method != http.MethodPost || hit.path != "/canvas/-/edit-document" || hit.query != "_x_version_ts=noversion":
		t.Errorf("%s %s?%s", hit.method, hit.path, hit.query)
	case hit.ctype != "application/x-www-form-urlencoded":
		t.Errorf("content type %q", hit.ctype)
	case hit.body != want:
		t.Errorf("body\n got %s\nwant %s", hit.body, want)
	case hit.origin != "" || hit.referer != "":
		t.Errorf("Origin %q, Referer %q: Clack's honest headers send neither", hit.origin, hit.referer)
	case hit.ua != UserAgent():
		t.Errorf("User-Agent %q, want Clack's %q", hit.ua, UserAgent())
	case !strings.Contains(hit.cookie, "d=xoxd-c"):
		t.Errorf("cookie %q", hit.cookie)
	}

	// a workspace outside an org: the team's
	c = New(Creds{Token: "xoxc-team", Cookie: "xoxd-c", Team: "T1", TeamURL: team.URL + "/"})
	if _, err := c.CanvasCall(context.Background(), "load-data", []CanvasField{{"request_binary", "CgA="}}); err != nil {
		t.Fatal(err)
	}
	if h := teamHits(); len(h) != 1 || h[0].path != "/canvas/-/load-data" || h[0].body != "request_binary=CgA%3D&token=xoxc-team" {
		t.Errorf("team call %+v", h)
	}
}

func TestCanvasCallRefuses(t *testing.T) {
	srv, hits := canvasServer(t, func(w http.ResponseWriter, r *http.Request) {})
	c := New(Creds{Token: "t", Cookie: "d", Team: "T1", TeamURL: srv.URL + "/"})
	for _, p := range []string{"run-document-fixers", "save-diagnostics-report", "../api/auth.signout", "load-data/x", ""} {
		if _, err := c.CanvasCall(context.Background(), p, nil); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("CanvasCall(%q) = %v, want ErrNotAllowed", p, err)
		}
	}
	c.Allow = func(method string) bool { return method != CanvasMethod("edit-document") }
	if _, err := c.CanvasCall(context.Background(), "edit-document", nil); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("a client that doesn't allow it: %v", err)
	}
	if n := len(hits()); n != 0 {
		t.Errorf("%d refused calls went out", n)
	}
}

func TestCanvasCallErrors(t *testing.T) {
	mode := ""
	srv, _ := canvasServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":false,"error":"not_allowed_token_type"}`))
		case "json body":
			w.Write([]byte(`{"ok":false,"error":"ratelimited"}`))
		case "json ok":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Write([]byte(`{"ok":true}`))
		case "auth":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
		case "409":
			w.WriteHeader(http.StatusConflict)
		case "brace":
			w.Write([]byte("{\x08\x01|")) // a group start for field 15: protobuf, not JSON
		}
	})
	c := New(Creds{Token: "t", Cookie: "d", Team: "T1", TeamURL: srv.URL + "/"})
	call := func(m string) ([]byte, error) {
		mode = m
		return c.CanvasCall(context.Background(), "fetch-latest", []CanvasField{{"request_binary", m}})
	}
	var se *Error
	if _, err := call("json"); !errors.As(err, &se) || se.Code != "not_allowed_token_type" || se.Method != "canvas/fetch-latest" {
		t.Errorf("JSON error: %v", err)
	}
	if _, err := call("json body"); !errors.As(err, &se) || se.Code != "ratelimited" {
		t.Errorf("JSON error without its content type: %v", err)
	}
	var pe *ParseError
	if _, err := call("json ok"); !errors.As(err, &pe) {
		t.Errorf("a JSON answer that isn't an error: %v", err)
	}
	var he *HTTPError
	if _, err := call("409"); !errors.As(err, &he) || he.Status != http.StatusConflict {
		t.Errorf("409: %v", err)
	}
	if b, err := call("brace"); err != nil || string(b) != "{\x08\x01|" {
		t.Errorf("protobuf starting with {: %q, %v", b, err)
	}
	if _, err := call("auth"); !IsAuthError(err) || !c.SignedOut() {
		t.Errorf("invalid_auth: %v, signed out %v", err, c.SignedOut())
	}
	if _, err := call("brace"); !errors.Is(err, ErrSignedOut) {
		t.Errorf("after signing out: %v", err)
	}
}

func TestEncodeURIComponent(t *testing.T) {
	for in, want := range map[string]string{
		"AZaz09-_.!~*'()": "AZaz09-_.!~*'()",
		" +/=&?#%":        "%20%2B%2F%3D%26%3F%23%25",
		"café😀":           "caf%C3%A9%F0%9F%98%80",
		"":                "",
	} {
		if got := encodeURIComponent(in); got != want {
			t.Errorf("encodeURIComponent(%q) = %q, want %q", in, got, want)
		}
	}
}

// A canvas answer past quip's cap is refused, read one byte past it and
// no further: nothing could decode it (the review of B1: the body was
// read whole, as every Web API answer is).
func TestCanvasCallAnswerCap(t *testing.T) {
	answer := func(n int) func(w http.ResponseWriter, r *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			chunk := make([]byte, 1<<20)
			for n > 0 {
				k := min(n, len(chunk))
				if _, err := w.Write(chunk[:k]); err != nil {
					return
				}
				n -= k
			}
		}
	}
	big, _ := canvasServer(t, answer(MaxCanvasAnswer+(4<<20)))
	c := New(Creds{Token: "t", Cookie: "d", Team: "T1", TeamURL: big.URL + "/"})
	if b, err := c.CanvasCall(context.Background(), "load-data", []CanvasField{{"request_binary", "big"}}); err == nil || b != nil {
		t.Errorf("an answer 4 MiB over the cap: %d bytes, %v", len(b), err)
	}
	// the control: an answer at the cap is read whole
	at, _ := canvasServer(t, answer(MaxCanvasAnswer))
	c2 := New(Creds{Token: "t", Cookie: "d", Team: "T1", TeamURL: at.URL + "/"})
	if b, err := c2.CanvasCall(context.Background(), "load-data", []CanvasField{{"request_binary", "at"}}); err != nil || len(b) != MaxCanvasAnswer {
		t.Errorf("an answer at the cap: %d bytes, %v", len(b), err)
	}
}
