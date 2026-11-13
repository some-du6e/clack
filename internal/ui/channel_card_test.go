package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"gioui.org/f32"
	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// chcLink is the window box of the visible message link to channel ch.
func (e *perfEnv) chcLink(conv, ch string) image.Rectangle {
	c := e.a.convos[conv+"/"]
	if c == nil {
		return image.Rectangle{}
	}
	l, ok := c.list.(channelList)
	if !ok {
		return image.Rectangle{}
	}
	var u image.Rectangle // the link's fragments (its icon, its name), as the trigger unions them
	for _, r := range l.LinkRects() {
		if r.Kind == richview.HitChannel && r.ID == ch {
			u = u.Union(r.R)
		}
	}
	return u
}

// chcOpen rests the pointer on the #channel link to ch until its card
// opens (350ms) and returns the trigger's box.
func (e *perfEnv) chcOpen(t *testing.T, conv, ch string) image.Rectangle {
	t.Helper()
	var r image.Rectangle
	dl := time.Now().Add(5 * time.Second)
	for r = e.chcLink(conv, ch); r.Empty() && time.Now().Before(dl); r = e.chcLink(conv, ch) {
		e.frame()
		time.Sleep(20 * time.Millisecond)
	}
	if r.Empty() {
		var seen []string
		if c := e.a.convos[conv+"/"]; c != nil {
			if l, ok := c.list.(channelList); ok {
				for _, lr := range l.LinkRects() {
					seen = append(seen, fmt.Sprintf("%s %d %s %v", lr.TS, lr.Kind, lr.ID, lr.R))
				}
			}
		} else {
			seen = append(seen, "no conversation view")
		}
		t.Fatalf("no link to %s in %s; links: %q; route %q", ch, conv, seen, e.a.router.Current().Channel())
	}
	mid := f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2))
	e.move(mid)
	ctl := &e.a.hover.ctl
	if p, ok := ctl.Pending(); !ok || p.ChannelID != ch || !p.Buttons || p.Texty {
		t.Fatalf("over the link to %s: pending %+v %v, want its channel card with buttons", ch, p, ok)
	}
	if _, _, open := ctl.Open(); open {
		t.Fatal("the card opened without HoverCardTrigger's 350ms")
	}
	time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
	e.frame()
	if req, _, open := ctl.Open(); !open || req.ChannelID != ch {
		t.Fatalf("after 400ms: open %v %+v", open, req)
	}
	return r
}

