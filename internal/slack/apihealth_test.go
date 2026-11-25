package slack

// Slack's API queue's error counts (client-boot 7quX _countError, build
// 889ac449a32d): ten tries in a row with no answer (CLIENT_UNKNOWN) or ten
// answers of 500 or more pause it for the network or the service; a
// success, a timeout, a cancel, a 429 or any other status reset both
// counts, and a client error resets the service count and back. A local
// server scripts each answer.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// scriptedAPI answers each call with the next mode: "hang" drops the
// connection, "slow" holds it 500 ms, a number answers that status, "" is
// {"ok":true}.
type scriptedAPI struct {
	mu    sync.Mutex
	modes []string
	srv   *httptest.Server
}

func newScriptedAPI(t *testing.T) (*scriptedAPI, *Client) {
	s := &scriptedAPI{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		mode := ""
		if len(s.modes) > 0 {
			mode, s.modes = s.modes[0], s.modes[1:]
		}
		s.mu.Unlock()
		switch mode {
		case "hang":
			if c, _, err := w.(http.Hijacker).Hijack(); err == nil {
				c.Close()
			}
		case "slow":
			select {
			case <-time.After(500 * time.Millisecond):
			case <-r.Context().Done():
			}
		case "":
			w.Write([]byte(`{"ok":true}`))
		default:
			n, _ := strconv.Atoi(mode)
			w.WriteHeader(n)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s, New(Creds{Token: "xoxc-test", Team: "T1", TeamURL: s.srv.URL + "/"})
}

func (s *scriptedAPI) script(modes ...string) {
	s.mu.Lock()
	s.modes = append(s.modes, modes...)
	s.mu.Unlock()
}

func repeat(mode string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = mode
	}
	return out
}

func TestAPIErrorCountsLikeSlack(t *testing.T) {
	s, c := newScriptedAPI(t)
	var clientHits, serviceHits atomic.Int32
	setAPIHealthHooks(c, func() { clientHits.Add(1) }, func() { serviceHits.Add(1) })
	n := 0
	call := func(timeout time.Duration) {
		t.Helper()
		n++
		ctx := NoBackoff(context.Background()) // each call is its own: the guard holds none back
		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		// a write: Chromium's resend on a dead pooled connection (Client.do)
		// is for reads, so each hang-up is one try
		_ = c.Call(ctx, "chat.postMessage", url.Values{"channel": {"C1"}, "text": {"synthetic " + strconv.Itoa(n)}}, nil)
	}
	calls := func(mode string, k int) {
		t.Helper()
		s.script(repeat(mode, k)...)
		for i := 0; i < k; i++ {
			call(0)
		}
	}
	counts := func(what string, wantClient, wantService int) {
		t.Helper()
		if cl, sv := apiErrors(c); cl != wantClient || sv != wantService {
			t.Errorf("%s: %d client and %d service errors, want %d and %d", what, cl, sv, wantClient, wantService)
		}
	}

	calls("hang", 9)
	counts("nine tries with no answer", 9, 0)
	if clientHits.Load() != 0 {
		t.Fatal("the client hook ran before the tenth")
	}
	calls("hang", 1)
	counts("ten", 10, 0)
	if clientHits.Load() != 1 {
		t.Fatalf("the client hook ran %d times at the tenth try with no answer, want once", clientHits.Load())
	}

	resetAPIErrors(c) // the wait ended (_unpause)
	calls("hang", 9)
	calls("", 1)
	counts("nine, then a success", 0, 0)
	calls("hang", 9)
	s.script("slow")
	call(50 * time.Millisecond) // CLIENT_TIMEOUT
	counts("nine, then a timeout", 0, 0)
	calls("hang", 9)
	calls("403", 1)
	counts("nine, then another status", 0, 0)
	if clientHits.Load() != 1 {
		t.Errorf("the client hook ran again (%d) without ten in a row", clientHits.Load())
	}

	calls("500", 9)
	counts("nine 500s", 0, 9)
	calls("hang", 1)
	counts("nine 500s, then no answer", 1, 0)
	calls("500", 9)
	calls("", 1)
	counts("nine 500s, then a success", 0, 0)
	calls("502", 10)
	counts("ten answers of 500 or more", 0, 10)
	if serviceHits.Load() != 1 {
		t.Errorf("the service hook ran %d times at the tenth, want once", serviceHits.Load())
	}
	calls("503", 1)
	if serviceHits.Load() != 2 {
		t.Errorf("past the threshold the service hook ran %d times, want again at each error (the queue's next tick)", serviceHits.Load())
	}
}
