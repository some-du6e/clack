package popups

// The Add emoji dialog's Emoji packs tab, ported from Slack's 8DMj
// (AddEmojiDialog, AddEmojiPackDialogContent, EmojiPackCard,
// EmojiPackDetails) and its store (5BgF, eyHx, wv1J). It said "Emoji
// packs aren't available in Clack yet." (Karim's standing order: no
// stubs). Synthetic: the in-repo fake's packs, shaped as Hack Club's
// emoji.collections.list (read-only, 2026-11-22).

import (
	"encoding/json"
	"image"
	"image/color"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// epkHost is the test host with Slack's Toast: the parts each toast had,
// and its onCloseComplete for the test to run.
type epkHost struct {
	*tzHost
	tmu   sync.Mutex
	parts [][]i18n.Part
	gone  []func()
}

func (h *epkHost) ToastParts(p []i18n.Part, _ string, _ color.NRGBA, _ time.Duration) {
	h.tmu.Lock()
	h.parts = append(h.parts, p)
	h.tmu.Unlock()
}

func (h *epkHost) ToastPartsThen(p []i18n.Part, _ time.Duration, gone func()) {
	h.tmu.Lock()
	h.parts = append(h.parts, p)
	h.gone = append(h.gone, gone)
	h.tmu.Unlock()
}

func (h *epkHost) toastsSeen() ([][]i18n.Part, []func()) {
	h.tmu.Lock()
	defer h.tmu.Unlock()
	return append([][]i18n.Part(nil), h.parts...), append([]func(){}, h.gone...)
}

// epkRT drives the dialog through a Gio router in a 1000×900 window, the
// fake's realtime events straight into core, every call recorded.
type epkRT struct {
	t     *testing.T
	h     *epkHost
	fs    *fakeslack.Server
	d     *epkDialog
	m     *Modal
	r     input.Router
	ops   op.Ops
	calls pfCalls
	at    time.Duration
	win   image.Point
	texts []string
}

func newEPK(t *testing.T, setup func(fs *fakeslack.Server)) *epkRT {
	t.Helper()
	th, fs := pfEnv(t)
	if setup != nil {
		setup(fs)
	}
	e := &epkRT{t: t, h: &epkHost{tzHost: th}, fs: fs, win: image.Pt(1000, 900)}
	fs.Emit = func(ev map[string]any) {
		b, _ := json.Marshal(ev)
		typ, _ := ev["type"].(string)
		th.a.Inject(slack.Event{Type: typ, Raw: b})
	}
	fs.SetHook(func(m string, f url.Values) (any, bool) {
		e.calls.add(m, epkFields(f))
		return nil, false
	})
	return e
}

// epkFields is a call's fields without the client's token.
func epkFields(f url.Values) url.Values {
	g := url.Values{}
	for k, v := range f {
		if k != "token" {
			g[k] = v
		}
	}
	return g
}

// open shows the dialog on tab.
func (e *epkRT) open(tab int) {
	e.d, e.m = epkOpen(e.h, tab)
	e.h.ShowOverlay(e.m)
}

func (e *epkRT) frame() {
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(e.win), Source: e.r.Source()}
	paint.Fill(&e.ops, colorsOf(e.h.th).Bg)
	stop := theme.StartTofuScan()
	e.h.mu.Lock()
	shown := append([]Overlay(nil), e.h.shown...)
	e.h.mu.Unlock()
	for _, o := range shown {
		if !o.Dismissed() {
			o.Layout(gtx)
		}
	}
	st := stop()
	if st.Notdef > 0 {
		e.t.Errorf("%d .notdef glyphs in %q", st.Notdef, st.Texts)
	}
	e.texts = st.Texts
	e.r.Frame(&e.ops)
	drain()
}

