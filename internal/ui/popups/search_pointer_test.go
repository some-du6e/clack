package popups

import (
	"context"
	"image"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
)

// Ctrl+K's pointer and keys as Slack's omniswitcher has them
// (SearchAutocomplete, client-boot-shared XPKo, build 031dbe6a7400):
//
//   - a row's onMouseMove picks it (selectedIndex, setText false,
//     navigationMethod "mouse"), so the pointer has to move: a row that
//     comes under a still pointer (the switcher opening over it, results
//     arriving, the list scrolling to a keyboard pick) gets no mousemove
//     and isn't picked;
//   - leaving the list (onMouseLeaveList) picks nothing;
//   - ↓ / ↑ move the pick a row (increment/decrementSelectedIndex);
//   - Enter takes the picked row, else the top hit (pseudo-selected);
//   - taking a person opens their DM (searchAutocompleteSuggestionAccept,
//     client-boot-render Gdrn: getDMIdForMember, else openDM), and their
//     profile only when Slack won't start the DM (cannot_initiate_dm).
//
// Karim (2026-11-18): "i did control k and searched clack up and hit
// enter but i think i was hovering over someone and i went to their
// profile": Clack picked the row that came under the resting pointer.

// srRowBox is row i's box in the window as the last frame laid it out.
func srRowBox(p *SearchPopover, i int) image.Rectangle {
	gtx := layout.Context{Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}}
	top := p.listOrg.Y + p.rowTop(gtx, i)
	return image.Rect(p.rect.Min.X, top, p.rect.Max.X, top+dp(gtx, p.rows[i].heightDp()))
}

// srRowUnder is the row whose box holds pt, or -1.
func srRowUnder(p *SearchPopover, pt image.Point) int {
	if !pt.In(p.rect) || pt.Y < p.listOrg.Y {
		return -1
	}
	for i := range p.rows {
		if pt.In(srRowBox(p, i)) {
			return i
		}
	}
	return -1
}

// srRowCenter is the middle of row i's box.
func srRowCenter(p *SearchPopover, i int) image.Point {
	r := srRowBox(p, i)
	return image.Pt(r.Min.X+r.Dx()/3, (r.Min.Y+r.Max.Y)/2)
}

// srClear empties the switcher's box.
func srClear(d *srDrv, p *SearchPopover) {
	d.r.Queue(key.EditEvent{Range: key.Range{Start: 0, End: p.ed.Len()}, Text: ""}, key.SelectionEvent{})
	d.frame()
	d.frame()
}

// srSettle draws frames for a while (the directory searches answer).
func srSettle(d *srDrv, dur time.Duration) {
	for end := time.Now().Add(dur); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		d.frame()
	}
}

// srPersonRow is person id's row, or -1.
func srPersonRow(p *SearchPopover, id string) int {
	for i := range p.rows {
		if p.rows[i].kind == srRUser && p.rows[i].id == id {
			return i
		}
	}
	return -1
}

// srNextRow is the row ↓ picks after row i, or -1.
func srNextRow(p *SearchPopover, i int) int {
	for j := i + 1; j < len(p.rows); j++ {
		if p.rows[j].selectable() {
			return j
		}
	}
	return -1
}

// srRouteOf is where Enter on r goes, for the rows that navigate at once.
func srRouteOf(r srRow) (string, bool) {
	switch {
	case r.kind == srRConv || r.kind == srRChan:
		return "c/" + r.id, true
	case r.kind == srRUser && r.conv != "":
		return "c/" + r.conv, true
	case r.kind == srRPlace:
		return r.id, true
	}
	return "", false
}

func srNavs(e *srEnv) []string {
	e.h.mu.Lock()
	defer e.h.mu.Unlock()
	return slices.Clone(e.h.navs)
}

func srToasts(e *srEnv) []string {
	e.h.mu.Lock()
	defer e.h.mu.Unlock()
	return slices.Clone(e.h.toasts)
}

// srRecordProfiles records the profiles opened (pfOpenFull).
func srRecordProfiles(t *testing.T) func() []string {
	t.Helper()
	old := OpenFullProfile
	var mu sync.Mutex
	var opened []string
	OpenFullProfile = func(id string) {
		mu.Lock()
		opened = append(opened, id)
		mu.Unlock()
	}
	t.Cleanup(func() { OpenFullProfile = old })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(opened)
	}
}

