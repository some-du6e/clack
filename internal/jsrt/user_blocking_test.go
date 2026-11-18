package jsrt

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// A user-blocking task (UserBlocking) runs right after the task running
// when it came, ahead of every task queued before it, as a browser runs
// input ahead of its queued work; everything else keeps its order. An
// own message's send check used to wait behind seconds of queued plugin
// renders (msg-latency).
func TestUserBlockingRunsFirst(t *testing.T) {
	r := newTest(t)
	var mu sync.Mutex
	var order []string
	note := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	// hold runs a task that waits for release: the task running when the
	// others come (it is running when hold returns)
	var holds []chan struct{}
	t.Cleanup(func() { // a failed check must not leave the JS thread held
		for _, c := range holds {
			select {
			case <-c:
			default:
				close(c)
			}
		}
	})
	hold := func() chan struct{} {
		c, running := make(chan struct{}), make(chan struct{})
		holds = append(holds, c)
		r.Post(func(*Ctx) {
			close(running)
			<-c
		})
		<-running
		return c
	}
	const slow, n = 20 * time.Millisecond, 10
	queue := func() {
		for i := 0; i < n; i++ {
			i := i
			r.Post(func(*Ctx) {
				note(fmt.Sprint("queued ", i))
				time.Sleep(slow)
			})
		}
	}
	waitUrgent := func() {
		for dl := time.Now().Add(5 * time.Second); r.urgentN.Load() == 0; time.Sleep(time.Millisecond) {
			if time.Now().After(dl) {
				t.Fatal("the user-blocking task never queued")
			}
		}
	}

	// the order: the user's task first, then the queue as it was, then
	// what came after
	release := hold()
	queue()
	userDone := make(chan error, 1)
	go func() {
		userDone <- r.do(task{kind: "Do", urgent: true}, func(*Ctx) error { note("user"); return nil })
	}()
	waitUrgent()
	r.Post(func(*Ctx) { note("after") })
	close(release)
	if err := <-userDone; err != nil {
		t.Fatal(err)
	}
	if err := r.Do(func(*Ctx) error { return nil }); err != nil { // behind them all
		t.Fatal(err)
	}
	mu.Lock()
	got := strings.Join(order, ", ")
	mu.Unlock()
	want := "user, queued 0, queued 1, queued 2, queued 3, queued 4, queued 5, queued 6, queued 7, queued 8, queued 9, after"
	if got != want {
		t.Errorf("tasks ran %s, want %s", got, want)
	}

	// Call with a UserBlocking context: answered after the running task,
	// not after the n queued (n×slow); without it, after them all
	for _, user := range []bool{true, false} {
		release := hold()
		queue()
		ctx := context.Background()
		if user {
			ctx = UserBlocking(ctx)
		}
		answered := make(chan struct{})
		go func() {
			defer close(answered)
			if _, err := r.Call(ctx, "Math.max", 1, 2); err != nil {
				t.Error(err)
			}
		}()
		if user {
			waitUrgent()
		} else {
			time.Sleep(20 * time.Millisecond) // queued behind the others
		}
		t0 := time.Now()
		close(release)
		<-answered
		since := time.Since(t0)
		switch {
		case user && since > n*slow/2:
			t.Errorf("a user-blocking Call answered %v after the JS thread was free: it waited for the %d queued tasks (%v each)", since, n, slow)
		case !user && since < n*slow:
			t.Errorf("an ordinary Call answered %v after the JS thread was free, before the %d queued tasks (%v each) could have run", since, n, slow)
		}
		t.Logf("user-blocking %v: answered %v after the thread was free", user, since.Round(time.Millisecond))
		_ = r.Do(func(*Ctx) error { return nil }) // drain
	}
}

// laneRecorder notes what ran, in order, and holds the JS thread on
// request.
type laneRecorder struct {
	t     *testing.T
	r     *Runtime
	mu    sync.Mutex
	order []string
	holds []chan struct{}
}

func newLaneRecorder(t *testing.T) *laneRecorder {
	l := &laneRecorder{t: t, r: newTest(t)}
	t.Cleanup(func() { // a failed check must not leave the JS thread held
		for _, c := range l.holds {
			select {
			case <-c:
			default:
				close(c)
			}
		}
	})
	return l
}

