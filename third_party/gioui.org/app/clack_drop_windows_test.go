// SPDX-License-Identifier: Unlicense OR MIT

//go:build windows

package app

import (
	stdcontext "context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	syscall "golang.org/x/sys/windows"

	"gioui.org/app/internal/windows"
	"gioui.org/f32"
	"gioui.org/io/system"
	"gioui.org/op"
)

// Clack patch (CLACK_PATCHES.md §11) on a real Windows desktop: a Gio
// window, and files dragged onto it by another process through OLE, the
// way Explorer drags them. The drag moves the real cursor, so it runs only
// where CLACK_WINDOWS_DROP_TESTS is set (ci's windows-applog job).
//
// The dragging process is Windows PowerShell 5.1 (STA) running C#, doing
// what Explorer does: the left button goes down (injected) on a small
// window of its own, then ole32's DoDragDrop with a WinForms DataObject
// (SetFileDropList: CF_HDROP, what Explorer offers) and its own
// IDropSource. A thread moves the cursor onto the Gio window's centre,
// then 10, 5 px on (a DragOver at the new point), each held until the
// drag loop has asked QueryContinueDrag a few times there, and then the
// source ends the drag with DRAGDROP_S_DROP, or DRAGDROP_S_CANCEL (what
// Esc does: the target gets DragLeave). Without the button down, OLE's
// capture never gets the mouse and the drag loop never runs (the first
// CI rounds: QueryContinueDrag never called).
//
// What it proves: RegisterDragDrop took, OLE finds the window under the
// cursor, the Go IDropTarget vtable is called across processes with the
// right argument layout (the POINTL by value, the key state, the effect
// pointer), CF_HDROP is read from a foreign IDataObject, the positions
// come out in client pixels, the effect returned is a copy, and the
// events reach Window.Event in order. What it doesn't: Explorer's own
// drag image and source behaviour, or a real mouse button held through
// the drag.

var (
	testUser32         = syscall.NewLazySystemDLL("user32.dll")
	testClientToScreen = testUser32.NewProc("ClientToScreen")
	testPostMessage    = testUser32.NewProc("PostMessageW")
	testKernel32       = syscall.NewLazySystemDLL("kernel32.dll")
	testGlobalAlloc    = testKernel32.NewProc("GlobalAlloc")
	testGlobalLock     = testKernel32.NewProc("GlobalLock")
	testGlobalUnlock   = testKernel32.NewProc("GlobalUnlock")
)

// dropTestWindow opens a Gio window and returns its HWND, its events and
// a close func.
func dropTestWindow(t *testing.T) (syscall.Handle, <-chan FileDropEvent) {
	t.Helper()
	w := new(Window)
	w.Option(Title("Clack drop test"), Size(480, 360))
	hwnds := make(chan syscall.Handle, 1)
	frames := make(chan struct{}, 1)
	drops := make(chan FileDropEvent, 64)
	done := make(chan struct{})
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
				ops.Reset()
				e.Frame(&ops)
				select {
				case frames <- struct{}{}:
				default:
				}
			case FileDropEvent:
				drops <- e
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
	var hwnd syscall.Handle
	select {
	case hwnd = <-hwnds:
	case <-time.After(30 * time.Second):
		t.Fatal("no Win32ViewEvent")
	}
	select {
	case <-frames:
	case <-time.After(30 * time.Second):
		t.Fatal("no first frame")
	}
	time.Sleep(500 * time.Millisecond) // shown, on top
	return hwnd, drops
}

// clientCentre is the window's client centre, in client and screen pixels.
func clientCentre(hwnd syscall.Handle) (client, screen windows.Point) {
	r := windows.GetClientRect(hwnd)
	client = windows.Point{X: (r.Right - r.Left) / 2, Y: (r.Bottom - r.Top) / 2}
	screen = client
	testClientToScreen.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&screen)))
	return client, screen
}

