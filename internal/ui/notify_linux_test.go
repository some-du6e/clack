//go:build linux

package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// TestToastLinuxBodyLikeSlack: on Linux the notification goes over D-Bus
// as Slack's Linux shell makes it (main.bundle.cjs 4.53.276's E5, the
// Linux build's gX, feeding Electron's Notification: libnotify passes the
// strings on as they are): the title capped (dP 1500/1700), the body
// capped (dP 1500/2000) and lodash unescaped, as toastTitle and toastBody
// make them for Windows and macOS. A clip line's message, which Slack's
// i18n escaped ("don&#39;t"), shows its apostrophe. Synthetic text.
func TestToastLinuxBodyLikeSlack(t *testing.T) {
	type sent struct {
		title, body string
		actions     []string
	}
	var got []sent
	n := linuxTestNotifier(func(title, body string, actions []string) (uint32, error) {
		got = append(got, sent{title, body, actions})
		return uint32(len(got)), nil
	})

	var m slack.Message
	if err := json.Unmarshal([]byte(`{"type":"message","ts":"1762520000.000100","user":"U0SYNTH0001","text":"don't &amp; &lt;go&gt;","files":[`+
		`{"id":"F0SYNTH0001","user":"U0SYNTH0001","name":"x.bin","title":"","mimetype":"audio/mp4","filetype":"m4a","pretty_type":"Binary","mode":"hosted","subtype":"slack_audio"}]}`), &m); err != nil {
		t.Fatal(err)
	}
	unknown := func(s richtext.Span) (string, bool) { return "", s.Kind == richtext.User }
	body := notifyText(heldNames{}, noteTestMessage(&m), unknown)
	if want := `sent an audio clip and a message, "don&#39;t &amp; &lt;go&gt;"`; body != want {
		t.Fatalf("the clip line: %q, Slack %q", body, want)
	}
	long := strings.Repeat("word ", 700) // 3500 units: past both caps
	for _, c := range []struct {
		title, body         string
		wantTitle, wantBody string
	}{
		{"New message from Ada", body, "New message from Ada", `sent an audio clip and a message, "don't & <go>"`},
		{"Q&amp;A", "Pat &amp; Sam O&#39;Neil &quot;x&quot;", "Q&amp;A", `Pat & Sam O'Neil "x"`},
		{long, long, (&desktopNote{Title: long}).toastTitle(), (&desktopNote{Body: long}).toastBody()},
	} {
		got = nil
		if err := n.Notify(c.title, c.body); err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].title != c.wantTitle || got[0].body != c.wantBody {
			t.Errorf("Notify(%.40q, %.40q): %+v, Slack's shell %.60q / %.60q", c.title, c.body, got, c.wantTitle, c.wantBody)
		}
	}
	if w := (&desktopNote{Body: long}).toastBody(); !strings.HasSuffix(w, "…") || len([]rune(w)) > 2000 {
		t.Fatalf("toastBody of %d units: %d runes, no cap", len(long), len([]rune(w)))
	}
	// a huddle's notification with buttons goes the same way
	got = nil
	if err := n.NotifyActions("Huddle", "Pat &amp; Sam invited you", []string{"join", "Join"}, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].body != "Pat & Sam invited you" || len(got[0].actions) != 2 {
		t.Errorf("NotifyActions: %+v, Slack's shell %q", got, "Pat & Sam invited you")
	}
}
