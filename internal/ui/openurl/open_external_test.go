package openurl

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// A link Slack's openExternal refuses is a log line with its scheme, never
// the link itself, and nothing for the caller to show.
func TestOpenExternalRefusedIsLogged(t *testing.T) {
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() { log.SetOutput(prev); log.SetFlags(flags) }()
	OpenExternal("javascript:alert(document.cookie)")
	got := buf.String()
	if !strings.Contains(got, `refusing to open a "javascript" link`) {
		t.Errorf("log %q", got)
	}
	if strings.Contains(got, "cookie") {
		t.Errorf("the log has the link: %q", got)
	}
}
