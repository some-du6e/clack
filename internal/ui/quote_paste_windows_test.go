//go:build windows

package ui

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"gioui.org/app"
	"gioui.org/app/winclip"
	"gioui.org/io/event"
	"golang.org/x/sys/windows"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/richclip"
)

// Karim's 2026-11-21 report (Windows, α.113): a quote started in the
// channel composer, by typing ">" at the start of the line or with the
// formatting bar's Blockquote button, then Ctrl+V, and Clack was gone, twice
// in a row, with nothing in clack.log or crash.log. TestWinQuotePaste does
// that in App.Run's own window on a real Windows desktop, signed in to
// internal/fakeslack: SendInput types ">" or clicks the button and presses
// Ctrl+V, Windows' own input path, and another process (PowerShell's
// WinForms DataObject) puts each kind of copy on the real clipboard, the
// way an app's copy does. It replaces the clipboard, so it runs only where
// CLACK_WINDOWS_CLIPBOARD_TESTS is set (ci's windows-applog job).
//
// Every paste must leave the process alive and drawing, and land the way
// Slack's composer lands it (Texty's handleOnPaste, preparePastedDelta
// (+hBz prepareDeltaForPaste) and insertContentsAtCursor with
// matchBlockFormat (LPC/ convertDeltaToBlockFormat)): every pasted line
// inside the quote, a pasted list's items and code lines quoted (border
// 1), a pasted quote a quote line, files and pictures attached with no
// text, an empty clipboard nothing.

var qpMapVirtualKey = asTestUser32.NewProc("MapVirtualKeyW")

const (
	qpVKShift   = 0x10
	qpVKControl = 0x11
	qpVKV       = 0x56
	qpVKPeriod  = 0xBE // VK_OEM_PERIOD: Shift+. types ">" on the runner's US layout
)

// qpKey is a KEYBDINPUT for virtual key vk.
func qpKey(vk uint16, up bool) asTestInput {
	in := asTestInput{typ: 1} // INPUT_KEYBOARD
	scan, _, _ := qpMapVirtualKey.Call(uintptr(vk), 0)
	var flags uint32
	if up {
		flags |= 0x2 // KEYEVENTF_KEYUP
	}
	binary.LittleEndian.PutUint16(in.u[0:], vk)
	binary.LittleEndian.PutUint16(in.u[2:], uint16(scan))
	binary.LittleEndian.PutUint32(in.u[4:], flags)
	return in
}

// qpChord presses vk with mods held down.
func qpChord(t *testing.T, vk uint16, mods ...uint16) {
	t.Helper()
	var ins []asTestInput
	for _, m := range mods {
		ins = append(ins, qpKey(m, false))
	}
	ins = append(ins, qpKey(vk, false), qpKey(vk, true))
	for i := len(mods) - 1; i >= 0; i-- {
		ins = append(ins, qpKey(mods[i], true))
	}
	asSend(t, ins...)
}

// qpClick is a left click at screen point p, the pointer brought there
// through a second point (a move to where it already is gives the window
// no pointer input).
func qpClick(t *testing.T, p image.Point) {
	t.Helper()
	asSend(t, asMouse(p.Add(image.Pt(5, 3)), 0), asMouse(p, 0))
	time.Sleep(60 * time.Millisecond)
	asSend(t, asMouse(p, asLeftDown), asMouse(p, asLeftUp))
}

// qpPowerShell runs script in Windows PowerShell 5.1 (STA, as WinForms'
// clipboard needs).
func qpPowerShell(t *testing.T, script string) {
	t.Helper()
	full := "$ErrorActionPreference = 'Stop'\n" +
		"Add-Type -AssemblyName System.Windows.Forms, System.Drawing\n" + script
	u := utf16.Encode([]rune(full))
	raw := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(raw[2*i:], c)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-STA",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(raw))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("powershell: %v\n%s", err, out)
	}
}

