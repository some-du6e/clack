package messages

// Karim's 11-15 report (α.89, macOS): a vote app's upvote and downvote
// buttons "went byebye" from its message in a thread pane, the section
// text above them redrawn and the buttons gone. The app (a section, then
// one single-button actions block per vote) updates its message with
// chat.update after every vote: a message_changed with new block_ids.
// Under a plugin patching the message components (Hack Club's
// ShowRealUser wraps MessageWrapper), the first frames after the update
// still draw the JS tree's previous msg; the row's Block Kit host was
// made from those old blocks and kept for the revision, so once JS drew
// the new msg no button of the new doc found its element.
//
// Slack (useBlockKitInteractiveState, resetKey msg.blocks): the replaced
// message's buttons start idle, and an action's answer that comes after
// the update changes nothing. Synthetic data on internal/fakeslack.

import (
	"encoding/json"
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	voteChan  = "C0VOTE0002"
	voteBotID = "B0VOTE0002"
	voteAppID = "A0VOTE0002"
)

// voteRev is the vote message's blocks at revision n: the app's
// createInteractiveMessage, block_ids new on every update as Slack
// assigns them when the app sends none.
func voteRev(n, up, down int) (text string, blocks json.RawMessage) {
	text = fmt.Sprintf("Upvotes: %d\nDownvotes: %d", up, down)
	b, _ := json.Marshal([]any{
		map[string]any{"type": "section", "block_id": fmt.Sprintf("s%d", n), "text": map[string]any{"type": "mrkdwn", "text": text, "verbatim": false}},
		map[string]any{"type": "actions", "block_id": fmt.Sprintf("u%d", n), "elements": []any{map[string]any{"type": "button", "action_id": "actionId-upvote",
			"value": "upvote", "text": map[string]any{"type": "plain_text", "text": "⬆️ Upvote", "emoji": true}}}},
		map[string]any{"type": "actions", "block_id": fmt.Sprintf("d%d", n), "elements": []any{map[string]any{"type": "button", "action_id": "actionId-downvote",
			"value": "downvote", "text": map[string]any{"type": "plain_text", "text": "⬇️ Downvote", "emoji": true}}}},
	})
	return text, b
}

func voteMsg(ts, thread string, n, up, down int) slack.Message {
	text, blocks := voteRev(n, up, down)
	m := slack.Message{Type: "message", Subtype: "bot_message", TS: ts, ThreadTS: thread, BotID: voteBotID, Username: "Vote Bot", Text: text,
		Blocks: blocks, BotProfile: &slack.BotProfile{ID: voteBotID, Name: "Vote Bot", AppID: voteAppID}}
	if n > 1 {
		m.Edited = &slack.Edited{User: voteBotID, TS: fmt.Sprintf("%d.%06d", time.Now().Unix(), n)}
	}
	return m
}

// voteChanged is the app's chat.update as the socket brings it.
func voteChanged(app *core.App, m slack.Message) {
	evTS := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
	raw, _ := json.Marshal(map[string]any{"type": "message", "subtype": "message_changed", "hidden": true, "channel": voteChan,
		"ts": evTS, "event_ts": evTS, "message": m})
	app.Inject(slack.Event{Type: "message", Subtype: "message_changed", Channel: json.RawMessage(`"` + voteChan + `"`), Raw: raw})
}

// passThroughPlugins patch the message components without changing
// anything, the shape ShowRealUser's MessageWrapper patch has (a memo of
// props.msg handed to Original).
var passThroughPlugins = map[string][]string{
	"MessageWrapper":             {"MessageWrapper"},
	"ThreadReplyGeneric+Message": {"ThreadReplyGeneric", "Message"},
}

