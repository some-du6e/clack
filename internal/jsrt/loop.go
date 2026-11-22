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

	// seq: the ExposeAsyncSeq calls waiting, run in order on one
	// goroutine while seqOn
	seqMu sync.Mutex
	seq   []func()
	seqOn bool

	mu    sync.Mutex
	queue []task // waiting tasks (mu); enqueue never grows it while holding mu
	spare []task // the loop's last drained queue, cleared, for the next swap (mu)
	// urgent are the user-blocking tasks (UserBlocking): each runs before
	// the next queued task, in the order they came (mu). urgentN is their
	// count, read between tasks without the lock.
	urgent  []task
	urgentN atomic.Int32
	// enqN numbers the tasks as they're queued (mu): the order in-order
	// work and the user's work keep (enqueue), never the clock, which
	// ticks with the clock interrupt on Windows (0.5-15.6 ms), so two
	// tasks queued in one tick had equal stamps and the later went first
	enqN atomic.Uint64
	// seenN is enqN of the newest task queued that JS can see run: every
	// task but a handle release's (releaseLater). A move joins its queued
	// task while that task is the newest seen (postMove): a release the
	// collector's cleanups queue between two moves runs no JS and sees no
	// event, and counting it split the moves in two (train-163's push ci
	// 45025576198, TestQueuedRectsAndMovesCoalesce: 2 mousemoves)
	seenN atomic.Uint64
	// ordQ are the in-order tasks (PostInOrder), from ordAt, in their
	// order (mu); ordN counts them, read between tasks without the lock.
	// They run ahead of the plain queue (runOrdered), and a user-blocking
	// task takes those queued before it ahead of itself (enqueue).
	ordQ  []task
	ordAt int
	ordN  atomic.Int32
	// batch is the queue the loop swapped out and runs now, from batchAt
	// (mu)
	batch   []task
	batchAt int
	// coalesce is bumped by every user-blocking task: a queued rects
	// update or continuous event takes newer ones only while it holds
	// (react.go), so nothing the user did comes between them
	coalesce atomic.Uint64
	rectsMu  sync.Mutex
	rectsQ   *rectsBatch // the boxes a queued setRects task carries (nil: none queued)
	movesMu  sync.Mutex
	movesQ   map[moveKey]*queuedMove // the continuous events queued, newest kept
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
	// relSeen, tests only: called on the JS thread with each batch's
	// handles once they're freed (release_batch_test.go's freedWatch).
	relSeen atomic.Pointer[func([]uint32)]

	// JS-thread-only state below.
	eng    *engine
	js     *Ctx
	timers timerHeap
	timerN int
	// urgentTimers counts the timers in timers a user-blocking task set
	urgentTimers int
	// holdUntil is when the hold a send's chain armed ends (monoNow), and
	// holdRoot that chain's root: awaitUserChain
	holdUntil  int64
	holdRoot   int64
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
		urgent:  make([]task, 0, 8),
		ordQ:    make([]task, 0, 32),
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

// PostUserBlocking is Post for the user's input (a click, a key, a
// control's callback): it runs ahead of the tasks queued before it, in
// order with the other user-blocking tasks (UserBlocking).
func (r *Runtime) PostUserBlocking(f func(js *Ctx)) {
	r.enqueue(task{kind: "Post", pc: callerPC(1), f: func() { f(r.js) }, urgent: true})
}

// PostInOrder is Post for a change to state JS reads (a store update, a
// socket event): a user-blocking task queued after it runs after it, as a
// browser's input handlers see the store as every earlier event left it.
// Rendering and other queued work stays behind.
func (r *Runtime) PostInOrder(f func(js *Ctx)) {
	r.enqueue(task{kind: "Post", pc: callerPC(1), f: func() { f(r.js) }, ordered: true})
}

// PostInOrderAs is PostInOrder with what the task carries (an event's
// name, never its payload) in the runtime's task lines, as a Call is named
// by its path.
func (r *Runtime) PostInOrderAs(what string, f func(js *Ctx)) {
	r.enqueue(task{kind: "Post", what: what, pc: callerPC(1), f: func() { f(r.js) }, ordered: true})
}

