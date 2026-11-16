package jsrt

import (
	"bytes"
	"container/heap"
	"context"
	"fmt"
	"log"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/awake"
)

// Runtime is one JS engine instance with its own JS thread (a goroutine).
// All JS work is serialized through it.
type Runtime struct {
	opts   Options
	ctx    context.Context // cancelled by Close
	cancel context.CancelFunc

	mu       sync.Mutex
	queue    []task // waiting tasks (mu); enqueue never grows it while holding mu
	spare    []task // the loop's last drained queue, cleared, for the next swap (mu)
	enqN     atomic.Uint64
	modules  map[string]string
	wake     chan struct{}
	done     chan struct{} // closed when the loop exits (or the watchdog gives up on it)
	doneOnce sync.Once
	closed   atomic.Bool

	globalMu sync.RWMutex
	global   map[string]int // window/document listener counts by event type

	goid      atomic.Int64         // JS thread goroutine id
	deadline  atomic.Int64         // monoNow; the running task is interrupted after it (0 = none)
	taskStart atomic.Int64         // monoNow the running task began (0 = idle)
	running   atomic.Pointer[task] // the macrotask on the JS thread, for timeout lines (nil = none)

	interrupts atomic.Uint64 // watchdog interrupts (Interrupts)

	// For freeze reports (Snapshots), written on the JS thread.
	lastExit  atomic.Int64  // monoNow the JS thread last polled its interrupt or yielded
	yields    atomic.Uint64 // host_yield calls (csrc/quickjs-yield.patch)
	gcStart   atomic.Int64  // monoNow QuickJS's running collection began (0 = none)
	gcLast    atomic.Int64  // the last collection's length (ns)
	gcObjects atomic.Uint32 // objects the last collection scanned
	gcs       atomic.Uint64 // collections so far
	heap      atomic.Uint64 // JS heap bytes << 32 | live allocations (jr_heap)
	wasmMem   wasmMemStats  // the engine's wasm memory (wasmmem.go)

	// Released handles (releaseLater): a GC cycle's cleanups free them by
	// the thousand, as batches of one task each.
	relMu    sync.Mutex
	relOpen  *relBatch     // the batch still taking handles (relMu)
	relSeq   uint64        // enqN when relOpen's task was queued; 0 = not yet (relMu)
	released atomic.Uint64 // handles releaseLater's batches freed

	// JS-thread-only state below.
	eng        *engine
	js         *Ctx
	timers     timerHeap
	timerN     int
	hosts      map[uint32]HostFunc
	hostN      uint32
	roots      map[uint64]*Root
	onDoc      func(body []byte, css []string)
	onDocProps func(html, body []byte)
	onCommit   func(id uint64, tree []byte)
	fetches    map[int]context.CancelFunc
	opens      []time.Time // recent external opens (allowOpen)
	openWarned bool
	start      time.Time

	kvs      map[string]KV
	builtins map[string]string
	bootTime time.Duration
	phases   []Phase

	gcDepth   int    // nesting of QuickJS collections (gcEvent)
	slowGCs   int    // slow collections not logged yet
	slowGCMsg string // the latest one, logged after the task
	slowGCLog int64  // monoNow of the last slow-collection line

	initErr error
	ready   chan struct{}
}

// New starts a runtime: it instantiates the engine and evaluates the
// browser environment and React. Call it lazily, when the first plugin is
// enabled; it costs nothing before.
func New(opts Options) (*Runtime, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Second
	}
	if opts.MemoryLimit <= 0 {
		opts.MemoryLimit = 512 << 20
	}
	if opts.Log == nil {
		opts.Log = func(level, msg string) { log.Printf("jsrt %s: %s", level, msg) }
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{
		opts:    opts,
		ctx:     ctx,
		cancel:  cancel,
		modules: map[string]string{},
		queue:   make([]task, 0, 64),
		spare:   make([]task, 0, 64),
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
		roots:   map[uint64]*Root{},
		hosts:   map[uint32]HostFunc{},
		fetches: map[int]context.CancelFunc{},
		start:   time.Now(),
		ready:   make(chan struct{}),
	}
	live.Store(r, struct{}{})
	go r.loop()
	<-r.ready
	if r.initErr != nil {
		r.Close()
		return nil, r.initErr
	}
	return r, nil
}

