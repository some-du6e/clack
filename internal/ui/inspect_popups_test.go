package ui

// The inspect tool inside popups (Karim, 2026-11-18, α.108 on Windows:
// "you cant select the popup smh, the dev tool thing is half-assed"). His
// capture had an app's Block Kit modal open: the report said "overlays
// blockkit.Stack" and "region primary", and nothing inside the modal could
// be outlined or picked. In a session the picker outlines the element under
// the pointer in the topmost layer (a Block Kit modal's parts, a menu's
// rows, the emoji picker's emoji, a profile card, a tooltip), a Shift+click
// reports that element (its component, element, the elements around it and
// the popup as its region), and the popup never sees the click. The freeze
// holds a tooltip that would go when the pointer leaves its trigger.

import (
	"encoding/json"
	"fmt"
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/inspect"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// hoverAt moves the pointer to p and returns what the session outlines.
func (e *perfEnv) hoverAt(p image.Point) (string, image.Rectangle, bool) {
	e.move(f32.Pt(float32(p.X), float32(p.Y)))
	e.frame()
	return e.a.insp.Hovered()
}

// pickReport Shift+clicks p (the report copied at once) and returns the
// saved report's rows.
func (e *inspectEnv) pickReport(t *testing.T, p image.Point) map[string]string {
	t.Helper()
	e.pickAndCopy(t, p)
	txt, _ := e.readReport(t)
	return reportRows(txt)
}

// TestInspectPicksBlockKitModal: over the fake app's settings modal (Note
// settings: a context line, a divider, a context line, a text input; Edit
// and Cancel), the picker outlines the modal's parts as Slack's
// BlockKitModal draws them (a WizardModal over DeprecatedBaseDialog): the
// title, a block of the body, the footer's next button. A Shift+click on
// that button reports it: component BlockKitModal, the button's element,
// the footer around it, the modal as its region. The modal stays open with
// nothing submitted.
func TestInspectPicksBlockKitModal(t *testing.T) {
	fakeShots(t)
	t.Setenv("CLACK_INSPECT", "1")
	fs, err := fakeslack.Start(fakeslack.Gallery)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	var mu sync.Mutex
	var submits int
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		if method == "views.submit" {
			mu.Lock()
			submits++
			mu.Unlock()
		}
		return nil, false
	})
	pe := startEnv(t, fs, t.TempDir())
	fs.Emit = func(ev map[string]any) {
		b, _ := json.Marshal(ev)
		typ, _ := ev["type"].(string)
		pe.a.data.core.Inject(slack.Event{Type: typ, Raw: b})
	}
	e := &inspectEnv{perfEnv: pe}
	e.waitReady()
	e.openChannel(fakeslack.BlockKitGalleryID)
	e.until("the gallery", 20*time.Second, func() bool { return e.loadedMessages(fakeslack.BlockKitGalleryID) >= 7 })
	e.settle(500 * time.Millisecond)
	blockkit.RunShortcut(e.a, core.AppShortcut{ActionID: "SURFA3", AppID: fakeslack.AppAppID, AppName: "Deploy Bot", Type: "global_action"},
		fakeslack.BlockKitGalleryID, "")
	e.until("the settings modal", 15*time.Second, func() bool {
		m, ok := blockkit.OpenModal(e.a)
		fields := blockkit.ModalFields(e.a)
		return ok && m.ViewID == fakeslack.SurfNoteView && len(fields) > 0 && !fields[0].Body.Empty()
	})
	e.settle(300 * time.Millisecond)
	body := blockkit.ModalFields(e.a)[0].Body // the card's width, under its 68dp header

	e.hyperD()
	e.frame()
	if !e.a.insp.Active() {
		t.Fatal("the shortcut started no session over the modal")
	}
	// the footer's next button: 80dp wide ("Edit"), 24dp in from the card's
	// right, centred in the 76dp footer under the body
	next := image.Pt(body.Max.X-24-40, body.Max.Y+38)
	// (a label shows three classes: the data-qa names the button)
	if label, r, ok := e.hoverAt(next); !ok || !strings.HasPrefix(label, "button.c-button.c-button--primary") || !strings.Contains(label, "wizard_modal_next") || !next.In(r) {
		t.Errorf("over the modal's Edit button: outline %q %v (%v), want Slack's button.c-button.c-button--primary…[data-qa=wizard_modal_next] around %v", label, r, ok, next)
	}
	title := image.Pt(body.Min.X+100, body.Min.Y-34)
	if label, r, ok := e.hoverAt(title); !ok || !strings.Contains(label, "p-block_kit_modal__title") || !title.In(r) {
		t.Errorf("over the modal's title: outline %q %v (%v), want its p-block_kit_modal__title", label, r, ok)
	}
	block := image.Pt(body.Min.X+44, body.Min.Y+18)
	if label, r, ok := e.hoverAt(block); !ok || !strings.Contains(label, "p-block_kit_renderer__block_wrapper") || !block.In(r) {
		t.Errorf("over the modal's first block: outline %q %v (%v), want a p-block_kit_renderer__block_wrapper", label, r, ok)
	}

	rows := e.pickReport(t, next)
	if rows["component"] != "BlockKitModal" {
		t.Errorf("report component %q, want BlockKitModal", rows["component"])
	}
	if el := rows["element"]; !strings.Contains(el, "c-wizard_modal__next") {
		t.Errorf("report element %q, want the footer's next button", el)
	}
	if !strings.HasPrefix(rows["region"], "modal") {
		t.Errorf("report region %q, want the modal", rows["region"])
	}
	if !strings.Contains(rows["classes"], "c-dialog__footer") {
		t.Errorf("report classes %q, want the dialog's footer around the button", rows["classes"])
	}
	if m, ok := blockkit.OpenModal(e.a); !ok || m.ViewID != fakeslack.SurfNoteView {
		t.Errorf("after the pick: the modal open %v (%q), want it still open", ok, m.ViewID)
	}
	mu.Lock()
	n := submits
	mu.Unlock()
	if n != 0 {
		t.Errorf("the pick submitted the modal %d times", n)
	}
}