const oleDragCS = `
using System;
using System.Collections.Specialized;
using System.Diagnostics;
using System.Drawing;
using System.Runtime.InteropServices;
using System.Threading;
using System.Windows.Forms;

[ComImport, Guid("00000121-0000-0000-C000-000000000046"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IClackDropSource {
	[PreserveSig] int QueryContinueDrag(int fEscapePressed, uint grfKeyState);
	[PreserveSig] int GiveFeedback(uint dwEffect);
}

public class ClackDropSource : IClackDropSource {
	int calls;
	public bool Cancel;
	public volatile bool End;
	public int QueryContinueDrag(int esc, uint keys) {
		Interlocked.Increment(ref calls);
		if (End) return Cancel ? 0x00040101 : 0x00040100; // DRAGDROP_S_CANCEL : DRAGDROP_S_DROP
		return 0;
	}
	public int GiveFeedback(uint e) { return 0x00040102; } // DRAGDROP_S_USEDEFAULTCURSORS
	public int Calls { get { return Interlocked.CompareExchange(ref calls, 0, 0); } } // from the mover's thread
}

public static class ClackDrag {
	[StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }
	[DllImport("ole32.dll")] static extern int OleInitialize(IntPtr reserved);
	[DllImport("ole32.dll")] static extern int DoDragDrop(System.Runtime.InteropServices.ComTypes.IDataObject data, IClackDropSource src, int okEffects, out int effect);
	[DllImport("user32.dll")] static extern IntPtr WindowFromPoint(POINT p);
	[DllImport("user32.dll")] static extern bool SetCursorPos(int x, int y);
	[DllImport("user32.dll")] static extern void mouse_event(uint flags, int dx, int dy, uint data, UIntPtr extra);
	const uint LEFTDOWN = 0x2, LEFTUP = 0x4;

	// Run drags files from a small window of this process (at the screen's
	// top left, as Explorer drags from its own window: the button goes
	// down there, so OLE's capture gets the mouse) onto the point x, y,
	// moves 10, 5 px, then drops (or cancels). lateMs holds DoDragDrop
	// back (a slow runner's late drag loop, flake-check).
	public static string Run(int x, int y, string[] files, bool cancel, int lateMs) {
		int ole = OleInitialize(IntPtr.Zero); // DoDragDrop needs OLE, not just an STA
		if (ole < 0) return String.Format("OleInitialize=0x{0:X8}", ole);
		var list = new StringCollection();
		list.AddRange(files);
		var data = new DataObject();
		data.SetFileDropList(list);
		var form = new Form { FormBorderStyle = FormBorderStyle.None, StartPosition = FormStartPosition.Manual,
			Location = new Point(0, 0), Size = new Size(80, 80), TopMost = true, ShowInTaskbar = false };
		form.Show();
		Application.DoEvents();
		SetCursorPos(40, 40);
		Thread.Sleep(100);
		mouse_event(LEFTDOWN, 0, 0, 0, UIntPtr.Zero);
		Thread.Sleep(100);
		Application.DoEvents();
		IntPtr under = WindowFromPoint(new POINT { X = x, Y = y });
		var src = new ClackDropSource { Cancel = cancel };
		var sw = Stopwatch.StartNew();
		bool returned = false;
		// the moves, from a thread while DoDragDrop runs its loop, paced
		// by the loop rather than the clock: OLE asks QueryContinueDrag
		// for each input message and then hit-tests the cursor, so four
		// calls with the cursor on (x, y) mean the target has seen it
		// there (DragEnter), four more on (x+10, y+5) a DragOver there,
		// and then the drop. Moves at fixed times entered at the second
		// point when the loop started late (gap-w13's CI: calls=0 at
		// 1134 ms). A 1 px jitter every 50 ms keeps mouse messages coming,
		// until DoDragDrop returns (the call that ends it needs one too).
		var mover = new Thread(() => {
			int stage = 0, mark = -1;
			for (int i = 0; i < 400 && !Volatile.Read(ref returned); i++) {
				Thread.Sleep(50);
				int bx = stage == 0 ? x : x + 10, by = stage == 0 ? y : y + 5;
				SetCursorPos(bx + (i % 2), by);
				int calls = src.Calls;
				if (mark < 0) {
					mark = calls; // the calls before the cursor was on this point
				} else if (calls >= mark + 4 && !src.End) {
					if (stage == 0) {
						stage = 1;
						mark = -1;
					} else {
						src.End = true;
					}
				}
				if (i % 10 == 0) Console.Error.WriteLine("tick " + sw.ElapsedMilliseconds + "ms stage=" + stage + " calls=" + calls);
			}
			src.End = true; // 20 s without the loop moving on: end it anyway
		});
		mover.IsBackground = true;
		mover.Start();
		if (lateMs > 0) Thread.Sleep(lateMs);
		int effect;
		int hr = DoDragDrop(data, src, 1 | 2 | 4, out effect); // copy | move | link, as Explorer offers
		Volatile.Write(ref returned, true);
		src.End = true;
		mouse_event(LEFTUP, 0, 0, 0, UIntPtr.Zero);
		form.Close();
		return String.Format("hr=0x{0:X8} effect={1} calls={2} under=0x{3:X}", hr, effect, src.Calls, under.ToInt64());
	}
}
`

