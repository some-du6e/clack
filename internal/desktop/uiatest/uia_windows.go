package uiatest

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	oleaut32           = windows.NewLazySystemDLL("oleaut32.dll")
	procSysStringLen   = oleaut32.NewProc("SysStringLen")
	procSysFreeString  = oleaut32.NewProc("SysFreeString")
	procSysAllocString = oleaut32.NewProc("SysAllocString")
	procRtlMoveMemory  = windows.NewLazySystemDLL("kernel32.dll").NewProc("RtlMoveMemory")
	procCoCreateInst   = windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance")

	clsidCUIAutomation     = windows.GUID{Data1: 0xff48dba4, Data2: 0x60ef, Data3: 0x4201, Data4: [8]byte{0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e}}
	iidIUIAutomation       = windows.GUID{Data1: 0x30cbe57d, Data2: 0xd9d0, Data3: 0x452a, Data4: [8]byte{0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee}}
	iidIUIAutomationInvoke = windows.GUID{Data1: 0xfb377fbe, Data2: 0x8ea6, Data3: 0x46d5, Data4: [8]byte{0x9c, 0x73, 0x64, 0x99, 0x64, 0x2d, 0x30, 0x59}}
	iidIUIAutomationToggle = windows.GUID{Data1: 0x94cf8058, Data2: 0x9b8d, Data3: 0x4ab9, Data4: [8]byte{0x8b, 0xfd, 0x4c, 0xd0, 0xa3, 0x3c, 0x8c, 0x70}}
	iidIUIAutomationValue  = windows.GUID{Data1: 0xa94cd8b1, Data2: 0x0844, Data3: 0x4cd6, Data4: [8]byte{0x9d, 0x2d, 0x64, 0x05, 0x37, 0xab, 0x39, 0xe9}}
	errNotFound            = errors.New("uiatest: no such element")
	errNoPattern           = errors.New("uiatest: the element can't be invoked, toggled or edited")
)

// Vtable slots (UIAutomationClient.h, as windows-rs 0.54 lists them).
const (
	slotRelease                   = 2
	slotQueryInterface            = 0
	autoGetRootElement            = 5  // IUIAutomation
	autoCreateTrueCondition       = 21 // IUIAutomation
	elemFindAll                   = 6  // IUIAutomationElement
	elemGetCurrentPattern         = 16
	elemCurrentProcessID          = 20
	elemCurrentControlType        = 21
	elemCurrentName               = 23
	elemCurrentClassName          = 30
	elemCurrentHelpText           = 31
	arrLength                     = 3 // IUIAutomationElementArray
	arrGetElement                 = 4
	patInvoke                     = 3 // IUIAutomationInvokePattern
	patToggle                     = 3 // IUIAutomationTogglePattern
	patSetValue                   = 3 // IUIAutomationValuePattern
	invokePatternID               = 10000
	valuePatternID                = 10002
	togglePatternID               = 10015
	treeScopeChildren             = 2
	treeScopeDescendants          = 4
	controlTypeEdit         int32 = 50004
)

type com struct{ vtbl *[64]uintptr }

func (o *com) call(slot int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(o.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

func (o *com) release() {
	if o != nil {
		o.call(slotRelease)
	}
}

func (o *com) out(slot int, args ...uintptr) *com {
	var p *com
	if hr := o.call(slot, append(args, uintptr(unsafe.Pointer(&p)))...); int32(uint32(hr)) < 0 {
		return nil
	}
	return p
}

func (o *com) bstr(slot int) string {
	var b uintptr
	if hr := o.call(slot, uintptr(unsafe.Pointer(&b))); int32(uint32(hr)) < 0 || b == 0 {
		return ""
	}
	defer procSysFreeString.Call(b)
	n, _, _ := procSysStringLen.Call(b)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n)
	procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&buf[0])), b, n*2)
	return string(utf16.Decode(buf))
}

func (o *com) int32At(slot int) int32 {
	var v int32
	o.call(slot, uintptr(unsafe.Pointer(&v)))
	return v
}

// Element is what a test sees of one.
type Element struct {
	Name, Class, Help, Window string
	Control, PID              int32
}

func (e Element) String() string {
	return fmt.Sprintf("%q (control %d, class %q) in %q, pid %d", e.Name, e.Control, e.Class, e.Window, e.PID)
}

func describe(el *com, window string) Element {
	return Element{Name: el.bstr(elemCurrentName), Class: el.bstr(elemCurrentClassName), Help: el.bstr(elemCurrentHelpText),
		Window: window, Control: el.int32At(elemCurrentControlType), PID: el.int32At(elemCurrentProcessID)}
}

// session runs f with an IUIAutomation on a thread of its own in the
// multithreaded apartment.
func session(f func(auto *com) error) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread() // never unlocked: the thread goes with the goroutine
		if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil {
			done <- err
			return
		}
		defer windows.CoUninitialize()
		var auto *com
		const clsctxInprocServer = 1
		hr, _, _ := procCoCreateInst.Call(uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, clsctxInprocServer,
			uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&auto)))
		if int32(uint32(hr)) < 0 || auto == nil {
			done <- fmt.Errorf("uiatest: CoCreateInstance(CUIAutomation): HRESULT %#08x", uint32(hr))
			return
		}
		defer auto.release()
		done <- f(auto)
	}()
	return <-done
}

