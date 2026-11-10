package uipref

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/store"
)

func TestClockTime24(t *testing.T) {
	t.Cleanup(Reset)
	at := time.Date(2026, 11, 8, 15, 4, 0, 0, time.UTC)
	if Clock(at) != "3:04 PM" || ShortClock(at) != "3:04" || Layout("Mon Jan 2 at 3:04 PM") != "Mon Jan 2 at 3:04 PM" {
		t.Fatalf("12-hour: %q %q", Clock(at), ShortClock(at))
	}
	Set(store.Prefs{Time24: true})
	if Clock(at) != "15:04" || ShortClock(at) != "15:04" || at.Format(Layout("Mon Jan 2 at 3:04 PM")) != "Sun Nov 8 at 15:04" {
		t.Fatalf("time24: %q %q %q", Clock(at), ShortClock(at), at.Format(Layout("Mon Jan 2 at 3:04 PM")))
	}
}

func TestDefaultsAndGen(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	if p := Get(); !p.Jumbomoji || !p.ShowTyping || p.UnderlineLinks {
		t.Fatalf("defaults %+v", p)
	}
	g := Gen()
	Set(store.Prefs{Jumbomoji: true, ShowTyping: true, ConvertEmoticons: true, ColorNames: true})
	if Gen() != g {
		t.Error("same layout prefs bumped the generation")
	}
	Set(store.Prefs{Jumbomoji: true, UnderlineLinks: true})
	if Gen() == g {
		t.Error("underline_links didn't bump the layout generation")
	}
}