// The whole client on the fake Slack: a message's #channel opens Slack's
// ChannelHoverCard after 350ms, centred 12px above the link; the pointer
// may rest on the card; leaving closes it after 250ms; Escape closes it;
// Join Channel joins (conversations.join) and the card then offers View
// Channel, which goes to the channel and closes the card. Shots of the
// Slack Connect, not-member and archived cards, light and dark.
func TestChannelHoverCardInChannel(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	conv := fakeslack.ThreadPublicID // one fixture message: the posted one is newest and in view
	fs.PostQuiet(conv, fakeslack.ProfileID, "Notes in <#"+fakeslack.ThreadSharedID+"> and <#"+fakeslack.ThreadElsewhereID+
		">, the old ones in <#"+fakeslack.ThreadArchivedID+">")
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
	away := f32.Pt(float32(e.a.primaryOrigin.X)-60, float32(e.size.Y)/2) // the sidebar: no hover cards there
	leave := func() {
		e.move(away)
		time.Sleep(hovercard.HideDelay + 50*time.Millisecond)
		e.frame()
	}
	// waitCard waits for the open card to satisfy ok (a theme switch
	// rebuilds the message's link, whose card then reopens on its own
	// 350ms with the pointer still on it)
	waitCard := func(what string, ok func(c *popups.ChannelHoverCard) bool) {
		e.until(what, 5*time.Second, func() bool {
			c := e.a.hover.chc
			_, _, open := ctl.Open()
			return open && c != nil && ok(c)
		})
	}
	shotBoth := func(name string, ok func(c *popups.ChannelHoverCard) bool) {
		for _, dark := range []bool{false, true} {
			e.setDark(dark)
			time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
			e.frame()
			waitCard(name, ok)
			e.settle(150 * time.Millisecond)
			hcShot(t, e, name+"-"+map[bool]string{false: "light", true: "dark"}[dark])
		}
		e.setDark(false)
		time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
		e.frame()
		waitCard(name, ok)
	}

	// Slack Connect: every row, View Channel
	r := e.chcOpen(t, conv, fakeslack.ThreadSharedID)
	e.until("the shared card's rows", 5*time.Second, func() bool {
		c := e.a.hover.chc
		if c == nil || c.State() != popups.ChannelCardFull {
			return false
		}
		it := c.Items()
		return len(it) == 3 && it[0] != "" && it[1] != "" && it[2] == "Partner Co is in this channel"
	})
	card := e.a.hover.chc.Rect()
	if card.Dx() != 340 {
		t.Errorf("card width %d", card.Dx())
	}
	above := card.Max.Y == r.Min.Y-hovercard.OffsetY
	below := card.Min.Y == r.Max.Y+hovercard.OffsetY
	if !above && !below {
		t.Errorf("card %v isn't 12px above or below the link %v", card, r)
	}
	if c := card.Min.X + card.Dx()/2; c != r.Min.X+r.Dx()/2 && card.Min.X != hovercard.AllowanceX && card.Max.X != e.size.X-hovercard.AllowanceX {
		t.Errorf("card %v isn't centred on the link %v (position top)", card, r)
	}
	if b := e.a.hover.chc.Button(); b != "View Channel" {
		t.Errorf("button %q", b)
	}
	e.settle(300 * time.Millisecond) // the partner's icon
	shotBoth("channel-card-shared", func(c *popups.ChannelHoverCard) bool { return len(c.Items()) == 3 && c.Items()[1] != "" })
	card = e.a.hover.chc.Rect()
	// off the link straight to the sidebar: 250ms later it's gone
	e.move(away)
	time.Sleep(hovercard.HideDelay + 50*time.Millisecond)
	e.frame()
	if req, _, open := ctl.Open(); open {
		t.Fatalf("still open after leaving the link for %v: %+v (the link's view hovered: %v)", away, req, keyViewHovered(req.Key))
	}
	r = e.chcOpen(t, conv, fakeslack.ThreadSharedID)
	e.until("the shared card", 5*time.Second, func() bool { return e.a.hover.chc != nil })
	card = e.a.hover.chc.Rect()
	// onto the card and resting there: it stays
	e.move(f32.Pt(float32(card.Max.X-30), float32(card.Min.Y+20))) // clear of the senders' avatars and names under it
	time.Sleep(hovercard.HideDelay + 100*time.Millisecond)
	e.frame()
	if _, _, open := ctl.Open(); !open {
		t.Fatal("the card closed with the pointer on it")
	}
	// off both: 250ms later it's gone
	e.move(away)
	if _, _, open := ctl.Open(); !open {
		t.Fatal("closed without the 250ms grace")
	}
	time.Sleep(hovercard.HideDelay + 50*time.Millisecond)
	e.frame()
	if req, _, open := ctl.Open(); open {
		p, pend := ctl.Pending()
		t.Fatalf("still open after the pointer left for %v: %+v (over the card %v; pending %v %+v; the link's view hovered: %v)", away, req, ctl.OverCard(), pend, p, keyViewHovered(req.Key))
	}
	// Escape closes it
	e.chcOpen(t, conv, fakeslack.ThreadSharedID)
	e.key(key.NameEscape, 0)
	e.frame()
	if _, _, open := ctl.Open(); open {
		t.Fatal("Escape left the card open")
	}
	leave()

	// archived: no rows, View Channel
	e.chcOpen(t, conv, fakeslack.ThreadArchivedID)
	e.until("the archived card", 5*time.Second, func() bool {
		c := e.a.hover.chc
		return c != nil && c.State() == popups.ChannelCardFull && len(c.Items()) == 0
	})
	shotBoth("channel-card-archived", func(c *popups.ChannelHoverCard) bool { return len(c.Items()) == 0 })
	leave()

	// a channel the viewer isn't in: Join Channel joins it
	e.chcOpen(t, conv, fakeslack.ThreadElsewhereID)
	e.until("the not-member card", 5*time.Second, func() bool {
		c := e.a.hover.chc
		return c != nil && c.Button() == "Join Channel" && len(c.Items()) == 2 && c.Items()[0] != "" && c.Items()[1] != ""
	})
	shotBoth("channel-card-join", func(c *popups.ChannelHoverCard) bool {
		return c.Button() == "Join Channel" && len(c.Items()) == 2 && c.Items()[1] != ""
	})
	card = e.a.hover.chc.Rect()
	btn := image.Pt(card.Min.X+card.Dx()/2, card.Max.Y-16-14) // the 28px button, 16 from the bottom
	e.move(f32.Pt(float32(btn.X), float32(btn.Y)))
	e.clickAt(btn, 10*time.Second)
	e.frame()
	e.until("conversations.join", 5*time.Second, func() bool {
		return slices.ContainsFunc(fs.JoinCalls(), func(j fakeslack.JoinCall) bool { return j.Channel == fakeslack.ThreadElsewhereID })
	})
	e.until("View Channel after the join", 5*time.Second, func() bool {
		c := e.a.hover.chc
		return c != nil && c.Button() == "View Channel"
	})
	if _, _, open := ctl.Open(); !open {
		t.Fatal("joining closed the card (Slack's joinChannel leaves it open)")
	}
	if got := e.a.router.Current().Channel(); got != conv {
		t.Errorf("the join navigated to %q", got)
	}
	// View Channel goes there and closes the card
	e.clickAt(btn, 12*time.Second)
	e.frame()
	e.until("the joined channel", 5*time.Second, func() bool { return e.a.router.Current().Channel() == fakeslack.ThreadElsewhereID })
	if _, _, open := ctl.Open(); open {
		t.Error("View Channel left the card open")
	}
}

