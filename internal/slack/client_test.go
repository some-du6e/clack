package slack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Synthetic: a fake Slack with a team and an enterprise host.
func TestAdaptiveRouting(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		host := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0] // team | ent
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		mu.Lock()
		seen = append(seen, host+":"+method+":"+r.PostForm.Get("token"))
		mu.Unlock()
		switch {
		case method == "org.only" && host == "team":
			w.Write([]byte(`{"ok":false,"error":"team_is_restricted"}`))
		case method == "team.only" && host == "ent":
			w.Write([]byte(`{"ok":false,"error":"enterprise_is_restricted"}`))
		default:
			w.Write([]byte(`{"ok":true,"value":"` + host + `"}`))
		}
	}))
	defer srv.Close()
	c := New(Creds{Token: "team-tok", EnterpriseToken: "ent-tok", EnterpriseID: "E1", TeamURL: srv.URL + "/team/", EnterpriseURL: srv.URL + "/ent/"})
	ctx := context.Background()
	var out struct {
		Value string `json:"value"`
	}
	call := func(m string) string {
		out.Value = ""
		if err := c.Call(ctx, m, nil, &out); err != nil {
			t.Fatalf("%s: %v", m, err)
		}
		return out.Value
	}
	if call("org.only") != "ent" || call("org.only") != "ent" {
		t.Error("org.only not routed to the enterprise host")
	}
	if call("client.counts") != "ent" { // static entMethods
		t.Error("client.counts not on the enterprise host")
	}
	if call("team.only") != "team" || call("plain.method") != "team" {
		t.Error("team methods not on the team host")
	}
	mu.Lock()
	got := strings.Join(seen, " ")
	mu.Unlock()
	// The first org.only call falls back once; the second goes straight to ent.
	want := "team:org.only:team-tok ent:org.only:ent-tok ent:org.only:ent-tok ent:client.counts:ent-tok team:team.only:team-tok team:plain.method:team-tok"
	if got != want {
		t.Errorf("calls:\n got %s\nwant %s", got, want)
	}
}

func TestWebSocketURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true,"primary_websocket_url":"wss://wss-primary-x.slack.com/","fallback_websocket_url":"wss://wss-backup-x.slack.com/","routing_context":"TE1-2"}`))
	}))
	defer srv.Close()
	c := New(Creds{Token: "team-tok", EnterpriseToken: "ent-tok", EnterpriseID: "E1", Team: "T1", TeamURL: srv.URL + "/", EnterpriseURL: srv.URL + "/"})
	u := c.WebSocketURL(context.Background(), false)
	for _, want := range []string{"wss://wss-primary-x.slack.com/?", "token=ent-tok", "gateway_server=TE1-2", "enterprise_id=E1", "flannel=3"} {
		if !strings.Contains(u, want) {
			t.Errorf("ws url missing %q: %s", want, u)
		}
	}
	if strings.Contains(u, "agent_version%3D1%26") {
		t.Error("agent_version=1 makes the gateway drop the socket")
	}
	if b := c.WebSocketURL(context.Background(), true); !strings.HasPrefix(b, "wss://wss-backup-x.slack.com/?") {
		t.Errorf("fallback url: %s", b)
	}
}

func TestCallMultipart(t *testing.T) {
	var got struct {
		token, mode, name, file, fname string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("not multipart: %v", err)
		}
		got.token, got.mode, got.name = r.FormValue("token"), r.FormValue("mode"), r.FormValue("name")
		if f, h, err := r.FormFile("image"); err == nil {
			b := make([]byte, 16)
			n, _ := f.Read(b)
			got.file, got.fname = string(b[:n]), h.Filename
		}
		w.Write([]byte(`{"ok":false,"error":"error_name_taken"}`))
	}))
	defer srv.Close()
	c := New(Creds{Token: "team-tok", TeamURL: srv.URL + "/"})
	err := c.CallMultipart(context.Background(), "emoji.add", map[string][]string{"mode": {"data"}, "name": {"parrot"}}, "image", "parrot.png", []byte("PNGDATA"), nil)
	var se *Error
	if !errors.As(err, &se) || se.Code != "error_name_taken" {
		t.Errorf("error = %v", err)
	}
	if got.token != "team-tok" || got.mode != "data" || got.name != "parrot" || got.file != "PNGDATA" || got.fname != "parrot.png" {
		t.Errorf("multipart fields: %+v", got)
	}
}
