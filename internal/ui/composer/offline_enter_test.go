package composer

// MessageInput.onEnter while not recently online (boot-shared CPUF, build
// 889ac449a32d): nothing is sent, the draft stays, and the top-right
// toast says "No connection." with cloud-offline; send and the schedule
// caret are disabled (SendButton). An edit still saves (BaseEditMessage
// has no connectivity rule). Synthetic data on the composer's fake API.

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// topToastHost records the top-right toasts.
type topToastHost struct {
	slickHost
	mu    sync.Mutex
	parts [][]i18n.Part
	icons []string
}

func (h *topToastHost) ToastPartsTopRight(parts []i18n.Part, icon string, d time.Duration) {
	h.mu.Lock()
	h.parts, h.icons = append(h.parts, parts), append(h.icons, icon)
	h.mu.Unlock()
}

func TestComposerOfflineEnterLikeSlack(t *testing.T) {
	var mu sync.Mutex
	online := false
	setOnline(t, func() (bool, time.Time) { mu.Lock(); defer mu.Unlock(); return online, time.Time{} })
	app, fs := syntheticApp(t)
	stubTyping(t)
	h := &topToastHost{slickHost: slickHost{app: app, th: theme.New(false)}}
	c := New(h, tChGen, "")
	t.Cleanup(c.Close)
	d := NewDoc()
	d.Insert(0, "hello", Attr{})
	c.ed.SetDoc(d)
	c.submit()
	if got := c.ed.Doc().Text(); got != "hello" {
		t.Errorf("offline Enter left %q in the composer, want the draft kept", got)
	}
	time.Sleep(200 * time.Millisecond)
	runRestores()
	if n := len(fs.called("chat.postMessage")); n != 0 {
		t.Errorf("offline Enter sent the message (%d chat.postMessage)", n)
	}
	h.mu.Lock()
	if len(h.parts) != 1 || h.icons[0] != "cloud-offline" {
		t.Errorf("toasts %v icons %v, want one with cloud-offline", h.parts, h.icons)
	} else {
		p := h.parts[0]
		if p[0].Text != "No connection." || p[0].Tag != "b" || !strings.Contains(i18n.Plain(p), "You’ll be able to send messages as soon as you’re back online.") {
			t.Errorf("the toast reads %q (first part %+v)", i18n.Plain(p), p[0])
		}
	}
	h.mu.Unlock()
	if st := (chromeState{CanSend: true}); !st.sendOff() {
		t.Error("send isn't disabled while not recently online")
	}
	if st := (chromeState{CanSend: true, Mode: modeEdit}); st.sendOff() {
		t.Error("an edit's save is disabled offline; Slack's edit input has no connectivity rule")
	}
	if st := (chromeState{CanSend: true, Online: true}); st.sendOff() {
		t.Error("control: send is disabled while online")
	}

	// back online, Enter sends
	mu.Lock()
	online = true
	mu.Unlock()
	c.submit()
	for dl := time.Now().Add(5 * time.Second); len(fs.called("chat.postMessage")) == 0 && time.Now().Before(dl); time.Sleep(10 * time.Millisecond) {
		runRestores()
	}
	if len(fs.called("chat.postMessage")) == 0 {
		t.Error("online, Enter sent nothing")
	}

	// offline, an edit saves
	mu.Lock()
	online = false
	mu.Unlock()
	e := NewEdit(h, core.Message{Channel: tChGen, TS: "1700000000.000200", Text: "edit me", User: tSelf}, func() {})
	t.Cleanup(e.Close)
	saved := make(chan string, 1)
	e.SaveEdit = func(_ context.Context, _, _, text string, _ json.RawMessage, _ []string) error {
		saved <- text
		return nil
	}
	ed := NewDoc()
	ed.Insert(0, "edited offline", Attr{})
	e.ed.SetDoc(ed)
	e.submit()
	for dl := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		runRestores()
		select {
		case got := <-saved:
			if got != "edited offline" {
				t.Errorf("the edit saved %q", got)
			}
			return
		default:
		}
		if time.Now().After(dl) {
			t.Fatal("offline, Enter in an edit didn't save it")
		}
	}
}
