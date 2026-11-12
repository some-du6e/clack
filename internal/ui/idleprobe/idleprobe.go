// Package idleprobe counts the frames a Gio window draws while nobody
// touches it, and names the code that asked for each one. Tests only
// (internal/ui TestIdleFrames, internal/ui/huddleui
// TestHuddleWindowIdleFrames); nothing in the app imports it.
//
// A Gio window draws a frame when
//
//   - a frame executed an op.InvalidateCmd: the router keeps the earliest
//     At, and the window draws then (now, if At is zero or past, at the
//     display's rate);
//   - the router still holds events after the frame;
//   - something called app.Window.Invalidate (Clack: App.Invalidate,
//     the huddle window's invalidate), from any goroutine.
//
// Run reproduces that loop on the wall clock against a real input.Router,
// with Gio patch 10's input.ExecuteHook recording who executed each
// InvalidateCmd and Invalidated recording who called Invalidate.
package idleprobe

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/op"
)

// Vsync is the display's frame interval: a window that asks for a frame
// now gets it at the next vsync.
const Vsync = time.Second / 60

// Probe collects one window's wake-up requests.
type Probe struct {
	mu      sync.Mutex
	wake    chan struct{}
	invs    map[string]int // Invalidate callers since the last frame
	cmds    []cmd          // this frame's InvalidateCmds
	inFrame bool
}

type cmd struct {
	at  time.Time
	who string
}

var (
	hookMu  sync.Mutex
	current *Probe
)

// New makes a probe and points Gio's ExecuteHook at it (one probe at a
// time per process; Close lets go).
func New() *Probe {
	p := &Probe{wake: make(chan struct{}, 1), invs: map[string]int{}}
	hookMu.Lock()
	current = p
	hookMu.Unlock()
	input.ExecuteHook = executeHook
	return p
}

// Close detaches the probe.
func (p *Probe) Close() {
	hookMu.Lock()
	if current == p {
		current = nil
	}
	hookMu.Unlock()
}

func executeHook(c input.Command) {
	ic, ok := c.(op.InvalidateCmd)
	if !ok {
		return
	}
	hookMu.Lock()
	p := current
	hookMu.Unlock()
	if p == nil {
		return
	}
	who := Caller(1)
	p.mu.Lock()
	if p.inFrame {
		p.cmds = append(p.cmds, cmd{at: ic.At, who: who})
	}
	p.mu.Unlock()
}

