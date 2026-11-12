// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package app

import (
	"sync"
	stdsyscall "syscall"
	"unsafe"

	syscall "golang.org/x/sys/windows"

	"gioui.org/app/internal/windows"
	"gioui.org/f32"
	"gioui.org/io/key"
)

// Clack patch (CLACK_PATCHES.md §11): file drops on Windows.
//
// The window registers an OLE drop target (RegisterDragDrop), as
// Chromium's DropTargetWin does for every browser window: an IDropTarget
// whose vtable is Go callbacks, no cgo. DragEnter, DragOver, DragLeave and
// Drop become FileDropEvents when the data object has CF_HDROP (files from
// Explorer, a zip folder's extracted files, a browser's downloads bar).
// The shell's drag image keeps drawing over the window through
// IDropTargetHelper, as it does over Chromium (DropTargetWin calls it on
// every method).
//
// WM_DROPFILES (DragAcceptFiles) is the fallback when OLE can't start on
// the window's thread; it only knows the drop, so no hover. The window
// handles it either way, as one FileDrop.

var (
	ole32             = syscall.NewLazySystemDLL("ole32.dll")
	_OleInitialize    = ole32.NewProc("OleInitialize")
	_RegisterDragDrop = ole32.NewProc("RegisterDragDrop")
	_RevokeDragDrop   = ole32.NewProc("RevokeDragDrop")
	_ReleaseStgMedium = ole32.NewProc("ReleaseStgMedium")
	_CoCreateInstance = ole32.NewProc("CoCreateInstance")

	shell32          = syscall.NewLazySystemDLL("shell32.dll")
	_DragAcceptFiles = shell32.NewProc("DragAcceptFiles")
	_DragFinish      = shell32.NewProc("DragFinish")
	_DragQueryFileW  = shell32.NewProc("DragQueryFileW")
	_DragQueryPoint  = shell32.NewProc("DragQueryPoint")
)

const (
	cfHDROP          = 15
	dvaspectContent  = 1
	tymedHGlobal     = 1
	dropEffectNone   = 0
	dropEffectCopy   = 1
	mkShift          = 0x4
	mkControl        = 0x8
	mkAlt            = 0x20
	wmDropFiles      = 0x0233
	clsctxInproc     = 0x1
	eNoInterface     = 0x80004002
	comSOK           = 0
	dragQueryAllFile = 0xFFFFFFFF
)

var (
	iidIUnknown          = syscall.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIDropTarget       = syscall.GUID{Data1: 0x00000122, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	clsidDragDropHelper  = syscall.GUID{Data1: 0x4657278A, Data2: 0x411B, Data3: 0x11D2, Data4: [8]byte{0x83, 0x9A, 0x00, 0xC0, 0x4F, 0xD9, 0x18, 0xD0}}
	iidIDropTargetHelper = syscall.GUID{Data1: 0x4657278B, Data2: 0x411B, Data3: 0x11D2, Data4: [8]byte{0x83, 0x9A, 0x00, 0xC0, 0x4F, 0xD9, 0x18, 0xD0}}
)

// formatEtc and stgMedium are FORMATETC and STGMEDIUM.
type formatEtc struct {
	cfFormat uint16
	ptd      uintptr
	aspect   uint32
	lindex   int32
	tymed    uint32
}

type stgMedium struct {
	tymed         uint32
	data          uintptr
	unkForRelease uintptr
}

// dropTarget is the COM object: its first word points at the vtable.
type dropTarget struct {
	vtbl   *[7]uintptr
	w      *window
	hwnd   syscall.Handle
	helper uintptr // IDropTargetHelper, or 0
	files  bool    // the drag in progress carries files
	effect uint32
}

var (
	dropVtblOnce sync.Once
	dropVtbl     [7]uintptr
	// dropTargets maps a COM this pointer to its target (OLE calls back
	// with the address; the map keeps the object alive while registered).
	dropTargets sync.Map
)

func initDropVtbl() {
	enter, over, drop := dropCallbacks()
	dropVtbl = [7]uintptr{
		syscall.NewCallback(dtQueryInterface),
		syscall.NewCallback(dtAddRef),
		syscall.NewCallback(dtRelease),
		enter,
		over,
		syscall.NewCallback(dtDragLeave),
		drop,
	}
}

// registerFileDrop makes w's HWND an OLE drop target. It runs on the
// window's thread, which OLE needs initialized as a single-threaded
// apartment.
func (w *window) registerFileDrop() {
	if r, _, _ := _OleInitialize.Call(0); int32(r) < 0 {
		_DragAcceptFiles.Call(uintptr(w.hwnd), 1)
		return
	}
	dropVtblOnce.Do(initDropVtbl)
	t := &dropTarget{vtbl: &dropVtbl, w: w, hwnd: w.hwnd}
	this := uintptr(unsafe.Pointer(t))
	dropTargets.Store(this, t)
	if r, _, _ := _RegisterDragDrop.Call(uintptr(w.hwnd), this); r != comSOK {
		dropTargets.Delete(this)
		_DragAcceptFiles.Call(uintptr(w.hwnd), 1)
		return
	}
	var helper uintptr
	if r, _, _ := _CoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidDragDropHelper)), 0, clsctxInproc,
		uintptr(unsafe.Pointer(&iidIDropTargetHelper)), uintptr(unsafe.Pointer(&helper))); r == comSOK {
		t.helper = helper
	}
	w.drop = t
}