func (l *laneRecorder) note(s string) {
	l.mu.Lock()
	l.order = append(l.order, s)
	l.mu.Unlock()
}

func (l *laneRecorder) got() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.order, ", ")
}

// post queues a task that notes s (Post, PostInOrder or
// PostUserBlocking).
func (l *laneRecorder) post(post func(func(*Ctx)), s string) {
	post(func(*Ctx) { l.note(s) })
}

// hold queues a task that notes s, then waits for the returned channel to
// close; with wait it returns once that task runs.
func (l *laneRecorder) hold(s string, wait bool) chan struct{} {
	c, running := make(chan struct{}), make(chan struct{})
	l.holds = append(l.holds, c)
	l.r.Post(func(*Ctx) {
		l.note(s)
		close(running)
		<-c
	})
	if wait {
		<-running
	}
	return c
}

// drain waits until every task queued so far ran.
func (l *laneRecorder) drain() {
	if err := l.r.Do(func(*Ctx) error { return nil }); err != nil {
		l.t.Fatal(err)
	}
}

// A user-blocking task that comes while the loop runs a batch it swapped
// out goes before the rest of that batch: the real backlog case, renders
// queued as one batch of seconds. Two keep their order.
func TestUserBlockingPreemptsBatch(t *testing.T) {
	l := newLaneRecorder(t)
	g := l.hold("G", true)
	// queued while G runs: H, then the batch behind it
	h := l.hold("H", false)
	for i := 0; i < 5; i++ {
		l.post(l.r.Post, fmt.Sprint("q", i))
	}
	close(g)
	for dl := time.Now().Add(5 * time.Second); !strings.HasSuffix(l.got(), "H"); time.Sleep(time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("H never ran")
		}
	}
	// H runs from the batch the loop swapped out; q0… wait in it
	l.post(l.r.PostUserBlocking, "U1")
	l.post(l.r.PostUserBlocking, "U2")
	close(h)
	l.drain()
	if got, want := l.got(), "G, H, U1, U2, q0, q1, q2, q3, q4"; got != want {
		t.Errorf("tasks ran %s, want %s", got, want)
	}
}

// The store's updates and the socket's events (PostInOrder) stay in order
// with the user's work queued after them, as a browser's input handlers
// read the store as every earlier event left it; the rendering queued
// among them still waits for the user's task. A send must not overtake
// the click before it (OwlMode's mode toggle) or the message it replies to.
func TestUserBlockingAfterInOrderTasks(t *testing.T) {
	l := newLaneRecorder(t)
	g := l.hold("G", true)
	l.post(l.r.Post, "render0")
	l.post(l.r.PostInOrder, "store0")
	h := l.hold("H", false) // the loop swaps G's queue in: store1 below waits in the queue, the rest in the batch
	l.post(l.r.Post, "render1")
	l.post(l.r.PostInOrder, "store1")
	close(g)
	for dl := time.Now().Add(5 * time.Second); !strings.HasSuffix(l.got(), "H"); time.Sleep(time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("H never ran")
		}
	}
	l.post(l.r.PostInOrder, "store2")
	l.post(l.r.PostUserBlocking, "click")
	l.post(l.r.PostInOrder, "store3") // after the click: stays behind it
	l.post(l.r.PostUserBlocking, "send")
	l.post(l.r.Post, "render2")
	close(h)
	l.drain()
	// the in-order tasks run ahead of the renders queued before them
	// (store0, store1); store2 goes ahead of the click, store3 between the
	// click and the send, and the renders after all of it
	if got, want := l.got(), "G, store0, store1, render0, H, store2, click, store3, send, render1, render2"; got != want {
		t.Errorf("tasks ran %s,\nwant %s", got, want)
	}
}

