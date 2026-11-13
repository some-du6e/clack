package ui

import (
	"encoding/json"
	"image"
	"os"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// The whole client on the fake Slack: a channel-tab slug whose tab has a
// peek (session_channels_slug_peek on) opens that tab's peek through
// HoverCardTrigger (350ms, 12px above or below the slug, centred, 250ms
// close), not the channel card: the code channel's Code tab (getDiff,
// reason code_tab_peek: the branch and the stats), its html agent view
// (getView: the iframe's no-sandbox text) and block kit view
// (getBlockKitView), a canvas tab (the canvas in preview: no files.open),
// the Files & links tab (no search, the files and links), a list tab
// (a click opens the tab) and the Workflows tab. Light and dark shots of
// each.
func TestTabSlugPeeks(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader, sz.CodeChannels, sz.Files, sz.Workflows = true, true, true, true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	conv := fakeslack.ThreadPublicID
	type slugCase struct {
		name, ch, tab string
	}
	cases := []slugCase{
		{"code", fakeslack.CodeWorkingID, "code"},
		{"agent-html", fakeslack.CodeWorkingID, fakeslack.CodeHTMLViewID},
		{"agent-blocks", fakeslack.CodeWorkingID, "V0SUMMARY"},
		{"canvas", fakeslack.WorkflowsChannelID, "Ct" + fakeslack.CanvasChannel},
		{"files", fakeslack.FilesChannelID, fakeslack.FilesTabID},
		{"workflows", fakeslack.WorkflowsChannelID, "workflows"},
		{"list", fakeslack.WorkflowsChannelID, "Ct" + fakeslack.ListTodos},
	}
	var els []string
	for i, c := range cases {
		if i > 0 {
			els = append(els, `{"type":"text","text":" "}`)
		}
		els = append(els, `{"type":"channel","channel_id":"`+c.ch+`","tab_id":"`+c.tab+`"}`)
	}
	blocks := `[{"type":"rich_text","block_id":"tp","elements":[{"type":"rich_text_section","elements":[` + strings.Join(els, ",") + `]}]}]`
	fs.PostBlocksQuiet(conv, fakeslack.ProfileID, "Tab slugs", json.RawMessage(blocks))
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("HOVER_SHOTS_DIR") != "" {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	e.openChannel(conv)
	e.settle(800 * time.Millisecond)
	ctl := &e.a.hover.ctl
	away := f32.Pt(float32(e.a.primaryOrigin.X)-60, float32(e.size.Y)/2)
	leave := func() {
		e.move(away)
		time.Sleep(hovercard.HideDelay + 50*time.Millisecond)
		e.frame()
	}
	slug := func(c slugCase) image.Rectangle {
		var r image.Rectangle
		e.until("the slug to "+c.name, 10*time.Second, func() bool {
			r = e.chcRectOf(conv, func(l richview.LinkRect) bool {
				return l.Kind == richview.HitLink && l.ID == c.ch && strings.HasSuffix(l.URL, "/tabs/"+c.tab)
			})
			return !r.Empty()
		})
		return r
	}
	open := func(c slugCase) image.Rectangle {
		t.Helper()
		r := slug(c)
		e.move(f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2)))
		if p, ok := ctl.Pending(); !ok || p.Tab != c.tab || p.ChannelID != c.ch || p.Buttons {
			t.Fatalf("%s: pending %+v %v, want the tab's peek", c.name, p, ok)
		}
		if _, _, op := ctl.Open(); op {
			t.Fatalf("%s: open before HoverCardTrigger's 350ms", c.name)
		}
		time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
		e.frame()
		if req, _, op := ctl.Open(); !op || req.Tab != c.tab {
			t.Fatalf("%s: after 400ms open %v %+v", c.name, op, req)
		}
		return r
	}
	peekText := func() string { return e.a.hover.tpk.slugPeekTabText() }
	waitPeek := func(c slugCase, ok func(s string) bool) {
		t.Helper()
		e.until(c.name+"'s peek", 10*time.Second, func() bool {
			p := e.a.hover.tpk
			return p != nil && !p.Rect().Empty() && ok(peekText())
		})
	}
	shotBoth := func(c slugCase, ok func(s string) bool) {
		for _, dark := range []bool{false, true} {
			e.setDark(dark)
			time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
			e.frame()
			waitPeek(c, ok)
			e.settle(300 * time.Millisecond)
			hcShot(t, e, "tab-peek-"+c.name+"-"+map[bool]string{false: "light", true: "dark"}[dark])
		}
		e.setDark(false)
		time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
		e.frame()
		waitPeek(c, ok)
	}
	place := func(c slugCase, r image.Rectangle) {
		t.Helper()
		card := e.a.hover.tpk.Rect()
		if card.Dx() != 360 {
			t.Errorf("%s: peek width %d, want 360", c.name, card.Dx())
		}
		if card.Max.Y != r.Min.Y-hovercard.OffsetY && card.Min.Y != r.Max.Y+hovercard.OffsetY {
			t.Errorf("%s: peek %v isn't 12px above or below the slug %v", c.name, card, r)
		}
	}
	anyText := func(string) bool { return true }

	// the Code tab: CodeTabPeek
	c := cases[0]
	r := open(c)
	codeOK := func(s string) bool {
		return strings.HasPrefix(s, "code: main ← fix-login-redirect | 3 commits | ") && strings.Contains(s, "files changed")
	}
	waitPeek(c, codeOK)
	place(c, r)
	reason := ""
	for _, call := range fs.CodeCalls() {
		if call.Method == "client.codeChannels.getDiff" {
			reason = call.Args.Get("reason")
		}
	}
	if reason != "code_tab_peek" {
		t.Errorf("getDiff reason %q, want code_tab_peek", reason)
	}
	shotBoth(c, codeOK)
	leave()
	if _, _, op := ctl.Open(); op {
		t.Fatal("the Code peek stayed open after the pointer left")
	}

	// the html agent view and the block kit one
	for _, c := range cases[1:3] {
		want := map[string]string{"agent-html": "agent_view: html", "agent-blocks": "agent_view: block_kit"}[c.name]
		open(c)
		ok := func(s string) bool { return s == want }
		waitPeek(c, ok)
		if h := e.a.hover.tpk.Rect().Dy(); h != 240 {
			t.Errorf("%s: height %d, want contentHeight 240", c.name, h)
		}
		shotBoth(c, ok)
		leave()
	}

	// the canvas tab: the canvas in preview, never registered as open
	c = cases[3]
	open(c)
	waitPeek(c, anyText)
	e.until("the canvas in the peek", 10*time.Second, func() bool {
		b, ok := e.a.hover.tpk.body.(*canvasTabPeek)
		return ok && b.v != nil && b.v.Doc() != nil
	})
	if opened, _ := fs.CanvasCalls(fakeslack.CanvasChannel); opened != 0 {
		t.Errorf("the canvas preview sent files.open %d times (QuipEmbed with isPreview mounts no watcher)", opened)
	}
	shotBoth(c, anyText)
	leave()

	// the Files & links tab: no search, its files and links
	c = cases[4]
	open(c)
	var fb *filesTabPeek
	e.until("the files peek's rows", 15*time.Second, func() bool {
		var ok bool
		fb, ok = e.a.hover.tpk.body.(*filesTabPeek)
		if !ok {
			return false
		}
		rows := fb.v.PeekRows()
		return len(rows) > 0 && rows[0] != "loading"
	})
	for _, row := range fb.v.PeekRows() {
		if strings.HasPrefix(row, "empty:") {
			t.Errorf("files peek rows %q", fb.v.PeekRows())
		}
	}
	shotBoth(c, anyText)
	leave()

	// the Workflows tab: its bookmarked workflows, or the empty state
	c = cases[5]
	open(c)
	wfOK := func(s string) bool { return s != "workflows: loading" }
	waitPeek(c, wfOK)
	shotBoth(c, wfOK)
	leave()

	// a list tab: the list in its peek context; a click opens the tab
	c = cases[6]
	open(c)
	waitPeek(c, anyText)
	e.settle(500 * time.Millisecond)
	shotBoth(c, anyText)
	card := e.a.hover.tpk.Rect()
	mid := image.Pt(card.Min.X+card.Dx()/2, card.Min.Y+card.Dy()/2)
	e.move(f32.Pt(float32(mid.X), float32(mid.Y)))
	e.clickAt(mid, 10*time.Second)
	e.frame()
	e.until("the list tab's channel", 10*time.Second, func() bool { return e.a.router.Current().Channel() == fakeslack.WorkflowsChannelID })
	if _, _, op := ctl.Open(); op {
		t.Error("the click left the list peek open")
	}
}

