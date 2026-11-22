package views

import (
	"image"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The views draw a message's reactions with Slack's ReactionBar, as a
// channel does (messages.ClickReactionGroup): a pill's click toggles the
// viewer's reaction in the next frame, before reactions.add answers, and
// a reactions.remove that fails comes back with Slack's toast; the pill
// has .c-reaction:hover. Without canReact (a locked thread's messages:
// canAddReaction) the bar has no add button and its pills no button.

// rxFake answers reactions.add / reactions.remove for a view's test:
// recording each, holding each answer while hold is open, failing it
// while fail is set.
type rxFake struct {
	mu    sync.Mutex
	calls []string // "add <name> <ch>/<ts>"
	hold  chan struct{}
	fail  bool
}

func (x *rxFake) hook(next func(string, url.Values) (any, bool)) func(string, url.Values) (any, bool) {
	return func(method string, f url.Values) (any, bool) {
		if method != "reactions.add" && method != "reactions.remove" {
			if next != nil {
				return next(method, f)
			}
			return nil, false
		}
		x.mu.Lock()
		x.calls = append(x.calls, strings.TrimPrefix(method, "reactions.")+" "+f.Get("name")+" "+f.Get("channel")+"/"+f.Get("timestamp"))
		hold, fail := x.hold, x.fail
		x.mu.Unlock()
		if hold != nil {
			<-hold
		}
		if fail {
			return map[string]any{"ok": false, "error": "too_many_reactions"}, true
		}
		return map[string]any{"ok": true}, true
	}
}

func (x *rxFake) sent(call string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return slices.Contains(x.calls, call)
}

func (x *rxFake) n() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return len(x.calls)
}

// rxCase is one view's bar: b draws it in e; drawn is the view's copy of
// the message; toasts the host's.
type rxCase struct {
	t      *testing.T
	e      *vaEnv
	b      *base
	x      *rxFake
	drawn  func() *core.Message
	toasts func() []string
	ch, ts string
}

func (c *rxCase) key() string { return "message-" + c.ts + "-" + c.ch }

// pill is the middle of reaction name's pill, in window coordinates.
func (c *rxCase) pill(name string) (f32.Point, bool) {
	btn := rxPillBtn(c.b, c.key()+":"+name)
	if btn == nil {
		return f32.Point{}, false
	}
	r, _, ok := c.e.r.AreaBounds(btn)
	if !ok {
		return f32.Point{}, false
	}
	m := mid(r)
	return f32.Pt(float32(m.X)+.5, float32(m.Y)+.5), true
}

func (c *rxCase) click(p f32.Point) {
	c.e.r.Queue(asEvents([]pointer.Event{{Kind: pointer.Move, Source: pointer.Mouse, Position: p},
		{Kind: pointer.Press, Source: pointer.Mouse, Position: p, Buttons: pointer.ButtonPrimary},
		{Kind: pointer.Release, Source: pointer.Mouse, Position: p}})...)
	c.e.frame()
	c.e.frame()
}

// reacted reports whether the view draws the viewer's name reaction.
func (c *rxCase) reacted(name string) bool {
	m := c.drawn()
	return m != nil && rxReacted(c.b, m, name)
}

