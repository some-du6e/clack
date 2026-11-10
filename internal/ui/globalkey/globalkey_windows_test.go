//go:build windows

package globalkey

import (
	"testing"
	"time"
)

// Ctrl+Shift+Space registers (unless another app holds it on the runner),
// and the thread turns WM_HOTKEY into a press (posted by hand: nobody
// types on a CI runner).
func TestRegisterWindows(t *testing.T) {
	tids := make(chan uint32, 1)
	testHookThread = func(tid uint32) { tids <- tid }
	defer func() { testHookThread = nil }()
	pressed := make(chan struct{}, 2)
	stop, err := Register(func() { pressed <- struct{}{} })
	if err == ErrTaken {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	tid := <-tids
	procPostThreadMessageW.Call(uintptr(tid), wmHotkey, hotkeyID, 0)
	select {
	case <-pressed:
	case <-time.After(5 * time.Second):
		t.Fatal("no press")
	}
	stop()
	// once released it can be registered again
	stop2, err := Register(func() {})
	if err != nil {
		t.Fatalf("register after stop: %v", err)
	}
	stop2()
}