// pillMessage is the message the inspect env posted (its sender the fake's
// second user).
func (e *inspectEnv) pillMessage() core.Message {
	return core.Message{Channel: e.ch, TS: e.ts, User: e.fs.UserIDs()[1], Text: "the launch moved to friday, keep it quiet"}
}

// TestInspectPicksMenu: a message's menu opened in a session, the picker
// outlines its rows (li.c-menu_item__li in Slack's Menu), not the window
// under the menu, and a Shift+click on Copy link reports that row: the
// Menu component, the row's element, the menu card around it, the menu as
// its region. The menu stays open and runs nothing.
func TestInspectPicksMenu(t *testing.T) {
	fakeShots(t)
	e := newInspectEnv(t)
	e.startSession(t)
	pc := e.pillCenter()
	e.a.OpenMessageMenu(image.Pt(int(pc.X), int(pc.Y)), e.pillMessage())
	e.frame()
	e.frame()
	m, ok := topOverlay[*popups.Menu](e.a)
	if !ok {
		t.Fatalf("no menu after OpenMessageMenu (overlays %v)", e.overlayTypes())
	}
	rr := m.RowRect("Copy link")
	if rr.Empty() {
		t.Fatalf("the message menu has no Copy link row (%q)", m.Labels())
	}
	row := image.Pt(rr.Min.X+rr.Dx()/2, rr.Min.Y+rr.Dy()/2)
	label, r, ok := e.hoverAt(row)
	if !ok || !strings.HasPrefix(label, "li.c-menu_item__li") || !nearRect(r, rr, 2) {
		t.Errorf("over the menu's Copy link: outline %q %v (%v), want its li.c-menu_item__li around %v", label, r, ok, rr)
	}
	card := image.Pt(m.Rect().Min.X+m.Rect().Dx()/2, m.Rect().Min.Y+3) // the card's top padding
	if label, r, ok := e.hoverAt(card); !ok || !strings.HasPrefix(label, "div.c-menu") || !nearRect(r, m.Rect(), 2) {
		t.Errorf("over the menu card's padding: outline %q %v (%v), want the c-menu card %v", label, r, ok, m.Rect())
	}
	rows := e.pickReport(t, row)
	if rows["component"] != "Menu" {
		t.Errorf("report component %q, want Menu", rows["component"])
	}
	if !strings.HasPrefix(rows["element"], "li.c-menu_item__li") {
		t.Errorf("report element %q, want the row's li.c-menu_item__li", rows["element"])
	}
	if !strings.HasPrefix(rows["region"], "menu") {
		t.Errorf("report region %q, want the menu", rows["region"])
	}
	if !strings.Contains(rows["classes"], "c-menu") {
		t.Errorf("report classes %q, want the menu card around the row", rows["classes"])
	}
	if m.Dismissed() || strings.Contains(e.a.toast.text, "Link copied") {
		t.Errorf("the pick reached the menu: dismissed %v, toast %q", m.Dismissed(), e.a.toast.text)
	}
}

