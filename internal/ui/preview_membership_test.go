package ui

// "join channel thingy is not showing" (Karim, α.90 macOS, #僕のゴミ): a
// public channel he isn't in showed the composer (MessagePaneInput,
// patched by Better Typing Indicator) where Slack shows JoinChannelPane.
// Two holes, both against internal/fakeslack:
//
//   - core's refreshConv stored every conversations.info answer in the
//     member set, is_member:false or not. It runs on events every member
//     of the team gets (channel_created, channel_rename) and on a message
//     in a channel outside the member set: such a channel got a sidebar
//     row and the composer.
//   - a channel opened before conversations.info answered (a link, a
//     permalink, search, the channel browser) had no conversation info,
//     and no info meant footNone: the composer, until the answer turned
//     it into the preview. Slack's MessagePaneInput picks from the channel
//     object (channelIsPreview: !!(channel && isPreview(channel))), so
//     Clack now draws neither until membership is known (footPending).

import (
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// gomiID is a public channel someone else made (fakeslack AddOtherChannel).
const gomiID = "C0OTHER00777"

// watchFooter runs frames until c shows Slack's preview footer with its
// Join button, failing if a frame on the way chose the composer, and
// reports whether one waited on membership (footPending).
func watchFooter(t *testing.T, e *perfEnv, c *convo, what string) (pending bool) {
	t.Helper()
	var bad []footerKind
	e.until(what+": the preview footer", 20*time.Second, func() bool {
		switch f := e.a.footerFor(c); f {
		case footPreview:
			return c.jp != nil && !c.jp.joinRect.Empty()
		case footPending:
			pending = true
		default:
			bad = append(bad, f)
		}
		return false
	})
	if len(bad) > 0 {
		t.Errorf("%s: frames chose footer %v before the preview (footNone is the composer)", what, bad)
	}
	if !c.compRect.Empty() {
		t.Errorf("%s: the composer drew (rect %v) in a channel the viewer isn't in", what, c.compRect)
	}
	if ci := e.a.convOf(c.ch); ci == nil || !ci.Preview {
		t.Errorf("%s: conv %+v", what, ci)
	}
	if _, member := e.a.data.core.S.Conv(c.ch); member {
		t.Errorf("%s: core has %s in the member set", what, c.ch)
	}
	return pending
}

// holdInfo makes conversations.info answer slowly until the test is
// done with it (the returned func): membership is unknown at open.
func holdInfo(fs *fakeslack.Server) func() {
	fs.SetFault("conversations.info", fakeslack.Fault{Delay: 1200 * time.Millisecond})
	return func() { fs.SetFault("conversations.info", fakeslack.Fault{}) }
}

// Opened every way a channel outside the sidebar gets opened, with
// conversations.info still out: never the composer, the preview footer
// once membership is known.
func TestPreviewFooterMembershipUnknownAtOpen(t *testing.T) {
	ch := fakeslack.PreviewChannelID
	paths := []struct {
		name string
		open func(e *perfEnv)
	}{
		{"link", func(e *perfEnv) { e.openChannel(ch) }},
		{"permalink", func(e *perfEnv) { e.a.Navigate("c/" + ch + "/m/1762516800.000100"); e.frame() }},
		{"deep link", func(e *perfEnv) {
			if !e.a.openDeepLink("slack://channel?team=" + fakeslack.TeamID + "&id=" + ch) {
				t.Fatal("deep link not handled")
			}
			e.frame()
			e.frame()
		}},
		{"search", func(e *perfEnv) {
			e.a.Navigate("search?q=lounge")
			e.frame()
			e.openChannel(ch)
		}},
		{"channel browser", func(e *perfEnv) {
			e.a.Navigate("directories?tab=channels")
			e.frame()
			e.openChannel(ch)
		}},
	}
	for _, p := range paths {
		t.Run(p.name, func(t *testing.T) {
			e, fs := previewEnv(t)
			_, known := e.a.data.core.Conv(ch)
			release := holdInfo(fs)
			c := e.a.convoFor(ch, "")
			p.open(e)
			if got := e.a.router.Current().Channel(); got != ch {
				t.Fatalf("route %q", e.a.router.Current())
			}
			pending := watchFooter(t, e, c, p.name)
			release()
			if !known && !pending {
				t.Error("membership was unknown at open, but no frame waited on it")
			}
			if p.name == "channel browser" && e.a.previewBack(ch) == "" {
				t.Error("no Back to All channels after the channel browser")
			}
			e.settle(200 * time.Millisecond)
			if e.a.footerFor(c) != footPreview || !c.compRect.Empty() {
				t.Errorf("settled: footer %v, composer rect %v", e.a.footerFor(c), c.compRect)
			}
		})
	}
}

// A channel someone else made reaches the viewer as channel_created (and
// a rename as channel_rename): it stays out of the sidebar and opens as a
// preview, and Join swaps the footer for the composer, live.
func TestPreviewFooterChannelCreatedElsewhere(t *testing.T) {
	e, fs := previewEnv(t)
	fs.AddOtherChannel(slack.Conversation{ID: gomiID, Name: "僕のゴミ", Created: time.Now().Unix(), Creator: "U0FAKE00002",
		NumMembers: 2, Purpose: &slack.Purpose{Value: "gomi"}})
	svc := e.a.data.core
	inject := func(typ string) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			svc.Inject(slack.Event{Type: typ,
				Raw: []byte(`{"type":"` + typ + `","channel":{"id":"` + gomiID + `","name":"僕のゴミ","creator":"U0FAKE00002"}}`)})
		}()
		e.until(typ+" handled", 10*time.Second, func() bool {
			select {
			case <-done:
				return true
			default:
				return false
			}
		})
	}
	inject("channel_created")
	if _, member := svc.S.Conv(gomiID); member {
		t.Fatal("channel_created by someone else put the channel in the member set")
	}
	if cv, ok := svc.Conv(gomiID); !ok || !cv.Preview {
		t.Fatalf("created channel: ok=%v %+v", ok, cv)
	}
	e.settle(300 * time.Millisecond) // the resnapshot
	if e.sidebarHas(gomiID) {
		t.Error("a channel the viewer isn't in got a sidebar row")
	}
	inject("channel_rename")
	if _, member := svc.S.Conv(gomiID); member {
		t.Fatal("channel_rename put the channel in the member set")
	}

	c := e.a.convoFor(gomiID, "")
	e.openChannel(gomiID)
	watchFooter(t, e, c, "created elsewhere")
	e.until("its row while open", 10*time.Second, func() bool { return e.sidebarHas(gomiID) })
	e.shot("channel-preview-created-elsewhere")

	// Join: conversations.join, then the composer in the footer's place
	e.click(c.jp.joinRect.Min.Add(c.jp.joinRect.Size().Div(2)))
	e.until("conversations.join", 10*time.Second, func() bool {
		for _, j := range fs.JoinCalls() {
			if j.Channel == gomiID {
				return true
			}
		}
		return false
	})
	e.until("the composer", 10*time.Second, func() bool { return e.a.footerFor(c) == footNone && c.compRect.Dy() > 40 })
	if _, member := svc.S.Conv(gomiID); !member {
		t.Error("joined channel not in the member set")
	}
}

