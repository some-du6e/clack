//go:build stockgio

package richview

import (
	"time"
	"unicode"

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

// chromiumWordRange stands in for Gio's ClackWordRange (Chromium's word
// rules, patch 48): the run of letters and digits, of spaces, or the one
// other character at pos, the line's last word for a press on its break.
func chromiumWordRange(rs []rune, pos int, trailingSpace bool) (int, int) {
	n := len(rs)
	if n == 0 {
		return 0, 0
	}
	pos = min(max(pos, 0), n-1)
	if rs[pos] == '\n' && pos > 0 && rs[pos-1] != '\n' {
		pos--
	}
	class := func(r rune) int {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			return 1
		case r != '\n' && unicode.IsSpace(r):
			return 2
		}
		return 0
	}
	c := class(rs[pos])
	lo, hi := pos, pos+1
	if c != 0 {
		for lo > 0 && class(rs[lo-1]) == c {
			lo--
		}
		for hi < n && class(rs[hi]) == c {
			hi++
		}
	}
	if trailingSpace && c == 1 {
		for hi < n && class(rs[hi]) == 2 {
			hi++
		}
	}
	return lo, hi
}
