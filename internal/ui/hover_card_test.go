package ui

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/hovercard"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
)

// hcShot saves the window (HOVER_SHOTS_DIR set, in the hover-card
// workflow: synthetic fakeslack data only).
func hcShot(t *testing.T, e *perfEnv, name string) {
	t.Helper()
	dir := os.Getenv("HOVER_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	e.frame()
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func (e *perfEnv) move(p f32.Point) {
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	e.frame()
}

// The whole client on the fake Slack: resting the pointer on a message
// author's name opens Slack's hover card after 350ms above the name; the
// pointer may cross onto the card; leaving closes it after 250ms; Escape
// closes it before the composer sees the key; a menu blocks it.
func TestHoverCardInChannel(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("HOVER_SHOTS_DIR") != "" {
		w, err := headless.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	e.openChannel(fs.ChannelIDs()[1])
	e.settle(800 * time.Millisecond)
	ctl := &e.a.hover.ctl

	// find a sender name: sweep the messages pane until a trigger reports
	var at f32.Point
	var req hovercard.Request
	found := false
	x0 := float32(e.a.primaryOrigin.X)
	for y := float32(e.size.Y - 200); y > 150 && !found; y -= 5 {
		for _, dx := range []float32{68, 76, 90} {
			e.move(f32.Pt(x0+dx, y))
			if r, ok := ctl.Pending(); ok {
				at, req, found = f32.Pt(x0+dx, y), r, true
				break
			}
		}
	}
	if !found {
		t.Fatal("no message author name reported to the hover card")
	}
	if req.UserID == "" || !image.Pt(int(at.X), int(at.Y)).In(req.Anchor) {
		t.Fatalf("trigger %+v doesn't contain the pointer %v", req, at)
	}
	if _, _, open := ctl.Open(); open {
		t.Fatal("the card opened without the delay")
	}
	time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
	e.frame()
	got, _, open := ctl.Open()
	if !open || got.UserID != req.UserID {
		t.Fatalf("after 400ms: open=%v (%+v), want %s's card", open, got, req.UserID)
	}
	e.settle(150 * time.Millisecond) // fade, avatar
	card := e.a.hover.card
	if card == nil {
		t.Fatal("no card drawn")
	}
	r := card.Rect()
	if r.Max.Y != req.Anchor.Min.Y-hovercard.OffsetY && r.Min.Y != req.Anchor.Max.Y+hovercard.OffsetY {
		t.Errorf("card %v isn't 12px above or below the trigger %v", r, req.Anchor)
	}
	if r.Dx() != 340 {
		t.Errorf("card width %d", r.Dx())
	}
	hcShot(t, e, "hover-card-message-author")

	// cross onto the card (within the 250ms grace) and stay there
	mid := f32.Pt(float32(r.Min.X+r.Dx()/2), float32((r.Min.Y+r.Max.Y)/2))
	e.move(mid)
	time.Sleep(hovercard.HideDelay + 100*time.Millisecond)
	e.frame()
	if _, _, open := ctl.Open(); !open {
		t.Fatal("the card closed with the pointer on it")
	}
	// leave both: closed after 250ms
	e.move(f32.Pt(float32(e.size.X-40), float32(e.size.Y-40)))
	e.frame()
	if _, _, open := ctl.Open(); !open {
		t.Fatal("the card closed without the 250ms grace")
	}
	time.Sleep(hovercard.HideDelay + 50*time.Millisecond)
	e.frame()
	if _, _, open := ctl.Open(); open {
		t.Fatal("the card stayed open after the pointer left")
	}

	// Escape closes it, and only it
	e.move(at)
	time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
	e.frame()
	if _, _, open := ctl.Open(); !open {
		t.Fatal("the card didn't reopen")
	}
	e.key(key.NameEscape, 0)
	e.frame()
	if _, _, open := ctl.Open(); open {
		t.Fatal("Escape left the card open")
	}

	// a menu over the window: no card under it
	e.move(f32.Pt(float32(e.size.X-40), float32(e.size.Y-40)))
	e.a.ShowOverlay(popups.NewMenu(e.a, image.Pt(40, 40), []popups.MenuItem{{Label: "Copy"}}))
	e.frame()
	e.move(at)
	time.Sleep(hovercard.ShowDelay + 50*time.Millisecond)
	e.frame()
	if _, _, open := ctl.Open(); open {
		t.Fatal("a card opened under a menu")
	}

	e.a.dismissTop()
	e.frame()

	// Slack's variants, from the fake's fixed users: every field, a
	// workspace admin, a deactivated account, Slackbot, and yourself
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ids := []string{fakeslack.ProfileID, fakeslack.AdminID, fakeslack.DeactivatedID, fakeslack.SlackbotID, fakeslack.SelfID}
	e.a.data.core.EnsureUsers(ctx, ids)
	for _, id := range ids {
		anchor := image.Rect(e.a.primaryOrigin.X+60, e.size.Y-180, e.a.primaryOrigin.X+160, e.size.Y-158)
		ctl.Show(hovercard.Request{Key: id, UserID: id, Anchor: anchor}, time.Now())
		e.frame()
		r := e.a.hover.card.Rect()
		e.move(f32.Pt(float32(r.Min.X+20), float32(r.Min.Y+20))) // rest on the card
		e.settle(200 * time.Millisecond)
		if got, _, open := ctl.Open(); !open || got.UserID != id {
			t.Fatalf("%s: card not open (%+v)", id, got)
		}
		if r.Max.Y != anchor.Min.Y-hovercard.OffsetY {
			t.Errorf("%s: card %v not 12px above %v", id, r, anchor)
		}
		hcShot(t, e, "hover-card-"+strings.ToLower(id))
		e.move(f32.Pt(float32(e.size.X-40), 60))
		time.Sleep(hovercard.HideDelay + 50*time.Millisecond)
		e.frame()
	}
}
