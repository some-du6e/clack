package messages

// TEMPORARY (branch patched-tree-clicks): a diagnostic for the Block Kit
// clicks a channel row loses under a plugin patching Message. It logs,
// frame by frame, the renders Go sends for each patched root, the trees JS
// delivers, the DOM events dispatched, and the vote row's override, host
// generation and button gesture state, then clicks the channel's buttons
// many times. Runs only with CLACK_VOTE_DIAG=1 (flake-check's
// patched-clicks job). Synthetic data only (internal/fakeslack).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

type diagLog struct {
	mu    sync.Mutex
	t     *testing.T
	label string
	last  map[uint64][]byte
}

func (d *diagLog) setLabel(l string) {
	d.mu.Lock()
	d.label = l
	d.mu.Unlock()
}

func (d *diagLog) logf(format string, args ...any) {
	d.mu.Lock()
	l := d.label
	d.mu.Unlock()
	d.t.Logf("%s [%s] %s", time.Now().Format("15:04:05.000"), l, fmt.Sprintf(format, args...))
}

func (d *diagLog) deliver(root uint64, tree []byte) {
	d.mu.Lock()
	same := bytes.Equal(d.last[root], tree)
	d.last[root] = append([]byte(nil), tree...)
	d.mu.Unlock()
	d.logf("deliver #%d same=%v %s", root, same, diagTree(tree))
}

type diagBridge struct {
	*taut.Bridge
	d *diagLog
}

func (b *diagBridge) Render(root uint64, name string, props []byte) {
	b.d.logf("render #%d %s %s", root, name, diagMsg(props))
	b.Bridge.Render(root, name, props)
}

func (b *diagBridge) RenderIn(root, parent uint64, parentK int64, name string, props []byte) {
	b.d.logf("renderIn #%d %s %s", root, name, diagMsg(props))
	b.Bridge.RenderIn(root, parent, parentK, name, props)
}

func (b *diagBridge) Unmount(root uint64) {
	b.d.logf("unmount #%d", root)
	b.Bridge.Unmount(root)
}

func (b *diagBridge) Dispatch(target int64, ev []byte) {
	var e struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(ev, &e)
	b.d.logf("dispatch %s k%d", e.Type, target)
	b.Bridge.Dispatch(target, ev)
}

// diagMsg is props' msg: its ts and text (the test's vote counts).
func diagMsg(props []byte) string {
	var p struct {
		Msg struct {
			TS   string `json:"ts"`
			Text string `json:"text"`
		} `json:"msg"`
	}
	_ = json.Unmarshal(props, &p)
	return fmt.Sprintf("ts=%s text=%q", p.Msg.TS, p.Msg.Text)
}

// diagTree lists a tree's native nodes with their msg's ts and text.
func diagTree(tree []byte) string {
	var v any
	if json.Unmarshal(tree, &v) != nil {
		return "?"
	}
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch x := x.(type) {
		case []any:
			for _, y := range x {
				walk(y)
			}
		case map[string]any:
			if x["t"] == "@" {
				s := fmt.Sprintf("@%v k%v", x["n"], x["k"])
				if p, ok := x["p"].(map[string]any); ok {
					if m, ok := p["msg"].(map[string]any); ok {
						s += fmt.Sprintf(" ts=%v text=%q", m["ts"], m["text"])
					}
				}
				if x["focus"] == true {
					s += " focus"
				}
				out = append(out, s)
			}
			walk(x["c"])
		}
	}
	walk(v)
	return strings.Join(out, "; ")
}

// diagPane is a votePane drawn on a clock shared by every pane (no
// instance is swept for a clock running behind).
type diagPane struct {
	*votePane
	d     *diagLog
	clk   *time.Time
	n     int
	ts    string
	bid   string
	state string
}

func (p *diagPane) frame() {
	p.n++
	p.d.setLabel(fmt.Sprintf("%s f%d", p.name, p.n))
	p.now = *p.clk
	p.votePane.frame()
	*p.clk = p.now
	p.probe()
}

func (p *diagPane) probe() {
	st := "no row"
	if r := p.s.rows[p.ts]; r != nil {
		el := r.bk.el
		var gen uint32
		var w any
		pr, hv := false, false
		if el != nil {
			gen, w, pr, hv = el.Probe(p.bid)
		}
		u, d := "u"+p.bid[1:], "d"+p.bid[1:]
		bs, _, ok := p.buttons(p.ts, u, d)
		st = fmt.Sprintf("msgOv=%x text=%q el=%p gen=%d %s w=%p pressed=%v hovered=%v at=%v drawn=%v",
			r.msgOv, r.m.Text, el, gen, p.bid, w, pr, hv, bs[p.bid], ok)
	}
	if st != p.state {
		p.state = st
		p.d.logf("%s", st)
	}
}

func (p *diagPane) hover(at image.Rectangle) {
	*p.clk = p.clk.Add(1100 * time.Millisecond)
	p.clock += 600 * time.Millisecond
	c := at.Min.Add(at.Max).Div(2)
	p.d.logf("move %v", c)
	p.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(c.X), float32(c.Y)), Time: p.clock})
	for i := 0; i < 10; i++ {
		p.frame()
		time.Sleep(10 * time.Millisecond)
	}
}

