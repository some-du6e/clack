package huddleui

import (
	"time"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// speakerIconAt is the Test Speaker row's icon at `at` after the clicks
// (offsets from one start), and whether it jiggles.
func speakerIconAt(c *Controller, clicks []time.Duration, at time.Duration) (string, bool) {
	c.spk.mu.Lock()
	c.spk.start, c.spk.click = time.Time{}, time.Time{}
	c.spk.mu.Unlock()
	t0 := time.Unix(1_000_000, 0)
	for _, d := range clicks {
		c.spk.clicked(t0.Add(d))
	}
	name, jiggle, _ := c.spk.icon(t0.Add(at))
	return name, jiggle
}

// testAudioRows is the huddle window's Audio settings menu.
func testAudioRows(c *Controller, s *huddle.Session) []popups.MenuItem {
	return c.audioMenu(s, c.h.Theme(), false)
}

// testSpeakerRow is the menu's Test Speaker row, in the huddle window or
// the bottom bar.
func testSpeakerRow(c *Controller, s *huddle.Session, inBar bool) popups.MenuItem {
	for _, it := range c.audioMenu(s, c.h.Theme(), inBar) {
		if it.Key == "test-audio" {
			return it
		}
	}
	return popups.MenuItem{}
}