// srPeople are people the switcher lists by their full name, by id: the
// ones with a DM in the sidebar and the ones without. Boot knows only the
// sidebar's people, so the directory's are fetched first (users.info), as
// a workspace's would be once seen.
func srPeople(t *testing.T, e *srEnv) (withDM, noDM []core.User, dmOf map[string]string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	e.h.a.EnsureUsers(ctx, e.fs.UserIDs())
	probe := NewQuickSwitcher(e.h)
	d := &srDrv{size: image.Pt(1280, 800), w: probe}
	d.until(t, "the snapshot", func() bool { return probe.snapOK })
	dmOf = probe.dmOf
	ids := e.fs.UserIDs()
	slices.Sort(ids)
	for _, id := range ids {
		u, ok := e.h.a.User(id)
		if !ok || u.ID == e.h.a.Self().ID || u.IsBot || u.Deleted || u.DisplayName == "" || u.Handle == "" || !strings.Contains(u.RealName, " ") {
			continue
		}
		if dmOf[u.ID] != "" {
			withDM = append(withDM, u)
		} else {
			noDM = append(noDM, u)
		}
	}
	probe.Dismiss()
	if len(withDM) == 0 || len(noDM) < 2 {
		t.Fatalf("fixture: %d people with a DM, %d without", len(withDM), len(noDM))
	}
	return withDM, noDM, dmOf
}

// srWindow is the window under the switcher: views that hear the pointer
// (as the app's do), so a pointer resting there is one Gio's router
// tracks, and the switcher once open.
type srWindow struct {
	tag  int
	p    *SearchPopover
	open bool
}

func (w *srWindow) Layout(gtx layout.Context) layout.Dimensions {
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: &w.tag, Kinds: pointer.Move | pointer.Enter | pointer.Leave}); !ok {
			break
		}
	}
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, &w.tag)
	area.Pop()
	if w.open {
		return w.p.Layout(gtx)
	}
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// Results arriving under a still pointer (Karim, 2026-11-18): the pointer
// comes to rest below the switcher's short list, then the query goes and
// the blank slate's rows grow under it. No mousemove came, so nothing is
// picked, and Enter opens the top hit, not the row under the pointer.
func TestSearchSwitcherStillPointer(t *testing.T) {
	e := srNewEnv(t, false)
	p := NewSearchPopover(e.h, "qqzzxxjj", image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 800), w: p}
	d.until(t, "the switcher", func() bool { return p.snapOK && len(p.rows) > 0 && !p.rect.Empty() })
	srSettle(d, 400*time.Millisecond)
	rest := image.Pt(p.rect.Min.X+160, p.rect.Max.Y+40)
	srMoveTo(d, rest.X, rest.Y) // a real move, over nothing of the switcher's
	if p.picked {
		t.Fatalf("a move below the list picked row %d", p.sel)
	}
	srClear(d, p)
	d.until(t, "a row under the still pointer", func() bool {
		i := srRowUnder(p, rest)
		return i >= 0 && p.rows[i].selectable()
	})
	srSettle(d, 300*time.Millisecond)
	under := srRowUnder(p, rest)
	if under < 0 || under == p.firstRow() {
		t.Fatalf("fixture: row %d is under the pointer, the top hit is %d", under, p.firstRow())
	}
	if p.picked {
		t.Fatalf("row %d (%q) came under the still pointer and is picked: Slack picks a row only on a mousemove", p.sel, p.rows[p.sel].title)
	}
	top := p.rows[p.firstRow()]
	want, ok := srRouteOf(top)
	if !ok {
		t.Fatalf("fixture: the top hit is a %v row %+v", top.kind, top)
	}
	d.press(key.NameReturn)
	if navs := srNavs(e); !p.Dismissed() || len(navs) == 0 || navs[len(navs)-1] != want {
		t.Errorf("Enter: dismissed %v, navs %q; want the top hit %q, not row %d (%q) under the pointer", p.Dismissed(), navs, want, under, p.rows[under].title)
	}
}

