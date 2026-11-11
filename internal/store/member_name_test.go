package store

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// MemberName is Slack's getPrefCompliantMemberName (client-boot vUCu):
// the display name, the real name under display_real_names or without a
// display name, never the username, nothing trimmed (a lone zero-width
// space real name stays one), and "Name hidden" for someone in
// hidden_users. Synthetic users.
func TestMemberName(t *testing.T) {
	const zwsp = "​"
	s := New("")
	s.PutUsers([]slack.User{
		{ID: "U1", Name: "handle1", RealName: zwsp, Profile: slack.Profile{DisplayName: "kay", RealName: zwsp, Image72: "https://avatars.test/U1-72.png"}},
		{ID: "U2", Name: "handle2", RealName: "Robin Example", Profile: slack.Profile{RealName: "Robin Example"}},
		{ID: "U3", Name: "handle3", RealName: "Sam Legacy"},
		{ID: "U4", Name: "handle4"},
	})
	want := func(id, name string) {
		t.Helper()
		got, ok := s.MemberName(id)
		if !ok || got != name {
			t.Errorf("%s: %q (known %v), want %q", id, got, ok, name)
		}
	}
	want("U1", "kay")
	want("U2", "Robin Example")
	want("U3", "Sam Legacy") // getMemberRealName falls back to the top-level real_name
	want("U4", "")           // no username fallback (DisplayName has one)
	if DisplayName(s.Users["U4"]) != "handle4" {
		t.Error("control: DisplayName falls back to the username")
	}
	if _, ok := s.MemberName("U404"); ok {
		t.Error("an unknown member reported as known")
	}

	s.SetPrefs(Prefs{DisplayRealNames: true}, nil)
	want("U1", zwsp)
	want("U2", "Robin Example")

	s.SetPrefs(Prefs{}, nil)
	s.SetRawPrefs(map[string]json.RawMessage{"hidden_users": json.RawMessage(`"U9,U2"`)})
	want("U2", "Name hidden")
	want("U1", "kay")

	if got := s.MemberAvatar("U1"); got != "https://avatars.test/U1-72.png" {
		t.Errorf("avatar %q, want the 72px picture message rows draw", got)
	}
	if got := s.MemberAvatar("U404"); got != "" {
		t.Errorf("unknown member's avatar %q", got)
	}
}