func (e *epkRT) until(what string, cond func() bool) {
	e.t.Helper()
	for dl := time.Now().Add(10 * time.Second); ; {
		e.frame()
		if cond() {
			return
		}
		if time.Now().After(dl) {
			e.t.Fatalf("timed out waiting for %s (drawn: %q)", what, e.texts)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (e *epkRT) click(r image.Rectangle) {
	e.t.Helper()
	if r.Empty() {
		e.t.Fatal("clicking an empty rect")
	}
	p := f32.Pt(float32(r.Min.X+r.Max.X)/2, float32(r.Min.Y+r.Max.Y)/2)
	e.at += time.Second
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p, Time: e.at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: p, Time: e.at + time.Millisecond},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: p, Time: e.at + 2*time.Millisecond},
	)
	e.frame()
	e.frame()
}

// clickCard clicks pack id's card.
func (e *epkRT) clickCard(id string) {
	e.t.Helper()
	_, win, ok := e.d.card(id)
	if !ok {
		e.t.Fatalf("no card for %s", id)
	}
	e.click(win)
}

func (e *epkRT) drew(s string) bool { return slices.Contains(e.texts, s) }

// The tab through the dialog: Slack's list read, the packs newest first
// two to a row as 224 × 200 cards, the help line, no stub, within the
// dialog's 700px.
func TestEmojiPacksTabLikeSlack(t *testing.T) {
	e := newEPK(t, nil)
	e.open(0)
	e.frame()
	if n := len(e.calls.get("emoji.collections.list")); n != 0 {
		t.Fatalf("the Custom emoji tab asked for the packs %d times; Slack asks when the packs tab mounts", n)
	}
	e.click(e.d.tabRect(1))
	if e.d.tab() != 1 {
		t.Fatal("a click on Emoji packs didn't open the tab")
	}
	e.until("the cards", func() bool { return e.d.cards() == 5 })
	e.frame()
	for _, s := range e.texts {
		if strings.Contains(s, "available in") {
			t.Errorf("the stub is still drawn: %q", s)
		}
	}
	calls := e.calls.get("emoji.collections.list")
	if len(calls) != 1 || calls[0].Get("installed_only") != "false" {
		t.Errorf("emoji.collections.list asked %v; Slack's fetchEmojiCollections asks once, installed_only=false", calls)
	}
	for _, n := range []string{"Remote Work", "Alphabet", "Hybrid Work", "Gratitude", "Slackbot"} {
		if !e.drew(n) {
			t.Errorf("no %q card (drawn: %q)", n, e.texts)
		}
	}
	if e.drew("Halloween") {
		t.Error("drew Halloween, a pack only in installed: Slack's store keeps the available ones")
	}
	if e.drew("Added") {
		t.Error("drew \"Added\" with no pack installed")
	}
	// newest first, two to a row: Slackbot, Gratitude / Hybrid Work,
	// Alphabet / Remote Work and a spacer
	want := []struct {
		id       string
		row, col int
	}{
		{fakeslack.PackSlackbotID, 0, 0}, {fakeslack.PackGratitudeID, 0, 1},
		{fakeslack.PackHybridWorkID, 1, 0}, {fakeslack.PackAlphabetID, 1, 1},
		{fakeslack.PackRemoteWorkID, 2, 0},
	}
	for _, w := range want {
		r, _, _ := e.d.card(w.id)
		if r.Dx() != 224 || r.Dy() != 200 {
			t.Errorf("%s: a %v card; Slack's are 224 × 200 in the 464px body", w.id, r.Size())
		}
		if r.Min != image.Pt(w.col*240, w.row*216) {
			t.Errorf("%s at %v, want row %d column %d (%v)", w.id, r.Min, w.row, w.col, image.Pt(w.col*240, w.row*216))
		}
	}
	if h := e.d.height(); h > 700 {
		t.Errorf("the dialog is %d tall; .p-add_emoji_dialog's max-height is 700", h)
	}
	if !e.drew("Emoji") || !e.drew("picker") {
		t.Errorf("no help line (drawn: %q)", e.texts)
	}
	if !e.d.soon().Empty() {
		t.Error("drew Stay tuned with five packs; Slack shows it under two")
	}
}

