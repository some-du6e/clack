package ui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/demo"
	"github.com/some-du6e/clack/internal/ui/uiguard"
)

// The "your Slack, 1:1" beat shows Clack itself, not a drawing of it: the
// real app signed in to the demo workspace (internal/demo: Quillfeather
// Paper Co.), rendered by `clack intro-preview` (intro_preview_child.go)
// in a process of its own and drawn here as an image in the beat's card.
// Its rail, top bar, sidebar sections, #general's header, messages and
// composer, and the thread in its secondary pane are the app's own
// components, in the window's theme, font and locale (the ui.json the
// window writes for it).
//
// The window's side is only this file: it starts the renderer, reads one
// frame of pixels from its stdout and draws them. It changes nothing in
// the window's process (no plugin hook, emoji set, cache or registry is
// touched) and the renderer can't reach the window's: it is another
// process, locked to loopback, with its own temporary config folder. The
// image takes no input. close (the introduction's end, sign-out, quit)
// kills the renderer if it is still running, and its goroutine removes the
// folder once the renderer has exited.

// previewW, previewH are the preview's size in its own dp: the card shows
// it at cardW/previewW of the window's scale.
const (
	previewW = 1200
	previewH = 600
	cardW    = 660 // the card in stage units
	cardH    = 330
)

// introPreviewCommand builds the renderer's command (args are its flags):
// the running executable's `intro-preview`. Test binaries have none
// (introPreviewTestCommand stands in where a test wants one).
var introPreviewCommand = func(args ...string) (*exec.Cmd, error) {
	uiguard.OffUI("intro preview: exec") // run calls it on its goroutine
	if testing.Testing() {
		return nil, errors.New("no renderer in tests")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, append([]string{"intro-preview"}, args...)...)
	cmd.WaitDelay = 2 * time.Second // a killed renderer's pipes never hold Wait
	configureChild(cmd)             // Windows: no console; Linux: dies with the window
	return cmd, nil
}

// rendererEnv is the renderer's environment: the window's (base), with
// CLACK_HOME and YOLOSLACK_HOME pointing into the renderer's own folder so
// nothing that defaults to the user's config folder (Taut's and Slick's
// plugins, their storage and secrets, creds.json) can find it, and none
// of the window's test switches.
func rendererEnv(base []string, home string) []string {
	out := make([]string, 0, len(base)+2)
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "CLACK_HOME", "YOLOSLACK_HOME", "CLACK_MACOS_WINDOW_TESTS":
			continue
		}
		out = append(out, kv)
	}
	return append(out, "CLACK_HOME="+home, "YOLOSLACK_HOME="+home)
}

// introPreviewReportHook, when set (tests), asks the renderer for its
// report and gets it.
var introPreviewReportHook func(*introPreviewReport)

const previewDirPrefix = "clack-intro-preview-"

type introPreview struct {
	host *App

	// UI goroutine
	started bool
	fail    bool // the renderer couldn't start or render
	img     *image.RGBA
	op      paint.ImageOp
	fresh   bool      // img just came: readyAt starts at the next frame
	readyAt time.Time // the window frame the image came in at
	card    image.Rectangle

	mu     sync.Mutex // closed, proc, dir: the goroutine reads them
	closed bool
	proc   *os.Process
	dir    string
}

func startIntroPreview(host *App) *introPreview { return &introPreview{host: host} }

// ready reports whether there is an image to draw (nil-safe).
func (p *introPreview) ready() bool { return p != nil && p.img != nil }

// failed reports whether there will be none (nil-safe).
func (p *introPreview) failed() bool { return p == nil || p.fail }

