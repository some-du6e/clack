//go:build windows

package composer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/some-du6e/clack/internal/ui/richclip"
)

// Karim's crash (2026-11-21, Windows, α.113): a quote, then Ctrl+V, and
// Clack was gone, with nothing in clack.log or crash.log. A paste reads
// the clipboard on two goroutines at once: startClipHTML's rich read
// (richclip.ReadCustom, then clipReadHTML) and the composer's file probe
// (attClipboardFiles, then attClipboardImage). Each opened it with no
// window, which Windows lets two threads do at the same time, and one's
// CloseClipboard freed the copy GetClipboardData had handed the other:
// GlobalSize on it, and the heap ended the process from inside ntdll
// (STATUS_HEAP_CORRUPTION, 0xc0000374), where Go never sees it. Every
// open is a winclip.Session now, one at a time in the process.
//
// TestWinClipboardReadersOneAtATime runs those four readers on their own
// goroutines for a few seconds with a rich copy on the real clipboard
// (text, "HTML Format", and Chromium's custom data holding slack/texty, as
// Copy message makes it), in a child process: the child must live, and
// every read must bring back the copy whole. It replaces the clipboard,
// so it runs only where CLACK_WINDOWS_CLIPBOARD_TESTS is set (ci's
// windows-applog job).
func TestWinClipboardReadersOneAtATime(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_CLIPBOARD_TESTS") == "" {
		t.Skip("replaces the clipboard; set CLACK_WINDOWS_CLIPBOARD_TESTS=1 (CI's windows-applog job)")
	}
	if os.Getenv("CLACK_CLIP_READERS_CHILD") == "1" {
		clipReadersChild(t)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWinClipboardReadersOneAtATime$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), "CLACK_CLIP_READERS_CHILD=1")
	out, err := cmd.CombinedOutput()
	if lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n"); len(lines) > 40 {
		out = []byte(strings.Join(lines[len(lines)-40:], "\n"))
	}
	t.Logf("the readers' process:\n%s", out)
	if err != nil {
		code := uint32(cmd.ProcessState.ExitCode())
		what := ""
		switch code {
		case 0xc0000374:
			what = " (STATUS_HEAP_CORRUPTION: the heap found a freed or foreign block)"
		case 0xc0000005:
			what = " (an access violation outside Go)"
		case 0xc0000409:
			what = " (a fail-fast)"
		}
		t.Fatalf("the readers' process ended with exit code 0x%08x%s: %v", code, what, err)
	}
}

// clipReadersTexty is a message with a quote and a line after it, as
// Slack's Copy message (and Clack's) puts it on the clipboard.
const clipReadersTexty = `{"ops":[{"insert":"quoted from slack"},{"attributes":{"blockquote":true},"insert":"\n"},` +
	`{"insert":"and a line after it"},{"insert":"\n"}]}`

func clipReadersChild(t *testing.T) {
	c := richclip.Copy{
		Text:   "quoted from slack\r\nand a line after it",
		HTML:   "<blockquote>quoted from slack</blockquote><p>and a line after it</p>",
		Custom: map[string]string{"slack/texty": clipReadersTexty},
	}
	if err := richclip.Write(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	t.Logf("Windows, two threads opening the clipboard with no window: %s", clipOpenOverlap())

	const readers = 4
	var reads, busy [readers]atomic.Int64
	var mu sync.Mutex
	var bad []string
	fail := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		if len(bad) < 8 {
			bad = append(bad, fmt.Sprintf(format, args...))
		}
	}
	deadline := time.Now().Add(4 * time.Second)
	var wg sync.WaitGroup
	for g := 0; g < readers; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for time.Now().Before(deadline) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				var err error
				switch g {
				case 0: // the paste's rich read: Slack's own copy first
					var m map[string]string
					if m, err = richclip.ReadCustom(ctx); err == nil && m["slack/texty"] != clipReadersTexty {
						fail("ReadCustom: %d types, slack/texty %d bytes", len(m), len(m["slack/texty"]))
					}
				case 1: // then text/html
					var b []byte
					if b, err = clipReadHTML(ctx); err == nil && !strings.Contains(clipHTMLText(b), "and a line after it") {
						fail("HTML Format: %d bytes without the copy's last line", len(b))
					} else if errors.Is(err, errClipNoHTML) {
						err = nil // busy or gone: counted below as a busy read
						busy[g].Add(1)
					}
				case 2: // the composer's file probe: Explorer's files
					var paths []string
					if paths, err = attClipboardFiles(ctx); err == nil && len(paths) > 0 {
						fail("CF_HDROP: %d files on a text copy", len(paths))
					}
				case 3: // then a picture
					if _, _, err = attClipboardImage(ctx, false); errors.Is(err, errAttNoClipImage) {
						err = nil
					}
				}
				cancel()
				if err != nil {
					if strings.Contains(err.Error(), "busy") {
						busy[g].Add(1)
					} else {
						fail("reader %d: %v", g, err)
					}
				}
				reads[g].Add(1)
			}
		}(g)
	}
	wg.Wait()
	t.Logf("reads (busy): ReadCustom %d (%d), HTML %d (%d), files %d (%d), picture %d (%d)",
		reads[0].Load(), busy[0].Load(), reads[1].Load(), busy[1].Load(),
		reads[2].Load(), busy[2].Load(), reads[3].Load(), busy[3].Load())
	for _, b := range bad {
		t.Error(b)
	}
	for g := range reads {
		if reads[g].Load() == 0 || busy[g].Load()*2 > reads[g].Load() {
			t.Errorf("reader %d: %d reads, %d of them busy", g, reads[g].Load(), busy[g].Load())
		}
	}
}

// clipOpenOverlap is what Windows does when a second thread opens the
// clipboard with no window while another thread holds it that way (for
// the log: the readers raced because it lets the second one in).
func clipOpenOverlap() string {
	user32 := windows.NewLazySystemDLL("user32.dll")
	open, closeClip := user32.NewProc("OpenClipboard"), user32.NewProc("CloseClipboard")
	held, release, done := make(chan bool), make(chan struct{}), make(chan struct{})
	var firstClose uintptr
	go func() {
		defer close(done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		r, _, _ := open.Call(0)
		held <- r != 0
		<-release
		if r != 0 {
			firstClose, _, _ = closeClip.Call()
		}
	}()
	if !<-held {
		close(release)
		<-done
		return "the first open failed (another app held it)"
	}
	runtime.LockOSThread()
	r, _, e := open.Call(0)
	var second string
	if r != 0 {
		c, _, _ := closeClip.Call()
		second = fmt.Sprintf("the second open succeeded too; its close returned %d", c)
	} else {
		second = fmt.Sprintf("the second open failed (%v)", e)
	}
	runtime.UnlockOSThread()
	close(release)
	<-done
	return fmt.Sprintf("%s, and the first thread's close then returned %d", second, firstClose)
}
