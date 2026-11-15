//go:build darwin && cgo

package ui

import (
	"bufio"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/app/cocoatest"

	"github.com/some-du6e/clack/internal/ui/composer"
)

// keyV is kVK_ANSI_V (cocoatest names only the keys its own tests press).
const keyV = 0x09

// TestMacComposerPaste is ⌘V into the channel composer of App.Run's own
// window (CLACK_MACOS_WINDOW_TESTS=1, ci's macos job): the key goes
// through NSApp sendEvent:, the Edit menu's Paste (paste:) and Gio's
// view, which reads the general pasteboard's string (stringForType:
// NSPasteboardTypeString) for the text while the composer probes it for
// files and an image. Slack's useUploadPastedFiles takes the paste's files
// (getPastedFiles: clipboardData.files, minus an office copy's rendered
// picture) to the draft and preventDefaults the paste, so Texty's
// shouldSkipOnPaste inserts none of its text: a Finder copy attaches its
// files without their names (Finder puts them on the pasteboard as text
// too). Plain text, an image and rich text with a picture (TextEdit) paste
// as they did.
func TestMacComposerPaste(t *testing.T) {
	if !macWindowTests {
		t.Skip("CLACK_MACOS_WINDOW_TESTS not set")
	}
	t.Setenv("CLACK_INSPECT", "")
	ma := runMacBoundsApp(t, nil)
	defer ma.quit(t, true)
	onMain(func() {
		cocoatest.OrderFront(ma.view)
		cocoatest.TraceKeys(ma.view)
	})
	ma.until(t, "the key window", func() bool { return ma.state().Key })

	var comp *composer.Composer
	var ed *composer.Editor
	ma.until(t, "the channel composer", func() bool {
		ma.onUI(t, func() {
			ch, ok := strings.CutPrefix(string(ma.a.router.Current()), "c/")
			if !ok {
				return
			}
			if cv := ma.a.convos[ch+"/"]; cv != nil && cv.comp != nil && !cv.compRect.Empty() {
				if cb, ok := cv.comp.(composerBox); ok {
					ed, _ = cb.FocusTag().(*composer.Editor)
					comp = cb.Composer
					cb.Focus()
				}
			}
		})
		return ed != nil
	})
	time.Sleep(time.Second) // the focus request's frame

	dir := t.TempDir()
	notes, photo := filepath.Join(dir, "notes.txt"), filepath.Join(dir, "photo.png")
	if err := os.WriteFile(notes, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	pic := macPastePNG(t)
	if err := os.WriteFile(photo, pic, 0o644); err != nil {
		t.Fatal(err)
	}

	type state struct {
		text  string
		files int
	}
	read := func() (s state) {
		ma.onUI(t, func() { s = state{strings.TrimRight(ed.Doc().Text(), "\n"), comp.Attached()} })
		return s
	}
	paste := func(name string, want state) {
		t.Helper()
		ma.onUI(t, func() { ed.SetDoc(composer.NewDoc()) })
		base := read().files
		want.files += base
		var chars, ign string
		onMain(func() { chars, ign = cocoatest.PostKey(ma.view, keyV, cocoatest.Cmd) })
		// The text and the files land on their own frames: wait for what
		// is wanted, then a little longer for anything that shouldn't be.
		deadline := time.Now().Add(10 * time.Second)
		for s := read(); s != want && time.Now().Before(deadline); s = read() {
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(1500 * time.Millisecond)
		got := read()
		var trace string
		onMain(func() { trace = cocoatest.KeyTrace() })
		if got != want {
			t.Errorf("%s: ⌘V (characters %+q, ignoring %+q) left text %q and %d files, want text %q and %d files; the view got:\n%s",
				name, chars, ign, got.text, got.files-base, want.text, want.files-base, trace)
		} else {
			t.Logf("%s: text %q, %d files", name, got.text, got.files-base)
		}
	}

	// Finder's ⌘C of two files: both attach, and their names stay out.
	stop := macPasteFinderCopy(t, notes, photo)
	paste("Finder copy", state{"", 2})
	stop()

	macPasteSet(t, "pb.setStringForType('just text', 'public.utf8-plain-text');")
	paste("plain text", state{"just text", 0})

	p := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(p, pic, 0o644); err != nil {
		t.Fatal(err)
	}
	macPasteSet(t, "pb.setDataForType($.NSData.dataWithContentsOfFile(args[0]), 'public.png');", p)
	paste("image", state{"", 1})

	// TextEdit: RTF, its plain text and a picture of it. Text, no file.
	macPasteSet(t, "pb.setStringForType('{\\\\rtf1\\\\ansi hello}', 'public.rtf');\n"+
		"pb.setStringForType('hello', 'public.utf8-plain-text');\n"+
		"pb.setDataForType($.NSData.dataWithContentsOfFile(args[0]), 'public.png');", p)
	paste("rich text with a picture", state{"hello", 0})
}

// macPastePNG is a small opaque PNG.
func macPastePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	img.SetNRGBA(0, 0, color.NRGBA{0xff, 0, 0, 0xff})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// macPasteSet runs a JXA script with pb (the cleared general pasteboard)
// and args in scope.
func macPasteSet(t *testing.T, script string, args ...string) {
	t.Helper()
	full := "ObjC.import('AppKit');\n" +
		"function run(args) {\n" +
		"var pb = $.NSPasteboard.generalPasteboard;\n" +
		"pb.clearContents;\n" +
		script + "\n}\n"
	if out, err := exec.Command("osascript", append([]string{"-l", "JavaScript", "-e", full}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("osascript: %v\n%s", err, out)
	}
}

// macPasteFinderCopy puts paths on the pasteboard as Finder's ⌘C does
// (composer's macPBFinderCopy): an item per file with its public.file-url,
// the names as text and the first file's icon as a TIFF, the writer
// serving its run loop until stop.
func macPasteFinderCopy(t *testing.T, paths ...string) (stop func()) {
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
		"  it.setStringForType($.NSURL.fileURLWithPath($(ps[i])).absoluteString, 'public.file-url');\n" +
		"  names.push(ObjC.unwrap($(ps[i]).lastPathComponent));\n" +
		"  items.addObject(it);\n" +
		"}\n" +
		"var first = items.objectAtIndex(0);\n" +
		"first.setStringForType($(names.join('\\r')), 'public.utf8-plain-text');\n" +
		"first.setDataForType($.NSWorkspace.sharedWorkspace.iconForFile($(ps[0])).TIFFRepresentation, 'public.tiff');\n" +
		"pb.writeObjects(items);\n" +
		"console.log('ready');\n" +
		"var end = $.NSDate.dateWithTimeIntervalSinceNow(120);\n" +
		"while (!$.NSFileManager.defaultManager.fileExistsAtPath(args[1]) && end.timeIntervalSinceNow > 0) {\n" +
		"  $.NSRunLoop.currentRunLoop.runUntilDate($.NSDate.dateWithTimeIntervalSinceNow(0.05));\n" +
		"}\n" +
		"}\n"
	stopFile := filepath.Join(filepath.Dir(list), "stop")
	cmd := exec.Command("osascript", "-l", "JavaScript", "-e", script, list, stopFile)
	stderr, err := cmd.StderrPipe() // console.log
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	drained := make(chan struct{})
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		os.WriteFile(stopFile, nil, 0o644)
		<-drained
		cmd.Wait()
	}
	t.Cleanup(stop)
	var log strings.Builder
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		if sc.Text() == "ready" {
			go func() { io.Copy(io.Discard, stderr); close(drained) }()
			return stop
		}
		log.WriteString(sc.Text() + "\n")
	}
	close(drained)
	t.Fatalf("osascript ended before writing the pasteboard:\n%s", log.String())
	return stop
}
