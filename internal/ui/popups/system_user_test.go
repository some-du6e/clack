package popups

import (
	"context"
	"errors"
	"image"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/slackasset"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The Slack system user (USLACK) as users/info serves it (fakeslack's
// system DM fixture: every image_* the one slack_logo_mark.svg, an
// avatar_hash, is_bot and is_app_user false):
//   - every picture of it is Slack's bundled logo (getMemberProfileImage
//     6Knt: n8qp), the card's and the full profile's sizes included;
//   - ProfileHoverCard (JM3T) draws nothing for it: useProfileHoverApp
//     (MB9A) finds no app and isMemberHuman is false, so neither
//     AppHoverCard nor the member card renders, not even a loading card;
//   - ConnectedPresence's isSystemUser (Bh6F) is Slackbot's or its id:
//     BasePresence's status-slackbot-filled heart, "Active" whatever the
//     presence (MSOq getPresenceText).
func TestSlackSystemUserCard(t *testing.T) {
	sz := fakeslack.Small
	sz.SystemDM = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(""))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.EnsureUsers(ctx, []string{fakeslack.SelfID, fakeslack.SlackUserID, fakeslack.SlackbotID, fakeslack.ProfileID})
	if u, ok := a.S.User(fakeslack.SelfID); ok {
		a.S.Self = *u
	}
	var mu sync.Mutex
	var fetched []string
	fetch := func(_ context.Context, u string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		fetched = append(fetched, u)
		return nil, errors.New("no network in tests")
	}
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	h := &tzHost{a: a, th: th, im: imgcache.New(fetch, "", nil)}

	u, ok := a.User(fakeslack.SlackUserID)
	if !ok || u.Name != "Slack" || u.IsBot || u.AppUser {
		t.Fatalf("fixture: USLACK %+v", u)
	}
	for _, big := range []bool{false, true} {
		if got := pfAvatarURL(u, big); got != slackasset.SlackLogoMark {
			t.Errorf("card picture (big %v): %q, want Slack's logo %q", big, got, slackasset.SlackLogoMark)
		}
	}
	if u.Avatar != slackasset.SlackLogoMark || u.Avatar192 != slackasset.SlackLogoMark || u.Avatar512 != slackasset.SlackLogoMark {
		t.Errorf("USLACK's pictures %q %q %q, want the logo", u.Avatar, u.Avatar192, u.Avatar512)
	}

	card := NewProfileCard(h, image.Pt(300, 300), fakeslack.SlackUserID)
	for i := 0; i < 3; i++ {
		if r := hcLayout(t, card); !r.Empty() {
			t.Fatalf("frame %d: USLACK's card drew %v, want nothing (ProfileHoverCard renders undefined)", i, r)
		}
		Pump()
	}
	if !card.IsApp() || !card.app.failed {
		t.Errorf("USLACK's card: app %v failed %v, want the app branch that renders nothing", card.IsApp(), card.app.failed)
	}

	sb, _ := a.User(fakeslack.SlackbotID)
	person, _ := a.User(fakeslack.ProfileID)
	if !pfIsSystemUser(u) || !pfIsSystemUser(sb) || pfIsSystemUser(person) {
		t.Errorf("isSystemUser: USLACK %v, Slackbot %v, a person %v", pfIsSystemUser(u), pfIsSystemUser(sb), pfIsSystemUser(person))
	}
	if !pfShowsPresence(u) {
		t.Error("the member tip hides USLACK's presence")
	}
	mu.Lock()
	defer mu.Unlock()
	for _, f := range fetched {
		if f == fakeslack.SystemUserAvatarURL || f == slackasset.SlackLogoMark {
			t.Errorf("fetched %q: the logo is bundled", f)
		}
	}
}
