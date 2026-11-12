package ui

// Work object cards and their flexpane against internal/fakeslack's
// #work-objects (synthetic data, nothing talks to Slack): a click on the
// card opens WorkObjectFlexpane on its Details, with the unfurl's data at
// once and workObjects.getEntity's live details from
// entity_details_present; the Comments tab loads the comments
// (workObjects.getComments → entity_comments_present) and posts one
// (workObjects.postComment → entity_comment_action_acknowledged); the
// Conversations tab lists entities.refs.list's messages. With
// MESSAGES_AUDIT_SHOTS_DIR set (the messages-audit-shots workflow) each
// state is saved, light and dark.

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func woEnv(t *testing.T, dark bool) *perfEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.WorkObjects = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("MESSAGES_AUDIT_SHOTS_DIR") != "" {
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
	// the harness starts with live updates off (no banner shifting the
	// layout); work object details and comments only come over the socket
	go e.a.Core().SetRealtime(true)
	e.until("the socket", 15*time.Second, func() bool { return e.a.data.m.Connected })
	if dark {
		e.a.setDark(true)
	}
	return e
}

// woShot saves the window, and the flexpane alone when it's open.
func (e *perfEnv) woShot(name string) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	e.settle(800 * time.Millisecond)
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		e.tb.Fatalf("screenshot: %v", err)
	}
	save := func(name string, img image.Image) {
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			e.tb.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			e.tb.Fatal(err)
		}
	}
	save(name, img)
	if e.a.secondary.kind == secWorkObject && e.a.secondary.drawnW > 0 {
		w := e.a.secondary.drawnW
		save(name+"-pane", img.SubImage(image.Rect(e.size.X-w, 0, e.size.X, e.size.Y)))
	}
}

// woCards are #work-objects' on-screen cards.
func (e *perfEnv) woCards() []messages.WorkObjectRect {
	c := e.a.convos[fakeslack.WorkObjectsChannelID+"/"]
	if c == nil {
		return nil
	}
	l, ok := c.list.(channelList)
	if !ok {
		return nil
	}
	return l.WorkObjectRects()
}

func TestWorkObjectFlexpane(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := woEnv(t, dark)
			e.openChannel(fakeslack.WorkObjectsChannelID)
			var task messages.WorkObjectRect
			e.until("the task card", 20*time.Second, func() bool {
				for _, c := range e.woCards() {
					if c.TS == fakeslack.WorkObjectTS[0] {
						task = c
						return true
					}
				}
				return false
			})
			if n := len(e.woCards()); n != 3 {
				t.Errorf("%d cards on screen, want 3 (the task, two docs)", n)
			}
			e.woShot("work-objects-channel-" + mode)
			// the card's click: the flexpane on Details
			e.clickAt(image.Pt(task.R.Min.X+40, task.R.Max.Y-60), time.Second)
			e.until("the flexpane", 10*time.Second, func() bool { return e.a.secondary.kind == secWorkObject })
			p, ok := e.a.secondary.view.(*woPane)
			if !ok || p.tab != "details" || p.o.Entity.EntityID != fakeslack.WOTaskID {
				t.Fatalf("pane %T %+v", e.a.secondary.view, p)
			}
			ca := e.a.Core()
			e.until("the live details", 10*time.Second, func() bool {
				d := ca.WorkObject(fakeslack.WOTaskID)
				return d != nil && !d.Loading && !d.FromUnfurl
			})
			if p.title(p.entity()) != "Task" || p.caption(p.entity()) != "Tracker" {
				t.Errorf("header %q · %q, want Task · Tracker", p.title(p.entity()), p.caption(p.entity()))
			}
			e.woShot("work-object-details-" + mode)
			// Comments
			p.setTab("comments")
			e.until("the comments", 10*time.Second, func() bool {
				cs := ca.WorkObjectCommentsOf(fakeslack.WOTaskID)
				return cs != nil && len(cs.Comments) == 2
			})
			e.woShot("work-object-comments-" + mode)
			p.editor.SetText("Matches Slack now")
			p.post(p.entity())
			e.until("the posted comment", 10*time.Second, func() bool {
				cs := ca.WorkObjectCommentsOf(fakeslack.WOTaskID)
				return cs != nil && len(cs.Comments) == 3
			})
			if _, posted, _ := e.fs.WorkObjectCalls(); len(posted) != 1 {
				t.Errorf("postComment calls %d, want 1", len(posted))
			}
			// Conversations
			p.setTab("threads")
			e.until("the refs", 10*time.Second, func() bool { return len(ca.WorkObjectRefs(fakeslack.WOTaskID)) == 1 })
			e.woShot("work-object-conversations-" + mode)
		})
	}
}
