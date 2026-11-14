//go:build windows

package desktop

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The taskbar button's jump list, as Slack desktop's JumpListHandler makes
// it (main.bundle.cjs 4.53.276 win32, class v_ @1174233, createJumpList
// @1174947) through Electron's app.setJumpList and getJumpListSettings
// (shell/browser/api/electron_api_app.cc, shell/browser/ui/win/jump_list.cc
// at Electron main):
//
//   - JumpList::Begin: an ICustomDestinationList (CLSID_DestinationList),
//     SetAppID with the app's AppUserModelID, then BeginList, whose
//     removed objects are getJumpListSettings' removedItems: the tasks the
//     user took off the list;
//   - AppendCategory per custom category, skipped when it has no items:
//     each task an IShellLinkW (SetPath, SetArguments, SetWorkingDirectory,
//     SetDescription, refused past 260 characters, SetIconLocation, and
//     PKEY_Title through its IPropertyStore) in an
//     EnumerableObjectCollection, appended under the category's name;
//   - CommitList, even after a category failed ("it's better to have
//     something than nothing").
//
// Clack's process sets no AppUserModelID (aumid_windows.go: an explicit id
// would split taskbar pins), so its list belongs to the process's own id,
// the one its taskbar button has: an appID of "" skips SetAppID.

var (
	clsidDestinationList            = guid(0x77F10CF0, 0x3DB5, 0x4966, 0xB5, 0x20, 0xB7, 0xC5, 0x4F, 0xD3, 0x5E, 0xD6)
	iidICustomDestinationList       = guid(0x6332DEBF, 0x87B5, 0x4670, 0x90, 0xC0, 0x5E, 0x57, 0xB4, 0x08, 0xA4, 0x9E)
	clsidEnumerableObjectCollection = guid(0x2D3468C1, 0x36A7, 0x43B6, 0xAC, 0x24, 0xD3, 0xF0, 0x2F, 0xD9, 0x60, 0x7A)
	iidIObjectCollection            = guid(0x5632B1A4, 0xE38A, 0x400A, 0x92, 0x8A, 0xD4, 0xCD, 0x63, 0x23, 0x02, 0x95)
	iidIObjectArray                 = guid(0x92CA9DCD, 0x5622, 0x4BBA, 0xA8, 0x05, 0x5E, 0x9F, 0x54, 0x1B, 0xD8, 0xC9)
	clsidShellLink                  = guid(0x00021401, 0x0000, 0x0000, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46)
	iidIShellLinkW                  = guid(0x000214F9, 0x0000, 0x0000, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46)
	iidIPropertyStore               = guid(0x886D8EEB, 0x8CF2, 0x4446, 0x8D, 0x02, 0xCD, 0xBA, 0x1D, 0xBD, 0xCF, 0x99)

	pkeyTitle = propertyKey{guid(0xF29F85E0, 0x4FF9, 0x1068, 0xAB, 0x91, 0x08, 0x00, 0x2B, 0x27, 0xB3, 0xD9), 2}
)

// Vtable slots past IUnknown's three.
const (
	// ICustomDestinationList
	vtDestSetAppID      = 3
	vtDestBeginList     = 4
	vtDestAppendCat     = 5
	vtDestCommitList    = 8
	vtDestDeleteList    = 10
	vtDestAbortList     = 11
	vtObjArrayGetCount  = 3 // IObjectArray, and IObjectCollection's base
	vtObjArrayGetAt     = 4
	vtObjCollAddObject  = 5
	vtLinkGetPath       = 3 // IShellLinkW
	vtLinkSetDesc       = 7
	vtLinkSetWorkingDir = 9
	vtLinkGetArguments  = 10
	vtLinkSetArguments  = 11
	vtLinkSetIconLoc    = 17
	vtLinkSetPath       = 20
	vtPropSetValue      = 6 // IPropertyStore
	vtPropCommit        = 7
)

const (
	clsctxInprocServer = 1
	vtLPWSTR           = 31
	sFalse             = 1
)

type propertyKey struct {
	FmtID windows.GUID
	PID   uint32
}

// propVariant is PROPVARIANT holding a pointer (VT_LPWSTR): 24 bytes on
// 64-bit, 16 on 32-bit.
type propVariant struct {
	VT  uint16
	_   [3]uint16
	Val uintptr
	_   uintptr
}

// JumpListTask is a jump list task (Electron's JumpListItem of type
// "task"): what the shell runs and how it shows it.
type JumpListTask struct {
	Title       string
	Description string
	Program     string
	Args        string
	IconPath    string
	IconIndex   int // negative: the resource id in IconPath
}

// JumpListCategory is a custom category (type "custom").
type JumpListCategory struct {
	Name  string
	Items []JumpListTask
}

// errJumpListCategory is a category the shell refused; the error says which.
var errJumpListCategory = errors.New("desktop: jump list category")