// A canvas's channel mention card (Slack's embedded ChannelHovercard):
// 322px, under the mention with its left edge on the mention's; the name,
// "Joined", the member count; the ⋯ menu holds the card open and has View
// channel details (which opens the details) and Copy name. Light and
// dark shots, and one with the menu open.
func TestCanvasChannelCard(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("HOVER_SHOTS_DIR") != "" {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	e.openChannel("C0FAKE00000")
	e.a.OpenCanvas(fakeslack.CanvasChannel)
	e.settle(1500 * time.Millisecond)
	id, ok := e.a.Core().ResolveCanvasMention(richtext.Channel, "#general")
	if !ok {
		t.Fatal("no #general in the fixtures")
	}
	ctl := &e.a.hover.ctl
	anchor := image.Rect(e.size.X-400, 220, e.size.X-340, 240) // a mention in the side panel's canvas
	show := func() {
		ctl.Show(hovercard.Request{Key: "canvas-mention", ChannelID: id, Canvas: true, Delay: hovercard.CanvasShowDelay, Anchor: anchor}, time.Now())
		e.frame()
		e.until("the canvas card", 10*time.Second, func() bool {
			c := e.a.hover.cvc
			if c == nil || c.Rect().Empty() {
				return false
			}
			st := c.State()
			return st.Name == "general" && len(st.Metadata) >= 2
		})
		card := e.a.hover.cvc.Rect()
		e.move(f32.Pt(float32(card.Min.X+40), float32(card.Max.Y-10))) // rest on the card: it stays
		e.frame()
	}
	show()
	c := e.a.hover.cvc
	card := c.Rect()
	if card.Dx() != 322 {
		t.Errorf("card width %d, want 322", card.Dx())
	}
	if card.Min.X != anchor.Min.X || card.Min.Y != anchor.Max.Y {
		t.Errorf("card %v isn't right under the mention %v, left-aligned", card, anchor)
	}
	st := c.State()
	if st.Icon != "channel" || st.Metadata[0] != "Joined" || !strings.HasSuffix(st.Metadata[1], "members") {
		t.Errorf("card state %+v", st)
	}
	var labels []string
	for _, it := range c.MenuItems() {
		labels = append(labels, it.Label)
	}
	if strings.Join(labels, "|") != "View channel details|Copy name" {
		t.Errorf("menu %q", labels)
	}
	for _, dark := range []bool{false, true} {
		e.setDark(dark)
		e.settle(300 * time.Millisecond)
		if _, _, open := ctl.Open(); !open || e.a.hover.cvc == nil {
			show()
		}
		hcShot(t, e, "canvas-channel-card-"+map[bool]string{false: "light", true: "dark"}[dark])
	}
	e.setDark(false)
	e.settle(300 * time.Millisecond)
	if _, _, open := ctl.Open(); !open || e.a.hover.cvc == nil {
		show()
	}
	// the ⋯ menu: the card stays under it
	m := e.a.hover.cvc.MoreRect()
	e.move(f32.Pt(float32(m.Min.X+m.Dx()/2), float32(m.Min.Y+m.Dy()/2)))
	e.clickAt(image.Pt(m.Min.X+m.Dx()/2, m.Min.Y+m.Dy()/2), 20*time.Second)
	e.frame()
	e.until("the card's menu", 5*time.Second, func() bool { return e.a.hover.cvc != nil && e.a.hover.cvc.Holding() })
	e.settle(300 * time.Millisecond)
	if _, _, open := ctl.Open(); !open {
		t.Fatal("the menu closed the card")
	}
	hcShot(t, e, "canvas-channel-card-menu-light")
	// View channel details
	e.a.hover.cvc.MenuItems()[0].Do()
	e.until("channel details", 10*time.Second, func() bool {
		_, ok := topOverlay[detailsOverlay](e.a)
		return ok
	})
}
