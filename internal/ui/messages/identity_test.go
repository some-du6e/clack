package messages

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// identityHost serves the fake's images.
type identityHost struct {
	coreHost
	imgs *imgcache.Cache
}

func (h identityHost) Images() *imgcache.Cache { return h.imgs }

// identityApp is a core over a store seeded with fakeslack's identity
// fixtures (no boot, no network beyond the fake's images).
func identityApp(t *testing.T) (*core.App, *fakeslack.Server, []fakeslack.IdentityCase) {
	t.Helper()
	sz := fakeslack.Small
	sz.Identity = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	c := slack.New(fs.Creds())
	c.EdgeURL = fs.URL + "edge/"
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName = fakeslack.TeamID, "Fake Club"
	s.Unlock()
	self := slack.User{ID: fakeslack.SelfID, TeamID: fakeslack.TeamID, Name: "me", Profile: slack.Profile{DisplayName: "me"}}
	s.PutUsers(append([]slack.User{self}, fs.IdentityUsers()...))
	for _, b := range fs.IdentityBots() {
		s.PutBot(b.ID, store.BotInfo{Name: b.Name, AppID: b.AppID, Workflow: b.IsWorkflowBot,
			Icon: firstOf(b.Icons.Image72, b.Icons.Image48, b.Icons.Image36)})
	}
	s.PutConv(slack.Conversation{ID: fakeslack.IdentityChannelID, Name: "identity-fixtures", IsChannel: true, IsMember: true, NumMembers: 6})
	msgs, cases := fs.IdentityMessages()
	s.SetLatest(fakeslack.IdentityChannelID, msgs, false)
	app := core.New(c, s)
	app.S.Self = self
	app.ServeOffline()
	// the experiments Hack Club's account has (2026-11-08)
	app.SetExperimentGroup("show_as_agent", "on")
	app.SetExperimentGroup("agent_badge", "on")
	return app, fs, cases
}

func firstOf(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// Each identity fixture draws the avatar, name, badge, group and label
// Slack's web client would (core/sender.go, core/badge.go).
func TestSenderIdentityRender(t *testing.T) {
	app, fs, cases := identityApp(t)
	// one theme switched like the app does (SetDark bumps Gen, which
	// drops body layouts shaped in the other mode)
	th := theme.New(false)
	for _, dark := range []bool{false, true} {
		name := "light"
		if dark {
			name = "dark"
		}
		t.Run(name, func(t *testing.T) {
			th.SetDark(dark)
			imgs := imgcache.New(fs.Fetch, "", nil)
			h := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgs}
			v := NewChannelView(h, fakeslack.IdentityChannelID)
			v.Preview = true
			var r input.Router
			ops := new(op.Ops)
			now := time.Now()
			size := image.Pt(760, 860)
			frame := func() {
				ops.Reset()
				now = now.Add(20 * time.Millisecond)
				paint.FillShape(ops, th.C.Bg, clip.Rect{Max: size}.Op())
				gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(size), Now: now, Source: r.Source()}
				v.Layout(gtx)
				r.Frame(ops)
			}
			last := cases[len(cases)-1].TS
			until(t, 8*time.Second, "every row drawn", frame, func() bool {
				for _, c := range cases {
					if v.s.rows[c.TS] == nil {
						return false
					}
				}
				return v.s.rows[last].avDrawn != ""
			})
			header := map[string]bool{}
			for _, it := range v.s.items {
				if it.kind == itMsg {
					header[it.key] = it.header
				}
			}
			for _, c := range cases {
				row := v.s.rows[c.TS]
				if header[c.TS] != c.Header {
					t.Errorf("%s: header %v, want %v", c.Name, header[c.TS], c.Header)
				}
				if got := row.senderName(); got != c.Sender {
					t.Errorf("%s: sender %q, want %q", c.Name, got, c.Sender)
				}
				if row.badge != c.Badge {
					t.Errorf("%s: badge %q, want %q", c.Name, row.badge, c.Badge)
				}
				av := row.avatar
				if c.Header {
					av = row.avDrawn
				}
				switch {
				case c.AvatarUser != "":
					u, _ := app.User(c.AvatarUser)
					if av != u.Avatar {
						t.Errorf("%s: avatar %q, want %s's %q", c.Name, av, c.AvatarUser, u.Avatar)
					}
				case c.Avatar == "default":
					if !isDefaultBotAvatar(av) {
						t.Errorf("%s: avatar %q, want Slack's ava_0002", c.Name, av)
					}
				case strings.HasPrefix(c.Avatar, "emoji:"):
					if !strings.Contains(av, "1f680") { // :rocket:
						t.Errorf("%s: avatar %q, want the rocket emoji", c.Name, av)
					}
				case av != c.Avatar:
					t.Errorf("%s: avatar %q, want %q", c.Name, av, c.Avatar)
				}
				label := ""
				if row.m.Pinned {
					label = pinnedLabel(row.rc, row.m.PinnedBy)
				}
				if label != c.Label {
					t.Errorf("%s: label %q, want %q", c.Name, label, c.Label)
				}
			}
			identityShot(t, frame, ops, size, "identity-"+name)
		})
	}
}

