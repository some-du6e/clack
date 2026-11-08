//go:build !windows

package composer

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A tool whose child keeps the stdout pipe open (or that hangs) can't hold
// attRun past its context plus WaitDelay.
func TestAttRunHangingChild(t *testing.T) {
	for _, c := range []struct {
		name, script, want string
	}{
		{"grandchild holds stdout", "(sleep 30) & echo hi", "hi"},
		{"hangs after output", "echo hi; (sleep 30) & sleep 30", ""},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		t0 := time.Now()
		out, err := attRun(ctx, 1<<20, "sh", "-c", c.script)
		cancel()
		d := time.Since(t0)
		if d > 500*time.Millisecond+attWaitDelay+2*time.Second {
			t.Errorf("%s: attRun took %v", c.name, d)
		}
		if c.want != "" && strings.TrimSpace(string(out)) != c.want && err == nil {
			t.Errorf("%s: out %q", c.name, out)
		}
		t.Logf("%s: %v (err %v)", c.name, d.Round(10*time.Millisecond), err)
	}
	// over the limit: killed, errAttTooBig
	if _, err := attRun(context.Background(), 10, "sh", "-c", "yes | head -c 100000; sleep 30"); err != errAttTooBig {
		t.Errorf("big output: err %v, want errAttTooBig", err)
	}
}