// lateStartMs is CLACK_OLE_LATE_START_MS: how long the source holds
// DoDragDrop back after its mover starts (0 by default; flake-check sets it
// to replay a drag loop that starts late).
func lateStartMs() int {
	n, _ := strconv.Atoi(os.Getenv("CLACK_OLE_LATE_START_MS"))
	return max(n, 0)
}

func runOLEDrag(t *testing.T, at windows.Point, files []string, cancel bool) string {
	t.Helper()
	var quoted []string
	for _, f := range files {
		quoted = append(quoted, "'"+strings.ReplaceAll(f, "'", "''")+"'")
	}
	script := "$ErrorActionPreference = 'Stop'\n$ProgressPreference = 'SilentlyContinue'\n" +
		"Add-Type -AssemblyName System.Windows.Forms, System.Drawing\n" +
		"Add-Type -ReferencedAssemblies System.Windows.Forms, System.Drawing -TypeDefinition @'\n" + oleDragCS + "\n'@\n" +
		fmt.Sprintf("[ClackDrag]::Run(%d, %d, @(%s), $%v, %d)\n", at.X, at.Y, strings.Join(quoted, ", "), cancel, lateStartMs())
	u := utf16.Encode([]rune(script))
	raw := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(raw[2*i:], c)
	}
	// DoDragDrop runs until the source ends it; a source never asked
	// (no OLE loop) would hang, so the process gets a minute
	ctx, stop := stdcontext.WithTimeout(stdcontext.Background(), time.Minute)
	defer stop()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-STA",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(raw))
	out, err := cmd.CombinedOutput()
	if err != nil {
		// keep going: the events the window got say which side stuck
		t.Errorf("powershell: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// collect takes the window's FileDropEvents until want's last kind arrives.
func collect(t *testing.T, drops <-chan FileDropEvent, last FileDropKind, d time.Duration) []FileDropEvent {
	t.Helper()
	var got []FileDropEvent
	dl := time.After(d)
	for {
		select {
		case e := <-drops:
			got = append(got, e)
			if e.Kind == last {
				return got
			}
		case <-dl:
			return got
		}
	}
}

func dropTestFiles(t *testing.T) []string {
	dir := t.TempDir()
	var files []string
	for _, n := range []string{"shot 1.png", "notes.txt"} {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte(n), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, p)
	}
	return files
}

func near(p f32.Point, q windows.Point, tol float32) bool {
	dx, dy := p.X-float32(q.X), p.Y-float32(q.Y)
	return dx >= -tol && dx <= tol && dy >= -tol && dy <= tol
}

func TestOLEFileDrop(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_DROP_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_DROP_TESTS not set (the test moves the cursor)")
	}
	hwnd, drops := dropTestWindow(t)
	files := dropTestFiles(t)
	client, screen := clientCentre(hwnd)
	if w, ok := winMap.Load(hwnd); !ok || w.(*window).drop == nil {
		t.Fatal("the window has no OLE drop target (RegisterDragDrop failed; WM_DROPFILES only)")
	}

	t.Run("drop", func(t *testing.T) {
		out := runOLEDrag(t, screen, files, false)
		t.Logf("DoDragDrop: %s", out)
		got := collect(t, drops, FileDrop, 15*time.Second)
		for _, e := range got {
			t.Logf("  %v at %v paths %q mods %v", e.Kind, e.Position, e.Paths, e.Modifiers)
		}
		if len(got) < 2 || got[0].Kind != FileDragEnter || got[len(got)-1].Kind != FileDrop {
			t.Fatalf("events %v, want FileDragEnter … FileDrop", kinds(got))
		}
		if !reflect.DeepEqual(got[0].Paths, files) {
			t.Errorf("enter paths %q, want %q", got[0].Paths, files)
		}
		drop := got[len(got)-1]
		if !reflect.DeepEqual(drop.Paths, files) {
			t.Errorf("drop paths %q, want %q", drop.Paths, files)
		}
		if !near(got[0].Position, client, 2) {
			t.Errorf("enter at %v, want the client centre %v", got[0].Position, client)
		}
		// the source moved the cursor by (10, 5) before dropping
		if !near(drop.Position, windows.Point{X: client.X + 10, Y: client.Y + 5}, 2) {
			t.Errorf("drop at %v, want %v + (10, 5)", drop.Position, client)
		}
		over := false
		for _, e := range got[1 : len(got)-1] {
			over = over || e.Kind == FileDragOver
			if e.Kind != FileDragOver {
				t.Errorf("a %v between enter and drop", e.Kind)
			}
		}
		if !over {
			t.Error("no FileDragOver while the cursor moved")
		}
		if !strings.Contains(out, "hr=0x00040100") || !strings.Contains(out, "effect=1") {
			t.Errorf("DoDragDrop %s, want hr DRAGDROP_S_DROP and effect DROPEFFECT_COPY (1)", out)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		out := runOLEDrag(t, screen, files, true)
		t.Logf("DoDragDrop: %s", out)
		got := collect(t, drops, FileDragLeave, 15*time.Second)
		if len(got) < 2 || got[0].Kind != FileDragEnter || got[len(got)-1].Kind != FileDragLeave {
			t.Fatalf("events %v, want FileDragEnter … FileDragLeave", kinds(got))
		}
		for _, e := range got {
			if e.Kind == FileDrop {
				t.Fatal("a FileDrop on a cancelled drag")
			}
		}
	})
}

// TestWMDropFiles posts WM_DROPFILES with an HDROP built here (one
// process: an HGLOBAL means nothing in another), as DragAcceptFiles' old
// shell drop delivers it.
func TestWMDropFiles(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_DROP_TESTS") == "" {
		t.Skip("CLACK_WINDOWS_DROP_TESTS not set")
	}
	hwnd, drops := dropTestWindow(t)
	files := dropTestFiles(t)
	h := buildHDROP(t, files, windows.Point{X: 33, Y: 44})
	if r, _, err := testPostMessage.Call(uintptr(hwnd), wmDropFiles, h, 0); r == 0 {
		t.Fatalf("PostMessage: %v", err)
	}
	got := collect(t, drops, FileDrop, 10*time.Second)
	if len(got) != 1 || got[0].Kind != FileDrop {
		t.Fatalf("events %v, want one FileDrop", kinds(got))
	}
	if !reflect.DeepEqual(got[0].Paths, files) {
		t.Errorf("paths %q, want %q", got[0].Paths, files)
	}
	if got[0].Position != f32.Pt(33, 44) {
		t.Errorf("at %v, want (33, 44) (DROPFILES.pt, client pixels)", got[0].Position)
	}
}

