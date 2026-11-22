package popups

// The member hover card's role band, name, presence and status against
// Slack's MemberProfileHoverCard (client-boot-shared HIhk) →
// MemberProfileBaseEntity (vCze), build 35b42de8b870:
//
//   - MemberProfileHoverBanner (1onU) draws "Workspace Owner" (isOwner),
//     else "Workspace Admin" (isAdmin), else "Deactivated account". Its
//     isOwner/isAdmin (ltQJ Jm / LF) read the member's is_owner/is_admin
//     off Grid only; on Enterprise Grid they read accountTypes under the
//     org's id, which nothing fills, so a Grid card has no role band. No
//     other role (primary owner, org admin/owner, guests, external people,
//     bots) has a label on the card or in the full profile.
//   - The name is getMemberRealName (hWCs wg), never the display name.
//   - The presence after it is BasePresence (MSOq) with isDnd from
//     dnd.teamInfo: status-member-dnd while the member's DND window is on.
//   - The status emoji is status_emoji through status_emoji_display_info
//     by emoji_name (iz33), and the status text's :codes: are emoji
//     (HOzI FormatTextWithEmoji).
//
// The members are fakeslack's role fixtures (fakeslack/roles.go).

import (
	"context"
	"image"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// hrRoleIDs are every role fixture the card tests open.
var hrRoleIDs = []string{
	fakeslack.AdminID, fakeslack.OwnerID, fakeslack.PrimaryOwnerID,
	fakeslack.OrgAdminID, fakeslack.OrgOwnerID, fakeslack.OrgPrimaryOwnerID,
	fakeslack.GuestID, fakeslack.SingleGuestID, fakeslack.ExternalID,
	fakeslack.AppUserID, fakeslack.AppPersonID, fakeslack.MemberID,
	fakeslack.DeactivatedID, fakeslack.RoleCardID,
}

// hrRoleLabels are every role label Slack has anywhere; the card may
// draw only the one its rule picks.
var hrRoleLabels = []string{
	"Workspace Owner", "Workspace Admin", "Workspace Primary Owner", "Primary Owner",
	"Org Owner", "Org Admin", "Org Primary Owner", "Organization Owner", "Organization Admin",
	"Owner", "Admin", "Single-Channel Guest", "Multi-Channel Guest", "Guest", "External",
	"Deactivated account",
}

// hrFetches records the image URLs the card asks for.
type hrFetches struct {
	mu   sync.Mutex
	urls []string
}

func (r *hrFetches) fetch(_ context.Context, url string) ([]byte, error) {
	r.mu.Lock()
	r.urls = append(r.urls, url)
	r.mu.Unlock()
	return nil, context.Canceled
}

func (r *hrFetches) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.urls...)
}

func (r *hrFetches) asked(sub string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.urls {
		if strings.Contains(u, sub) {
			return true
		}
	}
	return false
}

// hrEnv is a core on fakeslack with every role fixture loaded; grid false
// makes the viewer's workspace a plain one (no enterprise in its creds).
func hrEnv(t *testing.T, grid bool) (*tzHost, *fakeslack.Server, *hrFetches) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	cr := fs.Creds()
	if !grid {
		cr.EnterpriseID = ""
	}
	a := core.New(slack.New(cr), store.New(""))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.EnsureUsers(ctx, append([]string{fakeslack.SelfID}, hrRoleIDs...))
	if u, ok := a.S.User(fakeslack.SelfID); ok {
		a.S.Self = *u
	}
	if a.IsEnterprise() != grid {
		t.Fatalf("fixture: IsEnterprise %v, want %v", a.IsEnterprise(), grid)
	}
	for _, id := range hrRoleIDs {
		if _, ok := a.S.User(id); !ok {
			t.Fatalf("fake users/info didn't serve %s", id)
		}
	}
	rec := &hrFetches{}
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	return &tzHost{a: a, th: th, im: imgcache.New(rec.fetch, "", nil)}, fs, rec
}

// hrDrawn lays id's hover card out and returns every string it drew.
func hrDrawn(t *testing.T, h *tzHost, id string) ([]string, image.Rectangle) {
	t.Helper()
	var mu sync.Mutex
	var drawn []string
	theme.DrawHook = func(s string) { mu.Lock(); drawn = append(drawn, s); mu.Unlock() }
	defer func() { theme.DrawHook = nil }()
	p := NewHoverCard(h, image.Rect(120, 600, 200, 622), id, "")
	r := hcLayout(t, p)
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), drawn...), r
}

