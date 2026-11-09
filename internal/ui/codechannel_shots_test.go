package ui

// Code channels (Slack Code) in the shell, over an offline core with the
// fakeslack fixtures (synthetic data, nothing talks to Slack): the Code
// channels section and its status dots, the header's code tile, the
// foreword and the agent context bar, one channel per agent status plus
// the archived and preview cases. With CODE_SHOTS_DIR set (the
// code-channels workflow) each state is saved as a PNG, light and dark.

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

func codeShell(t *testing.T) (*svEnv, fakeslack.CodeFixtures) {
	t.Helper()
	a, ca := ttApp(t, t.TempDir())
	f := fakeslack.CodeChannelFixtures(time.Now())
	ca.S.PutUsers([]slack.User{f.Agent})
	var counts []slack.CountEntry
	for _, c := range f.Convs {
		ca.S.PutConv(c)
		counts = append(counts, slack.CountEntry{ID: c.ID, Latest: f.Latest[c.ID], LastRead: f.Latest[c.ID]})
		ca.S.SetLatest(c.ID, f.History[c.ID], false)
	}
	ca.S.SetCounts(&slack.Counts{Channels: counts})
	ca.S.Sections = []slack.Section{{ID: "L0CHANNELS", Type: "channels"}, {ID: "L0DMS", Type: "direct_messages"}}
	b := newBackend(ca, "")
	b.app = a
	b.ctx, b.cancel = context.WithCancel(context.Background())
	t.Cleanup(b.cancel)
	b.fetched[ttGen] = time.Now()
	for _, c := range append(append([]slack.Conversation{}, f.Convs...), f.Others...) {
		b.fetched[c.ID] = time.Now() // offline: no tabs/facepile fetches
	}
	a.data = b
	b.apply(a, ca.Snapshot())
	e := &svEnv{t: t, a: a, ca: ca, ops: new(op.Ops), rt: new(input.Router), sz: image.Pt(1280, 820), trees: map[uint64][]byte{}}
	a.Navigate("c/" + ttGen)
	for i := 0; i < 5; i++ {
		e.frame()
	}
	return e, f
}

func TestCodeChannelsShell(t *testing.T) {
	e, f := codeShell(t)
	a := e.a
	m := a.model()
	if len(m.Sections) == 0 || m.Sections[0].Type != "coding_channels" || m.Sections[0].Name != "Code channels" {
		t.Fatalf("first section %+v, want Slack's Code channels", m.Sections[0])
	}
	got := map[string]bool{}
	for _, id := range m.Sections[0].Rows {
		got[id] = true
	}
	for _, c := range f.Convs {
		if !got[c.ID] {
			t.Errorf("%s (%s) isn't in Code channels: %v", c.Name, c.ID, m.Sections[0].Rows)
		}
		ci := m.conv(c.ID)
		if ci == nil || ci.Code == nil {
			t.Fatalf("%s: no code channel in the model", c.Name)
		}
		if ci.CodeStatus != f.Statuses[c.ID] {
			t.Errorf("%s: status %q, want %q", c.Name, ci.CodeStatus, f.Statuses[c.ID])
		}
		if ci.AgentName != "Codebot" { // useAgentName: the display name
			t.Errorf("%s: agent %q", c.Name, ci.AgentName)
		}
	}
	if sectionIcon("coding_channels") != "code" {
		t.Error("the Code channels heading should carry the code glyph")
	}
	// the context bar: the working chip and items, "No branch info" for an
	// empty code_channel, nothing on other channels
	bar := func(ch string) int {
		ops := new(op.Ops)
		gtx := ttContext(ops, image.Pt(900, 200))
		gtx.Constraints = layout.Constraints{Max: image.Pt(900, 200)}
		return a.layoutCodeContextBar(gtx, ch).Size.Y
	}
	if h := bar(fakeslack.CodeWorkingID); h < 38 {
		t.Errorf("working bar %dpx, want the 28px chip plus 6px padding", h)
	}
	if h := bar(fakeslack.CodeEmptyID); h == 0 || h > 30 {
		t.Errorf("empty code_channel bar %dpx, want one 16px line plus padding", h)
	}
	if h := bar(ttGen); h != 0 {
		t.Errorf("#general got a %dpx code channel bar", h)
	}
	a.data.core.SetExperimentGroup("session_channels", "off")
	if h := bar(fakeslack.CodeWorkingID); h != 0 {
		t.Errorf("bar shown with session_channels off (%dpx)", h)
	}
	a.data.core.SetExperimentGroup("session_channels", "on")
	if s := e.ca.CodeStatus(fakeslack.CodeDoneID); s != core.AgentClosed {
		t.Errorf("CodeStatus %q", s)
	}
}

func TestCodeChannelShots(t *testing.T) {
	dir := os.Getenv("CODE_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e, f := codeShell(t)
			if dark {
				e.a.setDark(true)
			}
			var win *headless.Window
			if dir != "" {
				w, err := headless.NewWindow(e.sz.X, e.sz.Y)
				if err != nil {
					t.Fatalf("headless window: %v", err)
				}
				t.Cleanup(w.Release)
				win = w
			}
			shot := func(name string) {
				for i := 0; i < 30; i++ {
					e.frame()
					time.Sleep(5 * time.Millisecond)
				}
				if win == nil {
					return
				}
				if err := win.Frame(e.ops); err != nil {
					t.Fatal(err)
				}
				img := image.NewRGBA(image.Rectangle{Max: e.sz})
				if err := win.Screenshot(img); err != nil {
					t.Fatal(err)
				}
				out, err := os.Create(filepath.Join(dir, name+"-"+mode+".png"))
				if err != nil {
					t.Fatal(err)
				}
				defer out.Close()
				if err := png.Encode(out, img); err != nil {
					t.Fatal(err)
				}
			}
			for _, c := range f.Convs {
				e.a.Navigate("c/" + c.ID)
				shot("code-" + f.Statuses[c.ID] + "-" + c.Name)
			}
			// archived: it leaves the sidebar; opened, the archived bar
			// replaces the composer
			arch := f.Others[0]
			e.ca.S.PutConv(arch)
			e.a.Navigate("c/" + arch.ID)
			e.a.data.apply(e.a, e.ca.Snapshot())
			shot("code-archived-" + arch.Name)
			// a code channel the viewer isn't in, while the agent works
			prev := f.Others[1]
			e.ca.S.PutConv(prev)
			e.a.Navigate("c/" + prev.ID)
			e.a.data.apply(e.a, e.ca.Snapshot())
			shot("code-preview-" + prev.Name)
		})
	}
}
