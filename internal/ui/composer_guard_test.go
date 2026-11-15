package ui

import (
	"fmt"
	"image"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// "my composer went byebye" (Karim, 2026-11-15, α.85 on macOS): in a
// channel he can post in, the composer was gone from under the message
// list until Clack restarted; the inspect report listed MessagePaneInput
// among the patched components. A plugin root that an uncaught JS error
// emptied sent the tree "[]", the box laid out to nothing and the list
// took its place. These drive the whole client into that, and into a
// composer that lays out to nothing on its own, and check the box stays
// under the list.

// guardBridge is the plugin side: it records renders, nothing else.
type guardBridge struct {
	mu      sync.Mutex
	renders map[string]uint64 // component → its latest root
}

func (b *guardBridge) Render(root uint64, name string, props []byte) {
	b.mu.Lock()
	b.renders[name] = root
	b.mu.Unlock()
}
func (b *guardBridge) RenderFn(root uint64, fn int64, args []byte)          {}
func (b *guardBridge) Unmount(root uint64)                                  {}
func (b *guardBridge) Dispatch(target int64, event []byte)                  {}
func (b *guardBridge) Call(fn int64, args []byte, done func([]byte, error)) {}
func (b *guardBridge) SetRects(rects map[int64][4]float32)                  {}
func (b *guardBridge) SetViewport(w, h, dpr float32, dark bool)             {}

func (b *guardBridge) root(name string) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.renders[name]
}

// zeroComposer is a composer whose state went bad: it lays out to nothing.
type zeroComposer struct{}

func (zeroComposer) Layout(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }
func (zeroComposer) SetOrigin(image.Point)                       {}
func (zeroComposer) SetOnEditLast(func())                        {}
func (zeroComposer) Done() bool                                  { return false }
func (zeroComposer) Focus()                                      {}
func (zeroComposer) Close()                                      {}

func TestComposerStaysInChannel(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	pe := startEnv(t, fs, t.TempDir())
	pe.waitReady()
	ch := fs.ChannelIDs()[1]
	pe.openChannel(ch)
	pe.until("history", 10*time.Second, func() bool { return pe.loadedMessages(ch) > 0 })
	pe.settle(300 * time.Millisecond)

	var mu sync.Mutex
	var vanish, shell []string
	composer.OnVanish = func(l string) { mu.Lock(); vanish = append(vanish, l); mu.Unlock() }
	guardLogf = func(f string, args ...any) { mu.Lock(); shell = append(shell, fmt.Sprintf(f, args...)); mu.Unlock() }
	t.Cleanup(func() { composer.OnVanish, guardLogf = nil, log.Printf })
	logged := func(lines *[]string, s string) bool {
		mu.Lock()
		defer mu.Unlock()
		for _, l := range *lines {
			if strings.Contains(l, s) {
				return true
			}
		}
		return false
	}

	cv := pe.a.convos[ch+"/"]
	if cv == nil || cv.comp == nil {
		t.Fatal("no conversation for the channel")
	}
	// the composer sits at the bottom of the panel, under the list
	atBottom := func(what string) {
		t.Helper()
		r := cv.compRect
		if r.Dy() < 60 {
			t.Fatalf("%s: the composer is %v tall (rect %v): gone from the channel", what, r.Dy(), r)
		}
		if r.Max.Y < pe.size.Y-16 || r.Max.Y > pe.size.Y { // the panel's bottom inset
			t.Errorf("%s: the composer ends at y %d, the window at %d", what, r.Max.Y, pe.size.Y)
		}
	}
	atBottom("unpatched")
	h0 := cv.compRect.Dy()

	t.Run("a plugin root that comes back empty", func(t *testing.T) {
		fb := &guardBridge{renders: map[string]uint64{}}
		slackc.SetBridge(fb)
		slackc.SetPatched([]string{"MessagePaneInput"})
		defer func() { slackc.Reset(); slackc.SetBridge(nil) }()
		pe.until("the composer's plugin root", 5*time.Second, func() bool { return fb.root("MessagePaneInput") != 0 })
		root := fb.root("MessagePaneInput")
		if err := slackc.Deliver(root, []byte("[]")); err != nil {
			t.Fatal(err)
		}
		pe.settle(200 * time.Millisecond)
		atBottom("the plugin's tree empty")
		if d := cv.compRect.Dy(); d < h0-2 || d > h0+2 {
			t.Errorf("the composer drawn without plugins is %dpx tall, %dpx before", d, h0)
		}
		if !logged(&vanish, "drew no message box") || !logged(&vanish, "MessagePaneInput: an empty tree") {
			t.Errorf("no guard line: %q", vanish)
		}
	})
	pe.settle(100 * time.Millisecond)
	atBottom("no plugin again")

	t.Run("a composer that lays out to nothing", func(t *testing.T) {
		cv.comp.Close()
		cv.comp = zeroComposer{}
		pe.frame()
		if _, ok := cv.comp.(composerBox); !ok {
			t.Fatalf("the composer is still a %T", cv.comp)
		}
		atBottom("after the zero-height frame")
		pe.settle(100 * time.Millisecond)
		atBottom("frames later")
		if !logged(&shell, "laid out to no height") || !logged(&shell, "putting a new one in its place") {
			t.Errorf("no shell line: %q", shell)
		}
	})
}