// buildHDROP is a DROPFILES block (wide names, double-NUL ended).
func buildHDROP(t *testing.T, files []string, pt windows.Point) uintptr {
	var names []uint16
	for _, f := range files {
		names = append(names, utf16.Encode([]rune(f))...)
		names = append(names, 0)
	}
	names = append(names, 0)
	const hdr = 20 // DROPFILES: pFiles, pt, fNC, fWide
	size := hdr + 2*len(names)
	h, _, err := testGlobalAlloc.Call(0x0042, uintptr(size)) // GMEM_MOVEABLE | GMEM_ZEROINIT
	if h == 0 {
		t.Fatalf("GlobalAlloc: %v", err)
	}
	p, _, _ := testGlobalLock.Call(h)
	buf := unsafe.Slice((*byte)(unsafe.Pointer(p)), size)
	binary.LittleEndian.PutUint32(buf[0:], hdr)
	binary.LittleEndian.PutUint32(buf[4:], uint32(pt.X))
	binary.LittleEndian.PutUint32(buf[8:], uint32(pt.Y))
	binary.LittleEndian.PutUint32(buf[12:], 0) // fNC: client coordinates
	binary.LittleEndian.PutUint32(buf[16:], 1) // fWide
	for i, c := range names {
		binary.LittleEndian.PutUint16(buf[hdr+2*i:], c)
	}
	testGlobalUnlock.Call(h)
	return h
}

func kinds(es []FileDropEvent) []FileDropKind {
	var k []FileDropKind
	for _, e := range es {
		k = append(k, e.Kind)
	}
	return k
}
