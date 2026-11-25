package ui

import (
	"image"
	"os"
	"sync"
	"testing"
	"time"

	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Karim, α.165 on macOS (a 1920pt window at zoom +1, 1745 CSS px), #clack
// with the thread pane open: the tab bar showed only Messages, then More,
// and the topic read "slack client in go: https://…" with room to spare.
// #clack's first tab is a canvas titled with a whole sentence. Slack's
// .channel_tab_label / .channel_tab_document_label are max-width 200px
// (padding in) with the label ellipsized (.channel_tab_label_text), and
// measureVisibleTabs reads the tabs' button rects, so that canvas is one
// 200px tab and everything fits. Clack measured the canvas tab at its
// whole title, wider than the bar, and folded it and every tab after it.
// The topic's ellipsis is text-overflow on its line: it cuts at the
// character, so the link keeps all of itself that fits; Clack's one-line
// layout broke at the link's last break opportunity, after "https://".
// Checked at zoom -1..+2, 1x and 2x, with the thread pane open and
// closed, the sidebar shown and hidden.
func TestHeaderTabsAndTopicFit(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	const ch = "C0FAKE00001"
	const title = "clack testingcanvas mention test from the Clack orchestrator, as you asked: clack testing"
	const link = "https://github.com/some-du6e/clack/tree/ui/docs/releases/alpha-165-notes-and-the-train-that-follows"
	fs.AddCanvas(fakeslack.FakeCanvas{ID: "F0HDRTABS01", Channel: ch, Title: title,
		TitleBlocks: []any{map[string]any{"type": "rich_text", "block_id": "t", "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": title}}}}}}})
	fs.SetTopic(ch, "slack client in go: <"+link+">")
	e := startEnv(t, fs, t.TempDir())
	e.waitReady()
	size, scale := e.size, e.scale
	t.Cleanup(func() {
		e.a.prefs.Zoom, e.a.sidebar.hidden = 0, false
		e.size, e.scale = size, scale
		theme.LayoutHook = nil
	})
	e.openChannel(ch)
	e.until("the canvas tab", 15*time.Second, func() bool {
		for _, tb := range e.a.channelTabs(ch) {
			if tb.fileID == "F0HDRTABS01" {
				return true
			}
		}
		return false
	})
	e.until("the topic", 10*time.Second, func() bool { c := e.a.model().conv(ch); return c != nil && c.Topic != "" })
	e.until("the channel's history", 15*time.Second, func() bool { return e.loadedMessages(ch) > 0 })
	rootTS := e.a.data.core.S.Timeline(ch).Msgs[0].TS
	for _, base := range []float32{1, 2} {
		for _, z := range []int{-1, 0, 1, 2} {
			for _, thread := range []bool{true, false} {
				for _, hidden := range []bool{false, true} {
					zf := zoomFactors[z-zoomMin]
					e.a.prefs.Zoom, e.scale = z, base*zf
					e.size = image.Pt(int(1920*base), int(1064*base))
					e.a.sidebar.hidden = hidden
					if thread {
						e.a.OpenThread(ch, rootTS)
					} else {
						e.a.CloseSecondary()
					}
					e.settle(200 * time.Millisecond)
					tag := "base " + ftoa(base) + ", zoom " + itoa(z) + map[bool]string{true: ", thread open", false: ", no thread"}[thread] + map[bool]string{true: ", sidebar hidden", false: ""}[hidden]
					m := unit.Metric{PxPerDp: e.scale, PxPerSp: e.scale}
					tabs := e.a.channelTabs(ch)
					gtx := e.frameContext()
					for _, tb := range tabs {
						if w := tabWidth(gtx, tb, e.a.tabLabelWidth(gtx, tb)); w > m.Dp(200) {
							t.Errorf("%s: the %q tab is %d px, over .channel_tab_label's 200px (%d px)", tag, tb.label, w, m.Dp(200))
						}
					}
					// Karim's: every tab shows (Messages, the canvas, Files & links)
					if hid := e.a.TabsHidden(ch); (z == 0 || z == 1) && len(hid) > 0 {
						t.Errorf("%s: tabs %v folded into More, want all %d to show", tag, hid, len(tabs))
					}
					// the topic: the link is cut at a character, never to its scheme
					var mu sync.Mutex
					shown, cut, seen := 0, false, false
					theme.LayoutHook = func(txt string, n int, c bool) {
						if txt == link {
							mu.Lock()
							shown, cut, seen = n, c, true
							mu.Unlock()
						}
					}
					e.frame()
					theme.LayoutHook = nil
					if base == 1 && z == 1 && thread && !hidden && (!seen || !cut) {
						t.Errorf("%s: the topic's link drawn %v, cut %v: Karim's header must cut it (the test proves nothing otherwise)", tag, seen, cut)
					}
					if !seen {
						continue // the header has no topic under 600px, or the link doesn't start in it
					}
					if cut && shown <= len("https://") {
						t.Errorf("%s: the topic's link shows %d runes (%q…), cut at its scheme", tag, shown, link[:shown])
					}
				}
			}
		}
	}
	// Karim's layout, light and dark (header-tabs-thread-*.png)
	if os.Getenv("PREVIEW_SHOTS_DIR") != "" {
		e.size, e.scale, e.a.prefs.Zoom, e.a.sidebar.hidden = image.Pt(1920, 1064), 1.1, 1, false
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		if e.gpu != nil {
			e.gpu.Release()
		}
		e.gpu = w
		e.a.OpenThread(ch, rootTS)
		e.settle(400 * time.Millisecond)
		pfShot(e, "header-tabs-thread")
	}
}
