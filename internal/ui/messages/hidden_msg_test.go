package messages

// Hidden people's messages (hidden_msg.go; docs/MESSAGES-AUDIT.md "Hidden
// people's messages"), against Slack's CDN build d3de3a487b1c:
// MessageWrapper's isSenderHidden (EXGU), MessageTombstoneContent (kzyb)
// and HiddenMessageBody (aOPx), HiddenUserAvatar (WbDp). On
// compact_shots_test.go's synthetic #release channel, with Ben (b1, b2
// and two of the root's repliers) hidden. Fake Slack only.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

const benID = "UBEN00001"

// hideBen hides Ben the way Hide does (hideUserById: users.prefs.set
// hidden_users).
func hideBen(t *testing.T, app *core.App) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := app.HideUser(ctx, benID, ""); err != nil {
		t.Fatal(err)
	}
}

func TestHiddenSenderRowsLikeSlack(t *testing.T) {
	for _, dense := range []bool{false, true} {
		mode := map[bool]string{false: "clean", true: "compact"}[dense]
		t.Run(mode, func(t *testing.T) {
			t.Cleanup(uipref.Reset)
			p := *uipref.Get()
			p.Dense = dense
			uipref.Set(p)
			app, _, ts := shotsApp(t)
			th := theme.New(false)
			v := NewChannelView(coreHost{app: app, th: th}, tShots)
			v.Preview = true
			f := frameFn(v)
			loadList(v, f)
			until(t, 5*time.Second, "the rows", f, func() bool { return itemHeight(v.s, ts["b1"]) > 0 && itemHeight(v.s, ts["root"]) > 0 })
			b1 := v.s.rows[ts["b1"]]
			shown := b1.nameShown
			plainH := itemHeight(v.s, ts["b1"])
			hideBen(t, app)
			until(t, 5*time.Second, "b1 hidden", f, func() bool { return b1.nameShown == "This message is hidden" })
			f()
			// HiddenMessageBody: the sender, the contents, Show anyway
			if b1.hidKey != "You’re hiding messages from this person.\x00" {
				t.Errorf("contents %q", b1.hidKey)
			}
			if b1.hidLinkText != "Show anyway" {
				t.Errorf("link %q, want Show anyway", b1.hidLinkText)
			}
			if !dense && !b1.avHidden {
				t.Error("b1's avatar isn't HiddenUserAvatar")
			}
			if len(b1.reactRects) != 0 {
				t.Errorf("b1 drew %d reactions under HiddenMessageBody", len(b1.reactRects))
			}
			// the 15px/22px lines: sender, contents, link (+8px margin);
			// Compact: one line, the link's 8px and the text's 4px
			hidH := itemHeight(v.s, ts["b1"])
			if hidH == plainH {
				t.Errorf("b1 kept its height %d", plainH)
			}
			if dense {
				if b1.bodyRect.Min.Y != b1.col.Y || b1.bodyRect.Min.X <= b1.col.X {
					t.Errorf("Compact: contents at %v, want inline after the sender (column %v)", b1.bodyRect.Min, b1.col)
				}
			} else if b1.bodyRect.Min.Y-b1.col.Y != 22 {
				t.Errorf("contents %dpx below the column top, want 22 (the sender's line)", b1.bodyRect.Min.Y-b1.col.Y)
			}
			for i := 0; i < 3; i++ {
				f()
				if h := itemHeight(v.s, ts["b1"]); h != hidH {
					t.Fatalf("b1's height moved %d → %d", hidH, h)
				}
			}
			// b2 follows b1 (no avatar), but HiddenMessageBody still names
			// the sender
			if b2 := v.s.rows[ts["b2"]]; b2 == nil || b2.nameShown != "This message is hidden" {
				t.Errorf("b2 isn't hidden")
			}
			// others are drawn as before
			if a1 := v.s.rows[ts["a1"]]; a1 == nil || a1.avHidden || a1.nameShown == "This message is hidden" {
				t.Error("Ana's message changed")
			}
			// the reply bar's ReplyBarAvatar for Ben is HiddenUserAvatar
			if !dense {
				root := v.s.rows[ts["root"]]
				found := false
				for _, av := range root.replyAvs {
					if av.id == benID {
						found = true
						if av.drawn != hiddenAvatarDrawn {
							t.Errorf("Ben's reply avatar %q, want HiddenUserAvatar", av.drawn)
						}
					}
				}
				if !found {
					t.Error("the root's reply bar has no avatar for Ben")
				}
			}

			// Show anyway: the message under "Name hidden", then Hide message
			b1.hidLink.Click()
			f()
			f()
			if !app.MessageUnhidden(tShots, ts["b1"]) {
				t.Fatal("Show anyway didn't set isUnhidden")
			}
			until(t, 2*time.Second, "b1 shown", f, func() bool { return b1.nameShown == "Name hidden" })
			if b1.hidLinkText != "Hide message" {
				t.Errorf("link %q, want Hide message", b1.hidLinkText)
			}
			if len(b1.reactRects) == 0 {
				t.Error("b1 shown anyway has no reactions")
			}
			if !dense && !b1.avHidden {
				t.Error("b1 shown anyway: the avatar stays HiddenUserAvatar")
			}
			if h := itemHeight(v.s, ts["b1"]); h <= plainH {
				t.Errorf("b1 shown anyway is %dpx, want its body (%d) plus the link", h, plainH)
			}
			if b2 := v.s.rows[ts["b2"]]; b2.nameShown != "This message is hidden" {
				t.Error("Show anyway on b1 showed b2 too")
			}
			// Hide message
			b1.hidLink.Click()
			f()
			f()
			until(t, 2*time.Second, "b1 hidden again", f, func() bool { return b1.nameShown == "This message is hidden" })
			if app.MessageUnhidden(tShots, ts["b1"]) {
				t.Error("Hide message left isUnhidden")
			}

			// unhideUserApi: back to Ben's own rows
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := app.UnhideUser(ctx, benID); err != nil {
				t.Fatal(err)
			}
			until(t, 5*time.Second, "b1 plain", f, func() bool { return b1.nameShown == shown })
			if b1.avHidden {
				t.Error("unhidden: the avatar is still HiddenUserAvatar")
			}
			if h := itemHeight(v.s, ts["b1"]); h != plainH {
				t.Errorf("unhidden: b1 is %dpx, was %d", h, plainH)
			}
		})
	}
}

