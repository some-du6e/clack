package composer

import (
	"fmt"
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// typingShaped is shaped like Better Typing Indicator, the plugin Karim's
// composer runs under (α.89): MessagePaneInput and ThreadTypingIndicator
// wrapped in a conversation context, a TypingNames patch that reads it,
// and a stylesheet (api.setStyle) for its typing line. With it the whole
// composer is drawn from the plugin's tree (MessagePaneInput's root), its
// parts as natives in that tree's slots.
const typingShaped = `(() => {
const TautPlugin = globalThis.TautPlugin
class TypingShaped extends TautPlugin {
  static id = 'TypingShaped'
  static pluginName = 'Typing Shaped'
  static description = 'a Better Typing Indicator shaped plugin (test)'
  static authors = ['test']
  static defaultConfig = { enabled: true }
  start() {
    const R = globalThis.React
    const Conv = R.createContext(null)
    this.api.setStyle('.taut-typing-indicator { display: block; flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .taut-typing-indicator__member { color: inherit; }')
    const wrap = (Original) => (props) => {
      const v = R.useMemo(() => ({ channelId: props.channelId, threadTs: props.threadTs }), [props.channelId, props.threadTs])
      return R.createElement(Conv.Provider, { value: v }, R.createElement(Original, { ...props }))
    }
    this.api.patchComponent('MessagePaneInput', wrap)
    this.api.patchComponent('ThreadTypingIndicator', wrap)
    this.api.patchComponent('TypingNames', (Original) => (props) => {
      const conv = R.useContext(Conv)
      if (!conv) return R.createElement(Original, props)
      return R.createElement('span', { className: 'taut-typing-indicator' }, R.createElement(Original, props))
    })
  }
  stop() {}
}
return TypingShaped
})()`

// plugRig is fmtRig over a composer drawn from typingShaped's tree.
func plugRig(t *testing.T) (*fmtRig, *slickEnv) {
	t.Helper()
	e := startTautCode(t, []byte(typingShaped), nil, nil)
	e.until(10*time.Second, "the plugin's patches", nil, func() bool {
		return slackc.Patched("MessagePaneInput") && slackc.Patched("TypingNames")
	})
	g := newFmtRigOn(t, 900, true, &ovHost{slickHost: e.h})
	e.why = func() string { return g.state(crStrike) }
	e.until(10*time.Second, "the composer through the plugin's tree", g.frame, func() bool {
		_, ok := e.treeWith(`"n":"MessageInputRoadblock"`)
		return ok && g.c.ui.f.boxOK
	})
	g.settle()
	if g.c.BoxVanished() {
		t.Fatal("the composer vanished under the plugin")
	}
	if !g.c.ed.Focused(layout.Context{Source: g.r.Source()}) {
		g.c.Focus()
		g.settle()
	}
	return g, e
}

// state is what a failure logs: the button, the selection and the box.
func (g *fmtRig) state(id crBtn) string {
	a, b := g.c.ed.Selection()
	return fmt.Sprintf("btn %v rect %v (top %d) pressed %v hovered %v shown %b; sel [%d,%d); focused %v; box %v ok %v; styles %v",
		id, g.c.ui.rect[id], g.c.top, g.c.ui.btn[id].Pressed(), g.c.ui.btn[id].Hovered(), g.c.ui.shown,
		a, b, g.c.ed.Focused(layout.Context{Source: g.r.Source()}), g.c.ui.f.box, g.c.ui.f.boxOK, edStyles(g.c.ed.Doc()))
}

var fmtPlugButtons = append(fmtKeys[:len(fmtKeys):len(fmtKeys)], struct {
	name  string
	key   key.Name
	mods  key.Modifiers
	style Style
	btn   crBtn
}{"underline", "U", key.ModShortcut, Underline, crUnderline})

// Karim (α.89, macOS, under Better Typing Indicator): "I can't click the
// freaking strikethrough button while selecting". With a plugin patching
// MessagePaneInput the composer is drawn from the plugin's tree, and a
// hover or a press makes slackc lay it out again, measuring the chrome
// with the box's draw reused; the chrome reset the buttons that measure
// pass hadn't laid out, the hovered and pressed one among them, so the
// release came to a button that was never pressed. A mouse click on each
// formatting button (press and release in frames of their own) formats
// the selected word, keeps it selected, and a second click takes the
// format off, as without plugins and in Slack.
func TestPluginComposerFormatButtonsOnSelection(t *testing.T) {
	g, _ := plugRig(t)
	mouseFormatButtons(t, g, false)
}

// The same while someone types: the plugin's typing line (its own span
// around TypingNames) is in the composer's tree.
func TestPluginComposerFormatButtonsWhileTyping(t *testing.T) {
	g, e := plugRig(t)
	hub := typingHub(e.app)
	hub.mu.Lock()
	hub.m[draftKey(tChGen, "")] = map[string]typer{"UALICE": {name: "alice", until: time.Now().Add(time.Hour)}}
	hub.mu.Unlock()
	e.until(10*time.Second, "the plugin's typing line", g.frame, func() bool {
		_, ok := e.treeWith(`taut-typing-indicator`)
		return ok
	})
	g.settle()
	mouseFormatButtons(t, g, false)
}

// The word selected with a mouse drag first, as Karim did.
func TestPluginComposerFormatButtonsAfterDragSelect(t *testing.T) {
	g, _ := plugRig(t)
	mouseFormatButtons(t, g, true)
}

// The same mouse clicks without plugins.
func TestFormatButtonsMouseOnSelection(t *testing.T) {
	g := newFmtRig(t, 900, true)
	mouseFormatButtons(t, g, false)
	mouseFormatButtons(t, g, true)
}

// A hovered button that isn't drawn for a frame (the toolbar hidden) loses
// its hover, so its tooltip doesn't pop when it comes back: the reset the
// chrome now makes once per frame (chrome.endFrame).
func TestFormatButtonHoverResetWhenUndrawn(t *testing.T) {
	g := newFmtRig(t, 900, true)
	r := g.c.ui.rect[crBold].Add(image.Pt(0, g.c.top))
	g.mouseMove(r.Min.Add(r.Size().Div(2)))
	if !g.c.ui.btn[crBold].Hovered() {
		t.Fatalf("Bold isn't hovered (%s)", g.state(crBold))
	}
	formattingWrite.Lock()
	formattingWrite.on = false
	formattingWrite.Unlock()
	showFormatting = false
	g.frame()
	if g.c.ui.btn[crBold].Hovered() || !g.c.ui.rect[crBold].Empty() {
		t.Errorf("Bold kept its hover or rect with the toolbar hidden (%s)", g.state(crBold))
	}
}

// caretAt is where the caret at i is drawn, in composer coordinates.
func (g *fmtRig) caretAt(i int) image.Point {
	g.c.ed.SetSelection(i, i)
	g.settle()
	r := g.c.ed.CaretRect().Add(g.c.ui.InputOffset()).Add(image.Pt(0, g.c.top))
	return image.Pt(r.Min.X, (r.Min.Y+r.Max.Y)/2)
}

// dragSelect selects [lo, hi) with a mouse drag.
func (g *fmtRig) dragSelect(lo, hi int) {
	a, b := g.caretAt(lo), g.caretAt(hi)
	g.c.ed.SetSelection(0, 0)
	g.settle()
	g.mouseMove(a)
	g.mouseDown(a)
	for i := 1; i <= 4; i++ {
		g.mouse(pointer.Move, pointer.ButtonPrimary, a.Add(b.Sub(a).Mul(i).Div(4)))
		g.frame()
	}
	g.settle()
	g.mouseUp(b)
}

// mouseFormatButtons clicks each formatting button twice on "this" in
// "make this formatted" (selected with a drag when drag is set).
func mouseFormatButtons(t *testing.T, g *fmtRig, drag bool) {
	t.Helper()
	if g.c.ui.fmtHidden != 0 {
		t.Fatalf("%d buttons folded at 900px", g.c.ui.fmtHidden)
	}
	for _, k := range fmtPlugButtons {
		g.draft("make this formatted", 5, 9)
		if drag {
			g.dragSelect(5, 9)
			if a, b := g.c.ed.Selection(); min(a, b) != 5 || max(a, b) != 9 {
				t.Fatalf("%s: the drag selected [%d,%d), want [5,9)", k.name, a, b)
			}
		}
		for _, on := range []bool{true, false} {
			r := g.c.ui.rect[k.btn].Add(image.Pt(0, g.c.top))
			if r.Empty() {
				t.Fatalf("%s: no button rect (%s)", k.name, g.state(k.btn))
			}
			p := r.Min.Add(r.Size().Div(2))
			g.mouseMove(p)
			g.mouseDown(p)
			if !g.c.ui.btn[k.btn].Pressed() {
				t.Errorf("%s: the press on the button didn't hold (%s)", k.name, g.state(k.btn))
			}
			if a, b := g.c.ed.Selection(); min(a, b) != 5 || max(a, b) != 9 {
				t.Errorf("%s: the press moved the selection to [%d,%d)", k.name, a, b)
			}
			g.mouseUp(p)
			g.expect(fmt.Sprintf("%s button, drag %v (%s)", k.name, drag, g.state(k.btn)), k.style, 5, 9, on)
		}
	}
}

// The same with the shortcuts (⌘B, ⌘I, ⌘⇧X, ⌘⇧C) in the plugin's tree.
func TestPluginComposerFormatKeysOnSelection(t *testing.T) {
	g, _ := plugRig(t)
	for _, k := range fmtKeys {
		g.draft("make this formatted", 5, 9)
		for _, on := range []bool{true, false} {
			g.r.Queue(key.Event{Name: k.key, Modifiers: k.mods, State: key.Press})
			g.settle()
			g.expect(k.name+" key", k.style, 5, 9, on)
		}
	}
}

// Typed ~burned~ turns into struck-through "burned" as it is typed
// (SlackMarkdown's strike rule), in the plugin's tree too: both in one go
// and with the tildes typed around a word already there, which is how
// Karim's draft had them.
func TestPluginComposerTypedTildesStrike(t *testing.T) {
	g, _ := plugRig(t)
	for _, c := range []struct {
		name  string
		start string
		at    []int // where each tilde is typed, in order
		typed string
	}{
		{"typed in order", "", nil, "~burned~ slack"},
		{"tildes around a word", "burned slack", []int{0, 7}, ""},
	} {
		g.draft(c.start, len([]rune(c.start)), len([]rune(c.start)))
		for _, r := range c.typed {
			g.typeRune(string(r))
		}
		for _, at := range c.at {
			g.c.ed.SetSelection(at, at)
			g.settle()
			g.typeRune("~")
		}
		d := g.c.ed.Doc()
		if got := d.Text(); got != "burned slack" {
			t.Errorf("%s: text %q, want the tildes gone (styles %v)", c.name, got, edStyles(d))
			continue
		}
		if !d.HasStyle(0, 6, Strike) || d.HasStyle(6, d.Len(), Strike) {
			t.Errorf("%s: strike on %v, want on \"burned\" only", c.name, edStyles(d))
		}
	}
}

// mouseMove, mouseDown and mouseUp are a mouse click's events, each in
// frames of its own as from a real mouse (a queued press and release are
// handled in one Clickable update and never see a frame between them).
func (g *fmtRig) mouseMove(p image.Point) {
	g.mouse(pointer.Move, 0, p)
	g.settle()
}

func (g *fmtRig) mouseDown(p image.Point) {
	g.mouse(pointer.Press, pointer.ButtonPrimary, p)
	g.settle()
}

func (g *fmtRig) mouseUp(p image.Point) {
	g.mouse(pointer.Release, 0, p)
	g.settle()
}

// mouse queues one mouse event, a second after the last one (events at
// one time count as a double click).
func (g *fmtRig) mouse(k pointer.Kind, b pointer.Buttons, p image.Point) {
	g.ptrT += time.Second
	g.r.Queue(pointer.Event{Kind: k, Source: pointer.Mouse, Buttons: b, Position: ptrAt(p), Time: g.ptrT})
}

func ptrAt(p image.Point) f32.Point { return f32.Pt(float32(p.X)+.5, float32(p.Y)+.5) }

// typeRune types s at the caret the way the window does: an EditEvent,
// then the selection after it.
func (g *fmtRig) typeRune(s string) {
	a, b := g.c.ed.Selection()
	lo, hi := min(a, b), max(a, b)
	n := lo + len([]rune(s))
	g.r.Queue(key.EditEvent{Range: key.Range{Start: lo, End: hi}, Text: s}, key.SelectionEvent{Start: n, End: n})
	g.settle()
}