// A pack's details from its card, then Add Pack: emoji.addCollection
// with the pack's id alone, the dialog closed, the InstallToast, the pack
// installed in the store, its emoji the workspace's, and the picker's
// Custom tab with the pack as its own group, Custom without its emoji.
func TestEmojiPackAddLikeSlack(t *testing.T) {
	e := newEPK(t, nil)
	// one custom emoji of the workspace's own, so Custom has a heading
	b, _ := json.Marshal(map[string]any{"type": "emoji_changed", "subtype": "add", "name": "own-parrot", "value": e.fs.URL + "img/64x64/" + fakeslack.TeamID + "/own-parrot/00000000000000ab.png"})
	e.h.a.Inject(slack.Event{Type: "emoji_changed", Raw: b})
	e.open(1)
	e.until("the cards", func() bool { return e.d.cards() == 5 })
	e.clickCard(fakeslack.PackGratitudeID)
	e.until("the details", func() bool { _, _, ok := e.d.detail(); return ok && e.drew("13 emoji") })
	if id, inst, _ := e.d.detail(); id != fakeslack.PackGratitudeID || inst {
		t.Fatalf("the details are %s (installed %v), want Gratitude, not installed", id, inst)
	}
	for _, s := range []string{"Gratitude", "by Slack", "|", "Add Pack"} {
		if !e.drew(s) {
			t.Errorf("the details drew no %q (drawn: %q)", s, e.texts)
		}
	}
	if e.drew("Remove Pack") || e.drew("Click an emoji to copy to clipboard") {
		t.Errorf("an uninstalled pack's details drew Remove Pack or the copy tip: %q", e.texts)
	}
	rs := e.d.emojiRects()
	if len(rs) != 13 {
		t.Fatalf("%d emoji, want the pack's 13", len(rs))
	}
	// five to a row, 56px, 28 between rows, space-between in 464
	for i, r := range rs {
		row, col := i/5, i%5
		if r.Dx() != 56 || r.Min.Y != row*84 {
			t.Errorf("emoji %d at %v, want 56px in row %d (y %d)", i, r, row, row*84)
		}
		if col == 0 && r.Min.X != 0 || col == 4 && r.Max.X != 464 {
			t.Errorf("emoji %d at x %d..%d: rows are space-between across 464", i, r.Min.X, r.Max.X)
		}
	}
	e.click(e.d.addRect())
	e.until("the dialog to close", func() bool { return e.m.Dismissed() })
	adds := e.calls.get("emoji.addCollection")
	if len(adds) != 1 || len(adds[0]) != 1 || adds[0].Get("collection_id") != fakeslack.PackGratitudeID {
		t.Fatalf("emoji.addCollection %v; Slack's addEmojiCollection sends collection_id=%s alone", adds, fakeslack.PackGratitudeID)
	}
	if !e.fs.EmojiPackInstalled(fakeslack.PackGratitudeID) {
		t.Fatal("the fake didn't install the pack")
	}
	parts, gone := e.h.toastsSeen()
	if len(parts) != 1 || i18n.Plain(parts[0]) != "The Gratitude emoji pack has been added to your workspace." {
		t.Fatalf("toasts %v, want the InstallToast", parts)
	}
	bold := false
	for _, p := range parts[0] {
		bold = bold || p.Text == "Gratitude" && p.Tag == "b"
	}
	if !bold {
		t.Errorf("the toast's pack name isn't bold: %+v", parts[0])
	}
	a := e.h.a
	packs, _ := epkPacks(a)
	var local []string
	for _, p := range packs {
		if p.ID == fakeslack.PackGratitudeID {
			if !p.Installed || len(p.Local) != 13 {
				t.Errorf("the store has Gratitude installed=%v with %d local emoji; emoji_collection_added installs it with 13", p.Installed, len(p.Local))
			}
			local = p.Local
		}
	}
	custom := a.CustomEmoji()
	for _, n := range local {
		if custom[n] == "" {
			t.Errorf("%s isn't a custom emoji after the add (emoji_changed)", n)
		}
	}
	// the badge waits for the toast to have gone (onCloseComplete)
	if epkBadge(a) {
		t.Error("the composer's badge showed before the toast closed")
	}
	if len(gone) != 1 || gone[0] == nil {
		t.Fatal("the InstallToast has no onCloseComplete")
	}
	gone[0]()
	if !epkBadge(a) {
		t.Error("no composer badge after the InstallToast closed (badgeEmojiCollection)")
	}
	epkClearBadge(a)
	if epkBadge(a) {
		t.Error("the badge stayed after removeEmojiCollectionInputBadge")
	}
	// the picker: the pack's group, under its name, before Custom
	read := epkPickerOf(e.h)
	var pk epkPicker
	for dl := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		var ok bool
		if pk, ok = read(); ok {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("the picker has no pack group")
		}
	}
	if !slices.Equal(pk.Groups, []string{"Gratitude"}) {
		t.Errorf("the pack groups are %q; Slack's is one group named after the pack", pk.Groups)
	}
	if !slices.Equal(pk.First, local) {
		t.Errorf("the pack group holds %q, want the pack's local emoji in its order %q", pk.First, local)
	}
	if pk.CustomShares {
		t.Error("Custom still holds the pack's emoji; Slack's leaves them to the pack's group")
	}
	if !pk.BeforeCustom {
		t.Error("the pack's group isn't right before Custom")
	}
	// opened again: "Added" with its count
	e.open(1)
	e.until("Added", func() bool { return e.drew("Added") && e.drew("1") })
}

