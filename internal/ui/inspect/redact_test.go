package inspect

import (
	"regexp"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

var tagRE = regexp.MustCompile(`^[a-z]+:[0-9a-f]{6}$`)

// Within one report an id always has the same tag, its kind from the id
// itself; different ids have different tags.
func TestTagsStablePerReport(t *testing.T) {
	r := NewRedactor()
	a, b := r.ID("C0B4DEFKFCJ"), r.ID("C0B4DEFKFCJ")
	if a != b {
		t.Errorf("one channel, two tags: %s %s", a, b)
	}
	if !tagRE.MatchString(a) || !strings.HasPrefix(a, "ch:") {
		t.Errorf("channel tag %q, want ch:<6 hex>", a)
	}
	for id, kind := range map[string]string{
		"U082DRXPMF1": "user", "W0123456789": "user", "D0123ABCDEF": "dm", "B01ABCDEFGH": "bot",
		"T0FAKE12345": "team", "F0123456789": "file", "1794632524.647929": "ts", "general": "id",
	} {
		if got := r.ID(id); !strings.HasPrefix(got, kind+":") || !tagRE.MatchString(got) {
			t.Errorf("ID(%q) = %q, want a %s: tag", id, got, kind)
		}
	}
	if r.ID("C0B4DEFKFCJ") == r.ID("C0B4DEFKFCK") {
		t.Error("two channels share a tag")
	}
	if r.ID("") != "" {
		t.Error("an empty id has a tag")
	}
	// an id gets one tag wherever it appears: as channelId, in a message,
	// in the route
	p := slackc.Props{"channelId": "C0B4DEFKFCJ", "msg": slackc.Props{"channel": "C0B4DEFKFCJ"}}
	ids := strings.Join(r.PropIDs(p), " ")
	if strings.Count(ids, a) != 2 || !strings.Contains(r.Route("c/C0B4DEFKFCJ"), a) {
		t.Errorf("the channel's tag %s isn't the same in props %q and the route %q", a, ids, r.Route("c/C0B4DEFKFCJ"))
	}
}

// Another report, another salt: the same id has an unrelated tag, so tags
// can't be matched up across reports or reversed.
func TestSaltPerReport(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		seen[NewRedactor().ID("U082DRXPMF1")] = true
	}
	if len(seen) < 7 {
		t.Errorf("8 reports gave the same user %d distinct tags, want 8 (barring a 24-bit collision)", len(seen))
	}
}

// A route keeps its page and loses every id and search query.
func TestRouteRedacted(t *testing.T) {
	r := NewRedactor()
	for _, c := range []struct{ in, prefix string }{
		{"c/C0B4DEFKFCJ", "c/ch:"},
		{"c/C0B4DEFKFCJ/1794632524.647929", "c/ch:"},
		{"search/karim's secret plans", "search/q:"},
		{"search?q=secret", "search?q:"},
		{"dms", "dms"},
		{"bbm/U082DRXPMF1", "bbm/user:"},
	} {
		got := r.Route(c.in)
		if !strings.HasPrefix(got, c.prefix) {
			t.Errorf("Route(%q) = %q, want it to start %q", c.in, got, c.prefix)
		}
		for _, leak := range []string{"C0B4DEFKFCJ", "1794632524", "secret", "U082DRXPMF1"} {
			if strings.Contains(got, leak) {
				t.Errorf("Route(%q) = %q keeps %q", c.in, got, leak)
			}
		}
	}
}

// Clack's route words stay where they are, between the tags.
func TestRouteKeepsWords(t *testing.T) {
	r := NewRedactor()
	got := r.Route("c/C0B4DEFKFCJ/t/1794632524.647929")
	if want := "c/" + r.ID("C0B4DEFKFCJ") + "/t/" + r.ID("1794632524.647929"); got != want {
		t.Errorf("thread route: %q, want %q", got, want)
	}
	if got := r.Route("directories/people"); got != "directories/people" {
		t.Errorf("directories tab: %q", got)
	}
	if got := r.Route("Karim's page"); strings.Contains(got, "Karim") {
		t.Errorf("an unknown page kept: %q", got)
	}
}

// Props give ids as tags and Slack's enumerations as they are; text,
// names and anything else in them never come out.
func TestPropIDsKeepNoContent(t *testing.T) {
	r := NewRedactor()
	p := slackc.Props{
		"$key":      "1794632524.647929",
		"channelId": "C0B4DEFKFCJ",
		"userId":    "U082DRXPMF1",
		"className": "c-message_kit__avatar",
		"msg": slackc.Props{
			"type": "message", "subtype": "bot_message", "ts": "1794632524.647929", "user": "U082DRXPMF1", "channel": "C0B4DEFKFCJ",
			"text": "the launch is moved to friday", "username": "Karim the Great", "client_msg_id": "abc-123",
			"blocks": []any{map[string]any{"type": "rich_text"}},
		},
		"user":       map[string]any{"real_name": "Karim", "id": "U082DRXPMF1"},
		"showCard":   true,
		"topic":      "secret topic",
		"emailField": "karim@example.com",
	}
	got := r.PropIDs(p)
	joined := strings.Join(got, " ")
	for _, leak := range []string{"launch", "friday", "Karim", "Great", "abc-123", "secret", "example.com", "C0B4DEFKFCJ", "U082DRXPMF1", "1794632524"} {
		if strings.Contains(joined, leak) {
			t.Errorf("PropIDs kept %q: %v", leak, got)
		}
	}
	want := map[string]bool{
		"$key=" + r.ID("1794632524.647929"): true, "channelId=" + r.ID("C0B4DEFKFCJ"): true, "userId=" + r.ID("U082DRXPMF1"): true,
		"msg.type=message": true, "msg.subtype=bot_message": true, "msg.ts=" + r.ID("1794632524.647929"): true,
		"msg.user=" + r.ID("U082DRXPMF1"): true, "msg.channel=" + r.ID("C0B4DEFKFCJ"): true, "showCard=true": true,
	}
	for _, s := range got {
		if !want[s] {
			t.Errorf("unexpected %q", s)
		}
		delete(want, s)
	}
	for s := range want {
		t.Errorf("missing %q in %v", s, got)
	}
	if ch := PropChannels(p); len(ch) != 2 || ch[0] != "C0B4DEFKFCJ" {
		t.Errorf("PropChannels = %v, want the channel twice (props and message)", ch)
	}
}

// A run-time attribute keeps an enumeration (data-qa) and tags the rest.
func TestAttrRedacted(t *testing.T) {
	r := NewRedactor()
	if got := r.Attr("data-qa", "add_reaction"); got != "add_reaction" {
		t.Errorf("data-qa enumeration redacted: %q", got)
	}
	if got := r.Attr("data-qa-channel-sidebar-channel-id", "C0B4DEFKFCJ"); got != r.ID("C0B4DEFKFCJ") {
		t.Errorf("a channel id attribute: %q", got)
	}
	if got := r.Attr("data-item-key", "general"); got == "general" {
		t.Errorf("an item key kept as is: %q", got)
	}
	if got := r.Attr("data-qa", "Karim's thing"); got == "Karim's thing" {
		t.Errorf("free text in data-qa kept: %q", got)
	}
}