// What a user-blocking task starts continues on its lane: a promise a Go
// function settles, and a timer it sets, run ahead of the queue; a timer
// due behind a user-blocking task waits for it.
func TestUserBlockingContinuations(t *testing.T) {
	l := newLaneRecorder(t)
	r := l.r
	r.ExposeAsync("answerLater", func(ctx context.Context, _ []json.RawMessage) (any, error) {
		time.Sleep(30 * time.Millisecond)
		return 1, nil
	})
	if _, err := r.Eval("lane.js", `globalThis.lane = {
		async awaitGo() { await __clack.answerLater(); return 'go'; },
		timer() { return new Promise((res) => setTimeout(() => res('timer'), 5)); },
	}`); err != nil {
		t.Fatal(err)
	}
	const slow, n = 20 * time.Millisecond, 10
	for _, path := range []string{"lane.awaitGo", "lane.timer"} {
		g := l.hold("G", true)
		for i := 0; i < n; i++ {
			r.Post(func(*Ctx) { time.Sleep(slow) })
		}
		answered := make(chan time.Duration, 1)
		go func() {
			t0 := time.Now()
			if _, err := r.Call(UserBlocking(context.Background()), path); err != nil {
				t.Error(err)
			}
			answered <- time.Since(t0)
		}()
		for dl := time.Now().Add(5 * time.Second); r.urgentN.Load() == 0; time.Sleep(time.Millisecond) {
			if time.Now().After(dl) {
				t.Fatal("the user-blocking call never queued")
			}
		}
		close(g)
		if d := <-answered; d > n*slow/2+100*time.Millisecond {
			t.Errorf("%s answered %v after it was called: its continuation waited for the %d queued tasks (%v each)", path, d, n, slow)
		} else {
			t.Logf("%s answered in %v", path, d.Round(time.Millisecond))
		}
		l.drain()
	}

	// a due timer's turn comes after a user-blocking task that came while
	// the timers before it ran
	r.Expose("userNow", func(js *Ctx, _ Value, _ []Value) (any, error) {
		go r.PostUserBlocking(func(*Ctx) { l.note("user") })
		for dl := time.Now().Add(5 * time.Second); r.urgentN.Load() == 0 && time.Now().Before(dl); {
			time.Sleep(time.Millisecond)
		}
		return nil, nil
	})
	r.Expose("noteTimer", func(js *Ctx, _ Value, args []Value) (any, error) {
		l.note(args[0].String())
		return nil, nil
	})
	l.mu.Lock()
	l.order = nil
	l.mu.Unlock()
	if _, err := r.Eval("timers.js", `setTimeout(() => { __clack.noteTimer('t1'); __clack.userNow(); }, 0);
		setTimeout(() => __clack.noteTimer('t2'), 0);`); err != nil {
		t.Fatal(err)
	}
	for dl := time.Now().Add(5 * time.Second); !strings.Contains(l.got(), "t2"); time.Sleep(time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatalf("the timers never ran: %s", l.got())
		}
	}
	l.drain()
	if got, want := l.got(), "t1, user, t2"; got != want {
		t.Errorf("ran %s, want %s: the second timer went ahead of the user's task", got, want)
	}
}

