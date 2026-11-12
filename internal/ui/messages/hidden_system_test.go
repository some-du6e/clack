package messages

// gap-w20 (docs/GAPS-2026-11-12.md gaps 5 and 11), against Slack's CDN
// build 2cd9270e786c on compact_shots_test.go's synthetic #release
// channel; fake Slack only.
//
//   - A hidden person's system rows: MessageTombstoneContent (boot-shared
//     kzyb) checks isUserHidden(msg.user) for every subtype, so Cara's
//     channel_join draws HiddenMessageBody under BaseMessageAvatar's
//     system glyph (Pgsp); shown anyway it is the redesigned line again,
//     her name RollupName's HiddenUserIconAndName (quVX, q9bm), then
//     "Hide message".
//   - Joins summaries: the rollup is its first message's row (client-boot
//     Nw4e), so a hidden first joiner hides the summary, and a hidden
//     member inside it is HiddenUserIconAndName.
//   - The deleted-message row in a channel: .p-message_pane_message__message
//     .c-message_kit__tombstone {padding: 8px 20px} lines the text up with
//     the bodies.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

const caraID = "UCARA0001"

func hideUser(t *testing.T, app *core.App, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := app.HideUser(ctx, id, ""); err != nil {
		t.Fatal(err)
	}
}

func unhideUser(t *testing.T, app *core.App, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := app.UnhideUser(ctx, id); err != nil {
		t.Fatal(err)
	}
}

// hiddenSpanOf is the doc's HiddenUserIconAndName span for id.
func hiddenSpanOf(d richtext.Doc, id string) (richtext.Span, bool) {
	for _, b := range d.Blocks {
		for _, sp := range b.Spans {
			if sp.Kind == richtext.User && sp.ID == id && sp.Hidden {
				return sp, true
			}
		}
	}
	return richtext.Span{}, false
}

// stableHeight fails when the row's height moves over a few frames.
func stableHeight(t *testing.T, s *listState, key string, f func()) int {
	t.Helper()
	h := itemHeight(s, key)
	for i := 0; i < 3; i++ {
		f()
		if h2 := itemHeight(s, key); h2 != h {
			t.Fatalf("row %s moved %d → %d", key, h, h2)
		}
	}
	return h
}

