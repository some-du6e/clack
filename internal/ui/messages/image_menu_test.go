package messages

import (
	"encoding/json"
	"image"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// Right-clicks on the images in a message, as Slack answers them (web
// client a09b625d90aa: should_show_message_contextmenu Ay9+ leaves <img>
// and links to the desktop shell; its menus are main.bundle.cjs
// buildMenuForLink / buildMenuForImage, the same on macOS, Windows and
// Linux in 4.53.276):
//
//   - an image block's or a classic attachment's image is AttachmentImage,
//     the CSS background of an <a href>: Copy Link, Open Link, for the
//     link (the image block's image_url, the attachment's
//     getExternalSourceUrl);
//   - a section's accessory image (BaseImageElement, a background div):
//     the message actions menu;
//   - a reaction's emoji (an <img>): Copy Image, Copy Image URL; its
//     count (text): no menu; the rest of the pill: the message's menu;
//   - a bare image link's preview is a v3 image card, whose own context
//     menu (renderV3Attachment's tv) takes the whole card: Copy link.

const (
	tsImgBlock = "1700000020.000100"
	tsImgAtt   = "1700000021.000100"
	tsImgAcc   = "1700000022.000100"
	tsImgReact = "1700000023.000100"
	tsImgCard  = "1700000024.000100"

	imgURL       = "https://example.com/img/400x200/chart.png"
	imgEmojiLink = "https://emoji.slack-edge.com/T0266FRGM/party/0a1b2c3d4e5f6a7b.png"
)

// imgMenuMsgs are the messages, one per test: the harness is 700px
// tall, and the list keeps to its newest message.
var imgMenuMsgs = map[string]slack.Message{
	tsImgBlock: {Type: "message", TS: tsImgBlock, User: "UALICE", Text: "chart",
		Blocks: json.RawMessage(`[{"type":"image","block_id":"i1","image_url":"` + imgURL + `","alt_text":"chart"}]`)},
	tsImgAtt: {Type: "message", TS: tsImgAtt, BotID: "B0EMOJI", Username: "emojibot", Text: "<" + imgEmojiLink + ">", Attachments: []slack.Attachment{{
		ID: 1, Fallback: "128x128px image", Title: "party.png", FromURL: imgEmojiLink, ImageURL: imgEmojiLink, ImageWidth: 128, ImageHeight: 128}}},
	tsImgAcc: {Type: "message", TS: tsImgAcc, User: "UALICE", Text: "build passed",
		Blocks: json.RawMessage(`[{"type":"section","block_id":"s1","text":{"type":"mrkdwn","text":"build passed"},` +
			`"accessory":{"type":"image","image_url":"` + imgURL + `","alt_text":"badge"}}]`)},
	tsImgReact: {Type: "message", TS: tsImgReact, User: "UBOB", Text: "new emoji",
		Reactions: []slack.Reaction{{Name: "party", Count: 2, Users: []string{"UALICE", "UBOB"}}}},
	tsImgCard: {Type: "message", TS: tsImgCard, BotID: "B0EMOJI", Username: "emojibot", Text: "<" + imgEmojiLink + ">", Attachments: []slack.Attachment{{
		ID: 1, Fallback: "128x128px image", FromURL: imgEmojiLink, OriginalURL: imgEmojiLink, ImageURL: imgEmojiLink, ImageWidth: 128, ImageHeight: 128}}},
}

// imgMenuEnv shows message ts alone, with :party: a custom emoji.
func imgMenuEnv(t *testing.T, ts string) *ctxEnv {
	t.Helper()
	return newCtxEnvSetup(t, tGen, []slack.Message{imgMenuMsgs[ts]}, func(app *core.App) {
		app.S.SetEmojiList(map[string]string{"party": imgEmojiLink})
	})
}

// imageAt is the middle of message ts's only viewer image, in view
// coordinates.
func (e *ctxEnv) imageAt(ts string) f32.Point {
	e.t.Helper()
	r := e.row(ts)
	if r == nil {
		e.t.Fatalf("row %s isn't on screen", ts)
	}
	rs := r.rv.ImageRects()
	if len(rs) != 1 {
		e.t.Fatalf("row %s has %d images, want 1", ts, len(rs))
	}
	top := e.v.s.pos[e.v.s.sel.keyIdx[ts]]
	c := rs[0].R.Min.Add(rs[0].R.Size().Div(2))
	return f32.Pt(float32(r.bodyRect.Min.X+c.X)+.5, float32(top+r.bodyRect.Min.Y+c.Y)+.5)
}

// clipOf runs a DoCtx row and returns what it put on the clipboard.
func clipOf(t *testing.T, it popups.MenuItem) string {
	t.Helper()
	if it.DoCtx == nil {
		t.Fatalf("%q copies nothing", it.Label)
	}
	var r input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(100, 100))}
	it.DoCtx(gtx)
	r.Frame(&ops)
	_, b, _ := r.WriteClipboard()
	return string(b)
}