// SetJumpList is app.setJumpList(categories) for custom categories of
// tasks, for appID ("": the process's own). The list is committed even if
// a category fails; the error is the first failure.
func SetJumpList(appID string, cats []JumpListCategory) error {
	return comThread(func() error {
		dest, _, err := beginJumpList(appID, false)
		if err != nil {
			return err
		}
		defer dest.release()
		var first error
		for _, c := range cats {
			if err := appendJumpListCategory(dest, c); err != nil && first == nil {
				first = err
			}
		}
		if hr := dest.call(vtDestCommitList); failed(hr) {
			if first == nil {
				first = fmt.Errorf("ICustomDestinationList::CommitList: %w", HResultError(hr))
			}
		}
		return first
	})
}

// JumpListRemoved is getJumpListSettings().removedItems' tasks: what the
// user removed from appID's list since it was last set (BeginList, then
// AbortList so nothing changes).
func JumpListRemoved(appID string) ([]JumpListTask, error) {
	var out []JumpListTask
	err := comThread(func() error {
		dest, removed, err := beginJumpList(appID, true)
		if err != nil {
			return err
		}
		defer dest.release()
		out = removed
		dest.call(vtDestAbortList)
		return nil
	})
	return out, err
}

// DeleteJumpList is app.setJumpList(null): ICustomDestinationList::DeleteList.
func DeleteJumpList(appID string) error {
	return comThread(func() error {
		dest, err := newDestinationList()
		if err != nil {
			return err
		}
		defer dest.release()
		id, err := optUTF16(appID)
		if err != nil {
			return err
		}
		if hr := dest.call(vtDestDeleteList, uintptr(unsafe.Pointer(id))); failed(hr) {
			return fmt.Errorf("ICustomDestinationList::DeleteList: %w", HResultError(hr))
		}
		return nil
	})
}

// comThread runs f on an OS thread of its own in a single-threaded
// apartment (the thread ends with it).
func comThread(f func() error) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread() // never unlocked: the thread goes with the goroutine
		if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil && err != syscall.Errno(sFalse) {
			done <- fmt.Errorf("desktop: CoInitializeEx: %w", err)
			return
		}
		defer windows.CoUninitialize()
		done <- f()
	}()
	return <-done
}

func newDestinationList() (*rtObject, error) {
	var p *rtObject
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidDestinationList)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidICustomDestinationList)), uintptr(unsafe.Pointer(&p)))
	if failed(hr) || p == nil {
		return nil, fmt.Errorf("CoCreateInstance(CLSID_DestinationList): %w", HResultError(hr))
	}
	return p, nil
}

// optUTF16 is s as a NUL-terminated string, nil for "".
func optUTF16(s string) (*uint16, error) {
	if s == "" {
		return nil, nil
	}
	return windows.UTF16PtrFromString(s)
}

// beginJumpList is JumpList::Begin: the list's transaction begun, and with
// wantRemoved its removed tasks.
func beginJumpList(appID string, wantRemoved bool) (*rtObject, []JumpListTask, error) {
	dest, err := newDestinationList()
	if err != nil {
		return nil, nil, err
	}
	if appID != "" {
		id, err := windows.UTF16PtrFromString(appID)
		if err != nil {
			dest.release()
			return nil, nil, err
		}
		if hr := dest.call(vtDestSetAppID, uintptr(unsafe.Pointer(id))); failed(hr) {
			dest.release()
			return nil, nil, fmt.Errorf("ICustomDestinationList::SetAppID: %w", HResultError(hr))
		}
	}
	var minSlots uint32
	var removed *rtObject
	if hr := dest.call(vtDestBeginList, uintptr(unsafe.Pointer(&minSlots)), uintptr(unsafe.Pointer(&iidIObjectArray)),
		uintptr(unsafe.Pointer(&removed))); failed(hr) || removed == nil {
		dest.release()
		return nil, nil, fmt.Errorf("ICustomDestinationList::BeginList: %w", HResultError(hr))
	}
	defer removed.release()
	if !wantRemoved {
		return dest, nil, nil
	}
	var n uint32
	removed.call(vtObjArrayGetCount, uintptr(unsafe.Pointer(&n)))
	var out []JumpListTask
	for i := uint32(0); i < n; i++ {
		var link *rtObject
		if hr := removed.call(vtObjArrayGetAt, uintptr(i), uintptr(unsafe.Pointer(&iidIShellLinkW)), uintptr(unsafe.Pointer(&link))); failed(hr) || link == nil {
			continue // a file (IShellItem): Clack's list has none
		}
		out = append(out, JumpListTask{Program: linkString(link, vtLinkGetPath, 260), Args: linkString(link, vtLinkGetArguments, 32768)})
		link.release()
	}
	return dest, out, nil
}

