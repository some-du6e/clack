package desktop

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows toast notifications through WinRT (Windows.UI.Notifications),
// the API Electron's Notification uses on Windows and so Slack desktop's
// NativeWindowsNotification (main.bundle.cjs 4.53.276, win32 x64: class
// EH builds the toast XML and hands it to Electron as toastXml). No cgo:
// the calls go through each interface's vtable with syscall.SyscallN, and
// the three event handlers are Go COM objects (syscall.NewCallback).
//
// Every WinRT call runs on one OS thread in the multithreaded apartment
// (RoInitialize RO_INIT_MULTITHREADED). WinRT calls the handlers on its
// own MTA threads; they only copy what they need and go on in a goroutine.
//
// The interface ids and vtable slots are Windows.UI.Notifications.idl's
// and Windows.Data.Xml.Dom.idl's (as windows-rs 0.54 lists them). The
// TypedEventHandler ids are WinRT's parameterized ids (UUIDv5 of the
// pinterface signature), computed and checked against the same source.

var (
	combase = windows.NewLazySystemDLL("combase.dll")

	procRoInitialize              = combase.NewProc("RoInitialize")
	procRoGetActivationFactory    = combase.NewProc("RoGetActivationFactory")
	procRoActivateInstance        = combase.NewProc("RoActivateInstance")
	procWindowsCreateString       = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString       = combase.NewProc("WindowsDeleteString")
	procWindowsGetStringRawBuffer = combase.NewProc("WindowsGetStringRawBuffer")
	procRtlMoveMemory             = kernel32.NewProc("RtlMoveMemory")
)

func guid(d1 uint32, d2, d3 uint16, d4 ...byte) windows.GUID {
	g := windows.GUID{Data1: d1, Data2: d2, Data3: d3}
	copy(g.Data4[:], d4)
	return g
}

var (
	iidIUnknown     = guid(0x00000000, 0x0000, 0x0000, 0xc0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46)
	iidIAgileObject = guid(0x94ea2b94, 0xe9cc, 0x49e0, 0xc0, 0xff, 0xee, 0x64, 0xca, 0x8f, 0x5b, 0x90)

	iidIToastNotificationManagerStatics  = guid(0x50ac103f, 0xd235, 0x4598, 0xbb, 0xef, 0x98, 0xfe, 0x4d, 0x1a, 0x3a, 0xd4)
	iidIToastNotificationManagerStatics2 = guid(0x7ab93c52, 0x0e48, 0x4750, 0xba, 0x9d, 0x1a, 0x41, 0x13, 0x98, 0x18, 0x47)
	iidIToastNotificationFactory         = guid(0x04124b20, 0x82c6, 0x4229, 0xb1, 0x09, 0xfd, 0x9e, 0xd4, 0x66, 0x2b, 0x53)
	iidIToastNotification2               = guid(0x9dfb9fd1, 0x143a, 0x490e, 0x90, 0xbf, 0xb9, 0xfb, 0xa7, 0x13, 0x2d, 0xe7)
	iidIToastActivatedEventArgs          = guid(0xe3bf92f3, 0xc197, 0x436f, 0x82, 0x65, 0x06, 0x25, 0x82, 0x4f, 0x8d, 0xac)
	iidIToastDismissedEventArgs          = guid(0x3f89d935, 0xd9cb, 0x4538, 0xa0, 0xf0, 0xff, 0xe7, 0x65, 0x99, 0x38, 0xf8)
	iidIToastFailedEventArgs             = guid(0x35176862, 0xcfd4, 0x44f8, 0xad, 0x64, 0xf5, 0x00, 0xfd, 0x89, 0x6c, 0x3b)
	iidIToastNotificationHistory         = guid(0x5caddc63, 0x01d3, 0x4c97, 0x98, 0x6f, 0x05, 0x33, 0x48, 0x3f, 0xee, 0x14)
	iidIToastNotificationHistory2        = guid(0x3bc3d253, 0x2f31, 0x4092, 0x91, 0x29, 0x8a, 0xd5, 0xab, 0xf0, 0x67, 0xda)
	iidIXmlDocument                      = guid(0xf7f3a506, 0x1e87, 0x42d6, 0xbc, 0xfb, 0xb8, 0xc8, 0x09, 0xfa, 0x54, 0x94)
	iidIXmlDocumentIO                    = guid(0x6cd0e74e, 0xee65, 0x4489, 0x9e, 0xbf, 0xca, 0x43, 0xe8, 0x7b, 0xa6, 0x37)

	// TypedEventHandler<ToastNotification, Object> (Activated),
	// <…, ToastDismissedEventArgs> (Dismissed), <…, ToastFailedEventArgs> (Failed)
	iidActivatedHandler = guid(0xab54de2d, 0x97d9, 0x5528, 0xb6, 0xad, 0x10, 0x5a, 0xfe, 0x15, 0x65, 0x30)
	iidDismissedHandler = guid(0x61c2402f, 0x0ed0, 0x5a18, 0xab, 0x69, 0x59, 0xf4, 0xaa, 0x99, 0xa3, 0x68)
	iidFailedHandler    = guid(0x95e3e803, 0xc969, 0x5e3a, 0x97, 0x53, 0xea, 0x2a, 0xd2, 0x2a, 0x9a, 0x33)
)

