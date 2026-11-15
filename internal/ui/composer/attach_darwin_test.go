//go:build darwin && !ios && cgo

package composer

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/app"
	"gioui.org/app/cocoatest"

	"github.com/some-du6e/clack/internal/desktop"
)

// The real macOS general pasteboard: JXA (osascript -l JavaScript, AppKit
// through its ObjC bridge) puts data there as other apps do, and Clack's
// reader takes it back. The reads hop to the main thread, so with
// CLACK_MACOS_PASTEBOARD_TESTS (ci's macos job) TestMain runs AppKit's own
// loop there (app.Main, as cmd/clack does) and the tests run beside it.
// They replace the user's clipboard, so they run only then.

func init() { runtime.LockOSThread() }

var macPasteboardTests = os.Getenv("CLACK_MACOS_PASTEBOARD_TESTS") != ""

func TestMain(m *testing.M) {
	if macPasteboardTests {
		go func() { os.Exit(m.Run()) }()
		app.Main()
		return
	}
	os.Exit(m.Run())
}

func macPBEnabled(t *testing.T) {
	if !macPasteboardTests {
		t.Skip("replaces the clipboard and needs app.Main; set CLACK_MACOS_PASTEBOARD_TESTS=1 (ci's macos job)")
	}
}

// macPBSet runs a JXA script with pb (the cleared general pasteboard) and
// args (its arguments) in scope, and returns what it printed.
func macPBSet(t *testing.T, script string, args ...string) string {
	t.Helper()
	full := "ObjC.import('AppKit');\n" +
		"function run(args) {\n" +
		"var pb = $.NSPasteboard.generalPasteboard;\n" +
		"pb.clearContents;\n" +
		script + "\n}\n"
	cmd := exec.Command("osascript", append([]string{"-l", "JavaScript", "-e", full}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("osascript: %v\n%s", err, out)
	}
	return string(out)
}

func macPBCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func macPBTypes(t *testing.T) []string {
	t.Helper()
	ts, err := desktop.PasteboardTypes(macPBCtx(t))
	if err != nil {
		t.Fatalf("PasteboardTypes: %v", err)
	}
	t.Logf("pasteboard types: %q", ts)
	return ts
}

// macPBPNG writes the 3x2 picture TestWinClipboard* use (one pixel at alpha
// 0x80) as a PNG file and returns it and its path.
func macPBPNG(t *testing.T) ([]byte, string) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for at, c := range macPBOpaque {
		img.SetNRGBA(at[0], at[1], c)
	}
	img.SetNRGBA(2, 1, color.NRGBA{0xff, 0, 0, 0x80})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return b.Bytes(), p
}

var macPBOpaque = map[[2]int]color.NRGBA{
	{0, 0}: {0xff, 0, 0, 0xff}, {1, 0}: {0, 0xff, 0, 0xff}, {2, 0}: {0, 0, 0xff, 0xff},
	{0, 1}: {0x12, 0x34, 0x56, 0xff}, {1, 1}: {0xff, 0xff, 0xff, 0xff},
}

func macPBNear(a, b color.NRGBA) bool {
	d := func(x, y uint8) bool { return x-y <= 3 || y-x <= 3 }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

// macPBPixels decodes a pasted PNG, which must be 3x2.
func macPBPixels(t *testing.T, data []byte) map[[2]int]color.NRGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	if s := img.Bounds().Size(); s.X != 3 || s.Y != 2 {
		t.Fatalf("size %v, want 3x2", s)
	}
	px := map[[2]int]color.NRGBA{}
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			px[[2]int{x, y}] = color.NRGBAModel.Convert(img.At(img.Bounds().Min.X+x, img.Bounds().Min.Y+y)).(color.NRGBA)
		}
	}
	t.Logf("pixels %v", px)
	return px
}

func macPBImage(t *testing.T, explicit bool) ([]byte, error) {
	t.Helper()
	data, mime, err := attClipboardImage(macPBCtx(t), explicit)
	if err == nil && mime != "image/png" {
		t.Fatalf("mime %q, want image/png", mime)
	}
	return data, err
}

