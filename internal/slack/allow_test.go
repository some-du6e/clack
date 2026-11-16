package slack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// Client.Allow: a method it doesn't allow (Web API, edge, or a file
// upload) never reaches the server; an allowed one does.
func TestClientAllow(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := New(Creds{Token: "xoxc-t", TeamURL: srv.URL + "/", EnterpriseURL: srv.URL + "/"})
	c.EdgeURL = srv.URL + "/edge/"
	c.Allow = func(m string) bool { return m == "users.info" }
	ctx := context.Background()
	if err := c.Call(ctx, "users.prefs.set", nil, nil); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("users.prefs.set: %v, want ErrNotAllowed", err)
	}
	if err := c.Edge(ctx, "users/info", map[string]any{}, nil); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("edge users/info: %v, want ErrNotAllowed", err)
	}
	if err := c.UploadBytes(ctx, srv.URL+"/upload", "a.txt", []byte("x"), nil); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("upload: %v, want ErrNotAllowed", err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("%d refused requests reached the server", n)
	}
	if err := c.Call(ctx, "users.info", nil, nil); err != nil {
		t.Errorf("users.info: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("the allowed call made %d requests, want 1", n)
	}
}
