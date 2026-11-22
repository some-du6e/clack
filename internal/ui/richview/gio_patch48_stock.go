//go:build stockgio

package richview

import (
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
)

// Stock Gio v0.10.3 has no platform double-click time or window click
// count (see gio_patch48.go): Chromium's Linux rule, 500 ms and 2 px from
// the last press, the fourth click a single again.

func doubleClickTime() time.Duration { return 500 * time.Millisecond }

type clickCounter struct {
	n  int
	at time.Duration
	p  f32.Point
}

func (c *clickCounter) press(_ input.Source, e pointer.Event) int {
	d := e.Position.Sub(c.p)
	if c.n > 0 && c.n < 3 && e.Time-c.at <= doubleClickTime() && d.X >= -2 && d.X <= 2 && d.Y >= -2 && d.Y <= 2 {
		c.n++
	} else {
		c.n = 1
	}
	c.at, c.p = e.Time, e.Position
	return c.n
}

func clicksOf(pointer.Event) int { return 0 }
