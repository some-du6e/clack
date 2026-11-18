package ui

// Diagnostic for hover-toolbar-pos (temporary, CI only): where the hover
// toolbar is drawn against the hovered row in each route Karim's report
// points at, on fakeslack's thread header fixtures and on an offline
// workspace under ChatTabs (Slick's, or a plugin of the same shape)
// (synthetic; nothing talks to Slack).

import (
	"fmt"
	"image"
	"os"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

type toolbarLister interface {
	RowRects() []messages.RowRect
	HoverToolbar() (string, image.Rectangle, bool)
	ListRect() image.Rectangle
}

// toolbarSweep hovers each visible row of l at six points (move puts the
// pointer there and draws a few frames) and logs where the toolbar is,
// returning how many hovers drew it away from the row under the pointer.
func toolbarSweep(t *testing.T, route string, l toolbarLister, move func(p f32.Point)) int {
	if l == nil {
		t.Errorf("%s: no list", route)
		return 1
	}
	bad := 0
	lr := l.ListRect()
	rows := l.RowRects()
	t.Logf("%s: list %v, rows %d", route, lr, len(rows))
	for _, r := range rows {
		t.Logf("%s:   row %s %v", route, r.TS, r.R)
	}
	for _, r := range rows {
		for _, fx := range []float32{0.5, 0.9} {
			for _, fy := range []float32{0.1, 0.5, 0.9} {
				p := f32.Pt(float32(r.R.Min.X)+fx*float32(r.R.Dx()), float32(r.R.Min.Y)+fy*float32(r.R.Dy()))
				if !image.Pt(int(p.X), int(p.Y)).In(lr) {
					continue
				}
				move(p)
				ts, card, ok := l.HoverToolbar()
				now := ""
				var nr image.Rectangle
				for _, q := range l.RowRects() {
					if image.Pt(int(p.X), int(p.Y)).In(q.R) {
						now, nr = q.TS, q.R
					}
				}
				dy := card.Min.Y - nr.Min.Y
				okPos := ok && ts == now && (dy == -16 || dy == -28 || dy == 4 || card.Min.Y == lr.Min.Y+4)
				mark := ""
				if !okPos {
					mark = "  <-- MISPLACED"
					bad++
				}
				t.Logf("%s: hover %s at (%.0f,%.0f) row-now %s %v: toolbar for %s card %v ok %v dy %d%s", route, r.TS, p.X, p.Y, now, nr, ts, card, ok, dy, mark)
			}
		}
	}
	return bad
}

func TestHoverToolbarDiag(t *testing.T) {
	if os.Getenv("HOVER_TOOLBAR_DIAG") == "" {
		t.Skip("HOVER_TOOLBAR_DIAG not set")
	}
	e := threadHeaderEnv(t, false)
	e.size = image.Pt(1918, 1080)
	ch := fakeslack.ThreadPublicID
	var pub fakeslack.ThreadHeaderCase
	for _, c := range e.fs.ThreadHeaderCases() {
		if c.Channel == ch {
			pub = c
		}
	}
	other := e.fs.ChannelIDs()[0]
	users := e.fs.UserIDs()[1:4]
	for n := 0; n < 40; n++ {
		_, ev := e.fs.PostQuiet(ch, users[n%len(users)], fmt.Sprintf("a synthetic newer message %d", n))
		e.fs.Push(ev)
	}
	e.settle(300 * time.Millisecond)
	root := fakeslack.ThreadRootTS
	chList := func() toolbarLister {
		if l, ok := e.a.convoFor(ch, "").list.(channelList); ok {
			return l
		}
		return nil
	}
	rootVisible := func() bool {
		l := chList()
		if l == nil {
			return false
		}
		for _, r := range l.RowRects() {
			if r.TS == root {
				return true
			}
		}
		return false
	}
	clock := 10 * time.Second
	move := func(p f32.Point) {
		clock += 50 * time.Millisecond
		e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: clock})
		e.frame()
		e.frame()
		e.frame()
	}
	bad := 0
	sweep := func(route string, l toolbarLister) {
		t.Logf("%s: route %s, secondary %v %s/%s", route, e.a.router.Current(), e.a.secondary.kind, e.a.secondary.channel, e.a.secondary.ts)
		bad += toolbarSweep(t, route, l, move)
	}

	// 1. Activity: the thread beside the inbox at a reply (OpenThreadAt,
	// what an Activity thread item opens), then the thread header's
	// channel link: the channel at the root (c/<id>/m/<root>), the
	// thread pane staying open
	e.a.Navigate("activity")
	e.settle(800 * time.Millisecond)
	e.a.OpenThreadAt(ch, root, fakeslack.ThreadReplyTS(1))
	hr := e.waitThreadHeader(pub)
	clock += time.Second
	e.clickAt(hr.Min.Add(hr.Size().Div(2)), clock)
	want := Route("c/" + ch + "/m/" + root)
	e.until("the click navigates", 5*time.Second, func() bool { return e.a.router.Current() == want })
	e.until("the root on screen", 10*time.Second, rootVisible)
	e.settle(600 * time.Millisecond)
	sweep("activity-thread-link", chList())

	// 2. the same with the thread pane closed
	e.a.CloseSecondary()
	e.settle(600 * time.Millisecond)
	sweep("activity-thread-link-closed", chList())

	// 3. a permalink from another channel
	e.openChannel(other)
	e.settle(400 * time.Millisecond)
	e.a.Navigate("c/" + ch + "/m/" + root)
	e.until("the root on screen (permalink)", 10*time.Second, rootVisible)
	e.settle(600 * time.Millisecond)
	sweep("permalink", chList())

	// 4. the channel at its newest
	e.openChannel(other)
	e.settle(300 * time.Millisecond)
	e.openChannel(ch)
	e.toLatest()
	sweep("newest", chList())

	// 5. the thread pane
	e.a.OpenThread(ch, root)
	e.until("the thread", 10*time.Second, func() bool {
		tv := e.threadView(ch, root)
		return tv != nil && len(tv.RowRects()) >= 3
	})
	e.settle(600 * time.Millisecond)
	if tv := e.threadView(ch, root); tv != nil {
		sweep("thread-pane", tv)
	}

	if bad > 0 {
		t.Errorf("%d hovers drew the toolbar away from the hovered row", bad)
	}
}

