package composer

// Texty's tooltips and hover cards over the composer's atoms
// (texty_tip.go): an emoji's title tooltip and a person's profile card.
// Synthetic data and internal/fakeslack only. With
// COMPOSER_AUDIT_SHOTS_DIR set (composer-audit-shots) the emoji tooltip
// is saved light and dark at 2x, in the middle of the box and at its
// left edge against the window's.

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/hovercard"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// windowIs makes the test frame the window for tooltip placement: its
// size, and the last pointer move (frame coordinates are window ones).
func (e *groupTipEnv) windowIs(ptr *image.Point) {
	ws, wp := theme.WindowSize, theme.WindowPointer
	e.t.Cleanup(func() { theme.WindowSize, theme.WindowPointer = ws, wp })
	theme.WindowSize = func() image.Point { return e.size }
	theme.WindowPointer = func() (image.Point, bool) { return *ptr, true }
}

// moveWin is move with the window pointer kept up to date.
func (e *groupTipEnv) moveWin(ptr *image.Point, p image.Point) {
	*ptr = p.Add(e.edOrigin())
	e.move(p)
	e.frame() // the tooltip's origin learnt from the move
}

// emojiAtom has a long name, so its tooltip is wider than the room left
// of an emoji at the start of the box.
func emojiAtom() *Atom {
	return &Atom{Kind: AtomEmoji, ID: "large_green_circle", Text: ":large_green_circle:", Uni: "🟢"}
}

func TestEmojiTooltipLikeSlack(t *testing.T) {
	e := newGroupTipEnv(t, false, 1)
	var ptr image.Point
	e.windowIs(&ptr)
	tt := &e.c.ttip.tip
	e.setDoc(emojiAtom())
	e.moveWin(&ptr, e.atomCentre(3))
	if !tt.Showing(e.now) {
		t.Fatal("no tooltip over the emoji (a Popover from onMouseOver: no delay)")
	}
	if got := emojiTitle(emojiAtom()); got != ":large_green_circle:" {
		t.Errorf("title %q, want the emoji's data-title :large_green_circle:", got)
	}
	pl := tt.Placed()
	if pl.Pos != theme.TipTop || pl.Box.Empty() {
		t.Errorf("placed %s %v, want over the emoji (TooltipTip --top)", pl.Pos, pl.Box)
	}
	if pl.Arrow.Half == 0 || pl.Arrow.C.Y != pl.Box.Max.Y {
		t.Errorf("arrow %+v: want it on the box's bottom edge", pl.Arrow)
	}
	if pl.Arrow.Half == 0 {
		t.Error("no arrow (c-tooltip__tip__arrow)")
	}
	// off the emoji it hides at once (no hover card in the document)
	e.moveWin(&ptr, image.Pt(2, e.atomCentre(3).Y))
	if tt.Showing(e.now) {
		t.Error("the tooltip stays off the emoji")
	}
	// not while text is selected
	e.moveWin(&ptr, e.atomCentre(3))
	e.c.ed.SetSelection(0, 2)
	e.frame()
	e.frame()
	if tt.Showing(e.now) {
		t.Error("the tooltip shows with text selected")
	}
	e.c.ed.SetSelection(0, 0)
	// a person's slug has no title tooltip
	e.setDoc(&Atom{Kind: AtomUser, ID: fakeslack.ProfileID, Text: "@prof"})
	e.moveWin(&ptr, e.atomCentre(3))
	if tt.Showing(e.now) {
		t.Error("a title tooltip over a user slug")
	}
}

// At the window's left edge the tooltip shifts inside instead of being
// clipped (Texty's Popover: allowance 0), its arrow still on the emoji.
func TestEmojiTooltipKeepsInsideTheWindow(t *testing.T) {
	e := newGroupTipEnv(t, false, 1)
	var ptr image.Point
	e.windowIs(&ptr)
	d := NewDoc()
	d.InsertAtom(0, emojiAtom())
	e.c.ed.SetDoc(d)
	e.frame()
	e.moveWin(&ptr, e.atomCentre(0))
	tt := &e.c.ttip.tip
	if !tt.Showing(e.now) {
		t.Fatal("no tooltip")
	}
	pl := tt.Placed()
	org := e.edOrigin()
	box := pl.Box.Add(org)
	if box.Min.X < 0 || box.Max.X > e.size.X || box.Min.Y < 0 {
		t.Errorf("tooltip %v leaves the %v window", box, e.size)
	}
	if c := e.atomCentre(0).X; pl.Arrow.C.X < c-2 || pl.Arrow.C.X > c+2 {
		t.Errorf("arrow at x %d, want on the emoji (%d)", pl.Arrow.C.X, c)
	}
}

