package ui

// Sending an image from the composer, end to end against internal/fakeslack
// (synthetic images only; nothing talks to Slack): the tile while the
// eager upload is processing, the placeholder row right after Enter
// ("Uploading…" over the File's own bytes at .5 with a spinner), and the
// final row (the text, "image.png ▾", then the image) once the share's
// echo replaced it; then the same with an image and no text. The test
// checks the row sequence: one row per send, never a FILE card, the text
// and the image together from the first frame. With IMAGE_SEND_SHOTS_DIR
// set (the image-send workflow) it saves each stage, light and dark.

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func sendSynthPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(30 + x*170/w), uint8(60 + y*150/h), 190, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func (e *perfEnv) sendShot(name string) {
	dir := os.Getenv("IMAGE_SEND_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	e.settle(300 * time.Millisecond)
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		e.tb.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		e.tb.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		e.tb.Fatal(err)
	}
}

func imageSendEnv(t *testing.T, dark bool) (*perfEnv, string) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.ProcessDelay = 2500 * time.Millisecond // long enough to see the tile and the placeholder
	e := startEnv(t, fs, t.TempDir())
	e.size = image.Pt(1280, 800)
	if os.Getenv("IMAGE_SEND_SHOTS_DIR") != "" {
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
	ch := fs.ChannelIDs()[2]
	e.openChannel(ch)
	e.until("the channel's history", 20*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	e.settle(800 * time.Millisecond)
	e.toLatest()
	return e, ch
}

// toLatest scrolls the open channel to its newest message.
func (e *perfEnv) toLatest() {
	for i := 0; i < 12; i++ {
		e.scroll(e.listPoint(), 20000)
		e.frame()
	}
	e.settle(300 * time.Millisecond)
}

// composer is the open channel's composer.
func (e *perfEnv) channelComposer(ch string) composerBox {
	e.tb.Helper()
	cv := e.a.convos[ch+"/"]
	if cv == nil || cv.comp == nil || cv.compRect.Empty() {
		e.tb.Fatal("no composer laid out")
	}
	cb, ok := cv.comp.(composerBox)
	if !ok {
		e.tb.Fatalf("the composer is a %T", cv.comp)
	}
	r := cv.compRect
	e.click(r.Min.Add(image.Pt(60, r.Dy()/2)))
	e.frame()
	return cb
}

func (e *perfEnv) rowsFor(ch, cid string) []core.Message {
	c := e.a.data.core
	var out []core.Message
	if tl := c.S.Timeline(ch); tl != nil {
		for i := range tl.Msgs {
			if m := tl.Msgs[i]; m.ClientMsgID == cid {
				out = append(out, c.MessageFromSlack(ch, &m))
			}
		}
	}
	return out
}

func TestImageSendShots(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e, ch := imageSendEnv(t, dark)
			defer e.shutdown()
			c := e.a.data.core
			evs, stop := c.Subscribe()
			defer stop()
			var cids []string
			watch := func() {
				for {
					select {
					case ev := <-evs:
						if ev.Type == "message" && ev.Msg != nil && ev.Msg.Pending && ev.Channel == ch {
							cids = append(cids, ev.ClientMsgID)
						}
					default:
						return
					}
				}
			}

			// 1. text and an image: the tile, the placeholder, the final row
			cb := e.channelComposer(ch)
			e.typeText("look at the new activity page")
			e.frame()
			cb.AttachFile("image.png", sendSynthPNG(t, 640, 400))
			e.settle(600 * time.Millisecond)
			e.sendShot("image-send-1-tile-" + mode)
			e.key(key.NameReturn, 0)
			e.frame()
			if cb.Attached() != 0 || cb.DocText() != "" {
				t.Fatalf("after Enter the composer holds %d files and %q", cb.Attached(), cb.DocText())
			}
			e.until("the placeholder", 5*time.Second, func() bool { watch(); return len(cids) == 1 })
			e.toLatest()
			e.sendShot("image-send-2-placeholder-" + mode)
			checkRows := func(cid string, wantText bool) {
				t.Helper()
				for _, m := range e.rowsFor(ch, cid) {
					var text, img, card bool
					for _, b := range m.Doc.Blocks {
						switch {
						case b.Kind == richtext.FileBlock && b.File != nil && b.File.IsImage:
							img = true
						case b.Kind == richtext.FileBlock:
							card = true
						case b.Kind == richtext.Paragraph:
							text = true
						}
					}
					if !img || card || text != wantText {
						t.Errorf("row %s (pending %v): image %v, FILE card %v, text %v", m.TS, m.Pending, img, card, text)
					}
				}
			}
			e.until("the shared message", 15*time.Second, func() bool {
				watch()
				checkRows(cids[0], true)
				rs := e.rowsFor(ch, cids[0])
				return len(rs) == 1 && !rs[0].Pending
			})
			e.settle(1500 * time.Millisecond) // the thumbnail from the fake's /img
			e.toLatest()
			if rs := e.rowsFor(ch, cids[0]); len(rs) != 1 {
				t.Errorf("%d rows for the send, want one", len(rs))
			}
			e.sendShot("image-send-3-final-" + mode)

			// 2. an image with no text (Karim's send)
			cb = e.channelComposer(ch)
			cb.AttachFile("image.png", sendSynthPNG(t, 480, 300))
			e.settle(300 * time.Millisecond)
			e.key(key.NameReturn, 0)
			e.frame()
			e.until("the second placeholder", 5*time.Second, func() bool { watch(); return len(cids) == 2 })
			e.toLatest()
			e.sendShot("image-send-4-placeholder-image-only-" + mode)
			e.until("the second shared message", 15*time.Second, func() bool {
				watch()
				checkRows(cids[1], false)
				rs := e.rowsFor(ch, cids[1])
				return len(rs) == 1 && !rs[0].Pending
			})
			e.settle(1500 * time.Millisecond)
			e.toLatest()
			e.sendShot("image-send-5-final-image-only-" + mode)
			if n := len(e.fs.Shared()); n != 2 {
				t.Errorf("%d shares, want 2", n)
			}
		})
	}
}