// Close stops the JS thread and frees the engine. Pending Do calls return
// ErrClosed. Safe to call more than once, from any goroutine (also from the
// JS thread).
func (r *Runtime) Close() {
	if r == nil || !r.closed.CompareAndSwap(false, true) {
		return
	}
	r.cancel()
	r.deadline.Store(monoNow()) // interrupt whatever runs now
	r.signal()
}

// Done is closed once the runtime has shut down.
func (r *Runtime) Done() <-chan struct{} { return r.done }

// Post queues f to run on the JS thread. Any goroutine; never blocks. After
// f returns, pending Promise jobs run and dirty React roots are flushed.
func (r *Runtime) Post(f func(js *Ctx)) {
	r.enqueue(task{kind: "Post", pc: callerPC(1), f: func() { f(r.js) }})
}

// Do runs f on the JS thread and waits for it (bounded by Options.Timeout).
// Called from the JS thread itself (inside a HostFunc, Post or another Do),
// it runs f inline.
func (r *Runtime) Do(f func(js *Ctx) error) error {
	return r.do(task{kind: "Do", pc: callerPC(1)}, f)
}

// do is Do with t naming the task (t.f is set here).
func (r *Runtime) do(t task, f func(js *Ctx) error) error {
	if r.closed.Load() {
		return ErrClosed
	}
	if r.onJSThread() {
		return f(r.js)
	}
	ch := make(chan error, 1)
	t.f = func() {
		defer func() {
			if p := recover(); p != nil {
				ch <- fmt.Errorf("jsrt: panic in Do: %v", p)
				r.afterPanic(p)
			}
		}()
		ch <- f(r.js)
	}
	r.enqueue(t)
	select {
	case err := <-ch:
		return err
	case <-r.done:
		select {
		case err := <-ch:
			return err
		default:
			return ErrClosed
		}
	}
}

// enqueue adds t to the queue and returns how many tasks were ever
// queued, t included. It allocates nothing while it holds mu: every
// goroutine that talks to JS posts here, the UI goroutine among them, and
// an allocation can make a goroutine help the collector (mark assist) for
// a long while, with everyone queued behind the lock. A full queue grows
// outside the lock.
func (r *Runtime) enqueue(t task) uint64 {
	for {
		r.mu.Lock()
		if len(r.queue) < cap(r.queue) {
			r.queue = append(r.queue, t) // room: no allocation
			n := r.enqN.Add(1)
			r.mu.Unlock()
			r.signal()
			return n
		}
		c := cap(r.queue)
		r.mu.Unlock()
		grown := make([]task, 0, max(2*c, 64))
		r.mu.Lock()
		if len(r.queue) == cap(r.queue) && len(r.queue) <= cap(grown) { // still full, and it fits
			r.queue = append(grown, r.queue...) // a copy within capacity
		}
		r.mu.Unlock()
	}
}

func (r *Runtime) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Runtime) onJSThread() bool { return r.goid.Load() == curGoid() }

// OnJSThread reports whether the caller runs on the JS thread (inside a
// task, a HostFunc or a Post): waiting there for Done would never return.
func (r *Runtime) OnJSThread() bool { return r.onJSThread() }