// Vtable slots: IUnknown's 0–2 and IInspectable's 3–5 come first.
const (
	vtQueryInterface = 0
	vtRelease        = 2

	vtStaticsCreateToastNotifierWithId = 7 // IToastNotificationManagerStatics
	vtStatics2History                  = 6 // IToastNotificationManagerStatics2
	vtNotifierShow                     = 6 // IToastNotifier
	vtNotifierSetting                  = 8
	vtFactoryCreateToastNotification   = 6 // IToastNotificationFactory
	vtToastAddDismissed                = 9 // IToastNotification
	vtToastRemoveDismissed             = 10
	vtToastAddActivated                = 11
	vtToastRemoveActivated             = 12
	vtToastAddFailed                   = 13
	vtToastRemoveFailed                = 14
	vtToast2PutTag                     = 6 // IToastNotification2
	vtToast2GetTag                     = 7
	vtToast2PutGroup                   = 8
	vtToast2GetGroup                   = 9
	vtActivatedArguments               = 6 // IToastActivatedEventArgs
	vtDismissedReason                  = 6 // IToastDismissedEventArgs
	vtFailedErrorCode                  = 6 // IToastFailedEventArgs
	vtHistoryRemoveGroupWithId         = 7 // IToastNotificationHistory
	vtHistoryRemoveGroupedTagWithId    = 8
	vtHistoryClearWithId               = 12
	vtHistory2GetHistoryWithId         = 7 // IToastNotificationHistory2
	vtVectorGetAt                      = 6 // IVectorView<ToastNotification>
	vtVectorSize                       = 7
	vtXMLLoadXml                       = 6 // IXmlDocumentIO
)

// NotificationSetting values (ToastNotifier.Setting).
const (
	ToastEnabled                = 0
	ToastDisabledForApplication = 1
	ToastDisabledForUser        = 2
	ToastDisabledByGroupPolicy  = 3
	ToastDisabledByManifest     = 4
)

// ToastDismissalReason values (Toast.Dismissed).
const (
	ToastUserCanceled      = 0
	ToastApplicationHidden = 1
	ToastTimedOut          = 2 // the banner went; the toast stays in Notification Center
)

// HResultError is a failed WinRT call.
type HResultError uint32

func (e HResultError) Error() string {
	return fmt.Sprintf("%s (HRESULT %#08x)", syscall.Errno(e).Error(), uint32(e))
}

func failed(hr uintptr) bool { return int32(uint32(hr)) < 0 }

// rtObject is a COM or WinRT interface pointer: its first word points at
// the vtable.
type rtObject struct{ vtbl *[32]uintptr }

func (o *rtObject) call(slot int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(o.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

func (o *rtObject) release() {
	if o != nil {
		o.call(vtRelease)
	}
}

// query is QueryInterface.
func (o *rtObject) query(iid *windows.GUID) (*rtObject, error) {
	var p *rtObject
	if hr := o.call(vtQueryInterface, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&p))); failed(hr) || p == nil {
		return nil, HResultError(hr)
	}
	return p, nil
}

// out calls a method whose last parameter receives an interface pointer.
func (o *rtObject) out(slot int, args ...uintptr) (*rtObject, error) {
	var p *rtObject
	if hr := o.call(slot, append(args, uintptr(unsafe.Pointer(&p)))...); failed(hr) || p == nil {
		return nil, HResultError(hr)
	}
	return p, nil
}

// hstring is a WinRT HSTRING.
type hstring uintptr