// What a user-blocking task starts keeps the user's lane only while the
// chain lasts (userChain from the user's action), counted when it settles
// or fires: a Go answer, a timer due past it, or one the busy loop fires
// past it, waits its turn like any queued work. They kept the lane however
// late they came, background work included.
func TestUserBlockingChainEnds(t *testing.T) {
	old := userChain
	userChain = 100 * time.Millisecond
	t.Cleanup(func() { userChain = old })
	l := newLaneRecorder(t)
	r := l.r
	called, settle := make(chan struct{}), make(chan struct{})
	r.ExposeAsync("answerWhenTold", func(ctx context.Context, _ []json.RawMessage) (any, error) {
		close(called)
		<-settle
		return 1, nil
	})
	r.Expose("noteJS", func(js *Ctx, _ Value, args []Value) (any, error) {
		l.note(args[0].String())
		return nil, nil
	})
	if _, err := r.Eval("chain.js", `globalThis.chain = {
		async awaitGo() { await __clack.answerWhenTold(); __clack.noteJS('settled'); },
		timers() {
			setTimeout(() => __clack.noteJS('timer 60'), 60);
			setTimeout(() => __clack.noteJS('timer 150'), 150);
		},
	}`); err != nil {
		t.Fatal(err)
	}
	queued := func(n int) {
		t.Helper()
		for dl := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
			r.mu.Lock()
			got := len(r.queue) + len(r.urgent)
			r.mu.Unlock()
			if got >= n {
				return
			}
			if time.Now().After(dl) {
				t.Fatalf("%d tasks queued, want %d", got, n)
			}
		}
	}

	t.Run("a Go answer", func(t *testing.T) {
		done := make(chan error, 1)
		go func() {
			_, err := r.Call(UserBlocking(context.Background()), "chain.awaitGo")
			done <- err
		}()
		<-called
		time.Sleep(userChain + 20*time.Millisecond) // the chain ends
		g := l.hold("G", true)
		l.post(r.Post, "render")
		close(settle) // the answer comes while G runs
		queued(2)
		close(g)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		l.drain()
		if got, want := l.got(), "G, render, settled"; got != want {
			t.Errorf("ran %s, want %s: an answer past the chain kept the user's lane", got, want)
		}
	})

	t.Run("timers", func(t *testing.T) {
		l.mu.Lock()
		l.order = nil
		l.mu.Unlock()
		t0 := time.Now()
		// timer 60 is set within the chain, timer 150 comes due past it
		if _, err := r.Call(UserBlocking(context.Background()), "chain.timers"); err != nil {
			t.Fatal(err)
		}
		h := l.hold("H", true)
		g := l.hold("G", false) // G and render go in one batch, after H
		l.post(r.Post, "render")
		close(h)
		time.Sleep(time.Until(t0.Add(200 * time.Millisecond))) // G holds the loop past both timers and the chain
		close(g)
		for dl := time.Now().Add(5 * time.Second); !strings.Contains(l.got(), "timer 150"); time.Sleep(time.Millisecond) {
			if time.Now().After(dl) {
				t.Fatalf("ran %s", l.got())
			}
		}
		l.drain()
		if got, want := l.got(), "H, G, render, timer 60, timer 150"; got != want {
			t.Errorf("ran %s, want %s: a timer fired past the chain kept the user's lane", got, want)
		}
	})
}

// A user-blocking timer runs after the in-order tasks queued before it
// came due, as every piece of the user's work runs after the input before
// it (PostInOrder), and ahead of the queued renders. It used to skip them.
func TestUserTimerAfterInOrderTasks(t *testing.T) {
	l := newLaneRecorder(t)
	r := l.r
	r.Expose("noteJS", func(js *Ctx, _ Value, args []Value) (any, error) {
		l.note(args[0].String())
		return nil, nil
	})
	if _, err := r.Eval("timer.js", `globalThis.lane = { timer() { setTimeout(() => __clack.noteJS('timer'), 30); } }`); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	if _, err := r.Call(UserBlocking(context.Background()), "lane.timer"); err != nil {
		t.Fatal(err)
	}
	h := l.hold("H", true)
	l.post(r.Post, "render")
	l.post(r.PostInOrder, "store")
	time.Sleep(time.Until(t0.Add(60 * time.Millisecond))) // the timer comes due while H runs
	close(h)
	for dl := time.Now().Add(5 * time.Second); !strings.Contains(l.got(), "render"); time.Sleep(time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatalf("ran %s", l.got())
		}
	}
	l.drain()
	if got, want := l.got(), "H, store, timer, render"; got != want {
		t.Errorf("ran %s, want %s", got, want)
	}
}

// A call marked InOrder (the native UI's dispatch: a reaction, a draft
// sync) runs before the user's later input, as a browser runs the user's
// actions in order; a plain one waits behind it.
func TestInOrderCallBeforeLaterInput(t *testing.T) {
	l := newLaneRecorder(t)
	r := l.r
	r.Expose("noteJS", func(js *Ctx, _ Value, args []Value) (any, error) {
		l.note(args[0].String())
		return nil, nil
	})
	if _, err := r.Eval("note.js", `globalThis.lane = { note(s) { __clack.noteJS(s) } }`); err != nil {
		t.Fatal(err)
	}
	g := l.hold("G", true)
	var wg sync.WaitGroup
	wg.Add(2)
	r.CallThen(context.Background(), "lane.note", func(json.RawMessage, error) { wg.Done() }, "plain")
	r.CallThen(InOrder(context.Background()), "lane.note", func(json.RawMessage, error) { wg.Done() }, "reaction")
	l.post(r.Post, "render")
	l.post(r.PostUserBlocking, "click")
	close(g)
	wg.Wait()
	l.drain()
	if got, want := l.got(), "G, reaction, click, plain, render"; got != want {
		t.Errorf("ran %s, want %s", got, want)
	}
}

