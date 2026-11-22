package views

import (
	"encoding/json"
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The Threads and Unreads views draw messages with Slack's message
// components (the thread list duay, the channel list), so the pointer
// reaches a message's body as in a channel: a link opens on a click, a
// right-click on a link or an image block is the desktop shell's link
// menu and on text the message's own menu, a drag selects text, and the
// row stays hovered over a link. The row's hover and right-click target
// are the content's parent (a sibling drawn on top of it took the
// pointer from every area in the body).

const (
	rhLink = "https://example.com/doc"
	rhImg  = "https://example.com/img/400x200/chart.png"
)

var rhImageBlocks = json.RawMessage(`[{"type":"rich_text","block_id":"r1","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"chart below"}]}]},` +
	`{"type":"image","block_id":"i1","image_url":"` + rhImg + `","alt_text":"chart"}]`)

const rhLinkText = "see <" + rhLink + "|the doc> for the plan and then some more words in this synthetic message"

// rhKB is the Threads view's host recording the links it opens.
type rhKB struct {
	*kbHost
	mu     sync.Mutex
	opened []string
}

func (h *rhKB) OpenURL(u string) { h.mu.Lock(); h.opened = append(h.opened, u); h.mu.Unlock() }
func (h *rhKB) links() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.opened...)
}

// rhVA is the same over vaHost (the Unreads view's host).
type rhVA struct {
	*vaHost
	mu     sync.Mutex
	opened []string
}

func (h *rhVA) OpenURL(u string) { h.mu.Lock(); h.opened = append(h.opened, u); h.mu.Unlock() }
func (h *rhVA) links() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.opened...)
}

// rhCase drives one view: e lays it out; body is a message's drawn body;
// open the overlays shown; links the links opened; hovered the row's
// hover state for a message.
type rhCase struct {
	t       *testing.T
	e       *vaEnv
	body    func(ts string) *richview.View
	open    func() []popups.Overlay
	links   func() []string
	hovered func(ts string) bool
}

// at is local point p of ts's body in window coordinates.
func (c *rhCase) at(ts string, p image.Point) f32.Point {
	c.t.Helper()
	v := c.body(ts)
	full, _, ok := c.e.r.AreaBounds(v)
	if !ok {
		c.t.Fatalf("message %s's body has no area", ts)
	}
	q := full.Min.Add(p)
	return f32.Pt(float32(q.X)+.5, float32(q.Y)+.5)
}

func (c *rhCase) queue(evs ...pointer.Event) {
	for i := range evs {
		evs[i].Source = pointer.Mouse
	}
	c.e.r.Queue(asEvents(evs)...)
	c.e.frame()
	c.e.frame()
}

func (c *rhCase) press(p f32.Point, b pointer.Buttons) {
	c.queue(pointer.Event{Kind: pointer.Move, Position: p},
		pointer.Event{Kind: pointer.Press, Position: p, Buttons: b},
		pointer.Event{Kind: pointer.Release, Position: p})
}

// lastMenu is the newest menu shown since n overlays, or fails.
func (c *rhCase) lastMenu(n int, what string) []string {
	c.t.Helper()
	os := c.open()
	for i := len(os) - 1; i >= n; i-- {
		if m, ok := os[i].(*popups.Menu); ok {
			var out []string
			for _, it := range m.Items() {
				out = append(out, it.Label)
			}
			return out
		}
	}
	c.t.Fatalf("%s: no menu opened", what)
	return nil
}

func mid(r image.Rectangle) image.Point { return r.Min.Add(r.Size().Div(2)) }

// textPoint is a point of plain text in v (local), away from its links.
func textPoint(t *testing.T, v *richview.View) image.Point {
	t.Helper()
	for y := 1; y < 60; y++ {
		for x := 1; x < 500; x++ {
			if h := v.HitAt(f32.Pt(float32(x)+.5, float32(y)+.5)); h.Text && h.Kind == richview.HitNone {
				return image.Pt(x, y)
			}
		}
	}
	t.Fatal("no plain text in the body")
	return image.Point{}
}