func newHString(s string) (hstring, error) {
	if s == "" {
		return 0, nil // the empty HSTRING is NULL
	}
	u := utf16.Encode([]rune(s))
	var h hstring
	if hr, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)), uintptr(unsafe.Pointer(&h))); failed(hr) {
		return 0, HResultError(hr)
	}
	return h, nil
}

func (h hstring) free() {
	if h != 0 {
		procWindowsDeleteString.Call(uintptr(h))
	}
}

func (h hstring) String() string {
	if h == 0 {
		return ""
	}
	var n uint32
	p, _, _ := procWindowsGetStringRawBuffer.Call(uintptr(h), uintptr(unsafe.Pointer(&n)))
	if p == 0 || n == 0 {
		return ""
	}
	buf := make([]uint16, n)
	procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&buf[0])), p, uintptr(n)*2)
	return string(utf16.Decode(buf))
}

// withHString passes s to f as an HSTRING.
func withHString(s string, f func(hstring) error) error {
	h, err := newHString(s)
	if err != nil {
		return err
	}
	defer h.free()
	return f(h)
}

// ---- the WinRT thread ----

var rt struct {
	once sync.Once
	err  error
	work chan func()

	statics *rtObject // IToastNotificationManagerStatics
	factory *rtObject // IToastNotificationFactory
	history *rtObject // IToastNotificationHistory
	notif   map[string]*rtObject
}

// onRT runs f on the WinRT thread and waits for it.
func onRT(f func() error) error {
	rt.once.Do(func() {
		if err := procRoInitialize.Find(); err != nil {
			rt.err = err // before Windows 8
			return
		}
		rt.work = make(chan func(), 16)
		ready := make(chan error, 1)
		go func() {
			runtime.LockOSThread() // for good: this thread is the apartment's
			const roInitMultithreaded = 1
			hr, _, _ := procRoInitialize.Call(roInitMultithreaded)
			if failed(hr) {
				ready <- fmt.Errorf("RoInitialize: %w", HResultError(hr))
				return
			}
			ready <- nil
			for f := range rt.work {
				f()
			}
		}()
		rt.err = <-ready
	})
	if rt.err != nil {
		return rt.err
	}
	done := make(chan error, 1)
	rt.work <- func() { done <- f() }
	return <-done
}

// activationFactory is RoGetActivationFactory(class, iid).
func activationFactory(class string, iid *windows.GUID) (*rtObject, error) {
	var p *rtObject
	err := withHString(class, func(h hstring) error {
		if hr, _, _ := procRoGetActivationFactory.Call(uintptr(h), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&p))); failed(hr) || p == nil {
			return fmt.Errorf("RoGetActivationFactory(%s): %w", class, HResultError(hr))
		}
		return nil
	})
	return p, err
}

// managerStatics is ToastNotificationManager's statics (WinRT thread).
func managerStatics() (*rtObject, error) {
	if rt.statics == nil {
		s, err := activationFactory("Windows.UI.Notifications.ToastNotificationManager", &iidIToastNotificationManagerStatics)
		if err != nil {
			return nil, err
		}
		rt.statics = s
	}
	return rt.statics, nil
}