// Karim (alpha.32, 2026-11-10) on the send button: "you cant make this
// shit up this doesnt work". A mouse click on its send half sends
// (MessageInput's SendButton onClick → sendMessage → chat.postMessage)
// plain text and an ordered list, and the chevron opens the schedule
// menu (ScheduledSendButton), both through the window's input router
// like a real click: move, press, a frame, release. In the channel
// composer, then in #new-joiners under the channel join banner (the
// state Karim's channel was in).
func TestSendButtonClicks(t *testing.T) {
	sz := fakeslack.Small
	sz.Unreads = true // #new-joiners and its join banner
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	var mu sync.Mutex
	var posts []url.Values
	fs.OnPost = func(f url.Values, _ map[string]any) {
		mu.Lock()
		posts = append(posts, f)
		mu.Unlock()
	}
	pe := startEnv(t, fs, t.TempDir())
	pe.waitReady()
	run := func(t *testing.T, ch string, banner bool) {
		pe.openChannel(ch)
		pe.until("history", 10*time.Second, func() bool { return pe.loadedMessages(ch) > 0 })
		pe.settle(300 * time.Millisecond)
		if banner {
			cv := pe.a.convoFor(ch, "")
			pe.until("the join banner", 20*time.Second, func() bool { return cv.bannerH > 0 })
		}
		e := &mfEnv{t: t, a: pe.a, rt: &pe.r, frame: pe.frame, ch: ch, size: pe.size}
		cb := e.focusComposer()
		mu.Lock()
		n0 := len(posts)
		mu.Unlock()
		click := func(what string, r image.Rectangle) {
			t.Helper()
			if r.Empty() {
				t.Fatalf("no %s drawn", what)
			}
			p := r.Min.Add(r.Size().Div(2))
			e.ptr(pointer.Move, p)
			e.frames(2)
			e.clock += time.Second // not a double click
			e.ptr(pointer.Press, p)
			e.frame()
			e.ptr(pointer.Release, p)
			e.frames(3)
		}
		posted := func(n int, what string) url.Values {
			t.Helper()
			deadline := time.Now().Add(8 * time.Second)
			for time.Now().Before(deadline) {
				mu.Lock()
				if len(posts) >= n0+n {
					f := posts[n0+n-1]
					mu.Unlock()
					return f
				}
				mu.Unlock()
				e.frame()
				time.Sleep(5 * time.Millisecond)
			}
			t.Fatalf("%s: no chat.postMessage (composer holds %q, send %v)", what, cb.DocText(), cb.SendRect())
			return nil
		}
		// 1. plain text
		e.typeIn(cb, "plain send")
		click("send button", cb.SendRect())
		if f := posted(1, "plain text"); f.Get("channel") != ch || !strings.Contains(f.Get("text"), "plain send") {
			t.Errorf("posted %v", f)
		}
		e.frames(3)
		if got := cb.DocText(); got != "" {
			t.Errorf("after the send the composer holds %q", got)
		}
		// 2. Karim's list: two items and an empty third (Shift+Enter makes
		// the next item, as in Slack); Slack's send keeps the empty item
		e.typeIn(cb, "goofy looking alignment")
		e.rt.Queue(key.Event{Name: "7", Modifiers: key.ModShortcut | key.ModShift, State: key.Press}, key.Event{Name: "7", Modifiers: key.ModShortcut | key.ModShift, State: key.Release})
		e.frames(2)
		shiftEnter := func() {
			e.rt.Queue(key.Event{Name: key.NameReturn, Modifiers: key.ModShift, State: key.Press}, key.Event{Name: key.NameReturn, Modifiers: key.ModShift, State: key.Release})
			e.frames(2)
		}
		shiftEnter()
		e.typeIn(cb, "why is the welcome there")
		shiftEnter()
		click("send button", cb.SendRect())
		f := posted(2, "the list")
		var blocks []struct {
			Elements []struct {
				Type     string            `json:"type"`
				Style    string            `json:"style"`
				Elements []json.RawMessage `json:"elements"`
			} `json:"elements"`
		}
		if err := json.Unmarshal([]byte(f.Get("blocks")), &blocks); err != nil || len(blocks) != 1 || len(blocks[0].Elements) != 1 {
			t.Fatalf("list blocks %s (%v)", f.Get("blocks"), err)
		}
		if l := blocks[0].Elements[0]; l.Type != "rich_text_list" || l.Style != "ordered" || len(l.Elements) != 3 ||
			string(l.Elements[2]) != `{"type":"rich_text_section","elements":[]}` {
			t.Errorf("list %s, want ordered with the two items and the empty third", f.Get("blocks"))
		}
		// 3. Enter in a list item sends (MessageInput.onEnter; Slack's
		// shouldInsertNewline has no list case)
		e.typeIn(cb, "enter sends")
		e.rt.Queue(key.Event{Name: "7", Modifiers: key.ModShortcut | key.ModShift, State: key.Press}, key.Event{Name: "7", Modifiers: key.ModShortcut | key.ModShift, State: key.Release})
		e.frames(2)
		e.rt.Queue(key.Event{Name: key.NameReturn, State: key.Press}, key.Event{Name: key.NameReturn, State: key.Release})
		e.frames(2)
		if f := posted(3, "Enter in a list item"); !strings.Contains(f.Get("blocks"), `"rich_text_list"`) {
			t.Errorf("Enter posted %v", f)
		}
		// 4. the chevron opens the schedule menu and sends nothing
		e.typeIn(cb, "for later")
		click("schedule chevron", cb.ScheduleRect())
		m := pe.topMenu()
		if m == nil {
			t.Fatalf("the chevron opened no menu (overlays %v)", pe.overlayTypes())
		}
		if l := attMenuLabels(m); !slices.Contains(l, "Custom time") {
			t.Errorf("schedule menu %v", l)
		}
		pe.closeMenu()
		mu.Lock()
		extra := len(posts) - n0 - 3
		mu.Unlock()
		if extra != 0 || !strings.Contains(cb.DocText(), "for later") {
			t.Errorf("the chevron sent %d messages; the composer holds %q", extra, cb.DocText())
		}
		e.clearComposer(cb)
		// 5. a tap: the press and the release in one batch before a frame
		// (a trackpad tap, or a quick click during a slow frame)
		tap := func(r image.Rectangle) {
			t.Helper()
			p := r.Min.Add(r.Size().Div(2))
			e.ptr(pointer.Move, p)
			e.frames(2)
			e.clock += time.Second
			e.ptr(pointer.Press, p)
			e.ptr(pointer.Release, p)
			e.frames(3)
		}
		blur := func() {
			t.Helper()
			e.rt.Source().Execute(key.FocusCmd{})
			e.frames(3)
			if e.rt.Source().Focused(cb.FocusTag()) {
				t.Fatal("the composer kept the focus")
			}
		}
		cb = e.focusComposer()
		e.typeIn(cb, "tapped")
		tap(cb.SendRect())
		if f := posted(4, "a tap"); !strings.Contains(f.Get("text"), "tapped") {
			t.Errorf("the tap posted %v", f)
		}
		// 6. the editor unfocused after typing (a click went elsewhere): the
		// press on the button focuses the composer, the click still sends
		e.typeIn(cb, "clicked unfocused")
		blur()
		click("send button", cb.SendRect())
		if f := posted(5, "a click with the composer unfocused"); !strings.Contains(f.Get("text"), "clicked unfocused") {
			t.Errorf("the unfocused click posted %v", f)
		}
		cb = e.focusComposer()
		e.typeIn(cb, "tapped unfocused")
		blur()
		tap(cb.SendRect())
		if f := posted(6, "a tap with the composer unfocused"); !strings.Contains(f.Get("text"), "tapped unfocused") {
			t.Errorf("the unfocused tap posted %v", f)
		}
	}
	t.Run("channel", func(t *testing.T) { run(t, fs.ChannelIDs()[1], false) })
	t.Run("under the join banner", func(t *testing.T) { run(t, fakeslack.JoinBannerID, true) })
}