// update starts the renderer at the first frame that draws the beats (the
// card's size in px is known then) and notes when an image came in.
func (p *introPreview) update(f *introFrame) {
	if p == nil {
		return
	}
	if p.fresh {
		p.fresh, p.readyAt = false, f.now
	}
	if p.started || p.fail {
		return
	}
	p.started = true
	r := p.cardRect(f)
	size := r.Size()
	if size.X < 64 || size.Y < 32 {
		p.fail = true
		return
	}
	// a card bigger than 2400 px renders at 2400 and is drawn up
	if size.X > 2400 {
		size = image.Pt(2400, 2400*size.Y/size.X)
	}
	spdp := float32(1)
	if m := f.gtx.Metric; m.PxPerDp > 0 {
		spdp = m.PxPerSp / m.PxPerDp
	}
	cfg, err := p.config()
	if err != nil {
		p.fail = true
		return
	}
	h := p.host
	h.previews.Add(1) // quit waits for every renderer (shutdownIntro)
	go func() {
		defer h.previews.Done()
		// the frame is shown as soon as it is read; the renderer's exit and
		// the folder's removal come after
		deliver := func(img *image.RGBA) {
			h.Post(func() {
				if !p.isClosed() {
					p.img, p.op, p.fresh = img, paint.NewImageOp(img), true
				}
			})
		}
		if err := p.run(cfg, size, float32(size.X)/previewW, spdp, deliver); err != nil {
			h.Post(func() {
				if !p.isClosed() {
					log.Printf("ui: intro preview: %v", err)
				}
				if p.img == nil {
					p.fail = true
				}
			})
		}
	}()
}

func (p *introPreview) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// config is the renderer's ui.json: the window's mode, account theme,
// font and locale, live updates off, #general open. UI goroutine.
func (p *introPreview) config() ([]byte, error) {
	h := p.host
	dark, off := h.th.Dark, false
	return json.Marshal(prefs{
		Dark: &dark, ThemeSync: &off, Realtime: &off,
		ClientTypeface: h.prefs.ClientTypeface,
		AcctTheme:      h.prefs.AcctTheme, AcctNoGradients: h.prefs.AcctNoGradients,
		Locale:      h.prefs.Locale,
		LastChannel: demo.GeneralID,
	})
}

// run starts the renderer in a new folder, hands its frame to deliver as
// soon as it is read, waits for it to exit and removes the folder (the
// renderer removes it too). Off the UI goroutine.
func (p *introPreview) run(cfg []byte, size image.Point, scale, spdp float32, deliver func(*image.RGBA)) error {
	sweepPreviewDirs()
	dir, err := os.MkdirTemp("", previewDirPrefix)
	if err != nil {
		return err
	}
	defer removePreviewDir(dir)
	if err := os.WriteFile(filepath.Join(dir, "ui.json"), cfg, 0o600); err != nil {
		return err
	}
	args := []string{"-dir", dir, "-w", fmt.Sprint(size.X), "-h", fmt.Sprint(size.Y),
		"-scale", fmt.Sprint(scale), "-spdp", fmt.Sprint(spdp)}
	hook := introPreviewReportHook
	if hook != nil {
		args = append(args, "-report")
	}
	cmd, err := introPreviewCommand(args...)
	if err != nil {
		return err
	}
	base := cmd.Env
	if base == nil {
		base = os.Environ()
	}
	cmd.Env = rendererEnv(base, filepath.Join(dir, "home"))
	cmd.Stdin = nil
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if p.isClosed() {
		return errors.New("closed")
	}
	// Linux's parent-death signal follows the thread that forks: keep this
	// one from Start to Wait, so no other goroutine's thread exiting (a
	// locked one ending) kills the renderer mid-render
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Start outside the lock: close (UI goroutine) never waits on a fork
	if err := cmd.Start(); err != nil {
		return err
	}
	p.mu.Lock()
	p.proc, p.dir = cmd.Process, dir
	if p.closed { // closed while it started
		_ = cmd.Process.Kill()
	}
	p.mu.Unlock()
	logged := make(chan struct{})
	go func() { // its log, marked as the preview's, in the window's
		defer close(logged)
		relayPreviewLog(stderr)
	}()
	img, rep, rerr := readPreviewFrame(bufio.NewReaderSize(stdout, 1<<16), size, hook != nil)
	if rerr == nil {
		deliver(img)
	}
	_, _ = io.Copy(io.Discard, stdout) // whatever is left, so Wait never blocks on the pipe
	<-logged
	werr := cmd.Wait()
	p.mu.Lock()
	p.proc = nil
	p.mu.Unlock()
	if rep != nil {
		hook(rep)
	}
	if rerr != nil {
		if werr != nil {
			return fmt.Errorf("renderer: %v (%v)", werr, rerr)
		}
		return rerr
	}
	return nil
}

