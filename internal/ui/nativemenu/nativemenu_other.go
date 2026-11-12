//go:build !windows && !(darwin && cgo)

package nativemenu

import (
	"image"

	"gioui.org/io/event"
)

func noteEvent(event.Event) {}

func supported() bool { return Fake != nil }

func popup(_ uintptr, _ func(func()), items []Item, pt image.Point, done func(int)) {
	if Fake != nil {
		done(Fake(items, pt))
		return
	}
	done(-1)
}