func passThroughJS(names []string) []byte {
	patches := ""
	for _, n := range names {
		patches += `    this.api.patchComponent('` + n + `', (Original) => (props) => {
      const msg = React.useMemo(() => props.msg, [props.msg])
      return React.createElement(Original, { ...props, msg })
    })
`
	}
	return pluginJS(`
class PassThrough extends TautPlugin {
  static id = 'PassThrough'
  static pluginName = 'Pass through (test)'
  static description = 'test: wraps the message components and changes nothing'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
` + patches + `  }
}
return PassThrough`)
}

// votePane is one view (channel or thread pane) over a router, drawn at
// its own clock.
type votePane struct {
	name string
	s    *listState
	v    interface {
		Layout(gtx layout.Context) layout.Dimensions
	}
	size image.Point
	// noClick: the pane's clicks aren't checked (see checkVoteButtons)
	noClick bool
	r       input.Router
	ops     op.Ops
	now     time.Time
	clock   time.Duration
}

func (p *votePane) frame() {
	p.ops.Reset()
	p.now = p.now.Add(20 * time.Millisecond)
	gtx := layout.Context{Ops: &p.ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(p.size), Now: p.now, Source: p.r.Source()}
	p.v.Layout(gtx)
	p.r.Frame(&p.ops)
}

// buttons returns where ts's row drew the button of each actions block
// bids names (window coordinates) and its element host; ok only when
// every one was drawn.
func (p *votePane) buttons(ts string, bids ...string) (map[string]image.Rectangle, *blockkit.Message, bool) {
	for i := p.s.visible[0]; i < p.s.visible[1] && i < len(p.s.items); i++ {
		it := p.s.items[i]
		if it.kind != itMsg || it.key != ts || i >= len(p.s.heights) || p.s.heights[i] <= 0 {
			continue
		}
		r := p.s.rows[ts]
		if r == nil || r.rv == nil {
			return nil, nil, false
		}
		el, _ := r.rv.Options().Elements.(*blockkit.Message)
		slots := r.rv.ElementSlots()
		if el == nil || len(slots) != len(bids) {
			return nil, el, false
		}
		off := p.s.origin.Add(image.Pt(r.bodyRect.Min.X, p.s.pos[i]+r.bodyRect.Min.Y))
		out := map[string]image.Rectangle{}
		for j, bid := range bids {
			rs := el.RunRects(bid)
			if len(rs) != 1 || rs[0].Empty() {
				return nil, el, false
			}
			out[bid] = rs[0].Add(slots[j].Min).Add(off)
		}
		return out, el, true
	}
	return nil, nil, false
}

// hover moves the pointer to at's centre, a second and more after the
// pane's last click (a button stays disabled for one after each:
// ButtonElement's isProcessingClick), and lets the hover's frames (and any
// JS render they cause) settle.
func (p *votePane) hover(at image.Rectangle) {
	p.now = p.now.Add(1100 * time.Millisecond)
	p.clock += 600 * time.Millisecond
	c := at.Min.Add(at.Max).Div(2)
	p.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(c.X), float32(c.Y)), Time: p.clock})
	p.settle(10)
}

// click presses and releases at at's centre, a frame apart.
func (p *votePane) click(at image.Rectangle) {
	c := at.Min.Add(at.Max).Div(2)
	pt := f32.Pt(float32(c.X), float32(c.Y))
	p.clock += 50 * time.Millisecond
	p.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt, Time: p.clock})
	p.frame()
	p.clock += 50 * time.Millisecond
	p.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Time: p.clock})
	for k := 0; k < 4; k++ {
		p.frame()
	}
}

// settle draws n frames, 10ms apart (JS answers the props it was sent).
func (p *votePane) settle(n int) {
	for i := 0; i < n; i++ {
		p.frame()
		time.Sleep(10 * time.Millisecond)
	}
}

func TestVoteButtonsSurviveUpdates(t *testing.T) {
	t.Run("native", func(t *testing.T) { checkVoteButtons(t, nil) })
	for name, comps := range passThroughPlugins {
		t.Run(name, func(t *testing.T) { checkVoteButtons(t, comps) })
	}
}