// revokeFileDrop undoes registerFileDrop (WM_DESTROY).
func (w *window) revokeFileDrop() {
	t := w.drop
	if t == nil {
		return
	}
	w.drop = nil
	_RevokeDragDrop.Call(uintptr(t.hwnd))
	if t.helper != 0 {
		comCall(t.helper, 2) // Release
		t.helper = 0
	}
	dropTargets.Delete(uintptr(unsafe.Pointer(t)))
}

func lookupDropTarget(this uintptr) *dropTarget {
	if v, ok := dropTargets.Load(this); ok {
		return v.(*dropTarget)
	}
	return nil
}

// comCall calls method i of the COM object obj.
func comCall(obj uintptr, i int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(i)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := stdsyscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

func dtQueryInterface(this, riid, ppv uintptr) uintptr {
	if ppv == 0 {
		return eNoInterface
	}
	id := *(*syscall.GUID)(unsafe.Pointer(riid))
	if id == iidIUnknown || id == iidIDropTarget {
		*(*uintptr)(unsafe.Pointer(ppv)) = this
		return comSOK
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = 0
	return eNoInterface
}

// The window owns the object for as long as it is registered, so the
// reference count is nominal.
func dtAddRef(this uintptr) uintptr  { return 1 }
func dtRelease(this uintptr) uintptr { return 1 }

func dtDragEnter(this, data uintptr, keys uint32, pt windows.Point, pEffect uintptr) uintptr {
	t := lookupDropTarget(this)
	if t == nil {
		return comSOK
	}
	paths := hdropPaths(data)
	t.files = len(paths) > 0
	t.effect = dropEffectNone
	if t.files && pEffect != 0 && *(*uint32)(unsafe.Pointer(pEffect))&dropEffectCopy != 0 {
		t.effect = dropEffectCopy
	}
	if pEffect != 0 {
		*(*uint32)(unsafe.Pointer(pEffect)) = t.effect
	}
	if t.helper != 0 {
		comCall(t.helper, 3, uintptr(t.hwnd), data, uintptr(unsafe.Pointer(&pt)), uintptr(t.effect))
	}
	if t.files {
		t.send(FileDragEnter, pt, keys, paths)
	}
	return comSOK
}

func dtDragOver(this uintptr, keys uint32, pt windows.Point, pEffect uintptr) uintptr {
	t := lookupDropTarget(this)
	if t == nil {
		return comSOK
	}
	if pEffect != 0 {
		*(*uint32)(unsafe.Pointer(pEffect)) = t.effect
	}
	if t.helper != 0 {
		comCall(t.helper, 5, uintptr(unsafe.Pointer(&pt)), uintptr(t.effect))
	}
	if t.files {
		t.send(FileDragOver, pt, keys, nil)
	}
	return comSOK
}

func dtDragLeave(this uintptr) uintptr {
	t := lookupDropTarget(this)
	if t == nil {
		return comSOK
	}
	if t.helper != 0 {
		comCall(t.helper, 4)
	}
	if t.files {
		t.files = false
		t.w.ProcessEvent(FileDropEvent{Kind: FileDragLeave})
	}
	return comSOK
}

func dtDrop(this, data uintptr, keys uint32, pt windows.Point, pEffect uintptr) uintptr {
	t := lookupDropTarget(this)
	if t == nil {
		return comSOK
	}
	paths := hdropPaths(data)
	if pEffect != 0 {
		*(*uint32)(unsafe.Pointer(pEffect)) = t.effect
	}
	if t.helper != 0 {
		comCall(t.helper, 6, data, uintptr(unsafe.Pointer(&pt)), uintptr(t.effect))
	}
	had := t.files
	t.files = false
	if had && len(paths) > 0 {
		t.send(FileDrop, pt, keys, paths)
	} else if had {
		t.w.ProcessEvent(FileDropEvent{Kind: FileDragLeave})
	}
	return comSOK
}

// send delivers a FileDropEvent at the screen point pt.
func (t *dropTarget) send(kind FileDropKind, pt windows.Point, keys uint32, paths []string) {
	windows.ScreenToClient(t.hwnd, &pt)
	var mods key.Modifiers
	if keys&mkShift != 0 {
		mods |= key.ModShift
	}
	if keys&mkControl != 0 {
		mods |= key.ModCtrl
	}
	if keys&mkAlt != 0 {
		mods |= key.ModAlt
	}
	t.w.ProcessEvent(FileDropEvent{Kind: kind, Position: f32.Pt(float32(pt.X), float32(pt.Y)), Paths: paths, Modifiers: mods})
}

// hdropPaths reads the CF_HDROP file list of an IDataObject, or nil.
func hdropPaths(obj uintptr) []string {
	if obj == 0 {
		return nil
	}
	fe := formatEtc{cfFormat: cfHDROP, aspect: dvaspectContent, lindex: -1, tymed: tymedHGlobal}
	var sm stgMedium
	if r := comCall(obj, 3, uintptr(unsafe.Pointer(&fe)), uintptr(unsafe.Pointer(&sm))); r != comSOK { // GetData
		return nil
	}
	defer _ReleaseStgMedium.Call(uintptr(unsafe.Pointer(&sm)))
	if sm.tymed != tymedHGlobal || sm.data == 0 {
		return nil
	}
	return dragQueryFiles(sm.data)
}

// dragQueryFiles lists an HDROP's files.
func dragQueryFiles(h uintptr) []string {
	n, _, _ := _DragQueryFileW.Call(h, dragQueryAllFile, 0, 0)
	var paths []string
	for i := uintptr(0); i < n; i++ {
		l, _, _ := _DragQueryFileW.Call(h, i, 0, 0)
		if l == 0 {
			continue
		}
		buf := make([]uint16, l+1)
		_DragQueryFileW.Call(h, i, uintptr(unsafe.Pointer(&buf[0])), l+1)
		if p := syscall.UTF16ToString(buf); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

// dropFiles handles WM_DROPFILES: one FileDrop at the drop point.
func (w *window) dropFiles(h uintptr) {
	paths := dragQueryFiles(h)
	var pt windows.Point
	_DragQueryPoint.Call(h, uintptr(unsafe.Pointer(&pt))) // client coordinates
	_DragFinish.Call(h)
	if len(paths) > 0 {
		w.ProcessEvent(FileDropEvent{Kind: FileDrop, Position: f32.Pt(float32(pt.X), float32(pt.Y)), Paths: paths, Modifiers: getModifiers()})
	}
}
