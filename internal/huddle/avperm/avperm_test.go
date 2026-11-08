package avperm

import (
	"runtime"
	"testing"
	"time"
)

func TestRequestAnswersOnce(t *testing.T) {
	for _, m := range []Media{Mic, Camera} {
		st := Check(m)
		if runtime.GOOS != "darwin" && st != Authorized {
			t.Fatalf("%v: %v off macOS", m, st)
		}
		if st == NotDetermined {
			continue // CI's macOS runner would show (or hang on) a prompt
		}
		select {
		case got := <-Request(m):
			if got != st {
				t.Fatalf("%v: Request %v, Check %v", m, got, st)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%v: no answer", m)
		}
		t.Logf("%v: %v, settings %q", m, st, SettingsURL(m))
	}
}