// check runs every case: linkTS's body has rhLink, imgTS's an image block.
func (c *rhCase) check(linkTS, imgTS string) {
	t := c.t
	lb := c.body(linkTS)
	lrs := lb.LinkRects()
	if len(lrs) == 0 {
		t.Fatal("the link's message has no link drawn")
	}
	link := c.at(linkTS, mid(lrs[0].R))

	// the row stays hovered over the link
	c.queue(pointer.Event{Kind: pointer.Move, Position: link})
	if !c.hovered(linkTS) {
		t.Error("the row isn't hovered with the pointer on its link")
	}
	// a click on the link opens it
	c.press(link, pointer.ButtonPrimary)
	if got := c.links(); len(got) != 1 || got[0] != rhLink {
		t.Errorf("a click on the link opened %q, want [%s]", got, rhLink)
	}
	// a right-click on it: the shell's link menu
	n := len(c.open())
	c.press(link, pointer.ButtonSecondary)
	if got := strings.Join(c.lastMenu(n, "right-click on the link"), "|"); got != "Copy Link|Open Link" {
		t.Errorf("link menu %q", got)
	}
	// on the image block: the link menu for image_url
	ib := c.body(imgTS)
	irs := ib.ImageRects()
	if len(irs) != 1 {
		t.Fatalf("%d images in the image block's message", len(irs))
	}
	n = len(c.open())
	c.press(c.at(imgTS, mid(irs[0].R)), pointer.ButtonSecondary)
	if got := strings.Join(c.lastMenu(n, "right-click on the image block"), "|"); got != "Copy Link|Open Link" {
		t.Errorf("image block menu %q", got)
	}
	// on plain text: the message's own menu
	tp := textPoint(t, lb)
	n = len(c.open())
	c.press(c.at(linkTS, tp), pointer.ButtonSecondary)
	if got := c.lastMenu(n, "right-click on the text"); !strings.Contains(strings.Join(got, "|"), "Add reaction") {
		t.Errorf("text menu %q, want the message actions menu", got)
	}
	// a drag over the text selects it
	a := c.at(linkTS, tp)
	b := a.Add(f32.Pt(120, 0))
	c.queue(pointer.Event{Kind: pointer.Move, Position: a},
		pointer.Event{Kind: pointer.Press, Position: a, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Move, Position: a.Add(f32.Pt(40, 0)), Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Move, Position: b, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Position: b})
	if !lb.HasSelection() {
		t.Error("a drag over the text selected nothing")
	}
}

func asEvents(evs []pointer.Event) []event.Event {
	out := make([]event.Event, len(evs))
	for i := range evs {
		out[i] = evs[i]
	}
	return out
}

func TestThreadsRowReachesTheBody(t *testing.T) {
	const root, reply = "1762690000.000100", "1762693000.000100"
	ch := fakeslack.ViewsChannel
	app, fs := vaFake(t)
	r := slack.Message{Type: "message", Channel: ch, TS: root, ThreadTS: root, User: "U0FAKE00001", Text: rhLinkText}
	r.ReplyCount, r.ReplyUsers, r.LatestReply = 1, []string{"U0FAKE00002"}, reply
	x := slack.Message{Type: "message", Channel: ch, TS: reply, ThreadTS: root, User: "U0FAKE00002", Text: "chart below", Blocks: rhImageBlocks}
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method == "subscriptions.thread.getView" {
			return thrOrderAnswer(map[string]any{"root_msg": r, "latest_replies": []slack.Message{x}, "unread_replies": []slack.Message{x}}), true
		}
		return nil, false
	})
	h := &rhKB{kbHost: newKBHost(app, false)}
	v := NewThreads(h)
	e := thrViewEnv(t, v, 1)
	e.frame()
	e.frame()
	c := &rhCase{t: t, e: e, open: h.open, links: h.links,
		body: func(ts string) *richview.View {
			ent := v.rich[ch+"/"+ts]
			if ent == nil {
				t.Fatalf("message %s's body isn't drawn", ts)
			}
			return ent.v
		},
		hovered: func(ts string) bool {
			card := v.cards[ch+"/"+root]
			if card == nil || card.msgs[ts] == nil {
				t.Fatalf("no row state for %s", ts)
			}
			return card.msgs[ts].hv.Update(e.r.Source())
		}}
	c.check(root, reply)
}