// identityShot saves the pane as IDENTITY_SHOTS_DIR/<name>.png (CI).
func identityShot(t *testing.T, frame func(), ops *op.Ops, size image.Point, name string) {
	dir := os.Getenv("IDENTITY_SHOTS_DIR")
	if dir == "" {
		return
	}
	for i := 0; i < 40; i++ { // avatars decode in the background
		frame()
		time.Sleep(25 * time.Millisecond)
	}
	frame()
	img := pixeltest.Render(t, size.X, size.Y, ops)
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", f.Name())
}

// A plugin round trip (MessageWrapper patched) of an app posting with a
// person's token keeps the person's picture: Clack used to take the
// bot_profile icon here.
func TestSlackMessageUserWinsOverBotProfile(t *testing.T) {
	app, fs, _ := identityApp(t)
	msgs, _ := fs.IdentityMessages()
	src := app.MessageFromSlack(fakeslack.IdentityChannelID, &msgs[2])
	if src.User != fakeslack.ProfileID || src.BotProfile == nil {
		t.Fatalf("fixture: %+v", src)
	}
	got := applySlackMessage(app, &src, roundTrip(t, slackMessage(&src)))
	if got.Avatar != "" || got.Name != "" || got.User != fakeslack.ProfileID {
		t.Errorf("as-user app message after a round trip: avatar %q name %q user %q, want the member's", got.Avatar, got.Name, got.User)
	}
	p := roundTrip(t, slackMessage(&src))
	if p.Map("bot_profile").Map("icons").StrOr("image_36", "") == "" {
		t.Errorf("bot_profile icons not passed to plugins as Slack sends them: %v", p["bot_profile"])
	}
}

// Slack's pin leans head up-left, needle down-right (icon_pin_filled).
func TestPinGlyphLean(t *testing.T) {
	segs := parseGlyph(pinGlyph)
	if len(segs) < 10 || segs[len(segs)-1].op != 'Z' {
		t.Fatalf("parsed %d segments", len(segs))
	}
	b := pinGlyphBounds(2000)
	if b.Dx() != 1400 || b.Dy() != 1800 {
		t.Errorf("glyph ink box %v, want 1400×1800 font units", b)
	}
	// the needle tip is the lowest point, the head the leftmost: Slack's
	// pin has the needle right of centre and the head in the upper half
	tip, left := f32.Pt(0, -1e9), f32.Pt(1e9, 0)
	for _, s := range segs {
		for _, p := range s.p {
			if s.op == 'Z' || p == (f32.Point{}) {
				continue
			}
			q := f32.Pt(p.X, float32(glyphAscent)-p.Y) // y down
			if q.Y > tip.Y {
				tip = q
			}
			if q.X < left.X {
				left = q
			}
		}
	}
	if tip.X <= 1000 || left.Y >= 1000 {
		t.Errorf("needle tip %v, leftmost point %v: want the needle down-right, the head up-left", tip, left)
	}
}
