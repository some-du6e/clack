package ui

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slick"
)

// The confirmation shown before a Slick plugin file runs.
func TestSlickInstallText(t *testing.T) {
	title, body, action := slickInstallText(slick.Peek{ID: "HelloSlick", Name: "Hello Slick"}, "https://example.com/Hello.js")
	if title != "Install Hello Slick?" || action != "Install" {
		t.Fatalf("%q %q", title, action)
	}
	for _, want := range []string{"FULL ACCESS", "Only install code you trust!", "https://example.com/Hello.js", "every 3 hours", "runs the file's code"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q: %s", want, body)
		}
	}
	title, body, _ = slickInstallText(slick.Peek{ID: "ClearURLs", Name: "ClearURLs", Main: true, Installed: true, HasMain: true, Capabilities: []string{"net"}}, "")
	if title != "Install a privileged half?" || !strings.Contains(body, "privileged half (main.ts) of ClearURLs") || !strings.Contains(body, "can use net") || !strings.Contains(body, "FULL ACCESS") {
		t.Fatalf("main: %q %s", title, body)
	}
	title, _, action = slickInstallText(slick.Peek{ID: "X", Name: "X", Installed: true}, "")
	if title != "Update X?" || action != "Update" {
		t.Fatalf("update: %q %q", title, action)
	}
	_, body, _ = slickInstallText(slick.Peek{ID: "MessageLogger", Name: "Message Logger", StoresMessages: true}, "")
	if !strings.Contains(body, slick.StoresMessagesNote) {
		t.Fatalf("message-keeping plugin without the policy note: %s", body)
	}
	if slickCapabilities(nil) != "declares no capabilities" || slickCapabilities([]string{"net", "shell"}) != "can use net, shell" {
		t.Fatal("slickCapabilities")
	}
}