func curGoid() int64 {
	var buf [64]byte
	b := buf[:runtime.Stack(buf[:], false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	if i := bytes.IndexByte(b, ' '); i > 0 {
		b = b[:i]
	}
	n, _ := strconv.ParseInt(string(b), 10, 64)
	return n
}

func (r *Runtime) loop() {
	r.goid.Store(curGoid())
	defer r.markDone()
	defer live.Delete(r) // before Done fires; an abandoned runtime stays listed while it still runs
	r.js = &Ctx{r: r}
	// boot runs outside runTask but holds the JS thread in wasm (the
	// environment and React's bundles) like a task, so Snapshots (freeze
	// reports) and runningLabel name it instead of calling the runtime
	// idle. It has no deadline: nothing interrupts it but Close.
	bootTask := task{kind: "boot", start: time.Now(), mono: monoNow()}
	r.running.Store(&bootTask)
	r.taskStart.Store(bootTask.mono)
	r.initErr = r.boot()
	r.taskStart.Store(0)
	r.running.Store(nil)
	close(r.ready)
	if r.initErr != nil {
		if r.eng != nil {
			r.eng.close()
			r.eng = nil
		}
		return
	}
	defer r.shutdown()
	go r.watchdog()
	idle := time.NewTimer(time.Hour)
	defer idle.Stop()
	for !r.closed.Load() {
		// swap in the spare queue, so enqueue appends into room it has
		r.mu.Lock()
		q := r.queue
		r.queue, r.spare = r.spare[:0], nil
		r.mu.Unlock()
		for i := range q {
			if r.closed.Load() {
				return
			}
			r.runTask(q[i])
		}
		clear(q) // the tasks' closures and whatever they hold
		r.mu.Lock()
		if r.spare == nil {
			r.spare = q[:0]
		}
		r.mu.Unlock()
		r.fireTimers()
		if r.closed.Load() {
			return
		}
		r.mu.Lock()
		pending := len(r.queue)
		r.mu.Unlock()
		if pending > 0 {
			continue
		}
		wait := time.Hour
		if len(r.timers) > 0 {
			wait = time.Until(r.timers[0].at)
			if wait <= 0 {
				continue
			}
		}
		idle.Reset(wait)
		select {
		case <-r.wake:
		case <-idle.C:
		}
		if !idle.Stop() {
			select {
			case <-idle.C:
			default:
			}
		}
	}
}

// runTask runs one macrotask with a deadline, then the microtask checkpoint
// and the render flush.
func (r *Runtime) runTask(t task) {
	defer func() {
		r.deadline.Store(0)
		r.taskStart.Store(0)
		r.running.Store(nil)
		if p := recover(); p != nil {
			r.afterPanic(p)
		}
		r.logSlowGC()
	}()
	start := monoNow()
	t.start, t.mono = time.Now(), start
	r.running.Store(&t)
	r.taskStart.Store(start)
	r.deadline.Store(start + int64(r.opts.Timeout))
	t.f()
	r.checkpoint()
}

// task is one macrotask on the JS thread, and what the timeout lines say
// about it: ids only (a Call path, a timer id, the Go function that
// queued a Post or Do), never arguments.
type task struct {
	f     func()
	kind  string  // "Call", "Do", "Post", "timer", "setImmediate", "release", "boot"
	what  string  // the Call's path, the timer's id
	pc    uintptr // who queued it (Post, Do): a return address, 0 = none
	start time.Time
	mono  int64 // monoNow at start
}

// callerPC is the return address into the caller of the function calling
// callerPC, skip frames further up (callerPC(1) in Post: Post's caller).
func callerPC(skip int) uintptr {
	var pc [1]uintptr
	if runtime.Callers(skip+2, pc[:]) == 0 {
		return 0
	}
	return pc[0]
}

// name names t: "Call taut.rtmEvent", "Post from
// taut.(*Host).render.func1", "timer 12", "boot".
func (t *task) name() string {
	s := t.kind
	if t.what != "" {
		s += " " + t.what
	}
	if t.pc != 0 {
		fr, _ := runtime.CallersFrames([]uintptr{t.pc}).Next()
		if fn := fr.Function; fn != "" {
			if i := strings.LastIndexByte(fn, '/'); i >= 0 {
				fn = fn[i+1:]
			}
			s += " from " + fn
		}
	}
	return s
}

// label is name() for a log line, with how long t has run so far.
func (t *task) label() string {
	s := t.name()
	if !t.start.IsZero() {
		ran := time.Duration(monoNow() - t.mono)
		s += fmt.Sprintf(", running %v", ran.Round(time.Millisecond))
		// The wall clock goes on while the computer sleeps and the awake
		// one (ran, and the deadline: monoNow) doesn't: a wide gap means
		// the system slept during the task.
		if wall := time.Now().Round(0).Sub(t.start.Round(0)); wall-ran > 2*time.Second {
			s += fmt.Sprintf(", %v on the wall clock (the system slept?)", wall.Round(time.Second))
		}
	}
	return s
}

// runningLabel is label() of the task on the JS thread ("" between tasks).
// Any goroutine.
func (r *Runtime) runningLabel() string {
	if t := r.running.Load(); t != nil {
		return t.label()
	}
	return ""
}

// afterPanic handles a panic on the JS thread: a Go panic in a task is
// logged and the loop goes on; an engine trap (the wasm state can't be
// trusted afterwards) closes the runtime, so callers see ErrClosed instead
// of Clack crashing.
func (r *Runtime) afterPanic(p any) {
	err, _ := p.(error)
	if err != nil && strings.Contains(err.Error(), "jsrt: engine trap") {
		r.opts.Log("error", fmt.Sprintf("jsrt: engine trapped, closing the runtime: %v", err))
		r.Close()
		return
	}
	r.opts.Log("error", fmt.Sprintf("jsrt: panic on the JS thread: %v\n%s", p, debug.Stack()))
}

// checkpoint drains Promise jobs and flushes React roots and the document.
func (r *Runtime) checkpoint() {
	if r.eng == nil {
		return
	}
	r.eng.runJobs(r.js)
	r.flushRender()
	r.eng.gcFloor()
	if r.eng != nil && r.eng.fns["jr_heap"] != nil && r.eng.mod != nil {
		r.heap.Store(r.eng.call("jr_heap"))
	}
}

// watchdog backs up the interrupt handler. QuickJS polls it in JS code,
// regexps and the generic array methods (csrc/quickjs-interrupt.patch), but
// a native loop that never polls would hold the JS thread forever. Once a
// task is max(Timeout, 1 s) past its deadline the Runtime is given up on:
// it closes and Done fires, so Do and Call callers get ErrClosed instead of
// waiting forever and Taut starts a fresh runtime. The stuck goroutine
// can't be stopped from outside the wasm; if it ever returns it shuts its
// engine down.
func (r *Runtime) watchdog() {
	grace := r.opts.Timeout
	if grace < time.Second {
		grace = time.Second
	}
	tick := time.NewTicker(grace / 4)
	defer tick.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-tick.C:
		}
		if d := r.deadline.Load(); d != 0 && monoNow() > d+int64(grace) {
			what := ""
			if l := r.runningLabel(); l != "" {
				what = " (" + l + ")"
			}
			r.opts.Log("error", fmt.Sprintf("jsrt: a task%s ignored its interrupt for %v past its deadline; abandoning the JS runtime", what, grace))
			r.Close()
			r.markDone()
			return
		}
	}
}