// relayPreviewLog copies the renderer's log lines into the window's,
// marked as the preview's, until it closes: always drained (a line past
// 4 KB is cut and the rest discarded), so the renderer never blocks on a
// full pipe. Its lines are the demo workspace's: no account or message of
// the window's is in that process.
func relayPreviewLog(r io.Reader) {
	br := bufio.NewReaderSize(r, 4096)
	for {
		line, err := br.ReadSlice('\n')
		if len(line) > 0 {
			log.Printf("ui: intro preview: %s", bytes.TrimRight(line, "\r\n"))
		}
		for err == bufio.ErrBufferFull { // the rest of a long line
			_, err = br.ReadSlice('\n')
		}
		if err != nil {
			_, _ = io.Copy(io.Discard, br)
			return
		}
	}
}

// readPreviewFrame reads the renderer's "CLACKPV1 w h" line, its pixels
// and (report) its report line.
func readPreviewFrame(r *bufio.Reader, want image.Point, report bool) (*image.RGBA, *introPreviewReport, error) {
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, nil, fmt.Errorf("no frame: %w", err)
		}
		var w, h int
		if n, _ := fmt.Sscanf(strings.TrimSpace(line), introPreviewMagic+" %d %d", &w, &h); n != 2 {
			continue // not ours (a test binary's own output)
		}
		if w != want.X || h != want.Y {
			return nil, nil, fmt.Errorf("frame %dx%d, asked for %v", w, h, want)
		}
		img := image.NewRGBA(image.Rectangle{Max: want})
		if _, err := io.ReadFull(r, img.Pix); err != nil {
			return nil, nil, fmt.Errorf("frame cut short: %w", err)
		}
		if !report {
			return img, nil, nil
		}
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return img, nil, nil
			}
			if s, ok := strings.CutPrefix(line, "report "); ok {
				rep := new(introPreviewReport)
				if json.Unmarshal([]byte(s), rep) == nil {
					return img, rep, nil
				}
			}
		}
	}
}

// removePreviewDir deletes a renderer's folder: only one under the temp
// directory with the preview's prefix.
func removePreviewDir(dir string) {
	if dir == "" || filepath.Dir(dir) != filepath.Clean(os.TempDir()) || !strings.HasPrefix(filepath.Base(dir), previewDirPrefix) {
		return
	}
	_ = os.RemoveAll(dir)
}

// sweepPreviewDirs removes renderer folders a crashed or killed Clack left
// over a day ago.
func sweepPreviewDirs() {
	ms, _ := filepath.Glob(filepath.Join(os.TempDir(), previewDirPrefix+"*"))
	for _, d := range ms {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() && time.Since(fi.ModTime()) > 24*time.Hour {
			removePreviewDir(d)
		}
	}
}

// close stops the renderer if it is still running and drops the image
// (nil-safe, idempotent; UI goroutine). Its goroutine removes the folder.
func (p *introPreview) close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.closed = true
	if p.proc != nil {
		_ = p.proc.Kill()
	}
	p.mu.Unlock()
	p.img, p.op = nil, paint.ImageOp{}
}

// shutdownIntro is the window closing: the introduction's renderer is
// stopped, and quit waits (up to 3 s) for every renderer started so far,
// this introduction's and any an earlier one closed, to exit and its
// folder to go.
func (a *App) shutdownIntro() {
	if in := a.intro; in != nil {
		in.preview.close()
	}
	done := make(chan struct{})
	go func() { a.previews.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
}

// cardRect is the card in device px: centred in the stage, cardW×cardH
// stage units.
func (p *introPreview) cardRect(f *introFrame) image.Rectangle {
	k := f.kit(f.in.geom(f))
	return k.rect((k.w-cardW)/2, 0, cardW, cardH)
}

// draw paints the image in r (device px) under the card's rounded clip.
func (p *introPreview) draw(f *introFrame, r image.Rectangle, rad int) {
	ops := f.gtx.Ops
	p.card = r
	defer clip.UniformRRect(r, rad).Push(ops).Pop()
	sz := p.img.Bounds().Size()
	aff := f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(float32(r.Dx())/float32(sz.X), float32(r.Dy())/float32(sz.Y))).Offset(layout.FPt(r.Min))
	defer op.Affine(aff).Push(ops).Pop()
	p.op.Add(ops)
	paint.PaintOp{}.Add(ops)
}
