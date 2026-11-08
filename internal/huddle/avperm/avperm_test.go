package avperm

import (
	"os"
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

// TestBundleUsage runs from inside a Clack.app (the huddle workflow's
// macOS job): with the usage strings in Info.plist, Check reports TCC's
// answer; without them it must say NoUsageString instead of asking (which
// would kill the process).
func TestBundleUsage(t *testing.T) {
	want := os.Getenv("AVPERM_EXPECT")
	if want == "" {
		t.Skip("AVPERM_EXPECT not set (run from a test .app bundle)")
	}
	for _, m := range []Media{Mic, Camera} {
		st := Check(m)
		t.Logf("%v: %v", m, st)
		switch want {
		case "usage":
			if st == NoUsageString {
				t.Fatalf("%v: the bundle's usage string isn't seen", m)
			}
		case "nousage":
			if st != NoUsageString {
				t.Fatalf("%v: %v, want %v", m, st, NoUsageString)
			}
			select { // and Request answers without asking
			case got := <-Request(m):
				if got != NoUsageString {
					t.Fatalf("%v: Request %v", m, got)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("%v: Request hung", m)
			}
		}
	}
}
