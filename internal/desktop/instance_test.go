package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestForwardToRunningInstance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("AF_UNIX path length on CI temp dirs")
	}
	dir, err := os.MkdirTemp("", "ci")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s.sock")
	if forward(path, Request{}) {
		t.Fatal("forwarded with nobody listening")
	}
	got := make(chan Request, 2)
	in, err := listen(path, func(r Request) { got <- r })
	if err != nil {
		t.Fatal(err)
	}
	if !forward(path, Request{URL: "slack://channel?team=T1&id=C1"}) {
		t.Fatal("not forwarded")
	}
	select {
	case r := <-got:
		if r.URL != "slack://channel?team=T1&id=C1" {
			t.Fatalf("got %q", r.URL)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler not called")
	}
	if _, err := listen(path, func(Request) {}); err == nil {
		t.Fatal("second listener on a live socket")
	}
	in.Close()

	// A socket file left behind by a crash is replaced.
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	in2, err := listen(path, func(r Request) { got <- r })
	if err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	defer in2.Close()
	if !forward(path, Request{}) {
		t.Fatal("not forwarded after replacing a stale socket")
	}
}

func TestURLArg(t *testing.T) {
	if u := URLArg([]string{"-dark", "slack://open?team=T1"}); u != "slack://open?team=T1" {
		t.Fatalf("got %q", u)
	}
	if u := URLArg([]string{"login"}); u != "" {
		t.Fatalf("got %q", u)
	}
}

func TestSocketPathPerConfigDir(t *testing.T) {
	if SocketPath("/a") == SocketPath("/b") {
		t.Fatal("same socket for different config dirs")
	}
	if p := SocketPath("/a"); len(p) > 100 {
		t.Fatalf("socket path too long for sun_path: %s", p)
	}
}