// The contents line mentions the viewer: "Someone you hid <b>mentioned
// you</b> in this message." with the middle run bold
// (containsDirectUserMention).
func TestHiddenMessageMentionContents(t *testing.T) {
	app, _, _ := shotsApp(t)
	rc := &rowCtx{h: coreHost{app: app, th: theme.New(false)}}
	r := &row{rev: 1, m: core.Message{Channel: tShots, TS: "1.000100", User: benID, Text: "ping <@" + fakeslack.SelfID + ">"}}
	r.hiddenDoc(rc)
	if r.hidKey != "Someone you hid \x00mentioned you\x00 in this message.\x00" {
		t.Fatalf("contents %q", r.hidKey)
	}
	sp := r.hidView.Doc().Blocks[0].Spans
	if len(sp) != 3 || sp[1].Style&richtext.Bold == 0 || sp[0].Style&richtext.Bold != 0 {
		t.Errorf("spans %+v: want only \"mentioned you\" bold", sp)
	}
	// @here isn't a direct mention
	r2 := &row{rev: 1, m: core.Message{Channel: tShots, TS: "2.000100", User: benID, Text: "<!here> ping"}}
	r2.hiddenDoc(rc)
	if r2.hidKey != "You’re hiding messages from this person.\x00" {
		t.Errorf("@here: contents %q", r2.hidKey)
	}
}

// An org that locks hide_person_opt_out on hides nobody, so the rows are
// drawn as they are (isUserHidden returns false).
func TestHiddenSenderOrgOptOut(t *testing.T) {
	app, fs, ts := shotsApp(t)
	fs.SetEnterpriseLockedPrefs(map[string]any{"hide_person_opt_out": true})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := app.LoadEnterprisePrefs(ctx); err != nil {
		t.Fatal(err)
	}
	hideBen(t, app)
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, tShots)
	v.Preview = true
	f := frameFn(v)
	loadList(v, f)
	until(t, 5*time.Second, "b1", f, func() bool { return itemHeight(v.s, ts["b1"]) > 0 })
	f()
	if b1 := v.s.rows[ts["b1"]]; b1.nameShown == "This message is hidden" || b1.avHidden {
		t.Error("hide_person_opt_out: b1 is hidden")
	}
}

// gap-hidden-{light,dark}.png: Ben hidden. b1 (HiddenMessageBody under
// his HiddenUserAvatar), b2 shown anyway (its list under "Name hidden",
// then Hide message), and the root's reply bar with his avatar hidden;
// then the same three rows under Compact.
func TestHiddenSenderShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("MESSAGES_AUDIT_SHOTS_DIR not set (the messages-audit-shots workflow)")
	}
	t.Cleanup(uipref.Reset)
	for _, dark := range []bool{false, true} {
		for _, dense := range []bool{false, true} {
			mode := map[bool]string{false: "light", true: "dark"}[dark]
			if dense {
				mode += "-compact"
			}
			p := *uipref.Get()
			p.Dense = dense
			uipref.Set(p)
			app, fs, ts := shotsApp(t)
			hideBen(t, app)
			app.SetMessageUnhidden(tShots, ts["b2"], benID, true)
			th := theme.New(dark)
			host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New(fs.Fetch, "", nil)}
			const w, h = 1100, 1500
			v := NewChannelView(host, tShots)
			v.Preview = true
			ops := new(op.Ops)
			f := func() { shotFrame(v, th, w, h, ops) }
			loadList(v, f)
			until(t, 5*time.Second, "the hidden rows", f, func() bool {
				r := v.s.rows[ts["b1"]]
				return r != nil && r.nameShown == "This message is hidden" && itemHeight(v.s, ts["root"]) > 0
			})
			for i := 0; i < 40; i++ { // avatars
				f()
				time.Sleep(10 * time.Millisecond)
			}
			f()
			img := pixeltest.Render(t, w, h, ops)
			y0, y1 := -1, 0
			for i, it := range v.s.items {
				if it.kind != itMsg || i >= len(v.s.pos) {
					continue
				}
				switch it.key {
				case ts["a2"]:
					y0 = v.s.pos[i] + v.s.origin.Y
				case ts["root"]:
					y1 = v.s.pos[i] + v.s.heights[i] + v.s.origin.Y
				}
			}
			if y0 < 0 || y1 <= y0 {
				t.Fatalf("%s: rows a2..root at %d..%d", mode, y0, y1)
			}
			savePNG(t, filepath.Join(dir, "gap-hidden-"+mode+".png"), crop(img, max(y0-8, 0), min(y1+8, h)))
		}
	}
}