func TestUnreadsRowReachesTheBody(t *testing.T) {
	ch := fakeslack.UnreadsDesignID
	var img, link string
	app, _ := unrFakeWith(t, nil, func(fs *fakeslack.Server) {
		img = fs.PutUnreadMessage(ch, slack.Message{User: "U0FAKE00002", Text: "chart below", Blocks: rhImageBlocks})
		link = fs.PutUnreadMessage(ch, slack.Message{User: "U0FAKE00001", Text: rhLinkText})
	})
	h := &rhVA{vaHost: &vaHost{slickHost: &slickHost{app: app, th: theme.New(false)}}}
	v := NewUnreads(h)
	e := &vaEnv{size: image.Pt(1100, 900)}
	e.draw = func(gtx layout.Context) { v.Layout(gtx) }
	// #unreads-launch (a mention, 31 unread) comes first: bring the
	// design channel's frame on screen
	e.until(t, "the frames", func() bool {
		if v.built && len(v.rows) > 0 && v.rows[len(v.rows)-1].kind == unrFooter {
			return true
		}
		v.list.ScrollTo(len(v.rows)) // pages load as the list nears its end (unrEnv)
		return false
	})
	hr, ok := unrHead(v, ch)
	if !ok {
		t.Fatalf("no frame for %s: %v", ch, v.Frames())
	}
	v.list.ScrollTo(v.headerRow(hr.f))
	e.frame()
	e.until(t, "the two messages drawn", func() bool {
		return v.rich[ch+"/"+link] != nil && v.rich[ch+"/"+img] != nil
	})
	for k := 0; k < 3; k++ {
		e.frame()
		time.Sleep(10 * time.Millisecond)
	}
	c := &rhCase{t: t, e: e, open: h.open, links: h.links,
		body: func(ts string) *richview.View {
			ent := v.rich[ch+"/"+ts]
			if ent == nil {
				t.Fatalf("message %s's body isn't drawn", ts)
			}
			return ent.v
		},
		hovered: func(ts string) bool {
			s := v.msgs[ch+"/"+ts]
			if s == nil {
				t.Fatalf("no row state for %s", ts)
			}
			return s.hv.Update(e.r.Source())
		}}
	c.check(link, img)
}

// The Pins tab's message row: the hover actions are drawn inside the row's
// hover, so a click on one runs it (the row's hover on top of them took the
// pointer), and the row stays hovered with the pointer on a button.
func TestPinsRowReachesItsActions(t *testing.T) {
	app, fs := pfCore(t)
	h := pfHostFor(t, app, fs, false)
	v := NewChannelPins(h, fakeslack.PinsChannelID)
	e := &vaEnv{size: image.Pt(1100, 900)}
	pfDraw(e, h, v.Layout)
	e.until(t, "the pins", func() bool { return v.Loaded() && len(v.Items()) == 4 })
	e.frame()
	key := fakeslack.PinnedTS[0]
	r := v.rows[key]
	if r == nil {
		t.Fatal("no row for the first pin")
	}
	full, _, ok := e.r.AreaBounds(&r.row)
	if !ok {
		t.Fatal("the first pin's row has no area")
	}
	c := &rhCase{t: t, e: e}
	c.queue(pointer.Event{Kind: pointer.Move, Position: f32.Pt(float32(full.Min.X+40), float32(full.Min.Y+20))})
	btn, _, ok := e.r.AreaBounds(&r.tb[cpTbReact])
	if !ok {
		t.Fatal("the hovered row draws no React button")
	}
	p := mid(btn)
	at := f32.Pt(float32(p.X)+.5, float32(p.Y)+.5)
	c.queue(pointer.Event{Kind: pointer.Move, Position: at})
	if !r.hv.Update(e.r.Source()) {
		t.Error("the row isn't hovered with the pointer on its React button")
	}
	n := len(h.open())
	c.press(at, pointer.ButtonPrimary)
	os := h.open()
	for _, o := range os[n:] {
		if _, ok := o.(*popups.EmojiPicker); ok {
			return
		}
	}
	t.Errorf("a click on the React button showed %T, want the reaction picker", os[n:])
}