// The same add with no custom emoji of the workspace's own: every custom
// emoji is the pack's, Custom holds none, and the Custom tab still goes
// to the pack's group (Slack's Custom tab is the packs' groups and Custom,
// and Custom stays whenever a pack does). Review of 013bdc44: the tab sat
// at Frequently Used and the Tab key skipped Custom.
func TestEmojiPackOnlyCustomTabLikeSlack(t *testing.T) {
	e := newEPK(t, nil)
	if n := len(e.h.a.CustomEmoji()); n != 0 {
		t.Fatalf("the workspace has %d custom emoji of its own: the case tests nothing", n)
	}
	e.open(1)
	e.until("the cards", func() bool { return e.d.cards() == 5 })
	e.clickCard(fakeslack.PackGratitudeID)
	e.until("the details", func() bool { _, _, ok := e.d.detail(); return ok && e.drew("Add Pack") })
	e.click(e.d.addRect())
	e.until("the dialog to close", func() bool { return e.m.Dismissed() })
	read := epkPickerOf(e.h)
	var pk epkPicker
	for dl := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		var ok bool
		if pk, ok = read(); ok {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("the picker has no pack group")
		}
	}
	if !slices.Equal(pk.Groups, []string{"Gratitude"}) {
		t.Fatalf("the pack groups are %q, want Gratitude's", pk.Groups)
	}
	if pk.CustomTab != "Gratitude" {
		t.Errorf("the Custom tab goes to %q; with every custom emoji a pack's, Slack's Custom tab shows the pack's group", pk.CustomTab)
	}
	if pk.CustomClick != "Gratitude" {
		t.Errorf("a click on the Custom tab scrolled to %q, want the pack's group", pk.CustomClick)
	}
}