// linkString reads one of IShellLinkW's string getters (GetPath takes two
// more arguments, null here).
func linkString(link *rtObject, slot, size int) string {
	buf := make([]uint16, size)
	args := []uintptr{uintptr(unsafe.Pointer(&buf[0])), uintptr(size)}
	if slot == vtLinkGetPath {
		args = append(args, 0, 0)
	}
	if hr := link.call(slot, args...); failed(hr) {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// appendJumpListCategory is JumpList::AppendCategory for a custom category.
func appendJumpListCategory(dest *rtObject, c JumpListCategory) error {
	if len(c.Items) == 0 {
		return nil
	}
	var coll *rtObject
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidEnumerableObjectCollection)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIObjectCollection)), uintptr(unsafe.Pointer(&coll)))
	if failed(hr) || coll == nil {
		return fmt.Errorf("CoCreateInstance(CLSID_EnumerableObjectCollection): %w", HResultError(hr))
	}
	defer coll.release()
	var first error
	added := 0
	for _, t := range c.Items {
		if err := appendJumpListTask(coll, t); err != nil {
			if first == nil {
				first = fmt.Errorf("task %q: %w", t.Title, err)
			}
			continue
		}
		added++
	}
	if added == 0 {
		return first
	}
	arr, err := coll.query(&iidIObjectArray)
	if err != nil {
		return fmt.Errorf("IObjectCollection as IObjectArray: %w", err)
	}
	defer arr.release()
	name, err := windows.UTF16PtrFromString(c.Name)
	if err != nil {
		return err
	}
	if hr := dest.call(vtDestAppendCat, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(arr))); failed(hr) {
		why := ""
		switch uint32(hr) {
		case 0x80070005: // E_ACCESSDENIED
			why = " (system privacy settings: recently opened items in Jump Lists are off)"
		case 0x80040F03:
			why = " (missing file type registration)"
		}
		return fmt.Errorf("%w %q%s: %w", errJumpListCategory, c.Name, why, HResultError(hr))
	}
	return first
}

// appendJumpListTask is AppendTask: the task as an IShellLinkW added to coll.
func appendJumpListTask(coll *rtObject, t JumpListTask) error {
	var link *rtObject
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIShellLinkW)), uintptr(unsafe.Pointer(&link)))
	if failed(hr) || link == nil {
		return fmt.Errorf("CoCreateInstance(CLSID_ShellLink): %w", HResultError(hr))
	}
	defer link.release()
	set := func(slot int, s, what string) error {
		u, err := windows.UTF16PtrFromString(s)
		if err != nil {
			return err
		}
		if hr := link.call(slot, uintptr(unsafe.Pointer(u))); failed(hr) {
			return fmt.Errorf("IShellLinkW::%s: %w", what, HResultError(hr))
		}
		return nil
	}
	for _, s := range []struct {
		slot      int
		val, what string
	}{
		{vtLinkSetPath, t.Program, "SetPath"},
		{vtLinkSetArguments, t.Args, "SetArguments"},
		{vtLinkSetWorkingDir, "", "SetWorkingDirectory"},
		{vtLinkSetDesc, t.Description, "SetDescription"},
	} {
		if err := set(s.slot, s.val, s.what); err != nil {
			return err
		}
	}
	// SetDescription takes up to INFOTIPSIZE, but past 260 characters the
	// task fails silently (Electron refuses those itself).
	if len(windows.StringToUTF16(t.Description))-1 > 260 {
		return errors.New("a description over 260 characters")
	}
	if t.IconPath != "" {
		u, err := windows.UTF16PtrFromString(t.IconPath)
		if err != nil {
			return err
		}
		if hr := link.call(vtLinkSetIconLoc, uintptr(unsafe.Pointer(u)), uintptr(t.IconIndex)); failed(hr) {
			return fmt.Errorf("IShellLinkW::SetIconLocation: %w", HResultError(hr))
		}
	}
	ps, err := link.query(&iidIPropertyStore)
	if err != nil {
		return fmt.Errorf("IShellLinkW as IPropertyStore: %w", err)
	}
	defer ps.release()
	title, err := windows.UTF16FromString(t.Title)
	if err != nil {
		return err
	}
	pv := propVariant{VT: vtLPWSTR, Val: uintptr(unsafe.Pointer(&title[0]))} // SetValue copies it
	hr = ps.call(vtPropSetValue, uintptr(unsafe.Pointer(&pkeyTitle)), uintptr(unsafe.Pointer(&pv)))
	runtime.KeepAlive(title)
	if failed(hr) {
		return fmt.Errorf("IPropertyStore::SetValue(PKEY_Title): %w", HResultError(hr))
	}
	if hr := ps.call(vtPropCommit); failed(hr) {
		return fmt.Errorf("IPropertyStore::Commit: %w", HResultError(hr))
	}
	if hr := coll.call(vtObjCollAddObject, uintptr(unsafe.Pointer(link))); failed(hr) {
		return fmt.Errorf("IObjectCollection::AddObject: %w", HResultError(hr))
	}
	return nil
}
