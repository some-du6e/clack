package popups

import "testing"

// isLinkSuspicious's cases: text naming another host, mentions-looking
// text off Slack, look-alike characters; same host or plain words are fine.
func TestLinkSuspicious(t *testing.T) {
	for _, c := range []struct {
		text, url string
		want      bool
	}{
		{"https://example.com", "https://example.com", false},
		{"example.com", "https://www.example.com/a", false},
		{"paypal.com", "https://evil.test/login", true},
		{"https://bank.com/login", "https://phish.example/x", true},
		{"click here", "https://evil.test", false},
		{"@channel", "https://evil.test", true},
		{"#general", "https://hackclub.slack.com/archives/C1", false},
		{"bank​.com", "https://bank.com", true},
		{"mail me at a@b.com", "https://evil.test", false},
		{"", "https://evil.test", false},
		{"https://ok.test/@evil.test", "https://ok.test/@evil.test", true},
	} {
		if got := LinkSuspicious(c.text, c.url); got != c.want {
			t.Errorf("LinkSuspicious(%q, %q) = %v, want %v", c.text, c.url, got, c.want)
		}
	}
}
