//go:build darwin && cgo

package globalkey

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Carbon -framework Foundation
int clack_hotkey_register(void);
void clack_hotkey_unregister(void);
*/
import "C"

import "sync"

// Carbon hot keys arrive through the application event target, which
// Gio's NSApp run loop services; the press handler runs on the main
// thread and hands off to a goroutine.

var (
	mu      sync.Mutex
	pressFn func()
)

func register(onPress func()) (func(), error) {
	mu.Lock()
	defer mu.Unlock()
	if pressFn != nil {
		return nil, ErrTaken // one registration per process
	}
	switch C.clack_hotkey_register() {
	case 0:
	case 1:
		return nil, ErrTaken
	default:
		return nil, ErrUnsupported
	}
	pressFn = onPress
	var once sync.Once
	return func() {
		once.Do(func() {
			C.clack_hotkey_unregister()
			mu.Lock()
			pressFn = nil
			mu.Unlock()
		})
	}, nil
}

//export clackHotkeyPressed
func clackHotkeyPressed() {
	mu.Lock()
	f := pressFn
	mu.Unlock()
	if f != nil {
		go f()
	}
}