// qpPS is s as a PowerShell single-quoted string.
func qpPS(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// qpFormat is one clipboard format's bytes, exactly as an app writes them.
type qpFormat struct {
	name string
	data []byte
}

// qpClip puts text (CF_UNICODETEXT, unless nil) and fs on the clipboard
// from PowerShell, each format's bytes from a file through a MemoryStream
// (they come back byte-exact), then runs extra on the DataObject $do.
func qpClip(t *testing.T, text *string, fs []qpFormat, extra string) {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("$do = New-Object System.Windows.Forms.DataObject\n")
	if text != nil {
		p := filepath.Join(dir, "text.txt")
		if err := os.WriteFile(p, []byte(*text), 0o600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "$do.SetData([System.Windows.Forms.DataFormats]::UnicodeText, $false, [System.IO.File]::ReadAllText(%s, (New-Object System.Text.UTF8Encoding($false))))\n", qpPS(p))
	}
	for i, f := range fs {
		p := filepath.Join(dir, fmt.Sprintf("format%d.bin", i))
		if err := os.WriteFile(p, f.data, 0o600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "$do.SetData(%s, $false, (New-Object System.IO.MemoryStream(,[System.IO.File]::ReadAllBytes(%s))))\n", qpPS(f.name), qpPS(p))
	}
	b.WriteString(extra)
	b.WriteString("[System.Windows.Forms.Clipboard]::SetDataObject($do, $true)\n")
	qpPowerShell(t, b.String())
}

// qpBitmap is the PowerShell for a 3x2 bitmap $b.
const qpBitmap = "$b = New-Object System.Drawing.Bitmap 3, 2, ([System.Drawing.Imaging.PixelFormat]::Format32bppArgb)\n" +
	"$b.SetPixel(0, 0, [System.Drawing.Color]::FromArgb(255, 255, 0, 0))\n" +
	"$b.SetPixel(1, 1, [System.Drawing.Color]::FromArgb(255, 0, 0, 255))\n"

// qpClipText is the clipboard's CF_UNICODETEXT ("" when it has none),
// read in a session like every reader in the process.
func qpClipText(t *testing.T) string {
	t.Helper()
	gcd := asTestUser32.NewProc("GetClipboardData")
	k32 := windows.NewLazySystemDLL("kernel32.dll")
	lock, unlock, size := k32.NewProc("GlobalLock"), k32.NewProc("GlobalUnlock"), k32.NewProc("GlobalSize")
	move := k32.NewProc("RtlMoveMemory")
	var out string
	err := winclip.Session(context.Background(), 0, func() error {
		h, _, _ := gcd.Call(13) // CF_UNICODETEXT
		if h == 0 {
			return nil
		}
		n, _, _ := size.Call(h)
		p, _, _ := lock.Call(h)
		if p == 0 || n < 2 {
			return nil
		}
		defer unlock.Call(h)
		u := make([]uint16, n/2)
		move.Call(uintptr(unsafe.Pointer(&u[0])), p, uintptr(2*len(u)))
		for i, c := range u {
			if c == 0 {
				u = u[:i]
				break
			}
		}
		out = string(utf16.Decode(u))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// qpWin is App.Run's window on the desktop, signed in to fakeslack.
type qpWin struct {
	a      *App
	hwnd   uintptr
	frames atomic.Int64
	done   chan error
}

func qpStart(t *testing.T) *qpWin {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	home := t.TempDir()
	cache := filepath.Join(home, "cache")
	w := &qpWin{done: make(chan error, 1)}
	hwnds := make(chan uintptr, 1)
	images := imgcache.New(fs.Fetch, "", nil)
	w.a = New(Options{ConfigDir: home, NoRealtime: true, Started: time.Now(), OnWindowEvent: func(e event.Event) {
		switch e := e.(type) {
		case app.Win32ViewEvent:
			if e.HWND != 0 {
				select {
				case hwnds <- e.HWND:
				default:
				}
			}
		case app.FrameEvent:
			w.frames.Add(1)
		}
	}}, images)
	w.a.Attach(core.New(slack.New(fs.Creds()), store.New(cache)), cache)
	w.a.SetTray(&fakeTray{})
	go func() { w.done <- w.a.Run() }()
	t.Cleanup(func() {
		w.a.Quit()
		select {
		case <-w.done:
		case <-time.After(30 * time.Second):
			t.Log("Run didn't return after Quit")
		}
	})
	select {
	case w.hwnd = <-hwnds:
	case <-time.After(60 * time.Second):
		t.Fatal("no Win32ViewEvent")
	}
	w.until(t, "the first frames", func() bool { return w.frames.Load() > 2 })
	w.until(t, "core ready", func() (ok bool) { w.on(t, func() { ok = !w.a.data.booted.IsZero() }); return })
	w.until(t, "the reveal", func() (ok bool) { w.on(t, func() { ok = w.a.rv.done }); return })
	time.Sleep(2 * time.Second) // boot settles
	cx, _, _ := asTestGetSystemMetrics.Call(0)
	cy, _, _ := asTestGetSystemMetrics.Call(1)
	ww, wh := min(int(cx)-40, 1200), min(int(cy)-60, 760)
	// topmost: SetForegroundWindow can be refused (the foreground lock), and
	// the keys and clicks go to the window in front
	tbSetWindowPos.Call(w.hwnd, ^uintptr(0) /* HWND_TOPMOST */, 16, 16, uintptr(ww), uintptr(wh), 0x0010) // SWP_NOACTIVATE
	w.settle(t)
	asTestSetForeground.Call(w.hwnd)
	w.settle(t)
	return w
}

// on runs f on the UI goroutine and waits for it. A Run that returned, or
// work that never runs, fails the test: the window is gone or stuck.
func (w *qpWin) on(t *testing.T, f func()) {
	t.Helper()
	ran := make(chan struct{})
	w.a.Post(func() { f(); close(ran) })
	select {
	case <-ran:
	case err := <-w.done:
		t.Fatalf("Run returned (%v)", err)
	case <-time.After(20 * time.Second):
		t.Fatal("posted work didn't run in 20 s: the UI goroutine is stuck")
	}
}

func (w *qpWin) until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for dl := time.Now().Add(30 * time.Second); !ok(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// settle waits for a frame drawn after now: the window still draws.
func (w *qpWin) settle(t *testing.T) {
	t.Helper()
	time.Sleep(200 * time.Millisecond)
	f := w.frames.Load()
	w.a.Invalidate()
	w.until(t, "a frame", func() bool { return w.frames.Load() > f })
	time.Sleep(200 * time.Millisecond)
}

// qpState is what the composer holds.
type qpState struct {
	lines []composer.Line
	texts []string
	files int
}

func (s qpState) String() string {
	var b strings.Builder
	for i, l := range s.lines {
		if i > 0 {
			b.WriteString(" | ")
		}
		fmt.Fprintf(&b, "%s", qpLineName(l))
		if i < len(s.texts) {
			txt := s.texts[i]
			if len(txt) > 40 {
				txt = fmt.Sprintf("%s…(%d)", txt[:40], len(txt))
			}
			fmt.Fprintf(&b, " %q", txt)
		}
	}
	fmt.Fprintf(&b, "; %d files", s.files)
	return b.String()
}

func qpLineName(l composer.Line) string {
	n := map[composer.BlockKind]string{composer.Para: "p", composer.Bullet: "ul", composer.Ordered: "ol",
		composer.Quote: "quote", composer.CodeBlock: "pre", composer.Header1: "h1", composer.Header2: "h2",
		composer.Header3: "h3", composer.Divider: "hr"}[l.Kind]
	if l.Quoted {
		n = "quoted " + n
	}
	if l.Indent > 0 {
		n += fmt.Sprintf("+%d", l.Indent)
	}
	return n
}

// qpLine is a wanted line: its format and text.
type qpLine struct {
	line composer.Line
	text string
}

func TestWinQuotePaste(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_CLIPBOARD_TESTS") == "" {
		t.Skip("replaces the clipboard; set CLACK_WINDOWS_CLIPBOARD_TESTS=1 (CI's windows-applog job)")
	}
	t.Setenv("CLACK_INSPECT", "")
	w := qpStart(t)

	var comp *composer.Composer
	var ed *composer.Editor
	w.until(t, "the channel composer", func() bool {
		w.on(t, func() {
			ch, ok := strings.CutPrefix(string(w.a.router.Current()), "c/")
			if !ok {
				return
			}
			if cv := w.a.convos[ch+"/"]; cv != nil && cv.comp != nil && !cv.compRect.Empty() {
				if cb, ok := cv.comp.(composerBox); ok {
					ed, _ = cb.FocusTag().(*composer.Editor)
					comp = cb.Composer
				}
			}
		})
		return ed != nil
	})
	org := tbOrigin(w.hwnd)
	read := func() (s qpState) {
		w.on(t, func() {
			d := ed.Doc()
			s.lines = append([]composer.Line(nil), d.Lines...)
			for i := range d.Lines {
				s.texts = append(s.texts, d.LineText(i))
			}
			s.files = comp.Attached()
		})
		return s
	}
	centre := func(r image.Rectangle) image.Point {
		return org.Add(image.Pt(r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2))
	}
	// a click in the input box: the window's key focus is the editor's
	var box image.Rectangle
	w.on(t, func() { box = comp.BoxRect() })
	if box.Empty() {
		t.Fatal("the composer's box wasn't drawn")
	}
	qpClick(t, centre(box))
	w.on(t, func() { comp.Focus() })
	w.settle(t)

	reset := func() {
		w.on(t, func() {
			ed.SetDoc(composer.NewDoc())
			comp.Focus()
		})
		w.settle(t)
	}
	isQuote := func() bool {
		s := read()
		return len(s.lines) == 1 && s.lines[0].Kind == composer.Quote && s.texts[0] == ""
	}
	ways := []struct {
		name  string
		quote func()
	}{
		{`">" typed`, func() { qpChord(t, qpVKPeriod, qpVKShift) }},
		{"the Blockquote button", func() {
			var r image.Rectangle
			w.on(t, func() { r = comp.QuoteRect() })
			if r.Empty() {
				t.Fatal("the formatting bar's Blockquote button wasn't drawn")
			}
			qpClick(t, centre(r))
		}},
	}

	str := func(s string) *string { return &s }
	quote := composer.Line{Kind: composer.Quote}
	qline := func(s ...string) []qpLine {
		out := make([]qpLine, len(s))
		for i, x := range s {
			out[i] = qpLine{quote, x}
		}
		return out
	}
	var long strings.Builder
	var longLines []string
	for i := 0; i < 400; i++ {
		l := fmt.Sprintf("line %03d of a long paste, with enough words in it to wrap the composer", i)
		longLines = append(longLines, l)
		long.WriteString(l)
		if i < 399 {
			long.WriteString("\r\n")
		}
	}
	// Chromium's copy of a web page's selection: CF_HTML around the
	// fragment, the text next to it.
	pageHTML := `<p style="margin:0">A paragraph with <b>bold</b> words</p><ul><li>item one</li><li>item two</li></ul>` +
		`<blockquote>an inner quote</blockquote><pre>code line</pre><p>the end</p>`
	pageText := "A paragraph with bold words\nitem one\nitem two\nan inner quote\ncode line\nthe end"
	// Slack's Copy message of a message with a quote and a line after it
	// (thunk 4EhV: text/plain, text/markdown and slack/texty in Chromium's
	// web custom data).
	texty := `{"ops":[{"insert":"quoted from slack"},{"attributes":{"blockquote":true},"insert":"\n"},{"insert":"and a ` +
		`line after it"},{"insert":"\n"}]}`
	slackText := "quoted from slack\nand a line after it"
	// Clack's own Copy message (popups' copyMessage): the message's text
	// and its delta (richclip.TextyDelta) through richclip.Write
	clackMsg := richtext.Doc{Blocks: []richtext.Block{
		{Kind: richtext.Quote, Spans: []richtext.Span{{Text: "quoted in clack"}}},
		{Kind: richtext.List, Items: [][]richtext.Span{{{Text: "an item"}}}},
		{Kind: richtext.Paragraph, Spans: []richtext.Span{{Text: "after it", Style: richtext.Bold}}},
	}}
	// the composer's own copy: Ctrl+C on a list and a line in the
	// composer (copySel), pasted back (its rich slice)
	ownDoc := &composer.Doc{Runes: []rune("item one\nitem two\nafter")}
	ownDoc.Attrs = make([]composer.Attr, len(ownDoc.Runes))
	ownDoc.Lines = []composer.Line{{Kind: composer.Bullet}, {Kind: composer.Bullet}, {}}
	copyOwn := func() {
		reset()
		w.on(t, func() {
			ed.SetDoc(ownDoc.Clone())
			ed.SetSelection(0, ownDoc.Len())
			comp.Focus()
		})
		w.settle(t)
		qpChord(t, 0x43, qpVKControl) // Ctrl+C
		w.until(t, "the composer's copy on the clipboard", func() bool { return qpClipText(t) == "item one\nitem two\nafter" })
	}
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		first func() // before the composer is reset (the composer's own copy)
		clip  func()
		want  []qpLine // nil: the composer keeps its empty quote line
		files int
	}{
		{"one line", nil, func() { qpClip(t, str("quoted words"), nil, "") }, qline("quoted words"), 0},
		{"several lines", nil, func() { qpClip(t, str("first line\nsecond line\nthird line"), nil, "") },
			qline("first line", "second line", "third line"), 0},
		{"CRLF line ends", nil, func() { qpClip(t, str("first line\r\nsecond line\r\nthird line"), nil, "") },
			qline("first line", "second line", "third line"), 0},
		{"a trailing newline", nil, func() { qpClip(t, str("ends with a break\r\n"), nil, "") },
			qline("ends with a break", ""), 0},
		{"a long text", nil, func() { qpClip(t, str(long.String()), nil, "") }, qline(longLines...), 0},
		{"a web page from Chromium (HTML Format)", nil, func() {
			qpClip(t, str(pageText), []qpFormat{{"HTML Format", append(richclip.CFHTML(pageHTML), 0)}}, "")
		}, []qpLine{
			{quote, "A paragraph with bold words"},
			{composer.Line{Kind: composer.Bullet, Quoted: true}, "item one"},
			{composer.Line{Kind: composer.Bullet, Quoted: true}, "item two"},
			{quote, "an inner quote"},
			{composer.Line{Kind: composer.CodeBlock, Quoted: true}, "code line"},
			{quote, "the end"},
		}, 0},
		{"Slack's Copy message (slack/texty)", nil, func() {
			qpClip(t, str(slackText), []qpFormat{{"Chromium Web Custom MIME Data Format",
				richclip.EncodeCustom(map[string]string{"slack/texty": texty})}}, "")
		}, qline("quoted from slack", "and a line after it"), 0},
		{"Clack's Copy message (richclip.Write)", nil, func() {
			c := richclip.Copy{Text: richtext.PlainText(clackMsg), Custom: map[string]string{"slack/texty": richclip.TextyDelta(clackMsg)}}
			if err := richclip.Write(context.Background(), c); err != nil {
				t.Fatal(err)
			}
		}, []qpLine{
			{quote, "quoted in clack"},
			{composer.Line{Kind: composer.Bullet, Quoted: true}, "an item"},
			{quote, "after it"},
			// its delta's last op is "after it\n", not a bare "\n":
			// prepareDeltaForPaste's closing quote line
			{quote, ""},
		}, 0},
		{"Clack's composer copy (Ctrl+C)", copyOwn, func() {}, []qpLine{
			{composer.Line{Kind: composer.Bullet, Quoted: true}, "item one"},
			{composer.Line{Kind: composer.Bullet, Quoted: true}, "item two"},
			{quote, "after"},
		}, 0},
		{"a picture (CF_BITMAP, CF_DIB, CF_DIBV5)", nil, func() {
			qpClip(t, nil, nil, qpBitmap+"$do.SetImage($b)\n")
		}, nil, 1},
		{`a picture with a "PNG"`, nil, func() {
			qpClip(t, nil, nil, qpBitmap+"$ms = New-Object System.IO.MemoryStream\n"+
				"$b.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)\n$do.SetData('PNG', $false, $ms)\n$do.SetImage($b)\n")
		}, nil, 1},
		{"a file from Explorer (CF_HDROP)", nil, func() {
			qpClip(t, nil, nil, "$fl = New-Object System.Collections.Specialized.StringCollection\n"+
				"[void]$fl.Add("+qpPS(file)+")\n$do.SetFileDropList($fl)\n")
		}, nil, 1},
		{"an empty clipboard", nil, func() { qpPowerShell(t, "[System.Windows.Forms.Clipboard]::Clear()\n") }, nil, 0},
	}
	// controls: the same pastes into a plain line (no quote)
	for _, c := range cases[:9] {
		name := "a plain line, " + c.name
		if c.first != nil {
			c.first()
		}
		reset()
		t.Logf("%s: the clipboard", name)
		c.clip()
		t.Logf("%s: Ctrl+V", name)
		qpChord(t, qpVKV, qpVKControl)
		time.Sleep(2 * time.Second)
		w.settle(t) // still drawing
		t.Logf("%s: %v", name, read())
	}
	for _, way := range ways {
		for _, c := range cases {
			name := way.name + ", " + c.name
			if c.first != nil {
				c.first()
			}
			reset()
			t.Logf("%s: starting the quote", name)
			way.quote()
			w.until(t, name+": the quote line", isQuote)
			t.Logf("%s: the clipboard", name)
			c.clip()
			before := read().files
			t.Logf("%s: Ctrl+V", name)
			qpChord(t, qpVKV, qpVKControl)
			ok := func(s qpState) bool {
				if s.files-before != c.files {
					return false
				}
				if c.want == nil {
					return len(s.lines) == 1 && s.lines[0].Kind == composer.Quote && s.texts[0] == ""
				}
				if len(s.lines) != len(c.want) {
					return false
				}
				for i, l := range c.want {
					if s.lines[i] != l.line || s.texts[i] != l.text {
						return false
					}
				}
				return true
			}
			// the text and the files land on their own frames: wait for
			// what's wanted, then a little longer for what shouldn't come
			s := read()
			for dl := time.Now().Add(8 * time.Second); !ok(s) && time.Now().Before(dl); s = read() {
				time.Sleep(50 * time.Millisecond)
			}
			time.Sleep(time.Second)
			w.settle(t) // still drawing
			s = read()
			if !ok(s) {
				var want strings.Builder
				for i, l := range c.want {
					if i > 0 {
						want.WriteString(" | ")
					}
					fmt.Fprintf(&want, "%s %q", qpLineName(l.line), l.text)
				}
				if c.want == nil {
					want.WriteString(`quote ""`)
				}
				t.Errorf("%s: the composer holds %v (%d new files), want %s; %d new files", name, s, s.files-before, &want, c.files)
				continue
			}
			t.Logf("%s: %v", name, s)
		}
	}
}
