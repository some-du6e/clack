package composer

import (
	"context"
	"fmt"
	"image"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// drawRec is what the composer drew while it records: text and icons.
type drawRec struct {
	mu           sync.Mutex
	texts, icons []string
}

func recordDrawn(t *testing.T) *drawRec {
	t.Helper()
	r := &drawRec{}
	pt := theme.DrawHook
	theme.DrawHook = func(s string) { r.mu.Lock(); r.texts = append(r.texts, s); r.mu.Unlock() }
	restore := hookIcons(func(n string) { r.mu.Lock(); r.icons = append(r.icons, n); r.mu.Unlock() })
	t.Cleanup(func() { theme.DrawHook = pt; restore() })
	return r
}

func (r *drawRec) reset() { r.mu.Lock(); r.texts, r.icons = nil, nil; r.mu.Unlock() }

// text reports a drawn string containing sub.
func (r *drawRec) text(sub string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.texts {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func (r *drawRec) icon(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.icons {
		if s == name {
			return true
		}
	}
	return false
}

// TestDraftManagementButton (Karim, α.112 on Windows, 2026-11-19, a
// capture of the composer's send button and hint: "this is maybe missing
// smth"): under draft_management_in_composer (treatment, the account's
// group) Slack draws a drafts button left of the send button while the
// conversation has a draft, the one being typed included
// (DraftManagementButton, boot-shared /vRM). Against internal/fakeslack,
// synthetic drafts, through a real input.Router: the button's place,
// size and count, its tooltip, its menu opened by a click (the newest 4,
// the composer's own checked, "View 2 more drafts", "Save as draft"),
// "Save as draft" keeping the composer's text as a stashed composer draft
// and emptying the composer, and picking a draft into the composer.
func TestDraftManagementButton(t *testing.T) {
	app, fs := fakeslackApp(t)
	old := draftSyncDelay
	draftSyncDelay = 30 * time.Millisecond
	t.Cleanup(func() { draftSyncDelay = old })
	ch := tChGen
	forget := func() { forgetDraft(ch); serverDraftIDs.Delete(draftKey(ch, "")) }
	forget()
	t.Cleanup(forget)
	app.SetExperimentGroup("draft_management_in_composer", "treatment")
	ctx := context.Background()
	for i := 1; i <= 5; i++ { // saved from this channel's composer before, oldest first
		if _, err := dmStash(ctx, app, ch, fmt.Sprintf("synthetic stashed draft %d", i)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(3 * time.Millisecond) // last_updated_ts is in ms
	}
	// a reply's draft isn't the channel's
	if _, err := app.SaveDraft(ctx, core.DraftOpts{Channel: ch, ThreadTS: "1700000000.000100", Text: "a synthetic reply"}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Drafts(ctx); err != nil {
		t.Fatal(err)
	}
	g := newFmtRigOn(t, 900, true, &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}})
	c := g.c
	drawn := recordDrawn(t)
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s (drafts %+v)", what, fs.LiveDrafts())
			}
			g.frame()
			time.Sleep(5 * time.Millisecond)
		}
	}
	g.settle()

	// an empty composer: the five drafts, none restored into it
	if txt := c.ed.Doc().Text(); txt != "" {
		t.Errorf("the composer restored %q: a draft saved from the composer isn't the channel's attached draft", txt)
		c.ed.SetDoc(NewDoc())
		g.settle()
	}
	r, send, n := dmButton(c)
	if n != 5 || r.Empty() {
		t.Fatalf("an empty composer in a channel with 5 drafts: the drafts button %v counts %d; want it showing 5", r, n)
	}
	// 4px left of the send button, its height, 8px padding around the
	// 18px glyph, 4px and the count
	if r.Max.X+4 != send.Min.X || r.Dy() != 28 || r.Max.Y != send.Max.Y {
		t.Errorf("the drafts button at %v, the send button at %v: want 4px left of it, 28 high, bottom-aligned", r, send)
	}
	if r.Dx() < 8+18+4+5+8 || r.Dx() > 8+18+4+9+8 {
		t.Errorf("the drafts button is %dpx wide: want 8 + 18 + 4 + the count + 8", r.Dx())
	}

	// typing: the composer's own draft is the sixth
	for _, s := range []string{"h", "e", "y"} {
		g.typeRune(s)
	}
	drawn.reset()
	g.settle()
	if _, _, n = dmButton(c); n != 6 {
		t.Errorf("typing in the composer: the drafts button counts %d, want 6 (the one being typed too)", n)
	}
	if !drawn.icon("edit") || !drawn.text("6") {
		t.Errorf("the drafts button drew icons %v, texts %v: want the edit glyph and the count", drawn.icons, drawn.texts)
	}
	if tip := dmTip(c); tip != "6 drafts in this channel" {
		t.Errorf("the drafts button's tooltip %q, want \"6 drafts in this channel\"", tip)
	}

	// a click opens the menu
	r, _, _ = dmButton(c)
	p := r.Min.Add(r.Size().Div(2)).Add(image.Pt(0, c.top))
	g.mouseMove(p)
	g.mouseDown(p)
	g.mouseUp(p)
	m, ok := g.h.last().(*popups.Menu)
	if !ok {
		t.Fatalf("a click on the drafts button opened %T, want its menu", g.h.last())
	}
	want := []string{"[Drafts in this channel]", "hey", "synthetic stashed draft 5", "synthetic stashed draft 4",
		"synthetic stashed draft 3", "---", "View 2 more drafts", "Save as draft"}
	if got := m.Labels(); !reflect.DeepEqual(got, want) {
		t.Errorf("the drafts menu %q, want %q", got, want)
	}
	items := dmMenu(c)
	byLabel := func(label string) (popups.MenuItem, bool) {
		for _, it := range items {
			if it.Label == label {
				return it, true
			}
		}
		return popups.MenuItem{}, false
	}
	for _, it := range items {
		own := it.Label == "hey"
		if it.Label != "" && (it.Radio != own || it.Checked != own) {
			t.Errorf("menu row %q: radio %v checked %v; want only the composer's own draft checked", it.Label, it.Radio, it.Checked)
		}
	}
	save, ok := byLabel("Save as draft")
	if !ok || save.Disabled || save.Do == nil {
		t.Fatalf("Save as draft: %+v", save)
	}

	// Save as draft: the text is a stashed composer draft, the composer empty
	until("the typed draft saved", func() bool { return c.serverDraftID() != "" })
	own := c.serverDraftID()
	save.Do()
	g.settle()
	// it empties once Slack has the draft (nothing typed is dropped if
	// Slack doesn't answer: TestSaveAsDraftKeepsTextWhenTheSaveFails)
	until("the composer empty after Save as draft", func() bool { return c.ed.Doc().Text() == "" })
	stashed := func() (fakeslack.Draft, bool) {
		for _, d := range fs.LiveDrafts() {
			if strings.Contains(string(d.Blocks), `"hey"`) {
				return d, true
			}
		}
		return fakeslack.Draft{}, false
	}
	until("the typed draft stashed", func() bool { d, ok := stashed(); return ok && dmStashed(d) })
	if d, _ := stashed(); d.ID != own {
		t.Errorf("Save as draft made draft %s; want the composer's own %s kept (Slack re-keys it)", d.ID, own)
	}
	if id := c.serverDraftID(); id != "" {
		t.Errorf("after Save as draft the composer still holds draft %s", id)
	}
	for range 10 { // the emptied composer deletes nothing
		g.frame()
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := stashed(); !ok {
		t.Fatal("the stashed draft was deleted when the composer emptied")
	}
	until("the button's count", func() bool { _, _, n := dmButton(c); return n == 6 })

	// picking a draft: it takes the composer, focused, the caret at its end
	items = dmMenu(c)
	pick, ok := byLabel("synthetic stashed draft 4")
	if !ok || pick.Do == nil {
		t.Fatalf("no row for draft 4 in %q", m.Labels())
	}
	if it, _ := byLabel("Save as draft"); !it.Disabled {
		t.Error("Save as draft is enabled with nothing in the composer")
	}
	pick.Do()
	g.settle()
	if txt := c.ed.Doc().Text(); txt != "synthetic stashed draft 4" {
		t.Fatalf("after picking draft 4 the composer holds %q", txt)
	}
	if a, b := c.ed.Selection(); a != b || b != c.ed.Doc().Len() {
		t.Errorf("after picking a draft the selection is [%d,%d), want the caret at its end", a, b)
	}
	until("the picked draft checked in the menu", func() bool {
		items = dmMenu(c)
		it, _ := byLabel("synthetic stashed draft 4")
		return it.Checked
	})
}

// TestHintBarLikeSlack: NotificationBar's formatting tip shows once the
// message is past two characters as Slack counts them
// (hasMoreThanTwoCharacters), fading in and out over .2s ease-in-out; a
// click on it opens Preferences on Advanced. Not recently online, the
// bar says "Slack is trying to connect." (", so messages can’t be sent
// yet." past two characters) with the cloud-offline-filled icon, and no
// tip. Synthetic data, a real input.Router, the rig's 16ms frames.
func TestHintBarLikeSlack(t *testing.T) {
	online := true
	setOnline(t, func() (bool, time.Time) { return online, time.Time{} })
	g := newFmtRig(t, 900, true)
	c := g.c
	drawn := recordDrawn(t)
	const tip = "to add a new line"

	g.typeRune("h")
	g.typeRune("i")
	drawn.reset()
	g.settle()
	if drawn.text(tip) {
		t.Errorf("the hint shows with 2 characters typed; Slack's waits for a third (texts %q)", drawn.texts)
	}
	g.r.Queue(key.EditEvent{Range: key.Range{Start: 2, End: 2}, Text: "!"}, key.SelectionEvent{Start: 3, End: 3})
	t0 := g.now
	for range 5 { // the edit's frame, then 80ms into the fade
		g.frame()
	}
	if a := hintOpacity(c, g.now); a <= 0.05 || a >= 0.8 {
		t.Errorf("80ms into the fade-in the hint's opacity is %.2f, want on its way (ease-in-out over 200ms)", a)
	}
	for g.now.Sub(t0) < 260*time.Millisecond {
		g.frame()
	}
	drawn.reset()
	g.frame()
	if a := hintOpacity(c, g.now); a != 1 || !drawn.text(tip) {
		t.Fatalf("the hint after its fade-in: opacity %.2f, texts %q", a, drawn.texts)
	}

	// a click on it opens Preferences > Advanced (input options)
	var opened []string
	prev := popups.OpenPreferencesSection
	popups.OpenPreferencesSection = func(sec string) { opened = append(opened, sec) }
	t.Cleanup(func() { popups.OpenPreferencesSection = prev })
	if r := hintRect(c); r.Empty() {
		t.Error("the visible hint takes no pointer (Slack's is a ButtonLink)")
	} else {
		p := r.Min.Add(r.Size().Div(2)).Add(image.Pt(0, c.top))
		g.mouseMove(p)
		g.mouseDown(p)
		g.mouseUp(p)
		if !reflect.DeepEqual(opened, []string{"advanced"}) {
			t.Errorf("a click on the hint opened Preferences on %q, want [advanced]", opened)
		}
	}

	// back to two characters: it fades out
	g.r.Queue(key.EditEvent{Range: key.Range{Start: 2, End: 3}, Text: ""}, key.SelectionEvent{Start: 2, End: 2})
	t1 := g.now
	for g.now.Sub(t1) < 260*time.Millisecond {
		g.frame()
	}
	drawn.reset()
	g.frame()
	if a := hintOpacity(c, g.now); a != 0 || drawn.text(tip) {
		t.Errorf("200ms after going back to 2 characters: opacity %.2f, texts %q", a, drawn.texts)
	}

	// offline: the notice, no tip
	online = false
	drawn.reset()
	g.settle()
	if !drawn.text("Slack is trying to connect.") || !drawn.icon("cloud-offline-filled") || drawn.text(tip) {
		t.Errorf("offline with 2 characters: texts %q icons %q; want \"Slack is trying to connect.\" and cloud-offline-filled", drawn.texts, drawn.icons)
	}
	g.typeRune("!")
	drawn.reset()
	g.settle()
	if !drawn.text("Slack is trying to connect, so messages can’t be sent yet.") || drawn.text(tip) {
		t.Errorf("offline with 3 characters: texts %q", drawn.texts)
	}
}