func TestHiddenSystemRowsLikeSlack(t *testing.T) {
	for _, dense := range []bool{false, true} {
		mode := map[bool]string{false: "clean", true: "compact"}[dense]
		t.Run(mode, func(t *testing.T) {
			t.Cleanup(uipref.Reset)
			p := *uipref.Get()
			p.Dense = dense
			uipref.Set(p)
			app, _, ts := shotsApp(t)
			v := NewChannelView(coreHost{app: app, th: theme.New(false)}, tShots)
			v.Preview = true
			f := frameFn(v)
			loadList(v, f)
			key := ts["join"]
			until(t, 5*time.Second, "the join", f, func() bool { return itemHeight(v.s, key) > 0 })
			join := v.s.rows[key]
			if !join.m.System {
				t.Fatal("the join isn't a redesigned system message (system_messages_redesign_m1 on)")
			}
			plainH := stableHeight(t, v.s, key, f)

			// hidden: HiddenMessageBody, the glyph kept (no avatar)
			hideUser(t, app, caraID)
			until(t, 5*time.Second, "the join hidden", f, func() bool {
				return v.s.rows[key] != nil && v.s.rows[key].nameShown == "This message is hidden"
			})
			join = v.s.rows[key]
			if join.hidLinkText != "Show anyway" {
				t.Errorf("link %q, want Show anyway", join.hidLinkText)
			}
			if join.hidKey != "You’re hiding messages from this person.\x00" {
				t.Errorf("contents %q", join.hidKey)
			}
			if join.avHidden || join.avDrawn != "" {
				t.Errorf("the gutter drew an avatar (hidden %v, %q), want the system glyph", join.avHidden, join.avDrawn)
			}
			hidH := stableHeight(t, v.s, key, f)
			if hidH == plainH {
				t.Errorf("the hidden join kept its height %d", plainH)
			}
			if !dense && hidH < 8+36+8 {
				t.Errorf("hidden join %dpx, under the glyph's 36px box and the padding", hidH)
			}

			// Show anyway: the redesigned line, her name hidden, Hide message
			join.hidLink.Click()
			f()
			f()
			if !app.MessageUnhidden(tShots, key) {
				t.Fatal("Show anyway didn't set isUnhidden")
			}
			until(t, 2*time.Second, "the join shown", f, func() bool {
				r := v.s.rows[key]
				return r != nil && r.hidLinkText == "Hide message"
			})
			join = v.s.rows[key]
			sp, ok := hiddenSpanOf(join.m.Doc, caraID)
			if !ok || sp.Text != "Name hidden" {
				t.Errorf("shown anyway: the actor %+v, want HiddenUserIconAndName", join.m.Doc)
			}
			if txt := rowText(join); txt == "" || !strings.Contains(txt, "Name hidden") || strings.Contains(txt, "cara") {
				t.Errorf("shown anyway: %q, want her name hidden", txt)
			}
			shownH := stableHeight(t, v.s, key, f)
			if shownH <= plainH {
				t.Errorf("shown anyway %dpx, want the line (%d) plus Hide message", shownH, plainH)
			}
			// Hide message
			join.hidLink.Click()
			f()
			f()
			until(t, 2*time.Second, "the join hidden again", f, func() bool { return !app.MessageUnhidden(tShots, key) && itemHeight(v.s, key) == hidH })

			// unhidden: her own line at its old height
			unhideUser(t, app, caraID)
			until(t, 5*time.Second, "the join plain", f, func() bool {
				r := v.s.rows[key]
				_, hid := hiddenSpanOf(r.m.Doc, caraID)
				return r != nil && !hid && itemHeight(v.s, key) == plainH
			})
			if txt := rowText(v.s.rows[key]); !strings.Contains(txt, "cara") {
				t.Errorf("unhidden: %q, want her name", txt)
			}
		})
	}
}

// addJoins puts Ben's and Ana's joins right after Cara's, so the three
// are one rollup whose first message is Cara's.
func addJoins(app *core.App, ts map[string]string) {
	base := tsTime(ts["join"]).Unix()
	for i, u := range []string{benID, "UANA00001"} {
		at := itoa(int(base)+60*(i+1)) + ".000100"
		ts["join-"+u] = at
		app.S.Upsert(tShots, slack.Message{Type: "message", TS: at, User: u, Subtype: "channel_join", Text: "<@" + u + "> has joined the channel"})
	}
}

func TestHiddenJoinsSummaryLikeSlack(t *testing.T) {
	t.Cleanup(uipref.Reset)
	app, _, ts := shotsApp(t)
	addJoins(app, ts)
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, tShots)
	v.Preview = true
	f := frameFn(v)
	loadList(v, f)
	key := "g" + ts["join"]
	until(t, 5*time.Second, "the summary", f, func() bool { return itemHeight(v.s, key) > 0 })
	g := v.s.rows[key]
	if g.m.TS != ts["join"] || g.m.User != caraID {
		t.Errorf("the summary is %s by %s, want the first join (%s by Cara)", g.m.TS, g.m.User, ts["join"])
	}
	plainH := stableHeight(t, v.s, key, f)

	// Ben hidden: his name in the summary is HiddenUserIconAndName
	hideUser(t, app, benID)
	until(t, 5*time.Second, "Ben's name hidden", f, func() bool {
		_, ok := hiddenSpanOf(v.s.rows[key].m.Doc, benID)
		return ok
	})
	g = v.s.rows[key]
	if sp, _ := hiddenSpanOf(g.m.Doc, benID); sp.Text != "Name hidden" {
		t.Errorf("Ben's name %q, want Name hidden", sp.Text)
	}
	if txt := rowText(g); strings.Contains(txt, "ben") || !strings.Contains(txt, "Name hidden") || !strings.Contains(txt, "cara") {
		t.Errorf("summary %q", txt)
	}
	if g.nameShown == "This message is hidden" {
		t.Error("a hidden member later in the rollup hid the whole summary")
	}
	if h := stableHeight(t, v.s, key, f); h != plainH {
		t.Errorf("summary %dpx with a name hidden, was %d (one line either way)", h, plainH)
	}

	// Cara hidden: the rollup is her message, so HiddenMessageBody
	hideUser(t, app, caraID)
	until(t, 5*time.Second, "the summary hidden", f, func() bool { return v.s.rows[key].nameShown == "This message is hidden" })
	if g := v.s.rows[key]; g.hidLinkText != "Show anyway" {
		t.Errorf("link %q", g.hidLinkText)
	}
	stableHeight(t, v.s, key, f)
}

