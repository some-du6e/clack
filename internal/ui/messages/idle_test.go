package messages

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
)

// Karim (alpha.24): "stuff is still spazzing out", rows re-laying out with
// nothing happening, first seen in a thread whose parent has a pasted
// image and whose reply is a bot's. A channel like his (an image file, a
// link unfurl with an image, bots with bot_profile icons, a user-less bot,
// a message_mention pill, a thread root, continuation rows) drawn with
// plugins wrapping the row parts the way Slick's do: once it has loaded
// and the plugins' trees are in, nothing may change and nothing may keep
// asking for frames. All synthetic.

const tIdle = "C0IDLEROWS"

// idleHost counts the frames the list asks the host for.
type idleHost struct {
	identityHost
	inval *atomic.Int64
}

func (h idleHost) Invalidate() { h.inval.Add(1) }

// idlePNG is a w×h PNG for every image URL (decoded off the UI goroutine
// like a real fetch).
func idlePNG(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 160, A: 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func idleMessages() []slack.Message {
	at := int64(1794243000)
	ts := func(i int) string { return fmt.Sprintf("%d.%06d", at+int64(i)*40, 100+i) }
	rt := func(els ...map[string]any) json.RawMessage {
		b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "b", "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": els}}}})
		return b
	}
	img := func(name string) string { return "https://files.invalid/" + name + ".png" }
	icons := slack.Icons{Image36: img("bot36"), Image48: img("bot48"), Image72: img("bot72")}
	var out []slack.Message
	// the thread root: text, a pasted 507×275 image, reactions, one reply
	root := slack.Message{Type: "message", TS: ts(0), User: "UALICE", Text: "look at this", Blocks: rt(textEl("look at this")),
		Files: []slack.File{{ID: "F0IDLE001", Name: "image.png", Title: "image.png", Mimetype: "image/png", Filetype: "png", PrettyType: "PNG",
			Mode: "hosted", URLPrivate: img("full"), Thumb360: img("t360"), Thumb480: img("t480"), Thumb360W: 360, Thumb360H: 195,
			OriginalW: 507, OriginalH: 275}},
		Reactions: []slack.Reaction{{Name: "eyes", Count: 1, Users: []string{"UBOB"}}}}
	root.ThreadTS, root.ReplyCount, root.ReplyUsers, root.LatestReply = root.TS, 1, []string{"UORPHEUS"}, ts(1)
	out = append(out, root)
	// a bot user with a bot_profile (Orpheus) mentioning someone in italics
	out = append(out, slack.Message{Type: "message", TS: ts(2), User: "UORPHEUS", BotID: "B0ORPHEUS",
		BotProfile: &slack.BotProfile{ID: "B0ORPHEUS", Name: "orpheus", AppID: "A0ORPHEUS", UserID: "UORPHEUS", Icons: icons},
		Text:       "hi <@UALICE>", Blocks: rt(textEl("hi "), map[string]any{"type": "user", "user_id": "UALICE", "style": map[string]any{"italic": true}})})
	// a message_mention pill (a pasted permalink)
	perma := "https://synthetic.slack.com/archives/" + tIdle + "/p1794242000000100"
	out = append(out, slack.Message{Type: "message", TS: ts(3), User: "UBOB", Text: perma,
		Blocks: rt(textEl("see "), map[string]any{"type": "message_mention", "message_ts": "1794242000.000100", "channel_id": tIdle,
			"author_id": "UALICE", "text": perma, "url": perma})})
	// continuation rows from the same sender
	for i := 4; i < 7; i++ {
		s := fmt.Sprintf("synthetic follow-up %d", i)
		out = append(out, slack.Message{Type: "message", TS: ts(i), User: "UBOB", Text: s, Blocks: rt(textEl(s))})
	}
	// a link unfurl with an image (no size given)
	out = append(out, slack.Message{Type: "message", TS: ts(8), User: "UALICE", Text: "<https://example.invalid/post>",
		Blocks: rt(map[string]any{"type": "link", "url": "https://example.invalid/post"}),
		Attachments: []slack.Attachment{{ID: 1, Fallback: "Example post", ServiceName: "Example", ServiceIcon: img("favicon"),
			Title: "A synthetic post", TitleLink: "https://example.invalid/post", FromURL: "https://example.invalid/post",
			Text: "a synthetic description", ImageURL: img("unfurl")}}})
	// a user-less bot with message icons
	out = append(out, slack.Message{Type: "message", TS: ts(9), BotID: "B0HOOK001", Username: "deploy bot", Text: "deployed",
		Icons: &slack.Icons{Image48: img("hook48")}, Blocks: rt(textEl("deployed"))})
	return out
}