// keyViewHovered reports whether the richview behind a mention's trigger
// key (richview's mentionKey{v, t}) thinks the pointer is on it.
func keyViewHovered(k any) string {
	rv := reflect.ValueOf(k)
	if rv.Kind() != reflect.Struct || rv.NumField() == 0 {
		return "not a mention key"
	}
	f := rv.Field(0)
	if f.Kind() != reflect.Pointer || f.IsNil() {
		return "no view"
	}
	return fmt.Sprint((*richview.View)(unsafe.Pointer(f.Pointer())).Pointer())
}

// chcRectOf is the window box of the first visible link in conv's view
// that ok accepts (all its fragments).
func (e *perfEnv) chcRectOf(conv string, ok func(richview.LinkRect) bool) image.Rectangle {
	c := e.a.convos[conv+"/"]
	if c == nil {
		return image.Rectangle{}
	}
	l, isList := c.list.(channelList)
	if !isList {
		return image.Rectangle{}
	}
	var u image.Rectangle
	for _, r := range l.LinkRects() {
		if ok(r.LinkRect) {
			u = u.Union(r.R)
		}
	}
	return u
}

// The whole client on the fake Slack with the code channel fixtures: a
// message's #code-channel opens CodeChannelEntityCard (the done one with
// its summary, the stale one's minus tile); a channel with a huddle on
// shows the huddle badge in the card's name row; a channel-tab slug to a
// tab with a peek (the Code tab, session_channels_slug_peek on) opens no
// card, one to a tab the channel doesn't have opens the channel card; a
// click on the code card goes to the channel and closes it. Light and
// dark shots.
func TestChannelHoverCardCodeHuddleTabs(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader, sz.CodeChannels = true, true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	conv := fakeslack.ThreadPublicID
	text := func(s string) string { return `{"type":"text","text":"` + s + `"},` }
	chEl := func(id, tab string) string {
		if tab != "" {
			return `{"type":"channel","channel_id":"` + id + `","tab_id":"` + tab + `"},`
		}
		return `{"type":"channel","channel_id":"` + id + `"},`
	}
	gone := "Ct0GONE000001"
	els := text("Code ") + chEl(fakeslack.CodeDoneID, "") + text(" and ") + chEl(fakeslack.CodeStaleID, "") +
		text(", tabs ") + chEl(fakeslack.CodeWorkingID, "code") + text(" and ") + chEl(fakeslack.CodeWorkingID, gone) +
		text(", a huddle in ") + chEl(fakeslack.ThreadSharedID, "")
	blocks := `[{"type":"rich_text","block_id":"hc2","elements":[{"type":"rich_text_section","elements":[` + strings.TrimSuffix(els, ",") + `]}]}]`
	fs.PostBlocksQuiet(conv, fakeslack.ProfileID, "Code channels, tabs and a huddle", json.RawMessage(blocks))
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("HOVER_SHOTS_DIR") != "" {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	// the done channel's summary is read from the store: its history
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if _, err := e.a.Core().History(ctx, fakeslack.CodeDoneID, ""); err != nil {
		t.Fatal(err)
	}
	cancel()
	e.openChannel(conv)
	e.settle(800 * time.Millisecond)
	ctl := &e.a.hover.ctl
	away := f32.Pt(float32(e.a.primaryOrigin.X)-60, float32(e.size.Y)/2)
	leave := func() {
		e.move(away)
		time.Sleep(hovercard.HideDelay + 50*time.Millisecond)
		e.frame()
	}
	waitCard := func(what string, ok func(c *popups.ChannelHoverCard) bool) {
		e.until(what, 5*time.Second, func() bool {
			c := e.a.hover.chc
			_, _, open := ctl.Open()
			return open && c != nil && ok(c)
		})
	}
	shotBoth := func(name string, ok func(c *popups.ChannelHoverCard) bool) {
		for _, dark := range []bool{false, true} {
			e.setDark(dark)
			time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
			e.frame()
			waitCard(name, ok)
			e.settle(150 * time.Millisecond)
			hcShot(t, e, name+"-"+map[bool]string{false: "light", true: "dark"}[dark])
		}
		e.setDark(false)
		time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
		e.frame()
		waitCard(name, ok)
	}
	code := func(c *popups.ChannelHoverCard) popups.CodeCardState {
		st, _ := c.CodeCard(time.Now())
		return st
	}

	// the done code channel: the summary, Done, the merged PR's branch tile
	e.chcOpen(t, conv, fakeslack.CodeDoneID)
	done := func(c *popups.ChannelHoverCard) bool {
		st := code(c)
		return c.State() == popups.ChannelCardCode && st.Summary != "" && len(st.Faces) > 1
	}
	waitCard("the done code card", done)
	if st := code(e.a.hover.chc); st.Tag != "Done" || st.Icon != "branch" || st.Subtitle != "Code channel" {
		t.Errorf("done card %+v", st)
	}
	if w := e.a.hover.chc.Rect().Dx(); w != 340 {
		t.Errorf("code card width %d", w)
	}
	e.settle(300 * time.Millisecond) // the faces
	// the facepile's tooltip (F2XN, 150ms) over the card's own button
	if fp := e.a.hover.chc.FacepileRect(); fp.Empty() {
		t.Error("no facepile on the done card")
	} else {
		e.move(f32.Pt(float32(fp.Min.X+fp.Dx()/2), float32(fp.Min.Y+fp.Dy()/2)))
		time.Sleep(250 * time.Millisecond)
		e.frame()
		e.frame()
		if !e.a.hover.chc.FacepileTipShowing(time.Now()) {
			t.Error("no tooltip on the facepile after 250ms")
		}
	}
	shotBoth("channel-card-code-done", done)
	leave()

	// the stale one (quiet for 5 days): minus, Inactive
	e.chcOpen(t, conv, fakeslack.CodeStaleID)
	stale := func(c *popups.ChannelHoverCard) bool {
		st := code(c)
		return c.State() == popups.ChannelCardCode && st.Tag == "Inactive" && st.Icon == "minus"
	}
	waitCard("the stale code card", stale)
	shotBoth("channel-card-code-stale", stale)
	leave()

	// a huddle on in the shared channel: the badge in the name row
	users := fs.GroupDMUsers()
	raw, _ := json.Marshal(map[string]any{"user": users[0].ID, "room": map[string]any{
		"id": "R0CARD", "channels": []string{fakeslack.ThreadSharedID}, "participants": []string{users[0].ID, users[1].ID},
		"date_start": time.Now().Unix() - 60, "name": ""}})
	e.a.huddles().Handle(core.Event{Type: "huddle", Name: "sh_room_join", Channel: fakeslack.ThreadSharedID, Raw: raw})
	e.chcOpen(t, conv, fakeslack.ThreadSharedID)
	huddleOn := func(c *popups.ChannelHoverCard) bool { return c.State() == popups.ChannelCardFull && c.HuddleBadge() }
	waitCard("the huddle badge", huddleOn)
	e.settle(300 * time.Millisecond)
	shotBoth("channel-card-huddle", huddleOn)
	leave()

	// the Code tab's slug: its peek (not drawn by Clack), no channel card
	slug := func(tab string) image.Rectangle {
		var r image.Rectangle
		e.until("the slug to "+tab, 5*time.Second, func() bool {
			r = e.chcRectOf(conv, func(l richview.LinkRect) bool {
				return l.Kind == richview.HitLink && l.ID == fakeslack.CodeWorkingID && strings.HasSuffix(l.URL, "/tabs/"+tab)
			})
			return !r.Empty()
		})
		return r
	}
	r := slug("code")
	e.move(f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2)))
	if p, ok := ctl.Pending(); ok {
		t.Errorf("the Code tab's slug started a card: %+v", p)
	}
	time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
	e.frame()
	if req, _, open := ctl.Open(); open {
		t.Errorf("the Code tab's slug opened %+v (Slack shows the tab's peek)", req)
	}
	leave()
	// a tab the channel doesn't have: BaseMrkdwnChannel's channel card
	r = slug(gone)
	e.move(f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2)))
	if p, ok := ctl.Pending(); !ok || p.ChannelID != fakeslack.CodeWorkingID || !p.Buttons {
		t.Fatalf("over the unresolved tab's slug: pending %+v %v, want the working channel's card", p, ok)
	}
	time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
	e.frame()
	tabCard := func(c *popups.ChannelHoverCard) bool {
		return c.State() == popups.ChannelCardCode && len(code(c).Faces) > 1
	}
	waitCard("the tab slug's card", tabCard)
	shotBoth("channel-card-tab-slug", tabCard)
	leave()

	// a click on the code card goes to the channel and closes it
	e.chcOpen(t, conv, fakeslack.CodeDoneID)
	waitCard("the done code card again", done)
	card := e.a.hover.chc.Rect()
	mid := image.Pt(card.Max.X-40, card.Min.Y+30) // the header, clear of the facepile's tooltip
	e.move(f32.Pt(float32(mid.X), float32(mid.Y)))
	e.clickAt(mid, 10*time.Second)
	e.frame()
	e.until("the done channel", 5*time.Second, func() bool { return e.a.router.Current().Channel() == fakeslack.CodeDoneID })
	if _, _, open := ctl.Open(); open {
		t.Error("the click left the code card open")
	}
}
