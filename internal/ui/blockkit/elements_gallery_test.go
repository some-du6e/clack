package blockkit

// The interactive elements of the Block Kit gallery (internal/fakeslack,
// bkgallery_elements.go), clicked for real: a fake host around a core.App
// talking to fakeslack, the message's element widgets laid out at known
// positions, Gio pointer and key events through an input.Router, and the
// blocks.actions / blocks.suggestions calls fakeslack recorded checked
// against the shapes Slack's web client sends (gantry-v2 2026-11, the
// element components' onAction objects). With BK_ELEMENTS_SHOTS_DIR set
// (the bk-elements workflow, under xvfb) every element is also saved
// closed and open, light and dark. Synthetic fixtures only.

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// ---- host ----

type bkHost struct {
	a        *core.App
	th       *theme.Theme
	img      *imgcache.Cache
	mu       sync.Mutex
	posts    []func()
	overlays []popups.Overlay
	ptr      image.Point
	routes   []string
	toasts   []string
}

func (h *bkHost) Core() *core.App                                         { return h.a }
func (h *bkHost) Theme() *theme.Theme                                     { return h.th }
func (h *bkHost) Images() *imgcache.Cache                                 { return h.img }
func (h *bkHost) Invalidate()                                             {}
func (h *bkHost) Navigate(r string)                                       { h.routes = append(h.routes, r) }
func (h *bkHost) OpenThread(channel, ts string)                           {}
func (h *bkHost) CloseSecondary()                                         {}
func (h *bkHost) ShowOverlay(o popups.Overlay)                            { h.overlays = append(h.overlays, o) }
func (h *bkHost) OpenEmojiPicker(anchor image.Point, onPick func(string)) {}
func (h *bkHost) OpenProfile(anchor image.Point, userID string)           {}
func (h *bkHost) OpenMessageMenu(anchor image.Point, m core.Message)      {}
func (h *bkHost) OpenLightbox(img richtext.Image, m core.Message)         {}
func (h *bkHost) Toast(s string)                                          { h.toasts = append(h.toasts, s) }
func (h *bkHost) PointerPos() image.Point                                 { return h.ptr }
func (h *bkHost) Post(f func()) {
	h.mu.Lock()
	h.posts = append(h.posts, f)
	h.mu.Unlock()
}

func (h *bkHost) runPosts() {
	h.mu.Lock()
	ps := h.posts
	h.posts = nil
	h.mu.Unlock()
	for _, f := range ps {
		f()
	}
}

// ---- environment ----

type bkEnv struct {
	t      *testing.T
	h      *bkHost
	fs     *fakeslack.Server
	r      input.Router
	ops    op.Ops
	size   image.Point
	win    *headless.Window
	dir    string
	msgs   map[string]core.Message
	cur    string
	origin image.Point // where the message's blocks start
	width  int
	urls   []string
	modals [][]*popups.ModalButton
}

func newBKEnv(t *testing.T, dark bool, shots bool) *bkEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.Gallery = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	// element hosts are cached per message across hosts; start clean
	msgsMu.Lock()
	msgs, order = map[string]*Message{}, nil
	msgsMu.Unlock()
	h := &bkHost{a: a, th: theme.New(dark)}
	h.img = imgcache.New(func(ctx context.Context, u string) ([]byte, error) {
		b, _, err := a.Fetch(ctx, u)
		return b, err
	}, "", nil)
	e := &bkEnv{t: t, h: h, fs: fs, size: image.Pt(1000, 760), origin: image.Pt(40, 40), width: 720, msgs: map[string]core.Message{}}
	pg, err := a.History(context.Background(), fakeslack.GalleryChannelID, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range pg.Messages {
		if n, ok := strings.CutPrefix(m.ClientMsgID, "bk-elements-"); ok {
			e.msgs[n] = m
		}
	}
	if len(e.msgs) != len(fakeslack.GalleryElementNames) {
		t.Fatalf("gallery: %d element messages, want %d", len(e.msgs), len(fakeslack.GalleryElementNames))
	}
	oldURL, oldModal := openURL, modalShown
	openURL = func(u string) { e.urls = append(e.urls, u) }
	modalShown = func(_ popups.Overlay, bs []*popups.ModalButton) { e.modals = append(e.modals, bs) }
	t.Cleanup(func() { openURL, modalShown = oldURL, oldModal })
	if shots {
		e.dir = os.Getenv("BK_ELEMENTS_SHOTS_DIR")
		if e.dir != "" {
			w, err := headless.NewWindow(e.size.X, e.size.Y)
			if err != nil {
				t.Fatalf("headless window: %v", err)
			}
			e.win = w
			t.Cleanup(w.Release)
		}
	}
	return e
}

// show puts gallery message name on screen (and resets nothing: values
// stay until the message's blocks change, like Slack).
func (e *bkEnv) show(name string) *Message {
	e.t.Helper()
	m, ok := e.msgs[name]
	if !ok {
		e.t.Fatalf("no gallery message %q", name)
	}
	e.cur = name
	me := MessageElements(e.h, &m)
	if me == nil {
		e.t.Fatalf("%s: not interactive", name)
	}
	e.frame()
	e.frame()
	return me
}

func (e *bkEnv) message() *Message {
	m := e.msgs[e.cur]
	return MessageElements(e.h, &m)
}

func (e *bkEnv) context() layout.Context {
	e.ops.Reset()
	return layout.Context{Ops: &e.ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(e.size), Source: e.r.Source()}
}

// slots remembers where each block's elements were drawn this frame.
type slotRect struct {
	block string
	at    image.Point
}

