package blockkit

// Elements inside other blocks are live. Slack renders a container's and
// a callout's child_blocks and a fallback's payload through the surface's
// renderables with its onAction (Container yWQF's childBlocks slot,
// Callout's createRenderBlocks a+Oj, Q5CE resolveFallback), so their
// buttons send block_actions with the child block's id.

import (
	"encoding/json"
	"image"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// TestNestedBlockElements: in a message, a container's actions buttons,
// a callout's section accessory and a fallback payload's section
// accessory each draw, and a click sends block_actions with the child
// block's id and the message container.
func TestNestedBlockElements(t *testing.T) {
	e := newBKEnv(t, false, false)
	ts := e.fs.GalleryElementsTS()
	e.show("nested")
	n := 0
	for _, c := range []struct{ bid, aid, text, value string }{
		{"bk_nested_actions", "nested_go", "Ship it", "go"},
		{"bk_nested_actions", "nested_wait", "Wait", "wait"},
		{"bk_nested_callout_sec", "nested_callout", "Approve", "yes"},
		{"bk_nested_fallback", "nested_fallback", "Retry", "retry"},
	} {
		e.idle(c.bid)
		e.clickEl(c.bid, c.aid)
		n++
		got := e.calls(n)[n-1]
		sameJSON(t, c.aid, got.Actions, []any{map[string]any{"type": "button", "block_id": c.bid, "action_id": c.aid,
			"text": pt(c.text), "value": c.value}})
		sameJSON(t, c.aid+" container", got.Container, map[string]any{"type": "message", "message_ts": ts["nested"],
			"channel_id": fakeslack.GalleryChannelID, "is_ephemeral": false})
	}
	// nested in an attachment's container: the attachment's container
	// (AttachmentBlocks' containerActionsPayload: message_attachment with
	// its attachment_id, for every action from its renderer)
	e.idle("bk_nested_att_actions")
	e.clickEl("bk_nested_att_actions", "nested_att_go")
	n++
	got := e.calls(n)[n-1]
	sameJSON(t, "attachment's nested action", got.Actions, []any{map[string]any{"type": "button", "block_id": "bk_nested_att_actions",
		"action_id": "nested_att_go", "text": pt("Deploy"), "value": "deploy"}})
	sameJSON(t, "attachment's nested container", got.Container, map[string]any{"type": "message_attachment", "message_ts": ts["nested"],
		"attachment_id": 1, "channel_id": fakeslack.GalleryChannelID, "is_ephemeral": false})
}

// TestCardElements: a card's action is a live element (Card's actions
// render through the SUI renderer with the block's onAction): it draws,
// and a click sends block_actions with the card's block id.
func TestCardElements(t *testing.T) {
	e := newBKEnv(t, false, false)
	ts := e.fs.GalleryElementsTS()
	e.show("nested")
	e.clickEl("bk_nested_card", "nested_card")
	got := e.calls(1)[0]
	sameJSON(t, "card action", got.Actions, []any{map[string]any{"type": "button", "block_id": "bk_nested_card", "action_id": "nested_card",
		"text": pt("Open"), "value": "open"}})
	sameJSON(t, "card container", got.Container, map[string]any{"type": "message", "message_ts": ts["nested"],
		"channel_id": fakeslack.GalleryChannelID, "is_ephemeral": false})
}

// TestSurfaceNestedElements: in a modal and in App Home, a container's
// actions child, a callout's section accessory and a card's action are the surface's own
// element widgets (drawn through richview's Elements host), not
// richview's static buttons, which only open links.
func TestSurfaceNestedElements(t *testing.T) {
	h, _ := visibleHost()
	blocks := []any{
		map[string]any{"type": "container", "block_id": "c1", "title": map[string]any{"type": "plain_text", "text": "Deploy"},
			"child_blocks": []any{map[string]any{"type": "actions", "block_id": "a1", "elements": []any{visBtn("go")}}}},
		map[string]any{"type": "callout", "block_id": "c2", "background_color": "blue",
			"child_blocks": []any{map[string]any{"type": "section", "block_id": "s1",
				"text": map[string]any{"type": "mrkdwn", "text": "Approve?"}, "accessory": visBtn("ok")}}},
		map[string]any{"type": "card", "block_id": "k1", "title": map[string]any{"type": "plain_text", "text": "A card"},
			"actions": []any{visBtn("cardgo")}},
	}
	raw, _ := json.Marshal(blocks)
	for _, typ := range []string{"modal", "home"} {
		s := newSurface(h, &core.View{Type: typ, Blocks: raw})
		s.Layout(visGtx(480))
		for _, k := range []string{"a1\x00go\x00button", "s1\x00ok\x00button", "k1\x00cardgo\x00button"} {
			if w := s.ws[k]; w == nil || w.main.sz == (image.Point{}) {
				t.Errorf("%s: %q isn't a drawn element widget", typ, k)
			}
		}
	}
}

// TestNestedElementsRichview: richview's own container, callout and
// fallback layout, with the message as its Elements host, gives every
// nested button a drawn widget.
func TestNestedElementsRichview(t *testing.T) {
	e := newBKEnv(t, false, false)
	m := e.msgs["nested"]
	me := MessageElements(e.h, &m)
	v := richview.New(m.Doc, richview.Options{Elements: me})
	for range 2 {
		v.Layout(e.context(), styleFor(e.h.th, e.h.img), &surfHandler{h: e.h})
	}
	for _, k := range []string{"bk_nested_actions\x00nested_go", "bk_nested_actions\x00nested_wait",
		"bk_nested_callout_sec\x00nested_callout", "bk_nested_fallback\x00nested_fallback", "bk_nested_card\x00nested_card"} {
		if w := me.ws[k]; w == nil || w.main.sz == (image.Point{}) {
			t.Errorf("%q isn't drawn", k)
		}
	}
}

// TestSurfaceNestedClick: in a modal a fallback payload's button and a
// container child's button are the surface's own elements: Q5CE's
// resolveFallback renders the payload, and Container its child_blocks,
// with the view's onAction, so a press sends block_actions with the child
// block's id and the view's container.
func TestSurfaceNestedClick(t *testing.T) {
	e := newBKEnv(t, false, false)
	raw, _ := json.Marshal([]any{
		map[string]any{"type": "container", "block_id": "c1", "title": map[string]any{"type": "plain_text", "text": "Deploy"},
			"child_blocks": []any{map[string]any{"type": "actions", "block_id": "a1", "elements": []any{visBtn("go")}}}},
		map[string]any{"type": "fallback_canary", "block_id": "f1", "fallback": []any{map[string]any{"type": "section", "block_id": "s1",
			"text": map[string]any{"type": "mrkdwn", "text": "Approve?"}, "accessory": visBtn("ok")}}},
	})
	// a view's actions go out with its app's bot as the service
	// (ViewContainer: bot_id, else invalid_service_id before any request)
	v := &core.View{ID: "V0FAKENEST", RootViewID: "V0FAKENEST", Type: "modal", BotID: "B0FAKENEST", AppID: "A0FAKENEST",
		Title: mustJSON(Text{Type: "plain_text", Text: "Nested"}), Close: mustJSON(Text{Type: "plain_text", Text: "Cancel"}), Blocks: raw}
	st := newStack(e.h, v)
	e.h.ShowOverlay(st)
	for range 3 {
		e.frame()
	}
	s := st.views[len(st.views)-1]
	n := len(e.fs.BlockActionCalls())
	for _, c := range []struct{ bid, aid string }{{"s1", "ok"}, {"a1", "go"}} {
		w := s.ws[c.bid+"\x00"+c.aid+"\x00button"]
		if w == nil || w.main.sz == (image.Point{}) {
			t.Fatalf("%s/%s isn't a drawn element of the modal (widget %v)", c.bid, c.aid, w)
		}
		w.press()
		n++
		got := e.calls(n)[n-1]
		if len(got.Actions) != 1 || got.Actions[0]["block_id"] != c.bid || got.Actions[0]["action_id"] != c.aid || got.Actions[0]["type"] != "button" {
			t.Errorf("%s: actions %v, want the button with block_id %s", c.aid, got.Actions, c.bid)
		}
		if got.Container["type"] != "view" || got.Container["view_id"] != "V0FAKENEST" {
			t.Errorf("%s's container %v, want the view V0FAKENEST", c.aid, got.Container)
		}
		e.until(c.bid+"'s action answered", 10*time.Second, func() bool { e.frame(); return !s.loading(c.bid) })
	}
}