// TestInspectPicksPopover: the emoji picker (Slack's EmojiPickerPopover)
// opened in a session: the picker outlines an emoji (EmojiListEmojiButton's
// button.c-link--button.p-emoji_picker__list_item), and a Shift+click
// reports it with the picker as its region, picking no emoji. A profile
// card's pick is reported in the card's region, not the window's.
func TestInspectPicksPopover(t *testing.T) {
	fakeShots(t)
	e := newInspectEnv(t)
	e.startSession(t)
	pc := e.pillCenter()
	picked := ""
	e.a.OpenEmojiPicker(image.Pt(int(pc.X), int(pc.Y)), func(name string) { picked = name })
	e.settle(500 * time.Millisecond)
	ep, ok := topOverlay[*popups.EmojiPicker](e.a)
	if !ok {
		t.Fatalf("no emoji picker after OpenEmojiPicker (overlays %v)", e.overlayTypes())
	}
	pr := ep.Rect()
	if pr.Empty() {
		t.Fatal("the emoji picker drew no card")
	}
	// an emoji in the list (the card's middle band, past the search field)
	var at image.Point
	found := ""
	for y := pr.Min.Y + pr.Dy()*2/5; y < pr.Max.Y-pr.Dy()/6 && found == ""; y += 9 {
		for x := pr.Min.X + 20; x < pr.Max.X-20; x += 11 {
			if label, _, ok := e.hoverAt(image.Pt(x, y)); ok && strings.Contains(label, "p-emoji_picker__list_item") {
				at, found = image.Pt(x, y), label
				break
			}
		}
	}
	if found == "" {
		label, r, _ := e.hoverAt(image.Pt(pr.Min.X+pr.Dx()/2, pr.Min.Y+pr.Dy()/2))
		t.Fatalf("no emoji outlined in the picker %v (its middle outlines %q %v)", pr, label, r)
	}
	rows := e.pickReport(t, at)
	if !strings.Contains(rows["element"], "p-emoji_picker__list_item") {
		t.Errorf("report element %q, want the emoji's button", rows["element"])
	}
	if !strings.HasPrefix(rows["component"], "EmojiList") {
		t.Errorf("report component %q, want Slack's EmojiListEmojiButton", rows["component"])
	}
	if !strings.HasPrefix(rows["region"], "emoji picker") {
		t.Errorf("report region %q, want the emoji picker", rows["region"])
	}
	if picked != "" {
		t.Errorf("the pick reached the picker: it picked %q", picked)
	}
	ep.Dismiss()
	e.frame()

	// a profile card (the card a click on a name opens)
	e.startSession(t)
	e.a.ShowOverlay(popups.NewProfileCard(e.a, image.Pt(int(pc.X), int(pc.Y)), e.fs.UserIDs()[1]))
	e.settle(500 * time.Millisecond)
	card, ok := topOverlay[*popups.ProfileCard](e.a)
	if !ok || card.Rect().Empty() {
		t.Fatalf("no profile card on screen (overlays %v)", e.overlayTypes())
	}
	cr := card.Rect()
	mid := image.Pt(cr.Min.X+cr.Dx()/2, cr.Min.Y+cr.Dy()/2)
	if label, r, ok := e.hoverAt(mid); !ok || !mid.In(r) || !r.In(cr.Inset(-2)) {
		t.Errorf("over the profile card: outline %q %v (%v), want something inside the card %v", label, r, ok, cr)
	}
	rows = e.pickReport(t, mid)
	if !strings.HasPrefix(rows["region"], "profile card") {
		t.Errorf("profile card report region %q, want the profile card", rows["region"])
	}
}

