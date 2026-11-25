// SPDX-License-Identifier: Unlicense OR MIT

//go:build linux && !android && !nox11

package app

import (
	"bytes"
	stdcontext "context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"gioui.org/io/clipboard"
	"gioui.org/io/system"
	"gioui.org/op"
)

// Clack patch (CLACK_PATCHES.md §69) on a real X server (ci's linux job
// runs it under Xvfb with CLACK_X11_DROP_TESTS set): a Gio window writes
// three formats at once (clipboard.FormatsType), as Copy Image's are, and
// python-xlib reads CLIPBOARD as another client would: TARGETS lists
// them, each target brings its data (the large one by INCR, the window's
// property limit made small), and UTF8_STRING is refused at once. A text
// write afterwards replaces them.

const clipReaderPy = `
import sys, time, json, base64, signal
from Xlib import X, display

signal.alarm(150)
d = display.Display()
w = d.screen().root.create_window(0, 0, 1, 1, 0, X.CopyFromParent, event_mask=X.PropertyChangeMask)
CLIP = d.intern_atom('CLIPBOARD')
PROP = d.intern_atom('CLACK_CLIP')
INCR = d.intern_atom('INCR')

def wait(pred, secs=10):
    end = time.time() + secs
    while time.time() < end:
        if d.pending_events():
            e = d.next_event()
            if pred(e):
                return e
        else:
            time.sleep(0.005)
    return None

def convert(name):
    w.delete_property(PROP)
    w.convert_selection(CLIP, d.intern_atom(name), PROP, X.CurrentTime)
    d.flush()
    e = wait(lambda e: e.type == X.SelectionNotify)
    if e is None:
        return {'error': 'no SelectionNotify'}
    if e.property == X.NONE:
        return {'refused': True}
    p = w.get_full_property(PROP, X.AnyPropertyType)
    if p is None:
        return {'error': 'no property'}
    if p.property_type == INCR:
        w.delete_property(PROP)
        d.flush()
        data, chunks, typ = b'', 0, None
        while True:
            ev = wait(lambda e: e.type == X.PropertyNotify and e.atom == PROP and e.state == X.PropertyNewValue)
            if ev is None:
                return {'error': 'INCR stalled', 'got': len(data)}
            q = w.get_full_property(PROP, X.AnyPropertyType)
            w.delete_property(PROP)
            d.flush()
            if q is None or len(q.value) == 0:
                break
            typ = d.get_atom_name(q.property_type)
            data += bytes(q.value)
            chunks += 1
        return {'type': typ, 'incr': chunks, 'data': base64.b64encode(data).decode()}
    if p.format == 32:
        return {'type': d.get_atom_name(p.property_type), 'atoms': [d.get_atom_name(a) for a in p.value]}
    return {'type': d.get_atom_name(p.property_type), 'data': base64.b64encode(bytes(p.value)).decode()}

print(json.dumps({name: convert(name) for name in sys.argv[1:]}))
`

type clipReply struct {
	Error   string   `json:"error"`
	Refused bool     `json:"refused"`
	Type    string   `json:"type"`
	Atoms   []string `json:"atoms"`
	Incr    int      `json:"incr"`
	Data    string   `json:"data"`
}

func (r clipReply) bytes(t *testing.T) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(r.Data)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func runClipReader(t *testing.T, targets ...string) map[string]clipReply {
	t.Helper()
	py := filepath.Join(t.TempDir(), "clip_reader.py")
	if err := os.WriteFile(py, []byte(clipReaderPy), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, stop := stdcontext.WithTimeout(stdcontext.Background(), 3*time.Minute)
	defer stop()
	out, err := exec.CommandContext(ctx, "python3", append([]string{"-I", py}, targets...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("python3 clipboard reader: %v\n%s", err, out)
	}
	var got map[string]clipReply
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("reader output %q: %v", out, err)
	}
	return got
}

// clipTestWindow is a window that executes each command sent on writes in
// its next frame (frames keep coming) and says so on written.
func clipTestWindow(t *testing.T) (writes chan<- clipboard.WriteCmd, written <-chan struct{}) {
	t.Helper()
	w := new(Window)
	w.Option(Title("Clack clipboard formats test"), Size(320, 240))
	in := make(chan clipboard.WriteCmd, 4)
	out := make(chan struct{}, 4)
	frames := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var ops op.Ops
		for {
			switch e := w.Event().(type) {
			case FrameEvent:
				ops.Reset()
				wrote := false
				select {
				case c := <-in:
					e.Source.Execute(c)
					wrote = true
				default:
				}
				e.Source.Execute(op.InvalidateCmd{At: e.Now.Add(20 * time.Millisecond)})
				e.Frame(&ops)
				if wrote {
					out <- struct{}{}
				}
				select {
				case frames <- struct{}{}:
				default:
				}
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
			t.Log("the window didn't close within 10 s")
		}
	})
	select {
	case <-frames:
	case <-time.After(30 * time.Second):
		t.Fatal("no first frame (is this X11?)")
	}
	return in, out
}

