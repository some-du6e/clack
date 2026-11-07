// Package blockkit makes Slack apps interactive in Clack, 1:1 with
// Slack's desktop client: Block Kit elements in messages (buttons, every
// select, overflow menus, date/time pickers, checkboxes, radio buttons,
// confirm dialogs, loading and error states), legacy attachment actions,
// modals (views.open/push/update with every input type, validation
// errors, response_action and private_metadata), App Home (Home /
// Messages / About tabs of app DMs), the global and message shortcuts
// menus, workflow buttons and ephemeral "Only visible to you" messages.
//
// The service side is core.Interactive (internal/core/interactive.go).
// Here are the widgets and their glue:
//
//   - message.go: richview.Elements for a message body, so richview's
//     cached layout reserves a slot per run of elements and this package
//     draws live widgets in it every frame.
//   - elements.go, menus.go: the element widgets and their dropdowns.
//   - surface.go: a full Block Kit surface (modal body, App Home).
//   - modal.go: the view stack overlay.
//   - apphome.go, shortcuts.go: App DM tabs and the shortcuts menus.
//
// Slack's thunks route through Taut when a plugin patches them
// (docs/PLUGINS-taut.md): sendActions (blocks.actions), executeAppAction
// (shortcuts), submitView (modals) and blocksFormatFetcher. Unpatched,
// each is one atomic load.
//
// Scraping policy: views, values and ephemeral messages are memory only.
package blockkit

import (
	"context"
	"image"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/layout"
	"gioui.org/text"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Host is what blockkit needs from the ui root: the shared Host of
// docs/NATIVE.md (one *ui.App satisfies it).
type Host interface {
	popups.Host
}

// PostHost is optional: Post runs f on the UI goroutine (the ui root
// has it). Without it, results wait for the next blockkit Layout.
type PostHost interface{ Post(f func()) }

// PointerHost is optional: the last pointer position in window
// coordinates, for anchoring dropdowns.
type PointerHost interface{ PointerPos() image.Point }

// ---- async ----

var (
	asyncMu sync.Mutex
	queue   []func()
)

// post runs f on the UI goroutine.
func post(h Host, f func()) {
	if ph, ok := h.(PostHost); ok {
		ph.Post(f)
		return
	}
	asyncMu.Lock()
	queue = append(queue, f)
	asyncMu.Unlock()
	h.Invalidate()
}

// drain runs queued callbacks (every blockkit Layout calls it).
func drain() {
	asyncMu.Lock()
	q := queue
	queue = nil
	asyncMu.Unlock()
	for _, f := range q {
		f()
	}
}

// Pump runs queued blockkit callbacks; the ui root may call it per frame.
func Pump() { drain() }

// run does work off the UI goroutine (timeout d) and then done on it.
func run[T any](h Host, d time.Duration, work func(ctx context.Context) (T, error), done func(T, error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), d)
		v, err := work(ctx)
		cancel()
		post(h, func() {
			if done != nil {
				done(v, err)
			}
		})
	}()
}

// ---- Taut ----

var tautRT atomic.Pointer[taut.Taut]

// SetTaut attaches the plugin runtime (nil detaches) and registers the
// natives of the thunks blockkit owns, so plugins dispatching them reach
// Clack's implementation.
func SetTaut(t *taut.Taut) {
	tautRT.Store(t)
	if t == nil {
		return
	}
	registerThunks(t)
}

func tautFor(thunk string) *taut.Taut {
	t := tautRT.Load()
	if t == nil || !t.ThunkPatched(thunk) {
		return nil
	}
	return t
}

// ---- shared state ----

// app is the per-Host state: the event pump and the modal stack.
type app struct {
	h     Host
	ix    *core.Interactive
	stack *Stack
	stop  func()
}

var (
	appsMu sync.Mutex
	apps   = map[Host]*app{}
)