// Keys only, with the pointer resting over the list: ↓ moves the pick a
// row at a time, also once the list scrolls under the pointer (no
// mousemove), and Enter opens the row the keys picked.
func TestSearchSwitcherKeyboardOnly(t *testing.T) {
	e := srNewEnv(t, false)
	p := NewSearchPopover(e.h, "a", image.Rectangle{})
	d := &srDrv{size: image.Pt(1280, 300), w: p} // a short window: the list scrolls
	d.until(t, "the switcher's rows", func() bool { return p.snapOK && p.dirFired && len(p.rows) > 0 })
	srSettle(d, 500*time.Millisecond)
	start := srNextRow(p, srNextRow(p, -1)) // the second row
	if start < 0 {
		t.Fatalf("fixture: rows %+v", p.rows)
	}
	pt := srRowCenter(p, start)
	srMoveTo(d, pt.X, pt.Y) // the pointer moves onto it (a pick), then rests
	if !p.picked || p.sel != start {
		t.Fatalf("the move onto row %d picked %v row %d", start, p.picked, p.sel)
	}
	want, scrolled := start, false
	for n := 0; n < 14; n++ {
		next := srNextRow(p, want)
		if next < 0 {
			break
		}
		pos := p.list.Position
		d.press(key.NameDownArrow)
		scrolled = scrolled || p.list.Position != pos
		if !p.picked || p.sel != next {
			t.Fatalf("↓ %d: picked %v row %d (%q), want row %d (%q); row %d is under the still pointer",
				n+1, p.picked, p.sel, p.rows[max(p.sel, 0)].title, next, p.rows[next].title, srRowUnder(p, pt))
		}
		want = next
		if r, ok := srRouteOf(p.rows[want]); ok && scrolled && n >= 2 {
			n0 := len(srNavs(e))
			d.press(key.NameReturn)
			if navs := srNavs(e); !p.Dismissed() || len(navs) != n0+1 || navs[n0] != r {
				t.Errorf("Enter after the keys: dismissed %v, navs %q, want %q", p.Dismissed(), navs[n0:], r)
			}
			return
		}
	}
	t.Fatalf("fixture: the list scrolled %v, %d rows; no row to open after three ↓ with it scrolled", scrolled, len(p.rows))
}

// A hover, then Enter: Ctrl+K opens over a pointer resting where a
// person's row comes, and nothing is picked (no mousemove); the pointer
// moves on the row, which picks it, and Enter opens the person's DM, not
// their profile.
func TestSearchSwitcherHoverThenEnter(t *testing.T) {
	e := srNewEnv(t, false)
	profiles := srRecordProfiles(t)
	withDM, _, dmOf := srPeople(t, e)
	who := withDM[0]
	// where the person's row is, in a switcher like the one that opens
	probe := NewSearchPopover(e.h, who.RealName, image.Rectangle{})
	pd := &srDrv{size: image.Pt(1280, 800), w: probe}
	pd.until(t, "the person's row", func() bool { return probe.snapOK && probe.dirFired && srPersonRow(probe, who.ID) >= 0 })
	srSettle(pd, 500*time.Millisecond)
	pt := srRowCenter(probe, srPersonRow(probe, who.ID))
	probe.Dismiss()

	p := NewSearchPopover(e.h, who.RealName, image.Rectangle{})
	w := &srWindow{p: p}
	d := &srDrv{size: image.Pt(1280, 800), w: w}
	d.frame()
	srMoveTo(d, pt.X, pt.Y) // the pointer rests over the window
	w.open = true           // Ctrl+K
	d.until(t, "the person's row under the pointer", func() bool {
		return p.snapOK && p.dirFired && srPersonRow(p, who.ID) >= 0
	})
	srSettle(d, 500*time.Millisecond)
	i := srPersonRow(p, who.ID)
	if srRowUnder(p, pt) != i {
		t.Fatalf("fixture: row %d is under the pointer, the person's is %d", srRowUnder(p, pt), i)
	}
	if p.picked {
		t.Fatalf("the switcher opened over the still pointer and picked row %d (%q): Slack picks a row only on a mousemove", p.sel, p.rows[p.sel].title)
	}
	srMoveTo(d, pt.X+6, pt.Y) // the pointer moves on the row
	if !p.picked || p.sel != i {
		t.Fatalf("a move on %s's row: picked %v row %d, want %d", who.RealName, p.picked, p.sel, i)
	}
	d.press(key.NameReturn)
	navs := srNavs(e)
	if want := "c/" + dmOf[who.ID]; !p.Dismissed() || len(navs) == 0 || navs[len(navs)-1] != want {
		t.Errorf("Enter on %s: dismissed %v, navs %q, want their DM %q", who.RealName, p.Dismissed(), navs, want)
	}
	if got := profiles(); len(got) != 0 {
		t.Errorf("Enter on a person opened profiles %q", got)
	}
}

