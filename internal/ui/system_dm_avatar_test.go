package ui

import (
	"image"
	"image/color"
	"os"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slackasset"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Karim (2026-11-21, α.113, Windows): the DM with the Slack system user
// (USLACK) drew a generic blue person in its header where Slack draws its
// logo. users/info gives USLACK an avatar_hash, whose ca.slack-edge.com
// picture is that blue person, and image_* that are all one SVG;
// getMemberProfileImage (client-boot 6Knt) ignores both and returns its
// bundled slack_logo_mark (n8qp) for "USLACK" at every size, drawn with
// no tile behind it (BaseAvatar's isSystemUser: c-avatar--transparent_bg).
//
// Over fakeslack's system DM fixture: the sidebar row and the header ask
// for the logo, the cache draws it from the shipped bytes, and with a GPU
// (CLACK_PIXEL_TESTS) the window shows its four colours in the sidebar
// column and in the header. The DM's posts are user-less bot_messages:
// each row is named by its username override, pictured by its
// icons.image_48 (over the bot_profile's icons) and badged APP
// (BaseMessageSender: getMsgSenderEntity, renderBotLabel). Synthetic data.
func TestSlackSystemDMAvatar(t *testing.T) {
	sz := fakeslack.Small
	sz.SystemDM = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("CLACK_PIXEL_TESTS") != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	ch := fakeslack.SystemDMID
	e.until("the system DM's row", 20*time.Second, func() bool {
		c := e.a.model().conv(ch)
		return c != nil && c.Avatar != ""
	})
	e.openChannel(ch)
	e.until("the system DM's posts", 20*time.Second, func() bool {
		cv := e.channelView(ch)
		return cv != nil && len(cv.Senders()) >= 2 && e.a.images.Pending() == 0
	})
	e.settle(300 * time.Millisecond)

	// the sidebar row's and the header's picture (both draw conv.Avatar)
	c := e.a.model().conv(ch)
	if c.Avatar != slackasset.SlackLogoMark {
		t.Errorf("the system DM's avatar is %q, want Slack's logo %q (not the profile's %q)", c.Avatar, slackasset.SlackLogoMark, fakeslack.SystemUserAvatarURL)
	}
	if u, ok := e.a.Core().User(fakeslack.SlackUserID); !ok || u.Avatar != slackasset.SlackLogoMark || u.Avatar512 != slackasset.SlackLogoMark {
		t.Errorf("USLACK's pictures: %+v", u)
	}
	if !e.a.images.Loaded(slackasset.SlackLogoMark) || e.a.images.Failed(slackasset.SlackLogoMark, e.context().Dp(32)) {
		t.Errorf("the logo isn't drawn: loaded %v", e.a.images.Loaded(slackasset.SlackLogoMark))
	}

	// the posts' rows
	for _, s := range e.channelView(ch).Senders() {
		if s.Name != fakeslack.SystemDMUsername || s.Avatar != fakeslack.SystemDMIconURL || s.Badge != "APP" {
			t.Errorf("post %s: name %q avatar %q badge %q, want %q, %q, APP", s.TS, s.Name, s.Avatar, s.Badge,
				fakeslack.SystemDMUsername, fakeslack.SystemDMIconURL)
		}
	}

	if e.gpu == nil {
		t.Log("no GPU: pixels not checked")
		return
	}
	// the row sits in the sidebar's lower part (an app DM, after 30-odd
	// conversations): scroll it to the top of the list
	row := -1
	for i, r := range e.a.sidebar.rows {
		if r.key == ch {
			row = i
		}
	}
	if row < 0 {
		t.Fatal("the system DM has no sidebar row")
	}
	t.Logf("sidebar row %d of %d, list at %+v", row, len(e.a.sidebar.rows), e.a.sidebar.list.Position)
	e.a.sidebar.list.Position.First, e.a.sidebar.list.Position.Offset = max(row-2, 0), 0
	e.settle(300 * time.Millisecond)
	t.Logf("list now at %+v", e.a.sidebar.list.Position)
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	gtx := e.context()
	po := e.a.primaryOrigin
	sidebar := image.Rect(gtx.Dp(theme.RailW), gtx.Dp(theme.TopNavH), po.X, e.size.Y)
	header := image.Rect(po.X, po.Y, e.size.X, po.Y+gtx.Dp(49))
	for _, r := range []struct {
		name string
		rect image.Rectangle
		min  int // pixels of each colour: a 16 or 24 dp logo
	}{{"sidebar row", sidebar, 2}, {"header", header, 4}} {
		for _, lc := range slackLogoColours {
			if n := countNear(img, r.rect, lc.c, 10); n < r.min {
				t.Errorf("%s %v: %d pixels of the logo's %s, want at least %d", r.name, r.rect, n, lc.name, r.min)
			}
		}
	}
}

var slackLogoColours = []struct {
	name string
	c    color.NRGBA
}{
	{"pink", color.NRGBA{0xe3, 0x06, 0x6a, 0xff}},
	{"blue", color.NRGBA{0x00, 0xb3, 0xff, 0xff}},
	{"green", color.NRGBA{0x41, 0xb6, 0x58, 0xff}},
	{"yellow", color.NRGBA{0xfc, 0xc0, 0x03, 0xff}},
}

// countNear counts r's pixels within tol of c in every channel.
func countNear(img *image.RGBA, r image.Rectangle, c color.NRGBA, tol int) int {
	near := func(a, b uint8) bool { d := int(a) - int(b); return d <= tol && d >= -tol }
	n := 0
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if near(p.R, c.R) && near(p.G, c.G) && near(p.B, c.B) {
				n++
			}
		}
	}
	return n
}