// A send's chain (UserChain) goes through Go between its JS steps: its
// patches await the native, which queues the next hop (the sendMessage
// thunk) a moment later, and its check answers Go, which dispatches the
// thunk. While it is in Go the loop starts no queued render for
// userChainHold from its last hop, and the next hop runs first, as Slack
// runs a send in one task; the hop goes on with the chain the native's ctx
// carries, so a store update queued after the send began (the drafts'
// clear at Enter) waits behind it, not ahead. Other user-blocking work
// holds nothing: a render queued behind a click's await of Go starts at
// once, and the click's next hop runs after what was queued before it.
func TestSendChainHoldsTheQueue(t *testing.T) {
	l := newLaneRecorder(t)
	r := l.r
	rendered := make(chan struct{}) // the click case: its hop comes once the render started
	r.ExposeAsync("native", func(ctx context.Context, args []json.RawMessage) (any, error) {
		var key string
		_ = json.Unmarshal(args[0], &key)
		if key == "click" {
			<-rendered
		} else {
			time.Sleep(2 * time.Millisecond) // the native's own work
		}
		l.post(r.PostInOrder, key+" store") // a store update meanwhile
		done := make(chan struct{})
		r.CallThen(UserBlocking(ctx), "chain.next", func(json.RawMessage, error) { close(done) }, key)
		<-done
		time.Sleep(50 * time.Millisecond) // chat.postMessage
		return nil, nil
	})
	r.Expose("noteJS", func(js *Ctx, _ Value, args []Value) (any, error) {
		l.note(args[0].String())
		return nil, nil
	})
	if _, err := r.Eval("chain.js", `globalThis.chain = {
		async send(key) { __clack.noteJS(key); await __clack.native(key); },
		next(key) { __clack.noteJS(key + ' next') },
	}`); err != nil {
		t.Fatal(err)
	}
	// the check answers Go, which dispatches the thunk 2 ms later
	t.Run("check, then the thunk", func(t *testing.T) {
		l.mu.Lock()
		l.order = nil
		l.mu.Unlock()
		ctx := UserChain(context.Background())
		g := l.hold("G", true)
		l.post(r.Post, "render")
		done := make(chan error, 1)
		r.CallThen(ctx, "__clack.noteJS", func(_ json.RawMessage, err error) {
			if err != nil {
				done <- err
				return
			}
			time.Sleep(2 * time.Millisecond)
			r.CallThen(ctx, "__clack.noteJS", func(_ json.RawMessage, err error) { done <- err }, "thunk")
		}, "check")
		close(g)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		l.drain()
		if got, want := l.got(), "G, check, thunk, render"; got != want {
			t.Errorf("ran %s, want %s", got, want)
		}
	})
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want string
	}{
		// the send's hop goes ahead of the store update that came during it,
		// as Slack's send is one task; a click's continuation runs after
		// what came before it
		{"send", UserChain(context.Background()), "G, send, send next, send store, render"},
		{"click", UserBlocking(context.Background()), "G, click, render, click store, click next"},
	} {
		l.mu.Lock()
		l.order = nil
		l.mu.Unlock()
		g := l.hold("G", true)
		click := tc.name == "click"
		l.r.Post(func(*Ctx) {
			l.note("render")
			if click {
				close(rendered)
			}
		})
		done := make(chan error, 1)
		r.CallThen(tc.ctx, "chain.send", func(_ json.RawMessage, err error) { done <- err }, tc.name)
		close(g)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		l.drain()
		if got := l.got(); got != tc.want {
			t.Errorf("%s: ran %s, want %s", tc.name, got, tc.want)
		}
	}
}