func hrHas(drawn []string, s string) bool {
	for _, d := range drawn {
		if d == s {
			return true
		}
	}
	return false
}

// Slack's role band, per role, on Grid (fakeslack's org: no role band for
// anyone, only "Deactivated account") and on a plain workspace (the
// member's is_owner / is_admin). Org roles, guests, external people, bots
// and app users never get a label.
func TestHoverCardRoleBand(t *testing.T) {
	type want struct{ grid, plain string }
	cases := map[string]want{
		fakeslack.AdminID:           {"", "Workspace Admin"},
		fakeslack.OwnerID:           {"", "Workspace Owner"},
		fakeslack.PrimaryOwnerID:    {"", "Workspace Owner"},
		fakeslack.RoleCardID:        {"", "Workspace Owner"}, // owner + org owner: Karim's eps
		fakeslack.OrgAdminID:        {"", ""},
		fakeslack.OrgOwnerID:        {"", ""}, // Izie's zrl
		fakeslack.OrgPrimaryOwnerID: {"", ""},
		fakeslack.GuestID:           {"", ""},
		fakeslack.SingleGuestID:     {"", ""},
		fakeslack.ExternalID:        {"", ""},
		fakeslack.AppUserID:         {"", ""}, // a bot: the app card
		fakeslack.AppPersonID:       {"", ""},
		fakeslack.MemberID:          {"", ""},
		fakeslack.DeactivatedID:     {"Deactivated account", "Deactivated account"},
	}
	for _, grid := range []bool{true, false} {
		mode := map[bool]string{true: "grid", false: "plain"}[grid]
		h, _, _ := hrEnv(t, grid)
		for _, id := range hrRoleIDs {
			w := cases[id]
			band := w.plain
			if grid {
				band = w.grid
			}
			drawn, _ := hrDrawn(t, h, id)
			for _, l := range hrRoleLabels {
				if l != band && hrHas(drawn, l) {
					t.Errorf("%s %s: the card drew %q, want %s", mode, id, l, map[bool]string{true: "no role label", false: "only " + band}[band == ""])
				}
			}
			if band != "" && !hrHas(drawn, band) {
				t.Errorf("%s %s: no %q band", mode, id, band)
			}
		}
	}
}

// Without the band an owner's card is as tall as a plain member's built
// the same way (Karim: Slack's card is shorter than Clack's).
func TestHoverCardOwnerHeightOnGrid(t *testing.T) {
	h, _, _ := hrEnv(t, true)
	_, owner := hrDrawn(t, h, fakeslack.OwnerID)
	_, member := hrDrawn(t, h, fakeslack.MemberID)
	if owner.Dy() != member.Dy() {
		t.Errorf("owner card %dpx tall, member card %dpx: the owner's carries a band Slack doesn't draw on Grid", owner.Dy(), member.Dy())
	}
}

// The card's name is the real name, not the display name.
func TestHoverCardShowsRealName(t *testing.T) {
	h, _, _ := hrEnv(t, true)
	for id, c := range map[string][2]string{
		fakeslack.RoleCardID: {fakeslack.RoleCardRealName, "ezs"},
		fakeslack.MemberID:   {"Milo Fairweather", "milo.f"},
	} {
		drawn, _ := hrDrawn(t, h, id)
		if !hrHas(drawn, c[0]) {
			t.Errorf("%s: real name %q not drawn: %q", id, c[0], drawn)
		}
		if hrHas(drawn, c[1]) {
			t.Errorf("%s: drew the display name %q", id, c[1])
		}
	}
}