// The deleted message with replies lines up with the channel's bodies
// (x 64, the square at the avatars' 20), in Clean.
func TestTombstoneColumnLikeSlack(t *testing.T) {
	t.Cleanup(uipref.Reset)
	app, _, ts := shotsApp(t)
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, tShots)
	v.Preview = true
	f := frameFn(v)
	loadList(v, f)
	until(t, 5*time.Second, "the rows", f, func() bool { return itemHeight(v.s, ts["gone"]) > 0 && itemHeight(v.s, ts["a1"]) > 0 })
	f()
	gone, a1 := v.s.rows[ts["gone"]], v.s.rows[ts["a1"]]
	if !gone.m.Tombstone {
		t.Fatal("gone isn't a tombstone")
	}
	if gone.bodyRect.Min.X != 64 {
		t.Errorf("the tombstone's text at x %d, want 64 (.c-message_kit__tombstone padding 20 + 36 + 8)", gone.bodyRect.Min.X)
	}
	if a1.bodyRect.Min.X != gone.bodyRect.Min.X {
		t.Errorf("a1's body at x %d, the tombstone's at %d", a1.bodyRect.Min.X, gone.bodyRect.Min.X)
	}
}

// gap-w20-{light,dark}{,-compact}.png: the rows from the root to the
// tombstone with Cara hidden (her broadcast, her join as
// HiddenMessageBody under the glyph), then Ben's and Ana's joins after
// hers as one rollup with Ben hidden, shown anyway; and the tombstone in
// the channel's column.
func TestHiddenSystemShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("MESSAGES_AUDIT_SHOTS_DIR not set (the messages-audit-shots workflow)")
	}
	t.Cleanup(uipref.Reset)
	for _, dark := range []bool{false, true} {
		for _, dense := range []bool{false, true} {
			for _, rollup := range []bool{false, true} {
				mode := map[bool]string{false: "light", true: "dark"}[dark]
				if dense {
					mode += "-compact"
				}
				name := "gap-w20-join-" + mode
				p := *uipref.Get()
				p.Dense = dense
				uipref.Set(p)
				app, fs, ts := shotsApp(t)
				want := ts["join"]
				if rollup {
					name = "gap-w20-rollup-" + mode
					addJoins(app, ts)
					hideUser(t, app, benID)
					hideUser(t, app, caraID)
					app.SetMessageUnhidden(tShots, ts["join"], caraID, true)
					want = "g" + ts["join"]
				} else {
					hideUser(t, app, caraID)
				}
				th := theme.New(dark)
				host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New(fs.Fetch, "", nil)}
				const w, h = 1100, 1500
				v := NewChannelView(host, tShots)
				v.Preview = true
				ops := new(op.Ops)
				f := func() { shotFrame(v, th, w, h, ops) }
				loadList(v, f)
				until(t, 5*time.Second, "the rows", f, func() bool {
					return itemHeight(v.s, want) > 0 && itemHeight(v.s, ts["gone"]) > 0
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
					case ts["bc"]:
						y0 = v.s.pos[i] + v.s.origin.Y
					case ts["gone"]:
						y1 = v.s.pos[i] + v.s.heights[i] + v.s.origin.Y
					}
				}
				if y0 < 0 || y1 <= y0 {
					t.Fatalf("%s: rows bc..gone at %d..%d", name, y0, y1)
				}
				savePNG(t, filepath.Join(dir, name+".png"), crop(img, max(y0-8, 0), min(y1+8, h)))
			}
		}
	}
}