// post is Post on the user-blocking lane when urgent (the user's input:
// a discrete event, a control's callback), else in order with it (a
// continuous event, a viewport or visibility change: a later click runs
// after them, as a browser dispatches input in order; rendering waits).
func (r *Runtime) post(urgent bool, f func(js *Ctx)) {
	r.enqueue(task{kind: "Post", pc: callerPC(1), f: func() { f(r.js) }, urgent: urgent, ordered: !urgent})
}

// postChain is Post continuing the user-blocking chain l (chainLane): on
// the user's lane while the chain lasts (userChain from the user's action,
// counted when it settles: a fetch, a promise a Go function settles),
// else on the queue.
func (r *Runtime) postChain(l lane, f func(js *Ctx)) {
	if l.root != 0 && monoNow()-l.root > int64(userChain) {
		l = lane{}
	}
	r.enqueue(task{kind: "Post", pc: callerPC(1), f: func() { f(r.js) }, urgent: l.root != 0, root: l.root, chainSeq: l.seq, send: l.send, hold: l.hold})
}

// lane is a user-blocking chain as what its tasks start carries it: when
// the user's action began (root, monoNow; 0: none), where it was queued
// (seq: enqN), whether it is a send's (send: UserChain), which, going on,
// takes ahead only the in-order tasks queued before it began, and whether
// its trips through Go hold the queued tasks back for a moment (hold: not
// past its last hop, NoChain; awaitUserChain).
type lane struct {
	root       int64
	seq        uint64
	send, hold bool
}

// userChain bounds how long what a user-blocking task starts keeps the
// user's lane, from the user's action: a send's awaits (a membership
// fetch, a 5 s timeout) take seconds at most, and what a click starts
// that settles or fires later (an animation, a poll, background work)
// goes back to the queue after it. A var for tests.
var userChain = 10 * time.Second

