package store

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/slackasset"
)

// getMemberProfileImage (client-boot 6Knt): memberId "USLACK" is the
// bundled slack_logo_mark at every size, whatever its profile carries:
// users.info's image_* SVGs, or (a member that came without them) only
// the avatar_hash whose ca.slack-edge.com picture is the generic blue
// person Karim saw in the DM's header (2026-11-21, α.113).
func TestAvatarURLSlackSystemUser(t *testing.T) {
	svg := "https://a.slack-edge.com/9cc0056/img/avatars/slack_system_user/slack_logo_mark.svg"
	full := &slack.User{ID: "USLACK", Name: "slack", RealName: "Slack", TeamID: "T0266FRGM", Color: "757575"}
	full.Profile.AvatarHash = "svb051040db2"
	full.Profile.Image48, full.Profile.Image72, full.Profile.Image192, full.Profile.Image512 = svg, svg, svg, svg
	bare := &slack.User{ID: "USLACK", TeamID: "T0266FRGM"}
	bare.Profile.AvatarHash = "svb051040db2"
	for _, u := range []*slack.User{full, bare, {ID: "USLACK"}} {
		for _, px := range []int{16, 20, 24, 32, 36, 48, 72, 192, 512, 1024} {
			if got := AvatarURL(u, px, "T0266FRGM"); got != slackasset.SlackLogoMark {
				t.Errorf("%+v at %d: %q, want %q", u.Profile, px, got, slackasset.SlackLogoMark)
			}
		}
	}
	// richtext's AvatarResolver (unfurl cards, mention chips)
	st := New("")
	st.PutUsers([]slack.User{*full})
	if got := st.UserAvatar("USLACK"); got != slackasset.SlackLogoMark {
		t.Errorf("UserAvatar(USLACK) = %q, want the logo", got)
	}
	// anyone else keeps their own picture
	o := &slack.User{ID: "U0SLACK0001", TeamID: "T0266FRGM"}
	o.Profile.AvatarHash = "abc"
	if got := AvatarURL(o, 72, "T0266FRGM"); got != "https://ca.slack-edge.com/T0266FRGM-U0SLACK0001-abc-72" {
		t.Fatalf("another member: %q", got)
	}
	if b, ok := slackasset.Get(slackasset.SlackLogoMark); !ok || len(b) != 883 {
		t.Fatalf("bundled logo: %d bytes, ok %v", len(b), ok)
	}
}
