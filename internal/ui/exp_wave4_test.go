package ui

// exp-wave-4 (core/exp_wave4.go): the thread pane header's title for agent
// threads (agent_channel_thread_naming, agent_dm_thread_naming; srGL
// renderThreadTitle) against fakeslack's thread header fixtures with
// Size.ThreadAgentTitles. With THREAD_HEADER_SHOTS_DIR set (the
// thread-header workflow) the hovered title rows are saved, light and
// dark.

import (
	"image"
	"os"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func agentTitleEnv(t *testing.T, dark bool) (*perfEnv, *fakeslack.Server) {
	t.Helper()
	sz := fakeslack.Small
	sz.ThreadHeader, sz.ThreadAgentTitles = true, true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("THREAD_HEADER_SHOTS_DIR") != "" {
		if e.gpu != nil {
			e.gpu.Release()
		}
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	if dark {
		e.a.setDark(true)
	}
	return e, fs
}

// waitTitle runs frames until the open thread's header title is want.
func (e *perfEnv) waitTitle(ch string, want core.ThreadTitle) *messages.ThreadView {
	e.tb.Helper()
	var got core.ThreadTitle
	dl := time.Now().Add(20 * time.Second)
	for {
		e.frame()
		if tv := e.threadView(ch, fakeslack.ThreadRootTS); tv != nil {
			if got = tv.HeaderTitle(); got == want {
				return tv
			}
		}
		if time.Now().After(dl) {
			e.tb.Fatalf("%s: the header's title is %+v, want %+v", ch, got, want)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// hoverTitle rests the pointer on the title until its edit button shows.
func (e *perfEnv) hoverTitle(tv *messages.ThreadView) image.Rectangle {
	e.tb.Helper()
	r := tv.TitleRect()
	e.movePtr(image.Pt(r.Min.X+4, (r.Min.Y+r.Max.Y)/2))
	var b image.Rectangle
	e.until("the edit button", 5*time.Second, func() bool {
		b = tv.EditTitleRect()
		return !b.Empty()
	})
	return b
}

func TestThreadAgentTitle(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e, fs := agentTitleEnv(t, dark)
			c := e.a.Core()
			c.SetExperimentGroup("agent_channel_thread_naming", "on")
			c.SetExperimentGroup("agent_dm_thread_naming", "on")

			// a channel thread whose root has an agent_session: its title,
			// editable; the channel's name still follows it
			e.a.Navigate("activity")
			e.a.OpenThread(fakeslack.ThreadPublicID, fakeslack.ThreadRootTS)
			tv := e.waitTitle(fakeslack.ThreadPublicID, core.ThreadTitle{Text: fakeslack.ThreadAgentTitle, Editable: true})
			if e.a.Core().AgentAppChannel(fakeslack.ThreadPublicID) {
				t.Error("a channel reads as an agent app's DM")
			}
			e.waitThreadHeader(fakeslack.ThreadHeaderCase{Name: "public", Channel: fakeslack.ThreadPublicID, Text: "th-launch-notes", Icon: "channel"})
			if !tv.EditTitleRect().Empty() {
				t.Error("the edit button shows before the title is hovered")
			}
			b := e.hoverTitle(tv)
			e.movePtr(b.Min.Add(b.Size().Div(2)))
			e.settle(400 * time.Millisecond) // the "Edit thread name" tooltip
			e.threadHeaderShot("thread-agent-title-" + mode)

			// Edit thread name: the modal on the title, then
			// agents.threads.rename and the new title at once
			e.click(b.Min.Add(b.Size().Div(2)))
			e.frame()
			e.frame()
			m, ok := topOverlay[*popups.AgentThreadNameModal](e.a)
			if !ok {
				t.Fatal("no EditAgentThreadNameModal from the edit button")
			}
			m.SetName("Ship list")
			m.Save()
			e.until("agents.threads.rename", 5*time.Second, func() bool {
				for _, w := range fs.AgentSessionEdits() {
					if w.Get("method") == "agents.threads.rename" && w.Get("channel") == fakeslack.ThreadPublicID &&
						w.Get("thread_ts") == fakeslack.ThreadRootTS && w.Get("name") == "Ship list" && w.Get("reason") == "rename_agent_thread" {
						return true
					}
				}
				return false
			})
			e.movePtr(image.Pt(e.size.X/3, e.size.Y-20))
			e.waitTitle(fakeslack.ThreadPublicID, core.ThreadTitle{Text: "Ship list", Editable: true})

			// off: "Thread" again, no button
			c.SetExperimentGroup("agent_channel_thread_naming", "off")
			e.waitTitle(fakeslack.ThreadPublicID, core.ThreadTitle{})
			c.SetExperimentGroup("agent_channel_thread_naming", "on")

			// an agent app's DM: the assistant thread's title, editable,
			// and no subtitle (renderThreadSubtitle)
			e.a.OpenThread(fakeslack.ThreadAppDMID, fakeslack.ThreadRootTS)
			tv = e.waitTitle(fakeslack.ThreadAppDMID, core.ThreadTitle{Text: fakeslack.ThreadAssistantTitle, Editable: true})
			if !c.AgentAppChannel(fakeslack.ThreadAppDMID) {
				t.Error("the app DM isn't an agent app's")
			}
			e.settle(200 * time.Millisecond)
			if _, _, shown := tv.HeaderName(); shown {
				t.Error("an agent app's DM thread names its conversation")
			}
			e.hoverTitle(tv)
			e.settle(200 * time.Millisecond)
			e.threadHeaderShot("thread-agent-dm-title-" + mode)

			// agent_dm_thread_naming off: AgentThreadTitle, not editable
			c.SetExperimentGroup("agent_dm_thread_naming", "off")
			e.waitTitle(fakeslack.ThreadAppDMID, core.ThreadTitle{Text: fakeslack.ThreadAssistantTitle})
			e.settle(200 * time.Millisecond)
			if !tv.EditTitleRect().Empty() {
				t.Error("the edit button shows with agent_dm_thread_naming off")
			}
		})
	}
}