// TestInspectFreezeHoldsPopups: the reaction pill's who-reacted tooltip
// shows while the pointer rests on the pill and goes when it leaves.
// Frozen (F), the tooltip stays in the frozen frame with the pointer on
// it, the picker outlines it (div.c-tooltip__tip), and a Shift+click
// reports the tooltip with the tooltip as its region.
func TestInspectFreezeHoldsPopups(t *testing.T) {
	fakeShots(t)
	e := newInspectEnv(t)
	e.startSession(t) // the pointer on the pill
	// its tooltip up: ReactionTip's "reacted with" drawn (word by word:
	// flowTip)
	dl := time.Now().Add(5 * time.Second)
	for !drawnContains(frameStrings(e.perfEnv), "reacted") {
		if time.Now().After(dl) {
			t.Fatal("the pill's who-reacted tooltip never showed in the session")
		}
		time.Sleep(5 * time.Millisecond)
	}
	e.key(inspect.FreezeKey, 0)
	e.frame()
	if !e.a.insp.Frozen() {
		t.Fatal("F didn't freeze")
	}
	pill := e.pill.R
	var at image.Point
	found := ""
	var seen []string
	for dy := 10; dy <= 160 && found == ""; dy += 5 {
		p := image.Pt(pill.Min.X+pill.Dx()/2, pill.Min.Y-dy)
		label, r, ok := e.hoverAt(p)
		if ok && strings.Contains(label, "c-tooltip__tip") {
			at, found = p, label
		} else if dy%20 == 10 {
			seen = append(seen, fmt.Sprintf("%dpx: %q %v", dy, label, r))
		}
	}
	if found == "" {
		// where the tooltip is outlined at all, if anywhere
		where := "nowhere"
		for y := pill.Min.Y - 260; y < pill.Max.Y+100 && where == "nowhere"; y += 12 {
			for x := pill.Min.X - 260; x < pill.Max.X+260; x += 12 {
				if label, r, ok := e.hoverAt(image.Pt(x, y)); ok && strings.Contains(label, "c-tooltip__tip") {
					where = fmt.Sprintf("at %v (%v)", image.Pt(x, y), r)
					break
				}
			}
		}
		t.Fatalf("frozen with the pill's tooltip up: no tooltip outlined above the pill %v; outlined above it: %s; the record's layers: %q; the tooltip outlined %s", pill, strings.Join(seen, "; "), e.a.insp.RecordedLayers(), where)
	}
	if !e.a.insp.Frozen() {
		t.Fatal("the freeze ended while the pointer moved")
	}
	rows := e.pickReport(t, at)
	if !strings.Contains(rows["element"], "c-tooltip__tip") {
		t.Errorf("report element %q, want the tooltip's c-tooltip__tip", rows["element"])
	}
	if !strings.HasPrefix(rows["region"], "tooltip") {
		t.Errorf("report region %q, want the tooltip", rows["region"])
	}
}