// Invalidated records a window Invalidate (call it from the window's
// invalidate path, any goroutine) and wakes Run.
func (p *Probe) Invalidated() {
	who := Caller(1)
	p.mu.Lock()
	p.invs[who]++
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// skipFrame reports frames that only pass a wake-up along: Gio's command
// plumbing and the invalidate helpers every caller goes through.
func skipFrame(fn string) bool {
	for _, pre := range []string{
		"runtime.", "gioui.org/io/input.", "gioui.org/layout.", "gioui.org/op.",
		"github.com/some-du6e/clack/internal/ui/idleprobe.",
	} {
		if strings.HasPrefix(fn, pre) {
			return true
		}
	}
	for _, suf := range []string{
		"internal/ui.(*App).Invalidate", "internal/ui.(*App).Post",
		"internal/ui/huddleui.(*window).invalidate",
	} {
		if strings.HasSuffix(fn, suf) {
			return true
		}
	}
	// the tests' per-window filters (TestIdleFrames.func1,
	// TestHuddleWindowIdleFrames.func1)
	return strings.Contains(fn, "IdleFrames.func1")
}

// Caller names the code that asked for a frame: the first two frames
// outside Gio's plumbing (skip more frames with skip), as
// "pkg.Func:line < pkg.Caller:line".
func Caller(skip int) string {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(skip+2, pcs)
	fr := runtime.CallersFrames(pcs[:n])
	var out []string
	for {
		f, more := fr.Next()
		if f.Function != "" && !skipFrame(f.Function) {
			fn := strings.TrimPrefix(f.Function, "github.com/some-du6e/clack/internal/")
			out = append(out, fmt.Sprintf("%s:%d", fn, f.Line))
			if len(out) == 2 {
				break
			}
		}
		if !more {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return strings.Join(out, " < ")
}

// Frame is one frame drawn while idle.
type Frame struct {
	At     time.Duration // since the idle period began
	Causes []string
}

// Result is an idle period's frames.
type Result struct {
	Name   string
	Dur    time.Duration
	Frames []Frame
}

// Count is the number of frames drawn.
func (r Result) Count() int { return len(r.Frames) }

// CountMatching is the number of frames every cause of which contains one
// of subs (e.g. the caret's blink).
func (r Result) CountMatching(subs ...string) int {
	n := 0
	for _, f := range r.Frames {
		all := len(f.Causes) > 0
		for _, c := range f.Causes {
			hit := false
			for _, s := range subs {
				if strings.Contains(c, s) {
					hit = true
				}
			}
			all = all && hit
		}
		if all {
			n++
		}
	}
	return n
}

// Log writes the frame count, the causes by frequency and the first
// frames' timeline.
func (r Result) Log(t testing.TB) {
	t.Helper()
	by := map[string]int{}
	for _, f := range r.Frames {
		for _, c := range f.Causes {
			by[c]++
		}
	}
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return by[keys[i]] > by[keys[j]] || by[keys[i]] == by[keys[j]] && keys[i] < keys[j]
	})
	var sb strings.Builder
	fmt.Fprintf(&sb, "idle-frames %s: %d frames in %v idle\n", r.Name, len(r.Frames), r.Dur)
	for _, k := range keys {
		fmt.Fprintf(&sb, "  %5d  %s\n", by[k], k)
	}
	for i, f := range r.Frames {
		if i == 40 {
			fmt.Fprintf(&sb, "  … %d more frames\n", len(r.Frames)-i)
			break
		}
		fmt.Fprintf(&sb, "  @%7.1fms  %s\n", float64(f.At)/float64(time.Millisecond), strings.Join(f.Causes, " | "))
	}
	t.Log(sb.String())
}

// Run lets the window sit for d with no input and records every frame it
// draws. frame lays out one frame at now and ends with r.Frame (the way
// the window's FrameEvent does); Run calls it once first, uncounted, to
// collect the wake-ups the settled window asks for.
func (p *Probe) Run(name string, d time.Duration, r *input.Router, frame func(now time.Time)) Result {
	p.mu.Lock()
	clear(p.invs)
	p.mu.Unlock()
	select { // an Invalidate before the period counts in it only if it comes again
	case <-p.wake:
	default:
	}
	pending := p.frame(r, time.Now(), frame)
	start := time.Now()
	end := time.After(d)
	res := Result{Name: name, Dur: d}
	last := start
	for {
		var timer <-chan time.Time
		if pending.ok {
			at := pending.at
			if next := last.Add(Vsync); at.Before(next) {
				at = next
			}
			timer = time.After(time.Until(at))
		}
		var causes []string
		select {
		case <-end:
			return res
		case <-timer:
			causes = pending.causes(time.Now())
		case <-p.wake:
			if wait := time.Until(last.Add(Vsync)); wait > 0 { // the next vsync
				time.Sleep(wait)
			}
		}
		now := time.Now()
		p.mu.Lock()
		for who, n := range p.invs {
			c := "Invalidate " + who
			if n > 1 {
				c += fmt.Sprintf(" (×%d)", n)
			}
			causes = append(causes, c)
		}
		clear(p.invs)
		p.mu.Unlock()
		select { // this frame covers them
		case <-p.wake:
		default:
		}
		if pending.ok && !now.Before(pending.at) && len(causes) == 0 {
			causes = pending.causes(now)
		}
		sort.Strings(causes)
		pending = p.frame(r, now, frame)
		res.Frames = append(res.Frames, Frame{At: now.Sub(start), Causes: causes})
		last = now
	}
}

// wakeup is what a frame left the window waiting for.
type wakeup struct {
	ok   bool
	at   time.Time
	cmds []cmd
}

// causes names the requests due at now (the router's own pending events
// when none is: Gio redraws while events are queued).
func (w wakeup) causes(now time.Time) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range w.cmds {
		if c.at.After(now) {
			continue
		}
		s := "InvalidateCmd " + c.who
		if c.at.IsZero() {
			s = "InvalidateCmd{} " + c.who
		}
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) == 0 && w.ok {
		out = append(out, "router: events pending after the frame")
	}
	return out
}

func (p *Probe) frame(r *input.Router, now time.Time, frame func(time.Time)) wakeup {
	p.mu.Lock()
	p.cmds = p.cmds[:0]
	p.inFrame = true
	p.mu.Unlock()
	frame(now)
	p.mu.Lock()
	p.inFrame = false
	cmds := append([]cmd(nil), p.cmds...)
	p.mu.Unlock()
	at, ok := r.WakeupTime()
	return wakeup{ok: ok, at: at, cmds: cmds}
}
