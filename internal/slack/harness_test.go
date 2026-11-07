package slack

import "testing"

func TestLoopbackRefusedOutsideHarness(t *testing.T) {
	if harnessLoopback {
		t.Skip("perfharness build")
	}
	if slackURL("ws://127.0.0.1:8930/ws", "wss") || CheckCredsHosts(Creds{TeamURL: "http://127.0.0.1:8930/"}) == nil {
		t.Fatal("release builds must refuse loopback Slack hosts")
	}
}

func TestHarnessAcceptsLoopbackOnly(t *testing.T) {
	old := harnessLoopback
	harnessLoopback = true
	defer func() { harnessLoopback = old }()
	if !slackURL("ws://127.0.0.1:8930/ws", "wss") || CheckCredsHosts(Creds{TeamURL: "http://localhost:1/"}) != nil {
		t.Fatal("harness should accept loopback")
	}
	if slackURL("ws://example.com/ws", "wss") || slackURL("ftp://127.0.0.1/", "https") {
		t.Fatal("harness must still refuse non-loopback hosts and other schemes")
	}
}
