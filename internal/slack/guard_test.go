package slack

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Synthetic: a fake Slack that counts requests.

func TestAuthFailureStopsAllTraffic(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	}))
	defer srv.Close()
	c := New(Creds{Token: "t", TeamURL: srv.URL + "/"})
	fired := make(chan string, 4)
	c.OnAuthFailed(func(code string) { fired <- code })
	err := c.Call(context.Background(), "conversations.history", nil, nil)
	if !IsAuthError(err) {
		t.Fatalf("first call: %v", err)
	}
	for i := 0; i < 20; i++ {
		if err := c.Call(context.Background(), "conversations.history", nil, nil); !errors.Is(err, ErrSignedOut) {
			t.Fatalf("call after sign-out: %v", err)
		}
		_ = c.Edge(context.Background(), "users/info", map[string]any{}, nil)
	}
	if _, err := c.Download(context.Background(), srv.URL+"/x"); !errors.Is(err, ErrSignedOut) {
		t.Errorf("download after sign-out: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("%d requests reached Slack, want 1", n)
	}
	select {
	case code := <-fired:
		if code != "invalid_auth" {
			t.Errorf("code %q", code)
		}
	case <-time.After(time.Second):
		t.Fatal("OnAuthFailed not called")
	}
	select {
	case <-fired:
		t.Error("OnAuthFailed called twice")
	case <-time.After(50 * time.Millisecond):
	}
	if !c.SignedOut() {
		t.Error("SignedOut false")
	}
	// The realtime loop must not even try to connect.
	r := c.NewRTM()
	done := make(chan struct{})
	go func() { r.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("RTM.Run kept running after sign-out")
	}
}

func TestBackoffOnRepeatedFailures(t *testing.T) {
	oldBase, oldMax := BackoffBase, BackoffMax
	BackoffBase, BackoffMax = 40*time.Millisecond, 120*time.Millisecond
	defer func() { BackoffBase, BackoffMax = oldBase, oldMax }()
	var hits atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if fail.Load() {
			w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := New(Creds{Token: "t", TeamURL: srv.URL + "/"})
	ctx := context.Background()
	call := func() error { return c.Call(ctx, "conversations.history", map[string][]string{"channel": {"C1"}}, nil) }
	for i := 0; i < 50; i++ { // a tight loop: only the first attempt goes out
		if err := call(); err == nil {
			t.Fatal("expected error")
		}
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("tight loop sent %d requests, want 1", n)
	}
	// A different request is not blocked.
	_ = c.Call(ctx, "conversations.history", map[string][]string{"channel": {"C2"}}, nil)
	if n := hits.Load(); n != 2 {
		t.Errorf("other request blocked: %d", n)
	}
	time.Sleep(50 * time.Millisecond) // past the first window (40 ms)
	_ = call()
	if n := hits.Load(); n != 3 {
		t.Errorf("after the window: %d requests, want 3", n)
	}
	_ = call() // second failure doubled the window (80 ms): blocked now
	if n := hits.Load(); n != 3 {
		t.Errorf("retried inside the doubled window: %d", n)
	}
	time.Sleep(130 * time.Millisecond) // capped at 120 ms
	fail.Store(false)
	if err := call(); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	fail.Store(true)
	_ = call() // success reset the state: this failure goes out
	if n := hits.Load(); n != 5 {
		t.Errorf("after success: %d requests, want 5", n)
	}
	if c.SignedOut() {
		t.Error("non-auth errors must not sign out")
	}
}

func TestMessageKeepsRawFields(t *testing.T) {
	var m Message
	raw := `{"ts":"1.0","text":"x","metadata":{"event_type":"relay","event_payload":{"user":"U9"}},
		"attachments":[{"id":1,"fallback":"f","extra_field":{"keep":true}}],
		"bot_profile":{"id":"B1","name":"relay","app_id":"A1","user_id":"U8","team_id":"T1"}}`
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if string(m.Metadata) == "" || len(m.Attachments) != 1 || m.Attachments[0].Fallback != "f" || m.BotProfile.UserID != "U8" {
		t.Errorf("decoded: %+v", m)
	}
	var att []map[string]any
	_ = json.Unmarshal(m.RawAttachments, &att)
	if len(att) != 1 || att[0]["extra_field"] == nil {
		t.Errorf("raw attachments lost fields: %s", m.RawAttachments)
	}
	var bad Message
	if err := json.Unmarshal([]byte(`{"ts":"2.0","attachments":"oops"}`), &bad); err != nil || bad.TS != "2.0" {
		t.Errorf("malformed attachments failed the message: %v", err)
	}
}

func TestNoBackoffForUserRetries(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`{"ok":false,"error":"msg_too_long"}`))
	}))
	defer srv.Close()
	c := New(Creds{Token: "t", TeamURL: srv.URL + "/"})
	_ = c.Call(context.Background(), "chat.postMessage", nil, nil)
	_ = c.Call(NoBackoff(context.Background()), "chat.postMessage", nil, nil)
	if n := hits.Load(); n != 2 {
		t.Errorf("user retry was backed off: %d", n)
	}
}
