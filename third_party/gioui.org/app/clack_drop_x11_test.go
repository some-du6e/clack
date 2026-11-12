// SPDX-License-Identifier: Unlicense OR MIT

//go:build linux && !android && !nox11

package app

import (
	stdcontext "context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/system"
	"gioui.org/op"
)

// Clack patch (CLACK_PATCHES.md §11) on a real X server (ci's linux job
// runs it under Xvfb with CLACK_X11_DROP_TESTS set): python-xlib plays a
// file manager's XDND source against a Gio window. It owns XdndSelection,
// sends XdndEnter (text/uri-list), two XdndPositions in root coordinates,
// then XdndDrop or XdndLeave, answers the window's SelectionRequest with
// the file: URIs and waits for XdndStatus and XdndFinished.
//
// What it proves: XdndAware is set, the client messages are parsed, the
// root point is translated to the window's, XdndStatus accepts with
// XdndActionCopy, the selection conversion and its SelectionNotify bring
// the paths (percent-decoded), and XdndFinished reports the copy. What it
// doesn't: a real file manager's timing or a window manager's frames.

const xdndSourcePy = `
import sys, time
from Xlib import X, display, Xatom
from Xlib.protocol import event

target_id, cx, cy, mode = int(sys.argv[1]), int(sys.argv[2]), int(sys.argv[3]), sys.argv[4]
uris = sys.argv[5:]
d = display.Display()
root = d.screen().root
A = lambda n: d.intern_atom(n)
aware, enter, position, status, leave, drop, finished = [A(n) for n in
    ("XdndAware", "XdndEnter", "XdndPosition", "XdndStatus", "XdndLeave", "XdndDrop", "XdndFinished")]
selection, uri_list, copy = A("XdndSelection"), A("text/uri-list"), A("XdndActionCopy")
target = d.create_resource_object("window", target_id)
src = root.create_window(0, 0, 1, 1, 0, X.CopyFromParent)
src.set_selection_owner(selection, X.CurrentTime)
p = target.get_full_property(aware, Xatom.ATOM)
print("XdndAware", list(p.value) if p else None)
def send(typ, data):
    target.send_event(event.ClientMessage(window=target, client_type=typ, data=(32, data + [0] * (5 - len(data)))))
    d.flush()
def wait(typ, secs=5):
    end = time.time() + secs
    while time.time() < end:
        while d.pending_events():
            e = d.next_event()
            if e.type == X.SelectionRequest:
                body = ("\r\n".join(uris) + "\r\n").encode()
                e.requestor.change_property(e.property, e.target, 8, body)
                e.requestor.send_event(event.SelectionNotify(time=e.time, requestor=e.requestor,
                    selection=e.selection, target=e.target, property=e.property))
                d.flush()
                print("SelectionRequest", d.get_atom_name(e.target))
            elif e.type == X.ClientMessage and e.client_type == typ:
                return e.data[1]
        time.sleep(0.01)
    return None
def rootpt(x, y):
    t = root.translate_coords(target, x, y)
    return (t.x << 16) | (t.y & 0xffff)
send(enter, [src.id, 5 << 24, uri_list])
send(position, [src.id, 0, rootpt(cx, cy), X.CurrentTime, copy])
s1 = wait(status)
print("XdndStatus", (s1[1] & 1) if s1 else None, s1[4] == copy if s1 else None)
time.sleep(0.2)
send(position, [src.id, 0, rootpt(cx + 10, cy + 5), X.CurrentTime, copy])
s2 = wait(status)
time.sleep(0.2)
if mode == "leave":
    send(leave, [src.id])
    time.sleep(0.5)
    print("left")
else:
    send(drop, [src.id, 0, X.CurrentTime])
    f = wait(finished, 10)
    print("XdndFinished", f[1] if f else None, f[2] == copy if f else None)
`