// An installed pack's details for a workspace admin: the local emoji as
// copy buttons with the tip, Remove Pack, which empties the store first
// and closes with the removed toast; the next open reads the packs again.
func TestEmojiPackRemoveLikeSlack(t *testing.T) {
	e := newEPK(t, func(fs *fakeslack.Server) {
		fs.SetEmojiPacks(fakeslack.DefaultEmojiPacks(), fakeslack.PackGratitudeID)
	})
	if u, ok := e.h.a.S.User(fakeslack.SelfID); ok {
		su := *u
		su.IsAdmin = true
		e.h.a.S.PutUsers([]slack.User{su})
	}
	e.open(1)
	e.until("the cards", func() bool { return e.d.cards() == 5 && e.drew("Added") })
	e.clickCard(fakeslack.PackGratitudeID)
	e.until("the details", func() bool { _, _, ok := e.d.detail(); return ok && e.drew("Remove Pack") })
	if !e.drew("Click an emoji to copy to clipboard") {
		t.Errorf("an installed pack's details have no copy tip (drawn: %q)", e.texts)
	}
	if _, inst, _ := e.d.detail(); !inst || e.d.emojiRects()[0].Dx() != 72 {
		t.Errorf("installed %v, emoji button %v: installed emoji are 72px buttons (56 + padding 8)", inst, e.d.emojiRects()[0])
	}
	e.click(e.d.removeRect())
	e.until("the dialog to close", func() bool { return e.m.Dismissed() })
	rm := e.calls.get("emoji.removeCollection")
	if len(rm) != 1 || rm[0].Get("collection_id") != fakeslack.PackGratitudeID {
		t.Fatalf("emoji.removeCollection %v, want collection_id=%s", rm, fakeslack.PackGratitudeID)
	}
	parts, _ := e.h.toastsSeen()
	if len(parts) != 1 || i18n.Plain(parts[0]) != "The Gratitude emoji pack has been removed from your workspace." {
		t.Errorf("toasts %v, want the removed InstallToast", parts)
	}
	if _, fetched := epkPacks(e.h.a); fetched {
		t.Error("the store kept hasFetched after the removal; uninstallEmojiCollection resets it")
	}
	before := len(e.calls.get("emoji.collections.list"))
	e.open(1)
	e.until("the packs again", func() bool { return len(e.calls.get("emoji.collections.list")) > before && e.d.cards() == 5 })
	if e.drew("Added") {
		t.Error("drew Added after the only installed pack went")
	}
}

// Under two packs Slack adds its "Stay tuned" card; a failed read draws
// nothing and is tried again on the next open.
func TestEmojiPacksSoonAndFailedRead(t *testing.T) {
	e := newEPK(t, func(fs *fakeslack.Server) {
		fs.SetEmojiPacks(fakeslack.DefaultEmojiPacks()[:1])
	})
	var mu sync.Mutex
	fail := true
	e.fs.SetHook(func(m string, f url.Values) (any, bool) {
		e.calls.add(m, epkFields(f))
		mu.Lock()
		defer mu.Unlock()
		if m == "emoji.collections.list" && fail {
			return map[string]any{"ok": false, "error": "fatal_error"}, true
		}
		return nil, false
	})
	e.open(1)
	e.until("the failed read", func() bool { return len(e.calls.get("emoji.collections.list")) == 1 })
	for i := 0; i < 20; i++ {
		e.frame()
	}
	if e.d.cards() != 0 || e.drew("Added") || e.drew("Stay tuned") {
		t.Errorf("drew packs after a failed read (drawn: %q)", e.texts)
	}
	if n := len(e.calls.get("emoji.collections.list")); n != 1 {
		t.Errorf("%d reads while the tab stayed open; Slack's content asks when it mounts, not every frame", n)
	}
	if _, fetched := epkPacks(e.h.a); fetched {
		t.Fatal("hasFetched stayed after the read failed")
	}
	mu.Lock()
	fail = false
	mu.Unlock()
	e.m.Close()
	e.open(1)
	e.until("the one pack", func() bool { return e.d.cards() == 1 })
	e.until("Stay tuned", func() bool {
		return e.drew("Stay tuned") && slices.ContainsFunc(e.texts, func(s string) bool { return strings.Contains(s, "More emoji packs") })
	})
	if r := e.d.soon(); r.Min != image.Pt(240, 0) || r.Dx() != 224 || r.Dy() != 200 {
		t.Errorf("Stay tuned at %v, want the first row's second 224 × 200 card", r)
	}
}
