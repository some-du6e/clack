package composer

// Texty's @group tooltip (group_tip.go): TextyTooltipUserGroup over a
// user group slug in the composer. Synthetic data and internal/fakeslack
// only. With COMPOSER_AUDIT_SHOTS_DIR set (composer-audit-shots) the
// tooltip is saved light and dark at 2x, for a 6-member group and a
// 14-member one ("… and 4 others").

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// bigGroupID is a synthetic workspace group of 14 (past
// TooltipMemberNames' 11).
const bigGroupID = "S0FAKETIPS"

// groupTipEnv is a composer over fakeslack with the boot's user groups
// (and bigGroupID) stored, the viewer signed in, and a count of
// usergroups.users.list calls.
type groupTipEnv struct {
	t     *testing.T
	app   *core.App
	c     *Composer
	r     input.Router
	ops   op.Ops
	now   time.Time
	scale float32
	size  image.Point
	mu    sync.Mutex
	lists map[string]int
}

func newGroupTipEnv(t *testing.T, dark bool, scale float32) *groupTipEnv {
	t.Helper()
	app, fs := fakeslackApp(t)
	stubTyping(t)
	e := &groupTipEnv{t: t, app: app, now: time.Now(), scale: scale, lists: map[string]int{}}
	big := make([]string, 14)
	for i := range big {
		big[i] = fmt.Sprintf("U0FAKE%05d", i+1)
	}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method != "usergroups.users.list" {
			return nil, false
		}
		e.mu.Lock()
		e.lists[form.Get("usergroup")]++
		e.mu.Unlock()
		if form.Get("usergroup") == bigGroupID {
			return map[string]any{"ok": true, "users": big}, true
		}
		return nil, false
	})
	t.Cleanup(func() { fs.SetHook(nil) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	gs, err := app.C.Usergroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gs = append(gs, slack.Usergroup{ID: bigGroupID, Handle: "bigcrew", Name: "Big crew", UserCount: 14, TeamID: fakeslack.TeamID})
	app.S.SetUsergroups(gs)
	app.S.Lock()
	app.S.Self = slack.User{ID: fakeslack.SelfID, Name: "self"}
	app.S.Unlock()
	ch := fs.ChannelIDs()[0]
	clearDraft(ch, "")
	t.Cleanup(func() { clearDraft(ch, "") })
	sh := theme.NewShaper()
	th := theme.New(dark)
	th.SetShapers(sh, sh)
	e.c = New(&slickHost{app: app, th: th}, ch, "")
	t.Cleanup(e.c.Close)
	e.c.st = e.c.editorStyle()
	e.size = image.Pt(int(560*scale), int(220*scale))
	return e
}

// listCalls is how many times usergroups.users.list was asked for group.
func (e *groupTipEnv) listCalls(group string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lists[group]
}

// frame lays out the editor 140dp down (room for the tooltip above it).
func (e *groupTipEnv) frame() {
	e.ops.Reset()
	th := e.c.h.Theme()
	paint.FillShape(&e.ops, th.C.Bg, clip.Rect{Max: e.size}.Op())
	gtx := layout.Context{Ops: &e.ops, Metric: unit.Metric{PxPerDp: e.scale, PxPerSp: e.scale}, Source: e.r.Source(), Now: e.now,
		Constraints: layout.Exact(e.size)}
	slackc.Frame(gtx)
	off := e.edOrigin()
	st := op.Offset(off).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(e.size.X-off.X*2, int(60*e.scale))}
	e.c.layoutEditor(g)
	st.Pop()
	e.r.Frame(&e.ops)
	e.now = e.now.Add(16 * time.Millisecond)
}

func (e *groupTipEnv) edOrigin() image.Point { return image.Pt(int(20*e.scale), int(140*e.scale)) }

// move puts the pointer at p (editor px) and lays out two frames.
func (e *groupTipEnv) move(p image.Point) {
	w := p.Add(e.edOrigin())
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(w.X), float32(w.Y))})
	e.frame()
	e.frame()
}

// atomCentre is the middle of the pill at rune pos.
func (e *groupTipEnv) atomCentre(pos int) image.Point {
	a, b := e.c.ed.PosRect(pos), e.c.ed.PosRect(pos+1)
	return image.Pt((a.Min.X+b.Min.X)/2, (a.Min.Y+a.Max.Y)/2)
}

// setDoc puts "cc " + the atom + " hi" in the composer.
func (e *groupTipEnv) setDoc(at *Atom) {
	d := NewDoc()
	d.Insert(0, "cc ", Attr{})
	d.InsertAtom(3, at)
	d.Insert(4, " hi", Attr{})
	e.c.ed.SetDoc(d)
	e.frame()
}

func (e *groupTipEnv) wait(what string, cond func() bool) {
	e.t.Helper()
	dl := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(dl) {
			e.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
		e.frame()
	}
}