// idlePlugin wraps the row parts like Slick's plugins do: MessageLogger's
// <div> around MessageWrapper, BetterBlock's bare <Original/>s,
// ShowSendingBot's avatar chain, LocalTime's span in the preamble, and a
// stylesheet (CSS active).
var idlePlugin = []byte(`(() => {
const TautPlugin = globalThis.TautPlugin
class IdleWrap extends TautPlugin {
  static id = 'IdleWrap'
  static pluginName = 'Idle Wrap'
  static description = 'wraps message parts'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    const R = React
    this.api.setStyle('.x-idle-wrap { color: inherit }')
    this.api.patchComponent('MessageWrapper', (O) => (p) => R.createElement('div', { className: 'x-idle-wrap' }, R.createElement(O, p)))
    for (const n of ['Message', 'MessageBackground', 'MessageText', 'Blocks', 'BaseVisuallyHiddenMessageText', 'MessageFiles',
      'MessageKitAttachments', 'ReactionBar', 'Reaction', 'ThreadReplies', 'MessageAvatar', 'BaseMessageAvatar', 'ConnectedBaseAvatar'])
      this.api.patchComponent(n, (O) => (p) => R.createElement(O, p))
    this.api.patchComponent('BaseBroadcastPreamble', (O) => (p) => {
      const kids = R.Children.toArray(p.children)
      kids.push(R.createElement('span', { key: 'x', className: 'x-idle-time' }, '1:23 PM'))
      return R.createElement(O, p, kids)
    })
  }
}
return IdleWrap
})()`)

func TestIdleRowsStayPut(t *testing.T) {
	app, _, tt, u, ch := pluginEnv(t)
	install(t, tt, idlePlugin)
	until(t, 5*time.Second, "the patches", func() {}, func() bool { return slackc.Patched("MessageWrapper") && slackc.Patched("ReactionBar") })
	idleViews(t, app, ch, func() bool { return u.anyTree("x-idle-wrap") })
}

// idleViews seeds the idle channel and its thread into app and checks a
// channel view, a thread pane and the thread pane placed like the ui
// root places a short thread; trees reports the plugins' trees are in.
func idleViews(t *testing.T, app *core.App, ch coreHost, trees func() bool) {
	app.S.PutConv(slack.Conversation{ID: tIdle, Name: "idle", IsChannel: true, IsMember: true, NumMembers: 4})
	app.S.PutUsers([]slack.User{{ID: "UORPHEUS", Name: "orpheus", RealName: "Orpheus", IsBot: true, Profile: slack.Profile{DisplayName: "orpheus"}}})
	app.S.SetLatest(tIdle, idleMessages(), false)
	pngs := map[bool][]byte{false: idlePNG(507, 275), true: idlePNG(48, 48)}
	var inval atomic.Int64
	imgs := imgcache.New(func(ctx context.Context, url string) ([]byte, error) {
		return pngs[len(url) < 40], nil
	}, "", func() { inval.Add(1) })
	h := idleHost{identityHost: identityHost{coreHost: ch, imgs: imgs}, inval: &inval}
	prev := slackc.CurrentEnv()
	env := prev
	env.Theme, env.Images, env.Invalidate = ch.th, imgs, func() { inval.Add(1) }
	slackc.Init(env)
	t.Cleanup(func() { slackc.Init(prev) })

	msgs := idleMessages()
	app.S.SetThread(tIdle, msgs[0].TS, []slack.Message{msgs[0], {Type: "message", TS: msgs[0].LatestReply, ThreadTS: msgs[0].TS,
		User: "UORPHEUS", BotID: "B0ORPHEUS", BotProfile: msgs[1].BotProfile, Text: "hi <@UALICE>", Blocks: msgs[1].Blocks}})
	cv := NewChannelView(h, tIdle)
	cv.Preview = true
	tv := NewThreadView(h, tIdle, msgs[0].TS)
	for _, tc := range []struct {
		name string
		v    interface {
			Layout(layout.Context) layout.Dimensions
		}
		s    *listState
		rows int
		size image.Point
	}{
		{"channel", cv, cv.s, len(msgs), image.Pt(1600, 1400)},
		{"thread", tv, tv.s, 2, image.Pt(800, 1400)},
		// the ui root's short-thread placement (conversation.go
		// layoutConvo): the list is as tall as last frame's content
		// height when that's shorter than the pane, the composer under it
		{"thread-fit", &fitPane{tv: tv, comp: 200}, tv.s, 2, image.Pt(800, 1400)},
	} {
		t.Run(tc.name, func(t *testing.T) { idleCheck(t, tc.v, tc.s, tc.rows, tc.size, trees, &inval) })
	}
}

