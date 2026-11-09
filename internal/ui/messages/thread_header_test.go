package messages

// The thread pane's header after "Thread" against internal/fakeslack's
// thread header fixtures (synthetic; nothing talks to Slack): Slack's
// threads Header names a channel with its icon (hash, filled lock,
// archive, " (archived)"), a DM with the partner's name and a group DM
// "Direct message with N others" with the members in a tooltip; a click
// is a link to the root's permalink (c/<id>/m/<root>), and the name hides
// once the primary view is that conversation. A conversation core
// doesn't know, or knows without a name (the bare "#" Karim saw on a
// thread opened from Activity), is looked up first: the header never
// draws a placeholder.

import (
	"context"
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

type headerHost struct {
	coreHost
	primary string
	routes  []string
}

func (h *headerHost) PrimaryChannel() string { return h.primary }
func (h *headerHost) Navigate(r string)      { h.routes = append(h.routes, r) }

func headerEnv(t *testing.T) (*core.App, *fakeslack.Server) {
	t.Helper()
	SetTaut(nil)
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	c := slack.New(fs.Creds())
	app := core.New(c, store.New(""))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- app.Boot(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("boot: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("boot timed out")
	}
	return app, fs
}

// headerFrames draws tv over a router; click clicks p.
func headerFrames(tv *ThreadView) (frame func(), click func(p image.Point)) {
	var r input.Router
	ops := new(op.Ops)
	now := time.Now()
	frame = func() {
		ops.Reset()
		now = now.Add(20 * time.Millisecond)
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(480, 700)),
			Now: now, Source: r.Source()}
		tv.Layout(gtx)
		r.Frame(ops)
	}
	var clock time.Duration
	click = func(p image.Point) {
		clock += 600 * time.Millisecond
		pt := f32.Pt(float32(p.X), float32(p.Y))
		r.Queue(
			pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Time: clock},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt, Time: clock + time.Millisecond},
			pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Time: clock + 2*time.Millisecond},
		)
		for k := 0; k < 4; k++ {
			frame()
		}
	}
	return frame, click
}

// waitHeader runs frames until the header names want, failing on any
// frame that draws a name without text.
func waitHeader(t *testing.T, tv *ThreadView, frame func(), want fakeslack.ThreadHeaderCase) image.Rectangle {
	t.Helper()
	var got HeaderSubtitle
	var r image.Rectangle
	deadline := time.Now().Add(15 * time.Second)
	for {
		frame()
		sub, rect, ok := tv.HeaderName()
		if ok && (sub.Text == "" || sub.Text == "#") {
			t.Fatalf("%s: the header drew %+v (a placeholder)", want.Name, sub)
		}
		got, r = sub, rect
		if ok && sub.Link && sub.Text == want.Text && sub.Icon == want.Icon && sub.Tip == want.Tip {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: the header shows %+v (shown %v), want %+v", want.Name, sub, ok, want)
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Logf("%s: %+v at %v", want.Name, got, r)
	return r
}

func TestThreadHeaderPerConversation(t *testing.T) {
	app, fs := headerEnv(t)
	for _, c := range fs.ThreadHeaderCases() {
		t.Run(c.Name, func(t *testing.T) {
			h := &headerHost{coreHost: coreHost{app: app, th: theme.New(false)}, primary: "C0SOMEWHERE"}
			tv := NewThreadView(h, c.Channel, fakeslack.ThreadRootTS)
			frame, click := headerFrames(tv)
			r := waitHeader(t, tv, frame, c)
			click(r.Min.Add(r.Size().Div(2)))
			want := "c/" + c.Channel + "/m/" + fakeslack.ThreadRootTS
			if len(h.routes) != 1 || h.routes[0] != want {
				t.Errorf("clicking the name navigated to %q, want [%s]", h.routes, want)
			}
			// the primary view now shows the conversation: ThreadSubtitle
			// hides the name
			h.primary = c.Channel
			frame()
			if sub, _, ok := tv.HeaderName(); ok {
				t.Errorf("the header still names %+v with its conversation in the primary view", sub)
			}
		})
	}
}

// A channel core holds without a name (a partial object) is looked up
// again; a channel it doesn't hold at all is looked up (Activity).
func TestThreadHeaderResolvesTheConversation(t *testing.T) {
	app, fs := headerEnv(t)
	cases := map[string]fakeslack.ThreadHeaderCase{}
	for _, c := range fs.ThreadHeaderCases() {
		cases[c.Name] = c
	}
	app.S.PutConv(slack.Conversation{ID: fakeslack.ThreadPublicID, IsChannel: true, IsMember: true})
	if cv, ok := app.Conv(fakeslack.ThreadPublicID); !ok || cv.Name != "" {
		t.Fatalf("seeding the nameless channel: %+v %v", cv, ok)
	}
	if _, ok := app.Conv(fakeslack.ThreadElsewhereID); ok {
		t.Fatal("the channel outside the viewer's conversations is already known")
	}
	for _, name := range []string{"public", "elsewhere", "archived"} {
		c := cases[name]
		h := &headerHost{coreHost: coreHost{app: app, th: theme.New(false)}, primary: "activity"}
		tv := NewThreadView(h, c.Channel, fakeslack.ThreadRootTS)
		frame, click := headerFrames(tv)
		r := waitHeader(t, tv, frame, c)
		click(r.Min.Add(r.Size().Div(2)))
		if want := "c/" + c.Channel + "/m/" + fakeslack.ThreadRootTS; len(h.routes) != 1 || h.routes[0] != want {
			t.Errorf("%s: clicking the name navigated to %q, want [%s]", name, h.routes, want)
		}
	}
	if cv, _ := app.Conv(fakeslack.ThreadPublicID); cv.Name != "th-launch-notes" {
		t.Errorf("the nameless member channel is still %+v", cv)
	}
	if _, member := app.S.Conv(fakeslack.ThreadPublicID); !member {
		t.Error("looking the member channel up took it out of the member set")
	}
}
