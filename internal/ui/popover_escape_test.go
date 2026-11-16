package ui

import (
	"slices"
	"sync"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// escBridge is the plugin side: it records renders and the JS functions
// Go calls.
type escBridge struct {
	mu      sync.Mutex
	renders map[string]uint64 // component → its latest root
	calls   []int64
}

func (b *escBridge) Render(root uint64, name string, props []byte) {
	b.mu.Lock()
	b.renders[name] = root
	b.mu.Unlock()
}
func (b *escBridge) RenderFn(root uint64, fn int64, args []byte) {}
func (b *escBridge) Unmount(root uint64)                         {}
func (b *escBridge) Dispatch(target int64, event []byte)         {}
func (b *escBridge) Call(fn int64, args []byte, done func([]byte, error)) {
	b.mu.Lock()
	b.calls = append(b.calls, fn)
	b.mu.Unlock()
}
func (b *escBridge) SetRects(rects map[int64][4]float32)      {}
func (b *escBridge) SetViewport(w, h, dpr float32, dark bool) {}

func (b *escBridge) root(name string) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.renders[name]
}

func (b *escBridge) called(fn int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Contains(b.calls, fn)
}

// In the main window Escape is an open plugin Popover's, as in Slack
// (react-modal closes on an Escape from anything inside it and stops it
// there): the shell's "Close / mark as read", read first in the frame,
// took it, so Escape marked the channel read and the popover stayed open,
// with the popover focused or the field in it. With no plugin layer open
// the binding has Escape again. Synthetic data on internal/fakeslack.
func TestEscapeClosesPluginPopoverFirst(t *testing.T) {
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
	if pe.a.overlayOpen() {
		t.Fatal("an overlay is open after boot: Escape would close it first")
	}

	fb := &escBridge{renders: map[string]uint64{}}
	slackc.SetBridge(fb)
	slackc.SetPatched([]string{"HelpButton"})
	t.Cleanup(func() { slackc.Reset(); slackc.SetBridge(nil) })
	pe.until("the help button's plugin root", 5*time.Second, func() bool { return fb.root("HelpButton") != 0 })
	root := fb.root("HelpButton")
	// the plugin's tree: the help button and, after it, a popover
	const onClose = 901
	popover := func(kids string) string {
		return `[{"t":"@","n":"HelpButton","k":1},{"t":"@","n":"Popover","k":2,"p":{"isOpen":true,` +
			`"targetBounds":{"x":1200,"y":40,"width":28,"height":28},"onClose":{"$fn":901}},"c":[{"t":"div","k":3,"c":[` + kids + `]}]}]`
	}
	// fakeLatest is the channel's newest message as the test makes it
	// unread: conversations.mark at this ts is the shell's Escape
	const fakeLatest = "9999999999.000100"
	unread := func() {
		c := pe.a.data.m.conv(ch)
		if c == nil {
			t.Fatal("no conversation in the model")
		}
		c.Unread, c.Latest = true, fakeLatest
	}
	for _, c := range []struct{ what, kids string }{
		{"the popover focused", `"Filter"`},
		{"the autoFocus field in it focused", `{"t":"@","n":"FormTextInput","k":4,"p":{"autoFocus":true,"value":""}}`},
	} {
		if err := slackc.Deliver(root, []byte(popover(c.kids))); err != nil {
			t.Fatal(err)
		}
		pe.settle(300 * time.Millisecond)
		unread()
		pe.key(key.NameEscape, 0)
		pe.settle(500 * time.Millisecond)
		if !fb.called(onClose) {
			t.Errorf("%s: Escape didn't close the plugin's popover (no onClose)", c.what)
		}
		if fs.Marked(ch) == fakeLatest {
			t.Fatalf("%s: Escape marked the channel read under the open popover", c.what)
		}
		fb.mu.Lock()
		fb.calls = nil
		fb.mu.Unlock()
	}
	// the plugin closes it: Escape is the shell's again
	if err := slackc.Deliver(root, []byte(`[{"t":"@","n":"HelpButton","k":1}]`)); err != nil {
		t.Fatal(err)
	}
	pe.settle(300 * time.Millisecond)
	unread()
	pe.key(key.NameEscape, 0)
	pe.until("Escape's conversations.mark with no popover open", 10*time.Second, func() bool { return fs.Marked(ch) == fakeLatest })
}
