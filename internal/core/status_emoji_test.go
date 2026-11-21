package core

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// The cases' expected values are Slack's own: ue25, iz33 and pH4l run in
// node over the same map (build 35b42de8b870; harness in
// clack-ops/scratch/profile-role-band/harness/oracle.cjs).
var seMap = map[string]StatusEmojiDisplay{
	"ping-9+":        {URL: "U/ping"},
	"speech_balloon": {Unicode: "1f4ac"},
	"wave":           {Alias: "hand", Unicode: "1f44b"},
}

func TestEmojiDisplayInfoLikeSlack(t *testing.T) {
	for _, c := range []struct{ code, name, url string }{
		{":speech_balloon:", ":speech_balloon:", ""},
		{":ping-9+:", ":ping-9+:", "U/ping"},
		{":wave::skin-tone-3:", ":hand::skin-tone-3:", ""},
		{":Wave:", ":hand:", ""},
		{":nope:", ":nope:", ""},
		{"plain", "plain", ""},
		{":a:b:", ":a:b:", ""},
		{":wave::skin-tone-7:", ":wave::skin-tone-7:", ""},
	} {
		name, url := EmojiDisplayInfo(c.code, seMap)
		if name != c.name || url != c.url {
			t.Errorf("%q: (%q, %q), want Slack's (%q, %q)", c.code, name, url, c.name, c.url)
		}
	}
}

func TestStatusTextPartsLikeSlack(t *testing.T) {
	for _, c := range []struct {
		text string
		want []StatusTextPart
	}{
		{":ping-9+: Absolutely DM me", []StatusTextPart{{Code: ":ping-9+:", URL: "U/ping"}, {Text: " Absolutely DM me"}}},
		{":wave::skin-tone-2: hi :x: and :ü_ok:", []StatusTextPart{{Code: ":hand::skin-tone-2:"}, {Text: " hi "}, {Code: ":x:"}, {Text: " and "}, {Code: ":ü_ok:"}}},
		{"no codes", []StatusTextPart{{Text: "no codes"}}},
		{"a :b c: d:e:", []StatusTextPart{{Text: "a :b c: d"}, {Code: ":e:"}}},
	} {
		if got := StatusTextParts(c.text, seMap); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %+v, want Slack's %+v", c.text, got, c.want)
		}
	}
}

// Karim's eps (2026-11-21): status_emoji :speech_balloon: and a display
// info whose first entry is the text's custom :ping-9+:. The status emoji
// is 💬, by name.
func TestUserStatusEmojiByName(t *testing.T) {
	a, _ := testApp(t)
	di, _ := json.Marshal([]map[string]any{
		{"emoji_name": "ping-9+", "display_url": "https://emoji.example/ping.png"},
		{"emoji_name": "speech_balloon", "unicode": "1f4ac"},
	})
	a.S.PutUsers([]slack.User{{ID: "U0STATUS01", Name: "eps", RealName: "Top Level",
		Profile: slack.Profile{StatusEmoji: ":speech_balloon:", StatusText: ":ping-9+: Absolutely", StatusEmojiDisplay: di}}})
	u, ok := a.User("U0STATUS01")
	if !ok {
		t.Fatal("no user")
	}
	if u.StatusEmoji != ":speech_balloon:" || u.StatusEmojiURL == "https://emoji.example/ping.png" {
		t.Errorf("status emoji %q (%q), want :speech_balloon: drawn as itself", u.StatusEmoji, u.StatusEmojiURL)
	}
	if u.StatusEmojiInfo["ping-9+"].URL != "https://emoji.example/ping.png" {
		t.Errorf("display info %+v", u.StatusEmojiInfo)
	}
	// getMemberRealName: profile.real_name, else the member's real_name
	if u.RealName != "Top Level" {
		t.Errorf("real name %q, want the member's real_name", u.RealName)
	}
}