// check toggles name (not the viewer's yet) on and, failing, off.
func (c *rxCase) check(name string) {
	t := c.t
	t.Helper()
	if c.reacted(name) {
		t.Fatalf("the viewer reacted %s already", name)
	}
	p, ok := c.pill(name)
	if !ok {
		t.Fatalf("the %s pill has no button", name)
	}
	// hovered: .c-reaction:hover
	c.e.r.Queue(asEvents([]pointer.Event{{Kind: pointer.Move, Source: pointer.Mouse, Position: p}})...)
	c.e.frame()
	if !rxPillBtn(c.b, c.key()+":"+name).Hovered() {
		t.Errorf("the %s pill isn't hovered under the pointer", name)
	}
	// a click adds it at once, before reactions.add answers
	hold := make(chan struct{})
	c.x.mu.Lock()
	c.x.hold = hold
	c.x.mu.Unlock()
	c.click(p)
	add := "add " + name + " " + c.ch + "/" + c.ts
	c.e.until(t, "reactions.add", func() bool { return c.x.sent(add) })
	c.e.until(t, "the pill reacted while reactions.add is out", func() bool { return c.reacted(name) })
	c.x.mu.Lock()
	c.x.hold = nil
	c.x.mu.Unlock()
	close(hold)
	time.Sleep(50 * time.Millisecond)
	c.e.frame()
	if !c.reacted(name) {
		t.Errorf("reactions.add answered ok and the %s pill isn't reacted", name)
	}
	// a remove that fails: off at once, then back with Slack's toast
	c.x.mu.Lock()
	c.x.fail = true
	c.x.mu.Unlock()
	n := len(c.toasts())
	if p, ok = c.pill(name); !ok {
		t.Fatalf("the reacted %s pill has no button", name)
	}
	c.click(p)
	c.e.until(t, "reactions.remove", func() bool { return c.x.sent("remove " + name + " " + c.ch + "/" + c.ts) })
	c.e.until(t, "the failure's toast", func() bool {
		ts := c.toasts()
		return len(ts) > n && ts[len(ts)-1] == "Couldn't update reaction"
	})
	c.e.until(t, "the pill reacted again", func() bool { return c.reacted(name) })
	c.x.mu.Lock()
	c.x.fail = false
	c.x.mu.Unlock()
}

func TestThreadsReactionPills(t *testing.T) {
	const root, reply, locked = "1762690000.000100", "1762693000.000100", "1762680000.000100"
	ch := fakeslack.ViewsChannel
	app, fs := vaFake(t)
	r := slack.Message{Type: "message", Channel: ch, TS: root, ThreadTS: root, User: "U0FAKE00001", Text: "a synthetic root people reacted to",
		Reactions: []slack.Reaction{{Name: "thumbsup", Count: 1, Users: []string{"U0FAKE00002"}}}}
	r.ReplyCount, r.ReplyUsers, r.LatestReply = 1, []string{"U0FAKE00002"}, reply
	x := slack.Message{Type: "message", Channel: ch, TS: reply, ThreadTS: root, User: "U0FAKE00002", Text: "a synthetic reply"}
	// a thread whose replies are turned off: its messages take no reaction
	l := slack.Message{Type: "message", Channel: ch, TS: locked, ThreadTS: locked, User: "U0FAKE00001", Text: "a synthetic locked root",
		IsLocked: true, Reactions: []slack.Reaction{{Name: "eyes", Count: 1, Users: []string{"U0FAKE00002"}}}}
	l.ReplyCount, l.ReplyUsers, l.LatestReply = 1, []string{"U0FAKE00002"}, locked[:10]+".000200"
	lr := slack.Message{Type: "message", Channel: ch, TS: l.LatestReply, ThreadTS: locked, User: "U0FAKE00002", Text: "a synthetic reply before the lock"}
	rx := &rxFake{}
	fs.SetHook(rx.hook(func(method string, _ url.Values) (any, bool) {
		if method == "subscriptions.thread.getView" {
			return thrOrderAnswer(
				map[string]any{"root_msg": r, "latest_replies": []slack.Message{x}, "unread_replies": []slack.Message{x}},
				map[string]any{"root_msg": l, "latest_replies": []slack.Message{lr}, "unread_replies": []slack.Message{lr}}), true
		}
		return nil, false
	}))
	h := newKBHost(app, false)
	v := NewThreads(h)
	e := thrViewEnv(t, v, 2)
	e.frame()
	e.frame()
	msgOf := func(root, ts string) *core.Message {
		if card := v.cards[ch+"/"+root]; card != nil && card.msgs[ts] != nil {
			return card.msgs[ts].m
		}
		return nil
	}
	c := &rxCase{t: t, e: e, b: &v.base, x: rx, toasts: h.toasts, ch: ch, ts: root,
		drawn: func() *core.Message { return msgOf(root, root) }}
	if _, _, ok := e.r.AreaBounds(v.addBtn(&core.Message{Channel: ch, TS: root})); !ok {
		t.Error("the root's bar has no add button")
	}
	c.check("thumbsup")

	// the locked thread's root: no add button, a pill without a button
	lc := &rxCase{t: t, e: e, b: &v.base, x: rx, ch: ch, ts: locked}
	if msgOf(locked, locked) == nil {
		t.Fatal("the locked thread's root isn't drawn")
	}
	if _, _, ok := e.r.AreaBounds(v.addBtn(&core.Message{Channel: ch, TS: locked})); ok {
		t.Error("a locked thread's root has an add button")
	}
	if _, ok := lc.pill("eyes"); ok {
		t.Error("a locked thread's pill takes clicks")
	}
	if !app.ThreadLocked(ch, locked) {
		t.Error("the store's root isn't locked: the test checks nothing")
	}
}