func (e *bkEnv) frame() []slotRect {
	e.h.runPosts()
	drain()
	gtx := e.context()
	th := e.h.th
	cl := colorsOf(th)
	paint.FillShape(gtx.Ops, cl.Bg, clip.Rect{Max: e.size}.Op())
	var slots []slotRect
	if e.cur != "" {
		slots = e.layoutMessage(gtx)
	}
	live := e.h.overlays[:0]
	for _, o := range e.h.overlays {
		g := gtx
		g.Constraints = layout.Constraints{Max: e.size}
		o.Layout(g)
		if !o.Dismissed() {
			live = append(live, o)
		}
	}
	e.h.overlays = live
	e.r.Frame(&e.ops)
	return slots
}

// layoutMessage draws the message's blocks the way richview places them:
// section text with its accessory on the right (or under it when the
// element stacks), actions runs full width.
func (e *bkEnv) layoutMessage(gtx layout.Context) []slotRect {
	m := e.msgs[e.cur]
	me := MessageElements(e.h, &m)
	th := e.h.th
	cl := colorsOf(th)
	var out []slotRect
	y := e.origin.Y
	for _, b := range m.Doc.Blocks {
		text := plainOf(b)
		if len(b.Buttons) == 0 {
			if text != "" {
				at(gtx, image.Pt(e.origin.X, y), func(gtx C) D { return label(gtx, th, 15, theme.Regular, cl.Text, text) })
				y += 26
			}
			continue
		}
		if b.Kind == richtext.Actions {
			sz, _ := me.ElementsSize(b.Buttons, e.width, false, gtx.Metric)
			p := image.Pt(e.origin.X, y)
			at(gtx, p, func(gtx C) D { return me.LayoutElements(fixed(gtx, sz), b.Buttons, false) })
			out = append(out, slotRect{b.Buttons[0].BlockID, p})
			y += sz.Y + 12
			continue
		}
		sz, stack := me.ElementsSize(b.Buttons[:1], e.width, true, gtx.Metric)
		if text != "" {
			at(gtx, image.Pt(e.origin.X, y), func(gtx C) D { return label(gtx, th, 15, theme.Bold, cl.Text, text) })
		}
		p := image.Pt(e.origin.X+e.width-sz.X, y)
		if stack || b.Buttons[0].Kind == "input" {
			y += 26
			p = image.Pt(e.origin.X, y)
		}
		at(gtx, p, func(gtx C) D { return me.LayoutElements(fixed(gtx, sz), b.Buttons[:1], true) })
		out = append(out, slotRect{b.Buttons[0].BlockID, p})
		y += max(sz.Y, 22) + 12
	}
	return out
}

func plainOf(b richtext.Block) string {
	var s strings.Builder
	for _, sp := range b.Spans {
		s.WriteString(sp.Text)
	}
	return s.String()
}

// widget returns the element widget and its window rectangle.
func (e *bkEnv) widget(bid, aid string) (*ew, image.Rectangle) {
	e.t.Helper()
	slots := e.frame()
	me := e.message()
	w := me.ws[bid+"\x00"+aid]
	if w == nil {
		e.t.Fatalf("no widget %s/%s", bid, aid)
	}
	for _, s := range slots {
		if s.block == bid {
			sz := w.main.sz
			if w.dialogMulti() {
				sz = w.btn.sz
			}
			if sz == (image.Point{}) && len(w.rows) > 0 {
				sz = image.Pt(w.rows[0].sz.X, 20) // checkboxes / radios
			}
			if sz == (image.Point{}) && w.ed != nil {
				sz = image.Pt(40, 20) // text inputs: a box inside the field
			}
			if sz == (image.Point{}) {
				e.t.Fatalf("%s/%s not drawn", bid, aid)
			}
			p := s.at.Add(w.slot)
			return w, image.Rectangle{Min: p, Max: p.Add(sz)}
		}
	}
	e.t.Fatalf("block %s not on screen", bid)
	return nil, image.Rectangle{}
}

func center(r image.Rectangle) image.Point { return r.Min.Add(r.Size().Div(2)) }

func (e *bkEnv) click(p image.Point) {
	e.h.ptr = p
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.r.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt},
	)
	e.frame()
	e.frame()
}

func (e *bkEnv) hover(p image.Point) {
	e.h.ptr = p
	e.r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(float32(p.X), float32(p.Y))})
	e.frame()
}

func (e *bkEnv) clickEl(bid, aid string) *ew {
	w, r := e.widget(bid, aid)
	e.click(center(r))
	return w
}

func (e *bkEnv) key(n key.Name) {
	e.r.Queue(key.Event{Name: n, State: key.Press}, key.Event{Name: n, State: key.Release})
	e.frame()
	e.frame()
}

func (e *bkEnv) typeText(s string) {
	st := e.r.EditorState()
	e.r.Queue(key.EditEvent{Range: st.Selection.Range, Text: s})
	e.frame()
	e.frame() // the router learns the new selection
}

// typeEnd types s at the end of w's editor.
func (e *bkEnv) typeEnd(w *ew, s string) {
	n := w.ed.Len()
	e.r.Queue(key.EditEvent{Range: key.Range{Start: n, End: n}, Text: s})
	e.frame()
	e.frame()
}

func (e *bkEnv) until(what string, d time.Duration, cond func() bool) {
	e.t.Helper()
	dl := time.Now().Add(d)
	for !cond() {
		if time.Now().After(dl) {
			e.t.Fatalf("timed out after %v waiting for %s", d, what)
		}
		e.frame()
		time.Sleep(5 * time.Millisecond)
	}
	e.frame()
}

func (e *bkEnv) settle(d time.Duration) {
	for dl := time.Now().Add(d); time.Now().Before(dl); {
		e.frame()
		time.Sleep(5 * time.Millisecond)
	}
}

