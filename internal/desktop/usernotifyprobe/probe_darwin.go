//go:build darwin && cgo

package usernotifyprobe

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation -framework UserNotifications -framework Security -framework CoreGraphics
#include <stdlib.h>
char *clack_unprobe_bundle(void);
char *clack_unprobe_center(void);
char *clack_unprobe_settings(int secs);
char *clack_unprobe_request(long opts, int secs);
char *clack_unprobe_post(const char *ident, const char *title, const char *subtitle, const char *body, const char *thread, int category, int secs);
char *clack_unprobe_delivered(int secs);
char *clack_unprobe_delivered_json(int secs);
char *clack_unprobe_categories(int secs);
char *clack_unprobe_constants(void);
char *clack_unprobe_delegate_class(void);
char *clack_unprobe_respond(const char *ident, const char *action, const char *text, int textInput, int secs);
long clack_unprobe_will_present(const char *ident, int secs);
*/
import "C"

import "unsafe"

// goStr takes a malloc'd C string from the .m side.
func goStr(p *C.char) string {
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p)
}

// bundle is the main bundle, the window-server session and the code
// signature as the process sees them.
func bundle() string { return goStr(C.clack_unprobe_bundle()) }

// centerErr is why UNUserNotificationCenter.currentNotificationCenter
// gave no center ("" when it did).
func centerErr() string { return goStr(C.clack_unprobe_center()) }

// settings is getNotificationSettings' answer.
func settings(secs int) string { return goStr(C.clack_unprobe_settings(C.int(secs))) }

// request is requestAuthorization(opts)'s answer, or that none came in
// secs.
func request(opts, secs int) string { return goStr(C.clack_unprobe_request(C.long(opts), C.int(secs))) }

// post adds a notification request (no trigger: show now), with
// Electron's category of a reply box, a button and the close button when
// category is set.
func post(ident, title, subtitle, body, thread string, category bool, secs int) string {
	cs := []*C.char{C.CString(ident), C.CString(title), C.CString(subtitle), C.CString(body), C.CString(thread)}
	defer func() {
		for _, c := range cs {
			C.free(unsafe.Pointer(c))
		}
	}()
	cat := C.int(0)
	if category {
		cat = 1
	}
	return goStr(C.clack_unprobe_post(cs[0], cs[1], cs[2], cs[3], cs[4], cat, C.int(secs)))
}

// delivered lists the notifications Notification Center holds for the
// process, and what the center's delegate was told.
func delivered(secs int) string { return goStr(C.clack_unprobe_delivered(C.int(secs))) }

// deliveredJSON is the delivered notifications as JSON ("null": no answer).
func deliveredJSON(secs int) string { return goStr(C.clack_unprobe_delivered_json(C.int(secs))) }

// categoriesJSON is the registered categories as JSON ("null": no answer).
func categoriesJSON(secs int) string { return goStr(C.clack_unprobe_categories(C.int(secs))) }

// actionConstants is UNNotificationDefaultActionIdentifier and
// UNNotificationDismissActionIdentifier, joined by "|".
func actionConstants() string { return goStr(C.clack_unprobe_constants()) }

// delegateClass is the center's delegate's class name.
func delegateClass() string { return goStr(C.clack_unprobe_delegate_class()) }

// respond hands the delegate a response to the delivered notification id
// ("" done, else why not).
func respond(id, action, text string, textInput bool, secs int) string {
	ci, ca, ct := C.CString(id), C.CString(action), C.CString(text)
	defer C.free(unsafe.Pointer(ci))
	defer C.free(unsafe.Pointer(ca))
	defer C.free(unsafe.Pointer(ct))
	in := C.int(0)
	if textInput {
		in = 1
	}
	return goStr(C.clack_unprobe_respond(ci, ca, ct, in, C.int(secs)))
}

// willPresent is the delegate's presentation options for the delivered
// notification id (-1: none).
func willPresent(id string, secs int) int {
	ci := C.CString(id)
	defer C.free(unsafe.Pointer(ci))
	return int(C.clack_unprobe_will_present(ci, C.int(secs)))
}