// notifier is CreateToastNotifierWithId(aumid), kept per id (WinRT thread).
func notifier(aumid string) (*rtObject, error) {
	if n := rt.notif[aumid]; n != nil {
		return n, nil
	}
	s, err := managerStatics()
	if err != nil {
		return nil, err
	}
	var n *rtObject
	err = withHString(aumid, func(h hstring) error {
		var err error
		if n, err = s.out(vtStaticsCreateToastNotifierWithId, uintptr(h)); err != nil {
			return fmt.Errorf("CreateToastNotifierWithId: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if rt.notif == nil {
		rt.notif = map[string]*rtObject{}
	}
	rt.notif[aumid] = n
	return n, nil
}

// toastHistory is ToastNotificationManager.History (WinRT thread).
func toastHistory() (*rtObject, error) {
	if rt.history == nil {
		s, err := managerStatics()
		if err != nil {
			return nil, err
		}
		s2, err := s.query(&iidIToastNotificationManagerStatics2)
		if err != nil {
			return nil, fmt.Errorf("IToastNotificationManagerStatics2: %w", err)
		}
		defer s2.release()
		h, err := s2.out(vtStatics2History)
		if err != nil {
			return nil, fmt.Errorf("History: %w", err)
		}
		rt.history = h
	}
	return rt.history, nil
}

// xmlDocument is a Windows.Data.Xml.Dom.XmlDocument loaded with x, as
// IXmlDocument (WinRT thread).
func xmlDocument(x string) (*rtObject, error) {
	var inst *rtObject
	err := withHString("Windows.Data.Xml.Dom.XmlDocument", func(h hstring) error {
		if hr, _, _ := procRoActivateInstance.Call(uintptr(h), uintptr(unsafe.Pointer(&inst))); failed(hr) || inst == nil {
			return fmt.Errorf("RoActivateInstance(XmlDocument): %w", HResultError(hr))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	defer inst.release()
	io, err := inst.query(&iidIXmlDocumentIO)
	if err != nil {
		return nil, fmt.Errorf("IXmlDocumentIO: %w", err)
	}
	defer io.release()
	err = withHString(x, func(h hstring) error {
		if hr := io.call(vtXMLLoadXml, uintptr(h)); failed(hr) {
			return fmt.Errorf("LoadXml: %w", HResultError(hr))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return inst.query(&iidIXmlDocument)
}

// ---- event handlers: Go COM objects ----

// rtHandler is a TypedEventHandler<ToastNotification, T>: IUnknown's
// three methods and Invoke(sender, args). WinRT holds it by its address;
// handlers keeps it reachable until the last Release.
type rtHandler struct {
	vtbl   *[4]uintptr
	refs   int32
	iid    windows.GUID
	invoke func(args *rtObject)
}

var (
	handlerVtbl [4]uintptr
	handlerOnce sync.Once
	handlersMu  sync.Mutex
	handlers    = map[*rtHandler]bool{}
)

const (
	hrOK          = 0
	hrNoInterface = 0x80004002
)

func newHandler(iid windows.GUID, invoke func(args *rtObject)) *rtHandler {
	handlerOnce.Do(func() {
		handlerVtbl = [4]uintptr{
			syscall.NewCallback(handlerQueryInterface),
			syscall.NewCallback(handlerAddRef),
			syscall.NewCallback(handlerRelease),
			syscall.NewCallback(handlerInvoke),
		}
	})
	h := &rtHandler{vtbl: &handlerVtbl, refs: 1, iid: iid, invoke: invoke}
	handlersMu.Lock()
	handlers[h] = true
	handlersMu.Unlock()
	return h
}

func handlerQueryInterface(h *rtHandler, iid *windows.GUID, out *uintptr) uintptr {
	switch *iid {
	case h.iid, iidIUnknown, iidIAgileObject: // agile: Invoke may come on any thread
		atomic.AddInt32(&h.refs, 1)
		*out = uintptr(unsafe.Pointer(h))
		return hrOK
	}
	*out = 0
	return hrNoInterface
}

func handlerAddRef(h *rtHandler) uintptr { return uintptr(atomic.AddInt32(&h.refs, 1)) }

func handlerRelease(h *rtHandler) uintptr {
	n := atomic.AddInt32(&h.refs, -1)
	if n == 0 {
		handlersMu.Lock()
		delete(handlers, h)
		handlersMu.Unlock()
	}
	return uintptr(n)
}

func handlerInvoke(h *rtHandler, sender, args *rtObject) uintptr {
	if h.invoke != nil && args != nil {
		h.invoke(args)
	}
	return hrOK
}

// ---- toasts ----

// Toast is one toast notification.
type Toast struct {
	XML string // the toast's content (ToastGeneric)
	// Tag and Group (IToastNotification2): a toast with the same pair
	// replaces this one, and History lists it by them.
	Tag, Group string
	// Activated gets the activation's arguments: the toast's launch
	// attribute after a click on its body, or the button's arguments
	// (activationType="foreground"). It comes only while this process
	// runs. Any goroutine.
	Activated func(args string)
	// Dismissed gets a ToastDismissalReason. Any goroutine.
	Dismissed func(reason int)
	// Failed: Windows didn't show it. Any goroutine.
	Failed func(err error)
}

// liveToast is a shown toast kept alive for its events: the banner's
// timeout leaves it in Notification Center, where a click still reaches
// this process while it runs.
type liveToast struct {
	obj      *rtObject // IToastNotification
	tag, grp string
	tokens   [3]int64 // Activated, Dismissed, Failed
}

var (
	live    []*liveToast // oldest first (WinRT thread)
	maxLive = 64         // Notification Center keeps 20 per app by default
)

// ShowToast shows t as the app aumid
// (ToastNotificationManager.CreateToastNotifierWithId(aumid).Show).
func ShowToast(aumid string, t Toast) error {
	return onRT(func() error {
		n, err := notifier(aumid)
		if err != nil {
			return err
		}
		if rt.factory == nil {
			f, err := activationFactory("Windows.UI.Notifications.ToastNotification", &iidIToastNotificationFactory)
			if err != nil {
				return err
			}
			rt.factory = f
		}
		doc, err := xmlDocument(t.XML)
		if err != nil {
			return err
		}
		defer doc.release()
		obj, err := rt.factory.out(vtFactoryCreateToastNotification, uintptr(unsafe.Pointer(doc)))
		if err != nil {
			return fmt.Errorf("CreateToastNotification: %w", err)
		}
		lt := &liveToast{obj: obj, tag: t.Tag, grp: t.Group}
		if t.Tag != "" || t.Group != "" {
			if err := setTagGroup(obj, t.Tag, t.Group); err != nil {
				obj.release()
				return err
			}
		}
		activated, dismissed, fail := t.Activated, t.Dismissed, t.Failed
		hs := []struct {
			slot int
			iid  windows.GUID
			f    func(args *rtObject)
		}{
			{vtToastAddActivated, iidActivatedHandler, func(args *rtObject) {
				s := activationArgs(args)
				go finish(lt)
				if activated != nil {
					go activated(s)
				}
			}},
			{vtToastAddDismissed, iidDismissedHandler, func(args *rtObject) {
				r := dismissalReason(args)
				if r != ToastTimedOut {
					go finish(lt)
				}
				if dismissed != nil {
					go dismissed(r)
				}
			}},
			{vtToastAddFailed, iidFailedHandler, func(args *rtObject) {
				err := toastError(args)
				go finish(lt)
				if fail != nil {
					go fail(err)
				}
			}},
		}
		for i, h := range hs {
			d := newHandler(h.iid, h.f)
			hr := obj.call(h.slot, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&lt.tokens[i])))
			handlerRelease(d) // the toast holds its own reference
			if failed(hr) {
				removeHandlers(lt, i)
				obj.release()
				return fmt.Errorf("adding the toast's event handler %d: %w", i, HResultError(hr))
			}
		}
		if hr := n.call(vtNotifierShow, uintptr(unsafe.Pointer(obj))); failed(hr) {
			removeHandlers(lt, len(hs))
			obj.release()
			return fmt.Errorf("Show: %w", HResultError(hr))
		}
		// one live toast per tag and group: the new one replaced the old
		for i, o := range live {
			if (lt.tag != "" || lt.grp != "") && o.tag == lt.tag && o.grp == lt.grp {
				live = append(live[:i], live[i+1:]...)
				drop(o)
				break
			}
		}
		live = append(live, lt)
		for len(live) > maxLive {
			o := live[0]
			live = live[1:]
			drop(o)
		}
		return nil
	})
}

func setTagGroup(obj *rtObject, tag, group string) error {
	t2, err := obj.query(&iidIToastNotification2)
	if err != nil {
		return fmt.Errorf("IToastNotification2: %w", err)
	}
	defer t2.release()
	for _, p := range []struct {
		slot int
		v    string
	}{{vtToast2PutTag, tag}, {vtToast2PutGroup, group}} {
		if p.v == "" {
			continue
		}
		err := withHString(p.v, func(h hstring) error {
			if hr := t2.call(p.slot, uintptr(h)); failed(hr) {
				return HResultError(hr)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("toast tag/group %q: %w", p.v, err)
		}
	}
	return nil
}

// finish forgets a toast that can't fire again (any goroutine).
func finish(lt *liveToast) {
	_ = onRT(func() error {
		for i, o := range live {
			if o == lt {
				live = append(live[:i], live[i+1:]...)
				drop(o)
				break
			}
		}
		return nil
	})
}

// drop removes a live toast's handlers and releases it (WinRT thread).
func drop(lt *liveToast) {
	removeHandlers(lt, 3)
	lt.obj.release()
}

func removeHandlers(lt *liveToast, n int) {
	for i, slot := range []int{vtToastRemoveActivated, vtToastRemoveDismissed, vtToastRemoveFailed}[:n] {
		if lt.tokens[i] != 0 {
			lt.obj.call(slot, uintptr(lt.tokens[i]))
			lt.tokens[i] = 0
		}
	}
}

func activationArgs(args *rtObject) string {
	a, err := args.query(&iidIToastActivatedEventArgs)
	if err != nil {
		return ""
	}
	defer a.release()
	var h hstring
	if hr := a.call(vtActivatedArguments, uintptr(unsafe.Pointer(&h))); failed(hr) {
		return ""
	}
	defer h.free()
	return h.String()
}

func dismissalReason(args *rtObject) int {
	a, err := args.query(&iidIToastDismissedEventArgs)
	if err != nil {
		return -1
	}
	defer a.release()
	var r int32
	if hr := a.call(vtDismissedReason, uintptr(unsafe.Pointer(&r))); failed(hr) {
		return -1
	}
	return int(r)
}

func toastError(args *rtObject) error {
	a, err := args.query(&iidIToastFailedEventArgs)
	if err != nil {
		return errors.New("the toast failed")
	}
	defer a.release()
	var code int32
	if hr := a.call(vtFailedErrorCode, uintptr(unsafe.Pointer(&code))); failed(hr) {
		return errors.New("the toast failed")
	}
	return fmt.Errorf("the toast failed: %w", HResultError(uint32(code)))
}

// ToastSetting is ToastNotifier.Setting for aumid: ToastEnabled, or why
// Windows won't show its toasts.
func ToastSetting(aumid string) (int, error) {
	var s int32
	err := onRT(func() error {
		n, err := notifier(aumid)
		if err != nil {
			return err
		}
		if hr := n.call(vtNotifierSetting, uintptr(unsafe.Pointer(&s))); failed(hr) {
			return fmt.Errorf("Setting: %w", HResultError(hr))
		}
		return nil
	})
	return int(s), err
}

// ToastRef names a toast in Notification Center.
type ToastRef struct{ Tag, Group string }

// ToastHistory lists aumid's toasts in Notification Center
// (ToastNotificationHistory.GetHistory(aumid)).
func ToastHistory(aumid string) ([]ToastRef, error) {
	var out []ToastRef
	err := onRT(func() error {
		h, err := toastHistory()
		if err != nil {
			return err
		}
		h2, err := h.query(&iidIToastNotificationHistory2)
		if err != nil {
			return fmt.Errorf("IToastNotificationHistory2: %w", err)
		}
		defer h2.release()
		var list *rtObject
		err = withHString(aumid, func(id hstring) error {
			var err error
			list, err = h2.out(vtHistory2GetHistoryWithId, uintptr(id))
			return err
		})
		if err != nil {
			return fmt.Errorf("GetHistory: %w", err)
		}
		defer list.release()
		var n uint32
		if hr := list.call(vtVectorSize, uintptr(unsafe.Pointer(&n))); failed(hr) {
			return fmt.Errorf("Size: %w", HResultError(hr))
		}
		for i := uint32(0); i < n && i < 1000; i++ {
			t, err := list.out(vtVectorGetAt, uintptr(i))
			if err != nil {
				continue
			}
			var ref ToastRef
			if t2, err := t.query(&iidIToastNotification2); err == nil {
				var tag, grp hstring
				t2.call(vtToast2GetTag, uintptr(unsafe.Pointer(&tag)))
				t2.call(vtToast2GetGroup, uintptr(unsafe.Pointer(&grp)))
				ref = ToastRef{Tag: tag.String(), Group: grp.String()}
				tag.free()
				grp.free()
				t2.release()
			}
			t.release()
			out = append(out, ref)
		}
		return nil
	})
	return out, err
}

// RemoveToasts removes aumid's toasts from Notification Center: the one
// with tag in group, every one in group (tag ""), or all of them (both
// ""). Their Dismissed handlers get ToastApplicationHidden.
func RemoveToasts(aumid, tag, group string) error {
	return onRT(func() error {
		h, err := toastHistory()
		if err != nil {
			return err
		}
		return withHString(aumid, func(id hstring) error {
			return withHString(group, func(g hstring) error {
				return withHString(tag, func(t hstring) error {
					var hr uintptr
					switch {
					case tag != "":
						hr = h.call(vtHistoryRemoveGroupedTagWithId, uintptr(t), uintptr(g), uintptr(id))
					case group != "":
						hr = h.call(vtHistoryRemoveGroupWithId, uintptr(g), uintptr(id))
					default:
						hr = h.call(vtHistoryClearWithId, uintptr(id))
					}
					if failed(hr) {
						return HResultError(hr)
					}
					return nil
				})
			})
		})
	})
}