// slotBridge stands in for a plugin runtime: it records each component's
// latest root.
type slotBridge struct {
	mu      sync.Mutex
	renders map[string]uint64
}

func (b *slotBridge) Render(root uint64, name string, props []byte) {
	b.mu.Lock()
	b.renders[name] = root
	b.mu.Unlock()
}
func (b *slotBridge) RenderFn(root uint64, fn int64, args []byte)          {}
func (b *slotBridge) Unmount(root uint64)                                  {}
func (b *slotBridge) Dispatch(target int64, event []byte)                  {}
func (b *slotBridge) Call(fn int64, args []byte, done func([]byte, error)) {}
func (b *slotBridge) SetRects(rects map[int64][4]float32)                  {}
func (b *slotBridge) SetViewport(w, h, dpr float32, dark bool)             {}

func (b *slotBridge) root(name string) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.renders[name]
}

// With a plugin patching the composer (Better Typing Indicator patches
// MessagePaneInput: Karim's inspect shot read "MessagePaneInput (patched
// by a plugin)"), a preview still shows Slack's footer: the shell never
// asks for the composer there, so the plugin's tree and the vanish
// guards (composer/vanish.go, mount_vanish_guard.go) never run for it.
// Joined, the patched composer comes in.
func TestPreviewFooterUnderComposerPlugin(t *testing.T) {
	e, fs := previewEnv(t)
	fb := &slotBridge{renders: map[string]uint64{}}
	slackc.SetBridge(fb)
	slackc.SetPatched([]string{"MessagePaneInput"})
	t.Cleanup(func() { slackc.Reset(); slackc.SetBridge(nil) })

	ch := fakeslack.PreviewChannelID
	release := holdInfo(fs)
	c := e.a.convoFor(ch, "")
	e.openChannel(ch)
	watchFooter(t, e, c, "patched composer")
	release()
	e.settle(300 * time.Millisecond)
	if f := e.a.footerFor(c); f != footPreview {
		t.Errorf("footer %v under the plugin", f)
	}
	if !c.compRect.Empty() {
		t.Errorf("the patched composer drew in a preview (rect %v)", c.compRect)
	}
	if r := fb.root("MessagePaneInput"); r != 0 {
		t.Errorf("the plugin's MessagePaneInput rendered (root %d) in a preview", r)
	}

	e.a.joinPreview(c, "")
	e.until("the patched composer", 10*time.Second, func() bool {
		return e.a.footerFor(c) == footNone && fb.root("MessagePaneInput") != 0
	})
}

// A channel id that conversations.info can't answer (channel_not_found)
// stops waiting: the shell falls back to what it drew before.
func TestPreviewFooterLookupFailureStopsWaiting(t *testing.T) {
	e, _ := previewEnv(t)
	ch := "C0NOSUCH0001"
	c := e.a.convoFor(ch, "")
	e.openChannel(ch)
	e.until("the lookup's end", 20*time.Second, func() bool { return e.a.footerFor(c) != footPending })
	if !e.a.data.extFailed[ch] {
		t.Error("the failed lookup wasn't recorded")
	}
	if r := e.a.router.Current(); r.Channel() != ch {
		t.Errorf("route %q", r)
	}
}