// With show_sidebar_avatars off a DM row's prefix is its partner's
// presence (BaseChannelPrefixPresence → ConnectedPresence, client-boot
// Bh6F), which passes isSystemUser for Slackbot and the Slack system user:
// BasePresence (MSOq) draws them status-slackbot-filled, "Active" with
// showText. Before, the row's BasePresence props said isSystemUser false
// for everyone.
func TestSidebarSystemUserPresence(t *testing.T) {
	for _, c := range []struct {
		user   string
		system bool
	}{{fakeslack.SlackUserID, true}, {fakeslack.SlackbotID, true}, {fakeslack.SelfID, false}, {"U0PERSON001", false}} {
		if got := sbSystemUser(c.user); got != c.system {
			t.Errorf("sbSystemUser(%s) = %v, want %v", c.user, got, c.system)
		}
		p := sbPresencePropsFor(c.user)
		if p.Bool("isSystemUser") != c.system {
			t.Errorf("%s: BasePresence props isSystemUser %v, want %v", c.user, p.Bool("isSystemUser"), c.system)
		}
	}
}

// With show_sidebar_avatars on (fakeslack's default) a DM row's prefix is
// its partner's avatar, and BaseAvatar (client-boot YN+Y) badges it with
// presence only for a member who isn't a system user: renderBadgeOrPresence
// draws the presence `if (showPresence && !isSystemUser)`, and
// ConnectedBaseAvatar (+JEQ) passes isSystemUser for USLACKBOT and USLACK.
// So the system DM's row has the logo alone, where Clack drew a heart over
// its corner, while a person's DM row keeps its dot.
func TestSidebarAvatarSystemUserNoPresence(t *testing.T) {
	sz := fakeslack.Small
	sz.SystemDM = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	ch := fakeslack.SystemDMID
	e.until("the system DM's row with avatars on", 20*time.Second, func() bool {
		c := e.a.model().conv(ch)
		return c != nil && c.Avatar != "" && uipref.Get().SidebarAvatars
	})
	// keys are the sidebar rows a frame drew (ListChannelEntity's $key) and
	// the badges it drew on them (BasePresence's "sb:" + the row's)
	drawn := func(row int) map[string]bool {
		e.a.sidebar.list.Position.First, e.a.sidebar.list.Position.Offset = max(row-2, 0), 0
		e.settle(200 * time.Millisecond)
		var f slackc.InspectFrame
		slackc.InspectBegin(&f, e.r.Source())
		e.frame()
		slackc.InspectEnd()
		keys := map[string]bool{}
		for i := range f.Nodes {
			n := &f.Nodes[i]
			if n.Kind != slackc.InspectComponent || (n.Name != "ListChannelEntity" && n.Name != "BasePresence") {
				continue
			}
			if p := n.Props(); p != nil {
				keys[n.Name+" "+p.StrOr("$key", "")] = true
			}
		}
		return keys
	}
	rowOf := func(id string) int {
		for i, r := range e.a.sidebar.rows {
			if r.key == id {
				return i
			}
		}
		return -1
	}
	if rowOf(ch) < 0 {
		t.Fatalf("the system DM %s has no sidebar row", ch)
	}
	keys := drawn(rowOf(ch))
	if !keys["ListChannelEntity "+ch] {
		t.Fatalf("the frame drew no row for the system DM %s: %v", ch, keys)
	}
	if keys["BasePresence sb:"+ch] {
		t.Errorf("the system DM's avatar row draws a presence badge; Slack's BaseAvatar draws none for a system user")
	}
	// a person's DM row still has its badge
	person := ""
	for _, r := range e.a.sidebar.rows {
		c := e.a.model().conv(r.key)
		if c != nil && c.Kind == "im" && !c.Bot && !c.Self && c.User != "" && !sbSystemUser(c.User) {
			person = c.ID
			break
		}
	}
	if person == "" {
		t.Fatal("no DM with a person")
	}
	keys = drawn(rowOf(person))
	if !keys["ListChannelEntity "+person] || !keys["BasePresence sb:"+person] {
		t.Errorf("a person's DM row %s: drew row %v, badge %v; want both", person, keys["ListChannelEntity "+person], keys["BasePresence sb:"+person])
	}
}
