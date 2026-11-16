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
	"context"
	"fmt"
	"image"
	"os"
	"path/filepath"
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
	s.PutUsers([]slack.User{self})
	ts := planSeed(s)
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

// planSeed puts the app, its channel and AppPlanMessages in s and
// returns each post's ts by name.
func planSeed(s *store.Store) map[string]string {
	bot := slack.User{ID: fakeslack.AppPlanUserID, TeamID: fakeslack.TeamID, Name: "eth", IsBot: true, Profile: slack.Profile{DisplayName: "eth"}}
	s.PutUsers([]slack.User{bot})
	s.PutBot(fakeslack.AppPlanBotID, store.BotInfo{Name: fakeslack.AppPlanBotName, AppID: fakeslack.AppPlanAppID})
	s.PutConv(slack.Conversation{ID: fakeslack.AppPlanChannelID, Name: "plans", IsChannel: true, IsMember: true, NumMembers: 2})
	msgs, names := fakeslack.AppPlanMessages()
	ts := map[string]string{}
	for i, n := range names {
		ts[n] = msgs[i].TS
	}
	s.SetLatest(fakeslack.AppPlanChannelID, msgs, false)
	return ts
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

// Every Slick plugin, installed alone, over the same posts: none may
// leave a plan post blank under its sender (ui-slick.yml; a plugin
// that doesn't start is skipped).
func TestSlickViewAppPlanEveryPlugin(t *testing.T) {
	dir := slickDir(t)
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range ents {
		n := e.Name()
		if strings.HasSuffix(n, ".js") && !strings.HasSuffix(n, ".main.js") && !strings.HasSuffix(n, ".user.js") {
			ids = append(ids, strings.TrimSuffix(n, ".js"))
		}
	}
	if len(ids) == 0 {
		t.Fatal("no plugins built")
	}
	for _, slim := range []bool{true, false} {
		for _, id := range ids {
			look := map[bool]string{true: "slim", false: "cards"}[slim]
			t.Run(look+"/"+id, func(t *testing.T) {
				var ts map[string]string
				e := newSlickEnv(t, func(s *store.Store) { ts = planSeed(s) })
				group := "off"
				if slim {
					group = "on"
				}
				e.app.SetExperimentGroup("agent_streaming_slimmer_task_cards", group)
				if !e.tryInstall(t, id) {
					t.Skipf("%s doesn't start here", id)
				}
				v := NewChannelView(e.h, fakeslack.AppPlanChannelID)
				v.Preview = true
				f := frameFn(v)
				drawn := func() bool {
					for _, k := range ts {
						if rw := v.s.rows[k]; rw == nil || rw.rv == nil {
							return false
						}
					}
					return true
				}
				for dl := time.Now().Add(8 * time.Second); time.Now().Before(dl) && !drawn(); {
					f()
					time.Sleep(15 * time.Millisecond)
				}
				for i := 0; i < 20; i++ {
					f()
					time.Sleep(10 * time.Millisecond)
				}
				for _, name := range []string{"plan", "mixed"} {
					rw := v.s.rows[ts[name]]
					if rw == nil {
						t.Logf("%s: %s not drawn (hidden by the plugin?)", id, name)
						continue
					}
					slots := rw.rv.ElementSlots()
					if len(slots) != 1 || slots[0].Dy() < 19 || rw.bodyRect.Dy() < 19 {
						t.Errorf("%s: %s blank: body %v slots %v doc [%s] elements %v", id, name, rw.bodyRect, slots,
							docKinds(rw.m.Doc), rw.rv.Options().Elements != nil)
					}
				}
			})
		}
	}
}

// tryInstall is install that reports a plugin that fails to import or
// start instead of failing the test.
func (e *slickEnv) tryInstall(t *testing.T, id string) bool {
	t.Helper()
	dir := slickDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, f := range []string{id + ".js", id + ".main.js"} {
		code, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			if f == id+".js" {
				return false
			}
			continue
		}
		if _, err := e.s.Import(ctx, code); err != nil {
			t.Logf("import %s: %v", f, err)
			return false
		}
	}
	if err := e.s.SetEnabled(ctx, id, true); err != nil {
		t.Logf("enable %s: %v", id, err)
		return false
	}
	for _, p := range e.s.Settings().Plugins {
		if p.ID == id && (!p.Running || p.StartError != "") {
			t.Logf("%s: running=%v error=%q", id, p.Running, p.StartError)
			return false
		}
	}
	time.Sleep(150 * time.Millisecond)
	return true
}
