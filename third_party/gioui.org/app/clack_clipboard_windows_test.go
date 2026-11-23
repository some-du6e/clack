// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package app

import (
	stdcontext "context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	syscall "golang.org/x/sys/windows"

	"gioui.org/app/winclip"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/system"
	"gioui.org/io/transfer"
	"gioui.org/op"
)

// Clack patch (CLACK_PATCHES.md §65): a window's own copy and paste
// (clipboard.WriteCmd, clipboard.ReadCmd: Clack's search box, modals, the
// sign-in paste) are winclip sessions, run off the window thread one after
// another, and the paste comes back to the window (_WM_CLACK_PASTE). On
// the real clipboard, in a real window:
//   - a copy and a paste asked for in one frame paste the copy (the order
//     kept), and another session reads the copy as Gio wrote it;
//   - a paste while another of the process's sessions holds the
//     clipboard waits for it, then arrives;
//   - a paste that can't get a session in clipWait is reported
//     (winclip.OnBusy), not dropped without a word.
//
// It replaces the clipboard, so it runs only where
// CLACK_WINDOWS_CLIPBOARD_TESTS is set (ci's windows-applog job).
func TestWinGioClipboard(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_CLIPBOARD_TESTS") == "" {
		t.Skip("replaces the clipboard: set CLACK_WINDOWS_CLIPBOARD_TESTS=1")
	}
	busy := make(chan bool, 4)
	winclip.OnBusy = func(write bool) { busy <- write }
	t.Cleanup(func() { winclip.OnBusy = nil })

	w := new(Window)
	w.Option(Title("Clack clipboard test"), Size(320, 240))
	type ask struct{ write, read string } // write: "" for none
	asks := make(chan ask, 4)
	pastes := make(chan string, 8)
	hwnds := make(chan syscall.Handle, 1)
	done := make(chan struct{})
	tag := new(int)
	go func() {
		defer close(done)
		var ops op.Ops
		for {
			switch e := w.Event().(type) {
			case Win32ViewEvent:
				if e.HWND != 0 {
					select {
					case hwnds <- syscall.Handle(e.HWND):
					default:
					}
				}
			case FrameEvent:
				gtx := NewContext(&ops, e)
				for {
					ev, ok := gtx.Event(transfer.TargetFilter{Target: tag, Type: "application/text"})
					if !ok {
						break
					}
					if de, ok := ev.(transfer.DataEvent); ok {
						r := de.Open()
						b, _ := io.ReadAll(r)
						r.Close()
						pastes <- string(b)
					}
				}
				select {
				case a := <-asks:
					if a.write != "" {
						gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(a.write))})
					}
					if a.read != "" {
						gtx.Execute(clipboard.ReadCmd{Tag: tag})
					}
				default:
				}
				event.Op(gtx.Ops, tag)
				gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(20 * time.Millisecond)})
				e.Frame(gtx.Ops)
			case DestroyEvent:
				return
			}
		}
	}()
	t.Cleanup(func() {
		w.Perform(system.ActionClose)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Log("the window didn't close")
		}
	})
	select {
	case <-hwnds:
	case <-time.After(30 * time.Second):
		t.Fatal("no Win32ViewEvent")
	}
	paste := func(what string, within time.Duration) string {
		t.Helper()
		select {
		case s := <-pastes:
			return s
		case <-time.After(within):
			t.Fatalf("%s: no paste in %v", what, within)
			return ""
		}
	}

	// a copy, then a paste, in one frame
	const first = "copied by Gio — ünïcode ✓"
	asks <- ask{write: first, read: "yes"}
	if got := paste("the copy and paste of one frame", 10*time.Second); got != first {
		t.Errorf("a paste in the frame of a copy pasted %q, want the copy %q", got, first)
	}
	if got := winclipText(t); got != first {
		t.Errorf("another session reads %q, want Gio's copy %q", got, first)
	}

	// a paste while another session holds the clipboard: it waits
	const second = "the second copy"
	asks <- ask{write: second}
	deadline := time.Now().Add(10 * time.Second)
	for winclipText(t) != second {
		if time.Now().After(deadline) {
			t.Fatal("Gio's second copy never reached the clipboard")
		}
		time.Sleep(20 * time.Millisecond)
	}
	held := make(chan struct{})
	release := make(chan struct{})
	go winclip.Session(stdcontext.Background(), 0, func() error {
		close(held)
		<-release
		return nil
	})
	<-held
	asks <- ask{read: "yes"}
	select {
	case s := <-pastes:
		t.Fatalf("the paste came back (%q) while another session held the clipboard", s)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if got := paste("the paste after the held session", 5*time.Second); got != second {
		t.Errorf("the paste after a held session pasted %q, want %q", got, second)
	}
	select {
	case wr := <-busy:
		t.Errorf("a paste that got its session was reported busy (write %v)", wr)
	default:
	}

	// a paste that can't get a session in clipWait: reported
	held, release = make(chan struct{}), make(chan struct{})
	go winclip.Session(stdcontext.Background(), 0, func() error {
		close(held)
		<-release
		return nil
	})
	<-held
	asks <- ask{read: "yes"}
	select {
	case wr := <-busy:
		if wr {
			t.Error("a busy paste was reported as a copy")
		}
	case <-time.After(clipWait + 5*time.Second):
		t.Error("a paste that never got a session wasn't reported (winclip.OnBusy)")
	}
	close(release)
}

const cfUnicodeText = 13 // CF_UNICODETEXT

// winclipText is the clipboard's CF_UNICODETEXT, read in a session.
func winclipText(t *testing.T) string {
	t.Helper()
	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), 5*time.Second)
	defer cancel()
	s, err := winclip.Read(ctx, func(f winclip.Format) int64 {
		if f.ID == cfUnicodeText {
			return 1 << 20
		}
		return 0
	})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.Bytes(cfUnicodeText)
	return winclip.UTF16Text(b)
}