// markDone closes done once (the loop exiting, or the watchdog giving up).
func (r *Runtime) markDone() { r.doneOnce.Do(func() { close(r.done) }) }

// Interrupts counts the times the watchdog interrupted JS (a task over
// Options.Timeout, or Close). QuickJS settles nothing an interrupted
// Promise job would have settled, so whatever awaits it waits forever;
// callers waiting on a promise can use this to stop. Any goroutine.
func (r *Runtime) Interrupts() uint64 { return r.interrupts.Load() }

// monoNow is the clock of every jsrt deadline and diagnostic: ns of awake
// time (internal/awake), never 0. Unlike the wall clock it stands still
// while the computer sleeps, on Linux, macOS and Windows (where Go's own
// monotonic clock counts sleep and awake subtracts it), so a task that was
// running when the lid closed isn't past its deadline on wake and the
// watchdog doesn't abandon the runtime (and restart every plugin) over a
// sleep.
func monoNow() int64 { return awake.Now() }

// ---- timers (setTimeout & co. are JS wrappers around these) ----

type timer struct {
	at  time.Time
	id  int
	seq int
	idx int
}

type timerHeap []*timer

func (h timerHeap) Len() int { return len(h) }
func (h timerHeap) Less(i, j int) bool {
	if h[i].at.Equal(h[j].at) {
		return h[i].seq < h[j].seq
	}
	return h[i].at.Before(h[j].at)
}
func (h timerHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i]; h[i].idx = i; h[j].idx = j }
func (h *timerHeap) Push(x any)   { t := x.(*timer); t.idx = len(*h); *h = append(*h, t) }
func (h *timerHeap) Pop() any {
	old := *h
	t := old[len(old)-1]
	*h = old[:len(old)-1]
	t.idx = -1
	return t
}
func (h *timerHeap) remove(id int) {
	for i, t := range *h {
		if t.id == id {
			heap.Remove(h, i)
			return
		}
	}
}

// addTimer schedules JS timer id to fire after d (JS thread).
func (r *Runtime) addTimer(id int, d time.Duration) {
	r.timerN++
	heap.Push(&r.timers, &timer{at: time.Now().Add(d), id: id, seq: r.timerN})
}

func (r *Runtime) fireTimers() {
	now := time.Now()
	for len(r.timers) > 0 && !r.timers[0].at.After(now) && !r.closed.Load() {
		t := heap.Pop(&r.timers).(*timer)
		id := t.id
		r.runTask(task{kind: "timer", what: strconv.Itoa(id), f: func() { r.eng.fireTimer(r.js, id) }})
	}
}

func (r *Runtime) shutdown() {
	for _, rt := range r.roots {
		rt.closed = true
	}
	if r.eng != nil {
		r.eng.close()
		r.eng = nil
	}
}