func clipUTF16(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return b
}

func TestClackClipFormatsX11(t *testing.T) {
	if os.Getenv("CLACK_X11_DROP_TESTS") == "" || os.Getenv("DISPLAY") == "" {
		t.Skip("CLACK_X11_DROP_TESTS or DISPLAY not set")
	}
	os.Unsetenv("WAYLAND_DISPLAY") // X11, not Wayland
	prev := x11ClipIncrMax
	x11ClipIncrMax = 64 << 10
	t.Cleanup(func() { x11ClipIncrMax = prev })
	big := make([]byte, 300<<10) // INCR: five 64 KB chunks
	for i := range big {
		big[i] = byte(i*7 + i>>9)
	}
	html := []byte(`<img src="https://slack-imgs.com/?c=1&amp;o1=ro&amp;url=x" alt="A test GIF"/>`)
	moz := clipUTF16("https://slack-imgs.com/?c=1&o1=ro&url=x\nA test GIF")
	writes, written := clipTestWindow(t)
	send := func(c clipboard.WriteCmd) {
		t.Helper()
		writes <- c
		select {
		case <-written:
		case <-time.After(10 * time.Second):
			t.Fatal("the write's frame never came")
		}
		time.Sleep(100 * time.Millisecond) // the window applies it after the frame
	}
	send(clipboard.WriteCmd{Type: clipboard.FormatsType, Data: io.NopCloser(bytes.NewReader(clipboard.EncodeFormats(
		[]clipboard.Format{{Type: "image/png", Data: big}, {Type: "text/html", Data: html}, {Type: "text/x-moz-url", Data: moz}})))})

	got := runClipReader(t, "TARGETS", "image/png", "text/html", "text/x-moz-url", "UTF8_STRING")
	if r := got["TARGETS"]; r.Type != "ATOM" || !slices.Equal(r.Atoms, []string{"TARGETS", "image/png", "text/html", "text/x-moz-url"}) {
		t.Errorf("TARGETS %+v, want the formats", r)
	}
	if r := got["image/png"]; r.Error != "" || r.Type != "image/png" || r.Incr < 2 || !bytes.Equal(r.bytes(t), big) {
		t.Errorf("image/png: %s, type %q, %d INCR chunks, %d bytes; want %d bytes by INCR", r.Error, r.Type, r.Incr, len(r.bytes(t)), len(big))
	}
	if r := got["text/html"]; r.Type != "text/html" || r.Incr != 0 || !bytes.Equal(r.bytes(t), html) {
		t.Errorf("text/html: %+v, want %q at once", r, html)
	}
	if r := got["text/x-moz-url"]; r.Type != "text/x-moz-url" || !bytes.Equal(r.bytes(t), moz) {
		t.Errorf("text/x-moz-url: %+v", r)
	}
	if r := got["UTF8_STRING"]; !r.Refused {
		t.Errorf("UTF8_STRING: %+v, want it refused (no text in the copy)", r)
	}

	// a text write replaces the formats
	send(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader("plain text"))})
	got = runClipReader(t, "TARGETS", "UTF8_STRING")
	if r := got["TARGETS"]; slices.Contains(r.Atoms, "image/png") || !slices.Contains(r.Atoms, "UTF8_STRING") {
		t.Errorf("after a text write TARGETS %q, want the text targets", r.Atoms)
	}
	if r := got["UTF8_STRING"]; string(r.bytes(t)) != "plain text" {
		t.Errorf("after a text write UTF8_STRING %+v", r)
	}
}
