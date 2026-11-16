package messages

// A custom app's plan posts in a channel, through the whole row
// (ChannelView → row → richview → blockkit's element host): the plan
// draws its block (title line, and open, its tasks with their status
// icons and rich-text details) in both of Slack's looks (slim, cards),
// light and dark, never a blank body under the sender; an unknown block
// type draws Slack's UnsupportedBlockFallback. Synthetic fixtures only
// (fakeslack.AppPlanMessages). BK_AGENT_SHOTS_DIR keeps the PNGs
// (bk-agent-shots.yml).

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func planApp(t *testing.T, slim bool) (*core.App, *fakeslack.Server, map[string]string) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
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
	bot := slack.User{ID: fakeslack.AppPlanUserID, TeamID: fakeslack.TeamID, Name: "eth", IsBot: true,
		Profile: slack.Profile{DisplayName: "eth"}}
	s.PutUsers([]slack.User{self, bot})
	s.PutBot(fakeslack.AppPlanBotID, store.BotInfo{Name: fakeslack.AppPlanBotName, AppID: fakeslack.AppPlanAppID})
	s.PutConv(slack.Conversation{ID: fakeslack.AppPlanChannelID, Name: "plans", IsChannel: true, IsMember: true, NumMembers: 2})
	msgs, names := fakeslack.AppPlanMessages()
	ts := map[string]string{}
	for i, n := range names {
		ts[n] = msgs[i].TS
	}
	s.SetLatest(fakeslack.AppPlanChannelID, msgs, false)
	app := core.New(c, s)
	app.S.Self = self
	app.ServeOffline()
	group := "off"
	if slim {
		group = "on"
	}
	app.SetExperimentGroup("agent_streaming_slimmer_task_cards", group)
	return app, fs, ts
}

func docKinds(d richtext.Doc) string {
	var ks []string
	for _, b := range d.Blocks {
		k := fmt.Sprint(b.Kind)
		if len(b.Buttons) > 0 {
			k += "/" + b.Buttons[0].Kind + ":" + b.Buttons[0].BlockID
		}
		ks = append(ks, k)
	}
	return strings.Join(ks, ",")
}

func TestAppPlanRows(t *testing.T) {
	for _, slim := range []bool{true, false} {
		look := map[bool]string{true: "slim", false: "cards"}[slim]
		for _, dark := range []bool{false, true} {
			mode := map[bool]string{false: "light", true: "dark"}[dark]
			t.Run(look+"/"+mode, func(t *testing.T) {
				app, fs, ts := planApp(t, slim)
				th := theme.New(dark)
				imgs := imgcache.New(fs.Fetch, "", nil)
				h := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgs}
				v := NewChannelView(h, fakeslack.AppPlanChannelID)
				v.Preview = true
				var r input.Router
				ops := new(op.Ops)
				now := time.Now()
				size := image.Pt(760, 1000)
				frame := func() {
					ops.Reset()
					now = now.Add(20 * time.Millisecond)
					paint.FillShape(ops, th.C.Bg, clip.Rect{Max: size}.Op())
					gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(size), Now: now, Source: r.Source()}
					v.Layout(gtx)
					r.Frame(ops)
				}
				until(t, 8*time.Second, "every row drawn", frame, func() bool {
					for _, k := range ts {
						if rw := v.s.rows[k]; rw == nil || rw.rv == nil {
							return false
						}
					}
					return true
				})
				for i := 0; i < 5; i++ {
					frame()
				}
				// what each row drew: its body, its element slots, its text
				report := func(name string) (row *row, slots []image.Rectangle, text string) {
					row = v.s.rows[ts[name]]
					slots = row.rv.ElementSlots()
					text = row.rv.TextRange(0, row.rv.PlainLen())
					t.Logf("%s: body %v slots %v doc [%s] elements %v text %q", name, row.bodyRect, slots,
						docKinds(row.m.Doc), row.rv.Options().Elements != nil, text)
					return
				}
				for _, name := range []string{"plan", "mixed"} {
					row, slots, _ := report(name)
					if len(slots) != 1 || slots[0].Dy() < 19 {
						t.Errorf("%s: the plan's slot is %v, want one block at least a line tall", name, slots)
					}
					if row.bodyRect.Dy() < 19 {
						t.Errorf("%s: the body is %v: blank under the sender", name, row.bodyRect)
					}
				}
				// Slack: SUIRendererItem has no renderer for the type, so
				// UnsupportedBlockFallback; the rest of the blocks draw
				for _, name := range []string{"unknown", "unknown-only"} {
					row, _, text := report(name)
					if !strings.Contains(text, "This message can’t be fully displayed.") {
						t.Errorf("%s: text %q, want UnsupportedBlockFallback's", name, text)
					}
					if name == "unknown" && !strings.Contains(text, "Text after the hologram.") {
						t.Errorf("unknown: the rich_text after the unknown block is missing: %q", text)
					}
					if strings.Contains(text, "Only the fallback text") {
						t.Errorf("%s: drew the message's text, not the blocks' fallback", name)
					}
					if row.bodyRect.Dy() < 19 {
						t.Errorf("%s: the body is %v", name, row.bodyRect)
					}
				}
				identityShotIn(t, "BK_AGENT_SHOTS_DIR", frame, ops, size, "app-plan-"+look+"-"+mode)
			})
		}
	}
}