// The status emoji is the display-info entry named by status_emoji (💬),
// not the list's first (the custom :ping-9+:), and the :ping-9+: inside
// the status text is the custom emoji, not the literal code.
func TestHoverCardStatusEmojiByName(t *testing.T) {
	h, fs, rec := hrEnv(t, true)
	var drawn []string
	deadline := time.Now().Add(10 * time.Second)
	for {
		drawn, _ = hrDrawn(t, h, fakeslack.RoleCardID)
		Pump()
		if rec.asked("1f4ac") && rec.asked(fs.RoleCardPingURL()) || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !rec.asked("1f4ac") {
		t.Errorf("the status emoji :speech_balloon: (1f4ac) was never drawn; asked %q", rec.list())
	}
	if !rec.asked(fs.RoleCardPingURL()) {
		t.Errorf("the text's custom :ping-9+: was never drawn")
	}
	for _, s := range drawn {
		if strings.Contains(s, ":ping-9+:") {
			t.Errorf("drew the code as text: %q", s)
		}
	}
	text := false
	for _, s := range drawn {
		text = text || strings.Contains(s, "Absolutely DM me")
	}
	if !text {
		t.Errorf("the status text wasn't drawn: %q", drawn)
	}
}

// The presence after the name is the DND glyph while dnd.teamInfo says the
// member's notifications are paused; a member without a window keeps the
// plain ring.
func TestHoverCardMemberDND(t *testing.T) {
	h, fs, _ := hrEnv(t, true)
	now := time.Now().Unix()
	fs.SetMemberDND(fakeslack.RoleCardID, now-60, now+3600)
	asked := func(id string) bool {
		for _, u := range fs.DNDTeamInfoCalls() {
			if strings.Contains(u, id) {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		id, icon string
	}{
		{fakeslack.RoleCardID, "status-member-dnd"},
		{fakeslack.MemberID, "status-member"},
	} {
		deadline := time.Now().Add(10 * time.Second)
		for !asked(c.id) && time.Now().Before(deadline) {
			hrDrawn(t, h, c.id)
			Pump()
			time.Sleep(10 * time.Millisecond)
		}
		if !asked(c.id) {
			t.Errorf("%s: the card never asked dnd.teamInfo (calls %q)", c.id, fs.DNDTeamInfoCalls())
			continue
		}
		var u core.User
		for deadline := time.Now().Add(5 * time.Second); ; {
			Pump()
			u, _ = lookupUser(h, c.id)
			if hrDND(u) == (c.icon == "status-member-dnd") || time.Now().After(deadline) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if got := hrPresenceIcon(u, false); got != c.icon {
			t.Errorf("%s: presence %q, want %q", c.id, got, c.icon)
		}
	}
}

// The full profile has no role label either: Slack's
// RimetoMemberProfileFlexpane (boot-shared N0B4) shows
// MemberProfileRestriction (guests, deactivated) in the avatar slot and
// nothing for admins, owners or org roles.
func TestFullProfileNoRoleLabels(t *testing.T) {
	for _, grid := range []bool{true, false} {
		h, _, _ := hrEnv(t, grid)
		for _, id := range []string{fakeslack.AdminID, fakeslack.OwnerID, fakeslack.PrimaryOwnerID, fakeslack.OrgAdminID,
			fakeslack.OrgOwnerID, fakeslack.OrgPrimaryOwnerID, fakeslack.RoleCardID, fakeslack.MemberID} {
			st := pvFrames(t, NewProfileView(h, id), nil, "")
			for _, l := range hrRoleLabels {
				if has(st.Texts, l) {
					t.Errorf("grid %v %s: the full profile drew %q", grid, id, l)
				}
			}
		}
	}
}

// A member in a huddle with no status emoji and no preset shows
// :headphones: and "In a huddle" (T1ap / fWSH), unless
// huddles_status_ultra is on (Karim's account), when the row is empty.
func TestHoverCardHuddleStatus(t *testing.T) {
	h, _, _ := hrEnv(t, true)
	const id = "U0HUDDLE01"
	h.a.S.PutUsers([]slack.User{{ID: id, TeamID: fakeslack.TeamID, Name: "hal.u", RealName: "Hal Udd",
		Profile: slack.Profile{RealName: "Hal Udd", HuddleState: "in_a_huddle", HuddleExpires: time.Now().Add(time.Hour).Unix()}}})
	for _, ultra := range []string{"off", "on"} {
		h.a.SetExperimentGroup("huddles_status_ultra", ultra)
		drawn, _ := hrDrawn(t, h, id)
		if got, want := hrHas(drawn, "In a huddle"), ultra == "off"; got != want {
			t.Errorf("huddles_status_ultra %s: \"In a huddle\" drawn %v, want %v (%q)", ultra, got, want, drawn)
		}
	}
}

// A blank real name still leaves the presence after a space, and the
// name's link over it (MemberProfileBaseEntity's link holds the name,
// the no-break space and the presence).
func TestHoverCardBlankNameLink(t *testing.T) {
	h, _, _ := hrEnv(t, true)
	const id = "U0BLANKNM1"
	h.a.S.PutUsers([]slack.User{{ID: id, TeamID: fakeslack.TeamID, Name: "zw", Profile: slack.Profile{DisplayName: "zw", RealName: "​"}}})
	p := NewHoverCard(h, image.Rect(120, 600, 200, 622), id, "")
	r := hcLayout(t, p)
	if d := p.PresenceRect(); d.Empty() || d.Min.X <= r.Min.X+16+72+12 {
		t.Errorf("presence %v, want it a space after the empty name (column at x %d)", d, r.Min.X+16+72+12)
	}
}
