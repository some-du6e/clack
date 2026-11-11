package composer

import (
	"encoding/json"
	"image"
	"regexp"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// gifTestHost is a host with the ui root's emoji and GIF picker: it
// records how the picker was asked for.
type gifTestHost struct {
	*ovHost
	opens []gifOpen
}

type gifOpen struct {
	anchor image.Rectangle
	onGif  func(popups.Gif)
	gifs   bool
}

func (h *gifTestHost) OpenEmojiGifPicker(anchor image.Rectangle, _ func(string), onGif func(popups.Gif), gifs bool) {
	h.opens = append(h.opens, gifOpen{anchor, onGif, gifs})
}

func testGif(id string) popups.Gif {
	return popups.Gif{ID: id, Title: "cat " + id, URL: "https://media.tenor.test/" + id + "/full.gif", GifW: 498, GifH: 280, GifBytes: 123456,
		GifStill: "https://media.tenor.test/" + id + "/full.png", GifStillBytes: 4567,
		Preview: "https://media.tenor.test/" + id + "/tiny.gif", Still: "https://media.tenor.test/" + id + "/tiny.png",
		TinyW: 220, TinyH: 124, TinyBytes: 23456, TinyStillBytes: 789}
}

// xVsA GifListItem: with giphy_fixed_width the payload is the gif and
// gifpreview formats, without it tinygif and tinygifpreview; a plugin's
// own payload goes through as it is.
func TestGifSelectionLikeSlack(t *testing.T) {
	g := testGif("a")
	full := g.Selected(true)
	if full != (popups.GifSelection{OriginalGifURL: g.URL, PreviewURL: g.GifStill, Name: "cat a", Width: 498, Height: 280, Bytes: 123456, PreviewBytes: 4567}) {
		t.Errorf("fixed width %+v", full)
	}
	tiny := g.Selected(false)
	if tiny != (popups.GifSelection{OriginalGifURL: g.Preview, PreviewURL: g.Still, Name: "cat a", Width: 220, Height: 124, Bytes: 23456, PreviewBytes: 789}) {
		t.Errorf("tinygif %+v", tiny)
	}
	g.Picked = &popups.GifSelection{OriginalGifURL: "https://x.test/p.gif", Name: "plugin"}
	if g.Selected(true).OriginalGifURL != "https://x.test/p.gif" {
		t.Error("a plugin's payload was replaced")
	}
}

// EwsX: one attachment per GIF, {blocks: [image block]} with a uuid
// block_id, the fallback as alt text, is_animated and the "GIF" title.
func TestGifAttachmentsLikeEwsX(t *testing.T) {
	gs := []*draftGif{{sel: testGif("a").Selected(true)}, {sel: popups.GifSelection{OriginalGifURL: "https://x.test/b.gif", Width: 10, Height: 20, Bytes: 30}}}
	var atts []struct {
		Blocks []map[string]any `json:"blocks"`
	}
	if err := json.Unmarshal(gifAttachments(gs, true), &atts); err != nil {
		t.Fatal(err)
	}
	if len(atts) != 2 || len(atts[0].Blocks) != 1 || len(atts[1].Blocks) != 1 {
		t.Fatalf("attachments %+v", atts)
	}
	b := atts[0].Blocks[0]
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if b["type"] != "image" || !uuid.MatchString(b["block_id"].(string)) || b["image_url"] != "https://media.tenor.test/a/full.gif" ||
		b["alt_text"] != "cat a" || b["fallback"] != "cat a" || b["image_width"] != 498.0 || b["image_height"] != 280.0 ||
		b["image_bytes"] != 123456.0 || b["is_animated"] != true {
		t.Errorf("block %v", b)
	}
	if tt, _ := b["title"].(map[string]any); tt["type"] != "plain_text" || tt["text"] != "GIF" || tt["emoji"] != true {
		t.Errorf("title %v", b["title"])
	}
	if b := atts[1].Blocks[0]; b["alt_text"] != "GIF" || b["fallback"] != "GIF" {
		t.Errorf("a GIF without a name falls back to \"GIF\": %v", b)
	}
	if gifAttachments(nil, true) != nil {
		t.Error("no GIFs, no attachments")
	}
	if back := gifsFromDraft(gifAttachments(gs, true)); len(back) != 2 || back[0].sel.OriginalGifURL != gs[0].sel.OriginalGifURL || back[1].sel.Name != "" {
		t.Errorf("read back from a draft: %+v", back)
	}
}

// The + menu (useComposerIAFormatter): with allow_native_gif_picker, "GIF"
// first with EducationTag's "New" while seen_gif_new_badge < 3; its row
// opens the picker on the GIFs tab at the + button; picking a GIF adds it
// to the draft (handleSelectGif, once per URL) instead of sending it, and
// the message then goes out with EwsX's attachments.
func TestGifGoesToDraftLikeSlack(t *testing.T) {
	app, fs := syntheticApp(t, func(*store.Store) {})
	app.SetTeamPrefs(map[string]json.RawMessage{"allow_native_gif_picker": json.RawMessage("true")})
	stubTyping(t)
	h := &gifTestHost{ovHost: &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}}
	c := freshComposer(t, h)

	pm := c.plusMenu()
	if pm.gif == nil {
		t.Fatal("no GIF row with allow_native_gif_picker on")
	}
	plus := image.Rect(300, 700, 328, 728)
	pm.anchor = plus
	items := c.att.plusMenuItems(pm)
	if len(items) < 3 || items[0].Label != "GIF" || items[0].Icon != "gif" || items[1].Label != "Text snippet" {
		t.Fatalf("rows %+v", items)
	}
	if tag := items[0].Tag; tag == nil || tag.Label != "New" || tag.Style != "education" {
		t.Errorf("GIF row's tag %+v, want EducationTag's micro New", tag)
	}
	items[0].Do()
	if len(h.opens) != 1 || !h.opens[0].gifs || h.opens[0].anchor != plus {
		t.Fatalf("picker opens %+v, want one on the GIFs tab at the + button", h.opens)
	}
	pick := h.opens[0].onGif
	pick(testGif("a"))
	pick(testGif("a")) // the same GIF again replaces it
	pick(testGif("b"))
	if n := c.att.gifCount(); n != 2 {
		t.Fatalf("%d GIFs in the draft, want 2", n)
	}
	if len(fs.called("chat.postMessage")) != 0 {
		t.Fatal("picking a GIF sent it")
	}

	// the emoji button opens the same picker (Emoji tab), GIFs to the draft
	c.openEmojiPicker(image.Rect(400, 700, 428, 728))
	if len(h.opens) != 2 || h.opens[1].gifs {
		t.Errorf("emoji button opens %+v", h.opens)
	}

	// drawn in the attachments row, after which a GIF-only message sends
	ops := new(op.Ops)
	for range 3 {
		ops.Reset()
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Constraints{Max: image.Pt(900, 600)}, Now: time.Now()}
		c.Layout(gtx)
	}
	if !c.chromeState(layout.Context{}).CanSend {
		t.Error("the send button is off with only GIFs in the draft")
	}
	c.submit()
	if c.att.gifCount() != 0 {
		t.Error("the GIFs stayed in the composer after sending")
	}
	waitUntil(t, "chat.postMessage", func() bool { return len(fs.called("chat.postMessage")) == 1 })
	var atts []struct {
		Blocks []map[string]any `json:"blocks"`
	}
	raw := fs.called("chat.postMessage")[0].Get("attachments")
	if err := json.Unmarshal([]byte(raw), &atts); err != nil || len(atts) != 2 {
		t.Fatalf("attachments %q (%v)", raw, err)
	}
	want := testGif("a").Selected(giphyFixedWidth(app)).OriginalGifURL
	if atts[0].Blocks[0]["image_url"] != want || atts[0].Blocks[0]["is_animated"] != true {
		t.Errorf("first GIF %v, want %s", atts[0].Blocks[0], want)
	}
}