// walk visits every element under each top-level window (the window's
// name goes along) until visit returns true.
func walk(auto *com, visit func(el *com, window string) bool) error {
	root := auto.out(autoGetRootElement)
	if root == nil {
		return errors.New("uiatest: no root element")
	}
	defer root.release()
	cond := auto.out(autoCreateTrueCondition)
	if cond == nil {
		return errors.New("uiatest: no condition")
	}
	defer cond.release()
	tops := root.out(elemFindAll, treeScopeChildren, uintptr(unsafe.Pointer(cond)))
	if tops == nil {
		return errors.New("uiatest: no top-level windows")
	}
	defer tops.release()
	n := tops.int32At(arrLength)
	for i := int32(0); i < n; i++ {
		w := tops.out(arrGetElement, uintptr(i))
		if w == nil {
			continue
		}
		name := w.bstr(elemCurrentName)
		if visit(w, name) {
			w.release()
			return nil
		}
		all := w.out(elemFindAll, treeScopeDescendants, uintptr(unsafe.Pointer(cond)))
		w.release()
		if all == nil {
			continue
		}
		m := all.int32At(arrLength)
		stop := false
		for j := int32(0); j < m && !stop; j++ {
			el := all.out(arrGetElement, uintptr(j))
			if el == nil {
				continue
			}
			stop = visit(el, name)
			el.release()
		}
		all.release()
		if stop {
			return nil
		}
	}
	return nil
}

// Find waits up to d for an element match accepts.
func Find(match func(Element) bool, d time.Duration) (Element, error) {
	var found Element
	err := retry(d, func(auto *com) (bool, error) {
		ok := false
		err := walk(auto, func(el *com, window string) bool {
			e := describe(el, window)
			if match(e) {
				found, ok = e, true
			}
			return ok
		})
		return ok, err
	})
	return found, err
}

// Act waits up to d for an element match accepts that can be invoked (or
// toggled) and does it.
func Act(match func(Element) bool, d time.Duration) (Element, error) {
	return act(match, d, func(el *com) bool {
		if p := el.out(elemGetCurrentPattern, invokePatternID); p != nil {
			defer p.release()
			if ip := p.queryOrSelf(&iidIUIAutomationInvoke); ip != nil {
				defer ip.release()
				return int32(uint32(ip.call(patInvoke))) >= 0
			}
		}
		if p := el.out(elemGetCurrentPattern, togglePatternID); p != nil {
			defer p.release()
			if tp := p.queryOrSelf(&iidIUIAutomationToggle); tp != nil {
				defer tp.release()
				return int32(uint32(tp.call(patToggle))) >= 0
			}
		}
		return false
	})
}

// SetValue waits up to d for an edit field match accepts and types text
// into it (ValuePattern.SetValue).
func SetValue(match func(Element) bool, text string, d time.Duration) (Element, error) {
	return act(func(e Element) bool { return e.Control == controlTypeEdit && match(e) }, d, func(el *com) bool {
		p := el.out(elemGetCurrentPattern, valuePatternID)
		if p == nil {
			return false
		}
		defer p.release()
		vp := p.queryOrSelf(&iidIUIAutomationValue)
		if vp == nil {
			return false
		}
		defer vp.release()
		u := append(utf16.Encode([]rune(text)), 0)
		b, _, _ := procSysAllocString.Call(uintptr(unsafe.Pointer(&u[0])))
		if b == 0 {
			return false
		}
		defer procSysFreeString.Call(b)
		return int32(uint32(vp.call(patSetValue, b))) >= 0
	})
}

// queryOrSelf is QueryInterface for iid (GetCurrentPattern hands back the
// pattern's own interface already, as an IUnknown).
func (o *com) queryOrSelf(iid *windows.GUID) *com {
	var p *com
	if hr := o.call(slotQueryInterface, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&p))); int32(uint32(hr)) < 0 || p == nil {
		return nil
	}
	return p
}

func act(match func(Element) bool, d time.Duration, do func(el *com) bool) (Element, error) {
	var found Element
	var noPattern bool
	err := retry(d, func(auto *com) (bool, error) {
		ok := false
		err := walk(auto, func(el *com, window string) bool {
			e := describe(el, window)
			if !match(e) {
				return false
			}
			if do(el) {
				found, ok = e, true
				return true
			}
			found, noPattern = e, true
			return false
		})
		return ok, err
	})
	if errors.Is(err, errNotFound) && noPattern {
		return found, fmt.Errorf("%w: %s", errNoPattern, found)
	}
	return found, err
}

func retry(d time.Duration, try func(auto *com) (bool, error)) error {
	deadline := time.Now().Add(d)
	for {
		var ok bool
		err := session(func(auto *com) error {
			var err error
			ok, err = try(auto)
			return err
		})
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return errNotFound
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// Dump lists the top-level windows, and the named elements of the shell's
// windows (CoreWindow, or a name with "otification" in it), at most per
// window each.
func Dump(per int) string {
	var b strings.Builder
	counts := map[string]int{}
	_ = session(func(auto *com) error {
		return walk(auto, func(el *com, window string) bool {
			e := describe(el, window)
			if e.Window == e.Name && counts[window] == 0 {
				fmt.Fprintf(&b, "[%s] %q pid %d\n", e.Class, e.Name, e.PID)
			}
			counts[window]++
			shell := strings.Contains(window, "otification") || strings.Contains(e.Class, "CoreWindow") || strings.Contains(window, "Toast")
			if shell && e.Name != "" && counts[window] <= per {
				fmt.Fprintf(&b, "    %d %q help %q class %q\n", e.Control, e.Name, e.Help, e.Class)
			}
			return false
		})
	})
	return b.String()
}
