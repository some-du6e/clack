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

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
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
		})
	}
}

// Over fakeslack (Size.CodeChannels): the fixtures arrive through
// users.conversations / conversations.info, a code channel the viewer
// isn't in opens as a preview with the join pane (its context bar shows
// while the agent works), and an archived one shows the archived bar.
func TestCodeChannelsFakeslack(t *testing.T) {
	sz := fakeslack.Small
	sz.CodeChannels = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	dir := os.Getenv("CODE_SHOTS_DIR")
	if dir != "" && e.gpu == nil {
		w, err := headless.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	shot := func(name string) {
		e.settle(300 * time.Millisecond)
		if dir == "" || e.gpu == nil {
			return
		}
		img := image.NewRGBA(image.Rectangle{Max: e.size})
		if err := e.gpu.Screenshot(img); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	e.until("Code channels section", 20*time.Second, func() bool {
		ss := e.a.model().Sections
		return len(ss) > 0 && ss[0].Type == "coding_channels" && len(ss[0].Rows) == 7
	})
	e.openChannel(fakeslack.CodeWorkingID)
	e.until("working status", 10*time.Second, func() bool {
		ci := e.a.model().conv(fakeslack.CodeWorkingID)
		return ci != nil && ci.CodeStatus == core.AgentProcessing
	})
	shot("code-fakeslack-working")

	prev := e.a.convoFor(fakeslack.CodePreviewID, "")
	e.openChannel(fakeslack.CodePreviewID)
	e.until("join pane", 20*time.Second, func() bool { return e.a.footerFor(prev) == footPreview && prev.jp != nil })
	if ci := e.a.convOf(fakeslack.CodePreviewID); ci == nil || ci.Code == nil || !ci.Preview {
		t.Errorf("preview code channel %+v", ci)
	}
	shot("code-fakeslack-preview")

	arch := e.a.convoFor(fakeslack.CodeArchivedID, "")
	e.openChannel(fakeslack.CodeArchivedID)
	e.until("archived bar", 20*time.Second, func() bool { return e.a.footerFor(arch) == footArchived })
	shot("code-fakeslack-archived")
	e.openChannel(fakeslack.CodeIdleID)
	e.until("archived row gone", 10*time.Second, func() bool { return !e.sidebarHas(fakeslack.CodeArchivedID) })
}