func TestUnreadsReactionPills(t *testing.T) {
	ch := fakeslack.UnreadsDesignID
	rx := &rxFake{}
	var ts string
	app, _ := unrFakeWith(t, nil, func(fs *fakeslack.Server) {
		ts = fs.PutUnreadMessage(ch, slack.Message{User: "U0FAKE00002", Text: "a synthetic message people liked",
			Reactions: []slack.Reaction{{Name: "tada", Count: 1, Users: []string{"U0FAKE00001"}}}})
		fs.SetHook(rx.hook(nil))
	})
	h := &vaHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	v := NewUnreads(h)
	e := &vaEnv{size: image.Pt(1100, 900)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	e.until(t, "the frames", func() bool {
		if v.built && len(v.rows) > 0 && v.rows[len(v.rows)-1].kind == unrFooter {
			return true
		}
		v.list.ScrollTo(len(v.rows))
		return false
	})
	hr, ok := unrHead(v, ch)
	if !ok {
		t.Fatalf("no frame for %s", ch)
	}
	v.list.ScrollTo(v.headerRow(hr.f))
	drawn := func() *core.Message {
		for _, r := range v.rows {
			if r.kind == unrMsgRow && r.msg != nil && r.msg.Channel == ch && r.msg.TS == ts {
				return r.msg
			}
		}
		return nil
	}
	c := &rxCase{t: t, e: e, b: &v.base, x: rx, toasts: h.toasts, ch: ch, ts: ts, drawn: drawn}
	e.until(t, "the message's pill", func() bool { _, ok := c.pill("tada"); return ok })
	c.check("tada")
}

// The Pins tab: a pill's click toggles it and doesn't open the pin
// (Reaction's click stops there).
func TestPinsReactionPills(t *testing.T) {
	app, fs := pfCore(t)
	rx := &rxFake{}
	fs.SetHook(rx.hook(nil))
	h := pfHostFor(t, app, fs, false)
	v := NewChannelPins(h, fakeslack.PinsChannelID)
	e := &vaEnv{size: image.Pt(1100, 900)}
	pfDraw(e, h, v.Layout)
	e.until(t, "the pins", func() bool { return v.Loaded() && len(v.Items()) == 4 })
	ts := fakeslack.PinsTS(2)
	drawn := func() *core.Message {
		for _, it := range v.Items() {
			if it.Msg != nil && it.Msg.TS == ts {
				return it.Msg
			}
		}
		return nil
	}
	c := &rxCase{t: t, e: e, b: &v.base, x: rx, toasts: h.toasts, ch: fakeslack.PinsChannelID, ts: ts, drawn: drawn}
	e.until(t, "the pin's pill", func() bool { _, ok := c.pill("white_check_mark"); return ok })
	c.check("white_check_mark")
	if len(h.pinned) != 0 {
		t.Errorf("a click on a pin's reaction opened %v", h.pinned)
	}
}

// Activity's reaction row: a pill's click toggles it and doesn't open the
// row (ControlledClick skips a BUTTON target).
func TestActivityReactionPills(t *testing.T) {
	v, h, e, fs := vaFakeActivity(t, false)
	rx := &rxFake{}
	fs.SetHook(rx.hook(nil))
	const key, ts = "fa-reaction", "1762700500.000100"
	r := v.rowFor(key)
	c := &rxCase{t: t, e: e, b: &v.base, x: rx, toasts: h.toasts, ch: fakeslack.ViewsChannel, ts: ts, drawn: r.msg}
	e.until(t, "the row's pill", func() bool { _, ok := c.pill("tada"); return ok })
	c.check("tada")
	if v.selOK && v.selKey == key {
		t.Error("a click on the row's reaction opened the row")
	}
}

// pointerAt moves the pointer to p and reports the window's cursor.
func pointerAt(e *vaEnv, p f32.Point) pointer.Cursor {
	e.r.Queue(asEvents([]pointer.Event{{Kind: pointer.Move, Source: pointer.Mouse, Position: p}})...)
	e.frame()
	return e.r.Cursor()
}

// A link's click in an Activity row opens the link and not the row
// (ControlledClick skips an A target); in a pin it opens both (the pin is
// a plain UnstyledButton and BaseMrkdwnLink doesn't stop the click). Body
// text in both rows is under the row's hand (cursor: pointer), in the
// Threads view the I-beam.
func TestRowLinkClicksAndCursorLikeSlack(t *testing.T) {
	t.Run("activity", func(t *testing.T) {
		const key, ts = "fa-reaction", "1762700500.000100"
		ch := fakeslack.ViewsChannel
		// the row's message (messages.list's) carries a link
		v, h, e, _ := vaFakeActivityWith(t, false, func(fs *fakeslack.Server) {
			fs.SetActivityMessage(slack.Message{Type: "message", Channel: ch, TS: ts, User: fakeslack.SelfID,
				Text:      "a synthetic message people liked, see <" + rhLink + "|the doc> for more words to fill a line",
				Reactions: []slack.Reaction{{Name: "tada", Count: 2, Users: []string{"U0FAKE00001", "U0FAKE00002"}}}})
		})
		body := func() *richEntry {
			for k, ent := range v.rich {
				if strings.HasSuffix(k, ch+"/"+ts) && len(ent.v.LinkRects()) > 0 {
					return ent
				}
			}
			return nil
		}
		e.until(t, "the edited row's link", func() bool { return body() != nil })
		e.frame()
		ent := body()
		full, _, ok := e.r.AreaBounds(ent.v)
		if !ok {
			t.Fatal("the row's body has no area")
		}
		at := func(p image.Point) f32.Point { q := full.Min.Add(p); return f32.Pt(float32(q.X)+.5, float32(q.Y)+.5) }
		if cur := pointerAt(e, at(textPoint(t, ent.v))); cur != pointer.CursorPointer {
			t.Errorf("the cursor over an Activity row's text is %v, want the row's hand", cur)
		}
		// the host has no browser: a link it follows is its toast
		n := len(h.toasts())
		link := at(mid(ent.v.LinkRects()[0].R))
		c := &rxCase{e: e}
		c.click(link)
		if ts := h.toasts()[n:]; len(ts) != 1 || ts[0] != rhLink {
			t.Errorf("the link's click opened %v", ts)
		}
		if v.selOK && v.selKey == key {
			t.Error("a click on the row's link opened the row")
		}
	})
	t.Run("pins", func(t *testing.T) {
		app, fs := pfCore(t)
		// the pin carries a link before the tab reads pins.list
		ts := fakeslack.PinsTS(2)
		if !fs.EditLive(fakeslack.PinsChannelID, ts, "The launch checklist is final, see <"+rhLink+"|the doc> before you edit it") {
			t.Fatal("the fake doesn't hold the pin")
		}
		h := pfHostFor(t, app, fs, false)
		v := NewChannelPins(h, fakeslack.PinsChannelID)
		e := &vaEnv{size: image.Pt(1100, 900)}
		pfDraw(e, h, v.Layout)
		e.until(t, "the pins", func() bool { return v.Loaded() && len(v.Items()) == 4 })
		ent := func() *richEntry { return v.rich["pane:"+fakeslack.PinsChannelID+"/"+ts] }
		e.until(t, "the edited pin's link", func() bool { return ent() != nil && len(ent().v.LinkRects()) > 0 })
		e.frame()
		full, _, ok := e.r.AreaBounds(ent().v)
		if !ok {
			t.Fatal("the pin's body has no area")
		}
		at := func(p image.Point) f32.Point { q := full.Min.Add(p); return f32.Pt(float32(q.X)+.5, float32(q.Y)+.5) }
		if cur := pointerAt(e, at(textPoint(t, ent().v))); cur != pointer.CursorPointer {
			t.Errorf("the cursor over a pin's text is %v, want the row's hand", cur)
		}
		c := &rxCase{e: e}
		c.click(at(mid(ent().v.LinkRects()[0].R)))
		if !slices.Contains(h.opened, "url:"+rhLink) {
			t.Errorf("the link's click opened %v", h.opened)
		}
		if len(h.pinned) != 1 || h.pinned[0] != fakeslack.PinsChannelID+"/"+ts {
			t.Errorf("the link's click opened the pins %v, want the pin too", h.pinned)
		}
	})
	t.Run("threads", func(t *testing.T) {
		const root = "1762690000.000100"
		ch := fakeslack.ViewsChannel
		app, fs := vaFake(t)
		r := slack.Message{Type: "message", Channel: ch, TS: root, ThreadTS: root, User: "U0FAKE00001", Text: rhLinkText}
		r.ReplyCount, r.ReplyUsers, r.LatestReply = 1, []string{"U0FAKE00002"}, "1762693000.000100"
		x := slack.Message{Type: "message", Channel: ch, TS: r.LatestReply, ThreadTS: root, User: "U0FAKE00002", Text: "a synthetic reply"}
		fs.SetHook(func(method string, _ url.Values) (any, bool) {
			if method == "subscriptions.thread.getView" {
				return thrOrderAnswer(map[string]any{"root_msg": r, "latest_replies": []slack.Message{x}, "unread_replies": []slack.Message{x}}), true
			}
			return nil, false
		})
		v := NewThreads(newKBHost(app, false))
		e := thrViewEnv(t, v, 1)
		e.frame()
		ent := v.rich[ch+"/"+root]
		if ent == nil {
			t.Fatal("the root's body isn't drawn")
		}
		full, _, ok := e.r.AreaBounds(ent.v)
		if !ok {
			t.Fatal("the root's body has no area")
		}
		q := full.Min.Add(textPoint(t, ent.v))
		if cur := pointerAt(e, f32.Pt(float32(q.X)+.5, float32(q.Y)+.5)); cur != pointer.CursorText {
			t.Errorf("the cursor over a Threads message's text is %v, want the I-beam", cur)
		}
	})
}

// getReactionGroups in the views' bar: one Reaction per baseName with the
// group's count, users and reactions, its button the group's emoji and
// count, reacted for any of the viewer's tones; without canReact the
// buttons are disabled and the bar has no add button.
func TestViewsReactionBarGroupsLikeSlack(t *testing.T) {
	app, _ := vaFake(t)
	v := NewThreads(newKBHost(app, false))
	m := &core.Message{Channel: "C1", TS: "1.000001", Reactions: []core.Reaction{
		{Name: "+1", Count: 2, Users: []string{"U1", "U2"}},
		{Name: "tada", Count: 1, Users: []string{"U3"}},
		{Name: "+1::skin-tone-4", Count: 2, Users: []string{"U2", fakeslack.SelfID}, Self: true}}}
	for _, can := range []bool{true, false} {
		bar, button, reacted := rxBar(&v.base, m, can)
		kids := bar.List("children")
		want := 2
		if can {
			want = 3 // and ReactionAddButton
		}
		if len(kids) != want {
			t.Errorf("canReact %v: %d children, want %d", can, len(kids), want)
			continue
		}
		rp := kids[0].(slackc.Slot).Props
		if rp.StrOr("name", "") != "+1" || rp.IntOr("count", 0) != 4 || len(rp.List("users")) != 3 || len(rp.List("reactions")) != 2 || rp.Bool("canReact") != can {
			t.Errorf("canReact %v: the +1 group's Reaction props %v", can, rp)
		}
		bp := button(0)
		if bp.StrOr("className", "") != "c-button-unstyled c-reaction c-reaction--reacted" || len(bp.List("children")) != 3 || bp.Bool("disabled") == can {
			t.Errorf("canReact %v: the +1 pill's button props %v", can, bp)
		}
		if reacted(1) {
			t.Error("the tada pill is the viewer's")
		}
	}
}