// A screenshot (⌃⇧⌘4) or a browser's Copy Image: public.png, pasted as
// those very bytes (Chromium's GetPngFromPasteboard), transparency kept.
func TestMacPasteboardPNG(t *testing.T) {
	macPBEnabled(t)
	want, p := macPBPNG(t)
	macPBSet(t, "pb.setDataForType($.NSData.dataWithContentsOfFile(args[0]), 'public.png');", p)
	macPBTypes(t)
	if paths, err := attClipboardFiles(macPBCtx(t)); err != nil || len(paths) != 0 {
		t.Errorf("files on an image pasteboard: %q, %v", paths, err)
	}
	got, err := macPBImage(t, false)
	if err != nil {
		t.Fatalf("attClipboardImage: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("pasted %d bytes, not the %d of the pasteboard's PNG", len(got), len(want))
	}
}

// Preview's or Photos' copy without a PNG: TIFF (or JPEG), read as an
// NSImage and encoded as a PNG with the same pixels (Chromium's
// GetNSImage + EncodeGfxImageToPng).
func TestMacPasteboardTIFF(t *testing.T) {
	macPBEnabled(t)
	_, p := macPBPNG(t)
	macPBSet(t, "var rep = $.NSBitmapImageRep.imageRepWithData($.NSData.dataWithContentsOfFile(args[0]));\n"+
		"pb.setDataForType(rep.TIFFRepresentation, 'public.tiff');", p)
	if ts := macPBTypes(t); slices.Contains(ts, "public.png") {
		t.Fatalf("setup: the pasteboard has a PNG: %q", ts)
	}
	got, err := macPBImage(t, false)
	if err != nil {
		t.Fatalf("attClipboardImage: %v", err)
	}
	px := macPBPixels(t, got)
	for at, want := range macPBOpaque {
		if !macPBNear(px[at], want) {
			t.Errorf("pixel %v = %v, want %v", at, px[at], want)
		}
	}
	if got, want := px[[2]int{2, 1}], (color.NRGBA{0xff, 0, 0, 0x80}); !macPBNear(got, want) {
		t.Errorf("translucent pixel = %v, want %v (TIFF keeps alpha)", got, want)
	}
}

// (3 is NSBitmapImageFileTypeJPEG.)
func TestMacPasteboardJPEG(t *testing.T) {
	macPBEnabled(t)
	_, p := macPBPNG(t)
	macPBSet(t, "var rep = $.NSBitmapImageRep.imageRepWithData($.NSData.dataWithContentsOfFile(args[0]));\n"+
		"pb.setDataForType(rep.representationUsingTypeProperties(3, $.NSDictionary.dictionary), 'public.jpeg');", p)
	macPBTypes(t)
	got, err := macPBImage(t, false)
	if err != nil {
		t.Fatalf("attClipboardImage: %v", err)
	}
	macPBPixels(t, got) // JPEG's colours are approximate: only the size counts
}

// Rich text with a picture (TextEdit, Pages, Word) pastes as text, unless
// the paste asks for the image; plain text is no image at all.
func TestMacPasteboardRichText(t *testing.T) {
	macPBEnabled(t)
	_, p := macPBPNG(t)
	macPBSet(t, "pb.setStringForType('{\\\\rtf1\\\\ansi hello}', 'public.rtf');\n"+
		"pb.setStringForType('hello', 'public.utf8-plain-text');\n"+
		"pb.setDataForType($.NSData.dataWithContentsOfFile(args[0]), 'public.png');", p)
	macPBTypes(t)
	if _, err := macPBImage(t, false); err != errAttNoClipImage {
		t.Errorf("rich text copy: err %v, want errAttNoClipImage", err)
	}
	if _, err := macPBImage(t, true); err != nil {
		t.Errorf("rich text copy, explicit: %v", err)
	}

	macPBSet(t, "pb.setStringForType('just text', 'public.utf8-plain-text');")
	if _, err := macPBImage(t, true); err != errAttNoClipImage {
		t.Errorf("text pasteboard: err %v, want errAttNoClipImage", err)
	}
	if paths, err := attClipboardFiles(macPBCtx(t)); err != nil || len(paths) != 0 {
		t.Errorf("files on a text pasteboard: %q, %v", paths, err)
	}
}

// macPBFinderCopy puts paths on the pasteboard the way Finder's ⌘C does:
// one item per file with its public.file-url, the names as text and the
// first file's icon as a TIFF. The paths go to JXA in a UTF-8 file
// (osascript's argv mangles emoji). Like Finder, the writer stays running
// (until the test ends, serving its run loop): the pasteboard server may
// ask the owner for later items' data, and an exited osascript dropped
// them at random.
func macPBFinderCopy(t *testing.T, paths ...string) {
	t.Helper()
	list := filepath.Join(t.TempDir(), "paths.txt")
	if err := os.WriteFile(list, []byte(strings.Join(paths, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "ObjC.import('AppKit');\n" +
		"function run(args) {\n" +
		"var pb = $.NSPasteboard.generalPasteboard;\n" +
		"pb.clearContents;\n" +
		"var ps = ObjC.unwrap($.NSString.stringWithContentsOfFileEncodingError(args[0], $.NSUTF8StringEncoding, null)).split('\\n');\n" +
		"var items = $.NSMutableArray.array;\n" +
		"var names = [];\n" +
		"for (var i = 0; i < ps.length; i++) {\n" +
		"  var it = $.NSPasteboardItem.alloc.init;\n" +
		"  var u = $.NSURL.fileURLWithPath($(ps[i])).absoluteString;\n" +
		"  console.log(ObjC.unwrap(u));\n" +
		"  it.setStringForType(u, 'public.file-url');\n" +
		"  names.push(ObjC.unwrap($(ps[i]).lastPathComponent));\n" +
		"  items.addObject(it);\n" +
		"}\n" +
		"var first = items.objectAtIndex(0);\n" +
		"first.setStringForType($(names.join('\\r')), 'public.utf8-plain-text');\n" +
		"first.setDataForType($.NSWorkspace.sharedWorkspace.iconForFile($(ps[0])).TIFFRepresentation, 'public.tiff');\n" +
		"pb.writeObjects(items);\n" +
		"console.log('ready');\n" +
		// serve the pasteboard (a run loop) until the stop file appears
		"var end = $.NSDate.dateWithTimeIntervalSinceNow(120);\n" +
		"while (!$.NSFileManager.defaultManager.fileExistsAtPath(args[1]) && end.timeIntervalSinceNow > 0) {\n" +
		"  $.NSRunLoop.currentRunLoop.runUntilDate($.NSDate.dateWithTimeIntervalSinceNow(0.05));\n" +
		"}\n" +
		"}\n"
	stop := filepath.Join(filepath.Dir(list), "stop")
	cmd := exec.Command("osascript", "-l", "JavaScript", "-e", script, list, stop)
	stderr, err := cmd.StderrPipe() // console.log
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	drained := make(chan struct{})
	t.Cleanup(func() {
		os.WriteFile(stop, nil, 0o644)
		<-drained
		cmd.Wait()
	})
	var log strings.Builder
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		if sc.Text() == "ready" {
			t.Logf("file URLs written:\n%s", log.String())
			go func() { io.Copy(io.Discard, stderr); close(drained) }()
			return
		}
		log.WriteString(sc.Text() + "\n")
	}
	close(drained)
	t.Fatalf("osascript ended before writing the pasteboard:\n%s", log.String())
}

// Files copied in Finder come back as their paths (spaces, accents and
// emoji intact), and their icon isn't pasted as an image.
func TestMacPasteboardFiles(t *testing.T) {
	macPBEnabled(t)
	dir := t.TempDir()
	want := []string{filepath.Join(dir, "a b.png"), filepath.Join(dir, "résumé 📎.txt")}
	for _, p := range want {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	macPBFinderCopy(t, want...)
	macPBTypes(t)
	got, err := attClipboardFiles(macPBCtx(t))
	if err != nil {
		t.Fatalf("attClipboardFiles: %v", err)
	}
	// t.TempDir is under /var, a symlink to /private/var, and file URLs may
	// change a name's Unicode normalization: compare the files themselves.
	same := len(got) == len(want)
	for i := 0; same && i < len(want); i++ {
		a, errA := os.Stat(got[i])
		b, errB := os.Stat(want[i])
		same = errA == nil && errB == nil && os.SameFile(a, b)
	}
	if !same {
		t.Fatalf("files %q, want %q", got, want)
	}
	if _, err := macPBImage(t, true); err != errAttNoClipImage {
		t.Errorf("image on a Finder copy: %v, want errAttNoClipImage", err)
	}
}

// macPBWait waits for a's paste goroutine and returns its chips' names.
func macPBWait(t *testing.T, a *attachments) []string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for a.pasting.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the paste never finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	var names []string
	for _, c := range a.chips {
		names = append(names, c.name)
	}
	return names
}

// ⌘V end to end (PasteClipboardImage, what the composer's EvPasted
// calls): a Finder copy attaches the files under their names, a folder
// among them gets the toast; a copied image attaches as image.png.
func TestMacPasteAttaches(t *testing.T) {
	macPBEnabled(t)
	dir := t.TempDir()
	f1, f2, sub := filepath.Join(dir, "notes.txt"), filepath.Join(dir, "photo.png"), filepath.Join(dir, "folder")
	if err := os.WriteFile(f1, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	pngData, _ := macPBPNG(t)
	if err := os.WriteFile(f2, pngData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	macPBFinderCopy(t, f1, sub, f2)
	h := &attFakeHost{}
	a := newAttachments(h)
	a.PasteClipboardImage()
	if names := macPBWait(t, a); !slices.Equal(names, []string{"notes.txt", "photo.png"}) {
		t.Errorf("attached %q, want notes.txt and photo.png", names)
	}
	if ts := h.takeToasts(); len(ts) != 1 || !strings.Contains(ts[0], "folder is a folder") {
		t.Errorf("toasts %q, want the folder's", ts)
	}

	_, p := macPBPNG(t)
	macPBSet(t, "pb.setDataForType($.NSData.dataWithContentsOfFile(args[0]), 'public.png');", p)
	a = newAttachments(h)
	a.PasteClipboardImage()
	if names := macPBWait(t, a); !slices.Equal(names, []string{"image.png"}) {
		t.Errorf("attached %q, want image.png", names)
	}
	if ts := h.takeToasts(); len(ts) != 0 {
		t.Errorf("toasts %q", ts)
	}
}

// A main thread stuck in a long frame: the read gives up after about a
// second instead of waiting for it (never a dispatch_sync), and the late
// answer is freed without harm.
func TestMacPasteboardBusyMain(t *testing.T) {
	macPBEnabled(t)
	_, p := macPBPNG(t)
	macPBSet(t, "pb.setDataForType($.NSData.dataWithContentsOfFile(args[0]), 'public.png');", p)
	started, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		cocoatest.OnMain(func() {
			close(started)
			time.Sleep(3500 * time.Millisecond)
		})
	}()
	<-started
	t0 := time.Now()
	_, err := desktop.PasteboardPNG(macPBCtx(t), attMaxFileSize)
	if !errors.Is(err, desktop.ErrPasteboardBusy) {
		t.Errorf("PasteboardPNG with the main thread busy: %v, want ErrPasteboardBusy", err)
	}
	if d := time.Since(t0); d > 2*time.Second {
		t.Errorf("waited %v for the busy main thread", d)
	}
	if _, err := attClipboardFiles(macPBCtx(t)); !errors.Is(err, desktop.ErrPasteboardBusy) {
		t.Errorf("attClipboardFiles with the main thread busy: %v, want ErrPasteboardBusy", err)
	}
	<-done
	// The abandoned reads ran after all; the next one works.
	if _, err := macPBImage(t, false); err != nil {
		t.Errorf("after the busy main thread: %v", err)
	}
}
