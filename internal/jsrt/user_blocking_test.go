package jsrt

import (
	"context"
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
	hold := func() chan struct{} {
		c := make(chan struct{})
		r.Post(func(*Ctx) { <-c }) // the task running when the others come
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
