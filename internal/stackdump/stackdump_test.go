package stackdump

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

// One dump record stays under the cap with the stalled goroutines first,
// and says how much it left out. A dump of every goroutine went whole into
// one record, up to 16 MiB: it rotated the 5 MiB clack.log by itself and
// filled a log grab's last 1 MiB.
func TestLogCapsTheDump(t *testing.T) {
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4000 { // parked goroutines: a dump well past the cap
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-stop
		}()
	}
	defer func() {
		close(stop)
		wg.Wait()
	}()
	time.Sleep(100 * time.Millisecond)
	const limit = 512 << 10
	if n := len(All()); n <= limit {
		t.Fatalf("setup: the dump is %d bytes, not past %d", n, limit)
	}
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(prev)
		log.SetFlags(flags)
	}()
	l := Limiter{Every: time.Hour}
	if !l.Log("stackdump test: the goroutines", "stackdump.TestLogCapsTheDump(") {
		t.Fatal("the limiter let no dump through")
	}
	out := buf.String()
	if len(out) > limit+1024 {
		t.Errorf("one record of %d bytes, want at most %d and a note", len(out), limit)
	}
	if !strings.Contains(out, "bytes more of goroutines left out") {
		t.Error("the cut record doesn't say what it left out")
	}
	head, _, _ := strings.Cut(out, "all goroutines:")
	if !strings.HasPrefix(out, "stackdump test: the goroutines:\n") || !strings.Contains(head, "stackdump.TestLogCapsTheDump(") {
		t.Error("the stalled goroutine doesn't come first")
	}
}