func TestGroupTooltipLikeSlack(t *testing.T) {
	e := newGroupTipEnv(t, false, 1)
	g := &e.c.gtip
	e.setDoc(&Atom{Kind: AtomUsergroup, ID: fakeslack.WorkspaceGroupID, Text: "@snackcrew"})

	// no members stored: the hover fetches them, nothing shows meanwhile
	e.move(e.atomCentre(3))
	if g.last != fakeslack.WorkspaceGroupID {
		t.Fatalf("hovering the slug: group %q, want %s", g.last, fakeslack.WorkspaceGroupID)
	}
	e.wait("usergroups.users.list", func() bool { ids, _ := e.app.UsergroupTooltipIDs(fakeslack.WorkspaceGroupID); return len(ids) == 6 })
	if n := e.listCalls(fakeslack.WorkspaceGroupID); n != 1 {
		t.Errorf("usergroups.users.list asked %d times, want once", n)
	}
	e.frame()
	if !g.tip.Showing(e.now) {
		t.Fatal("the members came in but no tooltip shows (no delay: a Popover from onMouseOver)")
	}
	ids, _ := e.app.UsergroupTooltipIDs(fakeslack.WorkspaceGroupID)
	items := popups.TooltipMemberNameItems(ids, fakeslack.SelfID, e.app.S.MemberName)
	if len(items) != 6 || items[0].Text != "You" {
		t.Errorf("items %+v: want 6, the viewer (first) as You", items)
	}

	// off the slug it hides at once; back on, no second fetch
	e.move(image.Pt(2, e.atomCentre(3).Y))
	if g.last != "" || g.tip.Showing(e.now) {
		t.Errorf("off the slug: group %q, showing %v", g.last, g.tip.Showing(e.now))
	}
	e.move(e.atomCentre(3))
	if !g.tip.Showing(e.now) || e.listCalls(fakeslack.WorkspaceGroupID) != 1 {
		t.Errorf("back on the slug: showing %v, %d lists; want shown from the stored members, 1 list", g.tip.Showing(e.now), e.listCalls(fakeslack.WorkspaceGroupID))
	}

	// a selection with a length: showTooltip returns
	e.c.ed.SetSelection(0, 2)
	e.frame()
	e.frame()
	if g.tip.Showing(e.now) {
		t.Error("the tooltip shows with text selected")
	}
	e.c.ed.SetSelection(0, 0)

	// a person's slug has no group tooltip
	e.setDoc(&Atom{Kind: AtomUser, ID: fakeslack.ProfileID, Text: "@prof"})
	e.move(e.atomCentre(3))
	if g.last != "" || g.tip.Showing(e.now) {
		t.Errorf("over a user slug: group %q, showing %v", g.last, g.tip.Showing(e.now))
	}
}

// TooltipMemberNames past 11: the first 10 and "4 others".
func TestGroupTooltipTruncatesLikeSlack(t *testing.T) {
	e := newGroupTipEnv(t, false, 1)
	e.setDoc(&Atom{Kind: AtomUsergroup, ID: bigGroupID, Text: "@bigcrew"})
	e.move(e.atomCentre(3))
	e.wait("the 14 members", func() bool { ids, _ := e.app.UsergroupTooltipIDs(bigGroupID); return len(ids) == 14 })
	e.frame()
	ids, _ := e.app.UsergroupTooltipIDs(bigGroupID)
	items := popups.TooltipMemberNameItems(ids, fakeslack.SelfID, e.app.S.MemberName)
	if len(items) != 11 || items[10].Text != "4 others" {
		t.Errorf("14 members: %d items, last %+v; want 10 names and \"4 others\"", len(items), items[len(items)-1])
	}
	if !e.c.gtip.tip.Showing(e.now) {
		t.Error("no tooltip over the 14-member group")
	}
}

func TestGroupTooltipShots(t *testing.T) {
	dir := os.Getenv("COMPOSER_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("COMPOSER_AUDIT_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		for _, grp := range []struct{ id, handle, name string }{
			{fakeslack.WorkspaceGroupID, "@snackcrew", "group-tooltip"},
			{bigGroupID, "@bigcrew", "group-tooltip-14"},
		} {
			e := newGroupTipEnv(t, dark, 2)
			e.setDoc(&Atom{Kind: AtomUsergroup, ID: grp.id, Text: grp.handle})
			e.move(e.atomCentre(3))
			e.wait("the members", func() bool { ids, _ := e.app.UsergroupTooltipIDs(grp.id); return len(ids) > 0 })
			// the names: users.info for each member
			time.Sleep(500 * time.Millisecond)
			e.frame()
			e.frame()
			img := pixeltest.Render(t, e.size.X, e.size.Y, &e.ops)
			name := grp.name + "-" + map[bool]string{false: "light", true: "dark"}[dark]
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