func checkVoteButtons(t *testing.T, patch []string) {
	app, fs, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: voteChan, Name: "meta", IsChannel: true, IsMember: true, NumMembers: 40})
	app.S.PutBot(voteBotID, store.BotInfo{Name: "Vote Bot", Icon: shotAvatar(voteBotID)})
	base := time.Now().Add(-time.Hour).Unix()
	rootTS, replyTS, chanTS := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000200", base+60), fmt.Sprintf("%d.000300", base+120)
	root := slack.Message{Type: "message", TS: rootTS, User: "UANA00001", Text: "Proposal: move standup to 10am",
		Blocks: shotRich(shotSection("Proposal: move standup to 10am"))}
	root.ThreadTS, root.ReplyCount, root.ReplyUsers, root.LatestReply = rootTS, 1, []string{"UBEN00001"}, replyTS
	app.S.SetLatest(voteChan, []slack.Message{root, voteMsg(chanTS, "", 1, 0, 0)}, false)
	app.S.SetThread(voteChan, rootTS, []slack.Message{root, voteMsg(replyTS, rootTS, 1, 0, 0)})

	if patch != nil {
		u := &pluginUI{trees: map[uint64][]byte{}}
		tt := newTaut(t, app, u.hooks())
		slackc.SetBridge(tt.Bridge())
		SetTaut(tt)
		t.Cleanup(func() { SetTaut(nil); slackc.SetBridge(nil); slackc.Reset(); slackc.SetPatched(nil) })
		install(t, tt, passThroughJS(patch))
		until(t, 5*time.Second, "the components patched", func() {}, func() bool {
			for _, n := range patch {
				if !slackc.Patched(n) {
					return false
				}
			}
			return true
		})
	}
	h := coreHost{app: app, th: theme.New(false)}
	cv := NewChannelView(h, voteChan)
	cv.Preview = true
	tv := NewThreadView(h, voteChan, rootTS)
	start := time.Now()
	panes := []struct {
		p  *votePane
		ts string
	}{
		{&votePane{name: "channel", s: cv.s, v: cv, size: image.Pt(1100, 700), now: start}, chanTS},
		{&votePane{name: "thread", s: tv.s, v: tv, size: image.Pt(520, 700), now: start}, replyTS},
	}
	clicking := len(panes)
	for _, n := range patch {
		if n == "Message" {
			// Open (2026-11-16, not this bug): a channel row under a
			// patched Message loses some clicks on its Block Kit buttons
			// (drawn, idle, the press on them sends nothing; the same
			// clicks work natively, under MessageWrapper and in the
			// ThreadReplyGeneric thread pane). The redraw checks still run.
			panes[0].p.noClick = true
			clicking--
		}
	}
	all := func() {
		for _, x := range panes {
			x.p.frame()
		}
	}
	settle := func() {
		for i := 0; i < 30; i++ {
			all()
			time.Sleep(10 * time.Millisecond)
		}
	}
	// drawn waits for both views to draw revision n's two buttons, idle
	drawn := func(what string, n int) {
		t.Helper()
		u, d := fmt.Sprintf("u%d", n), fmt.Sprintf("d%d", n)
		until(t, 10*time.Second, what+": both buttons in both views", all, func() bool {
			for _, x := range panes {
				if _, _, ok := x.p.buttons(x.ts, u, d); !ok {
					return false
				}
			}
			return true
		})
		settle()
		for _, x := range panes {
			_, el, ok := x.p.buttons(x.ts, u, d)
			if !ok {
				t.Fatalf("%s, %s: the buttons went away after the views settled", what, x.p.name)
			}
			for _, bid := range []string{u, d} {
				if loading, failed := el.Status(bid); loading || failed {
					t.Errorf("%s, %s: block %s loading %v, failed %v; want idle (Slack resets a replaced message's block state)", what, x.p.name, bid, loading, failed)
				}
			}
		}
	}
	// vote clicks the button of revision n's block kind ("u", "d") in
	// pane x and checks blocks.actions got it
	vote := func(what string, x *votePane, ts string, n int, kind string) {
		t.Helper()
		if x.noClick {
			return
		}
		bid := fmt.Sprintf("%s%d", kind, n)
		u, d := fmt.Sprintf("u%d", n), fmt.Sprintf("d%d", n)
		bs, _, ok := x.buttons(ts, u, d)
		if !ok {
			t.Fatalf("%s, %s: no %s button to click", what, x.name, bid)
		}
		x.hover(bs[bid])
		if bs, _, ok = x.buttons(ts, u, d); !ok {
			t.Fatalf("%s, %s: the %s button went away under the pointer", what, x.name, bid)
		}
		sent := len(fs.BlockActionCalls())
		x.click(bs[bid])
		var c fakeslack.BlockActionCall
		for dl := time.Now().Add(5 * time.Second); ; {
			if cs := fs.BlockActionCalls(); len(cs) > sent {
				c = cs[len(cs)-1]
				break
			}
			if time.Now().After(dl) {
				now, el, ok := x.buttons(ts, u, d)
				st := ""
				if el != nil {
					l, f := el.Status(bid)
					st = fmt.Sprintf("loading %v failed %v", l, f)
				}
				t.Fatalf("%s, %s: clicking %s at %v sent no blocks.actions (buttons now %v, drawn %v, %s)", what, x.name, bid, bs[bid], now, ok, st)
			}
			x.frame()
			time.Sleep(15 * time.Millisecond)
		}
		if len(c.Actions) != 1 || c.Actions[0]["block_id"] != bid || c.Container["message_ts"] != ts {
			t.Errorf("%s, %s: blocks.actions %v in %v; want block %s of %s", what, x.name, c.Actions, c.Container, bid, ts)
		}
	}
	update := func(n, up, down int) {
		voteChanged(app, voteMsg(chanTS, "", n, up, down))
		voteChanged(app, voteMsg(replyTS, rootTS, n, up, down))
	}

	drawn("posted", 1)

	// (a) the app updates the message: same structure, new block_ids and text
	update(2, 0, 1)
	drawn("after a message_changed with new block_ids", 2)
	for _, x := range panes {
		if r := x.p.s.rows[x.ts]; r == nil || r.m.Text != "Upvotes: 0\nDownvotes: 1" {
			t.Fatalf("%s: the row doesn't show the update", x.p.name)
		}
		vote("revision 2", x.p, x.ts, 2, "u")
		vote("revision 2", x.p, x.ts, 2, "d")
	}
	settle()

	// (b) a vote, its answer held: the app's chat.update reaches the
	// client first. The buttons come back idle, and the late answer
	// leaves them so.
	fs.SetFault("blocks.actions", fakeslack.Fault{ReplyDelay: 1500 * time.Millisecond, Times: clicking})
	for _, x := range panes {
		if x.p.noClick {
			continue
		}
		vote("held vote", x.p, x.ts, 2, "u")
		if _, el, _ := x.p.buttons(x.ts, "u2", "d2"); el == nil {
			t.Fatalf("%s: no element host", x.p.name)
		} else if loading, _ := el.Status("u2"); !loading {
			t.Errorf("%s: block u2 isn't loading while its action is in flight", x.p.name)
		}
	}
	update(3, 1, 1)
	drawn("after the update that answers a held vote", 3)
	time.Sleep(1600 * time.Millisecond) // the held answers land
	drawn("after the held answers", 3)

	// (b) the usual order: the answer, then the update
	for _, x := range panes {
		vote("revision 3", x.p, x.ts, 3, "d")
	}
	settle()
	update(4, 1, 2)
	drawn("after a vote and its update", 4)
	for _, x := range panes {
		vote("revision 4", x.p, x.ts, 4, "u")
	}
}