// Without allow_native_gif_picker there is no GIF row and the emoji
// button is the plain emoji picker.
func TestNoGifRowWithoutThePicker(t *testing.T) {
	app, _ := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	h := &gifTestHost{ovHost: &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}}
	c := freshComposer(t, h)
	if c.plusMenu().gif != nil {
		t.Error("a GIF row without allow_native_gif_picker")
	}
	c.openEmojiPicker(image.Rect(400, 700, 428, 728))
	if len(h.opens) != 0 {
		t.Error("the GIF picker opened without allow_native_gif_picker")
	}
}

// useComposerIAFormatter writes Text snippet's hint "⌘⇧Enter" on macOS,
// ⌘ first, and "⌘O" for Upload.
func TestPlusMenuMacHints(t *testing.T) {
	old := shortcut.GOOS
	shortcut.GOOS = "darwin"
	t.Cleanup(func() { shortcut.GOOS = old })
	app, _ := syntheticApp(t, func(*store.Store) {})
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	a := newAttachments(h)
	items := a.plusMenuItems(plusMenu{ch: tChGen, snippet: func() {}})
	if len(items) != 2 || items[0].Hint != "⌘⇧Enter" || items[1].Hint != "⌘O" {
		t.Errorf("hints %q %q, want ⌘⇧Enter and ⌘O", items[0].Hint, items[len(items)-1].Hint)
	}
}