func (p *diagPane) click(at image.Rectangle) {
	c := at.Min.Add(at.Max).Div(2)
	pt := f32.Pt(float32(c.X), float32(c.Y))
	p.clock += 50 * time.Millisecond
	p.d.logf("press %v", c)
	p.r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt, Time: p.clock})
	p.frame()
	p.clock += 50 * time.Millisecond
	p.d.logf("release %v", c)
	p.r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Time: p.clock})
	for k := 0; k < 4; k++ {
		p.frame()
	}
}

func TestVoteClickDiag(t *testing.T) {
	if os.Getenv("CLACK_VOTE_DIAG") == "" {
		t.Skip("diagnostic: CLACK_VOTE_DIAG=1")
	}
	for i := 0; i < 3; i++ {
		t.Run(fmt.Sprint(i), voteDiag)
	}
}

func voteDiag(t *testing.T) {
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

	d := &diagLog{t: t, label: "setup", last: map[uint64][]byte{}}
	t.Logf("roots: channel root %s, vote %s; thread reply %s", rootTS, chanTS, replyTS)
	u := &pluginUI{trees: map[uint64][]byte{}}
	hooks := u.hooks()
	hooks.Attached = func(b *taut.Bridge) { slackc.SetBridge(&diagBridge{Bridge: b, d: d}) }
	deliver := hooks.Deliver
	hooks.Deliver = func(root uint64, tree []byte) {
		d.deliver(root, tree)
		deliver(root, tree)
	}
	tt := newTaut(t, app, hooks)
	slackc.SetBridge(&diagBridge{Bridge: tt.Bridge(), d: d})
	SetTaut(tt)
	t.Cleanup(func() { SetTaut(nil); slackc.SetBridge(nil); slackc.Reset(); slackc.SetPatched(nil) })
	install(t, tt, passThroughJS([]string{"ThreadReplyGeneric", "Message"}))
	until(t, 5*time.Second, "the components patched", func() {}, func() bool {
		return slackc.Patched("Message") && slackc.Patched("ThreadReplyGeneric")
	})

	h := coreHost{app: app, th: theme.New(false)}
	cv := NewChannelView(h, voteChan)
	cv.Preview = true
	tv := NewThreadView(h, voteChan, rootTS)
	clk := time.Now()
	ch := &diagPane{votePane: &votePane{name: "channel", s: cv.s, v: cv, size: image.Pt(1100, 700)}, d: d, clk: &clk, ts: chanTS, bid: "u1"}
	th := &diagPane{votePane: &votePane{name: "thread", s: tv.s, v: tv, size: image.Pt(520, 700)}, d: d, clk: &clk, ts: replyTS, bid: "u1"}
	panes := []*diagPane{ch, th}
	all := func() {
		for _, p := range panes {
			p.frame()
		}
	}
	drawn := func(n int) {
		t.Helper()
		ub, db := fmt.Sprintf("u%d", n), fmt.Sprintf("d%d", n)
		for _, p := range panes {
			p.bid = ub
		}
		until(t, 10*time.Second, fmt.Sprintf("revision %d drawn", n), all, func() bool {
			for _, p := range panes {
				if _, _, ok := p.buttons(p.ts, ub, db); !ok {
					return false
				}
			}
			return true
		})
		d.logf("=== revision %d drawn in both; settling", n)
		for i := 0; i < 30; i++ {
			all()
			time.Sleep(10 * time.Millisecond)
		}
	}
	lost := 0
	vote := func(p *diagPane, n int, kind string) {
		t.Helper()
		bid := fmt.Sprintf("%s%d", kind, n)
		ub, db := fmt.Sprintf("u%d", n), fmt.Sprintf("d%d", n)
		p.bid = bid
		bs, _, ok := p.buttons(p.ts, ub, db)
		if !ok {
			d.logf("NO BUTTONS for %s", bid)
			t.Errorf("%s: no %s to click", p.name, bid)
			return
		}
		p.hover(bs[bid])
		if bs, _, ok = p.buttons(p.ts, ub, db); !ok {
			d.logf("BUTTONS WENT AWAY under the pointer (%s)", bid)
			t.Errorf("%s: %s went away under the pointer", p.name, bid)
			return
		}
		sent := len(fs.BlockActionCalls())
		p.click(bs[bid])
		for dl := time.Now().Add(3 * time.Second); time.Now().Before(dl); {
			if len(fs.BlockActionCalls()) > sent {
				d.logf("CLICK OK %s", bid)
				return
			}
			p.frame()
			time.Sleep(15 * time.Millisecond)
		}
		lost++
		d.logf("CLICK LOST %s", bid)
		t.Errorf("%s: the click on %s sent nothing", p.name, bid)
	}
	update := func(n, up, down int) {
		d.logf("=== update to revision %d", n)
		voteChanged(app, voteMsg(chanTS, "", n, up, down))
		voteChanged(app, voteMsg(replyTS, rootTS, n, up, down))
	}

	drawn(1)
	update(2, 0, 1)
	drawn(2)
	d.logf("=== idle 40 frames")
	for i := 0; i < 40; i++ {
		all()
		time.Sleep(10 * time.Millisecond)
	}
	for i := 0; i < 5; i++ {
		vote(ch, 2, "u")
		vote(ch, 2, "d")
	}
	vote(th, 2, "u")
	vote(th, 2, "d")
	for n := 3; n <= 5; n++ {
		update(n, n, n)
		drawn(n)
		for i := 0; i < 3; i++ {
			vote(ch, n, "u")
			vote(ch, n, "d")
		}
		vote(th, n, "u")
		vote(th, n, "d")
	}
	t.Logf("lost %d clicks", lost)
}