func xdndTestWindow(t *testing.T) (uintptr, <-chan FileDropEvent) {
	t.Helper()
	w := new(Window)
	w.Option(Title("Clack XDND test"), Size(480, 360))
	views := make(chan uintptr, 1)
	frames := make(chan struct{}, 1)
	drops := make(chan FileDropEvent, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var ops op.Ops
		for {
			switch e := w.Event().(type) {
			case X11ViewEvent:
				if e.Window != 0 {
					select {
					case views <- e.Window:
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
				if e.Err != nil {
					t.Logf("window: %v", e.Err)
				}
				return
			}
		}
	}()
	t.Cleanup(func() {
		w.Perform(system.ActionClose)
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	})
	var win uintptr
	select {
	case win = <-views:
	case <-time.After(30 * time.Second):
		t.Fatal("no X11ViewEvent (is this X11?)")
	}
	select {
	case <-frames:
	case <-time.After(30 * time.Second):
		t.Fatal("no first frame")
	}
	return win, drops
}

func runXDNDSource(t *testing.T, win uintptr, at f32.Point, mode string, uris []string) string {
	t.Helper()
	dir := t.TempDir()
	py := filepath.Join(dir, "xdnd_source.py")
	if err := os.WriteFile(py, []byte(xdndSourcePy), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, stop := stdcontext.WithTimeout(stdcontext.Background(), time.Minute)
	defer stop()
	args := append([]string{"-I", py, strconv.FormatUint(uint64(win), 10),
		strconv.Itoa(int(at.X)), strconv.Itoa(int(at.Y)), mode}, uris...)
	out, err := exec.CommandContext(ctx, "python3", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("python3 xdnd source: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func collectX(drops <-chan FileDropEvent, last FileDropKind, d time.Duration) []FileDropEvent {
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

func TestXDNDFileDrop(t *testing.T) {
	if os.Getenv("CLACK_X11_DROP_TESTS") == "" || os.Getenv("DISPLAY") == "" {
		t.Skip("CLACK_X11_DROP_TESTS or DISPLAY not set")
	}
	os.Unsetenv("WAYLAND_DISPLAY") // X11, not Wayland
	win, drops := xdndTestWindow(t)
	at := f32.Pt(200, 150)
	uris := []string{"file:///tmp/clack%20drop/shot%201.png", "file:///tmp/notes.txt"}
	want := []string{"/tmp/clack drop/shot 1.png", "/tmp/notes.txt"}

	t.Run("drop", func(t *testing.T) {
		out := runXDNDSource(t, win, at, "drop", uris)
		t.Logf("source:\n%s", out)
		got := collectX(drops, FileDrop, 10*time.Second)
		for _, e := range got {
			t.Logf("  %v at %v paths %q", e.Kind, e.Position, e.Paths)
		}
		if len(got) < 3 || got[0].Kind != FileDragEnter || got[1].Kind != FileDragOver || got[len(got)-1].Kind != FileDrop {
			t.Fatalf("events %v, want FileDragEnter, FileDragOver, FileDrop", xkinds(got))
		}
		if got[0].Position != at {
			t.Errorf("enter at %v, want %v", got[0].Position, at)
		}
		if p := at.Add(f32.Pt(10, 5)); got[1].Position != p || got[len(got)-1].Position != p {
			t.Errorf("over/drop at %v/%v, want %v", got[1].Position, got[len(got)-1].Position, p)
		}
		if !reflect.DeepEqual(got[len(got)-1].Paths, want) {
			t.Errorf("paths %q, want %q", got[len(got)-1].Paths, want)
		}
		// XdndAware 5, XdndStatus accepting with XdndActionCopy, the
		// uri-list conversion, XdndFinished accepted with the copy
		for _, w := range []string{"XdndAware [5]", "XdndStatus 1 True", "SelectionRequest text/uri-list", "XdndFinished 1 True"} {
			if !strings.Contains(out, w) {
				t.Errorf("the source's log has no %q", w)
			}
		}
	})

	t.Run("leave", func(t *testing.T) {
		out := runXDNDSource(t, win, at, "leave", uris)
		t.Logf("source:\n%s", out)
		got := collectX(drops, FileDragLeave, 10*time.Second)
		if len(got) < 2 || got[0].Kind != FileDragEnter || got[len(got)-1].Kind != FileDragLeave {
			t.Fatalf("events %v, want FileDragEnter … FileDragLeave", xkinds(got))
		}
	})
}

func xkinds(es []FileDropEvent) []FileDropKind {
	var k []FileDropKind
	for _, e := range es {
		k = append(k, e.Kind)
	}
	return k
}
