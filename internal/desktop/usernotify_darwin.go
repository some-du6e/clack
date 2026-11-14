//go:build darwin && cgo

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework UserNotifications
#include <stdint.h>
#include <stdlib.h>
int clack_un_available(void);
void clack_un_set_delegate(void);
void clack_un_request(uintptr_t h);
void clack_un_status(uintptr_t h);
void clack_un_show(const char *json, uintptr_t h);
*/
import "C"

import (
	"encoding/json"
	"runtime/cgo"
	"sync"
	"sync/atomic"
	"unsafe"
)

// UserNotificationsAvailable reports whether this process can use
// UNUserNotificationCenter: it is an app bundle LaunchServices knows (a
// Clack.app). Outside one, currentNotificationCenter raises inside a
// dispatch_once, which aborts the process (ci run 43585762971), so nothing
// here may touch the center unless this says yes. Cheap; any goroutine.
func UserNotificationsAvailable() bool {
	unAvailOnce.Do(func() { unAvail = C.clack_un_available() != 0 })
	return unAvail
}

var (
	unAvailOnce sync.Once
	unAvail     bool
	unHandler   atomic.Pointer[func(UserNotificationEvent)]
)

// SetUserNotificationHandler makes on the receiver of every response
// (NotificationCenterDelegate) and sets the center's delegate, which also
// shows banners while Clack is frontmost. on runs on the center's queue.
func SetUserNotificationHandler(on func(UserNotificationEvent)) {
	if !UserNotificationsAvailable() {
		return
	}
	unHandler.Store(&on)
	C.clack_un_set_delegate()
}

// RequestUserNotificationAuthorization asks for alerts, sounds and badges
// (macOS asks the user the first time). done gets the answer on the
// center's queue; it never comes while the prompt waits.
func RequestUserNotificationAuthorization(done func(granted bool, err error)) {
	if !UserNotificationsAvailable() {
		if done != nil {
			done(false, errNoUserNotifications)
		}
		return
	}
	C.clack_un_request(C.uintptr_t(cgo.NewHandle(done)))
}

// UserNotificationAuthorization reads the authorization status
// (getNotificationSettings) and hands it to done on the center's queue.
func UserNotificationAuthorization(done func(UserNotificationAuth, error)) {
	if !UserNotificationsAvailable() {
		if done != nil {
			done(AuthNotDetermined, errNoUserNotifications)
		}
		return
	}
	C.clack_un_status(C.uintptr_t(cgo.NewHandle(done)))
}

// ShowUserNotification adds n for Notification Center to show now. done
// gets the center's answer (nil: added) on its queue.
func ShowUserNotification(n UserNotification, done func(error)) {
	if !UserNotificationsAvailable() {
		if done != nil {
			done(errNoUserNotifications)
		}
		return
	}
	if n.Actions == nil {
		n.Actions = []string{}
	}
	b, err := json.Marshal(n)
	if err != nil {
		if done != nil {
			done(err)
		}
		return
	}
	cs := C.CString(string(b))
	defer C.free(unsafe.Pointer(cs))
	C.clack_un_show(cs, C.uintptr_t(cgo.NewHandle(done)))
}

func unErr(domain *C.char, code C.long, desc *C.char) error {
	if domain == nil {
		return nil
	}
	return &UserNotificationError{Domain: C.GoString(domain), Code: int(code), Desc: C.GoString(desc)}
}

//export clackUNAuthDone
func clackUNAuthDone(h C.uintptr_t, granted C.int, domain *C.char, code C.long, desc *C.char) {
	hd := cgo.Handle(h)
	f, _ := hd.Value().(func(bool, error))
	hd.Delete()
	if f != nil {
		f(granted != 0, unErr(domain, code, desc))
	}
}

//export clackUNStatusDone
func clackUNStatusDone(h C.uintptr_t, status C.int) {
	hd := cgo.Handle(h)
	f, _ := hd.Value().(func(UserNotificationAuth, error))
	hd.Delete()
	if f != nil {
		f(UserNotificationAuth(status), nil)
	}
}

//export clackUNShowDone
func clackUNShowDone(h C.uintptr_t, domain *C.char, code C.long, desc *C.char) {
	hd := cgo.Handle(h)
	f, _ := hd.Value().(func(error))
	hd.Delete()
	if f != nil {
		f(unErr(domain, code, desc))
	}
}

//export clackUNResponse
func clackUNResponse(id, category, action, text *C.char, textInput C.int) {
	ev, ok := userNotificationEvent(C.GoString(id), C.GoString(category), C.GoString(action), C.GoString(text), textInput != 0)
	if !ok {
		return
	}
	if on := unHandler.Load(); on != nil && *on != nil {
		(*on)(ev)
	}
}
