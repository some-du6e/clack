package messages

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Messages & media > Additional options: time24 and show_typing.
func TestTime24AndShowTyping(t *testing.T) {
	t.Cleanup(uipref.Reset)
	at := time.Date(2026, 11, 8, 17, 30, 0, 0, time.Local)
	uipref.Set(store.Prefs{ShowTyping: true})
	if clock(at) != "5:30 PM" || shortClock(at) != "5:30" || typingText([]string{"Ann"}) != "Ann is typing" {
		t.Fatalf("defaults: %q %q", clock(at), shortClock(at))
	}
	uipref.Set(store.Prefs{Time24: true})
	if clock(at) != "17:30" || shortClock(at) != "17:30" {
		t.Errorf("time24: %q %q", clock(at), shortClock(at))
	}
	if typingText([]string{"Ann"}) != "" {
		t.Error("show_typing off still shows who's typing")
	}
}