// chainLane is, when the task running now is user-blocking, its chain
// (when it began: the user's action), for what it starts (a promise a Go
// function settles, a fetch, a timer) to continue on its lane; the zero
// lane when it isn't, or the chain is older than userChain (JS thread).
func (r *Runtime) chainLane() lane {
	t := r.running.Load()
	if t == nil || !t.urgent || t.root == 0 || monoNow()-t.root > int64(userChain) {
		return lane{}
	}
	return lane{root: t.root, seq: t.chainSeq, send: t.send, hold: t.hold}
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

// doCtx is do bounded by ctx while the task waits its turn: when ctx ends
// before it starts, it never runs and doCtx returns ctx's error, so a
// synchronous caller's deadline (the composer's 150 ms for a plugin's
// inline markup) caps its wait. Once the task runs, doCtx waits for it.
// Skipped, not run late, on purpose: a caller its deadline left with an
// error has gone on without the call (the composer drew the native
// markup, a dispatch ran its native), and the call run late applied its
// effect on top of what the caller did instead.
func (r *Runtime) doCtx(ctx context.Context, t task, f func(js *Ctx) error) error {
	if ctx.Done() == nil || r.onJSThread() {
		return r.do(t, f)
	}
	if r.closed.Load() {
		return ErrClosed
	}
	var state atomic.Int32 // 0 queued, 1 running, 2 given up
	ch := make(chan error, 1)
	t.f = func() {
		if !state.CompareAndSwap(0, 1) {
			return // its caller gave up on it while it waited
		}
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
	case <-ctx.Done():
		if state.CompareAndSwap(0, 2) {
			return ctx.Err()
		}
		select { // running: it answers
		case err := <-ch:
			return err
		case <-r.done:
			return ErrClosed
		}
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
	q := &r.queue
	switch {
	case t.urgent:
		q = &r.urgent
		r.coalesce.Add(1)
	case t.ordered:
		q = &r.ordQ
	}
	for {
		r.mu.Lock()
		need := 1
		if t.urgent {
			need += len(r.ordQ) - r.ordAt // the in-order tasks queued before it go first
		}
		if t.ordered && r.ordAt > 0 && len(r.ordQ)+need > cap(r.ordQ) {
			r.compactOrderedLocked() // room behind the run part, within capacity
		}
		if len(*q)+need <= cap(*q) {
			n := r.enqN.Add(1)
			moved := 0
			if t.urgent && len(r.ordQ) > r.ordAt {
				// every in-order task queued before it; a send's chain
				// going on takes those queued before the chain began, as
				// Slack's send is one JS task the store's later updates
				// wait for
				cutoff := n
				if t.send && t.chainSeq != 0 {
					cutoff = t.chainSeq
				}
				moved = r.promoteOrderedLocked(cutoff)
			}
			if t.urgent && t.root == 0 {
				t.root, t.chainSeq = monoNow(), n // the user's action: what it starts continues on its lane (chainLane)
			}
			t.seq = n
			if t.kind != "release" {
				r.seenN.Store(n)
			}
			*q = append(*q, t) // room: no allocation
			switch {
			case t.urgent:
				r.urgentN.Add(int32(1 + moved))
			case t.ordered:
				r.ordN.Add(1)
			}
			r.mu.Unlock()
			r.signal()
			if h := queueHook.Load(); h != nil {
				(*h)(QueuedTask{Name: t.name(), Urgent: t.urgent, Ordered: t.ordered})
			}
			return n
		}
		c, l := cap(*q), len(*q)
		r.mu.Unlock()
		grown := make([]task, 0, max(2*c, l+need, 64))
		r.mu.Lock()
		if len(*q) <= cap(grown) && cap(*q) < cap(grown) { // it fits, and it grew
			*q = append(grown, *q...) // a copy within capacity
		}
		r.mu.Unlock()
	}
}

// compactOrderedLocked drops the run part of ordQ (mu held), within its
// capacity.
func (r *Runtime) compactOrderedLocked() {
	n := copy(r.ordQ, r.ordQ[r.ordAt:])
	clear(r.ordQ[n:])
	r.ordQ, r.ordAt = r.ordQ[:n], 0
}

// promoteOrderedLocked moves the in-order tasks queued before cutoff (an
// enqN) to the end of the user-blocking queue, in their order (mu held;
// the urgent queue has room for them), and returns how many. They run as
// they were, without the user's lane for what they start: a store
// update's renders stay behind the user's work.
func (r *Runtime) promoteOrderedLocked(cutoff uint64) int {
	moved := 0
	for r.ordAt < len(r.ordQ) && r.ordQ[r.ordAt].seq < cutoff {
		x := r.ordQ[r.ordAt]
		x.ordered = false
		r.urgent = append(r.urgent, x)
		r.ordQ[r.ordAt] = task{}
		r.ordAt++
		moved++
	}
	if r.ordAt == len(r.ordQ) {
		r.ordQ, r.ordAt = r.ordQ[:0], 0
	}
	r.ordN.Add(int32(-moved))
	return moved
}

// User-blocking work. A browser runs input ahead of the tasks already
// queued: Chromium's scheduler gives input its own high-priority queue,
// React runs a discrete event's update synchronously, and the Scheduler
// API names the priority 'user-blocking'. So Slack's send checks, send
// transforms and the send thunks run the moment the user presses Enter,
// however much rendering is pending. On Clack's JS thread every Go → JS
// call waited its turn behind everything queued before it: plugin trees
// to render, mirror invalidations, view fetches, the socket's events,
// seconds of them on a busy workspace with plugins that patch every row
// (the perf job's plugin passes reported 4–10 s of backlog), and an own
// message waited for its send check through all of it before its pending
// copy showed (msg-latency).

type userBlockingKey struct{}

// UserBlocking marks ctx's calls (Call, CallThen) as the user's: each runs
// on the JS thread right after the task running now, ahead of every task
// queued before it, in order among themselves and with the user's input
// (PostUserBlocking: the clicks and keys before a send run before it) and
// after the in-order tasks queued before it (PostInOrder, InOrder: the
// store as earlier events left it, the native UI's earlier dispatches).
// For work the user is waiting on, such as sending a message; rendering
// and other queued work waits. What such a call starts (a promise a Go
// function settles, a fetch, a timer) continues on its lane while the
// chain lasts (userChain), ahead of what was queued meanwhile, though
// never of a task already running: a chain that needs Go between two JS
// steps (a send's check, then its thunk) belongs in one JS call
// (taut's checkThenDispatch), as Slack runs them in one task.
// setImmediate, MessageChannel and React's scheduler wakes go back to the
// queue.
func UserBlocking(ctx context.Context) context.Context {
	return context.WithValue(ctx, userBlockingKey{}, true)
}

type inOrderKey struct{}

// InOrder marks ctx's calls as in order with the user's work: a
// user-blocking task queued after one runs after it (PostInOrder's rule).
// For what the native UI dispatches (a reaction, a draft sync): a plugin
// click after it can't overtake it, as nothing the user did overtakes an
// earlier action of theirs in a browser.
func InOrder(ctx context.Context) context.Context {
	return context.WithValue(ctx, inOrderKey{}, true)
}

// IsInOrder reports whether ctx came from InOrder.
func IsInOrder(ctx context.Context) bool {
	b, _ := ctx.Value(inOrderKey{}).(bool)
	return b
}

type userChainKey struct{}

// UserChain is UserBlocking for a send, whose chain goes through Go
// between its JS steps: its checks answer Go, which dispatches the send's
// thunk, and its patches call the native, which queues the next hops (the
// send transforms, the sendMessage thunk) a goroutine switch later. When
// one of its tasks returns to Go, and while it awaits an async Go function
// (ExposeAsync), the loop starts no queued task for a moment
// (awaitUserChain), as Slack runs a whole send in one JS task: a render
// the loop started there held the next hop for its whole run, and the
// pending copy missed its frame. Other user-blocking work (a key, a
// click, a reaction) holds nothing.
func UserChain(ctx context.Context) context.Context {
	return context.WithValue(UserBlocking(ctx), userChainKey{}, true)
}

// IsUserChain reports whether ctx came from UserChain.
func IsUserChain(ctx context.Context) bool {
	b, _ := ctx.Value(userChainKey{}).(bool)
	return b
}

type noHoldKey struct{}

// NoChain keeps ctx's calls on a send's lane (UserChain: in order, ahead
// of the store updates that came during it) without the hold: the chain's
// last hop, after which only the network follows (a send's sendMessage
// thunk), or a send with no hop to come back.
func NoChain(ctx context.Context) context.Context {
	return context.WithValue(ctx, noHoldKey{}, true)
}

// IsNoChain reports whether ctx came from NoChain.
func IsNoChain(ctx context.Context) bool {
	b, _ := ctx.Value(noHoldKey{}).(bool)
	return b
}

type laneKey struct{}

// laneOf is the chain an async Go function's ctx carries (ExposeAsync:
// the lane of the task that called it): the user-blocking calls it makes
// go on with that chain, a send's next hop part of the send.
func laneOf(ctx context.Context) (lane, bool) {
	l, ok := ctx.Value(laneKey{}).(lane)
	return l, ok && l.root != 0 && monoNow()-l.root <= int64(userChain)
}

// Ordinary undoes UserBlocking for ctx's calls: a probe the user isn't
// waiting on (taut's sendCheckWaiting) queues behind the work queued
// before it.
func Ordinary(ctx context.Context) context.Context {
	return context.WithValue(ctx, userBlockingKey{}, false)
}

// IsUserBlocking reports whether ctx came from UserBlocking.
func IsUserBlocking(ctx context.Context) bool {
	b, _ := ctx.Value(userBlockingKey{}).(bool)
	return b
}

// userChainHold is how long the loop holds its queued tasks back after a
// send's chain went to Go (awaitUserChain): Go goes on with the chain a
// goroutine switch or two later (the native's transforms, its sendMessage
// thunk), and the chain's next task releases it; renders mustn't wait out
// a network call.
const userChainHold = 30 * time.Millisecond

// armHold starts or extends the hold for the chain at root (JS thread).
func (r *Runtime) armHold(root int64) {
	if at := monoNow() + int64(userChainHold); at > r.holdUntil {
		r.holdUntil = at
	}
	r.holdRoot = root
}

// awaitUserChain holds the queued tasks (in-order, plain and the queue's
// timers) back while a send's chain is in Go (UserChain: armHold), until
// its next task comes or userChainHold passes, running the user-blocking
// tasks and the user's timers as they come (JS thread). It reports false
// once the runtime closed.
func (r *Runtime) awaitUserChain() bool {
	for {
		if r.closed.Load() {
			return false
		}
		d := time.Duration(r.holdUntil - monoNow())
		if d <= 0 {
			return true
		}
		if r.urgentN.Load() > 0 || r.dueUrgentTimer() {
			if !r.runUrgent() {
				return false
			}
			continue
		}
		if at, ok := r.nextUrgentTimer(); ok {
			d = min(d, max(time.Until(at), 0))
		}
		t := time.NewTimer(d)
		select {
		case <-r.wake:
		case <-t.C:
		}
		t.Stop()
	}
}

// heldAwait arms the hold when the task running now, a send's chain's
// (UserChain, short of its last hop), calls an async Go function: its next
// hop comes from Go (JS thread).
func (r *Runtime) heldAwait() {
	if l := r.chainLane(); l.hold {
		r.armHold(l.root)
	}
}

// runUrgent runs the user-blocking tasks queued so far, then the
// user-blocking timers due, each after the in-order tasks queued before it
// (JS thread). It reports false once the runtime closed.
func (r *Runtime) runUrgent() bool {
	// the user's timers due now, not those their callbacks set meanwhile:
	// a setTimeout(0) yield loop gives the queue its turns; and only the
	// in-order tasks queued before the round found them due come first
	// (cut, set then)
	round := time.Now()
	var cut uint64
	for {
		for r.urgentN.Load() > 0 {
			fresh := make([]task, 0, 8) // outside the lock (enqueue's rule); u runs below
			r.mu.Lock()
			u := r.urgent
			r.urgent = fresh
			r.urgentN.Store(0)
			r.mu.Unlock()
			for i := range u {
				if r.closed.Load() {
					return false
				}
				r.runTask(u[i])
			}
		}
		if r.closed.Load() {
			return false
		}
		if r.urgentTimers == 0 || !r.dueUrgentTimerAt(round) {
			return true
		}
		// a user's timer runs after the in-order tasks queued before it
		// came due, as any of the user's work does; not after the ones
		// queued since: promoting every in-order task waiting, again after
		// each batch, a steady stream of them (socket events, store
		// updates) held the timer, and every plain task with it (the
		// renders, the views' reads), until the stream paused
		if cut == 0 {
			cut = r.enqN.Load() + 1
		}
		if r.promoteOrderedBefore(cut) {
			continue
		}
		if !r.fireUrgentTimer(round) {
			return false
		}
	}
}

// promoteOrderedBefore moves the in-order tasks queued before cutoff
// (enqN) to the user-blocking queue (promoteOrderedLocked) and reports
// whether there were any. As enqueue, it allocates nothing while it holds
// mu.
func (r *Runtime) promoteOrderedBefore(cutoff uint64) bool {
	for {
		r.mu.Lock()
		n := len(r.ordQ) - r.ordAt
		if n == 0 || r.ordQ[r.ordAt].seq >= cutoff {
			r.mu.Unlock()
			return false
		}
		if len(r.urgent)+n <= cap(r.urgent) {
			moved := r.promoteOrderedLocked(cutoff)
			r.urgentN.Add(int32(moved))
			r.mu.Unlock()
			return moved > 0
		}
		c, l := cap(r.urgent), len(r.urgent)
		r.mu.Unlock()
		grown := make([]task, 0, max(2*c, l+n, 64))
		r.mu.Lock()
		if len(r.urgent) <= cap(grown) && cap(r.urgent) < cap(grown) {
			r.urgent = append(grown, r.urgent...)
		}
		r.mu.Unlock()
	}
}

// orderedSlice is how long the in-order work may hold the JS thread with
// plain tasks waiting before one of them gets a turn (runOrdered): a busy
// socket mustn't starve the renders.
const orderedSlice = 50 * time.Millisecond

// runOrdered runs the in-order tasks queued so far, in their order, ahead
// of the plain queue, each after the user-blocking work queued by then,
// until they have held the thread for orderedSlice since the last plain
// task (since). Slack's store takes a socket event or a store update when
// it arrives, and the renders follow; a user's chain waiting on one
// (Owl's view_opened) waited behind every render queued before it. It
// reports false once the runtime closed (JS thread).
func (r *Runtime) runOrdered(since *int64) bool {
	for r.ordN.Load() > 0 {
		if r.closed.Load() {
			return false
		}
		if monoNow()-*since > int64(orderedSlice) {
			return true // a plain task's turn
		}
		r.mu.Lock()
		if r.ordAt >= len(r.ordQ) {
			r.mu.Unlock()
			break
		}
		t := r.ordQ[r.ordAt]
		r.ordQ[r.ordAt] = task{}
		r.ordAt++
		if r.ordAt == len(r.ordQ) {
			r.ordQ, r.ordAt = r.ordQ[:0], 0
		}
		r.ordN.Add(-1)
		r.mu.Unlock()
		r.runTask(t)
		if !r.runUrgent() || !r.awaitUserChain() {
			return false
		}
	}
	return !r.closed.Load()
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
		if !r.runUrgent() {
			return
		}
		// swap in the spare queue, so enqueue appends into room it has;
		// the batch is taken a task at a time under mu, as a user-blocking
		// task may take its in-order tasks ahead of itself (enqueue)
		r.mu.Lock()
		q := r.queue
		r.queue, r.spare = r.spare[:0], nil
		r.batch, r.batchAt = q, 0
		r.mu.Unlock()
		since := monoNow() // the last plain task (runOrdered's slice)
		for {
			// a user-blocking task that came meanwhile goes first, nothing
			// queued starts while a send's chain is in Go for a moment
			// (awaitUserChain), and the in-order work runs ahead of the
			// next plain task
			if r.closed.Load() || !r.runUrgent() || !r.awaitUserChain() || !r.runOrdered(&since) {
				return
			}
			r.mu.Lock()
			if r.batchAt >= len(r.batch) {
				r.batch, r.batchAt = nil, 0
				r.mu.Unlock()
				break
			}
			t := r.batch[r.batchAt]
			r.batch[r.batchAt] = task{} // the task's closure and whatever it holds
			r.batchAt++
			r.mu.Unlock()
			r.runTask(t)
			since = monoNow()
		}
		r.mu.Lock()
		if r.spare == nil {
			r.spare = q[:0]
		}
		r.mu.Unlock()
		if !r.runUrgent() || !r.awaitUserChain() || !r.runOrdered(&since) {
			return
		}
		r.fireTimers()
		if r.closed.Load() {
			return
		}
		r.mu.Lock()
		pending := len(r.queue) + len(r.urgent) + len(r.ordQ) - r.ordAt
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
	if t.urgent && t.root != 0 && t.root == r.holdRoot {
		r.holdUntil = 0 // the hop the hold waited for
	}
	r.running.Store(&t)
	r.taskStart.Store(start)
	r.deadline.Store(start + int64(r.opts.Timeout))
	t.f()
	r.checkpoint()
	if t.hold && t.kind == "Call" && monoNow()-t.root <= int64(userChain) {
		r.armHold(t.root) // a send's call answers Go, which goes on with the chain
	}
	if h := taskHook.Load(); h != nil {
		(*h)(TaskRun{Name: t.name(), Urgent: t.urgent, Hold: t.hold, Start: t.start, Took: time.Since(t.start)})
	}
}

// TaskRun is one task the JS thread ran (SetTaskHook).
type TaskRun struct {
	Name         string
	Urgent, Hold bool
	Start        time.Time
	Took         time.Duration
}

var taskHook atomic.Pointer[func(TaskRun)]

// SetTaskHook has every runtime report each task it ran to f, on the JS
// thread, until it is set to nil (tests: what ran between a key and its
// frame). f must not block.
func SetTaskHook(f func(TaskRun)) {
	if f == nil {
		taskHook.Store(nil)
		return
	}
	taskHook.Store(&f)
}

// QueuedTask is one task a runtime queued (SetQueueHook).
type QueuedTask struct {
	Name            string
	Urgent, Ordered bool
}

var queueHook atomic.Pointer[func(QueuedTask)]

// SetQueueHook has every runtime report each task it queues to f, on the
// goroutine that queued it, once it is queued, until it is set to nil
// (tests: an event reached the runtime before the user's input, without
// a sleep). f must not block.
func SetQueueHook(f func(QueuedTask)) {
	if f == nil {
		queueHook.Store(nil)
		return
	}
	queueHook.Store(&f)
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
	// urgent: user-blocking (UserBlocking), run ahead of the queue;
	// ordered: in order with the user-blocking tasks queued after it
	// (PostInOrder)
	urgent, ordered bool
	// root: when the user-blocking chain the task belongs to began, and
	// chainSeq where it was queued; send: the chain is a send's
	// (UserChain); hold: its trips through Go hold the queue (chainLane)
	root       int64
	chainSeq   uint64
	send, hold bool
	// seq: where the task was queued (enqN; promotion's cutoff)
	seq uint64
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
	// root, chainSeq: set by a user-blocking task, it fires on the user's
	// lane (chainLane's), without the hold: a timer is no trip through Go
	root     int64
	chainSeq uint64
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
func (h *timerHeap) remove(id int) *timer {
	for i, t := range *h {
		if t.id == id {
			heap.Remove(h, i)
			return t
		}
	}
	return nil
}

// addTimer schedules JS timer id to fire after d (JS thread). One a
// user-blocking task sets fires on the user's lane (runUrgent), unless it
// comes due past the chain's userChain.
func (r *Runtime) addTimer(id int, d time.Duration) {
	r.timerN++
	l := r.chainLane()
	if l.root != 0 && monoNow()+int64(d)-l.root > int64(userChain) {
		l = lane{}
	}
	if t := r.running.Load(); t != nil && t.kind == "timer" {
		// a timer's timer: the user's lane passes to what the input
		// started, not on down a yield loop (setTimeout(0),
		// requestIdleCallback), as no browser passes input priority on
		l = lane{}
	}
	t := &timer{at: time.Now().Add(d), id: id, seq: r.timerN, root: l.root, chainSeq: l.seq}
	if t.root != 0 {
		r.urgentTimers++
	}
	heap.Push(&r.timers, t)
}

// removeTimer is clearTimeout's (JS thread).
func (r *Runtime) removeTimer(id int) {
	if t := r.timers.remove(id); t != nil && t.root != 0 {
		r.urgentTimers--
	}
}

// fireTimers runs the timers due, each after the user-blocking tasks
// queued by then (JS thread).
func (r *Runtime) fireTimers() {
	now := time.Now()
	for len(r.timers) > 0 && !r.timers[0].at.After(now) && !r.closed.Load() {
		if !r.runUrgent() {
			return
		}
		if len(r.timers) == 0 || r.timers[0].at.After(now) {
			return
		}
		r.fireTimer(heap.Pop(&r.timers).(*timer))
	}
}

func (r *Runtime) fireTimer(t *timer) {
	if t.root != 0 {
		r.urgentTimers--
	}
	id := t.id
	r.runTask(task{kind: "timer", what: strconv.Itoa(id), urgent: t.root != 0, root: t.root, chainSeq: t.chainSeq, f: func() { r.eng.fireTimer(r.js, id) }})
}

// nextUrgentTimer is when the next user-blocking timer comes due (JS
// thread).
func (r *Runtime) nextUrgentTimer() (time.Time, bool) {
	var at time.Time
	ok := false
	for _, t := range r.timers {
		if t.root != 0 && (!ok || t.at.Before(at)) {
			at, ok = t.at, true
		}
	}
	return at, ok
}

// dueUrgentTimer reports whether a user-blocking timer is due (JS
// thread). One that comes due past its chain's userChain (the loop was
// busy) goes back to the queue's timers.
func (r *Runtime) dueUrgentTimer() bool { return r.dueUrgentTimerAt(time.Now()) }

// dueUrgentTimerAt is dueUrgentTimer as of now.
func (r *Runtime) dueUrgentTimerAt(now time.Time) bool {
	if r.urgentTimers == 0 {
		return false
	}
	mono := monoNow()
	due := false
	for _, t := range r.timers {
		if t.root == 0 || t.at.After(now) {
			continue
		}
		if mono-t.root > int64(userChain) {
			t.root = 0
			r.urgentTimers--
			continue
		}
		due = true
	}
	return due
}

// fireUrgentTimer fires the earliest user-blocking timer due as of now
// (JS thread). It reports false once the runtime closed.
func (r *Runtime) fireUrgentTimer(now time.Time) bool {
	var due *timer
	for _, t := range r.timers {
		if t.root != 0 && !t.at.After(now) && (due == nil || r.timers.lessTimer(t, due)) {
			due = t
		}
	}
	if due != nil {
		heap.Remove(&r.timers, due.idx)
		r.fireTimer(due)
	}
	return !r.closed.Load()
}

func (h timerHeap) lessTimer(a, b *timer) bool {
	if a.at.Equal(b.at) {
		return a.seq < b.seq
	}
	return a.at.Before(b.at)
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