// A person's slug opens their profile card through the root's hover card
// at once (Request.Texty); an app's or a bot's doesn't.
func TestSlugProfileCardLikeSlack(t *testing.T) {
	e := newGroupTipEnv(t, false, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	e.app.EnsureUsers(ctx, []string{fakeslack.ProfileID, fakeslack.ThreadAppUserID})
	if u, ok := e.app.User(fakeslack.ProfileID); !ok || u.IsBot {
		t.Fatalf("the profile fixture: %+v %v", u, ok)
	}
	var ptr image.Point
	prev := hovercard.Default()
	ctl := &hovercard.Controller{Pointer: func() image.Point { return ptr }}
	hovercard.SetDefault(ctl)
	t.Cleanup(func() { hovercard.SetDefault(prev) })
	e.setDoc(&Atom{Kind: AtomUser, ID: fakeslack.ProfileID, Text: "@prof"})
	ptr = e.atomCentre(3).Add(e.edOrigin())
	e.move(e.atomCentre(3))
	e.frame()
	ctl.Frame(e.now, false, false)
	req, _, open := ctl.Open()
	if !open || req.UserID != fakeslack.ProfileID || !req.Texty {
		t.Fatalf("over a person's slug: open %v, %+v; want their card at once (Texty)", open, req)
	}
	// the slug's window bounds: under the pointer, inside its line
	line := e.c.ed.PosRect(3).Add(e.edOrigin())
	if !ptr.In(req.Anchor) || req.Anchor.Min.Y < line.Min.Y || req.Anchor.Max.Y > line.Max.Y || req.Anchor.Dx() < 8 {
		t.Errorf("anchor %v: want the slug's box under the pointer %v, in its line %v", req.Anchor, ptr, line)
	}
	// off the slug: closes after hideTooltipDelayed's 250ms
	e.move(image.Pt(2, e.atomCentre(3).Y))
	ctl.Frame(e.now, false, false)
	if _, _, open := ctl.Open(); !open {
		t.Error("closed at once; Texty waits 250ms (the pointer may be going to the card)")
	}
	ctl.Frame(e.now.Add(300*time.Millisecond), false, false)
	if _, _, open := ctl.Open(); open {
		t.Error("still open 300ms after leaving the slug")
	}
	// an app's bot user: no profile card (renderComposerAppHoverCard)
	e.setDoc(&Atom{Kind: AtomUser, ID: fakeslack.ThreadAppUserID, Text: "@launch"})
	e.move(e.atomCentre(3))
	e.frame()
	ctl.Frame(e.now.Add(time.Second), false, false)
	if req, _, open := ctl.Open(); open {
		t.Errorf("a bot's slug opened %+v", req)
	}
}

func TestTextyTooltipShots(t *testing.T) {
	dir := os.Getenv("COMPOSER_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("COMPOSER_AUDIT_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		for _, edge := range []bool{false, true} {
			e := newGroupTipEnv(t, dark, 2)
			var ptr image.Point
			e.windowIs(&ptr)
			pos := 3
			if edge {
				d := NewDoc()
				d.InsertAtom(0, emojiAtom())
				e.c.ed.SetDoc(d)
				e.frame()
				pos = 0
			} else {
				e.setDoc(emojiAtom())
			}
			time.Sleep(300 * time.Millisecond) // the emoji image
			e.moveWin(&ptr, e.atomCentre(pos))
			e.frame()
			img := pixeltest.Render(t, e.size.X, e.size.Y, &e.ops)
			name := "emoji-tooltip"
			if edge {
				name += "-left-edge"
			}
			name += "-" + map[bool]string{false: "light", true: "dark"}[dark]
			f, err := os.Create(filepath.Join(dir, name+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}