// Start connects blockkit to the Host's core: realtime views (modals,
// App Home updates, legacy dialogs) start arriving. Call it once core
// is booted; calling it again is a no-op. Stop with the returned func.
func Start(h Host) func() {
	appsMu.Lock()
	if a, ok := apps[h]; ok {
		appsMu.Unlock()
		return a.stop
	}
	c := h.Core()
	if c == nil {
		appsMu.Unlock()
		return func() {}
	}
	a := &app{h: h, ix: c.Interactive()}
	apps[h] = a
	appsMu.Unlock()
	ch, cancel := a.ix.Subscribe()
	var once sync.Once
	a.stop = func() {
		once.Do(func() {
			cancel()
			appsMu.Lock()
			delete(apps, h)
			appsMu.Unlock()
		})
	}
	go func() {
		for ev := range ch {
			ev := ev
			post(h, func() { a.onEvent(ev) })
		}
	}()
	return a.stop
}

func appFor(h Host) *app {
	appsMu.Lock()
	a := apps[h]
	appsMu.Unlock()
	if a == nil {
		Start(h)
		appsMu.Lock()
		a = apps[h]
		appsMu.Unlock()
	}
	return a
}

// onEvent runs on the UI goroutine.
func (a *app) onEvent(ev core.ViewEvent) {
	switch ev.Kind {
	case "opened":
		a.openView(ev.View)
	case "updated":
		if a.stack != nil && !a.stack.Dismissed() {
			a.stack.update(ev.View)
		}
	case "home":
		homeUpdated(ev.Channel, ev.View)
	case "dialog":
		openDialog(a.h, ev.Dialog)
	case "ephemeral":
		ephemeralGen.Add(1)
	case "actions":
		shortcutsGen.Add(1)
	}
	a.h.Invalidate()
}

// openView shows a new modal, or pushes onto the open stack when the
// view says it follows the top one.
func (a *app) openView(v *core.View) {
	if v == nil {
		return
	}
	if a.stack != nil && !a.stack.Dismissed() {
		if v.PreviousViewID != "" && a.stack.has(v.PreviousViewID) || v.RootViewID == a.stack.root() {
			a.stack.push(v)
			return
		}
		if a.stack.has(v.ID) {
			a.stack.update(v)
			return
		}
	}
	a.stack = newStack(a.h, v)
	a.h.ShowOverlay(a.stack)
}

// ephemeralGen bumps when a message became known as ephemeral (rows
// repaint their "Only visible to you" label).
var ephemeralGen atomic.Uint32

// Ephemeral reports whether core knows the message as ephemeral.
func Ephemeral(h Host, ch, ts string) bool {
	c := h.Core()
	if c == nil {
		return false
	}
	return c.Interactive().Ephemeral(ch, ts)
}

// ---- richview style for surfaces ----

type styleEntry struct {
	st   *richview.Style
	dark bool
	init bool
}

var (
	stylesMu sync.Mutex
	styles   = map[*text.Shaper]*styleEntry{}
)

// styleFor is a richview.Style like the message list's, for text inside
// modals and App Home.
func styleFor(th *theme.Theme, images *imgcache.Cache) *richview.Style {
	stylesMu.Lock()
	defer stylesMu.Unlock()
	e, ok := styles[th.Shaper]
	if !ok {
		var imgs richview.Images
		if images != nil {
			imgs = images
		}
		e = &styleEntry{st: richview.NewStyle(th.Shaper, imgs, richview.LightPalette())}
		e.st.Face = theme.Typeface
		e.st.EmojiURL = th.EmojiURL
		styles[th.Shaper] = e
	}
	if th.Dark != e.dark || !e.init {
		e.dark, e.init = th.Dark, true
		e.st.Embolden = theme.BodyEmboldenFor(theme.LightPalette.Fg)
		e.st.LightTextFade = 0
		if th.Dark {
			e.st.SetPalette(richview.DarkPalette())
			e.st.Embolden = theme.BodyEmboldenFor(theme.DarkPalette.Fg)
			e.st.LightTextFade = 0.235
		} else {
			e.st.SetPalette(richview.LightPalette())
		}
	}
	return e.st
}

// pointer returns the pointer position (window coords) or fallback.
func pointerAt(h Host, fallback image.Point) image.Point {
	if ph, ok := h.(PointerHost); ok {
		return ph.PointerPos()
	}
	return fallback
}

type (
	C = layout.Context
	D = layout.Dimensions
)