// Enter on a person, keys only: their DM when one exists; else
// conversations.open and then the new DM; never their profile, except
// when Slack won't start the DM (cannot_initiate_dm), which shows it.
func TestSearchSwitcherEnterOnPerson(t *testing.T) {
	e := srNewEnv(t, false)
	profiles := srRecordProfiles(t)
	withDM, noDM, dmOf := srPeople(t, e)
	refused := noDM[1]
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "conversations.open" && f.Get("users") == refused.ID {
			return map[string]any{"ok": false, "error": "cannot_initiate_dm"}, true
		}
		return nil, false
	})
	enter := func(who core.User) *SearchPopover {
		t.Helper()
		// the switcher finds a person with no DM only through the
		// directory search, which fakeslack answers by handle
		q := who.RealName
		if dmOf[who.ID] == "" {
			q = who.Handle
		}
		p := NewSearchPopover(e.h, q, image.Rectangle{})
		d := &srDrv{size: image.Pt(1280, 800), w: p}
		d.until(t, who.RealName+"'s row", func() bool { return p.snapOK && p.dirFired && srPersonRow(p, who.ID) >= 0 })
		srSettle(d, 400*time.Millisecond)
		for n := 0; n <= len(p.rows) && !(p.picked && p.sel == srPersonRow(p, who.ID)); n++ {
			d.press(key.NameDownArrow)
		}
		if i := srPersonRow(p, who.ID); !p.picked || p.sel != i {
			t.Fatalf("↓ never picked %s's row %d (picked %v row %d)", who.RealName, i, p.picked, p.sel)
		}
		n0 := len(srNavs(e))
		d.press(key.NameReturn)
		srSettle(d, 300*time.Millisecond)
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end) && len(srNavs(e)) == n0 && len(profiles()) == 0 && len(srToasts(e)) == 0; {
			srSettle(d, 50*time.Millisecond) // conversations.open
		}
		if !p.Dismissed() {
			t.Errorf("Enter on %s left the switcher open", who.RealName)
		}
		return p
	}

	// a DM in the sidebar
	enter(withDM[0])
	if navs := srNavs(e); len(navs) == 0 || navs[len(navs)-1] != "c/"+dmOf[withDM[0].ID] {
		t.Errorf("Enter on %s (a DM): navs %q, want c/%s", withDM[0].RealName, navs, dmOf[withDM[0].ID])
	}
	// no DM yet: conversations.open, then the DM
	who := noDM[0]
	enter(who)
	navs := srNavs(e)
	if !slices.Contains(e.fs.OpenedDMs(), who.ID) || len(navs) == 0 || !strings.HasPrefix(navs[len(navs)-1], "c/D") {
		t.Errorf("Enter on %s (no DM): conversations.open for %q, navs %q, want the new DM", who.RealName, e.fs.OpenedDMs(), navs)
	}
	if got := profiles(); len(got) != 0 {
		t.Errorf("Enter on a person opened profiles %q", got)
	}
	// Slack won't start the DM: the profile
	n0 := len(srNavs(e))
	enter(refused)
	if got := profiles(); !slices.Equal(got, []string{refused.ID}) {
		t.Errorf("Enter on %s (cannot_initiate_dm): profiles %q, want theirs; toasts %q", refused.RealName, got, srToasts(e))
	}
	if navs := srNavs(e); len(navs) != n0 {
		t.Errorf("cannot_initiate_dm navigated to %q", navs[n0:])
	}
	if ts := srToasts(e); len(ts) != 0 {
		t.Errorf("toasts %q", ts)
	}
}