func (e *bkEnv) closeOverlays() {
	for _, o := range e.h.overlays {
		if d, ok := o.(interface{ Dismiss() }); ok {
			d.Dismiss()
		}
	}
	e.h.overlays = nil
	e.frame()
	e.frame()
}

func topOverlay[T any](e *bkEnv) (T, bool) {
	for i := len(e.h.overlays) - 1; i >= 0; i-- {
		if v, ok := e.h.overlays[i].(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// shot saves the window as name.png (with BK_ELEMENTS_SHOTS_DIR).
func (e *bkEnv) shot(name string) {
	e.t.Helper()
	e.frame()
	if e.win == nil {
		return
	}
	if err := e.win.Frame(&e.ops); err != nil {
		e.t.Fatalf("frame: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.win.Screenshot(img); err != nil {
		e.t.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(e.dir, name+".png"))
	if err != nil {
		e.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		e.t.Fatal(err)
	}
}

// ---- payload checks ----

// calls waits for n blocks.actions calls and returns them.
func (e *bkEnv) calls(n int) []fakeslack.BlockActionCall {
	e.t.Helper()
	e.until(fmt.Sprintf("%d blocks.actions calls", n), 10*time.Second, func() bool { return len(e.fs.BlockActionCalls()) >= n })
	cs := e.fs.BlockActionCalls()
	if len(cs) != n {
		e.t.Fatalf("%d blocks.actions calls, want %d", len(cs), n)
	}
	return cs
}

// noNewCall checks that no blocks.actions call arrives in d.
func (e *bkEnv) noNewCall(n int, d time.Duration, why string) {
	e.t.Helper()
	e.settle(d)
	if got := len(e.fs.BlockActionCalls()); got != n {
		e.t.Errorf("%s: %d blocks.actions calls, want %d", why, got, n)
	}
}

// norm turns a JSON-able value into plain maps for comparison.
func norm(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func sameJSON(t *testing.T, what string, got, want any) {
	t.Helper()
	g, w := norm(t, got), norm(t, want)
	if !reflect.DeepEqual(g, w) {
		gb, _ := json.Marshal(g)
		wb, _ := json.Marshal(w)
		t.Errorf("%s:\n got %s\nwant %s", what, gb, wb)
	}
}

// dig walks nested JSON objects (nil when a key is missing).
func dig(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func pt(s string) map[string]any {
	return map[string]any{"type": "plain_text", "text": s, "emoji": true}
}

func opt(text, value string) map[string]any { return map[string]any{"text": pt(text), "value": value} }

// ---- the tests ----

// Every element of the gallery, clicked, sends Slack's payload.
func TestGalleryElementPayloads(t *testing.T) {
	e := newBKEnv(t, false, false)
	ts := e.fs.GalleryElementsTS()
	n := 0
	next := func() fakeslack.BlockActionCall {
		t.Helper()
		n++
		return e.calls(n)[n-1]
	}

	// buttons: payload, container and service ids
	e.show("buttons")
	e.clickEl("bk_buttons", "btn_default")
	c := next()
	sameJSON(t, "button action", c.Actions, []any{map[string]any{"type": "button", "block_id": "bk_buttons", "action_id": "btn_default",
		"text": pt("Default"), "value": "d"}})
	sameJSON(t, "message container", c.Container, map[string]any{"type": "message", "message_ts": ts["buttons"],
		"channel_id": fakeslack.GalleryChannelID, "is_ephemeral": false})
	if c.Form.Get("service_id") != fakeslack.GalleryBotID || c.Form.Get("app_id") != fakeslack.GalleryAppID ||
		c.Form.Get("service_team_id") != fakeslack.TeamID || c.Form.Get("client_token") == "" {
		t.Errorf("service ids: %v", c.Form)
	}
	if c.Form.Has("state") {
		t.Errorf("a message without stateful elements sends no state: %s", c.Form.Get("state"))
	}
	// a second click within a second is swallowed (isProcessingClick)
	w := e.clickEl("bk_buttons", "btn_default")
	if !w.disabled(e.context()) {
		t.Error("button not disabled right after a click")
	}
	e.noNewCall(n, 300*time.Millisecond, "double click")
	e.settle(800 * time.Millisecond)
	if w.disabled(e.context()) {
		t.Error("button still disabled after a second")
	}

	e.clickEl("bk_buttons", "btn_primary")
	sameJSON(t, "primary", next().Actions[0], map[string]any{"type": "button", "block_id": "bk_buttons", "action_id": "btn_primary",
		"text": pt("Approve"), "value": "yes", "style": "primary"})

	// url button: the action (no url field), then the link
	e.clickEl("bk_buttons", "btn_url")
	sameJSON(t, "url button", next().Actions[0], map[string]any{"type": "button", "block_id": "bk_buttons", "action_id": "btn_url",
		"text": pt("Open docs"), "value": "url"})
	if len(e.urls) != 1 || e.urls[0] != "https://example.com/docs" {
		t.Errorf("url opened: %v", e.urls)
	}

	// confirm: nothing until Delete
	e.modals = nil
	e.clickEl("bk_buttons", "btn_confirm")
	if len(e.modals) != 1 || e.modals[0][0].Label != "Keep it" || e.modals[0][1].Label != "Delete" || e.modals[0][1].Kind != popups.BtnDanger {
		t.Fatalf("confirm dialog buttons: %+v", e.modals)
	}
	e.noNewCall(n, 100*time.Millisecond, "before confirming")
	e.modals[0][1].Do()
	sameJSON(t, "confirmed", next().Actions[0], map[string]any{"type": "button", "block_id": "bk_buttons", "action_id": "btn_confirm",
		"text": pt("Delete build"), "value": "del", "style": "danger"})
	e.closeOverlays()

	// loading: the block's elements are disabled and the spinner shows
	e.settle(1100 * time.Millisecond)
	e.clickEl("bk_buttons_state", "btn_slow")
	me := e.message()
	if !me.loading("bk_buttons_state") {
		t.Error("slow action: block not loading")
	}
	next() // the slow call reached the app (it answers in 1.5 s)
	e.clickEl("bk_buttons_state", "btn_fail")
	e.noNewCall(n, 200*time.Millisecond, "a loading block's button")
	e.until("slow action settles", 5*time.Second, func() bool { return !me.loading("bk_buttons_state") })

	// errors: the server's message is the tooltip; known ones show text
	e.settle(1100 * time.Millisecond)
	e.clickEl("bk_buttons_state", "btn_fail")
	next()
	e.until("error shown", 5*time.Second, func() bool { _, ok := me.errs["bk_buttons_state"]; return ok })
	if got := me.errs["bk_buttons_state"]; got != fakeslack.GalleryFailMessage {
		t.Errorf("error tooltip %q", got)
	}
	e.settle(1100 * time.Millisecond)
	e.clickEl("bk_buttons_state", "btn_disabled")
	next()
	e.until("disabled error", 5*time.Second, func() bool { return me.errs["bk_buttons_state"] == "action_disabled" })
	if knownActionError(me.errs["bk_buttons_state"]) != "This action isn’t available" {
		t.Error("action_disabled text")
	}

	// overflow: {text, value} only
	e.show("overflow")
	e.clickEl("bk_overflow_sec", "overflow_acc")
	menu, ok := topOverlay[*popups.Menu](e)
	if !ok {
		t.Fatal("overflow: no menu")
	}
	var labels []string
	for _, it := range menu.Items() {
		labels = append(labels, it.Label)
	}
	if strings.Join(labels, "|") != "Edit|Duplicate|Archive|View on the web|Delete" {
		t.Errorf("overflow items %v", labels)
	}
	menu.Items()[2].Do()
	sameJSON(t, "overflow", next().Actions[0], map[string]any{"type": "overflow", "block_id": "bk_overflow_sec", "action_id": "overflow_acc",
		"selected_option": opt("Archive", "archive")})
	e.closeOverlays()

	// static select with option groups: the placeholder rides along
	e.show("selects")
	e.clickEl("bk_selects", "static_groups")
	sm, ok := topOverlay[*selectMenu](e)
	if !ok {
		t.Fatal("static select: no menu")
	}
	var rows []string
	for _, it := range sm.items {
		if it.header {
			rows = append(rows, "["+it.label+"]")
		} else {
			rows = append(rows, it.label)
		}
	}
	if strings.Join(rows, "|") != "[Common]|Apple|Banana|[Stone fruit]|Cherry|Peach" {
		t.Errorf("option groups: %v", rows)
	}
	e.key(key.NameDownArrow)
	e.key(key.NameReturn)
	c = next()
	sameJSON(t, "static_select", c.Actions[0], map[string]any{"type": "static_select", "block_id": "bk_selects", "action_id": "static_groups",
		"selected_option": opt("Banana", "banana"), "placeholder": pt("Choose a fruit")})
	// message state: set values and initial ones (Slack's getInitialBlockState)
	if b, _ := dig(c.State, "values", "bk_selects").(map[string]any); b == nil || b["static_initial"] == nil || b["external_groups"] == nil || b["static_groups"] == nil || b["external_any"] != nil {
		t.Errorf("message state: %v", c.State)
	}

	// external select: min_query_length 0 asks on open; typing asks again
	before := len(e.fs.Suggestions())
	e.clickEl("bk_selects", "external_any")
	e.until("suggestions on open", 5*time.Second, func() bool { return len(e.fs.Suggestions()) > before })
	f := e.fs.Suggestions()[before]
	if f.Get("action_id") != "external_any" || f.Get("block_id") != "bk_selects" || f.Get("value") != "" ||
		f.Get("service_id") != fakeslack.GalleryBotID || !strings.Contains(f.Get("container"), ts["selects"]) {
		t.Errorf("blocks.suggestions form %v", f)
	}
	e.typeText("an")
	e.until("suggestions for an", 5*time.Second, func() bool {
		ss := e.fs.Suggestions()
		return ss[len(ss)-1].Get("value") == "an"
	})
	sm, _ = topOverlay[*selectMenu](e)
	e.until("suggested items", 5*time.Second, func() bool { return len(sm.items) == 3 && !sm.busy })
	e.key(key.NameReturn)
	sameJSON(t, "external_select", next().Actions[0], map[string]any{"type": "external_select", "block_id": "bk_selects", "action_id": "external_any",
		"selected_option": opt("Banana", "banana"), "placeholder": pt("Search fruit")})
	e.closeOverlays()

	// external with the default min_query_length (3): nothing until 3 chars
	before = len(e.fs.Suggestions())
	e.clickEl("bk_selects", "external_groups")
	sm, _ = topOverlay[*selectMenu](e)
	if got := sm.emptyMessage(); got != "Type a minimum of 3 characters to see options." {
		t.Errorf("min query message %q", got)
	}
	e.typeText("ber")
	e.until("grouped suggestions", 5*time.Second, func() bool { return len(sm.items) > 0 && !sm.busy })
	if !sm.items[0].header || sm.items[0].label != "Berries" {
		t.Errorf("option_groups suggestions: %+v", sm.items)
	}
	e.key(key.NameReturn)
	sameJSON(t, "external_select initial", next().Actions[0], map[string]any{"type": "external_select", "block_id": "bk_selects",
		"action_id": "external_groups", "selected_option": opt("Blackberry", "blackberry"), "initial_option": opt("Mango", "mango")})
	if len(e.fs.Suggestions())-before != 1 {
		t.Errorf("external select under min_query_length asked %d times", len(e.fs.Suggestions())-before)
	}
	e.closeOverlays()

	// people: no placeholder in these payloads; initial_* and
	// response_url_enabled are
	e.show("people")
	e.clickEl("bk_people", "users_one")
	sm, _ = topOverlay[*selectMenu](e)
	e.until("people listed on an empty query", 5*time.Second, func() bool { return len(sm.items) > 0 && !sm.busy })
	pick := sm.items[firstRow(sm.items)].id
	e.key(key.NameReturn)
	c = next()
	sameJSON(t, "users_select", c.Actions[0], map[string]any{"type": "users_select", "block_id": "bk_people", "action_id": "users_one",
		"selected_user": pick, "initial_user": fakeslack.ProfileID})
	sameJSON(t, "default_to_current_conversation", dig(c.State, "values", "bk_people", "convs_current"),
		map[string]any{"type": "conversations_select", "selected_conversation": fakeslack.GalleryChannelID})
	e.closeOverlays()
	e.clickEl("bk_people", "channels_one")
	sm, _ = topOverlay[*selectMenu](e)
	e.until("channels listed", 5*time.Second, func() bool { return len(sm.items) > 0 })
	ch := sm.items[firstRow(sm.items)].id
	e.key(key.NameReturn)
	sameJSON(t, "channels_select", next().Actions[0], map[string]any{"type": "channels_select", "block_id": "bk_people", "action_id": "channels_one",
		"selected_channel": ch, "initial_channel": "C0FAKE00001", "response_url_enabled": true})
	e.closeOverlays()

	// multi-selects in a message: a button and a dialog; Go sends
	e.show("multi")
	w, r := e.widget("bk_multi", "multi_static")
	if w.multiButtonText() != "1 selected" {
		t.Errorf("multi button %q", w.multiButtonText())
	}
	e.modals = nil
	e.click(center(r))
	if !w.inDialog || len(e.modals) != 1 {
		t.Fatal("multi-select dialog not open")
	}
	openSelect(w, image.Rectangle{Max: image.Pt(400, 36)})
	e.frame()
	e.key(key.NameDownArrow) // Banana (Apple is chosen)
	e.key(key.NameReturn)
	e.key(key.NameDownArrow)
	e.key(key.NameDownArrow)
	e.key(key.NameReturn) // Cherry: three, over max_selected_items 2
	if !w.overMax() || e.modals[0][1].Enabled() {
		t.Errorf("over max: overMax %v, Go enabled %v", w.overMax(), e.modals[0][1].Enabled())
	}
	if w.maxAlert() != "You can select up to 2 items." {
		t.Errorf("max alert %q", w.maxAlert())
	}
	e.noNewCall(n, 100*time.Millisecond, "picking in the dialog")
	w.remove("cherry")
	if e.modals[0][1].Enabled() != true {
		t.Error("Go still disabled at max")
	}
	e.closeOverlays() // the select menu (and the dialog: Cancel restores)
	if got := len(w.chips()); got != 1 || w.inDialog {
		t.Errorf("cancel: %d chips, inDialog %v", got, w.inDialog)
	}
	// again, and Go
	e.modals = nil
	e.click(center(r))
	openSelect(w, image.Rectangle{Max: image.Pt(400, 36)})
	e.frame()
	e.key(key.NameDownArrow)
	e.key(key.NameReturn)
	e.modals[0][1].Do()
	sameJSON(t, "multi_static_select", next().Actions[0], map[string]any{"type": "multi_static_select", "block_id": "bk_multi",
		"action_id": "multi_static", "selected_options": []any{opt("Apple", "apple"), opt("Banana", "banana")},
		"initial_options": []any{opt("Apple", "apple")}})
	e.closeOverlays()
	w, r = e.widget("bk_multi", "multi_users")
	e.click(center(r))
	e.modals[len(e.modals)-1][1].Do()
	sameJSON(t, "multi_users_select", next().Actions[0], map[string]any{"type": "multi_users_select", "block_id": "bk_multi",
		"action_id": "multi_users", "selected_users": []string{fakeslack.ProfileID, fakeslack.AdminID},
		"initial_users": []string{fakeslack.ProfileID, fakeslack.AdminID}})
	e.closeOverlays()
	_ = w

	// pickers (the popups pick through the same commit)
	e.show("pickers")
	w = e.clickEl("bk_pickers", "date_one")
	if !w.open {
		t.Error("datepicker didn't open")
	}
	e.closeOverlays()
	prev := w.snapshot()
	w.value().SelectedDate = "2026-11-21"
	w.commit(prev)
	sameJSON(t, "datepicker", next().Actions[0], map[string]any{"type": "datepicker", "block_id": "bk_pickers", "action_id": "date_one",
		"selected_date": "2026-11-21", "initial_date": "2026-11-20"})
	w, _ = e.widget("bk_pickers", "time_one")
	prev = w.snapshot()
	w.value().SelectedTime = "14:00"
	w.commit(prev)
	sameJSON(t, "timepicker", next().Actions[0], map[string]any{"type": "timepicker", "block_id": "bk_pickers", "action_id": "time_one",
		"selected_time": "14:00", "initial_time": "13:30", "timezone": "Europe/Berlin"})
	w, _ = e.widget("bk_pickers", "datetime_one")
	prev = w.snapshot()
	w.value().SelectedDateTime = 1795003600
	w.commit(prev)
	sameJSON(t, "datetimepicker", next().Actions[0], map[string]any{"type": "datetimepicker", "block_id": "bk_pickers",
		"action_id": "datetime_one", "selected_date_time": 1795003600, "initial_date_time": 1795000000})

	// checkboxes (no initial_options in the payload) and radio buttons
	e.show("options")
	w, r = e.widget("bk_checks", "checks")
	y0 := w.optionTextHeight(e.context(), &w.e.Options[0], r.Dx()-22) + 8
	e.click(image.Pt(r.Min.X+8, r.Min.Y+y0+6))
	email := map[string]any{"text": map[string]any{"type": "mrkdwn", "text": "*Email* me"}, "value": "email",
		"description": map[string]any{"type": "mrkdwn", "text": "A digest every _morning_"}}
	push := map[string]any{"text": pt("Push"), "value": "push", "description": pt("Right away")}
	sameJSON(t, "checkboxes", next().Actions[0], map[string]any{"type": "checkboxes", "block_id": "bk_checks", "action_id": "checks",
		"selected_options": []any{email, push}})
	_, r = e.widget("bk_radios", "radios")
	e.click(image.Pt(r.Min.X+8, r.Min.Y+8))
	sameJSON(t, "radio_buttons", next().Actions[0], map[string]any{"type": "radio_buttons", "block_id": "bk_radios", "action_id": "radios",
		"selected_option": map[string]any{"text": pt("Daily"), "value": "daily", "description": pt("Every weekday at 9")},
		"initial_option":  opt("Weekly", "weekly")})

	// inputs: Enter dispatches the trimmed value
	e.show("inputs")
	e.clickEl("bk_in_enter", "in_enter")
	e.typeText("  Printer on fire  ")
	e.key(key.NameReturn)
	c = next()
	sameJSON(t, "plain_text_input", c.Actions[0], map[string]any{"type": "plain_text_input", "block_id": "bk_in_enter",
		"action_id": "in_enter", "value": "Printer on fire"})
	sameJSON(t, "input state", dig(c.State, "values", "bk_in_multi"), map[string]any{"in_multi": map[string]any{"type": "plain_text_input", "value": "First line"}})
	// on_character_entered: leading, then trailing after 500ms
	w = e.clickEl("bk_in_chars", "in_chars")
	e.typeText("a")
	e.typeEnd(w, "b")
	c = next()
	if v := c.Actions[0]["value"]; v != "a" {
		t.Errorf("leading character dispatch %v", v)
	}
	e.noNewCall(n, 200*time.Millisecond, "inside the debounce")
	c = next()
	if v := c.Actions[0]["value"]; v != "ab" {
		t.Errorf("trailing character dispatch %v", v)
	}
	// number_input: Number(x).toString()
	e.clickEl("bk_in_number", "in_number")
	e.typeText("007.50")
	e.key(key.NameReturn)
	sameJSON(t, "number_input", next().Actions[0], map[string]any{"type": "number_input", "block_id": "bk_in_number",
		"action_id": "in_number", "value": "7.5"})
	// a block without dispatch_action never sends
	e.clickEl("bk_in_email", "in_email")
	e.typeText("ada@example.com")
	e.key(key.NameReturn)
	e.noNewCall(n, 200*time.Millisecond, "email without dispatch_action")

	// workflow buttons trip their trigger and send no action; icon
	// buttons send icon, text and value
	e.show("workflow")
	e.clickEl("bk_workflow", "wf_start")
	e.noNewCall(n, 300*time.Millisecond, "workflow button")
	e.clickEl("bk_workflow", "icon_trash")
	sameJSON(t, "icon_button", next().Actions[0], map[string]any{"type": "icon_button", "block_id": "bk_workflow", "action_id": "icon_trash",
		"icon": "trash", "text": pt("Delete"), "value": "trash-1"})

	// six elements all show; seven show five and "+ 2 more"
	e.show("more")
	me = e.message()
	drawn := func(bid string) int {
		k := 0
		for key, w := range me.ws {
			if strings.HasPrefix(key, bid+"\x00") && w.main.sz != (image.Point{}) {
				k++
			}
		}
		return k
	}
	if got := drawn("bk_six"); got != 6 {
		t.Errorf("six elements: %d drawn", got)
	}
	if got := drawn("bk_seven"); got != 5 {
		t.Errorf("seven elements: %d drawn before expanding", got)
	}
	var first *ew
	for key, w := range me.ws {
		if key == "bk_seven\x00seven_1" {
			first = w
		}
	}
	slots := e.frame()
	for _, s := range slots {
		if s.block == "bk_seven" {
			e.click(s.at.Add(first.moreAt).Add(image.Pt(10, 10)))
		}
	}
	if !me.more["bk_seven"] {
		t.Error("+ 2 more didn't expand")
	}

	// the app replacing the message (response_url replace_original /
	// chat.update arrive as message_changed) resets element state
	e.show("buttons")
	me = e.message()
	if len(me.errs) == 0 {
		t.Fatal("expected the earlier error on the buttons message")
	}
	m := e.msgs["buttons"]
	m.Blocks = json.RawMessage(strings.Replace(string(m.Blocks), "Default", "Done", 1))
	e.msgs["buttons"] = m
	me2 := e.message()
	if me2 != me || len(me2.errs) != 0 || len(me2.busy) != 0 {
		t.Errorf("replaced message kept errors %v", me2.errs)
	}
}

// Every element closed and open, light and dark (BK_ELEMENTS_SHOTS_DIR).
func TestGalleryElementShots(t *testing.T) {
	if os.Getenv("BK_ELEMENTS_SHOTS_DIR") == "" {
		t.Skip("BK_ELEMENTS_SHOTS_DIR not set")
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		e := newBKEnv(t, dark, true)
		shot := func(name string) { e.shot(mode + "-" + name) }
		openShot := func(name, bid, aid string, after func()) {
			t.Helper()
			e.closeOverlays()
			e.clickEl(bid, aid)
			if after != nil {
				after()
			}
			e.settle(150 * time.Millisecond)
			shot(name)
			e.closeOverlays()
		}
		for _, n := range fakeslack.GalleryElementNames {
			e.show(n)
			e.settle(200 * time.Millisecond)
			shot(n + "-closed")
		}
		e.show("buttons")
		e.hover(center(func() image.Rectangle { _, r := e.widget("bk_buttons", "btn_primary"); return r }()))
		shot("buttons-hover")
		openShot("buttons-confirm-open", "bk_buttons", "btn_confirm", nil)
		e.settle(1100 * time.Millisecond)
		e.clickEl("bk_buttons_state", "btn_slow")
		e.settle(100 * time.Millisecond)
		shot("buttons-loading")
		me := e.message()
		e.until("slow settles", 5*time.Second, func() bool { return !me.loading("bk_buttons_state") })
		e.settle(1100 * time.Millisecond)
		e.clickEl("bk_buttons_state", "btn_fail")
		e.until("error", 5*time.Second, func() bool { _, ok := me.errs["bk_buttons_state"]; return ok })
		for _, s := range e.frame() {
			if s.block == "bk_buttons_state" {
				// hover the warning icon for its tooltip
				e.hover(s.at.Add(me.statusAt["bk_buttons_state"]).Add(image.Pt(8, 14)))
			}
		}
		e.settle(600 * time.Millisecond)
		shot("buttons-error")
		e.settle(1100 * time.Millisecond)
		e.clickEl("bk_buttons_state", "btn_disabled")
		e.until("disabled", 5*time.Second, func() bool { return me.errs["bk_buttons_state"] == "action_disabled" })
		shot("buttons-action-disabled")

		e.show("overflow")
		openShot("overflow-open", "bk_overflow_sec", "overflow_acc", nil)
		e.show("selects")
		openShot("static-select-open", "bk_selects", "static_groups", nil)
		openShot("external-select-open", "bk_selects", "external_any", func() {
			e.typeText("an")
			e.until("items", 5*time.Second, func() bool { sm, ok := topOverlay[*selectMenu](e); return ok && len(sm.items) > 0 && !sm.busy })
		})
		openShot("external-select-min-query", "bk_selects", "external_groups", nil)
		e.show("people")
		openShot("users-select-open", "bk_people", "users_one", func() {
			e.until("people", 5*time.Second, func() bool { sm, ok := topOverlay[*selectMenu](e); return ok && len(sm.items) > 0 && !sm.busy })
		})
		openShot("conversations-select-open", "bk_people", "convs_current", func() { e.settle(200 * time.Millisecond) })
		openShot("channels-select-open", "bk_people", "channels_one", func() { e.settle(200 * time.Millisecond) })
		e.show("multi")
		openShot("multi-static-dialog", "bk_multi", "multi_static", func() {
			w, _ := e.widget("bk_multi", "multi_static")
			// the field sits at the dialog body's origin (popups.Modal:
			// centred, 28px side padding, 70px header, 76px footer)
			fh := w.multiHeight(e.context(), 520-56)
			top := (e.size.Y-(70+fh+76))/2 + 70
			openSelect(w, image.Rect((e.size.X-520)/2+28, top, (e.size.X+520)/2-28, top+fh))
			e.frame()
			e.key(key.NameDownArrow)
			e.key(key.NameReturn)
			e.key(key.NameDownArrow)
			e.key(key.NameReturn)
		})
		openShot("multi-users-dialog", "bk_multi", "multi_users", nil)
		e.show("pickers")
		openShot("datepicker-open", "bk_pickers", "date_one", nil)
		openShot("timepicker-open", "bk_pickers", "time_one", nil)
		openShot("datetimepicker-open", "bk_pickers", "datetime_one", nil)
		e.show("options")
		w, r := e.widget("bk_radios", "radios")
		e.click(image.Pt(r.Min.X+8, r.Min.Y+8))
		_ = w
		w, r = e.widget("bk_checks", "checks")
		e.click(image.Pt(r.Min.X+8, r.Min.Y+8))
		e.settle(200 * time.Millisecond)
		shot("options-toggled")
		e.show("inputs")
		e.clickEl("bk_in_enter", "in_enter")
		e.typeText("Printer on fire")
		shot("inputs-focused")
		e.show("workflow")
		_, r = e.widget("bk_workflow", "icon_trash")
		e.hover(center(r))
		e.settle(600 * time.Millisecond)
		shot("icon-button-tooltip")
		e.show("more")
		me = e.message()
		for _, s := range e.frame() {
			if s.block == "bk_seven" {
				w := me.ws["bk_seven\x00seven_1"]
				e.click(s.at.Add(w.moreAt).Add(image.Pt(10, 10)))
			}
		}
		shot("more-expanded")
	}
}

// Payload builders against Slack's element components, element by
// element (no network).
func TestSlackActionShapes(t *testing.T) {
	el := func(js string) *Element {
		var e Element
		if err := json.Unmarshal([]byte(js), &e); err != nil {
			t.Fatal(err)
		}
		return &e
	}
	val := func(e *Element, f func(v *Value)) *Value {
		v := initialValue(e)
		if f != nil {
			f(v)
		}
		return v
	}
	for _, c := range []struct {
		name string
		el   string
		set  func(v *Value)
		want string
	}{
		{"button with url and a11y label", `{"type":"button","action_id":"a","text":{"type":"plain_text","text":"Go"},"url":"https://x.test","accessibility_label":"Go there"}`, nil,
			`{"type":"button","block_id":"b","action_id":"a","text":{"type":"plain_text","text":"Go"}}`},
		{"users_select drops the placeholder", `{"type":"users_select","action_id":"a","placeholder":{"type":"plain_text","text":"Who"}}`, func(v *Value) { v.SelectedUser = "U1" },
			`{"type":"users_select","block_id":"b","action_id":"a","selected_user":"U1"}`},
		{"conversations_select", `{"type":"conversations_select","action_id":"a","initial_conversation":"C1","response_url_enabled":true,"placeholder":{"type":"plain_text","text":"x"}}`, func(v *Value) { v.SelectedConv = "C2" },
			`{"type":"conversations_select","block_id":"b","action_id":"a","selected_conversation":"C2","initial_conversation":"C1","response_url_enabled":true}`},
		{"multi_conversations initial", `{"type":"multi_conversations_select","action_id":"a","initial_conversations":["C1"]}`, func(v *Value) { v.SelectedConvs = []string{"C1", "C2"} },
			`{"type":"multi_conversations_select","block_id":"b","action_id":"a","selected_conversations":["C1","C2"],"initial_conversations":["C1"]}`},
		{"multi_channels initial", `{"type":"multi_channels_select","action_id":"a","initial_channels":["C1"]}`, nil,
			`{"type":"multi_channels_select","block_id":"b","action_id":"a","selected_channels":["C1"],"initial_channels":["C1"]}`},
		{"static_select cleared", `{"type":"static_select","action_id":"a","options":[{"text":{"type":"plain_text","text":"A"},"value":"a"}]}`, nil,
			`{"type":"static_select","block_id":"b","action_id":"a","selected_option":null}`},
		{"datepicker no placeholder", `{"type":"datepicker","action_id":"a","placeholder":{"type":"plain_text","text":"When"}}`, func(v *Value) { v.SelectedDate = "2026-01-02" },
			`{"type":"datepicker","block_id":"b","action_id":"a","selected_date":"2026-01-02"}`},
		{"timepicker timezone", `{"type":"timepicker","action_id":"a","timezone":"Asia/Tokyo"}`, func(v *Value) { v.SelectedTime = "09:05" },
			`{"type":"timepicker","block_id":"b","action_id":"a","selected_time":"09:05","timezone":"Asia/Tokyo"}`},
		{"datetimepicker empty", `{"type":"datetimepicker","action_id":"a"}`, nil,
			`{"type":"datetimepicker","block_id":"b","action_id":"a","selected_date_time":null}`},
		{"checkboxes ignore initial_options", `{"type":"checkboxes","action_id":"a","options":[{"text":{"type":"plain_text","text":"A"},"value":"a"}],"initial_options":[{"text":{"type":"plain_text","text":"A"},"value":"a"}]}`, nil,
			`{"type":"checkboxes","block_id":"b","action_id":"a","selected_options":[{"text":{"type":"plain_text","text":"A"},"value":"a"}]}`},
		{"plain_text_input trimmed", `{"type":"plain_text_input","action_id":"a","initial_value":"hi"}`, func(v *Value) { v.Text = "  hello  " },
			`{"type":"plain_text_input","block_id":"b","action_id":"a","value":"hello","initial_value":"hi"}`},
		{"plain_text_input blank is null", `{"type":"plain_text_input","action_id":"a"}`, func(v *Value) { v.Text = "   " },
			`{"type":"plain_text_input","block_id":"b","action_id":"a","value":null}`},
		{"number_input normalized", `{"type":"number_input","action_id":"a","is_decimal_allowed":true}`, func(v *Value) { v.Text = "-0012.300" },
			`{"type":"number_input","block_id":"b","action_id":"a","value":"-12.3"}`},
		{"icon_button", `{"type":"icon_button","action_id":"a","icon":"trash","text":{"type":"plain_text","text":"Delete"},"value":"v"}`, nil,
			`{"type":"icon_button","block_id":"b","action_id":"a","icon":"trash","text":{"type":"plain_text","text":"Delete"},"value":"v"}`},
		{"file_input", `{"type":"file_input","action_id":"a"}`, nil,
			`{"type":"file_input","block_id":"b","action_id":"a","files":[]}`},
	} {
		e := el(c.el)
		var v *Value
		if stateful(e.Type) {
			v = val(e, c.set)
		}
		got := actionPayload("b", e, v)
		var g, w any
		_ = json.Unmarshal(got, &g)
		if err := json.Unmarshal([]byte(c.want), &w); err != nil {
			t.Fatalf("%s: bad want: %v", c.name, err)
		}
		if !reflect.DeepEqual(g, w) {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
	// overflow keeps only text and value
	ov := el(`{"type":"overflow","action_id":"o","options":[{"text":{"type":"plain_text","text":"Web"},"value":"w","url":"https://x.test","description":{"type":"plain_text","text":"d"}}]}`)
	if got := string(overflowPayload("b", ov, &ov.Options[0])); got != `{"action_id":"o","block_id":"b","selected_option":{"text":{"type":"plain_text","text":"Web"},"value":"w"},"type":"overflow"}` {
		t.Errorf("overflow: %s", got)
	}
	// Slack's state value is the trimmed text too
	pv := &Value{Type: "plain_text_input", Text: " x "}
	if got, _ := json.Marshal(pv.stateJSON()); string(got) != `{"type":"plain_text_input","value":"x"}` {
		t.Errorf("state value %s", got)
	}
	if shortDateLabel("2026-03-04", time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local)) != "03/04/2026" ||
		shortDateLabel("2026-03-04", time.Date(2026, 3, 4, 15, 0, 0, 0, time.Local)) != "Today" {
		t.Error("message datepicker label")
	}
	b := &Block{Type: "input", DispatchAction: true, Element: el(`{"type":"plain_text_input","action_id":"a","dispatch_action_config":{"trigger_actions_on":["on_enter_pressed"]}}`)}
	if !enterHint(b) {
		t.Error("enter hint")
	}
	b.Element = el(`{"type":"plain_text_input","action_id":"a","dispatch_action_config":{"trigger_actions_on":["on_enter_pressed","on_character_entered"]}}`)
	if enterHint(b) {
		t.Error("enter hint with on_character_entered")
	}
}