// tabsShapePlugin is ChatTabs' layout around the primary view
// (src/plugins/ChatTabs/index.tsx: ClientPrimaryView in a display:contents
// .slick-chat-tabs-layout, the bar handed to RenderedViewContents as its
// first child, absolute at the top, and the view padded under it).
const tabsShapePlugin = `// slick-plugin {"id":"TabsShape"}
(globalThis.__slickPlugins ??= []).push((require) => {
var import_slick = require("$slick");
class P extends import_slick.SlickPlugin {
  static id = "TabsShape";
  static pluginName = "TabsShape";
  static description = "test: ChatTabs' layout around the primary view";
  static requiredAPIs = [];
  start() {
    this.api.setStyle(".tabs-shape-layout { display:contents; } .tabs-shape-layout .p-view_contents--primary:has(> .tabs-shape) { padding-top: 37px; } .tabs-shape { position:absolute; top:0; left:0; right:0; z-index:1; height:36px; background:#fff; border-bottom:1px solid #8885; }");
    const Bar = React.createContext(undefined);
    this.api.patchComponent("ClientPrimaryView", (Original) => (props) =>
      React.createElement("div", { className: "tabs-shape-layout" },
        React.createElement(Bar.Provider, { value: React.createElement("div", { className: "tabs-shape" }, "tabs") },
          React.createElement(Original, props))));
    this.api.patchComponent("RenderedViewContents", (Original) => (props) => {
      const bar = React.useContext(Bar);
      if (bar === undefined) return React.createElement(Original, props);
      return React.createElement(Original, props, bar, React.createElement(Bar.Provider, { value: undefined }, props.children));
    });
  }
}
return P;
});
`

func TestHoverToolbarDiagChatTabs(t *testing.T) {
	if os.Getenv("HOVER_TOOLBAR_DIAG") == "" {
		t.Skip("HOVER_TOOLBAR_DIAG not set")
	}
	dir := os.Getenv("SLICK_PLUGINS_DIR")
	e := newSVIn(t, dir, func(home string, a *App, ca *core.App) {
		var msgs []slack.Message
		for i := 0; i < 60; i++ {
			ts := fmt.Sprintf("%d.000100", 1700000100+i*60)
			m := slack.Message{Type: "message", TS: ts, User: []string{svAlice, svBob}[i%2],
				Text: fmt.Sprintf("message %d in general for the toolbar", i)}
			if i == 30 {
				m.ThreadTS, m.ReplyCount, m.ReplyUsers, m.LatestReply = ts, 2, []string{svBob}, fmt.Sprintf("%d.000200", 1700000100+59*60)
			}
			msgs = append(msgs, m)
		}
		ca.S.SetLatest(ttGen, msgs, false)
	})
	e.sz = image.Pt(1918, 1080)
	if dir != "" {
		e.install("ChatTabs", nil)
	} else {
		e.installCode("TabsShape", tabsShapePlugin)
	}
	e.until(8*time.Second, "the primary view patched", func() bool {
		return slackc.Patched("ClientPrimaryView") && slackc.Patched("RenderedViewContents")
	})
	root := fmt.Sprintf("%d.000100", 1700000100+30*60)
	chList := func() toolbarLister {
		if l, ok := e.a.convoFor(ttGen, "").list.(channelList); ok {
			return l
		}
		return nil
	}
	var clock time.Duration
	move := func(p f32.Point) {
		clock += 50 * time.Millisecond
		e.rt.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: clock})
		e.frame()
		e.frame()
		e.frame()
	}
	for i := 0; i < 30; i++ {
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	bad := toolbarSweep(t, "chattabs-newest", chList(), move)
	e.a.Navigate("c/" + ttGen + "/m/" + root)
	for i := 0; i < 30; i++ {
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	bad += toolbarSweep(t, "chattabs-permalink", chList(), move)
	if bad > 0 {
		t.Errorf("%d hovers drew the toolbar away from the hovered row", bad)
	}
}