// linkMenuDoes checks a link menu for link: its rows, what Copy Link
// copies and what Open Link opens.
func linkMenuDoes(t *testing.T, m *popups.Menu, link, what string, opened *[]string) {
	t.Helper()
	if got := strings.Join(labels(m), "|"); got != "Copy Link|Open Link" {
		t.Fatalf("%s: menu %q, want the link menu Copy Link|Open Link", what, got)
	}
	if got := clipOf(t, m.Items()[0]); got != link {
		t.Errorf("%s: Copy Link copied %q, want %q", what, got, link)
	}
	n := len(*opened)
	m.Items()[1].Do()
	if len(*opened) != n+1 || (*opened)[n] != link {
		t.Errorf("%s: Open Link opened %q, want %q", what, *opened, link)
	}
}

// An image block's image is its link's background: the link menu for
// image_url.
func TestContextMenuOnImageBlock(t *testing.T) {
	e := imgMenuEnv(t, tsImgBlock)
	var opened []string
	openURL = func(u string) { opened = append(opened, u) }
	e.rightClick(e.imageAt(tsImgBlock))
	linkMenuDoes(t, e.menu(0, "right-click on an image block"), imgURL, "image block", &opened)
}

// A classic attachment's image (a bot posting an image link, the
// preview titled): the link menu for getExternalSourceUrl, here the
// from_url off Slack's hosts.
func TestContextMenuOnAttachmentImage(t *testing.T) {
	e := imgMenuEnv(t, tsImgAtt)
	var opened []string
	openURL = func(u string) { opened = append(opened, u) }
	e.rightClick(e.imageAt(tsImgAtt))
	linkMenuDoes(t, e.menu(0, "right-click on an attachment image"), imgEmojiLink, "attachment image", &opened)
}

// A section's accessory image is BaseImageElement's background div: the
// message list opens the message's own menu.
func TestContextMenuOnAccessoryImage(t *testing.T) {
	e := imgMenuEnv(t, tsImgAcc)
	e.rightClick(e.imageAt(tsImgAcc))
	m := e.menu(0, "right-click on an accessory image")
	if !hasLabel(m, "Add reaction…") || !hasLabel(m, "Copy link") || hasLabel(m, "Copy Image") {
		t.Errorf("accessory image menu %q, want the message actions menu", labels(m))
	}
}

// reactionPoint is a point of message ts's first reaction pill, dx px
// from its left edge, on its middle line (view coordinates).
func (e *ctxEnv) reactionPoint(ts string, dx int) f32.Point {
	e.t.Helper()
	r := e.row(ts)
	if r == nil || len(r.reactRects) == 0 {
		e.t.Fatalf("row %s has no reaction pill on screen", ts)
	}
	pill := r.reactRects[0]
	top := e.v.s.pos[e.v.s.sel.keyIdx[ts]]
	return f32.Pt(float32(pill.Min.X+dx)+.5, float32(top+pill.Min.Y+pill.Dy()/2)+.5)
}

// A reaction pill: its emoji is an <img> (Copy Image, Copy Image URL for
// the emoji's image), its count text has no menu, its padding is the
// message's.
func TestContextMenuOnReaction(t *testing.T) {
	e := imgMenuEnv(t, tsImgReact)
	es := int(mt.reactEmoji) // PxPerDp 1
	e.rightClick(e.reactionPoint(tsImgReact, 8+es/2))
	m := e.menu(0, "right-click on a reaction's emoji")
	if got := strings.Join(labels(m), "|"); got != "Copy Image|Copy Image URL" {
		t.Fatalf("reaction emoji menu %q, want the image menu", got)
	}
	if got := clipOf(t, m.Items()[1]); got != imgEmojiLink {
		t.Errorf("Copy Image URL copied %q, want the emoji's %q", got, imgEmojiLink)
	}
	e.rightClick(e.reactionPoint(tsImgReact, 8+es+4+2))
	if n := len(e.h.menus()); n != 1 {
		t.Errorf("a right-click on the reaction's count opened a menu (%d menus)", n)
	}
	e.rightClick(e.reactionPoint(tsImgReact, 2))
	if m := e.menu(1, "right-click on a reaction's padding"); !hasLabel(m, "Add reaction…") {
		t.Errorf("reaction padding menu %q, want the message actions menu", labels(m))
	}
}

// The preview of a bare image link is a v3 image card: its own menu
// takes the whole card, image included (Copy link; Remove preview only
// for the sender), as in Slack.
func TestContextMenuOnImageCard(t *testing.T) {
	e := imgMenuEnv(t, tsImgCard)
	e.rightClick(e.imageAt(tsImgCard))
	m := e.menu(0, "right-click on an image card")
	if got := strings.Join(labels(m), "|"); got != "Copy link" {
		t.Fatalf("image card menu %q, want Copy link", got)
	}
	if got := clipOf(t, m.Items()[0]); got != imgEmojiLink {
		t.Errorf("Copy link copied %q, want %q", got, imgEmojiLink)
	}
}
