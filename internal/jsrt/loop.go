package jsrt

import (
	"bytes"
	"container/heap"
	"context"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Runtime is one JS engine instance with its own JS thread (a goroutine).
// All JS work is serialized through it.
type Runtime struct {
	opts   Options
	ctx    context.Context // cancelled by Close
	cancel context.CancelFunc

	mu      sync.Mutex
	queue   []func()
	modules map[string]string
	wake    chan struct{}
	done    chan struct{} // closed when the loop exits
	closed  atomic.Bool

	globalMu sync.RWMutex
	global   map[string]int // window/document listener counts by event type

	goid     atomic.Int64 // JS thread goroutine id
	deadline atomic.Int64 // unix nanos; the running task is interrupted after it (0 = none)

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
	start      time.Time

	kvs      map[string]KV
	builtins map[string]string
	bootTime time.Duration
	phases   []Phase

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
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
		roots:   map[uint64]*Root{},
		hosts:   map[uint32]HostFunc{},
		fetches: map[int]context.CancelFunc{},
		start:   time.Now(),
		ready:   make(chan struct{}),
	}
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
	r.deadline.Store(1) // interrupt whatever runs now
	r.signal()
}

// Done is closed once the runtime has shut down.
func (r *Runtime) Done() <-chan struct{} { return r.done }

// Post queues f to run on the JS thread. Any goroutine; never blocks. After
// f returns, pending Promise jobs run and dirty React roots are flushed.
func (r *Runtime) Post(f func(js *Ctx)) {
	r.enqueue(func() { f(r.js) })
}

// Do runs f on the JS thread and waits for it (bounded by Options.Timeout).
// Called from the JS thread itself (inside a HostFunc, Post or another Do),
// it runs f inline.
func (r *Runtime) Do(f func(js *Ctx) error) error {
	if r.closed.Load() {
		return ErrClosed
	}
	if r.onJSThread() {
		return f(r.js)
	}
	ch := make(chan error, 1)
	r.enqueue(func() {
		defer func() {
			if p := recover(); p != nil {
				ch <- fmt.Errorf("jsrt: panic: %v", p)
				panic(p)
			}
		}()
		ch <- f(r.js)
	})
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

func (r *Runtime) enqueue(f func()) {
	r.mu.Lock()
	r.queue = append(r.queue, f)
	r.mu.Unlock()
	r.signal()
}

func (r *Runtime) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Runtime) onJSThread() bool { return r.goid.Load() == curGoid() }

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
	defer close(r.done)
	r.js = &Ctx{r: r}
	r.initErr = r.boot()
	close(r.ready)
	if r.initErr != nil {
		return
	}
	defer r.shutdown()
	idle := time.NewTimer(time.Hour)
	defer idle.Stop()
	for !r.closed.Load() {
		r.mu.Lock()
		q := r.queue
		r.queue = nil
		r.mu.Unlock()
		for _, f := range q {
			if r.closed.Load() {
				return
			}
			r.runTask(f)
		}
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
func (r *Runtime) runTask(f func()) {
	r.deadline.Store(time.Now().Add(r.opts.Timeout).UnixNano())
	f()
	r.checkpoint()
	r.deadline.Store(0)
}

// checkpoint drains Promise jobs and flushes React roots and the document.
func (r *Runtime) checkpoint() {
	if r.eng == nil {
		return
	}
	r.eng.runJobs(r.js)
	r.flushRender()
	r.eng.gcFloor()
}

// interrupted is polled by the engine's interrupt handler.
func (r *Runtime) interrupted() bool {
	d := r.deadline.Load()
	return d != 0 && time.Now().UnixNano() > d
}

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
		r.runTask(func() { r.eng.fireTimer(r.js, t.id) })
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
