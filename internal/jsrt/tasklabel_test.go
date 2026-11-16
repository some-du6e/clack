package jsrt

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// logSink collects a Runtime's log lines.
type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (s *logSink) log(level, msg string) {
	s.mu.Lock()
	s.lines = append(s.lines, level+": "+msg)
	s.mu.Unlock()
}

// wait returns the first line containing sub, failing after 10 s.
func (s *logSink) wait(t *testing.T, sub string) string {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		s.mu.Lock()
		for _, l := range s.lines {
			if strings.Contains(l, sub) {
				s.mu.Unlock()
				return l
			}
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Fatalf("no log line with %q in %q", sub, s.lines)
	return ""
}

// "uncaught (in job): jsrt: timeout" once said nothing about which task
// ran the job; it names the task now (here the Call's path) and how long
// it ran.
func TestTimeoutInJobNamesTheTask(t *testing.T) {
	var sink logSink
	r := newTest(t, func(o *Options) { o.Timeout = 200 * time.Millisecond; o.Log = sink.log })
	if err := r.Do(func(js *Ctx) error {
		_, err := js.Eval("flood.js", `globalThis.floodJobs = () => { (function f() { Promise.resolve().then(f) })(); return 1 }`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Call(context.Background(), "floodJobs"); err != nil {
		t.Fatal(err)
	}
	l := sink.wait(t, "uncaught (in job)")
	if !strings.Contains(l, "uncaught (in job) in Call floodJobs, running ") || !strings.Contains(l, "jsrt: timeout") {
		t.Errorf("timeout line %q doesn't name the Call", l)
	}
	if got := evalString(t, r, `'alive'`); got != "alive" {
		t.Fatalf("after the flood: %q", got)
	}

	// a Post names the Go function that queued it
	r.Post(func(js *Ctx) {
		_, _ = js.Eval("flood2.js", `(function f() { Promise.resolve().then(f) })()`)
	})
	l = sink.wait(t, "in Post from ")
	if !strings.Contains(l, "Post from jsrt.TestTimeoutInJobNamesTheTask") {
		t.Errorf("timeout line %q doesn't name the Post's caller", l)
	}
}

// The watchdog's "abandoning the JS runtime" line names the stuck task.
func TestWatchdogNamesTheStuckTask(t *testing.T) {
	var sink logSink
	r := newTest(t, func(o *Options) { o.Timeout = 200 * time.Millisecond; o.Log = sink.log })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	r.Expose("stuck", func(js *Ctx, this Value, args []Value) (any, error) {
		select {
		case <-release:
		case <-time.After(30 * time.Second):
		}
		return nil, nil
	})
	err := r.Do(func(js *Ctx) error {
		_, err := js.Eval("stuck.js", `__clack.stuck()`)
		return err
	})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("stuck task: %v", err)
	}
	l := sink.wait(t, "abandoning the JS runtime")
	if !strings.Contains(l, "a task (Do from jsrt.TestWatchdogNamesTheStuckTask, running ") {
		t.Errorf("watchdog line %q doesn't name the task", l)
	}
}

// Timers are named by id.
func TestTaskLabel(t *testing.T) {
	for _, tc := range []struct {
		t    task
		want string
	}{
		{task{kind: "timer", what: "12"}, "timer 12"},
		{task{kind: "Call", what: "taut.rtmEvent"}, "Call taut.rtmEvent"},
		{task{kind: "setImmediate"}, "setImmediate"},
	} {
		if got := tc.t.label(); got != tc.want {
			t.Errorf("label %q, want %q", got, tc.want)
		}
	}
	tk := task{kind: "Post", pc: callerPC(0)}
	if got := tk.label(); got != "Post from jsrt.TestTaskLabel" {
		t.Errorf("Post label %q", got)
	}
}