// idleCheck loads v, lets it settle, then draws it idle, with the pointer
// off it and resting on each row in turn: no row may change height and
// nothing may keep asking for frames.
func idleCheck(t *testing.T, v interface {
	Layout(layout.Context) layout.Dimensions
}, s *listState, rows int, size image.Point, trees func() bool, inval *atomic.Int64) {
	var r input.Router
	ops := new(op.Ops)
	now := time.Now()
	wakes := 0 // frames that asked for another frame at once
	frame := func() {
		ops.Reset()
		now = time.Now() // frame times are wall-clock times, like the window's
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}, Constraints: layout.Exact(size), Now: now, Source: r.Source()}
		v.Layout(gtx)
		r.Frame(ops)
		if at, ok := r.WakeupTime(); ok && !at.After(now.Add(5*time.Millisecond)) {
			wakes++
		}
	}
	settle := func(n int) {
		for i := 0; i < n; i++ {
			frame()
			time.Sleep(10 * time.Millisecond)
		}
	}
	until(t, 8*time.Second, "every row", frame, func() bool { return len(s.rows) >= rows })
	until(t, 8*time.Second, "the plugins' trees", frame, trees)
	settle(150) // images decode, trees arrive, layout settles
	heights := func() map[string]int {
		out := map[string]int{}
		for i := s.visible[0]; i < s.visible[1]; i++ {
			out[s.items[i].key] = s.heights[i]
		}
		return out
	}
	idle := func(where string) {
		h0 := heights()
		inval.Store(0)
		wakes = 0
		const n = 60
		changed := map[string][]int{}
		for i := 0; i < n; i++ {
			frame()
			time.Sleep(5 * time.Millisecond)
			for k, hh := range heights() {
				if hh != h0[k] {
					changed[k] = append(changed[k], hh)
				}
			}
		}
		for k, hs := range changed {
			t.Errorf("idle, %s: row %s changed height (%d at rest, then %v)", where, k, h0[k], hs)
		}
		if wakes > n/4 {
			t.Errorf("idle, %s: %d of %d frames asked for another frame at once", where, wakes, n)
		}
		if c := inval.Load(); c > n/4 {
			t.Errorf("idle, %s: %d invalidations over %d frames", where, c, n)
		}
		t.Logf("idle, %s: %d wakes, %d invalidations over %d frames", where, wakes, inval.Load(), n)
	}
	idle("pointer away")
	if len(heights()) < rows {
		t.Fatalf("only %d of the rows on screen", len(heights()))
	}
	for i := s.visible[0]; i < s.visible[1]; i++ {
		if s.items[i].kind != itMsg {
			continue
		}
		key := s.items[i].key
		y := s.pos[i] + s.heights[i]/2
		r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(size.X)/3, float32(y))})
		settle(40) // the hover's props reach the plugin and back
		idle("pointer on " + key)
	}
}

// fitPane is conversation.go's layoutConvo for a thread: the list sized by
// the thread's last ContentHeight, a composer of comp px under it.
type fitPane struct {
	tv   *ThreadView
	comp int
	fits []int // list heights given, frame by frame
}

func (f *fitPane) Layout(gtx layout.Context) layout.Dimensions {
	sz := gtx.Constraints.Max
	listH := sz.Y - f.comp
	if h := f.tv.ContentHeight(); h > 0 && h < listH {
		listH = h
	}
	f.fits = append(f.fits, listH)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(sz.X, listH))
	f.tv.Layout(g)
	return layout.Dimensions{Size: sz}
}
